// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RemapExperience deep-copies an experience for derivation. Kinds and
// entities get new UUIDs in the child world; every reference in capability
// props, conditions and action params is rewritten. Local view and behavior
// ids stay stable because they are scoped to one object, not global identity.
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
	replacer := newUUIDReplacer(kindIDs, entityIDs)
	clone.Root = remapViewNode(clone.Root, kindIDs, entityIDs, replacer)
	for index := range clone.Behaviors {
		behavior := &clone.Behaviors[index]
		remapConditionRefs(behavior.Condition, entityIDs)
		for actionIndex := range behavior.Actions {
			action := &behavior.Actions[actionIndex]
			remapped, err := remapRawJSON(action.Params, replacer, entityIDs)
			if err != nil {
				return nil, fmt.Errorf("%w: action params", ErrInvalid)
			}
			action.Params = remapped
		}
	}
	return &clone, nil
}

func remapViewNode(node *ViewNode, kindIDs, entityIDs map[string]string, replacer *strings.Replacer) *ViewNode {
	if node == nil {
		return nil
	}
	clone := *node
	clone.Props, _ = remapRawJSON(node.Props, replacer, entityIDs)
	if node.VisibleWhen != nil {
		condition := cloneCondition(node.VisibleWhen)
		remapConditionRefs(condition, entityIDs)
		clone.VisibleWhen = condition
	}
	clone.Children = make([]ViewNode, len(node.Children))
	for index := range node.Children {
		child := remapViewNode(&node.Children[index], kindIDs, entityIDs, replacer)
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

func remapRawJSON(raw json.RawMessage, replacer *strings.Replacer, entityIDs map[string]string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	value = remapAny(value, replacer, entityIDs)
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func remapAny(value any, replacer *strings.Replacer, entityIDs map[string]string) any {
	switch typed := value.(type) {
	case string:
		if next, ok := entityIDs[typed]; ok {
			return next
		}
		return replacer.Replace(typed)
	case []any:
		for index := range typed {
			typed[index] = remapAny(typed[index], replacer, entityIDs)
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			typed[key] = remapAny(item, replacer, entityIDs)
		}
		return typed
	default:
		return value
	}
}
