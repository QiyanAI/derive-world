// SPDX-License-Identifier: Apache-2.0

// Package world builds projections for the World Stage and Living Pulse. The
// stage and pulse are read-only. User layout preferences are the only state
// this package writes; they are keyed by (user, object) and never alter object
// state, versions, ownership, membership, permissions, or derivation lineage.
package world

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrLayoutObjectNotFound hides an object that the caller cannot see.
var ErrLayoutObjectNotFound = errors.New("layout object not found")

// StageObject is one visible object on the stage.
type StageObject struct {
	ID                   string `json:"id"`
	ObjectType           string `json:"object_type"`
	PresentationTemplate string `json:"presentation_template"`
	Title                string `json:"title"`
	// Note, Purpose, DoneCount, and TotalCount are the minimal visible
	// projection needed to render a world node without exposing the whole
	// object state or issuing N object requests.
	Note        string      `json:"note"`
	Purpose     string      `json:"purpose,omitempty"`
	DoneCount   int         `json:"done_count"`
	TotalCount  int         `json:"total_count"`
	Version     int         `json:"version"`
	OwnerID     string      `json:"owner_id"`
	Permissions []string    `json:"permissions"`
	Role        string      `json:"role"`
	UpdatedAt   time.Time   `json:"updated_at"`
	MemberCount int         `json:"member_count"`
	BranchCount int         `json:"branch_count"`
	LatestEvent *StageEvent `json:"latest_event"`
}

// StageEvent is a safe latest-event projection.
type StageEvent struct {
	EventType  string    `json:"event_type"`
	Action     string    `json:"action,omitempty"`
	ActorType  string    `json:"actor_type"`
	ActorName  string    `json:"actor_name,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// StageEdge is a real derivation edge whose endpoints both remain visible.
type StageEdge struct {
	ParentID string `json:"parent_id"`
	ChildID  string `json:"child_id"`
}

// Stage is the World Stage projection.
type Stage struct {
	Objects     []StageObject `json:"objects"`
	Edges       []StageEdge   `json:"edges"`
	GeneratedAt time.Time     `json:"generated_at"`
}

// PulseItem is one real change. It is never synthesized.
type PulseItem struct {
	Kind       string    `json:"kind"`
	ObjectID   string    `json:"object_id"`
	Title      string    `json:"title"`
	EventType  string    `json:"event_type,omitempty"`
	Action     string    `json:"action,omitempty"`
	ActorType  string    `json:"actor_type,omitempty"`
	ActorName  string    `json:"actor_name,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Service builds the projections.
type Service struct {
	db *pgxpool.Pool
}

// NewService assembles the projection service.
func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// Stage returns visible objects, visible derivation edges, and the latest
// visible event for each object.
func (s *Service) Stage(ctx context.Context, userID uuid.UUID) (*Stage, error) {
	rows, err := s.db.Query(ctx, `
SELECT o.id, o.object_type, o.presentation_template, COALESCE(o.state->>'title',''),
       COALESCE(NULLIF(o.state->>'premise',''), NULLIF(o.state->>'summary',''),
                NULLIF(o.state->>'prompt',''), '') AS note,
       COALESCE(NULLIF(o.state->>'purpose',''),
                CASE WHEN o.object_type = 'open_world' THEN 'open' ELSE '' END) AS purpose,
       (SELECT count(*)
          FROM jsonb_array_elements(
                 CASE WHEN jsonb_typeof(o.state->'elements') = 'array'
                      THEN o.state->'elements' ELSE '[]'::jsonb END) AS element
         WHERE element->>'status' = 'done')
       + (SELECT count(*)
            FROM jsonb_array_elements(
                   CASE WHEN jsonb_typeof(o.state->'checklist') = 'array'
                        THEN o.state->'checklist' ELSE '[]'::jsonb END) AS item
           WHERE item->>'done' = 'true')
       + (SELECT count(*)
            FROM jsonb_array_elements(
                   CASE WHEN jsonb_typeof(o.state->'nodes') = 'array'
                        THEN o.state->'nodes' ELSE '[]'::jsonb END) AS scene_node
           WHERE scene_node->>'status' = 'done') AS done_count,
       (CASE WHEN jsonb_typeof(o.state->'elements') = 'array'
             THEN jsonb_array_length(o.state->'elements') ELSE 0 END)
       + (CASE WHEN jsonb_typeof(o.state->'checklist') = 'array'
               THEN jsonb_array_length(o.state->'checklist') ELSE 0 END)
       + (CASE WHEN jsonb_typeof(o.state->'nodes') = 'array'
               THEN jsonb_array_length(o.state->'nodes') ELSE 0 END) AS total_count,
       o.version, o.owner_id, m.permissions, o.updated_at,
       (SELECT count(*) FROM object_memberships mm
         WHERE mm.object_id = o.id AND mm.revoked_at IS NULL) AS member_count,
       (SELECT count(*)
          FROM derivation_edges d
          JOIN object_memberships cm
            ON cm.object_id = d.child_object_id
           AND cm.user_id = $1
           AND cm.revoked_at IS NULL
         WHERE d.parent_object_id = o.id) AS branch_count,
       e.event_type, e.actor_type, e.created_at, COALESCE(e.payload->>'action',''),
       COALESCE(actor.display_name,'')
  FROM living_objects o
  JOIN object_memberships m
    ON m.object_id = o.id AND m.user_id = $1 AND m.revoked_at IS NULL
  LEFT JOIN LATERAL (
        SELECT event_type, actor_type, actor_user_id, created_at, payload
          FROM object_events
         WHERE object_id = o.id
           AND (event_type <> 'ai.proposal_created' OR actor_user_id = $1)
           AND (event_type NOT IN ('object.invitation_created','object.invitation_revoked')
                OR m.permissions && ARRAY['invite','manage']::text[])
         ORDER BY seq DESC
         LIMIT 1
       ) e ON true
  LEFT JOIN users actor ON actor.id = e.actor_user_id
 WHERE o.status = 'active'
 ORDER BY o.updated_at DESC, o.id DESC
 LIMIT 100`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	objects := make([]StageObject, 0, 32)
	ids := make([]string, 0, 32)
	for rows.Next() {
		var (
			object      StageObject
			id          string
			ownerID     string
			permissions []string
			eventType   *string
			actorType   *string
			eventAt     *time.Time
			action      *string
			actorName   *string
		)
		if err := rows.Scan(&id, &object.ObjectType, &object.PresentationTemplate,
			&object.Title, &object.Note, &object.Purpose, &object.DoneCount, &object.TotalCount,
			&object.Version, &ownerID, &permissions, &object.UpdatedAt,
			&object.MemberCount, &object.BranchCount, &eventType, &actorType, &eventAt,
			&action, &actorName); err != nil {
			return nil, err
		}
		object.ID = id
		object.OwnerID = ownerID
		object.Permissions = permissions
		object.Role = roleFromPermissions(permissions)
		if eventType != nil {
			object.LatestEvent = &StageEvent{EventType: *eventType, ActorType: *actorType}
			if action != nil {
				object.LatestEvent.Action = *action
			}
			if actorName != nil {
				object.LatestEvent.ActorName = *actorName
			}
			if eventAt != nil {
				object.LatestEvent.OccurredAt = *eventAt
			}
		}
		objects = append(objects, object)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	edges := make([]StageEdge, 0)
	if len(ids) > 1 {
		edgeRows, err := s.db.Query(ctx, `
SELECT d.parent_object_id, d.child_object_id
  FROM derivation_edges d
  JOIN object_memberships pm
    ON pm.object_id = d.parent_object_id AND pm.user_id = $1 AND pm.revoked_at IS NULL
  JOIN object_memberships cm
    ON cm.object_id = d.child_object_id AND cm.user_id = $1 AND cm.revoked_at IS NULL
  JOIN living_objects po ON po.id = d.parent_object_id AND po.status = 'active'
  JOIN living_objects co ON co.id = d.child_object_id AND co.status = 'active'
 WHERE d.parent_object_id = ANY($2) AND d.child_object_id = ANY($2)
 LIMIT 200`, userID.String(), ids)
		if err != nil {
			return nil, err
		}
		defer edgeRows.Close()
		for edgeRows.Next() {
			var edge StageEdge
			if err := edgeRows.Scan(&edge.ParentID, &edge.ChildID); err != nil {
				return nil, err
			}
			edges = append(edges, edge)
		}
		if err := edgeRows.Err(); err != nil {
			return nil, err
		}
	}
	return &Stage{Objects: objects, Edges: edges, GeneratedAt: time.Now().UTC()}, nil
}

// PulseEvents returns recent visible object changes.
func (s *Service) PulseEvents(ctx context.Context, userID uuid.UUID, limit int) ([]PulseItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 30
	}
	rows, err := s.db.Query(ctx, `
SELECT e.object_id, COALESCE(o.state->>'title',''), e.event_type,
       COALESCE(e.payload->>'action',''), e.actor_type,
       COALESCE(u.display_name,''), e.created_at
  FROM object_events e
  JOIN living_objects o ON o.id = e.object_id AND o.status = 'active'
  JOIN object_memberships m
    ON m.object_id = e.object_id AND m.user_id = $1 AND m.revoked_at IS NULL
  LEFT JOIN users u ON u.id = e.actor_user_id
 WHERE e.event_type IN ('object.created','object.state_changed','object.derived',
                        'object.member_added','object.member_removed',
                        'ai.proposal_created','ai.proposal_applied')
   AND (e.event_type <> 'ai.proposal_created' OR e.actor_user_id = $1)
   AND (e.event_type NOT IN ('object.member_added','object.member_removed')
        OR m.permissions && ARRAY['invite','manage']::text[])
 ORDER BY e.seq DESC
 LIMIT $2`, userID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PulseItem, 0, limit)
	for rows.Next() {
		var item PulseItem
		if err := rows.Scan(&item.ObjectID, &item.Title, &item.EventType,
			&item.Action, &item.ActorType, &item.ActorName, &item.OccurredAt); err != nil {
			return nil, err
		}
		item.Kind = "object"
		item.Summary = summarize(item.EventType, item.Action)
		items = append(items, item)
	}
	return items, rows.Err()
}

func summarize(eventType, action string) string {
	switch eventType {
	case "object.created":
		return "created"
	case "object.derived":
		return "derived"
	case "object.state_changed":
		if action == "" {
			return "changed"
		}
		return action
	case "ai.proposal_created":
		return "proposal"
	case "ai.proposal_applied":
		return "proposal_applied"
	default:
		return eventType
	}
}

// LayoutItem is one user-private position on the spatial World Stage.
type LayoutItem struct {
	ObjectID  string    `json:"object_id"`
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Layout is the caller's private world-view projection.
type Layout struct {
	Items       []LayoutItem `json:"items"`
	GeneratedAt time.Time    `json:"generated_at"`
}

// Layout returns only positions for objects the caller can currently read.
// Stale positions for revoked memberships are never returned.
func (s *Service) Layout(ctx context.Context, userID uuid.UUID) (*Layout, error) {
	rows, err := s.db.Query(ctx, `
SELECT l.object_id, l.x, l.y, l.updated_at
  FROM user_world_layouts l
  JOIN living_objects o ON o.id = l.object_id AND o.status = 'active'
  JOIN object_memberships m
    ON m.object_id = o.id AND m.user_id = l.user_id AND m.revoked_at IS NULL
 WHERE l.user_id = $1
 ORDER BY l.updated_at DESC, l.object_id
 LIMIT 500`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]LayoutItem, 0, 32)
	for rows.Next() {
		var item LayoutItem
		if err := rows.Scan(&item.ObjectID, &item.X, &item.Y, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &Layout{Items: items, GeneratedAt: time.Now().UTC()}, nil
}

// SaveLayout upserts one user-private node position. The INSERT ... SELECT
// makes visibility a precondition: a caller cannot create a layout row for an
// object it cannot read.
func (s *Service) SaveLayout(ctx context.Context, userID, objectID uuid.UUID,
	x, y float64) (*LayoutItem, error) {
	var item LayoutItem
	err := s.db.QueryRow(ctx, `
INSERT INTO user_world_layouts (user_id, object_id, x, y)
SELECT $1, o.id, $3, $4
  FROM living_objects o
  JOIN object_memberships m
    ON m.object_id = o.id AND m.user_id = $1 AND m.revoked_at IS NULL
 WHERE o.id = $2 AND o.status = 'active'
ON CONFLICT (user_id, object_id)
DO UPDATE SET x = EXCLUDED.x, y = EXCLUDED.y, updated_at = now()
RETURNING object_id, x, y, updated_at`,
		userID.String(), objectID.String(), x, y).
		Scan(&item.ObjectID, &item.X, &item.Y, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLayoutObjectNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func roleFromPermissions(permissions []string) string {
	has := func(value string) bool {
		for _, permission := range permissions {
			if permission == value {
				return true
			}
		}
		return false
	}
	switch {
	case has("manage"):
		return "owner"
	case has("contribute") && has("derive"):
		return "editor"
	case len(permissions) == 1 && has("read"):
		return "viewer"
	default:
		return "custom"
	}
}
