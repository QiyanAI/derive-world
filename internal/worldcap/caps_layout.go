// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
)

// stackCapability composes other capabilities in one direction. It knows
// nothing about what its children show.
type stackCapability struct{}

func (stackCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.stack", Kind: "presentation", Version: 1,
		Title:          Localized{En: "Layout", Zh: "布局"},
		Description:    Localized{En: "Arrange any capabilities in a row or column.", Zh: "把任意能力组合成一行或一列。"},
		AllowsChildren: true,
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"direction": {
					Type: "string", Title: Localized{En: "Direction", Zh: "方向"},
					Enum: []string{"vertical", "horizontal"},
				},
				"gap": {
					Type: "number", Title: Localized{En: "Gap", Zh: "间距"},
					Minimum: floatPointer(0), Maximum: floatPointer(64),
				},
				"padding": {
					Type: "number", Title: Localized{En: "Padding", Zh: "内边距"},
					Minimum: floatPointer(0), Maximum: floatPointer(64),
				},
				"align": {
					Type: "string", Title: Localized{En: "Alignment", Zh: "对齐"},
					Enum: []string{"start", "center", "stretch"},
				},
				"tone": {
					Type: "string", Title: Localized{En: "Surface", Zh: "表面"},
					Enum: []string{"plain", "panel", "subtle"},
				},
			},
		},
	}
}

type stackProps struct {
	Direction string  `json:"direction"`
	Gap       float64 `json:"gap"`
	Padding   float64 `json:"padding"`
	Align     string  `json:"align"`
	Tone      string  `json:"tone"`
}

func (stackCapability) Validate(raw json.RawMessage, _ ValidateContext) (json.RawMessage, error) {
	var props stackProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if props.Direction == "" {
		props.Direction = "vertical"
	}
	if props.Align == "" {
		props.Align = "stretch"
	}
	if props.Tone == "" {
		props.Tone = "plain"
	}
	if props.Direction != "vertical" && props.Direction != "horizontal" {
		return nil, fmt.Errorf("%w: stack.direction", ErrInvalid)
	}
	if !finite(props.Gap) || props.Gap < 0 || props.Gap > 64 {
		return nil, fmt.Errorf("%w: stack.gap", ErrInvalid)
	}
	if !finite(props.Padding) || props.Padding < 0 || props.Padding > 64 {
		return nil, fmt.Errorf("%w: stack.padding", ErrInvalid)
	}
	if props.Align != "start" && props.Align != "center" && props.Align != "stretch" {
		return nil, fmt.Errorf("%w: stack.align", ErrInvalid)
	}
	if props.Tone != "plain" && props.Tone != "panel" && props.Tone != "subtle" {
		return nil, fmt.Errorf("%w: stack.tone", ErrInvalid)
	}
	return json.Marshal(props)
}

// textCapability renders bounded plain text. State references are validated
// here and substituted by the client as text nodes, never markup.
type textCapability struct{}

func (textCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.text", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Text", Zh: "文字"},
		Description: Localized{En: "A block of story, description or instruction. Reference real world state with {{...}}.", Zh: "一段故事、描述或说明，可用 {{...}} 引用世界中的真实状态。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"text": {
					Type: "string", Format: "template", MaxLength: maxTemplateRunes,
					Title:       Localized{En: "Text", Zh: "文字"},
					Description: Localized{En: "Plain text. {{world.title}}, {{entity.<id>.name}} and {{item.field.<key>}} are supported inside a collection.", Zh: "纯文本。集合内可使用 {{world.title}}、{{entity.<id>.name}} 与 {{item.field.<key>}}。"},
				},
				"size": {
					Type: "number", Title: Localized{En: "Size", Zh: "字号"},
					Minimum: floatPointer(12), Maximum: floatPointer(72),
				},
				"weight": {
					Type: "string", Title: Localized{En: "Weight", Zh: "字重"},
					Enum: []string{"regular", "medium", "bold"},
				},
				"tone": {
					Type: "string", Title: Localized{En: "Tone", Zh: "语气"},
					Enum: []string{"default", "muted", "accent"},
				},
				"align": {
					Type: "string", Title: Localized{En: "Alignment", Zh: "对齐"},
					Enum: []string{"left", "center", "right"},
				},
			},
			Required: []string{"text"},
		},
	}
}

type textProps struct {
	Text   string  `json:"text"`
	Size   float64 `json:"size"`
	Weight string  `json:"weight"`
	Tone   string  `json:"tone"`
	Align  string  `json:"align"`
}

func (textCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props textProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if props.Size == 0 {
		props.Size = 18
	}
	if props.Weight == "" {
		props.Weight = "regular"
	}
	if props.Tone == "" {
		props.Tone = "default"
	}
	if props.Align == "" {
		props.Align = "left"
	}
	if !finite(props.Size) || props.Size < 12 || props.Size > 72 {
		return nil, fmt.Errorf("%w: text.size", ErrInvalid)
	}
	if props.Weight != "regular" && props.Weight != "medium" && props.Weight != "bold" {
		return nil, fmt.Errorf("%w: text.weight", ErrInvalid)
	}
	if props.Tone != "default" && props.Tone != "muted" && props.Tone != "accent" {
		return nil, fmt.Errorf("%w: text.tone", ErrInvalid)
	}
	if props.Align != "left" && props.Align != "center" && props.Align != "right" {
		return nil, fmt.Errorf("%w: text.align", ErrInvalid)
	}
	if err := ValidateTemplate(props.Text, ctx); err != nil {
		return nil, err
	}
	return json.Marshal(props)
}

func floatPointer(value float64) *float64 {
	return &value
}
