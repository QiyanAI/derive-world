// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/handoff"
	"github.com/QiyanAI/derive-world/internal/model"
	"github.com/QiyanAI/derive-world/internal/trust"
	"github.com/QiyanAI/derive-world/internal/worldseed"
)

const (
	maxMessageBytes = 16 * 1024
	maxActions      = 5
)

// ObjectAccess is the already-loaded object snapshot supplied by the request
// handler. The runtime revalidates membership, delegation, version, and
// permissions inside the final transaction.
type ObjectAccess struct {
	ObjectID          uuid.UUID
	ObjectType        string
	Version           int
	State             json.RawMessage
	SharedContext     json.RawMessage
	MemberPermissions []string
}

// SendMessageInput is the trusted runtime input.
type SendMessageInput struct {
	UserID         uuid.UUID
	ConversationID uuid.UUID
	Content        string
	Intent         string
	Object         *ObjectAccess
	Route          string
	IdemKey        string
	LeaseUntil     time.Time
	RequestID      string
	IPHash         []byte
}

// AcceptedAction reports a proposal or confirmation that was actually
// persisted. Unsupported or unauthorized model output is omitted.
type AcceptedAction struct {
	Kind           string     `json:"kind"`
	ProposalID     *uuid.UUID `json:"proposal_id,omitempty"`
	ConfirmationID *uuid.UUID `json:"confirmation_id,omitempty"`
	Action         string     `json:"action"`
}

// SendMessageResult is the API result of one model turn.
type SendMessageResult struct {
	UserMessage      Message          `json:"user_message"`
	AssistantMessage Message          `json:"assistant_message"`
	ModelMode        string           `json:"model_mode"`
	ParseStatus      string           `json:"parse_status"`
	ProposedActions  []AcceptedAction `json:"proposed_actions"`
	// RejectedActions counts model-proposed actions that the server refused to
	// turn into a proposal or confirmation. It keeps "the model said nothing"
	// distinguishable from "the model proposed something invalid".
	RejectedActions int `json:"rejected_actions,omitempty"`
	// Rejections explains each refused action in user-facing language. The
	// count remains for compatibility; the list is what the UI shows.
	Rejections   []ActionRejection `json:"rejections,omitempty"`
	ContextStale bool              `json:"context_stale,omitempty"`
	Usage        model.Usage       `json:"usage"`
}

// ActionRejection is the bounded, non-secret reason one model action was not
// turned into a proposal or confirmation. Stage is a stable diagnostic code;
// Message is a localized sentence safe to render in chat.
type ActionRejection struct {
	CapabilityID string `json:"capability_id"`
	Stage        string `json:"stage"`
	Message      string `json:"message"`
}

type aiTurn struct {
	inputChars        int
	prompt            string
	messages          []model.Message
	reply             string
	actions           []rawAction
	parseStatus       string
	rawOutput         string
	mode              string
	response          model.Response
	durationMS        int
	invocationID      uuid.UUID
	requestedCreation bool
	requestedChange   bool
}

// SendMessage performs exactly one provider call, persists the private
// messages, records metering, and creates only server-authorized proposals or
// confirmations. Model text never becomes an execution instruction.
func (s *Service) SendMessage(ctx context.Context, input SendMessageInput) (*SendMessageResult, error) {
	content := strings.TrimSpace(input.Content)
	if content == "" || strings.TrimSpace(input.Content) == "" {
		return nil, fmt.Errorf("%w: content", ErrInvalidRequest)
	}
	if len([]byte(input.Content)) > maxMessageBytes {
		return nil, ErrPayloadTooLarge
	}
	conversation, err := loadConversation(ctx, s.db, input.UserID, input.ConversationID)
	if err != nil {
		return nil, err
	}
	if (conversation.ObjectID == nil) != (input.Object == nil) {
		return nil, ErrForbidden
	}
	if conversation.ObjectID != nil && *conversation.ObjectID != input.Object.ObjectID {
		return nil, ErrNotFound
	}
	profile, err := s.GetProfile(ctx, input.UserID)
	if err != nil {
		return nil, err
	}

	var projection *handoff.Projection
	if input.Object != nil {
		effective, err := s.trust.ResolveAI(ctx, input.UserID, profile.ID,
			&input.Object.ObjectID, input.Object.MemberPermissions)
		if err != nil {
			return nil, mapTrustError(err)
		}
		projection, err = s.handoff.Build(ctx, handoff.Source{
			ObjectID: input.Object.ObjectID, ObjectType: input.Object.ObjectType,
			Version: input.Object.Version, State: input.Object.State,
			SharedContext: input.Object.SharedContext,
			Permissions:   effective.Permissions,
		}, effective.Actor(), input.RequestID, input.IPHash)
		if err != nil {
			if errors.Is(err, handoff.ErrPayloadTooLarge) {
				return nil, ErrPayloadTooLarge
			}
			return nil, err
		}
	} else {
		if _, err := s.trust.ResolveGlobalAI(ctx, input.UserID, profile.ID); err != nil {
			return nil, mapTrustError(err)
		}
	}

	var private *PrivateContext
	if s.includePrivateContext {
		private, _ = s.GetContext(ctx, input.UserID)
	}
	history, err := s.history(ctx, input.ConversationID)
	if err != nil {
		return nil, err
	}
	inputBudget := model.MaxModelInputChars
	if s.quota != nil {
		inputBudget = s.quota.Config().MaxInputChars
	}
	system, messages, inputChars, err := buildPromptWithin(profile, private, projection, history,
		input.Content, input.Intent, inputBudget)
	if err != nil {
		return nil, err
	}
	release, err := s.quota.Reserve(ctx, input.UserID, inputChars)
	if err != nil {
		s.recordQuotaDenial(ctx, input, err)
		return nil, err
	}
	defer release(inputChars)

	// Reserve a durable one-call slot before any provider request. The row is
	// counted by the day-usage queries, so a crash, a rollback of the business
	// transaction, or a failed final update cannot make a possible billable
	// call disappear. If this insert fails, no provider call is made.
	mode := s.providerMode()
	invocationID, err := uuid.NewV7()
	if err != nil {
		release(inputChars)
		return nil, fmt.Errorf("model invocation id: %w", err)
	}
	reservedModel := s.modelName
	if reservedModel == "" {
		reservedModel = "unknown"
	}
	if runes := []rune(reservedModel); len(runes) > 120 {
		reservedModel = string(runes[:120])
	}
	if err := s.recorder.Reserve(ctx, s.db, model.Invocation{
		ID: invocationID, UserID: input.UserID,
		ConversationID: &input.ConversationID,
		Provider:       mode, Model: reservedModel, InputChars: inputChars,
		OutputChars: 0, DurationMS: 0, Status: "failed", ErrorCode: "in_flight",
	}); err != nil {
		release(inputChars)
		return nil, fmt.Errorf("model invocation reservation: %w", err)
	}

	started := time.Now()
	response, callErr := s.provider.Complete(ctx, model.Request{
		Model: s.modelName, System: system, Messages: messages,
		Temperature: 0.2,
	})
	duration := int(time.Since(started).Milliseconds())
	if callErr != nil {
		s.recordFailure(ctx, input, mode, inputChars, duration, callErr, invocationID)
		return nil, callErr
	}
	reply, actions, parseStatus, err := parseModelOutput(response.Text)
	if err != nil {
		s.recordFailure(ctx, input, mode, inputChars, duration, err, invocationID)
		return nil, err
	}
	turn := &aiTurn{
		inputChars: inputChars, prompt: system, messages: messages,
		reply: reply, actions: actions, parseStatus: parseStatus,
		rawOutput: response.Text, mode: mode, response: response,
		durationMS: duration, invocationID: invocationID,
		requestedCreation: input.Object == nil && input.Intent == "create",
		requestedChange:   input.Object != nil && input.Intent == "change",
	}
	return s.persistTurn(ctx, input, conversation, profile, turn)
}

func (s *Service) persistTurn(
	ctx context.Context,
	input SendMessageInput,
	conversation *Conversation,
	profile *Profile,
	turn *aiTurn,
) (*SendMessageResult, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked Conversation
	err = tx.QueryRow(ctx, `
SELECT id, user_id, object_id, title, created_at, updated_at, deleted_at
  FROM ai_conversations
 WHERE id = $1 AND user_id = $2
   FOR UPDATE`, input.ConversationID.String(), input.UserID.String()).Scan(
		&locked.ID, &locked.UserID, &locked.ObjectID, &locked.Title,
		&locked.CreatedAt, &locked.UpdatedAt, &locked.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && locked.DeletedAt != nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var effective *trust.Effective
	if input.Object != nil {
		effective, err = s.trust.ResolveAITx(ctx, tx, input.UserID, profile.ID, input.Object.ObjectID)
	} else {
		effective, err = s.trust.ResolveGlobalAITx(ctx, tx, input.UserID, profile.ID)
	}
	if err != nil {
		return nil, mapTrustError(err)
	}
	contextStale := input.Object != nil && effective.ObjectVersion != input.Object.Version

	userMessage, err := insertMessageTx(ctx, tx, input.ConversationID, "user", input.Content, turn.mode, nil, nil)
	if err != nil {
		return nil, err
	}

	// A raw provider document must never become chat text. When parsing fell
	// back and the output still looks like a JSON envelope, the assistant
	// message is replaced by a short server-authored sentence and the detailed
	// reason is stored as a bounded notice. Plain prose remains a normal reply:
	// it is a legitimate refusal or answer, not an unreadable document.
	zh := containsCJK(input.Content)
	fallbackReason := ""
	if turn.parseStatus == "fallback" && looksLikeModelJSON(turn.reply) {
		turn.reply, fallbackReason = fallbackReply(zh, turn.response.FinishReason)
	}
	notices := make([]string, 0, 4)
	rejections := make([]ActionRejection, 0, len(turn.actions))
	addRejection := func(capabilityID, stage string) {
		message := rejectionExplanation(stage, zh)
		if message == "" {
			return
		}
		rejections = append(rejections, ActionRejection{
			CapabilityID: capabilityID, Stage: stage, Message: message,
		})
		notices = append(notices, message)
	}
	if _, err := tx.Exec(ctx, `
UPDATE ai_conversations SET updated_at = GREATEST(now(), updated_at) WHERE id = $1`,
		input.ConversationID.String()); err != nil {
		return nil, err
	}
	invocationID := turn.invocationID
	conversationID := input.ConversationID
	modelName := turn.response.Model
	if modelName == "" {
		modelName = s.modelName
	}
	if modelName == "" {
		modelName = "unknown"
	}
	if runes := []rune(modelName); len(runes) > 120 {
		modelName = string(runes[:120])
	}
	invocation := model.Invocation{
		ID: invocationID, UserID: input.UserID, ConversationID: &conversationID,
		Provider: turn.mode, Model: modelName, InputChars: turn.inputChars,
		OutputChars: utf8.RuneCountInString(turn.rawOutput),
		DurationMS:  turn.durationMS, Status: "succeeded",
	}
	completed, err := s.recorder.CompleteTx(ctx, tx, invocation)
	if err != nil {
		return nil, err
	}
	if !completed {
		// The reservation row is normally present. If it is not, still record
		// the completed call in this transaction rather than losing it.
		if err := s.recorder.RecordTx(ctx, tx, invocation); err != nil {
			return nil, err
		}
	}

	accepted := make([]AcceptedAction, 0)
	if !contextStale && turn.parseStatus == "structured" &&
		(input.Object != nil || len(turn.actions) > 0) {
		if err := s.trust.LockAIActionBudgetTx(ctx, tx, input.UserID); err != nil {
			return nil, err
		}
		remaining, err := s.trust.RemainingAIActionsTx(ctx, tx, input.UserID,
			effective.AIProfileID, effective.MaxAIActionsPerHour)
		if err != nil {
			return nil, err
		}
		acceptedWorldCreate := false
		repairs := make([]*seedRepair, 0, 1)
		for _, action := range turn.actions {
			// One assistant reply may prepare at most one world. A second
			// world.create would render a duplicate confirm button with the same
			// meaning, so it is rejected before any confirmation row exists.
			if input.Object == nil && action.CapabilityID == "world.create" && acceptedWorldCreate {
				s.logActionRejection(action.CapabilityID, "duplicate_action", "")
				addRejection(action.CapabilityID, "duplicate_action")
				continue
			}
			if remaining <= 0 {
				s.logActionRejection(action.CapabilityID, "action_budget", "")
				addRejection(action.CapabilityID, "action_budget")
				break
			}
			var result *AcceptedAction
			var rejection *actionRejection
			var repair *seedRepair
			if input.Object != nil {
				result, rejection, repair, err = s.applyModelAction(ctx, tx, input, effective, action)
			} else {
				result, rejection, repair, err = s.applyGlobalModelAction(ctx, tx, input, effective, action)
			}
			if rejection != nil {
				s.logActionRejection(action.CapabilityID, rejection.Stage, rejection.Detail)
				addRejection(action.CapabilityID, rejection.Stage)
			}
			if err != nil {
				return nil, err
			}
			if result != nil {
				if input.Object == nil && action.CapabilityID == "world.create" {
					acceptedWorldCreate = true
				}
				accepted = append(accepted, *result)
				repairs = append(repairs, repair)
				remaining--
			}
		}
		// A plan the server had to repair still runs, but the human is told
		// that some detail was fitted to what the runtime supports. Silent
		// repair would be a claim the plan is exactly what they asked for.
		if text := seedRepairNotice(mergeRepairs(repairs), zh); text != "" {
			notices = append(notices, text)
		}
	}
	if contextStale {
		addRejection("", "context_stale")
	}
	if fallbackReason != "" {
		notices = append(notices, fallbackNotice(zh, fallbackReason))
	}
	// The model never executes anything. Its reply must therefore never claim
	// that a world or change is already live, and it must never send the user
	// looking for an approval card elsewhere. Any such sentence is replaced by
	// a plain, accurate sentence before it becomes chat history.
	turn.reply = guardAssistantReply(turn.reply, zh, accepted,
		len(turn.actions) > 0, turn.requestedCreation, turn.requestedChange)
	parseStatus := turn.parseStatus
	var notice *string
	if text := joinNoticeText(notices); text != "" {
		notice = &text
	}
	assistantMessage, err := insertMessageTx(ctx, tx, input.ConversationID, "assistant",
		turn.reply, turn.mode, &parseStatus, notice)
	if err != nil {
		return nil, err
	}
	// Link each persisted proposal/confirmation to this exact turn so the UI
	// can render its approval directly under this reply, including after a
	// refresh or a reopen. A linking failure rolls the whole turn back.
	if err := s.linkTurnApprovalsTx(ctx, tx, input.UserID, input.ConversationID, assistantMessage.ID, accepted); err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        effective.Actor(),
		Action:       audit.ActionAIMessage,
		ResourceType: "ai_conversation",
		ResourceID:   &input.ConversationID,
		Decision:     "allow",
		RequestID:    safeRequestID(input.RequestID),
		IPHash:       input.IPHash,
		Metadata:     map[string]any{"conversation_id": input.ConversationID.String()},
	}); err != nil {
		return nil, err
	}
	rejected := 0
	if turn.parseStatus == "structured" {
		rejected = len(turn.actions) - len(accepted)
		if rejected < 0 {
			rejected = 0
		}
		if contextStale {
			rejected = len(turn.actions)
		}
	}
	result := &SendMessageResult{
		UserMessage: userMessage, AssistantMessage: assistantMessage,
		ModelMode: turn.mode, ParseStatus: turn.parseStatus,
		ProposedActions: accepted, RejectedActions: rejected,
		Rejections: rejections, ContextStale: contextStale,
		Usage: turn.response.Usage,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if err := s.idem.Complete(ctx, tx, input.UserID, input.Route, input.IdemKey,
		input.LeaseUntil, 200, encoded); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) applyModelAction(
	ctx context.Context,
	tx pgx.Tx,
	input SendMessageInput,
	effective *trust.Effective,
	action rawAction,
) (*AcceptedAction, *actionRejection, *seedRepair, error) {
	switch action.CapabilityID {
	case "object.propose_change":
		var proposal proposalActionInput
		if err := decodeSingleJSON(string(action.Input), &proposal); err != nil {
			return nil, &actionRejection{Stage: "input_json", Detail: truncateActionDetail(err.Error())}, nil, nil
		}
		if proposal.Action == "" || len(proposal.Input) == 0 {
			return nil, &actionRejection{Stage: "empty_action"}, nil, nil
		}
		if proposal.ObjectID != nil && *proposal.ObjectID != input.Object.ObjectID.String() {
			return nil, &actionRejection{Stage: "object_mismatch"}, nil, nil
		}
		if proposal.ExpectedVersion != nil && *proposal.ExpectedVersion != effective.ObjectVersion {
			return nil, &actionRejection{Stage: "version_mismatch"}, nil, nil
		}
		if !effective.Can("contribute") {
			return nil, &actionRejection{Stage: "permission"}, nil, nil
		}
		proposalID, accepted, err := s.recordProposalAction(ctx, tx, input, effective,
			proposal.Action, proposal.Input)
		if err != nil {
			return nil, nil, nil, err
		}
		if !accepted {
			return nil, &actionRejection{Stage: "proposal_not_accepted"}, nil, nil
		}
		return &AcceptedAction{
			Kind: "proposal", ProposalID: &proposalID, Action: proposal.Action,
		}, nil, nil, nil
	case "object.derive":
		var derive deriveActionInput
		if err := decodeSingleJSON(string(action.Input), &derive); err != nil {
			return nil, &actionRejection{Stage: "input_json", Detail: truncateActionDetail(err.Error())}, nil, nil
		}
		if derive.ObjectID != nil && *derive.ObjectID != input.Object.ObjectID.String() {
			return nil, &actionRejection{Stage: "object_mismatch"}, nil, nil
		}
		if derive.ExpectedVersion != nil && *derive.ExpectedVersion != effective.ObjectVersion {
			return nil, &actionRejection{Stage: "derive_version"}, nil, nil
		}
		if !effective.Can("derive") {
			return nil, &actionRejection{Stage: "derive_permission"}, nil, nil
		}
		confirmationID, accepted, err := s.recordDeriveConfirmation(ctx, tx, input, effective, derive)
		if err != nil {
			return nil, nil, nil, err
		}
		if !accepted {
			return nil, &actionRejection{Stage: "derive_not_accepted"}, nil, nil
		}
		return &AcceptedAction{
			Kind: "confirmation", ConfirmationID: &confirmationID,
			Action: trust.ConfirmationDeriveCreate,
		}, nil, nil, nil
	case "world.reimagine":
		if input.Object == nil || input.Object.ObjectType != "open_world" || s.proposals == nil {
			return nil, &actionRejection{Stage: "reimagine_unsupported"}, nil, nil
		}
		if !effective.Can("contribute") {
			return nil, &actionRejection{Stage: "permission"}, nil, nil
		}
		var reimagine worldReimagineInput
		if err := decodeSingleJSON(string(action.Input), &reimagine); err != nil {
			return nil, &actionRejection{Stage: "reimagine_decode", Detail: truncateActionDetail(err.Error())}, nil, nil
		}
		if len(reimagine.Brief) == 0 {
			return nil, &actionRejection{Stage: "reimagine_decode", Detail: "empty brief"}, nil, nil
		}
		seed, repair, decodeRejection := decodeReimagineBrief(reimagine.Brief, input.Object.State)
		if decodeRejection != nil {
			return nil, decodeRejection, nil, nil
		}
		result, err := worldseed.Reshape(input.Object.State, seed)
		if err != nil {
			return nil, &actionRejection{Stage: "reimagine_reshape", Detail: truncateActionDetail(err.Error())}, nil, nil
		}
		encoded, err := json.Marshal(result.State)
		if err != nil {
			return nil, &actionRejection{Stage: "reimagine_encode", Detail: truncateActionDetail(err.Error())}, nil, nil
		}
		summary := strings.TrimSpace(reimagine.Summary)
		if summary == "" || utf8.RuneCountInString(summary) > 500 {
			summary = result.Summary
		}
		payload, err := json.Marshal(map[string]any{
			"note": summary, "content": json.RawMessage(encoded),
		})
		if err != nil {
			return nil, &actionRejection{Stage: "reimagine_encode", Detail: truncateActionDetail(err.Error())}, nil, nil
		}
		proposalID, accepted, err := s.proposals.CreateProposalTx(ctx, tx,
			input.Object.ObjectID, input.UserID, effective.AIProfileID,
			effective.DelegationID, "world.reimagine", "reshape_world", payload,
			effective.ObjectVersion, input.RequestID, input.IPHash)
		if err != nil {
			return nil, nil, nil, err
		}
		if !accepted {
			return nil, &actionRejection{Stage: "proposal_not_accepted"}, nil, nil
		}
		return &AcceptedAction{
			Kind: "proposal", ProposalID: &proposalID, Action: "reshape_world",
		}, nil, repair, nil
	default:
		return nil, &actionRejection{Stage: "capability_id"}, nil, nil
	}
}

// applyGlobalModelAction accepts only a world.create proposal from an unbound
// conversation. Object-bound actions remain impossible here, so one world's
// private context can never be smuggled into a global turn.
type actionRejection struct {
	Stage  string
	Detail string
}

// seedRepair records what the AI authoring path had to change before a brief
// could pass the strict validator. It is diagnostic only: the repaired brief
// still runs through every type, range and enum check.
type seedRepair struct {
	Dropped []string
	Clamped bool
}

func (repair *seedRepair) empty() bool {
	return repair == nil || (len(repair.Dropped) == 0 && !repair.Clamped)
}

func mergeRepairs(repairs []*seedRepair) *seedRepair {
	merged := &seedRepair{}
	for _, repair := range repairs {
		if repair == nil {
			continue
		}
		merged.Dropped = append(merged.Dropped, repair.Dropped...)
		merged.Clamped = merged.Clamped || repair.Clamped
	}
	return merged
}

// repairingSeedBrief applies the two repairs that are lossless for the product:
// documented-but-alternate shapes (a palette written as prose, audio listed as
// a tab), and out-of-range presentation numbers moved to the documented bound.
// Anything ambiguous is left for the strict decoder to reject.
func repairingSeedBrief(raw json.RawMessage) (json.RawMessage, *seedRepair) {
	repair := &seedRepair{}
	normalized, _ := normalizingSeedBrief(raw)
	if clamped, changed := worldseed.ClampBriefNumbers(normalized); changed {
		normalized = clamped
		repair.Clamped = true
	}
	return normalized, repair
}

// decodeReimagineBrief applies the same bounded repairs to a world.reimagine
// brief as to a world.create brief: documented alternate shapes are normalized,
// presentation numbers are clamped to their documented bounds, and unknown
// field names are removed only when the rest still validates. The strict
// decode, range and enum checks still run on the result, so a change that is
// ambiguous or beyond the runtime is rejected instead of half-applied.
func decodeReimagineBrief(raw, state json.RawMessage) (*worldseed.Brief, *seedRepair, *actionRejection) {
	normalized, repair := repairingSeedBrief(raw)
	// A change that adds content may name only the things; their kind is the
	// index (or exact name) of a kind the world already has.
	if completed, changed := adoptingExistingKinds(normalized, state); changed {
		normalized = completed
	}
	seed, dropped, err := worldseed.DecodeRepairing(normalized)
	if err != nil {
		return nil, nil, &actionRejection{Stage: "reimagine_decode", Detail: truncateActionDetail(err.Error())}
	}
	repair.Dropped = dropped
	return seed, repair, nil
}

// buildWorldCreateContent validates the model's world.create input before any
// confirmation row is written. The rejection stage lets the server record a
// redacted diagnostic without persisting the raw model output.
func buildWorldCreateContent(create worldCreateActionInput) (json.RawMessage, *actionRejection) {
	content, _, rejection := buildWorldCreate(create)
	return content, rejection
}

func buildWorldCreate(create worldCreateActionInput) (json.RawMessage, *seedRepair, *actionRejection) {
	title := strings.TrimSpace(create.Title)
	if title == "" || (len(create.Content) == 0 && len(create.Brief) == 0) {
		return nil, nil, &actionRejection{Stage: "title_or_body"}
	}
	if len(create.Brief) == 0 {
		return create.Content, nil, nil
	}
	// Benign provider shapes (audio listed as a tab) and out-of-range
	// presentation numbers are normalized before the strict compiler sees them;
	// the normalized brief still passes through the full Decode, Compose and
	// ValidateCreate chain. An unrecognized field name is removed only when the
	// rest of the brief still validates on its own.
	brief, repair := repairingSeedBrief(create.Brief)
	seed, dropped, err := worldseed.DecodeRepairing(brief)
	repair.Dropped = dropped
	if err != nil {
		return nil, nil, &actionRejection{Stage: "seed_decode", Detail: truncateActionDetail(err.Error())}
	}
	composition, err := worldseed.Compose(seed, title)
	if err != nil {
		return nil, nil, &actionRejection{Stage: "seed_compose", Detail: truncateActionDetail(err.Error())}
	}
	encoded, err := json.Marshal(composition.Content)
	if err != nil {
		return nil, nil, &actionRejection{Stage: "content_encode", Detail: truncateActionDetail(err.Error())}
	}
	return encoded, repair, nil
}

func truncateActionDetail(value string) string {
	const limit = 200
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (s *Service) logActionRejection(capabilityID, stage, detail string) {
	if s.logger == nil {
		return
	}
	s.logger.Error("personal_ai_action_rejected",
		"capability_id", capabilityID, "stage", stage, "detail", detail)
}

func (s *Service) applyGlobalModelAction(
	ctx context.Context,
	tx pgx.Tx,
	input SendMessageInput,
	effective *trust.Effective,
	action rawAction,
) (*AcceptedAction, *actionRejection, *seedRepair, error) {
	if action.CapabilityID != "world.create" {
		return nil, &actionRejection{Stage: "capability_id"}, nil, nil
	}
	if !effective.Can("contribute") {
		return nil, &actionRejection{Stage: "permission"}, nil, nil
	}
	var create worldCreateActionInput
	if err := decodeSingleJSON(string(action.Input), &create); err != nil {
		return nil, &actionRejection{Stage: "input_json", Detail: truncateActionDetail(err.Error())}, nil, nil
	}
	if pending, err := s.trust.HasPendingWorldCreateTx(ctx, tx,
		input.UserID, input.ConversationID); err != nil {
		return nil, nil, nil, err
	} else if pending {
		// One conversation keeps one live plan and therefore one confirm
		// button. The human can deny it before asking for a new one.
		return nil, &actionRejection{Stage: "world_pending"}, nil, nil
	}
	content, repair, rejection := buildWorldCreate(create)
	if rejection != nil {
		return nil, rejection, nil, nil
	}
	confirmation, accepted, err := s.trust.CreateWorldCreateConfirmationTx(ctx, tx,
		trust.WorldCreateConfirmationParams{
			UserID: input.UserID, AIProfileID: effective.AIProfileID,
			DelegationID: effective.DelegationID, Title: create.Title,
			Content: content, RequestID: input.RequestID, IPHash: input.IPHash,
		})
	if err != nil {
		if errors.Is(err, trust.ErrInvalidRequest) || errors.Is(err, trust.ErrPayloadTooLarge) {
			return nil, &actionRejection{Stage: "confirmation_invalid", Detail: truncateActionDetail(err.Error())}, nil, nil
		}
		return nil, nil, nil, err
	}
	if !accepted {
		return nil, &actionRejection{Stage: "confirmation_not_accepted"}, nil, nil
	}
	return &AcceptedAction{
		Kind: "confirmation", ConfirmationID: &confirmation.ID,
		Action: trust.ConfirmationWorldCreate,
	}, nil, repair, nil
}

func (s *Service) recordProposalAction(ctx context.Context, tx pgx.Tx, input SendMessageInput,
	effective *trust.Effective, action string, rawInput json.RawMessage) (uuid.UUID, bool, error) {
	if s.capabilities != nil {
		if !s.capabilities.AllowAI(input.UserID) {
			return uuid.Nil, false, nil
		}
		_, proposalID, accepted, err := s.capabilities.ProposeAITx(ctx, tx,
			input.UserID, effective.AIProfileID, input.Object.ObjectID,
			action, rawInput, effective.ObjectVersion, input.RequestID, input.IPHash)
		return proposalID, accepted, err
	}
	return s.proposals.CreateProposalTx(ctx, tx, input.Object.ObjectID, input.UserID,
		effective.AIProfileID, effective.DelegationID, "object.propose_change",
		action, rawInput,
		effective.ObjectVersion, input.RequestID, input.IPHash)
}

func (s *Service) recordDeriveConfirmation(ctx context.Context, tx pgx.Tx, input SendMessageInput,
	effective *trust.Effective, derive deriveActionInput) (uuid.UUID, bool, error) {
	if s.capabilities != nil {
		if !s.capabilities.AllowAI(input.UserID) {
			return uuid.Nil, false, nil
		}
		payload := map[string]any{
			"title":              derive.Title,
			"inheritance_fields": derive.InheritanceFields,
		}
		if derive.SourceVersion != nil {
			payload["source_version"] = *derive.SourceVersion
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return uuid.Nil, false, nil
		}
		_, confirmationID, accepted, err := s.capabilities.DeriveConfirmationAITx(ctx, tx,
			input.UserID, effective.AIProfileID, input.Object.ObjectID,
			encoded, effective.ObjectVersion, input.RequestID, input.IPHash)
		return confirmationID, accepted, err
	}
	confirmation, err := s.trust.CreateDeriveConfirmationTx(ctx, tx, trust.DeriveConfirmationParams{
		UserID: input.UserID, AIProfileID: effective.AIProfileID,
		DelegationID: effective.DelegationID, ObjectID: input.Object.ObjectID,
		ExpectedVersion: effective.ObjectVersion, Title: derive.Title,
		SourceVersion: derive.SourceVersion, InheritanceFields: derive.InheritanceFields,
		RequestID: input.RequestID, IPHash: input.IPHash,
	})
	if err != nil {
		if errors.Is(err, trust.ErrInvalidRequest) || errors.Is(err, trust.ErrPayloadTooLarge) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	return confirmation.ID, true, nil
}

func (s *Service) history(ctx context.Context, conversationID uuid.UUID) ([]Message, error) {
	rows, err := s.db.Query(ctx, `
SELECT id, conversation_id, role, content, model_mode, parse_status, notice, created_at
  FROM ai_messages
 WHERE conversation_id = $1
 ORDER BY created_at DESC, id DESC
 LIMIT 20`, conversationID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Message, 0, 20)
	for rows.Next() {
		var item Message
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.Role,
			&item.Content, &item.ModelMode, &item.ParseStatus, &item.Notice,
			&item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	return items, nil
}

// linkTurnApprovalsTx records which conversation turn produced each accepted
// proposal or confirmation. The write itself belongs to the owning package
// (objects owns object_proposals, trust owns confirmations); personal AI only
// passes the ids and the exact assistant message. The columns are display-only;
// authorization still revalidates the actor, delegation, version and payload
// hash at execution time.
func (s *Service) linkTurnApprovalsTx(ctx context.Context, tx pgx.Tx,
	userID, conversationID, messageID uuid.UUID, accepted []AcceptedAction) error {
	proposalIDs := make([]uuid.UUID, 0, len(accepted))
	confirmationIDs := make([]uuid.UUID, 0, len(accepted))
	for _, action := range accepted {
		switch {
		case action.Kind == "proposal" && action.ProposalID != nil:
			proposalIDs = append(proposalIDs, *action.ProposalID)
		case action.Kind == "confirmation" && action.ConfirmationID != nil:
			confirmationIDs = append(confirmationIDs, *action.ConfirmationID)
		}
	}
	if len(proposalIDs) > 0 {
		if err := s.proposals.LinkProposalsToMessageTx(ctx, tx,
			userID, conversationID, messageID, proposalIDs); err != nil {
			return err
		}
	}
	if len(confirmationIDs) > 0 {
		if err := s.trust.LinkConfirmationsToMessageTx(ctx, tx,
			userID, conversationID, messageID, confirmationIDs); err != nil {
			return err
		}
	}
	return nil
}

func insertMessageTx(ctx context.Context, tx pgx.Tx, conversationID uuid.UUID, role, content, mode string, parseStatus, notice *string) (Message, error) {
	messageID, err := uuid.NewV7()
	if err != nil {
		return Message{}, err
	}
	var (
		item         Message
		statusResult *string
		noticeResult *string
	)
	err = tx.QueryRow(ctx, `
INSERT INTO ai_messages (id, conversation_id, role, content, model_mode, parse_status, notice)
VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING id, conversation_id, role, content, model_mode, parse_status, notice, created_at`,
		messageID.String(), conversationID.String(), role, content, mode, parseStatus, notice).Scan(
		&item.ID, &item.ConversationID, &item.Role, &item.Content,
		&item.ModelMode, &statusResult, &noticeResult, &item.CreatedAt)
	if err != nil {
		return Message{}, err
	}
	item.ParseStatus = statusResult
	item.Notice = noticeResult
	return item, nil
}

func (s *Service) providerMode() string {
	if provider, ok := s.provider.(model.ModeProvider); ok {
		return provider.Mode()
	}
	return "fixture"
}

// recordQuotaDenial writes a deny audit for a pre-call budget, input, or kill
// switch decision. It uses the frozen rate.limit_hit action because the
// 49-action registry has no separate model-budget action. No prompt content is
// recorded, and an audit failure never changes the already-decided refusal.
func (s *Service) recordQuotaDenial(ctx context.Context, input SendMessageInput, cause error) {
	reason := "quota_denied"
	switch {
	case errors.Is(cause, model.ErrBudgetExceeded):
		reason = "user_budget_exceeded"
	case errors.Is(cause, model.ErrGlobalBudgetExceeded):
		reason = "global_budget_exceeded"
	case errors.Is(cause, model.ErrConcurrencyLimit):
		reason = "concurrency_limit"
	case errors.Is(cause, model.ErrInputTooLarge):
		reason = "input_too_large"
	case errors.Is(cause, model.ErrModelDisabled):
		reason = "kill_switch"
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(recordCtx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		s.logger.Error("model_budget_audit_failed", "error_type", fmt.Sprintf("%T", err))
		return
	}
	defer func() { _ = tx.Rollback(recordCtx) }()
	conversationID := input.ConversationID
	if err := s.audit.Write(recordCtx, tx, audit.Event{
		Actor:        audit.UserActor(input.UserID),
		Action:       audit.ActionRateLimitHit,
		ResourceType: "ai_conversation",
		ResourceID:   &conversationID,
		Decision:     "deny",
		RequestID:    safeRequestID(input.RequestID),
		IPHash:       input.IPHash,
		Metadata: map[string]any{
			"reason":     reason,
			"model_mode": s.providerMode(),
		},
	}); err != nil {
		s.logger.Error("model_budget_audit_failed", "error_type", fmt.Sprintf("%T", err))
		return
	}
	if err := tx.Commit(recordCtx); err != nil {
		s.logger.Error("model_budget_audit_failed", "error_type", fmt.Sprintf("%T", err))
	}
}

func (s *Service) recordFailure(ctx context.Context, input SendMessageInput,
	mode string, inputChars, duration int, callErr error, invocationID uuid.UUID) {
	status := "failed"
	code := string(model.CodeUnavailable)
	var modelErr *model.Error
	if errors.As(callErr, &modelErr) {
		code = string(modelErr.Code)
		if modelErr.Code == model.CodeTimeout {
			status = "timeout"
		}
	}
	conversationID := input.ConversationID
	modelName := s.modelName
	if modelName == "" {
		modelName = "unknown"
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(recordCtx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		s.logger.Error("model_invocation_record_failed", "error_type", fmt.Sprintf("%T", err))
		return
	}
	defer func() { _ = tx.Rollback(recordCtx) }()
	invocation := model.Invocation{
		ID: invocationID, UserID: input.UserID, ConversationID: &conversationID,
		Provider: mode, Model: modelName, InputChars: inputChars,
		OutputChars: 0, DurationMS: duration, Status: status, ErrorCode: code,
	}
	completed, err := s.recorder.CompleteTx(recordCtx, tx, invocation)
	if err != nil {
		s.logger.Error("model_invocation_record_failed", "error_type", fmt.Sprintf("%T", err))
		return
	}
	if !completed {
		if err := s.recorder.RecordTx(recordCtx, tx, invocation); err != nil {
			s.logger.Error("model_invocation_record_failed", "error_type", fmt.Sprintf("%T", err))
			return
		}
	}
	if err := tx.Commit(recordCtx); err != nil {
		s.logger.Error("model_invocation_record_failed", "error_type", fmt.Sprintf("%T", err))
	}
}

func mapTrustError(err error) error {
	switch {
	case errors.Is(err, trust.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, trust.ErrForbidden), errors.Is(err, trust.ErrNoActiveDelegation),
		errors.Is(err, trust.ErrDelegationNotActive):
		return ErrForbidden
	case errors.Is(err, trust.ErrPayloadTooLarge):
		return ErrPayloadTooLarge
	case errors.Is(err, trust.ErrInvalidRequest):
		return fmt.Errorf("%w: trust", ErrInvalidRequest)
	default:
		return err
	}
}
