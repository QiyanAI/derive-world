// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
)

// RemoveMember revokes a membership immediately. The owner cannot be removed
// and an owner cannot remove themself in v0.2.0.
func (s *Service) RemoveMember(ctx context.Context, actorID, objectID, targetID uuid.UUID, requestID string, ipHash []byte) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		version    int
		ownerID    string
		actorPerms []string
	)
	err = tx.QueryRow(ctx, `
SELECT o.version, o.owner_id, m.permissions
  FROM living_objects o
  JOIN object_memberships m
    ON m.object_id = o.id AND m.user_id = $2 AND m.revoked_at IS NULL
 WHERE o.id = $1 AND o.status = 'active'
   FOR SHARE OF o, m`, objectID.String(), actorID.String()).Scan(&version, &ownerID, &actorPerms)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !HasPermission(actorPerms, PermManage) {
		return ErrForbidden
	}
	if targetID.String() == ownerID {
		if actorID == targetID {
			return ErrOwnerCannotLeave
		}
		return ErrCannotRemoveOwner
	}

	var (
		targetPerms []string
		revokedAt   *time.Time
	)
	err = tx.QueryRow(ctx, `
SELECT permissions, revoked_at FROM object_memberships
 WHERE object_id = $1 AND user_id = $2
   FOR UPDATE`, objectID.String(), targetID.String()).Scan(&targetPerms, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && revokedAt != nil) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE object_memberships SET revoked_at = GREATEST(now(), created_at)
 WHERE object_id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		objectID.String(), targetID.String()); err != nil {
		return err
	}
	// A removed member must not be able to restore their own access with an
	// invitation token they minted before removal. Owner- or other-member-issued
	// invitations remain valid until their issuer or the owner revokes them.
	if _, err := tx.Exec(ctx, `
UPDATE invitations SET revoked_at = GREATEST(now(), created_at)
 WHERE object_id = $1 AND created_by = $2 AND revoked_at IS NULL`,
		objectID.String(), targetID.String()); err != nil {
		return err
	}
	if _, err := appendEventReturningID(ctx, tx, audit.UserActor(actorID), objectID,
		version, "object.member_removed", map[string]any{
			"member_user_id": targetID.String(),
			"reason":         "removed",
		}); err != nil {
		return err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(actorID),
		Action:       audit.ActionObjectMemberRemove,
		ResourceType: "living_object",
		ResourceID:   &objectID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"member_user_id": targetID.String()},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
