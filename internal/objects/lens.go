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
)

// LensVersion is a read-only historical marker. It intentionally carries no
// state payload; the state is only returned by StateAtVersionForActor after a
// fresh authorization check.
type LensVersion struct {
	Version   int        `json:"version"`
	EventType string     `json:"event_type"`
	Action    string     `json:"action,omitempty"`
	Actor     EventActor `json:"actor"`
	CreatedAt time.Time  `json:"created_at"`
}

// BranchChild is one directly derived child that the caller may currently
// read. Unreadable children are omitted rather than counted.
type BranchChild struct {
	ObjectID      string    `json:"object_id"`
	Title         string    `json:"title"`
	Version       int       `json:"version"`
	SourceVersion int       `json:"source_version"`
	CreatedAt     time.Time `json:"created_at"`
}

// Versions returns the real version history visible to an authorized reader.
func (s *Service) Versions(ctx context.Context, actorID, objectID uuid.UUID) ([]LensVersion, error) {
	object, err := s.LoadForActor(ctx, actorID, objectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
SELECT e.object_version, e.event_type, COALESCE(e.payload->>'action',''),
       e.actor_type, e.actor_user_id, e.created_at, u.display_name
  FROM object_events e
  LEFT JOIN users u ON u.id = e.actor_user_id
 WHERE e.object_id = $1
   AND e.event_type IN ('object.created','object.derived','object.state_changed')
 ORDER BY e.seq ASC`, object.ID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := make([]LensVersion, 0, object.Version)
	seen := make(map[int]bool, object.Version)
	for rows.Next() {
		var (
			version     int
			eventType   string
			action      string
			actorType   string
			actorUserID *string
			createdAt   time.Time
			displayName *string
		)
		if err := rows.Scan(&version, &eventType, &action, &actorType,
			&actorUserID, &createdAt, &displayName); err != nil {
			return nil, err
		}
		if seen[version] {
			continue
		}
		seen[version] = true
		entry := LensVersion{
			Version:   version,
			EventType: eventType,
			Action:    action,
			Actor:     EventActor{Type: actorType},
			CreatedAt: createdAt,
		}
		if actorUserID != nil {
			entry.Actor.UserID = *actorUserID
		}
		if displayName != nil {
			entry.Actor.DisplayName = *displayName
		}
		versions = append(versions, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return versions, nil
}

// StateAtVersionForActor returns a verified historical snapshot only after
// confirming the caller still holds read permission on the object.
func (s *Service) StateAtVersionForActor(ctx context.Context, actorID, objectID uuid.UUID, version int) (int, string, json.RawMessage, error) {
	object, err := s.LoadForActor(ctx, actorID, objectID)
	if err != nil {
		return 0, "", nil, err
	}
	if version < 1 || version > object.Version {
		return 0, "", nil, fmt.Errorf("%w: version", ErrInvalidRequest)
	}
	if version == object.Version {
		return object.Version, object.ObjectType, object.State, nil
	}
	state, err := stateAtVersion(ctx, s.db, objectID, version)
	if err != nil {
		return 0, "", nil, err
	}
	return object.Version, object.ObjectType, state, nil
}

// Children returns directly derived objects the caller can currently read.
// The query never reveals unreadable children, their count, or their titles.
func (s *Service) Children(ctx context.Context, actorID, objectID uuid.UUID) ([]BranchChild, error) {
	rows, err := s.db.Query(ctx, `
SELECT c.id, COALESCE(c.state->>'title',''), c.version, d.parent_version, d.created_at
  FROM derivation_edges d
  JOIN living_objects c ON c.id = d.child_object_id
  JOIN object_memberships m
    ON m.object_id = c.id
   AND m.user_id = $2
   AND m.revoked_at IS NULL
 WHERE d.parent_object_id = $1
   AND c.status = 'active'
 ORDER BY d.created_at DESC, c.id DESC`, objectID.String(), actorID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	children := make([]BranchChild, 0)
	for rows.Next() {
		var (
			child BranchChild
			id    string
		)
		if err := rows.Scan(&id, &child.Title, &child.Version, &child.SourceVersion, &child.CreatedAt); err != nil {
			return nil, err
		}
		child.ObjectID = id
		children = append(children, child)
	}
	return children, rows.Err()
}

func stateAtVersion(ctx context.Context, db queryer, objectID uuid.UUID, version int) (json.RawMessage, error) {
	var payload []byte
	var err error
	if version == 1 {
		err = db.QueryRow(ctx, `
SELECT payload->'state'
  FROM object_events
 WHERE object_id = $1
   AND event_type IN ('object.created','object.derived')
   AND object_version = 1
 ORDER BY seq ASC
 LIMIT 1`, objectID.String()).Scan(&payload)
	} else {
		err = db.QueryRow(ctx, `
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
