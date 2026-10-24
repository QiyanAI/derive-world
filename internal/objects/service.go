// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/store"
)

// DelegationRevalidator revalidates the authorization behind an AI-originated
// proposal. The Trust Kernel implements it; the composition root injects it
// through the constructor so package objects never imports package trust
// (D08 §3.2). A different active delegation must not stand in for the revoked
// original (D05-16), and the actor's current AI effective permission must still
// contain the permission the proposal exercises.
type DelegationRevalidator interface {
	ProposalApplyAllowedTx(ctx context.Context, tx pgx.Tx, delegationID, userID, profileID, objectID uuid.UUID, permission string) (bool, error)
}

// Service is the controlled write path for the object aggregate.
type Service struct {
	db          *pgxpool.Pool
	audit       *audit.Writer
	idem        *store.Idempotency
	delegations DelegationRevalidator
}

// NewService assembles the object service. The audit writer must fail closed:
// an audit error rolls the business transaction back.
func NewService(db *pgxpool.Pool, auditWriter *audit.Writer, idem *store.Idempotency, delegations DelegationRevalidator) *Service {
	return &Service{db: db, audit: auditWriter, idem: idem, delegations: delegations}
}

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const objectColumns = `
 o.id, o.object_type, o.schema_version, o.creator_id, o.owner_id,
 o.state, o.version, o.shared_context, o.access_policy,
 o.presentation_template, o.status, o.created_at, o.updated_at, o.deleted_at`

// Event payload limits. D04-21 froze every event at 96 KiB. C-G04-04 widens
// only object.state_changed, which must carry both the action input and the
// full state_after snapshot: 132,352 bytes is the frozen component upper
// bound (65,536 state + 65,792 proposal-input storage + 1,024 structural).
// The largest legal payload reachable through a whitelisted action is
// 131,064 jsonb-text bytes. This Go check must stay equal to the
// object_events_payload_size CHECK in migration 00005.
const (
	maxEventPayloadBytes             = 96 * 1024
	maxStateChangedEventPayloadBytes = 132352
)

func eventPayloadLimit(eventType string) int {
	if eventType == "object.state_changed" {
		return maxStateChangedEventPayloadBytes
	}
	return maxEventPayloadBytes
}

func exceedsEventPayloadLimit(eventType string, size int) bool {
	return size > eventPayloadLimit(eventType)
}

// LoadForActor is the single read entry point for object authorization. A
// missing object, a soft-deleted object, and a caller without an active
// membership are indistinguishable and all return ErrNotFound.
func (s *Service) LoadForActor(ctx context.Context, actorID, objectID uuid.UUID) (*Object, error) {
	return loadForActor(ctx, s.db, actorID, objectID)
}

func loadForActor(ctx context.Context, db queryer, actorID, objectID uuid.UUID) (*Object, error) {
	row := db.QueryRow(ctx, `
SELECT `+objectColumns+`, m.permissions
  FROM living_objects o
  JOIN object_memberships m
    ON m.object_id = o.id
   AND m.user_id = $2
   AND m.revoked_at IS NULL
 WHERE o.id = $1
   AND o.status = 'active'`, objectID.String(), actorID.String())
	object, err := scanObject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return object, nil
}

func scanObject(row pgx.Row) (*Object, error) {
	var (
		object       Object
		id           string
		creatorID    string
		ownerID      string
		state        []byte
		shared       []byte
		accessPolicy []byte
		deletedAt    *time.Time
		permissions  []string
	)
	err := row.Scan(
		&id, &object.ObjectType, &object.SchemaVersion, &creatorID, &ownerID,
		&state, &object.Version, &shared, &accessPolicy,
		&object.PresentationTemplate, &object.Status, &object.CreatedAt,
		&object.UpdatedAt, &deletedAt, &permissions,
	)
	if err != nil {
		return nil, err
	}
	object.ID = uuid.MustParse(id)
	object.CreatorID = uuid.MustParse(creatorID)
	object.OwnerID = uuid.MustParse(ownerID)
	object.State = state
	object.SharedContext = shared
	object.AccessPolicy = accessPolicy
	object.DeletedAt = deletedAt
	object.Permissions = permissions
	return &object, nil
}

// Get returns the object view for an authorized caller, including the lineage
// projection when the object was derived and the caller can still read the
// parent. An unreadable parent is omitted rather than leaked as metadata.
func (s *Service) Get(ctx context.Context, actorID, objectID uuid.UUID) (*ObjectView, error) {
	object, err := s.LoadForActor(ctx, actorID, objectID)
	if err != nil {
		return nil, err
	}
	view := object.View()
	lineage, err := s.LineageForObject(ctx, actorID, objectID)
	if err != nil {
		return nil, err
	}
	view.Lineage = lineage
	return &view, nil
}

// LineageForObject returns the derivation projection only when the actor can
// currently read the parent object. It is the only lineage-by-id read path.
func (s *Service) LineageForObject(ctx context.Context, actorID, objectID uuid.UUID) (*DeriveLineage, error) {
	var (
		parentID string
		version  int
		fields   []string
		readable bool
	)
	err := s.db.QueryRow(ctx, `
SELECT d.parent_object_id, d.parent_version, d.inherited_fields,
       EXISTS (
         SELECT 1
           FROM object_memberships m
           JOIN living_objects o ON o.id = m.object_id
          WHERE m.object_id = d.parent_object_id
            AND m.user_id = $2
            AND m.revoked_at IS NULL
            AND o.status = 'active'
       ) AS readable
  FROM derivation_edges d
 WHERE d.child_object_id = $1`, objectID.String(), actorID.String()).Scan(
		&parentID, &version, &fields, &readable)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !readable {
		return nil, nil
	}
	return &DeriveLineage{
		Parent:          DeriveParent{ObjectID: parentID, Version: version},
		InheritedFields: fields,
	}, nil
}

// List returns object cards visible to the actor. scope is owned, shared, or
// all; the caller validates the value.
func (s *Service) List(ctx context.Context, actorID uuid.UUID, scope string, cursor *ListCursor, limit int) ([]ListItem, *ListCursor, error) {
	query := `
SELECT o.id, o.object_type, COALESCE(o.state->>'title',''), o.version, o.owner_id,
       m.permissions, o.created_at, o.updated_at
  FROM living_objects o
  JOIN object_memberships m
    ON m.object_id = o.id
   AND m.user_id = $1
   AND m.revoked_at IS NULL
 WHERE o.status = 'active'`
	args := []any{actorID.String()}
	switch scope {
	case "owned":
		query += " AND o.owner_id = $1"
	case "shared":
		query += " AND o.owner_id <> $1"
	case "all":
	default:
		return nil, nil, fmt.Errorf("%w: scope", ErrInvalidRequest)
	}
	if cursor != nil {
		query += fmt.Sprintf(" AND (o.created_at, o.id) < ($%d::timestamptz, $%d::uuid)", len(args)+1, len(args)+2)
		args = append(args, cursor.CreatedAt, cursor.ID.String())
	}
	query += fmt.Sprintf(" ORDER BY o.created_at DESC, o.id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	items := make([]ListItem, 0, limit)
	var lastCreated time.Time
	var lastID uuid.UUID
	count := 0
	for rows.Next() {
		var (
			item        ListItem
			id          string
			ownerID     string
			permissions []string
		)
		if err := rows.Scan(&id, &item.ObjectType, &item.Title, &item.Version,
			&ownerID, &permissions, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, nil, err
		}
		item.ID = id
		item.OwnerID = ownerID
		item.Permissions = NormalizePermissions(permissions)
		item.Role = RoleFromPermissions(permissions)
		count++
		if count <= limit {
			lastCreated = item.CreatedAt
			lastID = uuid.MustParse(item.ID)
			items = append(items, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if count > limit {
		return items, &ListCursor{CreatedAt: lastCreated, ID: lastID}, nil
	}
	return items, nil, nil
}

// DerivationEdge is the source-of-truth projection used by the Capability
// Broker's post-execution check.
type DerivationEdge struct {
	ParentObjectID  uuid.UUID
	ParentVersion   int
	InheritedFields []string
}

// DerivationEdgeForValidation reads one derivation edge. It does not
// authorize; the caller has already established that the actor owns the child.
func (s *Service) DerivationEdgeForValidation(ctx context.Context, childID uuid.UUID) (*DerivationEdge, error) {
	var (
		parentID string
		edge     DerivationEdge
	)
	err := s.db.QueryRow(ctx, `
SELECT parent_object_id, parent_version, inherited_fields
  FROM derivation_edges WHERE child_object_id = $1`, childID.String()).Scan(
		&parentID, &edge.ParentVersion, &edge.InheritedFields)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	edge.ParentObjectID = uuid.MustParse(parentID)
	return &edge, nil
}

// LoadForDerive locks a parent row and loads the actor's membership in one
// controlled entry point. It is the only by-id read path exposed to derive.
func (s *Service) LoadForDerive(ctx context.Context, tx pgx.Tx, actorID, objectID uuid.UUID) (*Object, []string, error) {
	object, err := loadForShare(ctx, tx, objectID)
	if err != nil {
		return nil, nil, err
	}
	permissions, err := membershipPermissions(ctx, tx, actorID, objectID)
	if err != nil {
		return nil, nil, err
	}
	return object, permissions, nil
}

func loadForShare(ctx context.Context, tx pgx.Tx, objectID uuid.UUID) (*Object, error) {
	row := tx.QueryRow(ctx, `
SELECT `+objectColumns+`
  FROM living_objects o
 WHERE o.id = $1
   FOR SHARE`, objectID.String())
	var (
		object       Object
		id           string
		creatorID    string
		ownerID      string
		state        []byte
		shared       []byte
		accessPolicy []byte
		deletedAt    *time.Time
	)
	err := row.Scan(
		&id, &object.ObjectType, &object.SchemaVersion, &creatorID, &ownerID,
		&state, &object.Version, &shared, &accessPolicy,
		&object.PresentationTemplate, &object.Status, &object.CreatedAt,
		&object.UpdatedAt, &deletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	object.ID = uuid.MustParse(id)
	object.CreatorID = uuid.MustParse(creatorID)
	object.OwnerID = uuid.MustParse(ownerID)
	object.State = state
	object.SharedContext = shared
	object.AccessPolicy = accessPolicy
	object.DeletedAt = deletedAt
	return &object, nil
}

// Create inserts a new object, the owner membership, the creation event, and
// the audit row in one transaction, then completes the idempotency record.
func (s *Service) Create(ctx context.Context, actorID uuid.UUID, input CreateInput, route, idemKey string, leaseUntil time.Time, requestID string, ipHash []byte) (*ObjectView, error) {
	state, shared, err := ValidateCreate(input)
	if err != nil {
		return nil, err
	}
	template := TemplateCollaborative
	if input.ObjectType == TypeInteractive {
		template = TemplateInteractive
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	objectID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	var createdAt, updatedAt time.Time
	err = tx.QueryRow(ctx, `
INSERT INTO living_objects (
    id, object_type, schema_version, creator_id, owner_id, state, version,
    shared_context, access_policy, presentation_template, status
 ) VALUES ($1,$2,1,$3,$3,$4::jsonb,1,$5::jsonb,$6::jsonb,$7,'active')
RETURNING created_at, updated_at`,
		objectID.String(), input.ObjectType, actorID.String(), string(state), string(shared),
		string(defaultAccessPolicy()), template,
	).Scan(&createdAt, &updatedAt)
	if err != nil {
		return nil, mapConstraintError(err)
	}

	permissions := OwnerPermissions()
	if _, err := tx.Exec(ctx, `
INSERT INTO object_memberships (object_id, user_id, permissions, granted_by)
VALUES ($1,$2,$3,$2)`, objectID.String(), actorID.String(), permissions); err != nil {
		return nil, err
	}

	eventPayload := map[string]any{
		"state":                 json.RawMessage(state),
		"object_type":           input.ObjectType,
		"presentation_template": template,
	}
	if err := appendEvent(ctx, tx, actorID, objectID, 1, "object.created", eventPayload); err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(actorID),
		Action:       audit.ActionObjectCreate,
		ResourceType: "living_object",
		ResourceID:   &objectID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata: map[string]any{
			"object_type":           input.ObjectType,
			"presentation_template": template,
		},
	}); err != nil {
		return nil, err
	}

	view := ObjectView{
		ID:                   objectID.String(),
		ObjectType:           input.ObjectType,
		PresentationTemplate: template,
		SchemaVersion:        1,
		CreatorID:            actorID.String(),
		OwnerID:              actorID.String(),
		Version:              1,
		State:                state,
		SharedContext:        shared,
		Status:               StatusActive,
		Permissions:          permissions,
		Role:                 "owner",
		CreatedAt:            createdAt,
		UpdatedAt:            updatedAt,
	}
	body, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	if err := s.idem.Complete(ctx, tx, actorID, route, idemKey, leaseUntil, 201, body); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &view, nil
}

// ApplyAction executes one whitelisted state action under a row lock and a
// version condition. The caller has already loaded the object, authorized the
// action's permission, validated the action, and claimed the idempotency key.
func (s *Service) ApplyAction(ctx context.Context, actorID, objectID uuid.UUID, expectedVersion int, plan *ActionPlan, route, idemKey string, leaseUntil time.Time, requestID string, ipHash []byte) (*ObjectView, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		scannedID     string
		objectType    string
		stateRaw      []byte
		sharedRaw     []byte
		current       int
		status        string
		createdAt     time.Time
		updatedAt     time.Time
		creatorID     string
		ownerID       string
		template      string
		schemaVersion int
		accessPolicy  []byte
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
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != StatusActive {
		return nil, ErrNotFound
	}

	permissions, err := membershipPermissions(ctx, tx, actorID, objectID)
	if err != nil {
		return nil, err
	}
	if !HasPermission(permissions, plan.Permission) {
		return nil, ErrForbidden
	}
	if current != expectedVersion {
		return nil, ErrVersionConflict
	}

	mutation, err := applyStateMutationTx(ctx, tx, actorID, objectID, objectType,
		current, stateRaw, sharedRaw, plan)
	if err != nil {
		return nil, err
	}
	newState := mutation.NewState
	newShared := mutation.NewShared
	newVersion := mutation.NewVersion
	voteOption := mutation.VoteOption
	oldVoteOption := mutation.PreviousVoteOption

	eventPayload := map[string]any{
		"action":      plan.Action,
		"input":       plan.Input,
		"state_after": json.RawMessage(newState),
	}
	if plan.Action == "set_shared_context" {
		eventPayload["shared_context_changed"] = true
	}
	if err := appendEvent(ctx, tx, actorID, objectID, newVersion, "object.state_changed", eventPayload); err != nil {
		return nil, err
	}
	auditMetadata := map[string]any{
		"action":       plan.Action,
		"from_version": current,
		"to_version":   newVersion,
	}
	if plan.Action == "cast_vote" {
		auditMetadata["option_id"] = voteOption
		if oldVoteOption == "" {
			auditMetadata["previous_option_id"] = nil
		} else {
			auditMetadata["previous_option_id"] = oldVoteOption
		}
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(actorID),
		Action:       audit.ActionObjectAction,
		ResourceType: "living_object",
		ResourceID:   &objectID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     auditMetadata,
	}); err != nil {
		return nil, err
	}

	var newCreated, newUpdated time.Time
	if err := tx.QueryRow(ctx, `SELECT created_at, updated_at FROM living_objects WHERE id = $1`,
		objectID.String()).Scan(&newCreated, &newUpdated); err != nil {
		return nil, err
	}
	view := ObjectView{
		ID:                   objectID.String(),
		ObjectType:           objectType,
		PresentationTemplate: template,
		SchemaVersion:        schemaVersion,
		CreatorID:            creatorID,
		OwnerID:              ownerID,
		Version:              newVersion,
		State:                newState,
		SharedContext:        newShared,
		Status:               status,
		Permissions:          NormalizePermissions(permissions),
		Role:                 RoleFromPermissions(permissions),
		CreatedAt:            newCreated,
		UpdatedAt:            newUpdated,
	}
	body, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	if err := s.idem.Complete(ctx, tx, actorID, route, idemKey, leaseUntil, 200, body); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &view, nil
}

// Delete soft deletes an object. The version is unchanged and events remain.
func (s *Service) Delete(ctx context.Context, actorID, objectID uuid.UUID, requestID string, ipHash []byte) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		status  string
		version int
		ownerID string
	)
	err = tx.QueryRow(ctx, `
SELECT status, version, owner_id FROM living_objects WHERE id = $1 FOR UPDATE`,
		objectID.String()).Scan(&status, &version, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != StatusActive {
		return ErrNotFound
	}
	permissions, err := membershipPermissions(ctx, tx, actorID, objectID)
	if err != nil {
		return err
	}
	if !HasPermission(permissions, PermManage) || ownerID != actorID.String() {
		return ErrForbidden
	}

	var deletedAt time.Time
	err = tx.QueryRow(ctx, `
UPDATE living_objects
   SET status = 'deleted',
       deleted_at = GREATEST(now(), created_at),
       updated_at = GREATEST(now(), updated_at, created_at)
 WHERE id = $1 AND status = 'active'
RETURNING deleted_at`, objectID.String()).Scan(&deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, actorID, objectID, version, "object.deleted", map[string]any{
		"deleted_at": deletedAt,
	}); err != nil {
		return err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        audit.UserActor(actorID),
		Action:       audit.ActionObjectDelete,
		ResourceType: "living_object",
		ResourceID:   &objectID,
		Decision:     "allow",
		RequestID:    requestID,
		IPHash:       ipHash,
		Metadata:     map[string]any{"version": version},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Events returns the visibility-filtered timeline. cursor is the last seen
// seq (0 for the first page).
func (s *Service) Events(ctx context.Context, actorID, objectID uuid.UUID, permissions []string, cursor int64, limit int) ([]Event, *int64, error) {
	rows, err := s.db.Query(ctx, `
SELECT e.id, e.seq, e.object_version, e.event_type, e.actor_type,
       e.actor_user_id, e.payload, e.created_at, u.display_name
  FROM object_events e
  LEFT JOIN users u ON u.id = e.actor_user_id
 WHERE e.object_id = $1 AND e.seq > $2
 ORDER BY e.seq ASC
 LIMIT $3`, objectID.String(), cursor, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	items := make([]Event, 0, limit)
	count := 0
	var next *int64
	var lastKeptSeq int64
	for rows.Next() {
		var (
			event       Event
			id          string
			actorUserID *string
			payloadRaw  []byte
			displayName *string
		)
		if err := rows.Scan(&id, &event.Seq, &event.ObjectVersion, &event.EventType,
			&event.Actor.Type, &actorUserID, &payloadRaw, &event.CreatedAt, &displayName); err != nil {
			return nil, nil, err
		}
		count++
		if count > limit {
			continue
		}
		lastKeptSeq = event.Seq
		if !eventVisible(event.EventType, actorUserID, actorID, permissions) {
			continue
		}
		event.ID = id
		if actorUserID != nil {
			event.Actor.UserID = *actorUserID
		}
		if displayName != nil {
			event.Actor.DisplayName = *displayName
		}
		event.Payload = map[string]any{}
		if len(payloadRaw) > 0 {
			_ = json.Unmarshal(payloadRaw, &event.Payload)
		}
		delete(event.Payload, "state_after")
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if count > limit {
		value := lastKeptSeq
		next = &value
	}
	return items, next, nil
}

func eventVisible(eventType string, actorUserID *string, viewer uuid.UUID, permissions []string) bool {
	switch eventType {
	case "object.invitation_created", "object.invitation_revoked":
		return HasPermission(permissions, PermInvite) || HasPermission(permissions, PermManage)
	case "ai.proposal_created":
		return actorUserID != nil && *actorUserID == viewer.String()
	default:
		return true
	}
}

// Members lists active memberships. Emails are never projected.
func (s *Service) Members(ctx context.Context, objectID uuid.UUID) ([]Member, error) {
	rows, err := s.db.Query(ctx, `
SELECT m.user_id, u.display_name, m.permissions, m.granted_by, m.created_at
  FROM object_memberships m
  JOIN users u ON u.id = m.user_id
 WHERE m.object_id = $1 AND m.revoked_at IS NULL
 ORDER BY m.created_at ASC, m.user_id ASC`, objectID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]Member, 0)
	for rows.Next() {
		var (
			member      Member
			userID      string
			permissions []string
			grantedBy   *string
		)
		if err := rows.Scan(&userID, &member.DisplayName, &permissions, &grantedBy, &member.CreatedAt); err != nil {
			return nil, err
		}
		member.UserID = userID
		member.Permissions = NormalizePermissions(permissions)
		member.Role = RoleFromPermissions(permissions)
		member.GrantedBy = grantedBy
		members = append(members, member)
	}
	return members, rows.Err()
}

// StateAtVersion reads the immutable state snapshot for historical derive.
func (s *Service) StateAtVersion(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, version int) (json.RawMessage, error) {
	var payload []byte
	var err error
	if version == 1 {
		err = tx.QueryRow(ctx, `
SELECT payload->'state'
  FROM object_events
 WHERE object_id = $1
   AND event_type IN ('object.created','object.derived')
   AND object_version = 1
 ORDER BY seq ASC
 LIMIT 1`, objectID.String()).Scan(&payload)
	} else {
		err = tx.QueryRow(ctx, `
SELECT payload->'state_after'
  FROM object_events
 WHERE object_id = $1
   AND event_type = 'object.state_changed'
   AND object_version = $2
 ORDER BY seq ASC
 LIMIT 1`, objectID.String(), version).Scan(&payload)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSnapshotMissing
	}
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, ErrSnapshotMissing
	}
	return payload, nil
}

// DerivedInput carries the already-authorized, already-assembled child state.
type DerivedInput struct {
	ChildID         uuid.UUID
	ParentID        uuid.UUID
	ParentType      string
	Template        string
	ActorID         uuid.UUID
	Actor           audit.Actor
	State           json.RawMessage
	SourceVersion   int
	InheritedFields []string
	Lineage         *DeriveLineage
	Route           string
	IdemKey         string
	LeaseUntil      time.Time
	RequestID       string
	IPHash          []byte
	SkipIdempotency bool
	ConfirmationID  *uuid.UUID
	ConfirmedBy     *uuid.UUID
}

// CreateDerived writes the child object, owner membership, derivation edge,
// child event, audit row, and idempotency completion inside the caller's
// transaction. It never writes the parent object.
func (s *Service) CreateDerived(ctx context.Context, tx pgx.Tx, input DerivedInput) (*ObjectView, error) {
	if err := ValidateStateBytes(input.ParentType, input.State); err != nil {
		return nil, err
	}
	var createdAt, updatedAt time.Time
	err := tx.QueryRow(ctx, `
INSERT INTO living_objects (
    id, object_type, schema_version, creator_id, owner_id, state, version,
    shared_context, access_policy, presentation_template, status
) VALUES ($1,$2,1,$3,$3,$4,1,'{}'::jsonb,$5,$6,'active')
RETURNING created_at, updated_at`,
		input.ChildID.String(), input.ParentType, input.ActorID.String(),
		input.State, defaultAccessPolicy(), input.Template,
	).Scan(&createdAt, &updatedAt)
	if err != nil {
		return nil, mapConstraintError(err)
	}
	permissions := OwnerPermissions()
	if _, err := tx.Exec(ctx, `
INSERT INTO object_memberships (object_id, user_id, permissions, granted_by)
VALUES ($1,$2,$3,$2)`, input.ChildID.String(), input.ActorID.String(), permissions); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO derivation_edges (
    child_object_id, parent_object_id, parent_version, inherited_fields, created_by
) VALUES ($1,$2,$3,$4,$5)`,
		input.ChildID.String(), input.ParentID.String(), input.SourceVersion,
		input.InheritedFields, input.ActorID.String()); err != nil {
		return nil, err
	}
	eventPayload := map[string]any{
		"parent_object_id": input.ParentID.String(),
		"parent_version":   input.SourceVersion,
		"inherited_fields": input.InheritedFields,
		"state":            json.RawMessage(input.State),
	}
	actor := input.Actor
	if actor.Type == "" {
		actor = audit.UserActor(input.ActorID)
	}
	if input.ConfirmationID != nil {
		eventPayload["confirmation_id"] = input.ConfirmationID.String()
	}
	if input.ConfirmedBy != nil {
		eventPayload["confirmed_by_user_id"] = input.ConfirmedBy.String()
	}
	if _, err := appendEventReturningID(ctx, tx, actor, input.ChildID, 1, "object.derived", eventPayload); err != nil {
		return nil, err
	}
	if err := s.audit.Write(ctx, tx, audit.Event{
		Actor:        actor,
		Action:       audit.ActionObjectDerive,
		ResourceType: "living_object",
		ResourceID:   &input.ChildID,
		Decision:     "allow",
		RequestID:    input.RequestID,
		IPHash:       input.IPHash,
		Metadata: map[string]any{
			"parent_object_id": input.ParentID.String(),
			"parent_version":   input.SourceVersion,
		},
	}); err != nil {
		return nil, err
	}
	view := ObjectView{
		ID:                   input.ChildID.String(),
		ObjectType:           input.ParentType,
		PresentationTemplate: input.Template,
		SchemaVersion:        1,
		CreatorID:            input.ActorID.String(),
		OwnerID:              input.ActorID.String(),
		Version:              1,
		State:                input.State,
		SharedContext:        json.RawMessage(`{}`),
		Status:               StatusActive,
		Permissions:          permissions,
		Role:                 "owner",
		CreatedAt:            createdAt,
		UpdatedAt:            updatedAt,
		Lineage:              input.Lineage,
	}
	if input.SkipIdempotency {
		return &view, nil
	}
	body, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	if err := s.idem.Complete(ctx, tx, input.ActorID, input.Route, input.IdemKey, input.LeaseUntil, 201, body); err != nil {
		return nil, err
	}
	return &view, nil
}

func membershipPermissions(ctx context.Context, tx pgx.Tx, actorID, objectID uuid.UUID) ([]string, error) {
	var permissions []string
	err := tx.QueryRow(ctx, `
SELECT permissions FROM object_memberships
 WHERE object_id = $1 AND user_id = $2 AND revoked_at IS NULL
   FOR SHARE`, objectID.String(), actorID.String()).Scan(&permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return permissions, nil
}

func appendEvent(ctx context.Context, tx pgx.Tx, actorID, objectID uuid.UUID, version int, eventType string, payload map[string]any) error {
	_, err := appendEventReturningID(ctx, tx, audit.UserActor(actorID), objectID, version, eventType, payload)
	return err
}

// appendEventReturningID writes one event with the full server-derived actor
// quadruple. Personal AI events carry the profile and delegation that caused
// them; human events carry exactly a user id.
func appendEventReturningID(
	ctx context.Context,
	tx pgx.Tx,
	actor audit.Actor,
	objectID uuid.UUID,
	version int,
	eventType string,
	payload map[string]any,
) (uuid.UUID, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, err
	}
	// Measure the jsonb text form; json.Marshal's HTML escaping must not make
	// a legal event payload look oversized. The database CHECK is final.
	size, err := store.CompactJSONSize(payload)
	if err != nil {
		return uuid.Nil, err
	}
	if exceedsEventPayloadLimit(eventType, size) {
		return uuid.Nil, fmt.Errorf("%w: event payload", ErrPayloadTooLarge)
	}
	eventID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO object_events (
    id, object_id, object_version, event_type, actor_type,
    actor_user_id, actor_ai_profile_id, delegation_id, payload
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`,
		eventID.String(), objectID.String(), version, eventType, actor.Type,
		nullableUUIDString(actor.UserID), nullableUUIDString(actor.AIProfileID),
		nullableUUIDString(actor.DelegationID), string(encoded))
	if err != nil {
		return uuid.Nil, mapConstraintError(err)
	}
	return eventID, nil
}

func nullableUUIDString(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return value.String()
}

// mapConstraintError converts frozen size/shape constraints into domain errors
// so a near-limit JSONB value never surfaces as an anonymous 500.
func mapConstraintError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// PostgreSQL jsonb cannot store U+0000; it rejects the value with
		// SQLSTATE 22P05. The input is not storable object state, so this is a
		// client error, not an internal failure. The transaction still rolls
		// back through the normal error path.
		if pgErr.Code == "22P05" {
			return fmt.Errorf("%w: unsupported Unicode escape sequence", ErrInvalidRequest)
		}
		if pgErr.Code == "23514" {
			switch pgErr.ConstraintName {
			case "living_objects_state_size", "living_objects_shared_context_size",
				"object_events_payload_size", "object_action_receipts_payload_size",
				"object_proposals_input_size":
				return fmt.Errorf("%w: %s", ErrPayloadTooLarge, pgErr.ConstraintName)
			case "living_objects_template_matches_type", "living_objects_access_policy_shape":
				return fmt.Errorf("%w: %s", ErrInvalidRequest, pgErr.ConstraintName)
			}
		}
	}
	return err
}

func defaultAccessPolicy() []byte {
	return []byte(`{"visibility":"private","invite_enabled":true,"derive_link_enabled":true}`)
}
