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
	RejectedActions int         `json:"rejected_actions,omitempty"`
	ContextStale    bool        `json:"context_stale,omitempty"`
	Usage           model.Usage `json:"usage"`
}

type aiTurn struct {
	inputChars   int
	prompt       string
	messages     []model.Message
	reply        string
	actions      []rawAction
	parseStatus  string
	rawOutput    string
	mode         string
	response     model.Response
	durationMS   int
	invocationID uuid.UUID
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
	system, messages, inputChars, err := buildPrompt(profile, private, projection, history, input.Content, input.Intent)
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

	userMessage, err := insertMessageTx(ctx, tx, input.ConversationID, "user", input.Content, turn.mode, nil)
	if err != nil {
		return nil, err
	}
	parseStatus := turn.parseStatus
	assistantMessage, err := insertMessageTx(ctx, tx, input.ConversationID, "assistant", turn.reply, turn.mode, &parseStatus)
	if err != nil {
		return nil, err
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
		for _, action := range turn.actions {
			if remaining <= 0 {
				break
			}
			var result *AcceptedAction
			if input.Object != nil {
				result, err = s.applyModelAction(ctx, tx, input, effective, action)
			} else {
				result, err = s.applyGlobalModelAction(ctx, tx, input, effective, action)
			}
			if err != nil {
				return nil, err
			}
			if result != nil {
				accepted = append(accepted, *result)
				remaining--
			}
		}
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
		ProposedActions: accepted, RejectedActions: rejected, ContextStale: contextStale,
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
) (*AcceptedAction, error) {
	switch action.CapabilityID {
	case "object.propose_change":
		var proposal proposalActionInput
		if err := decodeSingleJSON(string(action.Input), &proposal); err != nil {
			return nil, nil
		}
		if proposal.Action == "" || len(proposal.Input) == 0 {
			return nil, nil
		}
		if proposal.ObjectID != nil && *proposal.ObjectID != input.Object.ObjectID.String() {
			return nil, nil
		}
		if proposal.ExpectedVersion != nil && *proposal.ExpectedVersion != effective.ObjectVersion {
			return nil, nil
		}
		if !effective.Can("contribute") {
			return nil, nil
		}
		proposalID, accepted, err := s.recordProposalAction(ctx, tx, input, effective,
			proposal.Action, proposal.Input)
		if err != nil {
			return nil, err
		}
		if !accepted {
			return nil, nil
		}
		return &AcceptedAction{
			Kind: "proposal", ProposalID: &proposalID, Action: proposal.Action,
		}, nil
	case "object.derive":
		var derive deriveActionInput
		if err := decodeSingleJSON(string(action.Input), &derive); err != nil {
			return nil, nil
		}
		if derive.ObjectID != nil && *derive.ObjectID != input.Object.ObjectID.String() {
			return nil, nil
		}
		if derive.ExpectedVersion != nil && *derive.ExpectedVersion != effective.ObjectVersion {
			return nil, nil
		}
		if !effective.Can("derive") {
			return nil, nil
		}
		confirmationID, accepted, err := s.recordDeriveConfirmation(ctx, tx, input, effective, derive)
		if err != nil {
			return nil, err
		}
		if !accepted {
			return nil, nil
		}
		return &AcceptedAction{
			Kind: "confirmation", ConfirmationID: &confirmationID,
			Action: trust.ConfirmationDeriveCreate,
		}, nil
	case "world.reimagine":
		if input.Object == nil || input.Object.ObjectType != "open_world" || s.proposals == nil {
			return nil, nil
		}
		if !effective.Can("contribute") {
			return nil, nil
		}
		var reimagine worldReimagineInput
		if err := decodeSingleJSON(string(action.Input), &reimagine); err != nil {
			return nil, nil
		}
		if len(reimagine.Brief) == 0 {
			return nil, nil
		}
		seed, err := worldseed.Decode(reimagine.Brief)
		if err != nil {
			return nil, nil
		}
		result, err := worldseed.Reshape(input.Object.State, seed)
		if err != nil {
			return nil, nil
		}
		encoded, err := json.Marshal(result.State)
		if err != nil {
			return nil, nil
		}
		summary := strings.TrimSpace(reimagine.Summary)
		if summary == "" || utf8.RuneCountInString(summary) > 500 {
			summary = result.Summary
		}
		payload, err := json.Marshal(map[string]any{
			"note": summary, "content": json.RawMessage(encoded),
		})
		if err != nil {
			return nil, nil
		}
		proposalID, accepted, err := s.proposals.CreateProposalTx(ctx, tx,
			input.Object.ObjectID, input.UserID, effective.AIProfileID,
			effective.DelegationID, "world.reimagine", "reshape_world", payload,
			effective.ObjectVersion, input.RequestID, input.IPHash)
		if err != nil {
			return nil, err
		}
		if !accepted {
			return nil, nil
		}
		return &AcceptedAction{
			Kind: "proposal", ProposalID: &proposalID, Action: "reshape_world",
		}, nil
	default:
		return nil, nil
	}
}

// applyGlobalModelAction accepts only a world.create proposal from an unbound
// conversation. Object-bound actions remain impossible here, so one world's
// private context can never be smuggled into a global turn.
func (s *Service) applyGlobalModelAction(
	ctx context.Context,
	tx pgx.Tx,
	input SendMessageInput,
	effective *trust.Effective,
	action rawAction,
) (*AcceptedAction, error) {
	if action.CapabilityID != "world.create" {
		return nil, nil
	}
	if !effective.Can("contribute") {
		return nil, nil
	}
	var create worldCreateActionInput
	if err := decodeSingleJSON(string(action.Input), &create); err != nil {
		return nil, nil
	}
	if strings.TrimSpace(create.Title) == "" || (len(create.Content) == 0 && len(create.Brief) == 0) {
		return nil, nil
	}
	content := create.Content
	if len(create.Brief) > 0 {
		seed, err := worldseed.Decode(create.Brief)
		if err != nil {
			return nil, nil
		}
		composition, err := worldseed.Compose(seed, strings.TrimSpace(create.Title))
		if err != nil {
			return nil, nil
		}
		encoded, err := json.Marshal(composition.Content)
		if err != nil {
			return nil, nil
		}
		content = encoded
	}
	confirmation, accepted, err := s.trust.CreateWorldCreateConfirmationTx(ctx, tx,
		trust.WorldCreateConfirmationParams{
			UserID: input.UserID, AIProfileID: effective.AIProfileID,
			DelegationID: effective.DelegationID, Title: create.Title,
			Content: content, RequestID: input.RequestID, IPHash: input.IPHash,
		})
	if err != nil {
		if errors.Is(err, trust.ErrInvalidRequest) || errors.Is(err, trust.ErrPayloadTooLarge) {
			return nil, nil
		}
		return nil, err
	}
	if !accepted {
		return nil, nil
	}
	return &AcceptedAction{
		Kind: "confirmation", ConfirmationID: &confirmation.ID,
		Action: trust.ConfirmationWorldCreate,
	}, nil
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
SELECT id, conversation_id, role, content, model_mode, parse_status, created_at
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
			&item.Content, &item.ModelMode, &item.ParseStatus, &item.CreatedAt); err != nil {
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

func insertMessageTx(ctx context.Context, tx pgx.Tx, conversationID uuid.UUID, role, content, mode string, parseStatus *string) (Message, error) {
	messageID, err := uuid.NewV7()
	if err != nil {
		return Message{}, err
	}
	var (
		item         Message
		statusResult *string
	)
	err = tx.QueryRow(ctx, `
INSERT INTO ai_messages (id, conversation_id, role, content, model_mode, parse_status)
VALUES ($1,$2,$3,$4,$5,$6)
RETURNING id, conversation_id, role, content, model_mode, parse_status, created_at`,
		messageID.String(), conversationID.String(), role, content, mode, parseStatus).Scan(
		&item.ID, &item.ConversationID, &item.Role, &item.Content,
		&item.ModelMode, &statusResult, &item.CreatedAt)
	if err != nil {
		return Message{}, err
	}
	item.ParseStatus = statusResult
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
