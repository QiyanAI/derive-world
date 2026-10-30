// SPDX-License-Identifier: Apache-2.0
package trust

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/store"
)

const maxConfirmationBytes = 16 * 1024

// DerivePayload is the canonical, non-secret payload of a derive.create
// confirmation. It deliberately contains no object state or private context.
type DerivePayload struct {
	Action            string    `json:"action"`
	ObjectID          uuid.UUID `json:"object_id"`
	ExpectedVersion   int       `json:"expected_version"`
	AIProfileID       uuid.UUID `json:"ai_profile_id"`
	DelegationID      uuid.UUID `json:"delegation_id"`
	Title             string    `json:"title"`
	SourceVersion     *int      `json:"source_version,omitempty"`
	InheritanceFields []string  `json:"inheritance_fields"`
}

// DeriveConfirmationParams is the trusted runtime input used to create a
// derive confirmation.
type DeriveConfirmationParams struct {
	UserID            uuid.UUID
	AIProfileID       uuid.UUID
	DelegationID      uuid.UUID
	ObjectID          uuid.UUID
	ExpectedVersion   int
	Title             string
	SourceVersion     *int
	InheritanceFields []string
	RequestID         string
	IPHash            []byte
}

// Confirmation is the internal row projection; Payload is never returned to a
// client directly.
type Confirmation struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Action       string
	ResourceType string
	ResourceID   uuid.UUID
	Payload      json.RawMessage
	PayloadHash  []byte
	Status       string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	DecidedAt    *time.Time
}

// ConfirmationView is the safe display projection from D05-13. It exposes no
// payload, no AI input, and no object state. G05 adds the target object id,
// the version the request is bound to, and a bounded effect summary so a human
// can identify and review the pending action without decoding the payload.
type ConfirmationView struct {
	ID                  uuid.UUID           `json:"id"`
	Action              string              `json:"action"`
	ResourceType        string              `json:"resource_type"`
	ResourceID          uuid.UUID           `json:"resource_id"`
	Status              string              `json:"status"`
	ExpiresAt           time.Time           `json:"expires_at"`
	CreatedAt           time.Time           `json:"created_at"`
	DecidedAt           *time.Time          `json:"decided_at"`
	PayloadHashPrefix   string              `json:"payload_hash_prefix"`
	TargetObjectID      *uuid.UUID          `json:"target_object_id,omitempty"`
	TargetObjectTitle   string              `json:"target_object_title"`
	TargetObjectVersion int                 `json:"target_object_version"`
	TargetObjectType    string              `json:"target_object_type"`
	BoundVersion        int                 `json:"bound_version,omitempty"`
	Effect              *ConfirmationEffect `json:"effect,omitempty"`
	PermissionSummary   string              `json:"permission_summary"`
	// ConversationID and MessageID place the approval directly under the
	// assistant reply that produced it. They are absent for confirmations that
	// did not originate in a Personal AI turn.
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	MessageID      *uuid.UUID `json:"message_id,omitempty"`
	// ResultObjectID identifies the object an approved confirmation actually
	// created, and is exposed only while the caller can still read that object.
	// A refreshed conversation uses it to show the real entry point instead of
	// asking for the same approval again.
	ResultObjectID    *uuid.UUID `json:"result_object_id,omitempty"`
	ResultObjectTitle string     `json:"result_object_title,omitempty"`
	ResultObjectType  string     `json:"result_object_type,omitempty"`
}

// ConfirmationEffect is the bounded, non-secret effect of a pending
// confirmation. It never includes object state, private context, or the raw
// model input.
type ConfirmationEffect struct {
	Title             string   `json:"title,omitempty"`
	InheritanceFields []string `json:"inheritance_fields,omitempty"`
	SourceVersion     *int     `json:"source_version,omitempty"`
	// ObjectType, Summary, and the counts describe a world.create
	// confirmation so a human can recognize what will be created without
	// seeing the raw payload.
	ObjectType string `json:"object_type,omitempty"`
	Summary    string `json:"summary,omitempty"`
	KindCount  int    `json:"kind_count,omitempty"`
	NodeCount  int    `json:"node_count,omitempty"`
	RuleCount  int    `json:"rule_count,omitempty"`
	// KindNames and ThingNames let a human preview the actual content they are
	// about to create instead of approving a blind title. They are bounded and
	// derived from the validated content, never from model prose.
	KindNames        []string `json:"kind_names,omitempty"`
	ThingNames       []string `json:"thing_names,omitempty"`
	InteractionCount int      `json:"interaction_count,omitempty"`
	PrimarySurface   string   `json:"primary_surface,omitempty"`
	// Surfaces names the real universal runtimes the pending world will use.
	// It is derived from the validated experience document, never from prose.
	Surfaces []string `json:"surfaces,omitempty"`
}

type confirmationPayloadProjection struct {
	ObjectID          uuid.UUID       `json:"object_id"`
	ExpectedVersion   int             `json:"expected_version"`
	Title             string          `json:"title"`
	SourceVersion     *int            `json:"source_version"`
	InheritanceFields []string        `json:"inheritance_fields"`
	Input             json.RawMessage `json:"input"`
}

// CreateDeriveConfirmationTx writes a pending confirmation and its audit row.
// The caller has already recomputed the AI intersection in this transaction.
func (s *Service) CreateDeriveConfirmationTx(ctx context.Context, tx pgx.Tx, params DeriveConfirmationParams) (*Confirmation, error) {
	if params.ExpectedVersion < 1 {
		return nil, fmt.Errorf("%w: expected_version", ErrInvalidRequest)
	}
	if len([]rune(params.Title)) < 1 || len([]rune(params.Title)) > 200 {
		return nil, fmt.Errorf("%w: title", ErrInvalidRequest)
	}
	if len(params.InheritanceFields) == 0 {
		return nil, fmt.Errorf("%w: inheritance_fields", ErrInvalidRequest)
	}
	payload := DerivePayload{
		Action:            ConfirmationDeriveCreate,
		ObjectID:          params.ObjectID,
		ExpectedVersion:   params.ExpectedVersion,
		AIProfileID:       params.AIProfileID,
		DelegationID:      params.DelegationID,
		Title:             params.Title,
		SourceVersion:     params.SourceVersion,
		InheritanceFields: append([]string(nil), params.InheritanceFields...),
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
) VALUES ($1,$2,$3,'living_object',$4,$5::jsonb,$6,'pending', now() + interval '5 minutes')
RETURNING created_at, expires_at`,
		confirmationID.String(), params.UserID.String(), ConfirmationDeriveCreate,
		params.ObjectID.String(), string(canonical), hash[:],
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
		ID: confirmationID, UserID: params.UserID, Action: ConfirmationDeriveCreate,
		ResourceType: "living_object", ResourceID: params.ObjectID,
		Payload: canonical, PayloadHash: hash[:], Status: "pending",
		ExpiresAt: expiresAt, CreatedAt: createdAt,
	}, nil
}

// confirmationViewSelect is the single safe projection shared by list, get and
// post-decision reads. It joins the target object (for an object-bound action)
// and the result object (for an approved creation), and it only exposes either
// while the caller can still read it.
const confirmationViewSelect = `
SELECT c.id, c.action, c.resource_type, c.resource_id, c.payload, c.status,
       c.expires_at, c.created_at, c.decided_at, c.payload_hash,
       COALESCE(o.state->>'title',''), COALESCE(o.version,0), COALESCE(o.object_type,''),
       COALESCE(o.status = 'active' AND EXISTS (
           SELECT 1 FROM object_memberships m
            WHERE m.object_id = o.id AND m.user_id = c.user_id AND m.revoked_at IS NULL
       ), false) AS readable,
       c.conversation_id, c.message_id,
       ro.id, COALESCE(ro.state->>'title',''), COALESCE(ro.object_type,'')
  FROM confirmations c
  LEFT JOIN living_objects o
    ON o.id = CASE
        WHEN c.resource_type = 'living_object' THEN c.resource_id
        ELSE NULLIF(c.payload->>'object_id','')::uuid
    END
  LEFT JOIN living_objects ro
    ON ro.id = c.result_object_id
   AND ro.status = 'active'
   AND EXISTS (
       SELECT 1 FROM object_memberships rm
        WHERE rm.object_id = ro.id AND rm.user_id = c.user_id AND rm.revoked_at IS NULL
   )`

// ListConfirmations returns the caller's pending confirmations by default.
// conversationID optionally scopes the read to one conversation, so a busy
// account can still find an unexpired approval after a refresh.
func (s *Service) ListConfirmations(ctx context.Context, userID uuid.UUID, status string, conversationID *uuid.UUID) ([]ConfirmationView, error) {
	query := confirmationViewSelect + `
 WHERE c.user_id = $1`
	args := []any{userID.String()}
	if conversationID != nil {
		query += fmt.Sprintf(` AND c.conversation_id = $%d`, len(args)+1)
		args = append(args, conversationID.String())
	}
	switch status {
	case "", "pending":
		query += ` AND c.status = 'pending' AND c.expires_at > now()`
	case "all":
	default:
		return nil, fmt.Errorf("%w: status", ErrInvalidRequest)
	}
	query += ` ORDER BY c.created_at DESC, c.id DESC LIMIT 100`
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ConfirmationView, 0)
	for rows.Next() {
		view, err := scanConfirmationView(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, view)
	}
	return items, rows.Err()
}

// GetConfirmation returns the safe projection. Expiry is evaluated lazily and
// returned as confirmation_expired (410).
func (s *Service) GetConfirmation(ctx context.Context, userID, confirmationID uuid.UUID) (*ConfirmationView, error) {
	row := s.db.QueryRow(ctx, confirmationViewSelect+`
 WHERE c.id = $1 AND c.user_id = $2`, confirmationID.String(), userID.String())
	view, err := scanConfirmationView(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if view.Status == "expired" {
		return nil, ErrConfirmationExpired
	}
	if view.Status == "pending" && !view.ExpiresAt.After(time.Now().UTC()) {
		_, _ = s.db.Exec(ctx, `
UPDATE confirmations SET status = 'expired'
 WHERE id = $1 AND user_id = $2 AND status = 'pending' AND expires_at <= now()`,
			confirmationID.String(), userID.String())
		return nil, ErrConfirmationExpired
	}
	return &view, nil
}

// ApproveExecutor executes the target domain action inside the approval
// transaction. It returns the serialized response and, when the action created
// an object, that object's id so the confirmation can remember the real result
// across a refresh. execute is supplied by the composition root and must
// revalidate permissions and version. On any execute error the confirmation
// remains pending and nothing is committed.
type ApproveExecutor func(ctx context.Context, tx pgx.Tx, confirmation Confirmation) ([]byte, *uuid.UUID, error)

// Approve applies the confirmation single-use semantics and idempotency
// completion. It returns the serialized successful response body.
func (s *Service) Approve(
	ctx context.Context,
	userID, confirmationID uuid.UUID,
	route, idemKey string,
	leaseUntil time.Time,
	requestID string,
	ipHash []byte,
	execute ApproveExecutor,
) ([]byte, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	confirmation, err := loadConfirmationForUpdate(ctx, tx, userID, confirmationID)
	if err != nil {
		return nil, err
	}
	switch confirmation.Status {
	case "approved":
		return nil, ErrConflict
	case "denied":
		return nil, ErrConflict
	case "expired":
		return nil, ErrConfirmationExpired
	case "pending":
	default:
		return nil, ErrConflict
	}
	if !confirmation.ExpiresAt.After(time.Now().UTC()) {
		if _, err := tx.Exec(ctx, `
UPDATE confirmations SET status = 'expired'
 WHERE id = $1 AND status = 'pending'`, confirmationID.String()); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, ErrConfirmationExpired
	}
	computed, err := canonicalPayloadHash(confirmation.Payload)
	if err != nil {
		return nil, err
	}
	if len(computed) != len(confirmation.PayloadHash) ||
		subtle.ConstantTimeCompare(computed, confirmation.PayloadHash) != 1 {
		return nil, ErrConflict
	}
	switch confirmation.ResourceType {
	case "living_object":
		if confirmation.Action != ConfirmationDeriveCreate {
			return nil, ErrUnsupportedAction
		}
		payload, err := DecodeDerivePayload(confirmation.Payload)
		if err != nil {
			return nil, ErrConflict
		}
		if payload.ObjectID != confirmation.ResourceID {
			return nil, ErrConflict
		}
	case "capability_invocation":
		payload, err := DecodeCapabilityConfirmationPayload(confirmation.Payload)
		if err != nil {
			return nil, ErrConflict
		}
		if payload.InvocationID != confirmation.ResourceID ||
			payload.Action != confirmation.Action {
			return nil, ErrConflict
		}
	case "personal_ai":
		if confirmation.Action != ConfirmationWorldCreate {
			return nil, ErrUnsupportedAction
		}
		payload, err := DecodeWorldCreatePayload(confirmation.Payload)
		if err != nil {
			return nil, ErrConflict
		}
		if payload.AIProfileID != confirmation.ResourceID {
			return nil, ErrConflict
		}
	default:
		return nil, ErrUnsupportedAction
	}

	body, resultObjectID, err := execute(ctx, tx, *confirmation)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || !json.Valid(body) {
		return nil, errors.New("confirmation executor returned invalid JSON")
	}
	var resultArg *string
	if resultObjectID != nil && *resultObjectID != uuid.Nil {
		value := resultObjectID.String()
		resultArg = &value
	}
	tag, err := tx.Exec(ctx, `
UPDATE confirmations
   SET status = 'approved', decided_at = GREATEST(now(), created_at),
       result_object_id = $2
 WHERE id = $1 AND status = 'pending'`, confirmationID.String(), resultArg)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrConflict
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionAIConfirmationApprove,
		ResourceType: "confirmation",
		ResourceID:   &confirmationID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"action": confirmation.Action},
	}); err != nil {
		return nil, err
	}
	if idemKey != "" {
		if err := s.idemComplete(ctx, tx, userID, route, idemKey, leaseUntil, body); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return body, nil
}

// Deny records a denial. Repeated deny is idempotent; approval and expiry are
// not.
func (s *Service) Deny(ctx context.Context, userID, confirmationID uuid.UUID, requestID string, ipHash []byte) (*ConfirmationView, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	confirmation, err := loadConfirmationForUpdate(ctx, tx, userID, confirmationID)
	if err != nil {
		return nil, err
	}
	if confirmation.Status == "denied" {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return s.confirmationView(ctx, userID, confirmationID)
	}
	if confirmation.Status == "approved" {
		return nil, ErrConflict
	}
	if confirmation.Status == "expired" {
		return nil, ErrConfirmationExpired
	}
	if !confirmation.ExpiresAt.After(time.Now().UTC()) {
		if _, err := tx.Exec(ctx, `
UPDATE confirmations SET status = 'expired'
 WHERE id = $1 AND status = 'pending'`, confirmationID.String()); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, ErrConfirmationExpired
	}
	if _, err := tx.Exec(ctx, `
UPDATE confirmations SET status = 'denied', decided_at = GREATEST(now(), created_at)
 WHERE id = $1 AND status = 'pending'`, confirmationID.String()); err != nil {
		return nil, err
	}
	metadata := map[string]any{
		"action":      confirmation.Action,
		"resource_id": confirmation.ResourceID.String(),
	}
	if confirmation.ResourceType == "living_object" {
		metadata["object_id"] = confirmation.ResourceID.String()
	}
	if confirmation.ResourceType == "personal_ai" {
		metadata["ai_profile_id"] = confirmation.ResourceID.String()
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionAIConfirmationDeny,
		ResourceType: "confirmation",
		ResourceID:   &confirmationID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     metadata,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.confirmationView(ctx, userID, confirmationID)
}

// LinkConfirmationsToMessageTx records the conversation turn behind each
// accepted confirmation. It is called inside the AI turn's transaction and is
// the only writer of these display-only columns. A zero-row update is an error:
// silently committing an unlinked confirmation would reproduce the very bug
// this feature fixes (the reply says "press the button below" and no button
// exists after a refresh).
func (s *Service) LinkConfirmationsToMessageTx(ctx context.Context, tx pgx.Tx,
	userID, conversationID, messageID uuid.UUID, ids []uuid.UUID) error {
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
UPDATE confirmations
   SET conversation_id = $1, message_id = $2
 WHERE id = $3 AND user_id = $4
   AND (message_id IS NULL OR message_id = $2)`,
			conversationID.String(), messageID.String(), id.String(), userID.String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: confirmation link", ErrConflict)
		}
	}
	return nil
}

// HasPendingWorldCreateTx reports whether this conversation already has an
// unexpired pending world plan. One plan per conversation keeps a single
// confirm button with one meaning; the human can deny it before asking again.
func (s *Service) HasPendingWorldCreateTx(ctx context.Context, tx pgx.Tx,
	userID, conversationID uuid.UUID) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM confirmations
     WHERE user_id = $1 AND conversation_id = $2
       AND action = $3 AND status = 'pending' AND expires_at > now()
)`, userID.String(), conversationID.String(), ConfirmationWorldCreate).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (s *Service) confirmationView(ctx context.Context, userID, confirmationID uuid.UUID) (*ConfirmationView, error) {
	row := s.db.QueryRow(ctx, confirmationViewSelect+`
 WHERE c.id = $1 AND c.user_id = $2`, confirmationID.String(), userID.String())
	view, err := scanConfirmationView(row)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanConfirmationView(row rowScanner) (ConfirmationView, error) {
	var (
		view           ConfirmationView
		id             string
		resourceID     string
		hash           []byte
		payload        []byte
		objectTitle    string
		readable       bool
		conversationID *string
		messageID      *string
		resultObjectID *string
		resultTitle    string
		resultType     string
	)
	err := row.Scan(&id, &view.Action, &view.ResourceType, &resourceID, &payload, &view.Status,
		&view.ExpiresAt, &view.CreatedAt, &view.DecidedAt, &hash,
		&objectTitle, &view.TargetObjectVersion, &view.TargetObjectType, &readable,
		&conversationID, &messageID, &resultObjectID, &resultTitle, &resultType)
	if err != nil {
		return ConfirmationView{}, err
	}
	view.ID = uuid.MustParse(id)
	view.ResourceID = uuid.MustParse(resourceID)
	view.PayloadHashPrefix = hex.EncodeToString(hash)
	if len(view.PayloadHashPrefix) > 12 {
		view.PayloadHashPrefix = view.PayloadHashPrefix[:12]
	}
	if conversationID != nil {
		if parsed, err := uuid.Parse(*conversationID); err == nil {
			view.ConversationID = &parsed
		}
	}
	if messageID != nil {
		if parsed, err := uuid.Parse(*messageID); err == nil {
			view.MessageID = &parsed
		}
	}
	// The result object join only matches an active object the caller can still
	// read, so a non-null id here is safe to expose and link to.
	if resultObjectID != nil && resultType != "" {
		if parsed, err := uuid.Parse(*resultObjectID); err == nil {
			view.ResultObjectID = &parsed
			view.ResultObjectTitle = resultTitle
			view.ResultObjectType = resultType
		}
	}
	// list/status=all reads must not present an already-expired row as pending.
	// GetConfirmation still performs the bounded lazy UPDATE when a caller
	// addresses one row directly.
	if view.Status == "pending" && !view.ExpiresAt.After(time.Now().UTC()) {
		view.Status = "expired"
	}
	view.PermissionSummary = permissionSummary(view.Action)
	// An unbound Personal AI world.create confirmation has no object to join.
	// Its resource is the caller's own AI profile, so the caller may always
	// review the bounded effect summary.
	if view.ResourceType == "personal_ai" && view.Action == ConfirmationWorldCreate {
		readable = true
	}

	// The confirmation belongs to the caller, but the target object may have
	// become unreadable after creation (membership removal or soft delete).
	// Its title, live version, and effect are hidden in that case; execution is
	// already blocked by the object authorization chain.
	if !readable {
		view.TargetObjectTitle = ""
		view.TargetObjectVersion = 0
		view.TargetObjectType = ""
		view.BoundVersion = 0
		view.Effect = nil
		return view, nil
	}
	view.TargetObjectTitle = objectTitle

	var projection confirmationPayloadProjection
	if len(payload) > 0 && json.Unmarshal(payload, &projection) == nil {
		view.BoundVersion = projection.ExpectedVersion
		targetID := projection.ObjectID
		if targetID == uuid.Nil && view.ResourceType == "living_object" {
			targetID = view.ResourceID
		}
		if targetID != uuid.Nil {
			idCopy := targetID
			view.TargetObjectID = &idCopy
		}
		if view.Action == ConfirmationDeriveCreate {
			title := projection.Title
			fields := projection.InheritanceFields
			sourceVersion := projection.SourceVersion
			if title == "" && len(projection.Input) > 0 {
				var effectInput struct {
					Title             string   `json:"title"`
					InheritanceFields []string `json:"inheritance_fields"`
					SourceVersion     *int     `json:"source_version"`
				}
				if json.Unmarshal(projection.Input, &effectInput) == nil {
					title = effectInput.Title
					fields = effectInput.InheritanceFields
					sourceVersion = effectInput.SourceVersion
				}
			}
			if title != "" {
				runes := []rune(title)
				if len(runes) > 200 {
					title = string(runes[:200])
				}
				if len(fields) > 10 {
					fields = fields[:10]
				}
				view.Effect = &ConfirmationEffect{
					Title:             title,
					InheritanceFields: append([]string(nil), fields...),
					SourceVersion:     sourceVersion,
				}
			}
		}
	}
	if view.Action == ConfirmationWorldCreate {
		var worldPayload WorldCreatePayload
		if json.Unmarshal(payload, &worldPayload) == nil && strings.TrimSpace(worldPayload.Title) != "" {
			var content struct {
				Premise string `json:"premise"`
				Kinds   []struct {
					Name string `json:"name"`
				} `json:"kinds"`
				Nodes []struct {
					Name string `json:"name"`
				} `json:"nodes"`
				Rules        []json.RawMessage `json:"rules"`
				Interactions []json.RawMessage `json:"interactions"`
				Experience   *struct {
					Root experienceViewNode `json:"root"`
				} `json:"experience"`
			}
			_ = json.Unmarshal(worldPayload.Content, &content)
			summary := strings.TrimSpace(content.Premise)
			if runes := []rune(summary); len(runes) > 300 {
				summary = string(runes[:300])
			}
			surfaces := []string{}
			if content.Experience != nil {
				collectExperienceSurfaces(content.Experience.Root, &surfaces, map[string]bool{})
			}
			kindNames := make([]string, 0, len(content.Kinds))
			for _, kind := range content.Kinds {
				if name := boundedPreviewName(kind.Name, 60); name != "" {
					kindNames = append(kindNames, name)
				}
				if len(kindNames) >= 8 {
					break
				}
			}
			thingNames := make([]string, 0, len(content.Nodes))
			for _, node := range content.Nodes {
				if name := boundedPreviewName(node.Name, 80); name != "" {
					thingNames = append(thingNames, name)
				}
				if len(thingNames) >= 12 {
					break
				}
			}
			primary := ""
			for _, surface := range surfaces {
				if surface != "audio" {
					primary = surface
					break
				}
			}
			if primary == "" && len(surfaces) > 0 {
				primary = surfaces[0]
			}
			view.Effect = &ConfirmationEffect{
				Title:            strings.TrimSpace(worldPayload.Title),
				ObjectType:       objects.TypeOpenWorld,
				Summary:          summary,
				KindCount:        len(content.Kinds),
				NodeCount:        len(content.Nodes),
				RuleCount:        len(content.Rules),
				KindNames:        kindNames,
				ThingNames:       thingNames,
				InteractionCount: len(content.Interactions),
				PrimarySurface:   primary,
				Surfaces:         surfaces,
			}
		}
	}
	return view, nil
}

// boundedPreviewName keeps a preview name short and single-line. It is display
// text, not an identifier, so it never carries control characters.
func boundedPreviewName(value string, max int) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.ReplaceAll(trimmed, "\n", " ")
	trimmed = strings.ReplaceAll(trimmed, "\r", " ")
	runes := []rune(trimmed)
	if len(runes) > max {
		return string(runes[:max])
	}
	return trimmed
}

func permissionSummary(action string) string {
	switch action {
	case ConfirmationDeriveCreate:
		return "Creates one new independent derived object from the current snapshot; the parent is not modified."
	case ConfirmationCapabilityInvoke:
		return "Invokes one capability on the target object."
	case ConfirmationInvitationCreate:
		return "Creates an invitation. This action is not triggered in v0.1.0."
	case ConfirmationWorldCreate:
		return "Creates one new private world owned by you. Nothing is written until you approve."
	default:
		return "No permission change."
	}
}

func loadConfirmationForUpdate(ctx context.Context, tx pgx.Tx, userID, confirmationID uuid.UUID) (*Confirmation, error) {
	var (
		confirmation Confirmation
		id           string
		resourceID   string
		payload      []byte
		hash         []byte
	)
	err := tx.QueryRow(ctx, `
SELECT id, user_id, action, resource_type, resource_id, payload, payload_hash,
       status, expires_at, created_at, decided_at
  FROM confirmations
 WHERE id = $1 AND user_id = $2
   FOR UPDATE`, confirmationID.String(), userID.String()).Scan(
		&id, &confirmation.UserID, &confirmation.Action, &confirmation.ResourceType,
		&resourceID, &payload, &hash, &confirmation.Status, &confirmation.ExpiresAt,
		&confirmation.CreatedAt, &confirmation.DecidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	confirmation.ID = uuid.MustParse(id)
	confirmation.ResourceID = uuid.MustParse(resourceID)
	confirmation.Payload = payload
	confirmation.PayloadHash = hash
	return &confirmation, nil
}

func canonicalPayloadHash(payload []byte) ([]byte, error) {
	canonical, err := store.CanonicalJSON(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	hash := sha256.Sum256(canonical)
	return hash[:], nil
}

// The confirmation service uses the frozen two-phase idempotency record. The
// actual completion is delegated to store so the table has a single writer.
func (s *Service) idemComplete(ctx context.Context, tx pgx.Tx, userID uuid.UUID, route, key string, leaseUntil time.Time, body []byte) error {
	if s.idem == nil {
		return errors.New("idempotency store is not configured")
	}
	return s.idem.Complete(ctx, tx, userID, route, key, leaseUntil, 200, body)
}

// DecodeDerivePayload parses and validates the confirmation payload.
func DecodeDerivePayload(raw []byte) (DerivePayload, error) {
	var payload DerivePayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return DerivePayload{}, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return DerivePayload{}, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	if payload.Action != ConfirmationDeriveCreate || payload.ObjectID == uuid.Nil ||
		payload.ExpectedVersion < 1 || len(payload.InheritanceFields) == 0 {
		return DerivePayload{}, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	return payload, nil
}

func safeRequestID(value string) string {
	if len(value) >= 8 && len(value) <= 64 {
		return value
	}
	return "internal-ai"
}

// experienceViewNode mirrors the composed capability tree. The worldseed
// composer wraps multi-surface experiences in core.tabs and audio layers in
// core.stack, so the preview must walk the tree instead of inspecting only the
// root and its immediate children.
type experienceViewNode struct {
	Capability string               `json:"capability"`
	Children   []experienceViewNode `json:"children"`
}

func collectExperienceSurfaces(node experienceViewNode, out *[]string, seen map[string]bool) {
	if name := surfaceNameForCapability(node.Capability); name != "" && !seen[name] {
		seen[name] = true
		*out = append(*out, name)
	}
	for _, child := range node.Children {
		collectExperienceSurfaces(child, out, seen)
	}
}

func surfaceNameForCapability(capability string) string {
	switch capability {
	case "core.live":
		return "live"
	case "core.story":
		return "story"
	case "core.workspace":
		return "workspace"
	case "core.custom":
		return "custom"
	case "core.space":
		return "space"
	case "core.scene3d":
		return "scene3d"
	case "core.media":
		return "media"
	case "core.audio":
		return "audio"
	default:
		return ""
	}
}
