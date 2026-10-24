// SPDX-License-Identifier: Apache-2.0
package trust

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/store"
)

// CapabilityConfirmationPayload is the canonical non-secret payload of a
// capability invocation confirmation. The capability input is included so the
// approval is bound to the exact parameters (D06-09).
type CapabilityConfirmationPayload struct {
	Action          string          `json:"action"`
	CapabilityID    string          `json:"capability_id"`
	ContractVersion string          `json:"contract_version"`
	InvocationID    uuid.UUID       `json:"invocation_id"`
	ObjectID        uuid.UUID       `json:"object_id"`
	ExpectedVersion int             `json:"expected_version"`
	AIProfileID     uuid.UUID       `json:"ai_profile_id"`
	DelegationID    uuid.UUID       `json:"delegation_id"`
	Input           json.RawMessage `json:"input"`
}

// CapabilityConfirmationParams is trusted runtime input for creating a
// capability confirmation.
type CapabilityConfirmationParams struct {
	UserID          uuid.UUID
	AIProfileID     uuid.UUID
	DelegationID    uuid.UUID
	ObjectID        uuid.UUID
	InvocationID    uuid.UUID
	ExpectedVersion int
	Action          string
	CapabilityID    string
	Input           json.RawMessage
	RequestID       string
	IPHash          []byte
}

// CreateCapabilityConfirmationTx writes a pending confirmation bound to a
// capability invocation. The caller has already recomputed the AI intersection
// and inserted the pending invocation in this transaction.
func (s *Service) CreateCapabilityConfirmationTx(ctx context.Context, tx pgx.Tx,
	params CapabilityConfirmationParams) (*Confirmation, error) {
	if params.Action != ConfirmationDeriveCreate && params.Action != ConfirmationCapabilityInvoke {
		return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
	}
	if params.CapabilityID == "" || params.InvocationID == uuid.Nil ||
		params.ObjectID == uuid.Nil || params.AIProfileID == uuid.Nil ||
		params.DelegationID == uuid.Nil || params.ExpectedVersion < 1 {
		return nil, fmt.Errorf("%w: capability confirmation", ErrInvalidRequest)
	}
	payload := CapabilityConfirmationPayload{
		Action: params.Action, CapabilityID: params.CapabilityID,
		ContractVersion: "1.0", InvocationID: params.InvocationID,
		ObjectID: params.ObjectID, ExpectedVersion: params.ExpectedVersion,
		AIProfileID: params.AIProfileID, DelegationID: params.DelegationID,
		Input: params.Input,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	canonical, err := store.CanonicalJSON(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	if len(canonical) > maxConfirmationBytes {
		return nil, ErrPayloadTooLarge
	}
	hash := sha256.Sum256(canonical)
	confirmationID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	var createdAt, expiresAt time.Time
	err = tx.QueryRow(ctx, `
INSERT INTO confirmations (
    id, user_id, action, resource_type, resource_id, payload, payload_hash,
    status, expires_at
) VALUES ($1,$2,$3,'capability_invocation',$4,$5::jsonb,$6,'pending', now() + interval '5 minutes')
RETURNING created_at, expires_at`,
		confirmationID.String(), params.UserID.String(), params.Action,
		params.InvocationID.String(), string(canonical), hash[:],
	).Scan(&createdAt, &expiresAt)
	if err != nil {
		return nil, err
	}
	actor := audit.Actor{
		Type: "personal_ai", UserID: &params.UserID,
		AIProfileID: &params.AIProfileID, DelegationID: &params.DelegationID,
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        actor,
		Action:       audit.ActionAIConfirmationCreate,
		ResourceType: "confirmation",
		ResourceID:   &confirmationID,
		Decision:     "allow",
		RequestID:    safeRequestID(params.RequestID),
		IPHash:       params.IPHash,
		Metadata:     map[string]any{"confirmation_id": confirmationID.String()},
	}); err != nil {
		return nil, err
	}
	return &Confirmation{
		ID: confirmationID, UserID: params.UserID, Action: params.Action,
		ResourceType: "capability_invocation", ResourceID: params.InvocationID,
		Payload: canonical, PayloadHash: hash[:], Status: "pending",
		ExpiresAt: expiresAt, CreatedAt: createdAt,
	}, nil
}

// DecodeCapabilityConfirmationPayload parses and validates the canonical
// capability confirmation payload.
func DecodeCapabilityConfirmationPayload(raw []byte) (CapabilityConfirmationPayload, error) {
	var payload CapabilityConfirmationPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return CapabilityConfirmationPayload{}, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return CapabilityConfirmationPayload{}, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	if (payload.Action != ConfirmationDeriveCreate && payload.Action != ConfirmationCapabilityInvoke) ||
		payload.CapabilityID == "" || payload.ContractVersion != "1.0" ||
		payload.InvocationID == uuid.Nil || payload.ObjectID == uuid.Nil ||
		payload.ExpectedVersion < 1 || payload.AIProfileID == uuid.Nil ||
		payload.DelegationID == uuid.Nil || len(payload.Input) == 0 {
		return CapabilityConfirmationPayload{}, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	return payload, nil
}

// DelegationForUser resolves an active delegation that permits a human or PAT
// capability call to create a proposal. The object-scoped delegation must
// cover the target object; a global delegation also qualifies. This is not an
// AI authorization decision: it supplies the provenance columns the frozen
// object_proposals table requires.
func (s *Service) DelegationForUser(ctx context.Context, userID, objectID uuid.UUID, permission string) (uuid.UUID, uuid.UUID, error) {
	var (
		profileIDText    string
		delegationIDText string
	)
	err := s.db.QueryRow(ctx, `
SELECT ai_profile_id, id
  FROM delegation_grants
 WHERE user_id = $1
   AND revoked_at IS NULL
   AND (expires_at IS NULL OR expires_at > now())
   AND $2 = ANY(permissions)
   AND (object_id IS NULL OR object_id = $3)
 ORDER BY (object_id IS NULL) DESC, created_at DESC, id DESC
 LIMIT 1`, userID.String(), permission, objectID.String()).Scan(
		&profileIDText, &delegationIDText)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, ErrNoActiveDelegation
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	profileID, err := uuid.Parse(profileIDText)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	delegationID, err := uuid.Parse(delegationIDText)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return profileID, delegationID, nil
}

// ConfirmationForReplay returns a confirmation's payload for an idempotent
// approval replay. It rechecks ownership but not object state; the caller
// performs the capability-specific reauthorization.
func (s *Service) ConfirmationForReplay(ctx context.Context, userID, confirmationID uuid.UUID) (*Confirmation, error) {
	var (
		confirmation Confirmation
		idText       string
		resourceText string
		payload      []byte
		hash         []byte
	)
	err := s.db.QueryRow(ctx, `
SELECT id, user_id, action, resource_type, resource_id, payload, payload_hash,
       status, expires_at, created_at, decided_at
  FROM confirmations
 WHERE id = $1 AND user_id = $2`, confirmationID.String(), userID.String()).Scan(
		&idText, &confirmation.UserID, &confirmation.Action, &confirmation.ResourceType,
		&resourceText, &payload, &hash, &confirmation.Status, &confirmation.ExpiresAt,
		&confirmation.CreatedAt, &confirmation.DecidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	confirmation.ID = uuid.MustParse(idText)
	confirmation.ResourceID = uuid.MustParse(resourceText)
	confirmation.Payload = payload
	confirmation.PayloadHash = hash
	return &confirmation, nil
}

// ExpireCapabilityConfirmation lazily expires a pending capability
// confirmation whose window has passed. The caller then transitions the
// invocation to denied in the capability module.
func (s *Service) ExpireCapabilityConfirmation(ctx context.Context, invocationID uuid.UUID) (bool, error) {
	tag, err := s.db.Exec(ctx, `
UPDATE confirmations SET status = 'expired'
 WHERE resource_type = 'capability_invocation' AND resource_id = $1
   AND status = 'pending' AND expires_at <= now()`, invocationID.String())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
