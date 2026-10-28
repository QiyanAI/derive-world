// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buttonCapability invokes one reusable behavior. The behavior's actions use
// $event.entity when the button sits inside a collection.
type buttonCapability struct{}

func (buttonCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.button", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Action button", Zh: "操作按钮"},
		Description: Localized{En: "Run a behavior when pressed. The behavior can change real world state.", Zh: "按下时运行一个行为，行为可以改变世界的真实状态。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"label": {
					Type: "string", MaxLength: 120,
					Title: Localized{En: "Label", Zh: "按钮文字"},
				},
				"behavior_id": {
					Type: "string", Format: "behavior",
					Title: Localized{En: "Behavior", Zh: "行为"},
				},
				"tone": {
					Type: "string", Title: Localized{En: "Style", Zh: "样式"},
					Enum: []string{"primary", "ghost"},
				},
			},
			Required: []string{"label", "behavior_id"},
		},
	}
}

type buttonProps struct {
	Label      string `json:"label"`
	BehaviorID string `json:"behavior_id"`
	Tone       string `json:"tone"`
}

func (buttonCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props buttonProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if props.Tone == "" {
		props.Tone = "primary"
	}
	props.Label = strings.TrimSpace(props.Label)
	if props.Label == "" || runeLength(props.Label) > 120 {
		return nil, fmt.Errorf("%w: button.label", ErrInvalid)
	}
	if !validLocalID(props.BehaviorID) || !ctx.World.HasBehavior(props.BehaviorID) {
		return nil, fmt.Errorf("%w: button.behavior_id", ErrInvalid)
	}
	if props.Tone != "primary" && props.Tone != "ghost" {
		return nil, fmt.Errorf("%w: button.tone", ErrInvalid)
	}
	return json.Marshal(props)
}

// choiceCapability presents several labeled behaviors. It is a generic
// control for text/choice worlds, not a story engine.
type choiceCapability struct{}

func (choiceCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.choice", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Choice", Zh: "选择"},
		Description: Localized{En: "Offer two to eight choices; each choice runs a behavior.", Zh: "提供 2–8 个选择，每个选择运行一个行为。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"prompt": {
					Type: "string", Format: "template", MaxLength: 500,
					Title: Localized{En: "Prompt", Zh: "提示"},
				},
				"options": {
					Type: "array", Title: Localized{En: "Choices", Zh: "选项"},
					Items: &ParamProperty{
						Type: "object",
						Properties: map[string]*ParamProperty{
							"label": {
								Type: "string", MaxLength: 120,
								Title: Localized{En: "Label", Zh: "文字"},
							},
							"behavior_id": {
								Type: "string", Format: "behavior",
								Title: Localized{En: "Behavior", Zh: "行为"},
							},
						},
						Required: []string{"label", "behavior_id"},
					},
				},
			},
			Required: []string{"options"},
		},
	}
}

type choiceOption struct {
	Label      string `json:"label"`
	BehaviorID string `json:"behavior_id"`
}

type choiceProps struct {
	Prompt  string         `json:"prompt"`
	Options []choiceOption `json:"options"`
}

func (choiceCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props choiceProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if runeLength(props.Prompt) > 500 {
		return nil, fmt.Errorf("%w: choice.prompt", ErrInvalid)
	}
	if err := ValidateTemplate(props.Prompt, ctx); err != nil {
		return nil, err
	}
	if len(props.Options) < 2 || len(props.Options) > 8 {
		return nil, fmt.Errorf("%w: choice.options", ErrInvalid)
	}
	for index := range props.Options {
		option := &props.Options[index]
		option.Label = strings.TrimSpace(option.Label)
		if option.Label == "" || runeLength(option.Label) > 120 {
			return nil, fmt.Errorf("%w: choice.options/%d/label", ErrInvalid, index)
		}
		if !validLocalID(option.BehaviorID) || !ctx.World.HasBehavior(option.BehaviorID) {
			return nil, fmt.Errorf("%w: choice.options/%d/behavior_id", ErrInvalid, index)
		}
	}
	return json.Marshal(props)
}

// fieldCapability edits one real entity field through the normal object action
// path. It is the generic building block of non-entertainment worlds.
type fieldCapability struct{}

func (fieldCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.field", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Field editor", Zh: "字段编辑"},
		Description: Localized{En: "Edit a text, number or on/off field of one thing. Every edit is versioned.", Zh: "编辑一个存在的文字、数字或开关字段；每次修改都会版本化保存。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"entity_id": {
					Type: "string", Format: "item_entity",
					Title: Localized{En: "Thing", Zh: "存在"},
				},
				"field": {
					Type: "string", Format: "field",
					Title: Localized{En: "Field", Zh: "字段"},
				},
				"label": {
					Type: "string", MaxLength: 80,
					Title: Localized{En: "Label", Zh: "标签"},
				},
				"control": {
					Type: "string", Title: Localized{En: "Control", Zh: "控件"},
					Enum: []string{"auto", "text", "number", "flag"},
				},
			},
			Required: []string{"entity_id", "field"},
		},
	}
}

type fieldProps struct {
	EntityID string `json:"entity_id"`
	Field    string `json:"field"`
	Label    string `json:"label"`
	Control  string `json:"control"`
}

func (fieldCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props fieldProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if props.Control == "" {
		props.Control = "auto"
	}
	if props.Control != "auto" && props.Control != "text" && props.Control != "number" && props.Control != "flag" {
		return nil, fmt.Errorf("%w: field.control", ErrInvalid)
	}
	if runeLength(props.Label) > 80 {
		return nil, fmt.Errorf("%w: field.label", ErrInvalid)
	}
	fieldType, err := presentationFieldType(ctx, props.EntityID, props.Field)
	if err != nil {
		return nil, err
	}
	if fieldType == "status" {
		return nil, fmt.Errorf("%w: field.status", ErrInvalid)
	}
	if props.Control != "auto" && props.Control != fieldType {
		return nil, fmt.Errorf("%w: field.control", ErrInvalid)
	}
	return json.Marshal(props)
}

// meterCapability reads one numeric field and draws a bounded progress bar.
// It is a pure presentation module: it does not own a special page or a
// special world type.
type meterCapability struct{}

func (meterCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.meter", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Meter", Zh: "进度条"},
		Description: Localized{En: "Show a real numeric field as a bounded bar.", Zh: "把一个真实数字字段显示为有界的进度条。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"entity_id": {
					Type: "string", Format: "item_entity",
					Title: Localized{En: "Thing", Zh: "存在"},
				},
				"field": {
					Type: "string", Format: "field",
					Title: Localized{En: "Number field", Zh: "数字字段"},
				},
				"label": {
					Type: "string", MaxLength: 80,
					Title: Localized{En: "Label", Zh: "标签"},
				},
				"min": {
					Type: "number", Title: Localized{En: "Minimum", Zh: "最小值"},
					Minimum: floatPointer(-1000000), Maximum: floatPointer(1000000),
				},
				"max": {
					Type: "number", Title: Localized{En: "Maximum", Zh: "最大值"},
					Minimum: floatPointer(-1000000), Maximum: floatPointer(1000000),
				},
				"tone": {
					Type: "string", Title: Localized{En: "Tone", Zh: "语气"},
					Enum: []string{"accent", "warm"},
				},
			},
			Required: []string{"entity_id", "field"},
		},
	}
}

type meterProps struct {
	EntityID string  `json:"entity_id"`
	Field    string  `json:"field"`
	Label    string  `json:"label"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Tone     string  `json:"tone"`
}

func (meterCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props meterProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if props.Max == 0 && props.Min == 0 {
		props.Max = 10
	}
	if props.Tone == "" {
		props.Tone = "accent"
	}
	if runeLength(props.Label) > 80 {
		return nil, fmt.Errorf("%w: meter.label", ErrInvalid)
	}
	if !finite(props.Min) || !finite(props.Max) || props.Max <= props.Min ||
		props.Min < -1000000 || props.Max > 1000000 {
		return nil, fmt.Errorf("%w: meter.range", ErrInvalid)
	}
	if props.Tone != "accent" && props.Tone != "warm" {
		return nil, fmt.Errorf("%w: meter.tone", ErrInvalid)
	}
	fieldType, err := presentationFieldType(ctx, props.EntityID, props.Field)
	if err != nil {
		return nil, err
	}
	if fieldType != "number" {
		return nil, fmt.Errorf("%w: meter.field", ErrInvalid)
	}
	return json.Marshal(props)
}

// tableCapability presents entities of one kind as a data table. It is the
// generic building block for work, tracking and other non-entertainment
// worlds; it is not selected by a world category.
type tableCapability struct{}

func (tableCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.table", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Table", Zh: "数据表"},
		Description: Localized{En: "Show every thing of one kind in a table, optionally wiring row clicks to a behavior.", Zh: "把某一类存在显示为表格，并可选地让点击行触发行为。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"kind_id": {
					Type: "string", Format: "kind",
					Title: Localized{En: "Kind", Zh: "种类"},
				},
				"columns": {
					Type: "array", Title: Localized{En: "Columns", Zh: "列"},
					Items: &ParamProperty{
						Type: "object",
						Properties: map[string]*ParamProperty{
							"source": {
								Type: "string", Format: "table_source",
								Title: Localized{En: "Source", Zh: "来源"},
							},
							"label": {
								Type: "string", MaxLength: 60,
								Title: Localized{En: "Label", Zh: "标题"},
							},
						},
						Required: []string{"source"},
					},
				},
				"row_behavior_id": {
					Type: "string", Format: "behavior",
					Title: Localized{En: "Row behavior (optional)", Zh: "点击行行为（可选）"},
				},
			},
			Required: []string{"kind_id", "columns"},
		},
	}
}

type tableColumn struct {
	Source string `json:"source"`
	Label  string `json:"label"`
}

type tableProps struct {
	KindID        string        `json:"kind_id"`
	Columns       []tableColumn `json:"columns"`
	RowBehaviorID string        `json:"row_behavior_id"`
}

func (tableCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props tableProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	kind, ok := ctx.World.Kinds[props.KindID]
	if !ok {
		return nil, fmt.Errorf("%w: table.kind_id", ErrInvalid)
	}
	if len(props.Columns) < 1 || len(props.Columns) > 8 {
		return nil, fmt.Errorf("%w: table.columns", ErrInvalid)
	}
	seen := map[string]bool{}
	for index := range props.Columns {
		column := &props.Columns[index]
		if column.Source != "name" && column.Source != "text" && column.Source != "status" {
			if _, ok := kind.Fields[column.Source]; !ok {
				return nil, fmt.Errorf("%w: table.columns/%d/source", ErrInvalid, index)
			}
		}
		if seen[column.Source] {
			return nil, fmt.Errorf("%w: table.columns/%d/source", ErrInvalid, index)
		}
		seen[column.Source] = true
		if runeLength(column.Label) > 60 {
			return nil, fmt.Errorf("%w: table.columns/%d/label", ErrInvalid, index)
		}
	}
	if props.RowBehaviorID != "" {
		if !validLocalID(props.RowBehaviorID) || !ctx.World.HasBehavior(props.RowBehaviorID) {
			return nil, fmt.Errorf("%w: table.row_behavior_id", ErrInvalid)
		}
	}
	return json.Marshal(props)
}

// presentationFieldType resolves an entity reference used by a presentation
// control. Presentation props are static, so $event is not available here;
// inside a collection $item refers to the current entity's kind.
func presentationFieldType(ctx ValidateContext, entityID, field string) (string, error) {
	if strings.TrimSpace(field) == "" || runeLength(field) > 40 {
		return "", fmt.Errorf("%w: entity field", ErrInvalid)
	}
	switch entityID {
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
		fieldType, ok := kind.Fields[field]
		if !ok {
			return "", fmt.Errorf("%w: item field", ErrInvalid)
		}
		return fieldType, nil
	case "$event.entity", "":
		return "", fmt.Errorf("%w: entity_id", ErrInvalid)
	default:
		fieldType, ok := ctx.World.FieldType(entityID, field)
		if !ok {
			return "", fmt.Errorf("%w: entity field", ErrInvalid)
		}
		return fieldType, nil
	}
}
