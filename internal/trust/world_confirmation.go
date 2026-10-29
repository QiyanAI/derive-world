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
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/store"
)

// maxWorldCreationContentBytes keeps the full confirmation payload small
// enough for the frozen 16 KiB confirmation limit. It is intentionally lower
// than the object state limit: a model proposal is a starting structure, not a
// bulk import.
const maxWorldCreationContentBytes = 12 * 1024

// WorldCreatePayload is the canonical, non-secret payload of one world.create
// confirmation. Content is an already-validated open_world create body; it is
// revalidated again inside the approval transaction before any write.
type WorldCreatePayload struct {
	Action       string          `json:"action"`
	AIProfileID  uuid.UUID       `json:"ai_profile_id"`
	DelegationID uuid.UUID       `json:"delegation_id"`
	Title        string          `json:"title"`
	Content      json.RawMessage `json:"content"`
}

// WorldCreateConfirmationParams is the trusted runtime input used to create a
// world.create confirmation from an unbound Personal AI conversation.
type WorldCreateConfirmationParams struct {
	UserID       uuid.UUID
	AIProfileID  uuid.UUID
	DelegationID uuid.UUID
	Title        string
	Content      json.RawMessage
	RequestID    string
	IPHash       []byte
}

// CreateWorldCreateConfirmationTx validates the model-proposed world content
// and stores a pending five-minute confirmation. A human approval is the only
// path that creates the object.
func (s *Service) CreateWorldCreateConfirmationTx(ctx context.Context, tx pgx.Tx,
	params WorldCreateConfirmationParams) (*Confirmation, bool, error) {
	title := strings.TrimSpace(params.Title)
	if runeLength(title) < 1 || runeLength(title) > 200 {
		return nil, false, nil
	}
	if params.AIProfileID == uuid.Nil || params.DelegationID == uuid.Nil {
		return nil, false, nil
	}
	content := bytes.TrimSpace(params.Content)
	if len(content) == 0 || !json.Valid(content) {
		return nil, false, nil
	}
	canonicalContent, err := store.CanonicalJSON(content)
	if err != nil || len(canonicalContent) > maxWorldCreationContentBytes {
		return nil, false, nil
	}
	// Validation is the same authority the normal create path uses. It proves
	// the model output is a complete, bounded open_world body before a human
	// is ever asked to approve it.
	if _, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld,
		Title:      title,
		Content:    canonicalContent,
	}); err != nil {
		// Keep the reason for the caller's redacted diagnostic. The caller maps
		// ErrInvalidRequest to a bounded rejection stage; it never returns this
		// error to the client verbatim.
		return nil, false, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	payload := WorldCreatePayload{
		Action:       ConfirmationWorldCreate,
		AIProfileID:  params.AIProfileID,
		DelegationID: params.DelegationID,
		Title:        title,
		Content:      canonicalContent,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	canonical, err := store.CanonicalJSON(encoded)
	if err != nil {
		return nil, false, ErrInvalidRequest
	}
	if len(canonical) > maxConfirmationBytes {
		return nil, false, ErrPayloadTooLarge
	}
	hash := sha256.Sum256(canonical)
	confirmationID, err := uuid.NewV7()
	if err != nil {
		return nil, false, err
	}
	var createdAt, expiresAt time.Time
	err = tx.QueryRow(ctx, `
INSERT INTO confirmations (
    id, user_id, action, resource_type, resource_id, payload, payload_hash,
    status, expires_at
) VALUES ($1,$2,$3,'personal_ai',$4,$5::jsonb,$6,'pending', now() + interval '5 minutes')
RETURNING created_at, expires_at`,
		confirmationID.String(), params.UserID.String(), ConfirmationWorldCreate,
		params.AIProfileID.String(), string(canonical), hash[:],
	).Scan(&createdAt, &expiresAt)
	if err != nil {
		return nil, false, err
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
		Metadata:     map[string]any{"confirmation_id": confirmationID.String(), "action": ConfirmationWorldCreate},
	}); err != nil {
		return nil, false, err
	}
	return &Confirmation{
		ID: confirmationID, UserID: params.UserID, Action: ConfirmationWorldCreate,
		ResourceType: "personal_ai", ResourceID: params.AIProfileID,
		Payload: canonical, PayloadHash: hash[:], Status: "pending",
		ExpiresAt: expiresAt, CreatedAt: createdAt,
	}, true, nil
}

// DecodeWorldCreatePayload parses and validates the stored payload.
func DecodeWorldCreatePayload(raw []byte) (WorldCreatePayload, error) {
	var payload WorldCreatePayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return WorldCreatePayload{}, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return WorldCreatePayload{}, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	if payload.Action != ConfirmationWorldCreate || payload.AIProfileID == uuid.Nil ||
		payload.DelegationID == uuid.Nil || strings.TrimSpace(payload.Title) == "" ||
		len(payload.Content) == 0 {
		return WorldCreatePayload{}, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	return payload, nil
}

func runeLength(value string) int {
	return utf8.RuneCountInString(value)
}

// WorldCreateReplayAllowed revalidates the exact global delegation behind an
// approved world.create confirmation before its cached response is replayed.
func (s *Service) WorldCreateReplayAllowed(ctx context.Context, userID, confirmationID uuid.UUID) error {
	var payload []byte
	err := s.db.QueryRow(ctx, `
SELECT payload FROM confirmations
 WHERE id = $1 AND user_id = $2`, confirmationID.String(), userID.String()).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	decoded, err := DecodeWorldCreatePayload(payload)
	if err != nil {
		return ErrNotFound
	}
	effective, err := s.ResolveGlobalAI(ctx, userID, decoded.AIProfileID)
	if err != nil {
		if errors.Is(err, ErrNoActiveDelegation) || errors.Is(err, ErrForbidden) ||
			errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if !effective.Can(objects.PermContribute) {
		return ErrNotFound
	}
	for _, id := range effective.DelegationIDs {
		if id == decoded.DelegationID {
			return nil
		}
	}
	return ErrNotFound
}
