// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
)

// buildOpenWorldState converts the strict create body into persisted state.
// The client (or the model) refers to kinds and nodes by index; the server is
// the only component that mints identity.
func buildOpenWorldState(title string, content OpenWorldContent) (*OpenWorldState, error) {
	if len(content.Kinds) > maxOpenWorldKinds {
		return nil, fmt.Errorf("%w: content.kinds", ErrInvalidRequest)
	}
	if len(content.Nodes) > maxOpenWorldNodes {
		return nil, fmt.Errorf("%w: content.nodes", ErrInvalidRequest)
	}
	if len(content.Links) > maxOpenWorldLinks {
		return nil, fmt.Errorf("%w: content.links", ErrInvalidRequest)
	}
	if len(content.Rules) > maxOpenWorldRules {
		return nil, fmt.Errorf("%w: content.rules", ErrInvalidRequest)
	}
	state := &OpenWorldState{
		Title:        title,
		Premise:      content.Premise,
		Kinds:        make([]OpenWorldKind, 0, len(content.Kinds)),
		Nodes:        make([]OpenWorldNode, 0, len(content.Nodes)),
		Links:        make([]OpenWorldLink, 0, len(content.Links)),
		Rules:        make([]OpenWorldRule, 0, len(content.Rules)),
		Views:        nil,
		Tags:         normalizeTags(content.Tags),
		Presentation: DefaultOpenWorldPresentation(),
	}
	views, err := normalizeOpenWorldViews(content.Views)
	if err != nil {
		return nil, err
	}
	state.Views = views
	if content.Presentation != nil {
		state.Presentation = *content.Presentation
	}
	if content.Experience != nil {
		state.Experience = content.Experience
	}

	kindIDs := make([]string, len(content.Kinds))
	for index, kind := range content.Kinds {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		kindIDs[index] = id.String()
		fields := make([]OpenWorldField, len(kind.Fields))
		copy(fields, kind.Fields)
		appearance := DefaultOpenWorldAppearance()
		if kind.Appearance != nil {
			appearance = *kind.Appearance
		}
		bindings := make([]OpenWorldBinding, len(kind.Bindings))
		copy(bindings, kind.Bindings)
		state.Kinds = append(state.Kinds, OpenWorldKind{
			ID: id.String(), Name: kind.Name, Hue: kind.Hue, Fields: fields,
			Appearance: appearance, Bindings: bindings,
		})
	}

	nodeIDs := make([]string, len(content.Nodes))
	for index, node := range content.Nodes {
		if node.KindIndex < 0 || node.KindIndex >= len(kindIDs) {
			return nil, fmt.Errorf("%w: content.nodes/%d/kind_index", ErrInvalidRequest, index)
		}
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		nodeIDs[index] = id.String()
		x, y := defaultOpenWorldPosition(index, len(content.Nodes))
		if node.X != nil {
			x = *node.X
		}
		if node.Y != nil {
			y = *node.Y
		}
		values := make(map[string]any, len(node.Values))
		for key, value := range node.Values {
			values[key] = value
		}
		state.Nodes = append(state.Nodes, OpenWorldNode{
			ID: id.String(), KindID: kindIDs[node.KindIndex], Name: node.Name,
			Text: node.Text, Values: values,
			Status: OpenWorldStatusOpen, X: x, Y: y,
		})
	}

	for index, link := range content.Links {
		if link.FromIndex < 0 || link.FromIndex >= len(nodeIDs) ||
			link.ToIndex < 0 || link.ToIndex >= len(nodeIDs) {
			return nil, fmt.Errorf("%w: content.links/%d", ErrInvalidRequest, index)
		}
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		state.Links = append(state.Links, OpenWorldLink{
			ID: id.String(), From: nodeIDs[link.FromIndex], To: nodeIDs[link.ToIndex],
			Label: link.Label,
		})
	}

	for index, rule := range content.Rules {
		if rule.TriggerIndex < 0 || rule.TriggerIndex >= len(nodeIDs) {
			return nil, fmt.Errorf("%w: content.rules/%d/trigger_index", ErrInvalidRequest, index)
		}
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		var condition *OpenWorldCondition
		if rule.Condition != nil {
			if rule.Condition.NodeIndex < 0 || rule.Condition.NodeIndex >= len(nodeIDs) {
				return nil, fmt.Errorf("%w: content.rules/%d/condition/node_index",
					ErrInvalidRequest, index)
			}
			condition = &OpenWorldCondition{
				NodeID: nodeIDs[rule.Condition.NodeIndex], Field: rule.Condition.Field,
				Op: rule.Condition.Op, Value: rule.Condition.Value,
			}
		}
		effects := make([]OpenWorldEffect, 0, len(rule.Effects))
		for effectIndex, effect := range rule.Effects {
			if effect.NodeIndex < 0 || effect.NodeIndex >= len(nodeIDs) {
				return nil, fmt.Errorf("%w: content.rules/%d/effects/%d/node_index",
					ErrInvalidRequest, index, effectIndex)
			}
			effects = append(effects, OpenWorldEffect{
				NodeID: nodeIDs[effect.NodeIndex], Field: effect.Field,
				Op: effect.Op, Value: effect.Value,
			})
		}
		state.Rules = append(state.Rules, OpenWorldRule{
			ID: id.String(), TriggerID: nodeIDs[rule.TriggerIndex],
			Condition: condition, Effects: effects,
		})
	}

	if err := ValidateOpenWorldState(state); err != nil {
		return nil, err
	}
	return state, nil
}

// defaultOpenWorldPosition lays nodes out on a readable grid when the creator
// did not choose coordinates. It is deterministic for a given node count.
func defaultOpenWorldPosition(index, total int) (float64, float64) {
	if total <= 0 {
		return openWorldWidth / 2, openWorldHeight / 2
	}
	columns := int(math.Ceil(math.Sqrt(float64(total))))
	if columns < 1 {
		columns = 1
	}
	rows := int(math.Ceil(float64(total) / float64(columns)))
	if rows < 1 {
		rows = 1
	}
	column := index % columns
	row := index / columns
	x := openWorldWidth / 2
	y := openWorldHeight / 2
	if columns > 1 {
		x = 180 + float64(column)*(1240/float64(columns-1))
	}
	if rows > 1 {
		y = 190 + float64(row)*(620/float64(rows-1))
	}
	return clampOpenWorld(x, openWorldMinX, openWorldMaxX),
		clampOpenWorld(y, openWorldMinY, openWorldMaxY)
}

func clampOpenWorld(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// FirstNodeID is a small helper for projections that need a stable default.
func (state *OpenWorldState) FirstNodeID() string {
	if len(state.Nodes) == 0 {
		return ""
	}
	return state.Nodes[0].ID
}

// Summary returns the premise with surrounding whitespace removed.
func (state *OpenWorldState) Summary() string {
	return strings.TrimSpace(state.Premise)
}
