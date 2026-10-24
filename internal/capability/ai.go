// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/trust"
)

// AICall is a server-derived Personal AI capability call. It is never exposed
// as an HTTP endpoint; the Personal AI runtime constructs it from a validated
// model action.
type AICall struct {
	UserID          uuid.UUID
	ProfileID       uuid.UUID
	ObjectID        uuid.UUID
	CapabilityID    string
	Action          string
	Input           json.RawMessage
	ExpectedVersion int
	RequestID       string
	IPHash          []byte
}

// ProposeAITx records the capability invocation and the proposal in the
// caller's transaction. A false accepted result means the model action was
// stale or unauthorized; it is not an infrastructure error.
func (s *Service) ProposeAITx(ctx context.Context, tx pgx.Tx, call AICall) (uuid.UUID, uuid.UUID, bool, error) {
	effective, err := s.trust.ResolveAITx(ctx, tx, call.UserID, call.ProfileID, call.ObjectID)
	if err != nil {
		if errors.Is(err, trust.ErrNotFound) || errors.Is(err, trust.ErrForbidden) ||
			errors.Is(err, trust.ErrNoActiveDelegation) {
			return uuid.Nil, uuid.Nil, false, nil
		}
		return uuid.Nil, uuid.Nil, false, err
	}
	if !effective.Can(objects.PermContribute) || effective.ObjectVersion != call.ExpectedVersion {
		return uuid.Nil, uuid.Nil, false, nil
	}
	canonicalInput, err := proposeCapabilityInput(call)
	if err != nil {
		return uuid.Nil, uuid.Nil, false, nil
	}
	definition, ok := s.registry.Definition(IDObjectProposeChange)
	if !ok || !definition.Enabled {
		return uuid.Nil, uuid.Nil, false, nil
	}
	if err := definition.Input.ValidateJSON(canonicalInput); err != nil {
		return uuid.Nil, uuid.Nil, false, nil
	}
	proposalID, accepted, err := s.objects.CreateProposalTx(ctx, tx, call.ObjectID,
		call.UserID, effective.AIProfileID, effective.DelegationID,
		call.Action, call.Input, call.ExpectedVersion, call.RequestID, call.IPHash)
	if err != nil || !accepted {
		return uuid.Nil, uuid.Nil, false, err
	}
	output, _ := json.Marshal(map[string]any{"proposal_id": proposalID, "status": "pending"})
	invocationID, err := s.insertAITerminalTx(ctx, tx, call, effective, definition,
		canonicalInput, output, 0)
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	return invocationID, proposalID, true, nil
}

// DeriveConfirmationAITx records a pending invocation and the human
// confirmation bound to it. Execution happens only on approval through
// ApproveAIDeriveTx.
func (s *Service) DeriveConfirmationAITx(ctx context.Context, tx pgx.Tx, call AICall) (uuid.UUID, uuid.UUID, bool, error) {
	effective, err := s.trust.ResolveAITx(ctx, tx, call.UserID, call.ProfileID, call.ObjectID)
	if err != nil {
		if errors.Is(err, trust.ErrNotFound) || errors.Is(err, trust.ErrForbidden) ||
			errors.Is(err, trust.ErrNoActiveDelegation) {
			return uuid.Nil, uuid.Nil, false, nil
		}
		return uuid.Nil, uuid.Nil, false, err
	}
	if !effective.Can(objects.PermDerive) {
		return uuid.Nil, uuid.Nil, false, nil
	}
	canonicalInput, err := deriveCapabilityInput(call)
	if err != nil {
		return uuid.Nil, uuid.Nil, false, nil
	}
	definition, ok := s.registry.Definition(IDObjectDerive)
	if !ok || !definition.Enabled {
		return uuid.Nil, uuid.Nil, false, nil
	}
	if err := definition.Input.ValidateJSON(canonicalInput); err != nil {
		return uuid.Nil, uuid.Nil, false, nil
	}
	var input map[string]any
	if err := json.Unmarshal(canonicalInput, &input); err != nil {
		return uuid.Nil, uuid.Nil, false, nil
	}
	object, err := s.objects.LoadForActor(ctx, call.UserID, call.ObjectID)
	if err != nil {
		if errors.Is(err, objects.ErrNotFound) {
			return uuid.Nil, uuid.Nil, false, nil
		}
		return uuid.Nil, uuid.Nil, false, err
	}
	if failure := semaValidate(definition, object, input); failure != nil {
		return uuid.Nil, uuid.Nil, false, nil
	}

	inputHash, err := canonicalInputHash(IDObjectDerive, canonicalInput)
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	redacted, err := redactedInput(IDObjectDerive, canonicalInput)
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	invocationID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO capability_invocations (
    id, capability_id, contract_version, provider_id, actor_user_id,
    actor_ai_profile_id, delegation_id, object_id, input_hash, input_redacted,
    status, output, error_code, error_message, duration_ms, completed_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,'pending',NULL,NULL,NULL,NULL,NULL)`,
		invocationID.String(), IDObjectDerive, ContractVersion, ProviderTrustedCore,
		call.UserID.String(), effective.AIProfileID.String(),
		effective.DelegationID.String(), call.ObjectID.String(),
		inputHash, string(redacted))
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	actor := effective.Actor()
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor: actor, Action: audit.ActionCapabilityInvoke,
		ResourceType: "capability_invocation", ResourceID: &invocationID,
		Decision: "allow", RequestID: safeRequestID(call.RequestID), IPHash: call.IPHash,
		Metadata: map[string]any{
			"capability_id": IDObjectDerive, "provider_id": ProviderTrustedCore,
			"contract_version": ContractVersion, "input_hash": hex.EncodeToString(inputHash),
		},
	}); err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	confirmation, err := s.trust.CreateCapabilityConfirmationTx(ctx, tx, trust.CapabilityConfirmationParams{
		UserID: call.UserID, AIProfileID: effective.AIProfileID,
		DelegationID: effective.DelegationID, ObjectID: call.ObjectID,
		ExpectedVersion: effective.ObjectVersion, InvocationID: invocationID,
		Action: trust.ConfirmationDeriveCreate, CapabilityID: IDObjectDerive,
		Input: canonicalInput, RequestID: call.RequestID, IPHash: call.IPHash,
	})
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	return invocationID, confirmation.ID, true, nil
}

func (s *Service) insertAITerminalTx(ctx context.Context, tx pgx.Tx, call AICall,
	effective *trust.Effective, definition *CompiledDefinition,
	canonicalInput json.RawMessage, output json.RawMessage, durationMS int) (uuid.UUID, error) {
	inputHash, err := canonicalInputHash(definition.CapabilityID, canonicalInput)
	if err != nil {
		return uuid.Nil, err
	}
	redacted, err := redactedInput(definition.CapabilityID, canonicalInput)
	if err != nil {
		return uuid.Nil, err
	}
	invocationID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO capability_invocations (
    id, capability_id, contract_version, provider_id, actor_user_id,
    actor_ai_profile_id, delegation_id, object_id, input_hash, input_redacted,
    status, output, error_code, error_message, duration_ms, completed_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,'succeeded',$11::jsonb,NULL,NULL,$12,now())`,
		invocationID.String(), definition.CapabilityID, ContractVersion,
		definition.ProviderID, call.UserID.String(),
		effective.AIProfileID.String(), effective.DelegationID.String(),
		call.ObjectID.String(), inputHash, string(redacted), string(output), durationMS)
	if err != nil {
		return uuid.Nil, err
	}
	actor := effective.Actor()
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor: actor, Action: audit.ActionCapabilityInvoke,
		ResourceType: "capability_invocation", ResourceID: &invocationID,
		Decision: "allow", RequestID: safeRequestID(call.RequestID), IPHash: call.IPHash,
		Metadata: map[string]any{
			"capability_id": definition.CapabilityID, "provider_id": definition.ProviderID,
			"contract_version": ContractVersion, "input_hash": hex.EncodeToString(inputHash),
		},
	}); err != nil {
		return uuid.Nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor: actor, Action: audit.ActionCapabilityCompleted,
		ResourceType: "capability_invocation", ResourceID: &invocationID,
		Decision: "allow", RequestID: safeRequestID(call.RequestID), IPHash: call.IPHash,
		Metadata: map[string]any{
			"invocation_id": invocationID.String(), "duration_ms": durationMS,
			"output_bytes": len(output),
		},
	}); err != nil {
		return uuid.Nil, err
	}
	return invocationID, nil
}

// ApproveAIDeriveTx executes an approved AI derive confirmation inside the
// Trust Kernel's approval transaction.
func (s *Service) ApproveAIDeriveTx(ctx context.Context, tx pgx.Tx, userID, confirmationID uuid.UUID,
	payload trust.CapabilityConfirmationPayload) (json.RawMessage, error) {
	var (
		status   string
		actorID  string
		objectID string
	)
	err := tx.QueryRow(ctx, `
SELECT status, actor_user_id, object_id
  FROM capability_invocations
 WHERE id = $1 FOR UPDATE`, payload.InvocationID.String()).Scan(
		&status, &actorID, &objectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, trust.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "pending" || actorID != userID.String() || objectID != payload.ObjectID.String() {
		return nil, trust.ErrConflict
	}
	effective, err := s.trust.ResolveAITx(ctx, tx, userID, payload.AIProfileID, payload.ObjectID)
	if err != nil {
		return nil, err
	}
	if !effective.Can(objects.PermDerive) {
		return nil, trust.ErrForbidden
	}
	active, err := s.trust.ActiveDelegationTx(ctx, tx, payload.DelegationID,
		userID, payload.AIProfileID, &payload.ObjectID)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, trust.ErrDelegationNotActive
	}
	if effective.ObjectVersion != payload.ExpectedVersion {
		return nil, trust.ErrVersionConflict
	}
	var input map[string]any
	if err := json.Unmarshal(payload.Input, &input); err != nil {
		return nil, trust.ErrInvalidRequest
	}
	title, _ := input["title"].(string)
	fields := stringSlice(input["inheritance_fields"])
	var sourceVersion *int
	if raw, ok := input["source_version"]; ok {
		value, ok := numberToInt(raw)
		if !ok {
			return nil, trust.ErrInvalidRequest
		}
		sourceVersion = &value
	}
	started := time.Now()
	result, err := s.deriver.CreateConfirmedTx(ctx, tx, userID, payload.ObjectID,
		payload.ExpectedVersion, DeriveRequest{
			Title: title, SourceVersion: sourceVersion, InheritanceFields: fields,
		}, payload.AIProfileID, payload.DelegationID, confirmationID, "capability-confirmation", nil)
	if err != nil {
		return nil, err
	}
	output, _ := json.Marshal(map[string]any{"child_object_id": result.ChildID})
	duration := int(time.Since(started).Milliseconds())
	if duration < 0 {
		duration = 0
	}
	tag, err := tx.Exec(ctx, `
UPDATE capability_invocations
   SET status = 'succeeded', output = $2::jsonb, duration_ms = $3, completed_at = now()
 WHERE id = $1 AND status = 'pending'`, payload.InvocationID.String(), string(output), duration)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, trust.ErrConflict
	}
	actor := audit.Actor{
		Type: "personal_ai", UserID: &userID,
		AIProfileID: &payload.AIProfileID, DelegationID: &payload.DelegationID,
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor: actor, Action: audit.ActionCapabilityCompleted,
		ResourceType: "capability_invocation", ResourceID: &payload.InvocationID,
		Decision: "allow", RequestID: "capability-confirmation", IPHash: nil,
		Metadata: map[string]any{
			"invocation_id": payload.InvocationID.String(),
			"duration_ms":   duration, "output_bytes": len(output),
		},
	}); err != nil {
		return nil, err
	}
	return result.ViewJSON, nil
}

// DenyAIInvocation marks a pending invocation denied after its confirmation
// was denied or expired. It is idempotent for already-terminal rows.
func (s *Service) DenyAIInvocation(ctx context.Context, invocationID, userID uuid.UUID, reason, errorCode string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
UPDATE capability_invocations
   SET status = 'denied', error_code = $2, error_message = $3,
       duration_ms = 0, completed_at = now()
 WHERE id = $1 AND actor_user_id = $4 AND status = 'pending'`,
		invocationID.String(), errorCode, truncateMessage(reason, 500), userID.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor: audit.UserActor(userID), Action: audit.ActionCapabilityDenied,
		ResourceType: "capability_invocation", ResourceID: &invocationID,
		Decision: "deny", ReasonCode: errorCode,
		RequestID: "capability-confirmation", IPHash: nil,
		Metadata: map[string]any{
			"capability_id": IDObjectDerive,
			"reason_code":   errorCode,
		},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AIDeriveReplayAllowed rechecks the exact delegation and current object read
// before an idempotent approval replay is returned (D05-20, A.7.1).
func (s *Service) AIDeriveReplayAllowed(ctx context.Context, userID uuid.UUID,
	payload trust.CapabilityConfirmationPayload) error {
	if payload.Action != trust.ConfirmationDeriveCreate {
		return ErrNotFound
	}
	effective, err := s.trust.ResolveAI(ctx, userID, payload.AIProfileID, &payload.ObjectID, nil)
	if err != nil {
		if errors.Is(err, trust.ErrNotFound) || errors.Is(err, trust.ErrForbidden) ||
			errors.Is(err, trust.ErrNoActiveDelegation) {
			return ErrNotFound
		}
		return err
	}
	if !effective.Can(objects.PermDerive) {
		return ErrNotFound
	}
	active := false
	for _, delegationID := range effective.DelegationIDs {
		if delegationID == payload.DelegationID {
			active = true
			break
		}
	}
	if !active {
		return ErrNotFound
	}
	if _, err := s.objects.LoadForActor(ctx, userID, payload.ObjectID); err != nil {
		return ErrNotFound
	}
	return nil
}

// ObjectIDForInvocation returns the invocation's object binding to the actor.
func (s *Service) ObjectIDForInvocation(ctx context.Context, userID, invocationID uuid.UUID) (uuid.UUID, bool, error) {
	var objectID *string
	err := s.db.QueryRow(ctx, `
SELECT object_id FROM capability_invocations
 WHERE id = $1 AND actor_user_id = $2`, invocationID.String(), userID.String()).Scan(&objectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	if objectID == nil {
		return uuid.Nil, false, nil
	}
	parsed, err := uuid.Parse(*objectID)
	if err != nil {
		return uuid.Nil, false, ErrNotFound
	}
	return parsed, true, nil
}

func proposeCapabilityInput(call AICall) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"object_id":        call.ObjectID.String(),
		"action":           call.Action,
		"input":            call.Input,
		"expected_version": call.ExpectedVersion,
	})
}

func deriveCapabilityInput(call AICall) (json.RawMessage, error) {
	var decoded map[string]any
	if err := json.Unmarshal(call.Input, &decoded); err != nil {
		return nil, err
	}
	out := map[string]any{
		"object_id":          call.ObjectID.String(),
		"title":              decoded["title"],
		"inheritance_fields": decoded["inheritance_fields"],
	}
	if value, ok := decoded["source_version"]; ok {
		out["source_version"] = value
	}
	return json.Marshal(out)
}
