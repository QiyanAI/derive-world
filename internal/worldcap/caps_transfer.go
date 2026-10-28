// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// transferCapability is a second behavior module. It exists to prove that a
// new interaction capability can be added as a module, registered once, and
// reused by any world without changing Experience, ViewNode, Behavior, Action,
// Condition or any objects-package branch.
type transferCapability struct{}

func (transferCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.transfer", Kind: "behavior", Version: 1,
		Title:       Localized{En: "Transfer", Zh: "转移"},
		Description: Localized{En: "Move a non-negative amount of a numeric field from one thing to another.", Zh: "把一个非负数值从一种存在转移到另一种存在。"},
		Actions: []ActionDescriptor{{
			ID: "move", Title: Localized{En: "Move", Zh: "转移"},
			Description: Localized{En: "Subtract from the source and add to the target, never below zero.", Zh: "从来源扣除并加到目标；来源不会低于零。"},
			Params: &ParamSchema{
				Type: "object",
				Properties: map[string]*ParamProperty{
					"from_entity": {
						Type: "string", Format: "entity",
						Title: Localized{En: "From", Zh: "来源"},
					},
					"to_entity": {
						Type: "string", Format: "entity",
						Title: Localized{En: "To", Zh: "目标"},
					},
					"field": {
						Type: "string", Format: "field",
						Title: Localized{En: "Number field", Zh: "数字字段"},
					},
					"amount": {
						Type: "number", Title: Localized{En: "Amount", Zh: "数量"},
						Minimum: floatPointer(0), Maximum: floatPointer(1000000),
					},
				},
				Required: []string{"from_entity", "to_entity", "field", "amount"},
			},
		}},
	}
}

type transferParams struct {
	FromEntity string  `json:"from_entity"`
	ToEntity   string  `json:"to_entity"`
	Field      string  `json:"field"`
	Amount     float64 `json:"amount"`
}

func (transferCapability) ValidateAction(action string, raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	if action != "move" {
		return nil, fmt.Errorf("%w: transfer action", ErrInvalid)
	}
	var params transferParams
	if err := decodeStrict(raw, &params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Field) == "" || runeLength(params.Field) > 40 {
		return nil, fmt.Errorf("%w: transfer.field", ErrInvalid)
	}
	if !finite(params.Amount) || params.Amount < 0 || params.Amount > 1000000 {
		return nil, fmt.Errorf("%w: transfer.amount", ErrInvalid)
	}
	fromKind, err := transferEntityKind(ctx, params.FromEntity, true)
	if err != nil {
		return nil, fmt.Errorf("%w: transfer.from_entity", ErrInvalid)
	}
	toKind, err := transferEntityKind(ctx, params.ToEntity, true)
	if err != nil {
		return nil, fmt.Errorf("%w: transfer.to_entity", ErrInvalid)
	}
	if params.FromEntity == params.ToEntity && params.FromEntity != "$event.entity" {
		return nil, fmt.Errorf("%w: transfer endpoints", ErrInvalid)
	}
	if fromKind.ID != "" {
		fieldType, ok := fromKind.Fields[params.Field]
		if !ok || fieldType != "number" {
			return nil, fmt.Errorf("%w: transfer.field", ErrInvalid)
		}
	}
	if toKind.ID != "" {
		fieldType, ok := toKind.Fields[params.Field]
		if !ok || fieldType != "number" {
			return nil, fmt.Errorf("%w: transfer.field", ErrInvalid)
		}
	}
	return json.Marshal(params)
}

func transferEntityKind(ctx ValidateContext, entityID string, allowEvent bool) (KindContext, error) {
	switch entityID {
	case "$event.entity":
		if !allowEvent || !ctx.AllowEvent {
			return KindContext{}, fmt.Errorf("%w: transfer event entity", ErrInvalid)
		}
		return KindContext{}, nil
	case "":
		return KindContext{}, fmt.Errorf("%w: transfer entity", ErrInvalid)
	default:
		kind, ok := ctx.World.KindOfEntity(entityID)
		if !ok {
			return KindContext{}, fmt.Errorf("%w: transfer entity", ErrInvalid)
		}
		return kind, nil
	}
}

func (transferCapability) Execute(action string, raw json.RawMessage, event Event, world WorldAccess) ([]AppliedAction, error) {
	if action != "move" {
		return nil, fmt.Errorf("%w: transfer action", ErrInvalid)
	}
	var params transferParams
	if err := decodeStrict(raw, &params); err != nil {
		return nil, fmt.Errorf("%w: transfer params", ErrInvalid)
	}
	fromID := params.FromEntity
	if fromID == "$event.entity" {
		fromID = event.EntityID
	}
	toID := params.ToEntity
	if toID == "$event.entity" {
		toID = event.EntityID
	}
	if fromID == "" || toID == "" || !world.HasEntity(fromID) || !world.HasEntity(toID) {
		return nil, fmt.Errorf("%w: transfer entity", ErrInvalid)
	}
	if fromID == toID {
		return nil, nil
	}
	fromType, ok := world.FieldType(fromID, params.Field)
	if !ok || fromType != "number" {
		return nil, fmt.Errorf("%w: transfer field", ErrInvalid)
	}
	toType, ok := world.FieldType(toID, params.Field)
	if !ok || toType != "number" {
		return nil, fmt.Errorf("%w: transfer field", ErrInvalid)
	}
	source, _ := world.Value(fromID, params.Field)
	sourceNumber, _ := source.(float64)
	target, _ := world.Value(toID, params.Field)
	targetNumber, _ := target.(float64)
	moved := math.Min(params.Amount, math.Max(0, sourceNumber))
	if moved <= 0 {
		return nil, nil
	}
	if err := world.SetValue(fromID, params.Field, sourceNumber-moved); err != nil {
		return nil, err
	}
	if err := world.SetValue(toID, params.Field, targetNumber+moved); err != nil {
		return nil, err
	}
	return []AppliedAction{
		{Capability: "core.transfer", Action: "move", EntityID: fromID, Field: params.Field, Value: sourceNumber - moved},
		{Capability: "core.transfer", Action: "move", EntityID: toID, Field: params.Field, Value: targetNumber + moved},
	}, nil
}
