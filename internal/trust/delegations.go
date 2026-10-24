// SPDX-License-Identifier: Apache-2.0
package trust

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/objects"
)

// CreateDelegationInput is the strict POST /ai/delegations body. AIProfileID
// is accepted only so a caller naming another user's profile can receive the
// frozen 404 rather than having the field silently ignored.
type CreateDelegationInput struct {
	AIProfileID   *uuid.UUID      `json:"ai_profile_id"`
	ObjectID      *uuid.UUID      `json:"object_id"`
	Permissions   []string        `json:"permissions"`
	Constraints   json.RawMessage `json:"constraints"`
	ExpiresInDays *int            `json:"expires_in_days"`
}

// Delegation is the caller-visible delegation record. It contains no secrets.
type Delegation struct {
	ID          uuid.UUID       `json:"id"`
	UserID      uuid.UUID       `json:"user_id"`
	AIProfileID uuid.UUID       `json:"ai_profile_id"`
	ObjectID    *uuid.UUID      `json:"object_id"`
	Permissions []string        `json:"permissions"`
	Constraints json.RawMessage `json:"constraints"`
	ExpiresAt   *time.Time      `json:"expires_at"`
	RevokedAt   *time.Time      `json:"revoked_at"`
	CreatedAt   time.Time       `json:"created_at"`
	Status      string          `json:"status"`
}

// CreateDelegation validates the frozen subset rules and writes the grant and
// its audit row in one transaction.
func (s *Service) CreateDelegation(ctx context.Context, userID, profileID uuid.UUID, input CreateDelegationInput, requestID string, ipHash []byte) (*Delegation, error) {
	permissions, err := normalizeDelegationPermissions(input.Permissions)
	if err != nil {
		return nil, err
	}
	constraints, err := ParseConstraints(input.Constraints)
	if err != nil {
		return nil, err
	}
	canonicalConstraints, err := constraints.MarshalCanonical()
	if err != nil {
		return nil, err
	}
	var expiresAt *time.Time
	if input.ExpiresInDays != nil {
		days := *input.ExpiresInDays
		if days < 1 || days > 365 {
			return nil, fmt.Errorf("%w: expires_in_days", ErrInvalidRequest)
		}
		value := time.Now().UTC().AddDate(0, 0, days)
		expiresAt = &value
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if input.ObjectID != nil {
		var memberPermissions []string
		err := tx.QueryRow(ctx, `
SELECT m.permissions
  FROM living_objects o
  JOIN object_memberships m
    ON m.object_id = o.id AND m.user_id = $2 AND m.revoked_at IS NULL
 WHERE o.id = $1 AND o.status = 'active'
   FOR SHARE OF o, m`, input.ObjectID.String(), userID.String()).Scan(&memberPermissions)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		for _, permission := range permissions {
			if !contains(memberPermissions, permission) {
				return nil, ErrForbidden
			}
		}
	}

	delegationID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	var (
		createdAt time.Time
		objectArg any
	)
	if input.ObjectID != nil {
		objectArg = input.ObjectID.String()
	}
	err = tx.QueryRow(ctx, `
INSERT INTO delegation_grants (
    id, user_id, ai_profile_id, object_id, permissions, constraints, expires_at
) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)
RETURNING created_at`,
		delegationID.String(), userID.String(), profileID.String(), objectArg,
		permissions, string(canonicalConstraints), expiresAt,
	).Scan(&createdAt)
	if err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionAIDelegationCreate,
		ResourceType: "delegation_grant",
		ResourceID:   &delegationID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"delegation_id": delegationID.String()},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &Delegation{
		ID: delegationID, UserID: userID, AIProfileID: profileID,
		ObjectID: input.ObjectID, Permissions: permissions,
		Constraints: canonicalConstraints, ExpiresAt: expiresAt,
		CreatedAt: createdAt, Status: "active",
	}, nil
}

// ListDelegations returns only the caller's grants.
func (s *Service) ListDelegations(ctx context.Context, userID uuid.UUID) ([]Delegation, error) {
	rows, err := s.db.Query(ctx, `
SELECT id, ai_profile_id, object_id, permissions, constraints,
       expires_at, revoked_at, created_at
  FROM delegation_grants
 WHERE user_id = $1
 ORDER BY created_at DESC, id DESC`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Delegation, 0)
	for rows.Next() {
		var (
			item       Delegation
			id         string
			profileID  string
			objectID   *string
			constraint []byte
		)
		if err := rows.Scan(&id, &profileID, &objectID, &item.Permissions,
			&constraint, &item.ExpiresAt, &item.RevokedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.ID = uuid.MustParse(id)
		item.UserID = userID
		item.AIProfileID = uuid.MustParse(profileID)
		if objectID != nil {
			parsed := uuid.MustParse(*objectID)
			item.ObjectID = &parsed
		}
		item.Permissions = orderedAIPermissions(permissionSet(item.Permissions))
		item.Constraints = constraint
		item.Status = delegationStatus(item.RevokedAt, item.ExpiresAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

// RevokeDelegation applies the revocation synchronously and writes its audit
// row in the same transaction. A non-owned id is indistinguishable from a
// missing one.
func (s *Service) RevokeDelegation(ctx context.Context, userID, delegationID uuid.UUID, requestID string, ipHash []byte) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
UPDATE delegation_grants
   SET revoked_at = GREATEST(now(), created_at)
 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		delegationID.String(), userID.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(userID),
		Action:       audit.ActionAIDelegationRevoke,
		ResourceType: "delegation_grant",
		ResourceID:   &delegationID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"delegation_id": delegationID.String()},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func normalizeDelegationPermissions(input []string) ([]string, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("%w: permissions", ErrInvalidRequest)
	}
	seen := make(map[string]bool, len(input))
	for _, permission := range input {
		switch permission {
		case objects.PermRead, objects.PermContribute, objects.PermDerive:
		default:
			return nil, fmt.Errorf("%w: permissions", ErrInvalidRequest)
		}
		if seen[permission] {
			return nil, fmt.Errorf("%w: permissions", ErrInvalidRequest)
		}
		seen[permission] = true
	}
	if !seen[objects.PermRead] {
		return nil, fmt.Errorf("%w: permissions", ErrInvalidRequest)
	}
	return orderedAIPermissions(seen), nil
}

func permissionSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func delegationScope(objectID *uuid.UUID) string {
	if objectID == nil {
		return "global"
	}
	return "object"
}

func delegationStatus(revokedAt, expiresAt *time.Time) string {
	if revokedAt != nil {
		return "revoked"
	}
	if expiresAt != nil && !expiresAt.After(time.Now().UTC()) {
		return "expired"
	}
	return "active"
}
