// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// variableCapability changes one bounded world-wide variable. It is the shared
// state primitive behind story progress, game scores and workspace summaries.
// It is not a second entity system: values are flat text/number/flag scalars.
type variableCapability struct{}

func (variableCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.variable", Kind: "behavior", Version: 1,
		Title:       Localized{En: "Change a world variable", Zh: "改变世界变量"},
		Description: Localized{En: "Set, add to, or toggle one shared world variable.", Zh: "设置、累加或切换一个共享的世界变量。"},
		Actions: []ActionDescriptor{
			{
				ID: "set", Title: Localized{En: "Set", Zh: "设为"},
				Description: Localized{En: "Replace a variable with a literal value.", Zh: "把变量设为字面量。"},
				Params:      variableParamsSchema(false),
			},
			{
				ID: "add", Title: Localized{En: "Add", Zh: "增加"},
				Description: Localized{En: "Add a number to a numeric variable.", Zh: "给数字变量加上一个数值。"},
				Params:      variableParamsSchema(true),
			},
			{
				ID: "toggle", Title: Localized{En: "Toggle", Zh: "切换"},
				Description: Localized{En: "Flip an on/off variable.", Zh: "切换一个开关变量。"},
				Params:      variableParamsSchema(false),
			},
		},
	}
}

func variableParamsSchema(requireValue bool) *ParamSchema {
	schema := &ParamSchema{
		Type: "object",
		Properties: map[string]*ParamProperty{
			"name": {
				Type: "string", Format: "text", MaxLength: 40,
				Title:       Localized{En: "Variable", Zh: "变量"},
				Description: Localized{En: "A lower-case name such as story.chapter or score.", Zh: "小写名称，例如 story.chapter 或 score。"},
			},
			"value": {
				Type: "string", Title: Localized{En: "Value", Zh: "值"},
			},
		},
		Required: []string{"name"},
	}
	if requireValue {
		schema.Required = append(schema.Required, "value")
	}
	return schema
}

type variableParams struct {
	Name  string `json:"name"`
	Value any    `json:"value,omitempty"`
}

func (variableCapability) ValidateAction(action string, raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	if action != "set" && action != "add" && action != "toggle" {
		return nil, fmt.Errorf("%w: variable action", ErrInvalid)
	}
	var params variableParams
	if err := decodeStrict(raw, &params); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(params.Name)
	if name == "" || len(name) > 40 || !validVariableName(name) {
		return nil, fmt.Errorf("%w: variable.name", ErrInvalid)
	}
	existingType, exists := "", false
	if ctx.World != nil && ctx.World.Variables != nil {
		existingType, exists = ctx.World.Variables[name]
	}
	switch action {
	case "set":
		if params.Value == "$event.value" {
			if !ctx.AllowEvent {
				return nil, fmt.Errorf("%w: variable.value", ErrInvalid)
			}
			break
		}
		valueType, ok := scalarType(params.Value)
		if !ok {
			return nil, fmt.Errorf("%w: variable.value", ErrInvalid)
		}
		if exists && existingType != valueType {
			return nil, fmt.Errorf("%w: variable.type", ErrInvalid)
		}
	case "add":
		if exists && existingType != "number" {
			return nil, fmt.Errorf("%w: variable.add", ErrInvalid)
		}
		number, ok := params.Value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("%w: variable.value", ErrInvalid)
		}
	case "toggle":
		if exists && existingType != "flag" {
			return nil, fmt.Errorf("%w: variable.toggle", ErrInvalid)
		}
		if params.Value != nil {
			return nil, fmt.Errorf("%w: variable.value", ErrInvalid)
		}
	}
	return json.Marshal(params)
}

func validVariableName(value string) bool {
	for index, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case (r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') && index > 0:
		default:
			return false
		}
	}
	return true
}

func scalarType(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		if runeLength(typed) > 1200 {
			return "", false
		}
		return "text", true
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return "", false
		}
		return "number", true
	case bool:
		return "flag", true
	default:
		return "", false
	}
}

func (variableCapability) Execute(action string, raw json.RawMessage, event Event, world WorldAccess) ([]AppliedAction, error) {
	variables, ok := world.(VariableWorld)
	if !ok {
		return nil, fmt.Errorf("%w: variables unavailable", ErrInvalid)
	}
	var params variableParams
	if err := decodeStrict(raw, &params); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(params.Name)
	current, has := variables.Variable(name)
	var next any
	switch action {
	case "set":
		next = params.Value
		if next == "$event.value" {
			next = event.Value
		}
	case "add":
		base := float64(0)
		if has {
			if number, ok := current.(float64); ok {
				base = number
			}
		}
		amount, ok := params.Value.(float64)
		if !ok {
			return nil, fmt.Errorf("%w: variable.add", ErrInvalid)
		}
		next = base + amount
		if math.IsNaN(next.(float64)) || math.IsInf(next.(float64), 0) {
			return nil, fmt.Errorf("%w: variable.add", ErrInvalid)
		}
	case "toggle":
		base := false
		if has {
			if flag, ok := current.(bool); ok {
				base = flag
			}
		}
		next = !base
	default:
		return nil, fmt.Errorf("%w: variable action", ErrInvalid)
	}
	if _, ok := scalarType(next); !ok {
		return nil, fmt.Errorf("%w: variable.value", ErrInvalid)
	}
	if err := variables.SetVariable(name, next); err != nil {
		return nil, err
	}
	return []AppliedAction{{
		Capability: "core.variable", Action: action, Field: name, Value: next,
	}}, nil
}

// createCapability adds one server-identified entity to a declared kind. It is
// the primitive behind "add a row" in a workspace and bounded spawning in a
// live world. It cannot create another object, set permissions, or choose ids.
type createCapability struct{}

func (createCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.create", Kind: "behavior", Version: 1,
		Title:       Localized{En: "Create a thing", Zh: "创造一个存在"},
		Description: Localized{En: "Add one new thing of a declared kind. The server mints its identity.", Zh: "向已声明的种类添加一个新存在；身份由服务器生成。"},
		Actions: []ActionDescriptor{{
			ID: "create", Title: Localized{En: "Create", Zh: "创建"},
			Description: Localized{En: "Create one thing with optional initial field values.", Zh: "创建一个存在，可带初始字段值。"},
			Params: &ParamSchema{
				Type: "object",
				Properties: map[string]*ParamProperty{
					"kind_id": {Type: "string", Format: "kind", Title: Localized{En: "Kind", Zh: "种类"}},
					"name":    {Type: "string", MaxLength: 120, Title: Localized{En: "Name", Zh: "名称"}},
					"values":  {Type: "object", Title: Localized{En: "Initial values", Zh: "初始值"}},
				},
				Required: []string{"kind_id", "name"},
			},
		}},
	}
}

type createParams struct {
	KindID string         `json:"kind_id"`
	Name   string         `json:"name"`
	Values map[string]any `json:"values,omitempty"`
}

func (createCapability) ValidateAction(action string, raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	if action != "create" {
		return nil, fmt.Errorf("%w: create action", ErrInvalid)
	}
	var params createParams
	if err := decodeStrict(raw, &params); err != nil {
		return nil, err
	}
	kind, ok := ctx.World.Kinds[params.KindID]
	if !ok {
		return nil, fmt.Errorf("%w: create.kind_id", ErrInvalid)
	}
	params.Name = strings.TrimSpace(params.Name)
	if params.Name == "" || runeLength(params.Name) > 120 {
		return nil, fmt.Errorf("%w: create.name", ErrInvalid)
	}
	if len(params.Values) > 8 {
		return nil, fmt.Errorf("%w: create.values", ErrInvalid)
	}
	for key, value := range params.Values {
		fieldType, ok := kind.Fields[key]
		if !ok {
			return nil, fmt.Errorf("%w: create.values/%s", ErrInvalid, key)
		}
		if !stateValueMatches(fieldType, value) {
			return nil, fmt.Errorf("%w: create.values/%s", ErrInvalid, key)
		}
	}
	return json.Marshal(params)
}

func stateValueMatches(fieldType string, value any) bool {
	switch fieldType {
	case "text":
		_, ok := value.(string)
		return ok
	case "number":
		number, ok := value.(float64)
		return ok && math.IsNaN(number) == false && math.IsInf(number, 0) == false
	case "flag":
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

func (createCapability) Execute(action string, raw json.RawMessage, _ Event, world WorldAccess) ([]AppliedAction, error) {
	entities, ok := world.(EntityWorld)
	if !ok {
		return nil, fmt.Errorf("%w: entities unavailable", ErrInvalid)
	}
	var params createParams
	if err := decodeStrict(raw, &params); err != nil {
		return nil, err
	}
	id, err := entities.CreateEntity(params.KindID, strings.TrimSpace(params.Name), params.Values)
	if err != nil {
		return nil, err
	}
	return []AppliedAction{{
		Capability: "core.create", Action: "create", EntityID: id,
		Field: "created", Value: strings.TrimSpace(params.Name),
	}}, nil
}
