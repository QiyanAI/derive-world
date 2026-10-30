// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/store"
)

const (
	proposalCapability = "object.propose_change"
	proposalTTL        = 24 * time.Hour
	// G04 contract closeout C-G04-03: proposal input is bounded by the
	// 64 KiB object-state ceiling, not the former 16 KiB limit.
	maxProposalInputBytes = 64 * 1024
)

// Proposal is the owner-only projection of an AI state-change proposal.
type Proposal struct {
	ID           uuid.UUID       `json:"id"`
	ObjectID     uuid.UUID       `json:"object_id"`
	ActorUserID  uuid.UUID       `json:"actor_user_id"`
	AIProfileID  uuid.UUID       `json:"ai_profile_id"`
	DelegationID uuid.UUID       `json:"delegation_id"`
	CapabilityID string          `json:"capability_id"`
	Action       string          `json:"action"`
	Input        json.RawMessage `json:"input"`
	BaseVersion  int             `json:"base_version"`
	Status       string          `json:"status"`
	CreatedAt    time.Time       `json:"created_at"`
	// ExpiresAt is the server-defined 24-hour proposal lifetime, exposed so the
	// browser never has to duplicate the constant or trust its own clock.
	ExpiresAt      time.Time  `json:"expires_at"`
	DecidedAt      *time.Time `json:"decided_at"`
	AppliedEventID *uuid.UUID `json:"applied_event_id"`
	// ConversationID and SourceMessageID place the proposal directly under the
	// assistant reply that produced it. They are display-only and never
	// participate in authorization; proposals that did not originate in a
	// Personal AI turn leave them null.
	ConversationID  *uuid.UUID `json:"conversation_id,omitempty"`
	SourceMessageID *uuid.UUID `json:"source_message_id,omitempty"`
}

// CreateProposalTx writes a pending object_proposals row and the
// ai.proposal_created event inside the caller's transaction. The caller has
// already recomputed the AI delegation intersection in that transaction.
// A false accepted result means the model output is stale or not permitted;
// it is not an infrastructure failure.
func (s *Service) CreateProposalTx(
	ctx context.Context,
	tx pgx.Tx,
	objectID, actorUserID, aiProfileID, delegationID uuid.UUID,
	capabilityID, action string,
	input json.RawMessage,
	baseVersion int,
	requestID string,
	ipHash []byte,
) (uuid.UUID, bool, error) {
	if capabilityID == "" {
		capabilityID = proposalCapability
	}
	if baseVersion < 1 {
		return uuid.Nil, false, nil
	}
	var (
		objectType string
		version    int
		status     string
		state      []byte
	)
	err := tx.QueryRow(ctx, `
SELECT object_type, version, status, state
  FROM living_objects
 WHERE id = $1
   FOR SHARE`, objectID.String()).Scan(&objectType, &version, &status, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	if status != StatusActive {
		return uuid.Nil, false, ErrNotFound
	}
	if version != baseVersion {
		return uuid.Nil, false, nil
	}
	permissions, err := membershipPermissions(ctx, tx, actorUserID, objectID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if !HasPermission(permissions, PermContribute) {
		return uuid.Nil, false, nil
	}
	plan, err := PlanAction(objectType, action, input)
	if err != nil {
		return uuid.Nil, false, nil
	}
	if plan.Permission != PermContribute {
		return uuid.Nil, false, nil
	}
	if !ReferencesExist(objectType, state, plan) {
		return uuid.Nil, false, nil
	}
	canonical, err := store.CanonicalJSON(mustMarshal(plan.Input))
	if err != nil {
		return uuid.Nil, false, nil
	}
	inputSize, err := store.CompactJSONSize(plan.Input)
	if err != nil {
		return uuid.Nil, false, nil
	}
	if inputSize > maxProposalInputBytes {
		return uuid.Nil, false, nil
	}
	hash := sha256.Sum256(canonical)
	proposalID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, false, err
	}
	var createdAt time.Time
	err = tx.QueryRow(ctx, `
INSERT INTO object_proposals (
    id, object_id, actor_user_id, ai_profile_id, delegation_id,
    capability_id, action, input, input_hash, base_version, status
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,'pending')
RETURNING created_at`,
		proposalID.String(), objectID.String(), actorUserID.String(),
		aiProfileID.String(), delegationID.String(), capabilityID, action,
		string(canonical), hash[:], baseVersion,
	).Scan(&createdAt)
	if err != nil {
		return uuid.Nil, false, mapConstraintError(err)
	}
	actor := audit.Actor{
		Type: "personal_ai", UserID: &actorUserID,
		AIProfileID: &aiProfileID, DelegationID: &delegationID,
	}
	if _, err := appendEventReturningID(ctx, tx, actor, objectID, version,
		"ai.proposal_created", map[string]any{
			"proposal_id": proposalID.String(),
			"action":      action,
		}); err != nil {
		return uuid.Nil, false, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        actor,
		Action:       audit.ActionObjectProposalCreate,
		ResourceType: "living_object",
		ResourceID:   &objectID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"proposal_id": proposalID.String()},
	}); err != nil {
		return uuid.Nil, false, err
	}
	_ = createdAt
	return proposalID, true, nil
}

// LinkProposalsToMessageTx records the conversation turn behind each accepted
// proposal. It is called inside the AI turn's transaction and is the only
// writer of the display-only provenance columns. A zero-row update fails the
// turn: an unlinked proposal would be invisible under its reply after reload.
func (s *Service) LinkProposalsToMessageTx(ctx context.Context, tx pgx.Tx,
	actorID, conversationID, messageID uuid.UUID, ids []uuid.UUID) error {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		tag, err := tx.Exec(ctx, `
UPDATE object_proposals
   SET conversation_id = $1, source_message_id = $2
 WHERE id = $3 AND actor_user_id = $4
   AND (source_message_id IS NULL OR source_message_id = $2)`,
			conversationID.String(), messageID.String(), id.String(), actorID.String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
	}
	return nil
}

// ProposalForValidation returns one proposal row for the Capability Broker's
// post-execution semantic check. It is not an authorization entry point; the
// caller has already authorized the invocation and verifies the row against
// the server-derived actor.
func (s *Service) ProposalForValidation(ctx context.Context, proposalID uuid.UUID) (*Proposal, error) {
	row := s.db.QueryRow(ctx, `
SELECT id, object_id, actor_user_id, ai_profile_id, delegation_id,
       capability_id, action, input, base_version, status, created_at,
       decided_at, applied_event_id, conversation_id, source_message_id
  FROM object_proposals
 WHERE id = $1`, proposalID.String())
	proposal, err := scanProposal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &proposal, nil
}

// ListProposals returns only the caller's proposals for the object. Pending
// proposals older than 24 hours are presented as expired.
func (s *Service) ListProposals(ctx context.Context, actorID, objectID uuid.UUID) ([]Proposal, error) {
	// A version change or the 24-hour lifetime makes a pending proposal
	// unusable. Persist that clear state here so the browser does not have to
	// discover the conflict by clicking an already-stale card.
	if _, err := s.db.Exec(ctx, `
UPDATE object_proposals p
   SET status = 'expired', decided_at = COALESCE(p.decided_at, now())
  FROM living_objects o
 WHERE p.object_id = o.id
   AND p.object_id = $1
   AND p.actor_user_id = $2
   AND p.status = 'pending'
   AND (p.base_version < o.version OR p.created_at + interval '24 hours' <= now())`,
		objectID.String(), actorID.String()); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
SELECT id, object_id, actor_user_id, ai_profile_id, delegation_id,
       capability_id, action, input, base_version, status, created_at,
       decided_at, applied_event_id, conversation_id, source_message_id
  FROM object_proposals
 WHERE object_id = $1 AND actor_user_id = $2
 ORDER BY created_at DESC, id DESC
 LIMIT 100`, objectID.String(), actorID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Proposal, 0)
	for rows.Next() {
		item, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		if item.Status == "pending" && !item.CreatedAt.Add(proposalTTL).After(time.Now().UTC()) {
			item.Status = "expired"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ApplyProposal executes a proposal exactly once. Re-applying an already
// applied proposal returns the current object without writing again.
func (s *Service) ApplyProposal(
	ctx context.Context,
	actorID, objectID, proposalID uuid.UUID,
	expectedVersion int,
	requestID string,
	ipHash []byte,
) (*ObjectView, *Proposal, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		scannedID     string
		objectType    string
		schemaVersion int
		creatorID     string
		ownerID       string
		stateRaw      []byte
		current       int
		sharedRaw     []byte
		accessPolicy  []byte
		template      string
		status        string
		createdAt     time.Time
		updatedAt     time.Time
		deletedAt     *time.Time
	)
	err = tx.QueryRow(ctx, `
SELECT `+objectColumns+`
  FROM living_objects o
 WHERE o.id = $1
   FOR UPDATE`, objectID.String()).Scan(
		&scannedID, &objectType, &schemaVersion, &creatorID, &ownerID,
		&stateRaw, &current, &sharedRaw, &accessPolicy,
		&template, &status, &createdAt, &updatedAt, &deletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if status != StatusActive {
		return nil, nil, ErrNotFound
	}
	permissions, err := membershipPermissions(ctx, tx, actorID, objectID)
	if err != nil {
		return nil, nil, err
	}
	if !HasPermission(permissions, PermContribute) {
		return nil, nil, ErrForbidden
	}
	proposal, err := loadProposalForUpdate(ctx, tx, objectID, actorID, proposalID)
	if err != nil {
		return nil, nil, err
	}
	if proposal.Status == "applied" {
		object, err := loadForActor(ctx, tx, actorID, objectID)
		if err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		view := object.View()
		return &view, proposal, nil
	}
	if proposal.Status == "denied" {
		return nil, nil, ErrConflict
	}
	if proposal.Status == "expired" ||
		(proposal.Status == "pending" && !proposal.CreatedAt.Add(proposalTTL).After(time.Now().UTC())) {
		if proposal.Status == "pending" {
			if _, err := tx.Exec(ctx, `
UPDATE object_proposals SET status = 'expired'
 WHERE id = $1 AND status = 'pending'`, proposalID.String()); err != nil {
				return nil, nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, nil, err
			}
		}
		return nil, nil, ErrConflict
	}
	// Execution-time revalidation of the AI authorization behind the proposal.
	// The frozen endpoint table authorizes apply with the human's contribute;
	// this stricter check implements the independent-review requirement that a
	// revoked AI authorization must not be reused through a pending proposal
	// (D05-16 revocation intent, D05-13 execution-time revalidation pattern).
	// The recorded delegation must still be active, even if another delegation
	// exists, and the current effective AI permission must still include
	// contribute. The Trust Kernel takes a share lock on the recorded grant, so
	// a revocation that commits while this transaction waits is observed before
	// the state write.
	if s.delegations == nil {
		return nil, nil, errors.New("object proposal delegation revalidation is not configured")
	}
	allowed, err := s.delegations.ProposalApplyAllowedTx(ctx, tx, proposal.DelegationID,
		proposal.ActorUserID, proposal.AIProfileID, objectID, PermContribute)
	if err != nil {
		return nil, nil, err
	}
	if !allowed {
		return nil, nil, ErrDelegationNotActive
	}
	if proposal.BaseVersion != expectedVersion || current != expectedVersion {
		return nil, nil, ErrVersionConflict
	}
	plan, err := PlanAction(objectType, proposal.Action, proposal.Input)
	if err != nil {
		return nil, nil, ErrInvalidRequest
	}
	if plan.Permission != PermContribute {
		return nil, nil, ErrForbidden
	}

	mutation, err := applyStateMutationTx(ctx, tx, actorID, objectID, objectType,
		current, stateRaw, sharedRaw, plan)
	if err != nil {
		return nil, nil, err
	}
	newState := mutation.NewState
	newVersion := mutation.NewVersion
	eventInput := plan.Input
	if plan.EventInput != nil {
		eventInput = plan.EventInput
	}
	eventPayload := map[string]any{
		"action":      plan.Action,
		"input":       eventInput,
		"state_after": json.RawMessage(newState),
	}
	if plan.Action == "set_shared_context" {
		eventPayload["shared_context_changed"] = true
	}
	if len(plan.Effects) > 0 {
		eventPayload["effects"] = plan.Effects
	}
	stateEventID, err := appendEventReturningID(ctx, tx, audit.UserActor(actorID), objectID,
		newVersion, "object.state_changed", eventPayload)
	if err != nil {
		return nil, nil, err
	}
	if _, err := appendEventReturningID(ctx, tx, audit.UserActor(actorID), objectID,
		newVersion, "ai.proposal_applied", map[string]any{
			"proposal_id":        proposalID.String(),
			"action":             plan.Action,
			"applied_by_user_id": actorID.String(),
		}); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE object_proposals
   SET status = 'applied', decided_at = GREATEST(now(), created_at), applied_event_id = $2
 WHERE id = $1 AND status = 'pending'`,
		proposalID.String(), stateEventID.String()); err != nil {
		return nil, nil, err
	}
	// Any other pending plan prepared for an older version can no longer run.
	// Expire it in the same transaction instead of leaving a card the user has
	// to discover, click, and then close manually.
	if _, err := tx.Exec(ctx, `
UPDATE object_proposals
   SET status = 'expired', decided_at = GREATEST(now(), created_at)
 WHERE object_id = $1 AND status = 'pending' AND id <> $2 AND base_version < $3`,
		objectID.String(), proposalID.String(), newVersion); err != nil {
		return nil, nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(actorID),
		Action:       audit.ActionObjectProposalApply,
		ResourceType: "living_object",
		ResourceID:   &objectID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"proposal_id": proposalID.String()},
	}); err != nil {
		return nil, nil, err
	}
	object, err := loadForActor(ctx, tx, actorID, objectID)
	if err != nil {
		return nil, nil, err
	}
	proposal.Status = "applied"
	proposal.DecidedAt = ptrTime(time.Now().UTC())
	proposal.AppliedEventID = &stateEventID
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	view := object.View()
	return &view, proposal, nil
}

// DenyProposal marks the caller's pending proposal denied. Repeated deny is
// idempotent.
func (s *Service) DenyProposal(ctx context.Context, actorID, objectID, proposalID uuid.UUID, requestID string, ipHash []byte) (*Proposal, error) {
	tx, err := s.db.BeginTx(ctx, txOptions)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	proposal, err := loadProposalForUpdate(ctx, tx, objectID, actorID, proposalID)
	if err != nil {
		return nil, err
	}
	if proposal.Status == "denied" {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return proposal, nil
	}
	if proposal.Status == "applied" {
		return nil, ErrConflict
	}
	if proposal.Status == "expired" ||
		(proposal.Status == "pending" && !proposal.CreatedAt.Add(proposalTTL).After(time.Now().UTC())) {
		if proposal.Status == "pending" {
			if _, err := tx.Exec(ctx, `
UPDATE object_proposals SET status = 'expired'
 WHERE id = $1 AND status = 'pending'`, proposalID.String()); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
		}
		return nil, ErrConflict
	}
	if _, err := tx.Exec(ctx, `
UPDATE object_proposals SET status = 'denied', decided_at = GREATEST(now(), created_at)
 WHERE id = $1 AND status = 'pending'`, proposalID.String()); err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(actorID),
		Action:       audit.ActionObjectProposalDeny,
		ResourceType: "living_object",
		ResourceID:   &objectID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"proposal_id": proposalID.String()},
	}); err != nil {
		return nil, err
	}
	proposal.Status = "denied"
	proposal.DecidedAt = ptrTime(time.Now().UTC())
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return proposal, nil
}

var txOptions = pgx.TxOptions{IsoLevel: pgx.ReadCommitted}

func loadProposalForUpdate(ctx context.Context, tx pgx.Tx, objectID, actorID, proposalID uuid.UUID) (*Proposal, error) {
	row := tx.QueryRow(ctx, `
SELECT id, object_id, actor_user_id, ai_profile_id, delegation_id,
       capability_id, action, input, base_version, status, created_at,
       decided_at, applied_event_id, conversation_id, source_message_id
  FROM object_proposals
 WHERE id = $1 AND object_id = $2 AND actor_user_id = $3
   FOR UPDATE`, proposalID.String(), objectID.String(), actorID.String())
	proposal, err := scanProposal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &proposal, nil
}

type scanRow interface {
	Scan(dest ...any) error
}

func scanProposal(row scanRow) (Proposal, error) {
	var (
		item            Proposal
		id              string
		objectID        string
		actorID         string
		aiProfileID     string
		delegationID    string
		input           []byte
		appliedEventID  *string
		conversationID  *string
		sourceMessageID *string
	)
	err := row.Scan(&id, &objectID, &actorID, &aiProfileID, &delegationID,
		&item.CapabilityID, &item.Action, &input, &item.BaseVersion,
		&item.Status, &item.CreatedAt, &item.DecidedAt, &appliedEventID,
		&conversationID, &sourceMessageID)
	if err != nil {
		return Proposal{}, err
	}
	item.ID = uuid.MustParse(id)
	item.ObjectID = uuid.MustParse(objectID)
	item.ActorUserID = uuid.MustParse(actorID)
	item.AIProfileID = uuid.MustParse(aiProfileID)
	item.DelegationID = uuid.MustParse(delegationID)
	item.Input = input
	item.ExpiresAt = item.CreatedAt.Add(proposalTTL)
	if appliedEventID != nil {
		parsed := uuid.MustParse(*appliedEventID)
		item.AppliedEventID = &parsed
	}
	if conversationID != nil {
		if parsed, err := uuid.Parse(*conversationID); err == nil {
			item.ConversationID = &parsed
		}
	}
	if sourceMessageID != nil {
		if parsed, err := uuid.Parse(*sourceMessageID); err == nil {
			item.SourceMessageID = &parsed
		}
	}
	return item, nil
}

// ReferencesExist checks the identifiers inside a validated plan against the
// current state. It is shared by proposal storage and the capability broker so
// one authority decides whether an action can even refer to something real.
func ReferencesExist(objectType string, state json.RawMessage, plan *ActionPlan) bool {
	switch plan.Action {
	case "toggle_checklist_item", "remove_checklist_item":
		var collaborative CollaborativeState
		if json.Unmarshal(state, &collaborative) != nil {
			return false
		}
		itemID, _ := plan.Input["item_id"].(string)
		for _, item := range collaborative.Checklist {
			if item.ID == itemID {
				return true
			}
		}
		return false
	case "cast_vote":
		var interactive InteractiveState
		if json.Unmarshal(state, &interactive) != nil {
			return false
		}
		optionID, _ := plan.Input["option_id"].(string)
		for _, option := range interactive.Options {
			if option.ID == optionID {
				return true
			}
		}
		return false
	}
	if plan.Action == "batch_change" {
		if len(plan.Batch) == 0 {
			return false
		}
		current := state
		for _, sub := range plan.Batch {
			if !ReferencesExist(objectType, current, sub) {
				return false
			}
			next, _, _, err := applyOpenWorldAction(current, json.RawMessage(`{}`), sub)
			if err != nil {
				return false
			}
			current = next
		}
		return true
	}
	if objectType != TypeOpenWorld || !isOpenWorldAction(plan.Action) {
		return true
	}
	var world OpenWorldState
	if json.Unmarshal(state, &world) != nil {
		return false
	}
	hasNode := func(id string) *OpenWorldNode { return findOpenWorldNode(&world, id) }
	hasKind := func(id string) bool { return findOpenWorldKind(&world, id) != nil }
	switch plan.Action {
	case "add_node":
		kindID, _ := plan.Input["kind_id"].(string)
		return hasKind(kindID)
	case "update_kind", "remove_kind":
		kindID, _ := plan.Input["kind_id"].(string)
		return hasKind(kindID)
	case "update_node", "remove_node", "move_node", "set_node_status", "touch_node":
		nodeID, _ := plan.Input["node_id"].(string)
		return hasNode(nodeID) != nil
	case "set_node_field":
		nodeID, _ := plan.Input["node_id"].(string)
		node := hasNode(nodeID)
		if node == nil {
			return false
		}
		fieldKey, _ := plan.Input["field"].(string)
		field := openWorldFieldFor(&world, node, fieldKey)
		if field == nil {
			return false
		}
		value, ok := plan.Input["value"]
		return ok && validateOpenWorldValue(*field, value, "input.value") == nil
	case "add_link":
		from, _ := plan.Input["from"].(string)
		to, _ := plan.Input["to"].(string)
		return hasNode(from) != nil && hasNode(to) != nil
	case "remove_link":
		linkID, _ := plan.Input["link_id"].(string)
		for _, link := range world.Links {
			if link.ID == linkID {
				return true
			}
		}
		return false
	case "add_rule":
		triggerID, _ := plan.Input["trigger_id"].(string)
		if hasNode(triggerID) == nil {
			return false
		}
		if rawCondition, ok := plan.Input["condition"]; ok && rawCondition != nil {
			conditionMap, _ := rawCondition.(map[string]any)
			raw, err := json.Marshal(conditionMap)
			if err != nil {
				return false
			}
			var condition OpenWorldCondition
			if err := json.Unmarshal(raw, &condition); err != nil {
				return false
			}
			if validateOpenWorldCondition(&world, &condition, "input.condition") != nil {
				return false
			}
		}
		effects, _ := plan.Input["effects"].([]any)
		for _, raw := range effects {
			effect, _ := raw.(map[string]any)
			nodeID, _ := effect["node_id"].(string)
			node := hasNode(nodeID)
			if node == nil {
				return false
			}
			fieldKey, _ := effect["field"].(string)
			op, _ := effect["op"].(string)
			value := effect["value"]
			if fieldKey == OpenWorldStatusField {
				if op == OpenWorldOpSet {
					status, _ := value.(string)
					if status != OpenWorldStatusOpen && status != OpenWorldStatusDone {
						return false
					}
				} else if op != OpenWorldOpToggle {
					return false
				}
				continue
			}
			field := openWorldFieldFor(&world, node, fieldKey)
			if field == nil {
				return false
			}
			switch field.Type {
			case OpenWorldFieldText:
				if op != OpenWorldOpSet {
					return false
				}
			case OpenWorldFieldNumber:
				if op != OpenWorldOpSet && op != OpenWorldOpAdd {
					return false
				}
			case OpenWorldFieldFlag:
				if op != OpenWorldOpSet && op != OpenWorldOpToggle {
					return false
				}
			}
			if op != OpenWorldOpToggle && validateOpenWorldValue(*field, value, "input.value") != nil {
				return false
			}
		}
		return true
	case "remove_rule":
		ruleID, _ := plan.Input["rule_id"].(string)
		for _, rule := range world.Rules {
			if rule.ID == ruleID {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func mustMarshal(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func ptrTime(value time.Time) *time.Time { return &value }
