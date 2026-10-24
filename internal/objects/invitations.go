// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
)

const (
	invitationTokenBytes  = 32
	invitationTokenLength = 43
	defaultInvitationDays = 7
	maxInvitationDays     = 30
	defaultInvitationUses = 1
	maxInvitationUses     = 100
)

var (
	// ErrInvitationInvalid merges missing, revoked, expired, and exhausted
	// invitations into one non-enumerable result.
	ErrInvitationInvalid = errors.New("invitation is not valid")
	// ErrCannotRemoveOwner is an explicit 409 for the owner-protection rule.
	ErrCannotRemoveOwner = errors.New("cannot remove owner")
	// ErrOwnerCannotLeave is an explicit 409 for owner self-removal.
	ErrOwnerCannotLeave = errors.New("owner cannot leave")
)

// CreateInvitationInput is the strict invitation body. Omitting Permissions
// grants exactly ["read"] (D05-09).
type CreateInvitationInput struct {
	Permissions   []string `json:"permissions"`
	MaxUses       *int     `json:"max_uses"`
	ExpiresInDays *int     `json:"expires_in_days"`
}

// InvitationView is the metadata projection. Token is non-nil only in the
// single creation response; idempotent replays carry token:null.
type InvitationView struct {
	ID            uuid.UUID  `json:"id"`
	ObjectID      uuid.UUID  `json:"object_id"`
	Token         *string    `json:"token"`
	TokenRedacted bool       `json:"token_redacted,omitempty"`
	Permissions   []string   `json:"permissions"`
	MaxUses       int        `json:"max_uses"`
	UseCount      int        `json:"use_count"`
	ExpiresAt     time.Time  `json:"expires_at"`
	RevokedAt     *time.Time `json:"revoked_at"`
	CreatedAt     time.Time  `json:"created_at"`
	Status        string     `json:"status"`
}

// InvitationPreview is the logged-in invite preview. It never contains object
// state or the invitation token hash.
type InvitationPreview struct {
	InvitationID  uuid.UUID `json:"invitation_id"`
	ObjectID      uuid.UUID `json:"object_id"`
	ObjectType    string    `json:"object_type"`
	Title         string    `json:"title"`
	InviterName   string    `json:"inviter_name"`
	Permissions   []string  `json:"permissions"`
	ExpiresAt     time.Time `json:"expires_at"`
	MaxUses       int       `json:"max_uses"`
	RemainingUses int       `json:"remaining_uses"`
	AlreadyMember bool      `json:"already_member"`
}

// InvitationObjectSummary is the only object data exposed by accept.
type InvitationObjectSummary struct {
	ID                   uuid.UUID `json:"id"`
	ObjectType           string    `json:"object_type"`
	Version              int       `json:"version"`
	Title                string    `json:"title"`
	PresentationTemplate string    `json:"presentation_template"`
}

// AcceptedInvitation is the accept response after the membership write.
type AcceptedInvitation struct {
	Object        InvitationObjectSummary `json:"object"`
	Permissions   []string                `json:"permissions"`
	RoleEffective string                  `json:"role_effective"`
	AlreadyMember bool                    `json:"already_member"`
}

// CreateInvitation writes the invitation, event, audit, and redacted
// idempotency response in one transaction.
func (s *Service) CreateInvitation(
	ctx context.Context,
	actorID, objectID uuid.UUID,
	input CreateInvitationInput,
	route, idemKey string,
	leaseUntil time.Time,
	requestID string,
	ipHash []byte,
) (*InvitationView, error) {
	permissions, err := normalizeInvitationPermissions(input.Permissions)
	if err != nil {
		return nil, err
	}
	maxUses := defaultInvitationUses
	if input.MaxUses != nil {
		maxUses = *input.MaxUses
	}
	if maxUses < 1 || maxUses > maxInvitationUses {
		return nil, fmt.Errorf("%w: max_uses", ErrInvalidRequest)
	}
	days := defaultInvitationDays
	if input.ExpiresInDays != nil {
		days = *input.ExpiresInDays
	}
	if days < 1 || days > maxInvitationDays {
		return nil, fmt.Errorf("%w: expires_in_days", ErrInvalidRequest)
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		currentVersion int
		inviterPerms   []string
	)
	err = tx.QueryRow(ctx, `
SELECT o.version, m.permissions
  FROM living_objects o
  JOIN object_memberships m
    ON m.object_id = o.id AND m.user_id = $2 AND m.revoked_at IS NULL
 WHERE o.id = $1 AND o.status = 'active'
   FOR SHARE OF o, m`, objectID.String(), actorID.String()).Scan(&currentVersion, &inviterPerms)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !HasPermission(inviterPerms, PermInvite) {
		return nil, ErrForbidden
	}
	for _, permission := range permissions {
		if permission == PermManage || !HasPermission(inviterPerms, permission) {
			return nil, ErrForbidden
		}
	}

	plaintext, hash, err := newInvitationToken()
	if err != nil {
		return nil, err
	}
	invitationID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	var expiresAt, createdAt time.Time
	err = tx.QueryRow(ctx, `
INSERT INTO invitations (
    id, object_id, created_by, token_hash, permissions, max_uses, expires_at
) VALUES ($1,$2,$3,$4,$5,$6, now() + make_interval(days => $7))
RETURNING expires_at, created_at`,
		invitationID.String(), objectID.String(), actorID.String(), hash,
		permissions, maxUses, days,
	).Scan(&expiresAt, &createdAt)
	if err != nil {
		return nil, err
	}
	eventPayload := map[string]any{
		"invitation_id": invitationID.String(),
		"permissions":   permissions,
		"max_uses":      maxUses,
		"expires_at":    expiresAt,
	}
	if _, err := appendEventReturningID(ctx, tx, audit.UserActor(actorID), objectID,
		currentVersion, "object.invitation_created", eventPayload); err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(actorID),
		Action:       audit.ActionObjectInvitationCreate,
		ResourceType: "living_object",
		ResourceID:   &objectID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"invitation_id": invitationID.String()},
	}); err != nil {
		return nil, err
	}

	full := &InvitationView{
		ID: invitationID, ObjectID: objectID, Token: &plaintext,
		Permissions: permissions, MaxUses: maxUses, UseCount: 0,
		ExpiresAt: expiresAt, CreatedAt: createdAt, Status: "active",
	}
	redacted := *full
	redacted.Token = nil
	redacted.TokenRedacted = true
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return nil, err
	}
	if err := s.idem.Complete(ctx, tx, actorID, route, idemKey, leaseUntil, 200, encoded); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return full, nil
}

// ListInvitations returns active invitations under the object. It never
// returns a plaintext token or token hash.
func (s *Service) ListInvitations(ctx context.Context, objectID uuid.UUID) ([]InvitationView, error) {
	rows, err := s.db.Query(ctx, `
SELECT id, object_id, permissions, max_uses, use_count, expires_at,
       revoked_at, created_at
  FROM invitations
 WHERE object_id = $1
   AND revoked_at IS NULL
   AND expires_at > now()
   AND use_count < max_uses
 ORDER BY created_at DESC, id DESC`, objectID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]InvitationView, 0)
	for rows.Next() {
		var (
			item InvitationView
			id   string
			obj  string
		)
		if err := rows.Scan(&id, &obj, &item.Permissions, &item.MaxUses,
			&item.UseCount, &item.ExpiresAt, &item.RevokedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.ID = uuid.MustParse(id)
		item.ObjectID = uuid.MustParse(obj)
		item.Permissions = NormalizePermissions(item.Permissions)
		item.Status = "active"
		items = append(items, item)
	}
	return items, rows.Err()
}

// PreviewInvitation returns only safe invitation metadata and never state.
func (s *Service) PreviewInvitation(ctx context.Context, userID uuid.UUID, token string) (*InvitationPreview, error) {
	hash, ok := invitationTokenHash(token)
	if !ok {
		return nil, ErrInvitationInvalid
	}
	var (
		preview      InvitationPreview
		invitationID string
		objectID     string
		objectStatus string
		storedHash   []byte
		revokedAt    *time.Time
		useCount     int
	)
	err := s.db.QueryRow(ctx, `
SELECT i.id, i.object_id, i.token_hash, i.permissions, i.max_uses, i.use_count,
       i.expires_at, i.revoked_at, o.status, o.object_type,
       COALESCE(o.state->>'title',''), u.display_name
  FROM invitations i
  JOIN living_objects o ON o.id = i.object_id
  JOIN users u ON u.id = i.created_by
 WHERE i.token_hash = $1`, hash).Scan(
		&invitationID, &objectID, &storedHash, &preview.Permissions,
		&preview.MaxUses, &useCount, &preview.ExpiresAt, &revokedAt,
		&objectStatus, &preview.ObjectType, &preview.Title, &preview.InviterName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvitationInvalid
	}
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(storedHash, hash) != 1 ||
		revokedAt != nil || !preview.ExpiresAt.After(time.Now().UTC()) ||
		useCount >= preview.MaxUses || objectStatus != StatusActive {
		return nil, ErrInvitationInvalid
	}
	preview.InvitationID = uuid.MustParse(invitationID)
	preview.ObjectID = uuid.MustParse(objectID)
	preview.Permissions = NormalizePermissions(preview.Permissions)
	preview.RemainingUses = preview.MaxUses - useCount
	preview.AlreadyMember = activeMember(ctx, s.db, preview.ObjectID, userID)
	return &preview, nil
}

// AcceptInvitation consumes one use atomically and writes the membership,
// event, and audits in one transaction. An already-active member receives an
// idempotent 200 without consuming a use.
func (s *Service) AcceptInvitation(ctx context.Context, userID uuid.UUID, token string, requestID string, ipHash []byte) (*AcceptedInvitation, error) {
	hash, ok := invitationTokenHash(token)
	if !ok {
		return nil, ErrInvitationInvalid
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		invitationID string
		objectID     string
		createdBy    string
		storedHash   []byte
		permissions  []string
		maxUses      int
		useCount     int
		expiresAt    time.Time
		revokedAt    *time.Time
	)
	err = tx.QueryRow(ctx, `
SELECT id, object_id, created_by, token_hash, permissions, max_uses,
       use_count, expires_at, revoked_at
  FROM invitations
 WHERE token_hash = $1
   FOR UPDATE`, hash).Scan(
		&invitationID, &objectID, &createdBy, &storedHash, &permissions,
		&maxUses, &useCount, &expiresAt, &revokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvitationInvalid
	}
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(storedHash, hash) != 1 || revokedAt != nil ||
		!expiresAt.After(time.Now().UTC()) || useCount >= maxUses {
		return nil, ErrInvitationInvalid
	}
	objectUUID := uuid.MustParse(objectID)
	invitationUUID := uuid.MustParse(invitationID)

	var (
		objectType string
		version    int
		title      string
		template   string
		status     string
	)
	err = tx.QueryRow(ctx, `
SELECT object_type, version, COALESCE(state->>'title',''),
       presentation_template, status
  FROM living_objects
 WHERE id = $1
   FOR SHARE`, objectID).Scan(&objectType, &version, &title, &template, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvitationInvalid
	}
	if err != nil {
		return nil, err
	}
	if status != StatusActive {
		return nil, ErrInvitationInvalid
	}
	summary := InvitationObjectSummary{
		ID: objectUUID, ObjectType: objectType, Version: version,
		Title: title, PresentationTemplate: template,
	}

	var existing []string
	err = tx.QueryRow(ctx, `
SELECT permissions FROM object_memberships
 WHERE object_id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		objectID, userID.String()).Scan(&existing)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &AcceptedInvitation{
			Object: summary, Permissions: NormalizePermissions(existing),
			RoleEffective: RoleFromPermissions(existing), AlreadyMember: true,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	var newUseCount int
	err = tx.QueryRow(ctx, `
UPDATE invitations
   SET use_count = use_count + 1
 WHERE id = $1
   AND use_count < max_uses
   AND revoked_at IS NULL
   AND expires_at > now()
RETURNING use_count`, invitationID).Scan(&newUseCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvitationInvalid
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO object_memberships (object_id, user_id, permissions, granted_by)
VALUES ($1,$2,$3,$4)
ON CONFLICT (object_id, user_id) DO UPDATE
   SET permissions = EXCLUDED.permissions,
       revoked_at = NULL,
       granted_by = EXCLUDED.granted_by`,
		objectID, userID.String(), permissions, createdBy); err != nil {
		return nil, mapConstraintError(err)
	}
	if _, err := appendEventReturningID(ctx, tx, audit.UserActor(userID), objectUUID,
		version, "object.member_added", map[string]any{
			"member_user_id": userID.String(),
			"permissions":    permissions,
		}); err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionObjectInvitationAccept,
		ResourceType: "living_object",
		ResourceID:   &objectUUID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"invitation_id": invitationUUID.String()},
	}); err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionObjectMemberAdd,
		ResourceType: "living_object",
		ResourceID:   &objectUUID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"member_user_id": userID.String()},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &AcceptedInvitation{
		Object: summary, Permissions: NormalizePermissions(permissions),
		RoleEffective: RoleFromPermissions(permissions), AlreadyMember: false,
	}, nil
}

// RevokeInvitation is synchronous and immediately invalidates the token.
func (s *Service) RevokeInvitation(ctx context.Context, actorID, objectID, invitationID uuid.UUID, requestID string, ipHash []byte) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock order is invitation -> object, matching AcceptInvitation. Locks are
	// acquired in this order to avoid a deadlock between accept and revoke.
	var (
		createdBy string
		revokedAt *time.Time
	)
	err = tx.QueryRow(ctx, `
SELECT created_by, revoked_at FROM invitations
 WHERE id = $1 AND object_id = $2
   FOR UPDATE`, invitationID.String(), objectID.String()).Scan(&createdBy, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	var (
		version    int
		actorPerms []string
	)
	err = tx.QueryRow(ctx, `
SELECT o.version, m.permissions
  FROM living_objects o
  JOIN object_memberships m
    ON m.object_id = o.id AND m.user_id = $2 AND m.revoked_at IS NULL
 WHERE o.id = $1 AND o.status = 'active'
   FOR SHARE OF o, m`, objectID.String(), actorID.String()).Scan(&version, &actorPerms)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !HasPermission(actorPerms, PermInvite) && !HasPermission(actorPerms, PermManage) {
		return ErrForbidden
	}
	if !HasPermission(actorPerms, PermManage) && createdBy != actorID.String() {
		return ErrNotFound
	}
	if revokedAt != nil {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
UPDATE invitations SET revoked_at = GREATEST(now(), created_at)
 WHERE id = $1 AND revoked_at IS NULL`, invitationID.String()); err != nil {
		return err
	}
	if _, err := appendEventReturningID(ctx, tx, audit.UserActor(actorID), objectID,
		version, "object.invitation_revoked", map[string]any{
			"invitation_id": invitationID.String(),
		}); err != nil {
		return err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(actorID),
		Action:       audit.ActionObjectInvitationRevoke,
		ResourceType: "living_object",
		ResourceID:   &objectID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata: map[string]any{
			"invitation_id": invitationID.String(),
		},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func normalizeInvitationPermissions(input []string) ([]string, error) {
	if len(input) == 0 {
		return []string{PermRead}, nil
	}
	seen := make(map[string]bool, len(input))
	for _, permission := range input {
		switch permission {
		case PermRead, PermContribute, PermDerive, PermInvite:
		default:
			return nil, fmt.Errorf("%w: permissions", ErrInvalidRequest)
		}
		if seen[permission] {
			return nil, fmt.Errorf("%w: permissions", ErrInvalidRequest)
		}
		seen[permission] = true
	}
	if !seen[PermRead] {
		return nil, fmt.Errorf("%w: permissions", ErrInvalidRequest)
	}
	return NormalizePermissions(input), nil
}

func newInvitationToken() (string, []byte, error) {
	buffer := make([]byte, invitationTokenBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", nil, err
	}
	plaintext := base64.RawURLEncoding.EncodeToString(buffer)
	if len(plaintext) != invitationTokenLength {
		return "", nil, errors.New("invitation token length is invalid")
	}
	sum := sha256.Sum256([]byte(plaintext))
	return plaintext, sum[:], nil
}

func invitationTokenHash(token string) ([]byte, bool) {
	if len(token) != invitationTokenLength {
		return nil, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != invitationTokenBytes {
		return nil, false
	}
	sum := sha256.Sum256([]byte(token))
	return sum[:], true
}

type memberQueryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func activeMember(ctx context.Context, q memberQueryer, objectID, userID uuid.UUID) bool {
	var exists bool
	err := q.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM object_memberships
   WHERE object_id = $1 AND user_id = $2 AND revoked_at IS NULL
)`, objectID.String(), userID.String()).Scan(&exists)
	return err == nil && exists
}
