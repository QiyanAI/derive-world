// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// MaxLineageDepth is frozen by G00 contract §A.6.6.
const MaxLineageDepth = 10

// LineageResponse is the bounded lineage projection for one starting object.
type LineageResponse struct {
	Object      LineageRoot         `json:"object"`
	Ancestors   []LineageAncestor   `json:"ancestors"`
	Descendants []LineageDescendant `json:"descendants"`
	MaxDepth    int                 `json:"max_depth"`
	Truncated   bool                `json:"truncated"`
}

// LineageRoot identifies the starting object.
type LineageRoot struct {
	ObjectID string `json:"object_id"`
	Version  int    `json:"version"`
}

// LineageAncestor is either a readable ancestor or a redacted placeholder.
type LineageAncestor struct {
	ObjectID string `json:"object_id,omitempty"`
	Title    string `json:"title,omitempty"`
	Version  int    `json:"version,omitempty"`
	Depth    int    `json:"depth"`
	Redacted bool   `json:"redacted,omitempty"`
}

// LineageDescendant is a readable descendant node.
type LineageDescendant struct {
	ObjectID      string              `json:"object_id"`
	Title         string              `json:"title"`
	Version       int                 `json:"version"`
	Depth         int                 `json:"depth"`
	SourceVersion int                 `json:"source_version"`
	Children      []LineageDescendant `json:"children"`
}

type ancestorRow struct {
	ObjectID string
	Depth    int
	Title    string
	Version  int
	Status   string
	Readable bool
}

type descendantRow struct {
	ObjectID      string
	ParentID      string
	Depth         int
	SourceVersion int
	Title         string
	Version       int
	Status        string
	Readable      bool
}

// Lineage returns ancestors and descendants with privacy clipping. The caller
// has already established read access to the starting object.
func (s *Service) Lineage(ctx context.Context, actorID, objectID uuid.UUID, startVersion, depth int) (*LineageResponse, error) {
	if depth < 1 || depth > MaxLineageDepth {
		return nil, fmt.Errorf("%w: depth", ErrInvalidRequest)
	}
	response := &LineageResponse{
		Object:      LineageRoot{ObjectID: objectID.String(), Version: startVersion},
		Ancestors:   make([]LineageAncestor, 0),
		Descendants: make([]LineageDescendant, 0),
		MaxDepth:    depth,
	}

	ancestors, ancestorTruncated, err := s.ancestors(ctx, actorID, objectID, depth)
	if err != nil {
		return nil, err
	}
	for _, row := range ancestors {
		if row.Depth > depth {
			continue
		}
		if !row.Readable || row.Status != StatusActive {
			response.Ancestors = append(response.Ancestors, LineageAncestor{
				Depth: row.Depth, Redacted: true,
			})
			continue
		}
		response.Ancestors = append(response.Ancestors, LineageAncestor{
			ObjectID: row.ObjectID,
			Title:    row.Title,
			Version:  row.Version,
			Depth:    row.Depth,
		})
	}
	response.Truncated = response.Truncated || ancestorTruncated

	descendants, descendantTruncated, err := s.descendants(ctx, actorID, objectID, depth)
	if err != nil {
		return nil, err
	}
	response.Truncated = response.Truncated || descendantTruncated
	response.Descendants = buildDescendantTree(objectID.String(), descendants, depth)
	return response, nil
}

func (s *Service) ancestors(ctx context.Context, actorID, objectID uuid.UUID, depth int) ([]ancestorRow, bool, error) {
	rows, err := s.db.Query(ctx, `
WITH RECURSIVE anc AS (
    SELECT d.parent_object_id AS object_id, 1 AS depth
      FROM derivation_edges d
     WHERE d.child_object_id = $1
    UNION ALL
    SELECT d.parent_object_id, a.depth + 1
      FROM anc a
      JOIN derivation_edges d ON d.child_object_id = a.object_id
     WHERE a.depth < $3
)
SELECT a.object_id, a.depth, COALESCE(o.state->>'title',''), o.version, o.status,
       EXISTS (
         SELECT 1 FROM object_memberships m
          WHERE m.object_id = a.object_id
            AND m.user_id = $2
            AND m.revoked_at IS NULL
       ) AS readable
  FROM anc a
  JOIN living_objects o ON o.id = a.object_id
 ORDER BY a.depth ASC`, objectID.String(), actorID.String(), depth+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	result := make([]ancestorRow, 0)
	truncated := false
	for rows.Next() {
		var row ancestorRow
		if err := rows.Scan(&row.ObjectID, &row.Depth, &row.Title, &row.Version,
			&row.Status, &row.Readable); err != nil {
			return nil, false, err
		}
		if row.Depth > depth {
			truncated = true
			continue
		}
		result = append(result, row)
	}
	return result, truncated, rows.Err()
}

func (s *Service) descendants(ctx context.Context, actorID, objectID uuid.UUID, depth int) ([]descendantRow, bool, error) {
	rows, err := s.db.Query(ctx, `
WITH RECURSIVE child_tree AS (
    SELECT d.child_object_id AS object_id, d.parent_object_id, 1 AS depth,
           d.parent_version AS source_version
      FROM derivation_edges d
     WHERE d.parent_object_id = $1
    UNION ALL
    SELECT d.child_object_id, d.parent_object_id, x.depth + 1, d.parent_version
      FROM child_tree x
      JOIN derivation_edges d ON d.parent_object_id = x.object_id
     WHERE x.depth < $3
)
SELECT x.object_id, x.parent_object_id, x.depth, x.source_version,
       COALESCE(o.state->>'title',''), o.version, o.status,
       EXISTS (
         SELECT 1 FROM object_memberships m
          WHERE m.object_id = x.object_id
            AND m.user_id = $2
            AND m.revoked_at IS NULL
       ) AS readable
  FROM child_tree x
  JOIN living_objects o ON o.id = x.object_id
 ORDER BY x.depth ASC, x.object_id ASC`, objectID.String(), actorID.String(), depth+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	result := make([]descendantRow, 0)
	truncated := false
	for rows.Next() {
		var row descendantRow
		if err := rows.Scan(&row.ObjectID, &row.ParentID, &row.Depth, &row.SourceVersion,
			&row.Title, &row.Version, &row.Status, &row.Readable); err != nil {
			return nil, false, err
		}
		if row.Depth > depth {
			truncated = true
			continue
		}
		result = append(result, row)
	}
	return result, truncated, rows.Err()
}

// buildDescendantTree attaches only readable subtrees. An unreadable node is
// omitted together with its entire subtree so grandchildren cannot prove that
// the hidden middle node exists.
func buildDescendantTree(rootID string, rows []descendantRow, depth int) []LineageDescendant {
	type node struct {
		value    LineageDescendant
		children []*node
	}
	byID := make(map[string]*node, len(rows))
	blocked := make(map[string]bool)
	roots := make([]*node, 0)
	for _, row := range rows {
		if !row.Readable || row.Status != StatusActive || blocked[row.ParentID] {
			blocked[row.ObjectID] = true
			continue
		}
		current := &node{value: LineageDescendant{
			ObjectID:      row.ObjectID,
			Title:         row.Title,
			Version:       row.Version,
			Depth:         row.Depth,
			SourceVersion: row.SourceVersion,
			Children:      make([]LineageDescendant, 0),
		}}
		byID[row.ObjectID] = current
		if row.ParentID == rootID {
			roots = append(roots, current)
			continue
		}
		parent, ok := byID[row.ParentID]
		if !ok {
			blocked[row.ObjectID] = true
			continue
		}
		parent.children = append(parent.children, current)
	}
	var render func(*node) LineageDescendant
	render = func(current *node) LineageDescendant {
		value := current.value
		value.Children = make([]LineageDescendant, 0, len(current.children))
		for _, child := range current.children {
			value.Children = append(value.Children, render(child))
		}
		return value
	}
	out := make([]LineageDescendant, 0, len(roots))
	for _, root := range roots {
		out = append(out, render(root))
	}
	return out
}
