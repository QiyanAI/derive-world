// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// stateCapability is the built-in behavior module. It changes declared entity
// fields through the same bounded world state the rest of the product uses.
// New behavior modules can be registered without changing this one or the
// world core protocol.
type stateCapability struct{}

func (stateCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.state", Kind: "behavior", Version: 1,
		Title:       Localized{En: "Change state", Zh: "改变状态"},
		Description: Localized{En: "Set, add to, or toggle a declared field or status.", Zh: "设置、累加或切换一个已声明字段或状态。"},
		Actions: []ActionDescriptor{
			{
				ID: "set", Title: Localized{En: "Set", Zh: "设为"},
				Description: Localized{En: "Replace a field with a literal value or the event value.", Zh: "把字段设为字面量或事件值。"},
				Params:      stateParamsSchema(false),
			},
			{
				ID: "add", Title: Localized{En: "Add", Zh: "增加"},
				Description: Localized{En: "Add a number to a numeric field.", Zh: "给数字字段加上一个数值。"},
				Params:      stateParamsSchema(true),
			},
			{
				ID: "toggle", Title: Localized{En: "Toggle", Zh: "切换"},
				Description: Localized{En: "Flip an on/off field or the open/done status.", Zh: "切换开关字段或 open/done 状态。"},
				Params:      stateParamsSchema(false),
			},
		},
	}
}

func stateParamsSchema(requireValue bool) *ParamSchema {
	schema := &ParamSchema{
		Type: "object",
		Properties: map[string]*ParamProperty{
			"entity_id": {
				Type: "string", Format: "entity",
				Title:       Localized{En: "Thing", Zh: "存在"},
				Description: Localized{En: "An existing thing, or $event.entity for the control's thing.", Zh: "已有存在，或使用 $event.entity 表示控件当前的存在。"},
			},
			"field": {
				Type: "string", Format: "field",
				Title: Localized{En: "Field", Zh: "字段"},
			},
			"value": {
				Type: "string", Title: Localized{En: "Value", Zh: "值"},
				Description: Localized{En: "A literal value or $event.value.", Zh: "字面量或 $event.value。"},
			},
		},
		Required: []string{"entity_id", "field"},
	}
	if requireValue {
		schema.Required = append(schema.Required, "value")
	}
	return schema
}

type stateParams struct {
	EntityID string `json:"entity_id"`
	Field    string `json:"field"`
	Value    any    `json:"value,omitempty"`
}

func (stateCapability) ValidateAction(action string, raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	if action != "set" && action != "add" && action != "toggle" {
		return nil, fmt.Errorf("%w: state action", ErrInvalid)
	}
	var params stateParams
	if err := decodeStrict(raw, &params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Field) == "" || runeLength(params.Field) > 40 {
		return nil, fmt.Errorf("%w: state.field", ErrInvalid)
	}
	entityID := params.EntityID
	fieldType := ""
	switch entityID {
	case "$event.entity":
		if !ctx.AllowEvent {
			return nil, fmt.Errorf("%w: state.entity_id", ErrInvalid)
		}
		if params.Field == "status" {
			fieldType = "status"
		} else {
			fieldType = "event"
		}
	case "$item", "":
		return nil, fmt.Errorf("%w: state.entity_id", ErrInvalid)
	default:
		if !ctx.World.HasEntity(entityID) {
			return nil, fmt.Errorf("%w: state.entity_id", ErrInvalid)
		}
		value, ok := ctx.World.FieldType(entityID, params.Field)
		if !ok {
			return nil, fmt.Errorf("%w: state.field", ErrInvalid)
		}
		fieldType = value
	}
	switch action {
	case "set":
		if params.Field == "status" {
			value, ok := params.Value.(string)
			if !ok || (value != "open" && value != "done") {
				return nil, fmt.Errorf("%w: state.value", ErrInvalid)
			}
			break
		}
		if isEventValue(params.Value) && ctx.AllowEvent {
			break
		}
		if err := validateStateValue(fieldType, params.Value); err != nil {
			return nil, err
		}
	case "add":
		if params.Field == "status" || (fieldType != "" && fieldType != "number" && fieldType != "event") {
			return nil, fmt.Errorf("%w: state.add", ErrInvalid)
		}
		if isEventValue(params.Value) && ctx.AllowEvent {
			break
		}
		value, ok := params.Value.(float64)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("%w: state.value", ErrInvalid)
		}
	case "toggle":
		if params.Value != nil {
			return nil, fmt.Errorf("%w: state.value", ErrInvalid)
		}
		if params.Field != "status" && fieldType != "flag" && fieldType != "event" {
			return nil, fmt.Errorf("%w: state.toggle", ErrInvalid)
		}
	}
	return json.Marshal(params)
}

func validateStateValue(fieldType string, value any) error {
	switch fieldType {
	case "text":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%w: state.value", ErrInvalid)
		}
	case "number":
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("%w: state.value", ErrInvalid)
		}
	case "flag":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%w: state.value", ErrInvalid)
		}
	case "event":
		switch value.(type) {
		case string, float64, bool:
		default:
			return fmt.Errorf("%w: state.value", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: state.value", ErrInvalid)
	}
	return nil
}

func (stateCapability) Execute(action string, raw json.RawMessage, event Event, world WorldAccess) ([]AppliedAction, error) {
	var params stateParams
	if err := decodeStrict(raw, &params); err != nil {
		return nil, fmt.Errorf("%w: state params", ErrInvalid)
	}
	entityID := params.EntityID
	if entityID == "$event.entity" {
		entityID = event.EntityID
	}
	if entityID == "" || !world.HasEntity(entityID) {
		return nil, fmt.Errorf("%w: state entity", ErrInvalid)
	}
	value := params.Value
	if isEventValue(value) {
		value = event.Value
	}
	if params.Field == "status" {
		current, _ := world.Status(entityID)
		switch action {
		case "set":
			next, ok := value.(string)
			if !ok || (next != "open" && next != "done") {
				return nil, fmt.Errorf("%w: state status value", ErrInvalid)
			}
			if err := world.SetStatus(entityID, next); err != nil {
				return nil, err
			}
			return []AppliedAction{{Capability: "core.state", Action: action, EntityID: entityID, Field: "status", Value: next}}, nil
		case "toggle":
			next := "done"
			if current == "done" {
				next = "open"
			}
			if err := world.SetStatus(entityID, next); err != nil {
				return nil, err
			}
			return []AppliedAction{{Capability: "core.state", Action: action, EntityID: entityID, Field: "status", Value: next}}, nil
		default:
			return nil, fmt.Errorf("%w: state status action", ErrInvalid)
		}
	}
	fieldType, ok := world.FieldType(entityID, params.Field)
	if !ok {
		return nil, fmt.Errorf("%w: state field", ErrInvalid)
	}
	current, hasValue := world.Value(entityID, params.Field)
	switch action {
	case "set":
		if err := validateStateValue(fieldType, value); err != nil {
			return nil, err
		}
		if err := world.SetValue(entityID, params.Field, value); err != nil {
			return nil, err
		}
		return []AppliedAction{{Capability: "core.state", Action: action, EntityID: entityID, Field: params.Field, Value: value}}, nil
	case "add":
		if fieldType != "number" {
			return nil, fmt.Errorf("%w: state add field", ErrInvalid)
		}
		amount, ok := value.(float64)
		if !ok || math.IsNaN(amount) || math.IsInf(amount, 0) {
			return nil, fmt.Errorf("%w: state add value", ErrInvalid)
		}
		base := float64(0)
		if hasValue {
			if number, ok := current.(float64); ok {
				base = number
			}
		}
		next := base + amount
		if math.IsNaN(next) || math.IsInf(next, 0) {
			return nil, fmt.Errorf("%w: state add result", ErrInvalid)
		}
		if err := world.SetValue(entityID, params.Field, next); err != nil {
			return nil, err
		}
		return []AppliedAction{{Capability: "core.state", Action: action, EntityID: entityID, Field: params.Field, Value: next}}, nil
	case "toggle":
		if fieldType != "flag" {
			return nil, fmt.Errorf("%w: state toggle field", ErrInvalid)
		}
		currentFlag, _ := current.(bool)
		next := !currentFlag
		if err := world.SetValue(entityID, params.Field, next); err != nil {
			return nil, err
		}
		return []AppliedAction{{Capability: "core.state", Action: action, EntityID: entityID, Field: params.Field, Value: next}}, nil
	}
	return nil, fmt.Errorf("%w: state action", ErrInvalid)
}
