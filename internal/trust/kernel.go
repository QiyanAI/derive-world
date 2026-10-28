// SPDX-License-Identifier: Apache-2.0

// Package trust is the single authorization decision point for Personal AI
// delegation, confirmation records, and confirmation execution. It owns
// delegation_grants and confirmations and never writes object state.
package trust

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/store"
)

// Service is the Trust Kernel. All AI effective-permission decisions pass
// through it; there is no process-wide permission cache (D05-16).
type Service struct {
	db    *pgxpool.Pool
	audit *audit.Writer
	idem  *store.Idempotency
}

// NewService assembles the Trust Kernel.
func NewService(db *pgxpool.Pool, auditWriter *audit.Writer, idem *store.Idempotency) *Service {
	return &Service{db: db, audit: auditWriter, idem: idem}
}

// Effective is the result of the frozen three-way intersection:
// membership ∩ active delegation permissions ∩ constraints, with human-only
// permissions removed. DelegationIDs contains every active delegation that
// participated, while DelegationID is the primary one recorded on writes.
type Effective struct {
	UserID              uuid.UUID
	AIProfileID         uuid.UUID
	DelegationID        uuid.UUID
	DelegationIDs       []uuid.UUID
	Permissions         []string
	RequireConfirmation map[string]bool
	MaxAIActionsPerHour int
	ObjectVersion       int
}

// Can reports whether the effective AI permission set contains value.
func (e *Effective) Can(value string) bool {
	if e == nil {
		return false
	}
	for _, permission := range e.Permissions {
		if permission == value {
			return true
		}
	}
	return false
}

// RequiresConfirmation reports whether an AI action must be confirmed by a
// human. Derive and side-effecting capabilities are always gated, regardless
// of a narrower stored constraint set.
func (e *Effective) RequiresConfirmation(action string) bool {
	if action == ConfirmationDeriveCreate || action == ConfirmationCapabilityInvoke {
		return true
	}
	if e == nil {
		return true
	}
	return e.RequireConfirmation[action]
}

// Actor returns the server-derived personal_ai audit actor.
func (e *Effective) Actor() audit.Actor {
	userID, profileID, delegationID := e.UserID, e.AIProfileID, e.DelegationID
	return audit.Actor{
		Type: "personal_ai", UserID: &userID,
		AIProfileID: &profileID, DelegationID: &delegationID,
	}
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ResolveAI computes effective permissions for an already-loaded object. The
// caller passes membership permissions from LoadForActor so the request does
// not repeat the membership query.
func (s *Service) ResolveAI(ctx context.Context, userID, profileID uuid.UUID, objectID *uuid.UUID, memberPermissions []string) (*Effective, error) {
	return resolveAI(ctx, s.db, userID, profileID, objectID, memberPermissions)
}

// ResolveAITx recomputes the intersection inside the caller's transaction and
// locks the object and membership rows so a concurrent revocation cannot slip
// between authorization and execution. It is the execution-time revalidation
// required by D05-13 and D05-16.
func (s *Service) ResolveAITx(ctx context.Context, tx pgx.Tx, userID, profileID uuid.UUID, objectID uuid.UUID) (*Effective, error) {
	var status string
	var version int
	if err := tx.QueryRow(ctx, `
SELECT status, version FROM living_objects WHERE id = $1 FOR SHARE`,
		objectID.String()).Scan(&status, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != objects.StatusActive {
		return nil, ErrNotFound
	}
	var permissions []string
	if err := tx.QueryRow(ctx, `
SELECT permissions FROM object_memberships
 WHERE object_id = $1 AND user_id = $2 AND revoked_at IS NULL
   FOR SHARE`, objectID.String(), userID.String()).Scan(&permissions); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	effective, err := resolveAI(ctx, tx, userID, profileID, &objectID, permissions)
	if err != nil {
		return nil, err
	}
	effective.ObjectVersion = version
	return effective, nil
}

// ResolveGlobalAI resolves the global delegation set for an unbound private
// conversation. There is no object membership to intersect.
func (s *Service) ResolveGlobalAI(ctx context.Context, userID, profileID uuid.UUID) (*Effective, error) {
	return resolveAI(ctx, s.db, userID, profileID, nil, nil)
}

// ResolveGlobalAITx revalidates the same decision inside the message
// transaction.
func (s *Service) ResolveGlobalAITx(ctx context.Context, tx pgx.Tx, userID, profileID uuid.UUID) (*Effective, error) {
	return resolveAI(ctx, tx, userID, profileID, nil, nil)
}

func resolveAI(ctx context.Context, q queryer, userID, profileID uuid.UUID, objectID *uuid.UUID, memberPermissions []string) (*Effective, error) {
	if objectID != nil {
		if len(memberPermissions) == 0 {
			var permissions []string
			if err := q.QueryRow(ctx, `
SELECT m.permissions
  FROM object_memberships m
  JOIN living_objects o ON o.id = m.object_id
 WHERE m.object_id = $1 AND m.user_id = $2
   AND m.revoked_at IS NULL AND o.status = 'active'`,
				objectID.String(), userID.String()).Scan(&permissions); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, ErrNotFound
				}
				return nil, err
			}
			memberPermissions = permissions
		}
		if !contains(memberPermissions, objects.PermRead) {
			return nil, ErrNotFound
		}
	}

	var objectArg any
	if objectID != nil {
		objectArg = objectID.String()
	}
	rows, err := q.Query(ctx, `
SELECT id, object_id, permissions, constraints
  FROM delegation_grants
 WHERE user_id = $1
   AND ai_profile_id = $2
   AND revoked_at IS NULL
   AND (expires_at IS NULL OR expires_at > now())
   AND (($3::uuid IS NOT NULL AND (object_id IS NULL OR object_id = $3))
        OR ($3::uuid IS NULL AND object_id IS NULL))
 ORDER BY (object_id IS NOT NULL) DESC, created_at DESC, id DESC
   FOR SHARE`,
		userID.String(), profileID.String(), objectArg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	effective := &Effective{
		UserID: userID, AIProfileID: profileID,
		RequireConfirmation: map[string]bool{},
		MaxAIActionsPerHour: defaultMaxAIActionsPerHour,
	}
	merged := make(map[string]bool)
	var mergedConstraints Constraints
	found := false
	for rows.Next() {
		var (
			id          string
			objectScan  *string
			permissions []string
			constraints []byte
		)
		if err := rows.Scan(&id, &objectScan, &permissions, &constraints); err != nil {
			return nil, err
		}
		delegationID, err := uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		if !found {
			effective.DelegationID = delegationID
			found = true
		}
		effective.DelegationIDs = append(effective.DelegationIDs, delegationID)
		for _, permission := range permissions {
			switch permission {
			case objects.PermRead, objects.PermContribute, objects.PermDerive:
				merged[permission] = true
			}
		}
		parsed, err := ParseConstraints(constraints)
		if err != nil {
			return nil, err
		}
		if err := mergeConstraints(&mergedConstraints, parsed); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNoActiveDelegation
	}
	if objectID != nil {
		for permission := range merged {
			if !contains(memberPermissions, permission) {
				delete(merged, permission)
			}
		}
	}
	if !merged[objects.PermRead] {
		return nil, ErrForbidden
	}
	effective.Permissions = orderedAIPermissions(merged)
	for _, action := range mergedConstraints.RequireConfirmation {
		effective.RequireConfirmation[action] = true
	}
	effective.RequireConfirmation[ConfirmationDeriveCreate] = true
	effective.RequireConfirmation[ConfirmationCapabilityInvoke] = true
	// World creation is structurally confirmation-only for the unbound AI.
	// Recording it here keeps the effective decision honest for any caller.
	effective.RequireConfirmation[ConfirmationWorldCreate] = true
	effective.MaxAIActionsPerHour = mergedConstraints.MaxAIActionsPerHour
	return effective, nil
}

func orderedAIPermissions(present map[string]bool) []string {
	order := []string{objects.PermRead, objects.PermContribute, objects.PermDerive}
	out := make([]string, 0, len(order))
	for _, permission := range order {
		if present[permission] {
			out = append(out, permission)
		}
	}
	return out
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// ActiveDelegationTx reports whether a specific delegation still covers the
// object for this user/profile, and holds a share lock on the grant until the
// caller's transaction ends. Used during confirmation approval and proposal
// apply so a revocation can neither be masked by a different active delegation
// nor commit between revalidation and the authorized write.
func (s *Service) ActiveDelegationTx(ctx context.Context, tx pgx.Tx, delegationID, userID, profileID uuid.UUID, objectID *uuid.UUID) (bool, error) {
	var objectArg any
	if objectID != nil {
		objectArg = objectID.String()
	}
	var id string
	err := tx.QueryRow(ctx, `
SELECT id
  FROM delegation_grants
 WHERE id = $1 AND user_id = $2 AND ai_profile_id = $3
   AND revoked_at IS NULL
   AND (expires_at IS NULL OR expires_at > now())
   AND (object_id IS NULL OR object_id = $4)
   FOR SHARE`, delegationID.String(), userID.String(), profileID.String(), objectArg).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ProposalApplyAllowedTx is the execution-time proposal check. It requires
// both the exact delegation recorded on the proposal (so a revocation cannot be
// masked by a different active grant) and the actor's current AI effective
// permissions (so a proposal created when a different grant supplied
// contribute cannot outlive that grant). Both checks run in the caller's
// transaction; ActiveDelegationTx holds a share lock on the recorded grant.
func (s *Service) ProposalApplyAllowedTx(ctx context.Context, tx pgx.Tx, delegationID, userID, profileID, objectID uuid.UUID, permission string) (bool, error) {
	active, err := s.ActiveDelegationTx(ctx, tx, delegationID, userID, profileID, &objectID)
	if err != nil || !active {
		return false, err
	}
	effective, err := s.ResolveAITx(ctx, tx, userID, profileID, objectID)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) ||
			errors.Is(err, ErrNoActiveDelegation) {
			return false, nil
		}
		return false, err
	}
	return effective.Can(permission), nil
}

// LockAIActionBudgetTx serializes action-budget accounting for one user. It is
// a transaction-scoped advisory lock; concurrent AI turns on different
// conversations cannot both consume the last remaining action slot.
func (s *Service) LockAIActionBudgetTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
SELECT pg_advisory_xact_lock(hashtextextended('derive-world:ai-actions:' || $1, 0))`,
		userID.String())
	return err
}

// RemainingAIActionsTx returns how many AI-originated proposals and
// confirmations the profile may still create in the current hour. The count is
// performed in the execution transaction so a single turn cannot exceed the
// effective delegation constraint.
func (s *Service) RemainingAIActionsTx(ctx context.Context, tx pgx.Tx, userID, profileID uuid.UUID, limit int) (int, error) {
	if limit < 1 {
		return 0, nil
	}
	var used int
	err := tx.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM object_proposals
    WHERE actor_user_id = $1 AND ai_profile_id = $2
      AND created_at >= now() - interval '1 hour')
+ (SELECT count(*) FROM confirmations
    WHERE user_id = $1
      AND created_at >= now() - interval '1 hour')`,
		userID.String(), profileID.String()).Scan(&used)
	if err != nil {
		return 0, err
	}
	remaining := limit - used
	if remaining < 0 {
		return 0, nil
	}
	return remaining, nil
}

// CanonicalPayload validates and canonicalizes a confirmation payload.
func CanonicalPayload(payload json.RawMessage) ([]byte, error) {
	if len(payload) == 0 || len(payload) > 16*1024 {
		return nil, ErrPayloadTooLarge
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: payload", ErrInvalidRequest)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// SortPermissions is a helper for deterministic audit metadata strings.
func SortPermissions(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// ReplayAIAllowed revalidates a cached Personal AI turn before its idempotent
// response is replayed (D05-20 #4). It returns ErrNotFound when the caller no
// longer has the membership/delegation needed for the original turn.
func (s *Service) ReplayAIAllowed(ctx context.Context, userID, profileID uuid.UUID, objectID *uuid.UUID, memberPermissions []string) error {
	var (
		effective *Effective
		err       error
	)
	if objectID == nil {
		effective, err = s.ResolveGlobalAI(ctx, userID, profileID)
	} else {
		effective, err = s.ResolveAI(ctx, userID, profileID, objectID, memberPermissions)
	}
	if err != nil {
		if errors.Is(err, ErrNoActiveDelegation) || errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if !effective.Can(objects.PermRead) {
		return ErrNotFound
	}
	return nil
}

// ConfirmationReplayAllowed revalidates the exact delegation and derive
// permission behind an approved confirmation before its cached response is
// replayed.
func (s *Service) ConfirmationReplayAllowed(ctx context.Context, userID, confirmationID uuid.UUID) error {
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
	decoded, err := DecodeDerivePayload(payload)
	if err != nil {
		return ErrNotFound
	}
	effective, err := s.ResolveAI(ctx, userID, decoded.AIProfileID, &decoded.ObjectID, nil)
	if err != nil {
		if errors.Is(err, ErrNoActiveDelegation) || errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if !effective.Can(objects.PermDerive) {
		return ErrNotFound
	}
	active := false
	for _, id := range effective.DelegationIDs {
		if id == decoded.DelegationID {
			active = true
			break
		}
	}
	if !active {
		return ErrNotFound
	}
	return nil
}
