// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// RemapExperience deep-copies an experience for derivation. Kinds and
// entities get new UUIDs in the child world; declared reference fields and
// {{entity.<id>...}} template tokens are rewritten. Local view/behavior ids,
// field keys and ordinary text stay untouched, so an id that happens to
// contain a UUID is not corrupted.
func RemapExperience(experience *Experience, kindIDs, entityIDs map[string]string) (*Experience, error) {
	if experience == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(experience)
	if err != nil {
		return nil, fmt.Errorf("%w: copy experience", ErrInvalid)
	}
	var clone Experience
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return nil, fmt.Errorf("%w: copy experience", ErrInvalid)
	}
	clone.Root = remapViewNode(clone.Root, kindIDs, entityIDs)
	for index := range clone.Behaviors {
		behavior := &clone.Behaviors[index]
		remapConditionRefs(behavior.Condition, entityIDs)
		for actionIndex := range behavior.Actions {
			action := &behavior.Actions[actionIndex]
			remapped, err := remapRawJSON(action.Params, kindIDs, entityIDs)
			if err != nil {
				return nil, fmt.Errorf("%w: action params", ErrInvalid)
			}
			action.Params = remapped
		}
	}
	return &clone, nil
}

func remapViewNode(node *ViewNode, kindIDs, entityIDs map[string]string) *ViewNode {
	if node == nil {
		return nil
	}
	clone := *node
	clone.Props, _ = remapRawJSON(node.Props, kindIDs, entityIDs)
	if node.VisibleWhen != nil {
		condition := cloneCondition(node.VisibleWhen)
		remapConditionRefs(condition, entityIDs)
		clone.VisibleWhen = condition
	}
	clone.Children = make([]ViewNode, len(node.Children))
	for index := range node.Children {
		child := remapViewNode(&node.Children[index], kindIDs, entityIDs)
		clone.Children[index] = *child
	}
	return &clone
}

func cloneCondition(condition *Condition) *Condition {
	if condition == nil {
		return nil
	}
	clone := *condition
	if condition.Not != nil {
		clone.Not = cloneCondition(condition.Not)
	}
	clone.All = make([]Condition, len(condition.All))
	for index := range condition.All {
		clone.All[index] = *cloneCondition(&condition.All[index])
	}
	clone.Any = make([]Condition, len(condition.Any))
	for index := range condition.Any {
		clone.Any[index] = *cloneCondition(&condition.Any[index])
	}
	return &clone
}

func remapConditionRefs(condition *Condition, entityIDs map[string]string) {
	if condition == nil {
		return
	}
	if condition.EntityID != "" {
		if next, ok := entityIDs[condition.EntityID]; ok {
			condition.EntityID = next
		}
	}
	remapConditionRefs(condition.Not, entityIDs)
	for index := range condition.All {
		remapConditionRefs(&condition.All[index], entityIDs)
	}
	for index := range condition.Any {
		remapConditionRefs(&condition.Any[index], entityIDs)
	}
}

// referenceKeys are the declared cross-object reference properties used by the
// built-in modules. A new module that needs another reference key should add
// it here (and in its validator) rather than relying on blind substring
// replacement.
var referenceKeys = map[string]bool{
	"kind_id": true, "entity_id": true, "from_entity": true,
	"to_entity": true, "node_id": true, "trigger_id": true,
	"chapter_kind_id": true, "solid_kinds": true, "progress_entity": true,
}

var templateTokenPattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)

func newUUIDReplacer(kindIDs, entityIDs map[string]string) *strings.Replacer {
	pairs := make([]string, 0, 2*(len(kindIDs)+len(entityIDs)))
	for oldID, newID := range kindIDs {
		pairs = append(pairs, oldID, newID)
	}
	for oldID, newID := range entityIDs {
		pairs = append(pairs, oldID, newID)
	}
	return strings.NewReplacer(pairs...)
}

func remapRawJSON(raw json.RawMessage, kindIDs, entityIDs map[string]string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	replacer := newUUIDReplacer(kindIDs, entityIDs)
	value = remapAny(value, replacer, kindIDs, entityIDs, "")
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func remapAny(value any, replacer *strings.Replacer, kindIDs, entityIDs map[string]string, key string) any {
	switch typed := value.(type) {
	case string:
		if referenceKeys[key] {
			if next, ok := entityIDs[typed]; ok {
				return next
			}
			if next, ok := kindIDs[typed]; ok {
				return next
			}
		}
		// Only UUIDs inside an explicit template token are references. A
		// literal UUID in ordinary text or in a local id is not rewritten.
		return templateTokenPattern.ReplaceAllStringFunc(typed, func(token string) string {
			return replacer.Replace(token)
		})
	case []any:
		for index := range typed {
			typed[index] = remapAny(typed[index], replacer, kindIDs, entityIDs, key)
		}
		return typed
	case map[string]any:
		for childKey, item := range typed {
			typed[childKey] = remapAny(item, replacer, kindIDs, entityIDs, childKey)
		}
		return typed
	default:
		return value
	}
}
