// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/worldcap"
)

// OpenWorldAppliedEffect is one consequence that actually ran. It is written
// into the object event so a world's history explains itself.
type OpenWorldAppliedEffect struct {
	// Source is "behavior" for an experience action; legacy rule effects leave
	// it empty. It keeps the two effect dialects separable in one event array.
	Source string `json:"source,omitempty"`
	RuleID string `json:"rule_id"`
	NodeID string `json:"node_id"`
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value,omitempty"`
}

func isOpenWorldAction(action string) bool {
	switch action {
	case "add_kind", "update_kind", "remove_kind",
		"add_node", "update_node", "remove_node", "move_node",
		"set_node_field", "set_node_status", "touch_node",
		"add_link", "remove_link", "add_rule", "remove_rule", "set_views",
		"set_presentation", "set_experience", "invoke_behavior", "reshape_world",
		"custom_action":
		return true
	default:
		return false
	}
}

type openWorldFieldInput struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

type addOpenWorldKindInput struct {
	Name       string                `json:"name"`
	Hue        int                   `json:"hue"`
	Fields     []openWorldFieldInput `json:"fields"`
	Appearance *OpenWorldAppearance  `json:"appearance"`
	Bindings   []OpenWorldBinding    `json:"bindings"`
}

type updateOpenWorldKindInput struct {
	KindID     string                 `json:"kind_id"`
	Name       *string                `json:"name"`
	Hue        *int                   `json:"hue"`
	Fields     *[]openWorldFieldInput `json:"fields"`
	Appearance *OpenWorldAppearance   `json:"appearance"`
	Bindings   *[]OpenWorldBinding    `json:"bindings"`
}

type removeOpenWorldKindInput struct {
	KindID string `json:"kind_id"`
}

type addOpenWorldNodeInput struct {
	KindID string   `json:"kind_id"`
	Name   string   `json:"name"`
	Text   string   `json:"text"`
	X      *float64 `json:"x"`
	Y      *float64 `json:"y"`
}

type updateOpenWorldNodeInput struct {
	NodeID string   `json:"node_id"`
	Name   *string  `json:"name"`
	Text   *string  `json:"text"`
	X      *float64 `json:"x"`
	Y      *float64 `json:"y"`
}

type removeOpenWorldNodeInput struct {
	NodeID string `json:"node_id"`
}

type moveOpenWorldNodeInput struct {
	NodeID string  `json:"node_id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

type setOpenWorldFieldInput struct {
	NodeID string `json:"node_id"`
	Field  string `json:"field"`
	Value  any    `json:"value"`
}

type setOpenWorldStatusInput struct {
	NodeID string `json:"node_id"`
	Status string `json:"status"`
}

type touchOpenWorldNodeInput struct {
	NodeID string `json:"node_id"`
}

type reshapeOpenWorldInput struct {
	Note    string         `json:"note"`
	Content OpenWorldState `json:"content"`
}

type customActionInput struct {
	SurfaceID string `json:"surface_id"`
	Event     string `json:"event"`
	NodeID    string `json:"node_id,omitempty"`
	Key       string `json:"key,omitempty"`
	Value     any    `json:"value,omitempty"`
}

type addOpenWorldLinkInput struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

type removeOpenWorldLinkInput struct {
	LinkID string `json:"link_id"`
}

type openWorldEffectInput struct {
	NodeID string `json:"node_id"`
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
}

type openWorldConditionInput struct {
	NodeID string `json:"node_id"`
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
}

type addOpenWorldRuleInput struct {
	TriggerID string                   `json:"trigger_id"`
	Condition *openWorldConditionInput `json:"condition"`
	Effects   []openWorldEffectInput   `json:"effects"`
}

type removeOpenWorldRuleInput struct {
	RuleID string `json:"rule_id"`
}

type setOpenWorldViewsInput struct {
	Views []string `json:"views"`
}

type setOpenWorldPresentationInput struct {
	Presentation OpenWorldPresentation `json:"presentation"`
}

type setOpenWorldExperienceInput struct {
	Experience *worldcap.Experience `json:"experience"`
}

type invokeOpenWorldBehaviorInput struct {
	BehaviorID string `json:"behavior_id"`
	EntityID   string `json:"entity_id"`
	Value      any    `json:"value"`
}

func decodeOpenWorldInput(raw json.RawMessage, destination any) error {
	if err := decodeStrict(raw, destination); err != nil {
		return fmt.Errorf("%w: input", ErrInvalidRequest)
	}
	return nil
}

func validateOpenWorldKindInput(name string, hue int, fields []openWorldFieldInput) error {
	if runeLength(strings.TrimSpace(name)) < 1 || runeLength(name) > maxOpenWorldKindNameRunes {
		return fmt.Errorf("%w: input.name", ErrInvalidRequest)
	}
	if hue < 0 || hue > 359 {
		return fmt.Errorf("%w: input.hue", ErrInvalidRequest)
	}
	if len(fields) > maxOpenWorldKindFields {
		return fmt.Errorf("%w: input.fields", ErrInvalidRequest)
	}
	seen := make(map[string]bool, len(fields))
	for index, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" || runeLength(key) > maxOpenWorldFieldKeyRunes || seen[key] ||
			key == OpenWorldStatusField {
			return fmt.Errorf("%w: input.fields/%d/key", ErrInvalidRequest, index)
		}
		seen[key] = true
		if runeLength(field.Label) > maxOpenWorldFieldLabelRune {
			return fmt.Errorf("%w: input.fields/%d/label", ErrInvalidRequest, index)
		}
		if !validOpenWorldFieldType(field.Type) {
			return fmt.Errorf("%w: input.fields/%d/type", ErrInvalidRequest, index)
		}
	}
	return nil
}

func validateOpenWorldNodeName(name string) error {
	if runeLength(strings.TrimSpace(name)) < 1 || runeLength(name) > maxOpenWorldNodeNameRunes {
		return fmt.Errorf("%w: input.name", ErrInvalidRequest)
	}
	return nil
}

func validateOpenWorldNodeText(text string) error {
	if utf8.RuneCountInString(text) > maxOpenWorldNodeTextRunes {
		return fmt.Errorf("%w: input.text", ErrInvalidRequest)
	}
	return nil
}

func normalizeOpenWorldFieldInputs(fields []openWorldFieldInput) []OpenWorldField {
	out := make([]OpenWorldField, len(fields))
	for index, field := range fields {
		out[index] = OpenWorldField{
			Key: strings.TrimSpace(field.Key), Label: field.Label, Type: field.Type,
		}
	}
	return out
}

func fieldInputMap(fields []openWorldFieldInput) []any {
	out := make([]any, 0, len(fields))
	for _, field := range fields {
		out = append(out, map[string]any{
			"key": strings.TrimSpace(field.Key), "label": field.Label, "type": field.Type,
		})
	}
	return out
}

func validateEffectInput(input openWorldEffectInput) error {
	if input.Field == "" {
		return fmt.Errorf("%w: input.effects.field", ErrInvalidRequest)
	}
	switch input.Op {
	case OpenWorldOpSet, OpenWorldOpAdd, OpenWorldOpToggle:
	default:
		return fmt.Errorf("%w: input.effects.op", ErrInvalidRequest)
	}
	if input.Op == OpenWorldOpToggle {
		return nil
	}
	if input.Value == nil {
		return fmt.Errorf("%w: input.effects.value", ErrInvalidRequest)
	}
	return nil
}

// planOpenWorldAction validates the exact input shape of one open-world
// action and records the normalized values used by the event log.
func planOpenWorldAction(action string, raw json.RawMessage, plan *ActionPlan) error {
	plan.Permission = PermContribute
	switch action {
	case "add_kind":
		if err := validateInputKeys(raw, []string{"name"},
			[]string{"name", "hue", "fields", "appearance", "bindings"}); err != nil {
			return err
		}
		var input addOpenWorldKindInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if err := validateOpenWorldKindInput(input.Name, input.Hue, input.Fields); err != nil {
			return err
		}
		appearance := DefaultOpenWorldAppearance()
		if input.Appearance != nil {
			normalized, err := normalizeOpenWorldAppearance(*input.Appearance)
			if err != nil {
				return err
			}
			appearance = normalized
		}
		fieldMap := make(map[string]OpenWorldField, len(input.Fields))
		for _, field := range normalizeOpenWorldFieldInputs(input.Fields) {
			fieldMap[field.Key] = field
		}
		bindings, err := normalizeOpenWorldBindings(input.Bindings, fieldMap)
		if err != nil {
			return fmt.Errorf("%w: input.bindings", ErrInvalidRequest)
		}
		plan.Input["name"] = strings.TrimSpace(input.Name)
		plan.Input["hue"] = input.Hue
		plan.Input["fields"] = fieldInputMap(input.Fields)
		plan.Input["appearance"] = appearance
		plan.Input["bindings"] = bindings
	case "update_kind":
		if err := validateInputKeys(raw, []string{"kind_id"},
			[]string{"kind_id", "name", "hue", "fields", "appearance", "bindings"}); err != nil {
			return err
		}
		var input updateOpenWorldKindInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.KindID); err != nil {
			return fmt.Errorf("%w: input.kind_id", ErrInvalidRequest)
		}
		if input.Name == nil && input.Hue == nil && input.Fields == nil &&
			input.Appearance == nil && input.Bindings == nil {
			return fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		if input.Appearance != nil {
			appearance, err := normalizeOpenWorldAppearance(*input.Appearance)
			if err != nil {
				return err
			}
			plan.Input["appearance"] = appearance
		}
		if input.Bindings != nil {
			for _, binding := range *input.Bindings {
				if err := validateOpenWorldBindingShape(binding); err != nil {
					return fmt.Errorf("%w: input.bindings", ErrInvalidRequest)
				}
			}
			plan.Input["bindings"] = *input.Bindings
		}
		if input.Name != nil {
			if runeLength(strings.TrimSpace(*input.Name)) < 1 || runeLength(*input.Name) > maxOpenWorldKindNameRunes {
				return fmt.Errorf("%w: input.name", ErrInvalidRequest)
			}
			plan.Input["name"] = strings.TrimSpace(*input.Name)
		}
		if input.Hue != nil {
			if *input.Hue < 0 || *input.Hue > 359 {
				return fmt.Errorf("%w: input.hue", ErrInvalidRequest)
			}
			plan.Input["hue"] = *input.Hue
		}
		if input.Fields != nil {
			if len(*input.Fields) > maxOpenWorldKindFields {
				return fmt.Errorf("%w: input.fields", ErrInvalidRequest)
			}
			seen := make(map[string]bool, len(*input.Fields))
			for index, field := range *input.Fields {
				key := strings.TrimSpace(field.Key)
				if key == "" || runeLength(key) > maxOpenWorldFieldKeyRunes || seen[key] ||
					key == OpenWorldStatusField {
					return fmt.Errorf("%w: input.fields/%d/key", ErrInvalidRequest, index)
				}
				seen[key] = true
				if runeLength(field.Label) > maxOpenWorldFieldLabelRune {
					return fmt.Errorf("%w: input.fields/%d/label", ErrInvalidRequest, index)
				}
				if !validOpenWorldFieldType(field.Type) {
					return fmt.Errorf("%w: input.fields/%d/type", ErrInvalidRequest, index)
				}
			}
			plan.Input["fields"] = fieldInputMap(*input.Fields)
		}
		plan.Input["kind_id"] = input.KindID
	case "remove_kind":
		if err := validateInputKeys(raw, []string{"kind_id"}, []string{"kind_id"}); err != nil {
			return err
		}
		var input removeOpenWorldKindInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.KindID); err != nil {
			return fmt.Errorf("%w: input.kind_id", ErrInvalidRequest)
		}
		plan.Input["kind_id"] = input.KindID
	case "add_node":
		if err := validateInputKeys(raw, []string{"kind_id", "name"},
			[]string{"kind_id", "name", "text", "x", "y"}); err != nil {
			return err
		}
		var input addOpenWorldNodeInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.KindID); err != nil {
			return fmt.Errorf("%w: input.kind_id", ErrInvalidRequest)
		}
		if err := validateOpenWorldNodeName(input.Name); err != nil {
			return err
		}
		if err := validateOpenWorldNodeText(input.Text); err != nil {
			return err
		}
		if input.X != nil {
			if err := validateOpenWorldCoordinate(*input.X, openWorldMinX, openWorldMaxX, "input.x"); err != nil {
				return err
			}
		}
		if input.Y != nil {
			if err := validateOpenWorldCoordinate(*input.Y, openWorldMinY, openWorldMaxY, "input.y"); err != nil {
				return err
			}
		}
		plan.Input["kind_id"] = input.KindID
		plan.Input["name"] = strings.TrimSpace(input.Name)
		plan.Input["text"] = input.Text
		if input.X != nil {
			plan.Input["x"] = *input.X
		}
		if input.Y != nil {
			plan.Input["y"] = *input.Y
		}
	case "update_node":
		if err := validateInputKeys(raw, []string{"node_id"},
			[]string{"node_id", "name", "text", "x", "y"}); err != nil {
			return err
		}
		var input updateOpenWorldNodeInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.NodeID); err != nil {
			return fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		if input.Name == nil && input.Text == nil && input.X == nil && input.Y == nil {
			return fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		if input.Name != nil {
			if err := validateOpenWorldNodeName(*input.Name); err != nil {
				return err
			}
			plan.Input["name"] = strings.TrimSpace(*input.Name)
		}
		if input.Text != nil {
			if err := validateOpenWorldNodeText(*input.Text); err != nil {
				return err
			}
			plan.Input["text"] = *input.Text
		}
		if input.X != nil {
			if err := validateOpenWorldCoordinate(*input.X, openWorldMinX, openWorldMaxX, "input.x"); err != nil {
				return err
			}
			plan.Input["x"] = *input.X
		}
		if input.Y != nil {
			if err := validateOpenWorldCoordinate(*input.Y, openWorldMinY, openWorldMaxY, "input.y"); err != nil {
				return err
			}
			plan.Input["y"] = *input.Y
		}
		plan.Input["node_id"] = input.NodeID
	case "remove_node", "touch_node":
		var input removeOpenWorldNodeInput
		key := "node_id"
		if err := validateInputKeys(raw, []string{key}, []string{key}); err != nil {
			return err
		}
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.NodeID); err != nil {
			return fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		plan.Input["node_id"] = input.NodeID
	case "move_node":
		if err := validateInputKeys(raw, []string{"node_id", "x", "y"}, []string{"node_id", "x", "y"}); err != nil {
			return err
		}
		var input moveOpenWorldNodeInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.NodeID); err != nil {
			return fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		if err := validateOpenWorldCoordinate(input.X, openWorldMinX, openWorldMaxX, "input.x"); err != nil {
			return err
		}
		if err := validateOpenWorldCoordinate(input.Y, openWorldMinY, openWorldMaxY, "input.y"); err != nil {
			return err
		}
		plan.Input["node_id"] = input.NodeID
		plan.Input["x"] = input.X
		plan.Input["y"] = input.Y
	case "set_node_field":
		if err := validateInputKeys(raw, []string{"node_id", "field", "value"},
			[]string{"node_id", "field", "value"}); err != nil {
			return err
		}
		var input setOpenWorldFieldInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.NodeID); err != nil {
			return fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		if strings.TrimSpace(input.Field) == "" || runeLength(input.Field) > maxOpenWorldFieldKeyRunes {
			return fmt.Errorf("%w: input.field", ErrInvalidRequest)
		}
		plan.Input["node_id"] = input.NodeID
		plan.Input["field"] = input.Field
		plan.Input["value"] = input.Value
	case "set_node_status":
		if err := validateInputKeys(raw, []string{"node_id", "status"},
			[]string{"node_id", "status"}); err != nil {
			return err
		}
		var input setOpenWorldStatusInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.NodeID); err != nil {
			return fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		if input.Status != OpenWorldStatusOpen && input.Status != OpenWorldStatusDone {
			return fmt.Errorf("%w: input.status", ErrInvalidRequest)
		}
		plan.Input["node_id"] = input.NodeID
		plan.Input["status"] = input.Status
	case "add_link":
		if err := validateInputKeys(raw, []string{"from", "to"}, []string{"from", "to", "label"}); err != nil {
			return err
		}
		var input addOpenWorldLinkInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.From); err != nil {
			return fmt.Errorf("%w: input.from", ErrInvalidRequest)
		}
		if _, err := uuid.Parse(input.To); err != nil {
			return fmt.Errorf("%w: input.to", ErrInvalidRequest)
		}
		if input.From == input.To {
			return fmt.Errorf("%w: input.to", ErrInvalidRequest)
		}
		if runeLength(input.Label) > maxOpenWorldLinkLabelRune {
			return fmt.Errorf("%w: input.label", ErrInvalidRequest)
		}
		plan.Input["from"] = input.From
		plan.Input["to"] = input.To
		plan.Input["label"] = input.Label
	case "remove_link":
		if err := validateInputKeys(raw, []string{"link_id"}, []string{"link_id"}); err != nil {
			return err
		}
		var input removeOpenWorldLinkInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.LinkID); err != nil {
			return fmt.Errorf("%w: input.link_id", ErrInvalidRequest)
		}
		plan.Input["link_id"] = input.LinkID
	case "add_rule":
		if err := validateInputKeys(raw, []string{"trigger_id", "effects"},
			[]string{"trigger_id", "effects", "condition"}); err != nil {
			return err
		}
		var input addOpenWorldRuleInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.TriggerID); err != nil {
			return fmt.Errorf("%w: input.trigger_id", ErrInvalidRequest)
		}
		if input.Condition != nil {
			if _, err := uuid.Parse(input.Condition.NodeID); err != nil {
				return fmt.Errorf("%w: input.condition.node_id", ErrInvalidRequest)
			}
			if strings.TrimSpace(input.Condition.Field) == "" ||
				!validOpenWorldConditionOp(input.Condition.Op) {
				return fmt.Errorf("%w: input.condition", ErrInvalidRequest)
			}
			plan.Input["condition"] = map[string]any{
				"node_id": input.Condition.NodeID, "field": input.Condition.Field,
				"op": input.Condition.Op, "value": input.Condition.Value,
			}
		}
		if len(input.Effects) == 0 || len(input.Effects) > maxOpenWorldRuleEffects {
			return fmt.Errorf("%w: input.effects", ErrInvalidRequest)
		}
		effects := make([]any, 0, len(input.Effects))
		for index, effect := range input.Effects {
			if _, err := uuid.Parse(effect.NodeID); err != nil {
				return fmt.Errorf("%w: input.effects/%d/node_id", ErrInvalidRequest, index)
			}
			if err := validateEffectInput(effect); err != nil {
				return fmt.Errorf("%w: input.effects/%d", ErrInvalidRequest, index)
			}
			effects = append(effects, map[string]any{
				"node_id": effect.NodeID, "field": effect.Field,
				"op": effect.Op, "value": effect.Value,
			})
		}
		plan.Input["trigger_id"] = input.TriggerID
		plan.Input["effects"] = effects
	case "remove_rule":
		if err := validateInputKeys(raw, []string{"rule_id"}, []string{"rule_id"}); err != nil {
			return err
		}
		var input removeOpenWorldRuleInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if _, err := uuid.Parse(input.RuleID); err != nil {
			return fmt.Errorf("%w: input.rule_id", ErrInvalidRequest)
		}
		plan.Input["rule_id"] = input.RuleID
	case "set_views":
		if err := validateInputKeys(raw, []string{"views"}, []string{"views"}); err != nil {
			return err
		}
		var input setOpenWorldViewsInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		views, err := normalizeOpenWorldViews(input.Views)
		if err != nil {
			return err
		}
		items := make([]any, len(views))
		for index, view := range views {
			items[index] = view
		}
		plan.Input["views"] = items
	case "set_presentation":
		if err := validateInputKeys(raw, []string{"presentation"}, []string{"presentation"}); err != nil {
			return err
		}
		var input setOpenWorldPresentationInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		presentation, err := normalizeOpenWorldPresentation(input.Presentation)
		if err != nil {
			return err
		}
		plan.Input["presentation"] = presentation
	case "set_experience":
		// The key must be present, but an explicit null clears the experience.
		if err := validateInputKeys(raw, nil, []string{"experience"}); err != nil {
			return err
		}
		var present map[string]json.RawMessage
		if err := decodeStrict(raw, &present); err != nil {
			return fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		if _, ok := present["experience"]; !ok {
			return fmt.Errorf("%w: input.experience", ErrInvalidRequest)
		}
		var input setOpenWorldExperienceInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		// Full reference validation needs the current world and happens in
		// applyOpenWorldAction, after the row lock. This phase only checks the
		// exact input shape so proposals cannot smuggle other keys.
		plan.Input["experience"] = input.Experience
	case "invoke_behavior":
		if err := validateInputKeys(raw, []string{"behavior_id"},
			[]string{"behavior_id", "entity_id", "value"}); err != nil {
			return err
		}
		var input invokeOpenWorldBehaviorInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if !worldcap.ValidLocalID(input.BehaviorID) {
			return fmt.Errorf("%w: input.behavior_id", ErrInvalidRequest)
		}
		if input.EntityID != "" {
			if _, err := uuid.Parse(input.EntityID); err != nil {
				return fmt.Errorf("%w: input.entity_id", ErrInvalidRequest)
			}
		}
		plan.Input["behavior_id"] = input.BehaviorID
		if input.EntityID != "" {
			plan.Input["entity_id"] = input.EntityID
		}
		if input.Value != nil {
			encoded, err := json.Marshal(input.Value)
			if err != nil || len(encoded) > 4096 {
				return fmt.Errorf("%w: input.value", ErrPayloadTooLarge)
			}
			plan.Input["value"] = input.Value
		}
	case "reshape_world":
		if err := validateInputKeys(raw, []string{"content"},
			[]string{"note", "content"}); err != nil {
			return err
		}
		var input reshapeOpenWorldInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		note := strings.TrimSpace(input.Note)
		if runeLength(note) > 500 {
			return fmt.Errorf("%w: input.note", ErrInvalidRequest)
		}
		if err := ValidateOpenWorldState(&input.Content); err != nil {
			return err
		}
		plan.Input["note"] = note
		plan.Input["content"] = input.Content
		plan.EventInput = map[string]any{
			"note": note, "kind_count": len(input.Content.Kinds),
			"node_count": len(input.Content.Nodes), "link_count": len(input.Content.Links),
			"variable_count": len(input.Content.Variables),
			"experience":     input.Content.Experience != nil,
		}
	case "custom_action":
		if err := validateInputKeys(raw, []string{"surface_id", "event"},
			[]string{"surface_id", "event", "node_id", "key", "value"}); err != nil {
			return err
		}
		var input customActionInput
		if err := decodeOpenWorldInput(raw, &input); err != nil {
			return err
		}
		if !worldcap.ValidLocalID(input.SurfaceID) {
			return fmt.Errorf("%w: input.surface_id", ErrInvalidRequest)
		}
		switch input.Event {
		case "click", "key", "input", "drag":
		default:
			return fmt.Errorf("%w: input.event", ErrInvalidRequest)
		}
		if input.NodeID != "" && !worldcap.ValidLocalID(input.NodeID) {
			return fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		if runeLength(input.Key) > 20 {
			return fmt.Errorf("%w: input.key", ErrInvalidRequest)
		}
		if input.Value != nil {
			encoded, err := json.Marshal(input.Value)
			if err != nil || len(encoded) > 4096 {
				return fmt.Errorf("%w: input.value", ErrPayloadTooLarge)
			}
			plan.Input["value"] = input.Value
		}
		plan.Input["surface_id"] = input.SurfaceID
		plan.Input["event"] = input.Event
		if input.NodeID != "" {
			plan.Input["node_id"] = input.NodeID
		}
		if input.Key != "" {
			plan.Input["key"] = input.Key
		}
	default:
		return fmt.Errorf("%w: action", ErrInvalidRequest)
	}
	return nil
}

func findOpenWorldKind(state *OpenWorldState, kindID string) *OpenWorldKind {
	for index := range state.Kinds {
		if state.Kinds[index].ID == kindID {
			return &state.Kinds[index]
		}
	}
	return nil
}

func findOpenWorldNode(state *OpenWorldState, nodeID string) *OpenWorldNode {
	for index := range state.Nodes {
		if state.Nodes[index].ID == nodeID {
			return &state.Nodes[index]
		}
	}
	return nil
}

func openWorldFieldFor(state *OpenWorldState, node *OpenWorldNode, key string) *OpenWorldField {
	kind := findOpenWorldKind(state, node.KindID)
	if kind == nil {
		return nil
	}
	for index := range kind.Fields {
		if kind.Fields[index].Key == key {
			return &kind.Fields[index]
		}
	}
	return nil
}

// applyOpenWorldRules runs one pass over rules triggered by nodeID. Effects
// apply in rule and effect order and never trigger further rules.
func applyOpenWorldRules(state *OpenWorldState, nodeID string) []OpenWorldAppliedEffect {
	applied := make([]OpenWorldAppliedEffect, 0)
	for _, rule := range state.Rules {
		if rule.TriggerID != nodeID {
			continue
		}
		if rule.Condition != nil && !openWorldConditionHolds(state, rule.Condition) {
			continue
		}
		for _, effect := range rule.Effects {
			node := findOpenWorldNode(state, effect.NodeID)
			if node == nil {
				continue
			}
			if effect.Field == OpenWorldStatusField {
				switch effect.Op {
				case OpenWorldOpSet:
					value, _ := effect.Value.(string)
					if value != OpenWorldStatusOpen && value != OpenWorldStatusDone {
						continue
					}
					node.Status = value
					applied = append(applied, OpenWorldAppliedEffect{
						RuleID: rule.ID, NodeID: node.ID, Field: effect.Field,
						Op: effect.Op, Value: value,
					})
				case OpenWorldOpToggle:
					next := OpenWorldStatusDone
					if node.Status == OpenWorldStatusDone {
						next = OpenWorldStatusOpen
					}
					node.Status = next
					applied = append(applied, OpenWorldAppliedEffect{
						RuleID: rule.ID, NodeID: node.ID, Field: effect.Field,
						Op: effect.Op, Value: next,
					})
				}
				continue
			}
			field := openWorldFieldFor(state, node, effect.Field)
			if field == nil {
				continue
			}
			switch field.Type {
			case OpenWorldFieldText:
				value, ok := effect.Value.(string)
				if !ok || effect.Op != OpenWorldOpSet {
					continue
				}
				if node.Values == nil {
					node.Values = map[string]any{}
				}
				node.Values[field.Key] = value
				applied = append(applied, OpenWorldAppliedEffect{
					RuleID: rule.ID, NodeID: node.ID, Field: field.Key,
					Op: effect.Op, Value: value,
				})
			case OpenWorldFieldNumber:
				value, ok := effect.Value.(float64)
				if !ok {
					continue
				}
				if node.Values == nil {
					node.Values = map[string]any{}
				}
				current, _ := node.Values[field.Key].(float64)
				switch effect.Op {
				case OpenWorldOpSet:
					node.Values[field.Key] = value
				case OpenWorldOpAdd:
					node.Values[field.Key] = current + value
				default:
					continue
				}
				applied = append(applied, OpenWorldAppliedEffect{
					RuleID: rule.ID, NodeID: node.ID, Field: field.Key,
					Op: effect.Op, Value: node.Values[field.Key],
				})
			case OpenWorldFieldFlag:
				if node.Values == nil {
					node.Values = map[string]any{}
				}
				current, _ := node.Values[field.Key].(bool)
				switch effect.Op {
				case OpenWorldOpSet:
					value, ok := effect.Value.(bool)
					if !ok {
						continue
					}
					node.Values[field.Key] = value
				case OpenWorldOpToggle:
					node.Values[field.Key] = !current
				default:
					continue
				}
				applied = append(applied, OpenWorldAppliedEffect{
					RuleID: rule.ID, NodeID: node.ID, Field: field.Key,
					Op: effect.Op, Value: node.Values[field.Key],
				})
			}
		}
	}
	return applied
}

// openWorldConditionHolds evaluates one bounded comparison against current
// values. Missing values read as zero/false/empty, never as an error, so a
// condition is total over the declared field space.
func openWorldConditionHolds(state *OpenWorldState, condition *OpenWorldCondition) bool {
	node := findOpenWorldNode(state, condition.NodeID)
	if node == nil {
		return false
	}
	if condition.Field == OpenWorldStatusField {
		return compareOpenWorldString(node.Status, condition.Op, condition.Value)
	}
	field := openWorldFieldFor(state, node, condition.Field)
	if field == nil {
		return false
	}
	switch field.Type {
	case OpenWorldFieldText:
		current, _ := node.Values[field.Key].(string)
		return compareOpenWorldString(current, condition.Op, condition.Value)
	case OpenWorldFieldNumber:
		current, _ := node.Values[field.Key].(float64)
		target, ok := condition.Value.(float64)
		if !ok {
			return false
		}
		switch condition.Op {
		case OpenWorldCondEq:
			return current == target
		case OpenWorldCondNe:
			return current != target
		case OpenWorldCondGt:
			return current > target
		case OpenWorldCondGte:
			return current >= target
		case OpenWorldCondLt:
			return current < target
		case OpenWorldCondLte:
			return current <= target
		}
		return false
	case OpenWorldFieldFlag:
		current, _ := node.Values[field.Key].(bool)
		switch condition.Op {
		case OpenWorldCondTruthy:
			return current
		case OpenWorldCondFalsy:
			return !current
		case OpenWorldCondEq, OpenWorldCondNe:
			target, ok := condition.Value.(bool)
			if !ok {
				return false
			}
			if condition.Op == OpenWorldCondEq {
				return current == target
			}
			return current != target
		}
		return false
	}
	return false
}

func compareOpenWorldString(current, op string, value any) bool {
	target, ok := value.(string)
	if !ok {
		return false
	}
	switch op {
	case OpenWorldCondEq:
		return current == target
	case OpenWorldCondNe:
		return current != target
	default:
		return false
	}
}

// applyOpenWorldAction mutates a copy of the state for one validated action.
func applyOpenWorldAction(stateRaw, sharedRaw json.RawMessage, plan *ActionPlan) (json.RawMessage, json.RawMessage, string, error) {
	var state OpenWorldState
	if err := decodeStrict(stateRaw, &state); err != nil {
		return nil, nil, "", fmt.Errorf("%w: state", ErrInvalidRequest)
	}
	switch plan.Action {
	case "set_title":
		state.Title = plan.Input["title"].(string)
	case "set_premise":
		state.Premise = plan.Input["premise"].(string)
	case "add_tag":
		tag := plan.Input["tag"].(string)
		for _, existing := range state.Tags {
			if existing == tag {
				return nil, nil, "", fmt.Errorf("%w: tag already exists", ErrConflict)
			}
		}
		if len(state.Tags) >= maxTags {
			return nil, nil, "", fmt.Errorf("%w: tag limit reached", ErrConflict)
		}
		state.Tags = append(state.Tags, tag)
		sort.Strings(state.Tags)
	case "remove_tag":
		tag := plan.Input["tag"].(string)
		found := false
		next := make([]string, 0, len(state.Tags))
		for _, existing := range state.Tags {
			if existing == tag {
				found = true
				continue
			}
			next = append(next, existing)
		}
		if !found {
			return nil, nil, "", fmt.Errorf("%w: input.tag", ErrInvalidRequest)
		}
		state.Tags = next
	case "add_kind":
		var input addOpenWorldKindInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		if len(state.Kinds) >= maxOpenWorldKinds {
			return nil, nil, "", fmt.Errorf("%w: kind limit reached", ErrConflict)
		}
		id, err := uuid.NewV7()
		if err != nil {
			return nil, nil, "", err
		}
		appearance := DefaultOpenWorldAppearance()
		if input.Appearance != nil {
			normalized, err := normalizeOpenWorldAppearance(*input.Appearance)
			if err != nil {
				return nil, nil, "", err
			}
			appearance = normalized
		}
		fields := normalizeOpenWorldFieldInputs(input.Fields)
		fieldMap := make(map[string]OpenWorldField, len(fields))
		for _, field := range fields {
			fieldMap[field.Key] = field
		}
		bindings, err := normalizeOpenWorldBindings(input.Bindings, fieldMap)
		if err != nil {
			return nil, nil, "", err
		}
		state.Kinds = append(state.Kinds, OpenWorldKind{
			ID: id.String(), Name: strings.TrimSpace(input.Name), Hue: input.Hue,
			Fields: fields, Appearance: appearance, Bindings: bindings,
		})
	case "update_kind":
		var input updateOpenWorldKindInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		kind := findOpenWorldKind(&state, input.KindID)
		if kind == nil {
			return nil, nil, "", fmt.Errorf("%w: input.kind_id", ErrInvalidRequest)
		}
		if input.Name != nil {
			kind.Name = strings.TrimSpace(*input.Name)
		}
		if input.Hue != nil {
			kind.Hue = *input.Hue
		}
		if input.Fields != nil {
			nextFields := make(map[string]bool, len(*input.Fields))
			for _, field := range *input.Fields {
				nextFields[strings.TrimSpace(field.Key)] = true
			}
			for _, existing := range kind.Fields {
				if !nextFields[existing.Key] &&
					worldcap.ExperienceReferencesEventField(state.Experience, existing.Key) {
					return nil, nil, "", fmt.Errorf(
						"%w: field %q is used by an event-scoped experience action", ErrConflict, existing.Key)
				}
			}
			kind.Fields = normalizeOpenWorldFieldInputs(*input.Fields)
			allowed := make(map[string]bool, len(kind.Fields))
			for _, field := range kind.Fields {
				allowed[field.Key] = true
			}
			for index := range state.Nodes {
				if state.Nodes[index].KindID != kind.ID {
					continue
				}
				for key := range state.Nodes[index].Values {
					if !allowed[key] {
						delete(state.Nodes[index].Values, key)
					}
				}
			}
			// A binding to a field that no longer exists would make the world
			// invalid. Drop only those bindings; everything else is preserved.
			kept := make([]OpenWorldBinding, 0, len(kind.Bindings))
			for _, binding := range kind.Bindings {
				if allowed[binding.Field] {
					kept = append(kept, binding)
				}
			}
			kind.Bindings = kept
		}
		if input.Appearance != nil {
			appearance, err := normalizeOpenWorldAppearance(*input.Appearance)
			if err != nil {
				return nil, nil, "", err
			}
			kind.Appearance = appearance
		}
		if input.Bindings != nil {
			fieldMap := make(map[string]OpenWorldField, len(kind.Fields))
			for _, field := range kind.Fields {
				fieldMap[field.Key] = field
			}
			bindings, err := normalizeOpenWorldBindings(*input.Bindings, fieldMap)
			if err != nil {
				return nil, nil, "", err
			}
			kind.Bindings = bindings
		}
	case "remove_kind":
		var input removeOpenWorldKindInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		kind := findOpenWorldKind(&state, input.KindID)
		if kind == nil {
			return nil, nil, "", fmt.Errorf("%w: input.kind_id", ErrInvalidRequest)
		}
		if worldcap.ExperienceReferencesKind(state.Experience, kind.ID) {
			return nil, nil, "", fmt.Errorf("%w: kind is used by the experience", ErrConflict)
		}
		for _, node := range state.Nodes {
			if node.KindID == kind.ID {
				return nil, nil, "", fmt.Errorf("%w: kind is still in use", ErrConflict)
			}
		}
		next := make([]OpenWorldKind, 0, len(state.Kinds)-1)
		for _, existing := range state.Kinds {
			if existing.ID != kind.ID {
				next = append(next, existing)
			}
		}
		state.Kinds = next
	case "add_node":
		var input addOpenWorldNodeInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		if findOpenWorldKind(&state, input.KindID) == nil {
			return nil, nil, "", fmt.Errorf("%w: input.kind_id", ErrInvalidRequest)
		}
		if len(state.Nodes) >= maxOpenWorldNodes {
			return nil, nil, "", fmt.Errorf("%w: node limit reached", ErrConflict)
		}
		id, err := uuid.NewV7()
		if err != nil {
			return nil, nil, "", err
		}
		x, y := defaultOpenWorldPosition(len(state.Nodes), len(state.Nodes)+1)
		if input.X != nil {
			x = *input.X
		}
		if input.Y != nil {
			y = *input.Y
		}
		state.Nodes = append(state.Nodes, OpenWorldNode{
			ID: id.String(), KindID: input.KindID, Name: strings.TrimSpace(input.Name),
			Text: input.Text, Values: map[string]any{}, Status: OpenWorldStatusOpen,
			X: x, Y: y,
		})
	case "update_node":
		var input updateOpenWorldNodeInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		node := findOpenWorldNode(&state, input.NodeID)
		if node == nil {
			return nil, nil, "", fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		if input.Name != nil {
			node.Name = strings.TrimSpace(*input.Name)
		}
		if input.Text != nil {
			node.Text = *input.Text
		}
		if input.X != nil {
			node.X = *input.X
		}
		if input.Y != nil {
			node.Y = *input.Y
		}
	case "remove_node":
		var input removeOpenWorldNodeInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		node := findOpenWorldNode(&state, input.NodeID)
		if node == nil {
			return nil, nil, "", fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		if worldcap.ExperienceReferencesEntity(state.Experience, node.ID) {
			return nil, nil, "", fmt.Errorf("%w: thing is used by the experience", ErrConflict)
		}
		nextNodes := make([]OpenWorldNode, 0, len(state.Nodes)-1)
		for _, existing := range state.Nodes {
			if existing.ID != node.ID {
				nextNodes = append(nextNodes, existing)
			}
		}
		state.Nodes = nextNodes
		nextLinks := make([]OpenWorldLink, 0, len(state.Links))
		for _, link := range state.Links {
			if link.From != node.ID && link.To != node.ID {
				nextLinks = append(nextLinks, link)
			}
		}
		state.Links = nextLinks
		nextRules := make([]OpenWorldRule, 0, len(state.Rules))
		for _, rule := range state.Rules {
			if rule.TriggerID == node.ID {
				continue
			}
			// A guard that reads a removed thing cannot be evaluated. The whole
			// rule is removed with it, just as a rule whose trigger is removed.
			if rule.Condition != nil && rule.Condition.NodeID == node.ID {
				continue
			}
			effects := make([]OpenWorldEffect, 0, len(rule.Effects))
			for _, effect := range rule.Effects {
				if effect.NodeID != node.ID {
					effects = append(effects, effect)
				}
			}
			if len(effects) == 0 {
				continue
			}
			rule.Effects = effects
			nextRules = append(nextRules, rule)
		}
		state.Rules = nextRules
	case "move_node":
		var input moveOpenWorldNodeInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		node := findOpenWorldNode(&state, input.NodeID)
		if node == nil {
			return nil, nil, "", fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		node.X = input.X
		node.Y = input.Y
	case "set_node_field":
		var input setOpenWorldFieldInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		node := findOpenWorldNode(&state, input.NodeID)
		if node == nil {
			return nil, nil, "", fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		field := openWorldFieldFor(&state, node, input.Field)
		if field == nil {
			return nil, nil, "", fmt.Errorf("%w: input.field", ErrInvalidRequest)
		}
		if err := validateOpenWorldValue(*field, input.Value, "input.value"); err != nil {
			return nil, nil, "", err
		}
		if node.Values == nil {
			node.Values = map[string]any{}
		}
		node.Values[field.Key] = input.Value
		plan.Effects = append(plan.Effects, applyOpenWorldRules(&state, node.ID)...)
	case "set_node_status":
		var input setOpenWorldStatusInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		node := findOpenWorldNode(&state, input.NodeID)
		if node == nil {
			return nil, nil, "", fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		node.Status = input.Status
		plan.Effects = append(plan.Effects, applyOpenWorldRules(&state, node.ID)...)
	case "touch_node":
		var input touchOpenWorldNodeInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		node := findOpenWorldNode(&state, input.NodeID)
		if node == nil {
			return nil, nil, "", fmt.Errorf("%w: input.node_id", ErrInvalidRequest)
		}
		if node.Status == OpenWorldStatusDone {
			node.Status = OpenWorldStatusOpen
		} else {
			node.Status = OpenWorldStatusDone
		}
		plan.Effects = append(plan.Effects, applyOpenWorldRules(&state, node.ID)...)
	case "add_link":
		var input addOpenWorldLinkInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		if findOpenWorldNode(&state, input.From) == nil || findOpenWorldNode(&state, input.To) == nil {
			return nil, nil, "", fmt.Errorf("%w: input.endpoints", ErrInvalidRequest)
		}
		for _, link := range state.Links {
			if link.From == input.From && link.To == input.To {
				return nil, nil, "", fmt.Errorf("%w: link already exists", ErrConflict)
			}
		}
		if len(state.Links) >= maxOpenWorldLinks {
			return nil, nil, "", fmt.Errorf("%w: link limit reached", ErrConflict)
		}
		id, err := uuid.NewV7()
		if err != nil {
			return nil, nil, "", err
		}
		state.Links = append(state.Links, OpenWorldLink{
			ID: id.String(), From: input.From, To: input.To, Label: input.Label,
		})
	case "remove_link":
		var input removeOpenWorldLinkInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		found := false
		next := make([]OpenWorldLink, 0, len(state.Links))
		for _, link := range state.Links {
			if link.ID == input.LinkID {
				found = true
				continue
			}
			next = append(next, link)
		}
		if !found {
			return nil, nil, "", fmt.Errorf("%w: input.link_id", ErrInvalidRequest)
		}
		state.Links = next
	case "add_rule":
		var input addOpenWorldRuleInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		if findOpenWorldNode(&state, input.TriggerID) == nil {
			return nil, nil, "", fmt.Errorf("%w: input.trigger_id", ErrInvalidRequest)
		}
		if len(state.Rules) >= maxOpenWorldRules {
			return nil, nil, "", fmt.Errorf("%w: rule limit reached", ErrConflict)
		}
		id, err := uuid.NewV7()
		if err != nil {
			return nil, nil, "", err
		}
		var condition *OpenWorldCondition
		if input.Condition != nil {
			condition = &OpenWorldCondition{
				NodeID: input.Condition.NodeID, Field: input.Condition.Field,
				Op: input.Condition.Op, Value: input.Condition.Value,
			}
			if err := validateOpenWorldCondition(&state, condition, "input.condition"); err != nil {
				return nil, nil, "", err
			}
		}
		effects := make([]OpenWorldEffect, 0, len(input.Effects))
		for _, effect := range input.Effects {
			if findOpenWorldNode(&state, effect.NodeID) == nil {
				return nil, nil, "", fmt.Errorf("%w: input.effects.node_id", ErrInvalidRequest)
			}
			effects = append(effects, OpenWorldEffect{
				NodeID: effect.NodeID, Field: effect.Field, Op: effect.Op, Value: effect.Value,
			})
		}
		state.Rules = append(state.Rules, OpenWorldRule{
			ID: id.String(), TriggerID: input.TriggerID,
			Condition: condition, Effects: effects,
		})
	case "remove_rule":
		var input removeOpenWorldRuleInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		found := false
		next := make([]OpenWorldRule, 0, len(state.Rules))
		for _, rule := range state.Rules {
			if rule.ID == input.RuleID {
				found = true
				continue
			}
			next = append(next, rule)
		}
		if !found {
			return nil, nil, "", fmt.Errorf("%w: input.rule_id", ErrInvalidRequest)
		}
		state.Rules = next
	case "set_views":
		var input setOpenWorldViewsInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		views, err := normalizeOpenWorldViews(input.Views)
		if err != nil {
			return nil, nil, "", err
		}
		state.Views = views
	case "set_presentation":
		var input setOpenWorldPresentationInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		presentation, err := normalizeOpenWorldPresentation(input.Presentation)
		if err != nil {
			return nil, nil, "", err
		}
		state.Presentation = presentation
	case "set_experience":
		var input setOpenWorldExperienceInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		state.Experience = input.Experience
	case "reshape_world":
		var input reshapeOpenWorldInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		if strings.TrimSpace(input.Content.Title) != state.Title {
			input.Content.Title = state.Title
		}
		state = input.Content
	case "custom_action":
		var input customActionInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		program, ok := worldcap.CustomProgramFor(state.Experience, input.SurfaceID)
		if !ok {
			return nil, nil, "", fmt.Errorf("%w: input.surface_id", ErrInvalidRequest)
		}
		applied, err := worldcap.ExecuteCustomEvent(program, worldcap.CustomEvent{
			Event: input.Event, NodeID: input.NodeID, Key: input.Key, Value: input.Value,
		}, openWorldCapabilityWorld{state: &state})
		if err != nil {
			return nil, nil, "", fmt.Errorf("%w: custom action", ErrInvalidRequest)
		}
		affected := make([]string, 0, len(applied))
		seen := map[string]bool{}
		for _, effect := range applied {
			plan.Effects = append(plan.Effects, OpenWorldAppliedEffect{
				Source: "custom", RuleID: "custom", NodeID: effect.EntityID,
				Field: effect.Field, Op: effect.Action, Value: effect.Value,
			})
			if effect.EntityID != "" && !seen[effect.EntityID] {
				seen[effect.EntityID] = true
				affected = append(affected, effect.EntityID)
			}
		}
		sort.Strings(affected)
		for _, entityID := range affected {
			plan.Effects = append(plan.Effects, applyOpenWorldRules(&state, entityID)...)
		}
	case "invoke_behavior":
		var input invokeOpenWorldBehaviorInput
		if err := decodeOpenWorldInput(plan.RawInput, &input); err != nil {
			return nil, nil, "", err
		}
		behavior := worldcap.FindBehavior(state.Experience, input.BehaviorID)
		if behavior == nil {
			return nil, nil, "", fmt.Errorf("%w: input.behavior_id", ErrInvalidRequest)
		}
		if input.EntityID != "" && findOpenWorldNode(&state, input.EntityID) == nil {
			return nil, nil, "", fmt.Errorf("%w: input.entity_id", ErrInvalidRequest)
		}
		applied, err := worldcap.ExecuteBehavior(
			worldcap.Default(), behavior,
			worldcap.Event{EntityID: input.EntityID, Value: input.Value},
			openWorldCapabilityWorld{state: &state},
		)
		if err != nil {
			return nil, nil, "", fmt.Errorf("%w: experience behavior", ErrInvalidRequest)
		}
		for _, effect := range applied {
			plan.Effects = append(plan.Effects, OpenWorldAppliedEffect{
				Source: "behavior",
				RuleID: behavior.ID, NodeID: effect.EntityID, Field: effect.Field,
				Op: effect.Action, Value: effect.Value,
			})
		}
		// A behavior is another way to make the same persisted changes as
		// set_node_field / set_node_status / touch_node. Run one legacy rule
		// pass for each changed thing so both interaction surfaces have the
		// same change semantics. Rules still never chain.
		affected := make([]string, 0, len(applied))
		seen := make(map[string]bool, len(applied))
		for _, effect := range applied {
			if effect.EntityID == "" || seen[effect.EntityID] {
				continue
			}
			seen[effect.EntityID] = true
			affected = append(affected, effect.EntityID)
		}
		sort.Strings(affected)
		for _, entityID := range affected {
			plan.Effects = append(plan.Effects, applyOpenWorldRules(&state, entityID)...)
		}
	default:
		return nil, nil, "", fmt.Errorf("%w: action", ErrInvalidRequest)
	}
	if err := ValidateOpenWorldState(&state); err != nil {
		return nil, nil, "", err
	}
	// Event input must record the normalized experience, not the raw request.
	// A document can pass the per-props checks while its raw JSON is much larger
	// than the persisted/normalized form.
	if plan.Action == "set_experience" {
		plan.Input["experience"] = state.Experience
	}
	encoded, err := marshalState(state)
	if err != nil {
		return nil, nil, "", err
	}
	return encoded, sharedRaw, "", nil
}
