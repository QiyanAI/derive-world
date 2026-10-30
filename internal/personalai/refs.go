// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/objects"
)

// resolveChangeReferences turns a kind/node/field name or 0-based index into
// the exact id the strict change action requires. It only rewrites one
// unambiguous match against the current state; unknown or duplicate names are
// left untouched so the normal strict rejection still wins. It never invents a
// reference and never changes anything except the named id field.
func resolveChangeReferences(state, raw json.RawMessage, action string) (json.RawMessage, bool) {
	var world objects.OpenWorldState
	if err := json.Unmarshal(state, &world); err != nil {
		return raw, false
	}
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		return raw, false
	}
	resolver := buildReferenceResolver(world)
	changed := false
	resolveKind := func(container map[string]any, key string) {
		if value, ok := container[key].(string); ok {
			if id, ok := resolver.kind(value); ok {
				container[key] = id
				changed = true
			}
			return
		}
		if number, ok := container[key].(float64); ok {
			index := int(number)
			if float64(index) == number && index >= 0 && index < len(resolver.kinds) {
				container[key] = resolver.kinds[index].ID
				changed = true
			}
		}
	}
	resolveNode := func(container map[string]any, key string) {
		if value, ok := container[key].(string); ok {
			if id, ok := resolver.node(value); ok {
				container[key] = id
				changed = true
			}
			return
		}
		if number, ok := container[key].(float64); ok {
			index := int(number)
			if float64(index) == number && index >= 0 && index < len(resolver.nodes) {
				container[key] = resolver.nodes[index].ID
				changed = true
			}
		}
	}
	// Providers often use a short key ("kind", "node", "field_label") for the
	// same reference. Move an unambiguous alias to the documented key; the
	// value still has to resolve and the strict action validator still runs.
	alias := func(target string, names ...string) {
		if _, ok := input[target]; ok {
			return
		}
		for _, name := range names {
			if value, ok := input[name]; ok {
				input[target] = value
				delete(input, name)
				changed = true
				return
			}
		}
	}
	resolveField := func(container map[string]any, key, nodeID string) {
		value, ok := container[key].(string)
		if !ok {
			return
		}
		if field, ok := resolver.field(nodeID, value); ok {
			container[key] = field
			changed = true
		}
	}
	nodeID := func(container map[string]any) string {
		value, _ := container["node_id"].(string)
		return value
	}

	switch action {
	case "add_node":
		alias("kind_id", "kind", "kind_name")
		resolveKind(input, "kind_id")
	case "update_kind", "remove_kind":
		alias("kind_id", "kind", "kind_name")
		resolveKind(input, "kind_id")
	case "update_node", "remove_node", "move_node", "set_node_status", "touch_node":
		alias("node_id", "node", "node_name")
		resolveNode(input, "node_id")
	case "set_node_field":
		alias("node_id", "node", "node_name")
		alias("field", "field_label", "label")
		resolveNode(input, "node_id")
		resolveField(input, "field", nodeID(input))
	case "add_link":
		alias("from", "from_name")
		alias("to", "to_name")
		resolveNode(input, "from")
		resolveNode(input, "to")
	case "add_rule":
		resolveNode(input, "trigger_id")
		if effects, ok := input["effects"].([]any); ok {
			for _, item := range effects {
				effect, ok := item.(map[string]any)
				if !ok {
					continue
				}
				resolveNode(effect, "node_id")
				resolveField(effect, "field", nodeID(effect))
			}
		}
	case "invoke_behavior":
		resolveNode(input, "entity_id")
	case "batch_change":
		actions, _ := input["actions"].([]any)
		for _, item := range actions {
			sub, ok := item.(map[string]any)
			if !ok {
				continue
			}
			subAction, _ := sub["action"].(string)
			subInput, ok := sub["input"].(map[string]any)
			if !ok {
				continue
			}
			encoded, err := json.Marshal(subInput)
			if err != nil {
				continue
			}
			resolved, subChanged := resolveChangeReferences(state, encoded, subAction)
			if !subChanged {
				continue
			}
			var updated map[string]any
			if json.Unmarshal(resolved, &updated) != nil {
				continue
			}
			sub["input"] = updated
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return raw, false
	}
	return encoded, true
}

type referenceResolver struct {
	kinds       []objects.OpenWorldKind
	nodes       []objects.OpenWorldNode
	kindsByID   map[string]string
	kindsByName map[string][]string
	nodesByID   map[string]string
	nodesByName map[string][]string
	nodeKind    map[string]string
	fields      map[string]map[string]string // node id -> label/key -> field key
}

func buildReferenceResolver(world objects.OpenWorldState) *referenceResolver {
	resolver := &referenceResolver{
		kinds:       world.Kinds,
		nodes:       world.Nodes,
		kindsByID:   map[string]string{},
		kindsByName: map[string][]string{},
		nodesByID:   map[string]string{},
		nodesByName: map[string][]string{},
		nodeKind:    map[string]string{},
		fields:      map[string]map[string]string{},
	}
	kindsByID := map[string]objects.OpenWorldKind{}
	labelCounts := map[string]map[string]int{}
	for _, kind := range world.Kinds {
		resolver.kindsByID[kind.ID] = kind.ID
		resolver.kindsByName[strings.TrimSpace(kind.Name)] = append(
			resolver.kindsByName[strings.TrimSpace(kind.Name)], kind.ID)
		kindsByID[kind.ID] = kind
		counts := map[string]int{}
		for _, field := range kind.Fields {
			if label := strings.TrimSpace(field.Label); label != "" {
				counts[label] += 1
			}
		}
		labelCounts[kind.ID] = counts
	}
	for _, node := range world.Nodes {
		resolver.nodesByID[node.ID] = node.ID
		resolver.nodesByName[strings.TrimSpace(node.Name)] = append(
			resolver.nodesByName[strings.TrimSpace(node.Name)], node.ID)
		resolver.nodeKind[node.ID] = node.KindID
		kind, ok := kindsByID[node.KindID]
		fields := map[string]string{}
		if ok {
			for _, field := range kind.Fields {
				label := strings.TrimSpace(field.Label)
				if label != "" && labelCounts[kind.ID][label] == 1 {
					fields[label] = field.Key
				}
				fields[field.Key] = field.Key
			}
		}
		resolver.fields[node.ID] = fields
	}
	return resolver
}

func (r *referenceResolver) kind(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if _, err := uuid.Parse(value); err == nil {
		if _, ok := r.kindsByID[value]; ok {
			return value, false
		}
		return "", false
	}
	if ids := r.kindsByName[value]; len(ids) == 1 {
		return ids[0], true
	}
	// A model sometimes names the kind after one of its things ("add another
	// lamp like 屋檐灯"). If the value is a unique node name, its kind is the
	// only possible kind reference; the strict action validator still runs.
	if ids := r.nodesByName[value]; len(ids) == 1 {
		if kindID, ok := r.nodeKind[ids[0]]; ok && kindID != "" {
			return kindID, true
		}
	}
	if index, ok := referenceIndex(value); ok && index < len(r.kinds) {
		return r.kinds[index].ID, true
	}
	return "", false
}

func (r *referenceResolver) node(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if _, err := uuid.Parse(value); err == nil {
		if _, ok := r.nodesByID[value]; ok {
			return value, false
		}
		return "", false
	}
	if ids := r.nodesByName[value]; len(ids) == 1 {
		return ids[0], true
	}
	if index, ok := referenceIndex(value); ok && index < len(r.nodes) {
		return r.nodes[index].ID, true
	}
	return "", false
}

func (r *referenceResolver) field(nodeID, value string) (string, bool) {
	fields := r.fields[nodeID]
	if fields == nil {
		return "", false
	}
	if key, ok := fields[strings.TrimSpace(value)]; ok {
		return key, true
	}
	return "", false
}

func referenceIndex(value string) (int, bool) {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number < 0 {
		return 0, false
	}
	return number, true
}
