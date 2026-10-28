// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	// Version is the only experience protocol version this build interprets.
	Version = 1

	maxViewNodes        = 64
	maxViewDepth        = 8
	maxChildrenPerNode  = 24
	maxExpandedViews    = 400
	maxBehaviors        = 24
	maxActionsPerAction = 8
	maxPropsBytes       = 4 * 1024
	maxExperienceBytes  = 32 * 1024
	maxConditionDepth   = 4
	maxConditionLeaves  = 16
	maxBehaviorName     = 120
	maxTemplateRunes    = 4000
	maxConditionText    = 1200
	maxLocalIDLength    = 64
)

// ValidLocalID reports whether a view or behavior id uses the bounded local
// slug grammar. It is exported for the objects action planner.
func ValidLocalID(value string) bool {
	return validateLocalID(value)
}

func validLocalID(value string) bool {
	return validateLocalID(value)
}

// validateLocalID checks the local id grammar.
func validateLocalID(value string) bool {
	if len(value) < 1 || len(value) > maxLocalIDLength {
		return false
	}
	for index, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && index > 0:
		case (r == '_' || r == '-') && index > 0:
		default:
			return false
		}
	}
	return true
}

// ValidateExperience validates and normalizes one experience document against
// the current world context. It mutates only the document it is given; the
// caller validates the rest of the world before persisting.
func ValidateExperience(experience *Experience, ctx *Context) error {
	return ValidateExperienceWith(Default(), experience, ctx)
}

// ValidateExperienceWith is the registry-injectable form used by tests that
// add a capability without changing the world core.
func ValidateExperienceWith(registry *Registry, experience *Experience, ctx *Context) error {
	if experience == nil {
		return nil
	}
	if registry == nil || ctx == nil {
		return fmt.Errorf("%w: missing registry or context", ErrInvalid)
	}
	if experience.Version != Version {
		return fmt.Errorf("%w: version", ErrInvalid)
	}
	if experience.Root == nil {
		return fmt.Errorf("%w: root", ErrInvalid)
	}
	if len(experience.Behaviors) > maxBehaviors {
		return fmt.Errorf("%w: behaviors", ErrInvalid)
	}

	// Behaviors are validated first: view props may reference their ids, and
	// behaviors never reference view nodes.
	ctx.Behaviors = make(map[string]bool, len(experience.Behaviors))
	behaviorIDs := make(map[string]bool, len(experience.Behaviors))
	for index := range experience.Behaviors {
		behavior := &experience.Behaviors[index]
		if !validateLocalID(behavior.ID) || behaviorIDs[behavior.ID] {
			return fmt.Errorf("%w: behaviors/%d/id", ErrInvalid, index)
		}
		behaviorIDs[behavior.ID] = true
		ctx.Behaviors[behavior.ID] = true
		if runeLength(behavior.Name) > maxBehaviorName {
			return fmt.Errorf("%w: behaviors/%d/name", ErrInvalid, index)
		}
		if behavior.Condition != nil {
			if _, err := validateCondition(behavior.Condition, ValidateContext{
				World: ctx, AllowEvent: true,
			}, 1); err != nil {
				return fmt.Errorf("%w: behaviors/%d/condition", ErrInvalid, index)
			}
		}
		if len(behavior.Actions) < 1 || len(behavior.Actions) > maxActionsPerAction {
			return fmt.Errorf("%w: behaviors/%d/actions", ErrInvalid, index)
		}
		for actionIndex := range behavior.Actions {
			action := &behavior.Actions[actionIndex]
			if err := validateAction(registry, action, ValidateContext{
				World: ctx, AllowEvent: true,
			}); err != nil {
				return fmt.Errorf("%w: behaviors/%d/actions/%d", ErrInvalid, index, actionIndex)
			}
		}
	}

	seenNodes := make(map[string]bool, maxViewNodes)
	state := &nodeValidationState{
		registry: registry, context: ctx, seen: seenNodes,
	}
	if err := validateNode(experience.Root, ValidateContext{World: ctx}, state, 1, 1); err != nil {
		return err
	}
	if state.count > maxViewNodes {
		return fmt.Errorf("%w: nodes", ErrInvalid)
	}
	encoded, err := json.Marshal(experience)
	if err != nil {
		return fmt.Errorf("%w: encode", ErrInvalid)
	}
	if len(encoded) > maxExperienceBytes {
		return fmt.Errorf("%w: experience size", ErrInvalid)
	}
	return nil
}

type nodeValidationState struct {
	registry *Registry
	context  *Context
	seen     map[string]bool
	count    int
	expanded int
}

func validateNode(node *ViewNode, ctx ValidateContext, state *nodeValidationState, depth, instances int) error {
	if node == nil {
		return fmt.Errorf("%w: node", ErrInvalid)
	}
	if depth > maxViewDepth {
		return fmt.Errorf("%w: depth", ErrInvalid)
	}
	if !validateLocalID(node.ID) || state.seen[node.ID] {
		return fmt.Errorf("%w: node id %q", ErrInvalid, node.ID)
	}
	state.seen[node.ID] = true
	state.count++
	if state.count > maxViewNodes {
		return fmt.Errorf("%w: node count", ErrInvalid)
	}
	state.expanded += instances
	if state.expanded > maxExpandedViews {
		return fmt.Errorf("%w: expanded view budget", ErrInvalid)
	}
	capability, ok := state.registry.Presentation(node.Capability)
	if !ok {
		return fmt.Errorf("%w: unknown capability %q", ErrInvalid, node.Capability)
	}
	descriptor := capability.Descriptor()
	if len(bytes.TrimSpace(node.Props)) > maxPropsBytes {
		return fmt.Errorf("%w: node props size", ErrInvalid)
	}
	normalized, err := capability.Validate(node.Props, ctx)
	if err != nil {
		return fmt.Errorf("%w: node %q props: %v", ErrInvalid, node.ID, err)
	}
	if len(normalized) > maxPropsBytes {
		return fmt.Errorf("%w: node props size", ErrInvalid)
	}
	node.Props = normalized
	switch node.Capability {
	case "core.space":
		// A space renders every entity that has a matching kind view. Charge
		// that inner work to the same expanded-instance budget so a repeated
		// space cannot multiply the whole world.
		state.expanded += len(state.context.Entities)
	case "core.table":
		if kindID, err := propString(normalized, "kind_id"); err == nil {
			count := 0
			for _, entity := range state.context.Entities {
				if entity.KindID == kindID {
					count++
				}
			}
			state.expanded += count
		}
	}
	if state.expanded > maxExpandedViews {
		return fmt.Errorf("%w: node %q expanded view budget", ErrInvalid, node.ID)
	}
	if node.VisibleWhen != nil {
		if _, err := validateCondition(node.VisibleWhen, ctx, 1); err != nil {
			return fmt.Errorf("%w: node %q visible_when", ErrInvalid, node.ID)
		}
	}
	if len(node.Children) > 0 && !descriptor.AllowsChildren {
		return fmt.Errorf("%w: node %q children", ErrInvalid, node.ID)
	}
	if len(node.Children) > maxChildrenPerNode {
		return fmt.Errorf("%w: node %q child count", ErrInvalid, node.ID)
	}
	childContext := ctx
	childInstances := instances
	if descriptor.ChildScope == "item" {
		kindID, err := propString(normalized, descriptor.ChildKindProp)
		if err != nil || !state.context.HasKind(kindID) {
			return fmt.Errorf("%w: node %q child scope", ErrInvalid, node.ID)
		}
		childContext.ItemKindID = kindID
		count := 0
		for _, entity := range state.context.Entities {
			if entity.KindID == kindID {
				count++
			}
		}
		if count < 1 {
			count = 1
		}
		childInstances = instances * count
		if childInstances > maxExpandedViews {
			return fmt.Errorf("%w: node %q expanded view budget", ErrInvalid, node.ID)
		}
	}
	for index := range node.Children {
		if err := validateNode(&node.Children[index], childContext, state, depth+1, childInstances); err != nil {
			return err
		}
	}
	return nil
}

func validateAction(registry *Registry, action *Action, ctx ValidateContext) error {
	if action == nil {
		return fmt.Errorf("%w: action", ErrInvalid)
	}
	capability, ok := registry.Behavior(action.Capability)
	if !ok {
		return fmt.Errorf("%w: unknown behavior capability %q", ErrInvalid, action.Capability)
	}
	if !descriptorHasAction(capability.Descriptor(), action.Action) {
		return fmt.Errorf("%w: unknown action %q", ErrInvalid, action.Action)
	}
	if len(bytes.TrimSpace(action.Params)) > maxPropsBytes {
		return fmt.Errorf("%w: action params size", ErrInvalid)
	}
	normalized, err := capability.ValidateAction(action.Action, action.Params, ctx)
	if err != nil {
		return fmt.Errorf("%w: action %q params: %v", ErrInvalid, action.Action, err)
	}
	if len(normalized) > maxPropsBytes {
		return fmt.Errorf("%w: action params size", ErrInvalid)
	}
	action.Params = normalized
	return nil
}

func descriptorHasAction(descriptor Descriptor, action string) bool {
	for _, candidate := range descriptor.Actions {
		if candidate.ID == action {
			return true
		}
	}
	return false
}

// validateCondition validates and normalizes one bounded condition tree. It
// returns the number of leaves in this subtree.
func validateCondition(condition *Condition, ctx ValidateContext, depth int) (int, error) {
	if condition == nil {
		return 0, fmt.Errorf("%w: condition", ErrInvalid)
	}
	if depth > maxConditionDepth {
		return 0, fmt.Errorf("%w: condition depth", ErrInvalid)
	}
	forms := 0
	if condition.Op != "" {
		forms++
	}
	if len(condition.All) > 0 {
		forms++
	}
	if len(condition.Any) > 0 {
		forms++
	}
	if condition.Not != nil {
		forms++
	}
	if forms != 1 {
		return 0, fmt.Errorf("%w: condition form", ErrInvalid)
	}
	if condition.Op != "" {
		return 1, validateConditionLeaf(condition, ctx)
	}
	if condition.Not != nil {
		return validateCondition(condition.Not, ctx, depth+1)
	}
	children := condition.All
	if len(condition.Any) > 0 {
		children = condition.Any
	}
	if len(children) > 8 {
		return 0, fmt.Errorf("%w: condition children", ErrInvalid)
	}
	total := 0
	for index := range children {
		leaves, err := validateCondition(&children[index], ctx, depth+1)
		if err != nil {
			return 0, err
		}
		total += leaves
		if total > maxConditionLeaves {
			return 0, fmt.Errorf("%w: condition leaves", ErrInvalid)
		}
	}
	return total, nil
}

func validateConditionLeaf(condition *Condition, ctx ValidateContext) error {
	if condition.EntityID == "" {
		return fmt.Errorf("%w: condition entity_id", ErrInvalid)
	}
	if strings.TrimSpace(condition.Field) == "" || runeLength(condition.Field) > 40 {
		return fmt.Errorf("%w: condition field", ErrInvalid)
	}
	fieldType, err := conditionFieldType(ctx, condition.EntityID, condition.Field)
	if err != nil {
		return err
	}
	switch fieldType {
	case "status":
		if condition.Op != "eq" && condition.Op != "ne" {
			return fmt.Errorf("%w: condition op", ErrInvalid)
		}
		value, ok := condition.Value.(string)
		if !ok || (value != "open" && value != "done") {
			return fmt.Errorf("%w: condition value", ErrInvalid)
		}
	case "text":
		if condition.Op != "eq" && condition.Op != "ne" {
			return fmt.Errorf("%w: condition op", ErrInvalid)
		}
		if isEventValue(condition.Value) && ctx.AllowEvent {
			return nil
		}
		value, ok := condition.Value.(string)
		if !ok || runeLength(value) > maxConditionText {
			return fmt.Errorf("%w: condition value", ErrInvalid)
		}
	case "number":
		switch condition.Op {
		case "eq", "ne", "gt", "gte", "lt", "lte":
		default:
			return fmt.Errorf("%w: condition op", ErrInvalid)
		}
		if isEventValue(condition.Value) && ctx.AllowEvent {
			return nil
		}
		value, ok := condition.Value.(float64)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("%w: condition value", ErrInvalid)
		}
	case "flag":
		switch condition.Op {
		case "eq", "ne":
			if isEventValue(condition.Value) && ctx.AllowEvent {
				return nil
			}
			if _, ok := condition.Value.(bool); !ok {
				return fmt.Errorf("%w: condition value", ErrInvalid)
			}
		case "truthy", "falsy":
			if condition.Value != nil {
				return fmt.Errorf("%w: condition value", ErrInvalid)
			}
		default:
			return fmt.Errorf("%w: condition op", ErrInvalid)
		}
	case "event":
		// The event entity's kind is not known statically. The field and value
		// types are checked again by EvaluateCondition at execution time.
		switch condition.Op {
		case "eq", "ne", "gt", "gte", "lt", "lte", "truthy", "falsy":
			return nil
		default:
			return fmt.Errorf("%w: condition op", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: condition field", ErrInvalid)
	}
	return nil
}

// conditionFieldType resolves the declared type for an explicit entity, an
// item scope, or an event entity. An event entity's field type is checked
// again at execution time because its kind is not known statically.
func conditionFieldType(ctx ValidateContext, entityID, field string) (string, error) {
	switch entityID {
	case "$event.entity":
		if !ctx.AllowEvent {
			return "", fmt.Errorf("%w: event scope", ErrInvalid)
		}
		if field == "status" {
			return "status", nil
		}
		return "event", nil
	case "$item":
		if ctx.ItemKindID == "" {
			return "", fmt.Errorf("%w: item scope", ErrInvalid)
		}
		kind, ok := ctx.World.Kinds[ctx.ItemKindID]
		if !ok {
			return "", fmt.Errorf("%w: item kind", ErrInvalid)
		}
		if field == "status" {
			return "status", nil
		}
		value, ok := kind.Fields[field]
		if !ok {
			return "", fmt.Errorf("%w: item field", ErrInvalid)
		}
		return value, nil
	default:
		fieldType, ok := ctx.World.FieldType(entityID, field)
		if !ok {
			return "", fmt.Errorf("%w: condition field", ErrInvalid)
		}
		return fieldType, nil
	}
}

func isEventValue(value any) bool {
	text, ok := value.(string)
	return ok && text == "$event.value"
}

func propString(raw json.RawMessage, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: missing property name", ErrInvalid)
	}
	var values map[string]any
	if err := decodeStrict(raw, &values); err != nil {
		return "", err
	}
	value, ok := values[key].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("%w: property %s", ErrInvalid, key)
	}
	return value, nil
}

func decodeStrict(raw []byte, destination any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	err := decoder.Decode(&extra)
	if err == nil {
		return errors.New("value must contain a single JSON document")
	}
	if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func runeLength(value string) int {
	return utf8.RuneCountInString(value)
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
