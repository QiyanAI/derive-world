// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// spaceCapability is a 2D vector surface. It does not have a fixed set of
// shapes: every kind supplies its own SVG path. The capability only interprets
// positions, paths, layers and bounded field-driven visual bindings.
type spaceCapability struct{}

func (spaceCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.space", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Vector space", Zh: "向量空间"},
		Description: Localized{En: "Place things in a 2D space and give each kind any vector path. Clicking a thing can run a behavior.", Zh: "把存在放进 2D 空间，每种存在使用任意矢量路径。点击存在可运行行为。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"background": {
					Type: "object", Title: Localized{En: "Background", Zh: "背景"},
					Properties: map[string]*ParamProperty{
						"mode": {
							Type: "string", Title: Localized{En: "Mode", Zh: "模式"},
							Enum: []string{"gradient", "solid", "none"},
						},
						"hue": {
							Type: "number", Format: "color_hue", Title: Localized{En: "Sky hue", Zh: "天色"},
							Minimum: floatPointer(0), Maximum: floatPointer(359),
						},
						"hue2": {
							Type: "number", Format: "color_hue", Title: Localized{En: "Ground hue", Zh: "地色"},
							Minimum: floatPointer(0), Maximum: floatPointer(359),
						},
						"lightness": {
							Type: "number", Title: Localized{En: "Lightness", Zh: "明度"},
							Minimum: floatPointer(2), Maximum: floatPointer(40),
						},
						"accent_hue": {
							Type: "number", Format: "color_hue", Title: Localized{En: "Accent hue", Zh: "强调色"},
							Minimum: floatPointer(0), Maximum: floatPointer(359),
						},
					},
				},
				"show_links": {
					Type: "boolean", Title: Localized{En: "Show relations", Zh: "显示关系"},
				},
				"link_style": {
					Type: "string", Title: Localized{En: "Relation style", Zh: "关系样式"},
					Enum: []string{"curve", "line"},
				},
				"views": {
					Type: "array", Title: Localized{En: "Forms of each kind", Zh: "每种存在的形体"},
					Items: &ParamProperty{
						Type: "object",
						Properties: map[string]*ParamProperty{
							"kind_id": {
								Type: "string", Format: "kind", Title: Localized{En: "Kind", Zh: "种类"},
							},
							"path": {
								Type: "string", Format: "path", MaxLength: 2000,
								Title:       Localized{En: "Vector path", Zh: "矢量路径"},
								Description: Localized{En: "Any SVG path. Only drawing commands and numbers are accepted.", Zh: "任意 SVG 路径，只接受绘图命令与数字。"},
							},
							"size": {
								Type: "number", Title: Localized{En: "Size", Zh: "大小"},
								Minimum: floatPointer(12), Maximum: floatPointer(240),
							},
							"fill_hue": {
								Type: "number", Format: "color_hue", Title: Localized{En: "Fill hue", Zh: "填充色"},
								Minimum: floatPointer(0), Maximum: floatPointer(359),
							},
							"opacity": {
								Type: "number", Title: Localized{En: "Opacity", Zh: "透明度"},
								Minimum: floatPointer(0.1), Maximum: floatPointer(1),
							},
							"paint": {
								Type: "string", Title: Localized{En: "Paint", Zh: "绘制方式"},
								Enum: []string{"fill", "stroke"},
							},
							"label": {
								Type: "string", Title: Localized{En: "Label", Zh: "名称"},
								Enum: []string{"always", "hover", "hidden"},
							},
							"layer": {
								Type: "string", Title: Localized{En: "Layer", Zh: "层次"},
								Enum: []string{"back", "mid", "front"},
							},
							"bindings": {
								Type: "array", Title: Localized{En: "Field bindings", Zh: "字段绑定"},
								Items: &ParamProperty{
									Type: "object",
									Properties: map[string]*ParamProperty{
										"field": {
											Type: "string", Format: "field", Title: Localized{En: "Field", Zh: "字段"},
										},
										"target": {
											Type: "string", Title: Localized{En: "Visual property", Zh: "视觉属性"},
											Enum: []string{"size", "opacity", "hue"},
										},
										"in_min": {
											Type: "number", Title: Localized{En: "Input min", Zh: "输入最小"},
										},
										"in_max": {
											Type: "number", Title: Localized{En: "Input max", Zh: "输入最大"},
										},
										"out_min": {
											Type: "number", Title: Localized{En: "Output min", Zh: "输出最小"},
										},
										"out_max": {
											Type: "number", Title: Localized{En: "Output max", Zh: "输出最大"},
										},
									},
									Required: []string{"field", "target", "in_min", "in_max", "out_min", "out_max"},
								},
							},
						},
						Required: []string{"kind_id", "path"},
					},
				},
				"activate_behavior_id": {
					Type: "string", Format: "behavior",
					Title: Localized{En: "Click behavior (optional)", Zh: "点击行为（可选）"},
				},
			},
		},
	}
}

type spaceBackground struct {
	Mode      string  `json:"mode"`
	Hue       float64 `json:"hue"`
	Hue2      float64 `json:"hue2"`
	Lightness float64 `json:"lightness"`
	AccentHue float64 `json:"accent_hue"`
}

type spaceBinding struct {
	Field  string  `json:"field"`
	Target string  `json:"target"`
	InMin  float64 `json:"in_min"`
	InMax  float64 `json:"in_max"`
	OutMin float64 `json:"out_min"`
	OutMax float64 `json:"out_max"`
}

type spaceView struct {
	KindID   string         `json:"kind_id"`
	Path     string         `json:"path"`
	Size     float64        `json:"size"`
	FillHue  float64        `json:"fill_hue"`
	Opacity  float64        `json:"opacity"`
	Paint    string         `json:"paint"`
	Label    string         `json:"label"`
	Layer    string         `json:"layer"`
	Bindings []spaceBinding `json:"bindings,omitempty"`
}

type spaceProps struct {
	Background         spaceBackground `json:"background"`
	ShowLinks          bool            `json:"show_links"`
	LinkStyle          string          `json:"link_style"`
	Views              []spaceView     `json:"views"`
	ActivateBehaviorID string          `json:"activate_behavior_id"`
}

func (spaceCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props spaceProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if props.Background.Mode == "" {
		props.Background = spaceBackground{Mode: "gradient", Hue: 222, Hue2: 205, Lightness: 10, AccentHue: 42}
	}
	if props.LinkStyle == "" {
		props.LinkStyle = "curve"
	}
	if props.Background.Mode != "gradient" && props.Background.Mode != "solid" && props.Background.Mode != "none" {
		return nil, fmt.Errorf("%w: space.background.mode", ErrInvalid)
	}
	if !finite(props.Background.Hue) || props.Background.Hue < 0 || props.Background.Hue > 359 ||
		!finite(props.Background.Hue2) || props.Background.Hue2 < 0 || props.Background.Hue2 > 359 ||
		!finite(props.Background.AccentHue) || props.Background.AccentHue < 0 || props.Background.AccentHue > 359 {
		return nil, fmt.Errorf("%w: space.background.hue", ErrInvalid)
	}
	if !finite(props.Background.Lightness) || props.Background.Lightness < 2 || props.Background.Lightness > 40 {
		return nil, fmt.Errorf("%w: space.background.lightness", ErrInvalid)
	}
	if props.LinkStyle != "curve" && props.LinkStyle != "line" {
		return nil, fmt.Errorf("%w: space.link_style", ErrInvalid)
	}
	if len(props.Views) > 8 {
		return nil, fmt.Errorf("%w: space.views", ErrInvalid)
	}
	seenKinds := map[string]bool{}
	for index := range props.Views {
		view := &props.Views[index]
		kind, ok := ctx.World.Kinds[view.KindID]
		if !ok || seenKinds[view.KindID] {
			return nil, fmt.Errorf("%w: space.views/%d/kind_id", ErrInvalid, index)
		}
		seenKinds[view.KindID] = true
		if err := validateSVGPath(view.Path); err != nil {
			return nil, fmt.Errorf("%w: space.views/%d/path", ErrInvalid, index)
		}
		if view.Size == 0 && view.FillHue == 0 && view.Opacity == 0 {
			view.Size = 56
			view.FillHue = 198
			view.Opacity = 0.9
		}
		if view.Paint == "" {
			view.Paint = "fill"
		}
		if view.Label == "" {
			view.Label = "always"
		}
		if view.Layer == "" {
			view.Layer = "mid"
		}
		if !finite(view.Size) || view.Size < 12 || view.Size > 240 {
			return nil, fmt.Errorf("%w: space.views/%d/size", ErrInvalid, index)
		}
		if !finite(view.FillHue) || view.FillHue < 0 || view.FillHue > 359 {
			return nil, fmt.Errorf("%w: space.views/%d/fill_hue", ErrInvalid, index)
		}
		if !finite(view.Opacity) || view.Opacity < 0.1 || view.Opacity > 1 {
			return nil, fmt.Errorf("%w: space.views/%d/opacity", ErrInvalid, index)
		}
		if view.Paint != "fill" && view.Paint != "stroke" {
			return nil, fmt.Errorf("%w: space.views/%d/paint", ErrInvalid, index)
		}
		if view.Label != "always" && view.Label != "hover" && view.Label != "hidden" {
			return nil, fmt.Errorf("%w: space.views/%d/label", ErrInvalid, index)
		}
		if view.Layer != "back" && view.Layer != "mid" && view.Layer != "front" {
			return nil, fmt.Errorf("%w: space.views/%d/layer", ErrInvalid, index)
		}
		if len(view.Bindings) > 4 {
			return nil, fmt.Errorf("%w: space.views/%d/bindings", ErrInvalid, index)
		}
		seenTargets := map[string]bool{}
		for bindingIndex := range view.Bindings {
			if err := validateSpaceBinding(&view.Bindings[bindingIndex], kind, seenTargets); err != nil {
				return nil, fmt.Errorf("%w: space.views/%d/bindings/%d", ErrInvalid, index, bindingIndex)
			}
		}
	}
	if props.ActivateBehaviorID != "" {
		if !validLocalID(props.ActivateBehaviorID) || !ctx.World.HasBehavior(props.ActivateBehaviorID) {
			return nil, fmt.Errorf("%w: space.activate_behavior_id", ErrInvalid)
		}
	}
	return json.Marshal(props)
}

func validateSpaceBinding(binding *spaceBinding, kind KindContext, seenTargets map[string]bool) error {
	fieldType, ok := kind.Fields[binding.Field]
	if !ok || (fieldType != "number" && fieldType != "flag") {
		return fmt.Errorf("%w: binding field", ErrInvalid)
	}
	if seenTargets[binding.Target] {
		return fmt.Errorf("%w: duplicate binding target", ErrInvalid)
	}
	seenTargets[binding.Target] = true
	if !finite(binding.InMin) || !finite(binding.InMax) || binding.InMax <= binding.InMin ||
		!finite(binding.OutMin) || !finite(binding.OutMax) {
		return fmt.Errorf("%w: binding range", ErrInvalid)
	}
	if binding.InMin < -1e9 || binding.InMin > 1e9 || binding.InMax < -1e9 || binding.InMax > 1e9 {
		return fmt.Errorf("%w: binding input range", ErrInvalid)
	}
	switch binding.Target {
	case "size":
		if binding.OutMin < 0.2 || binding.OutMin > 3 || binding.OutMax < 0.2 || binding.OutMax > 3 {
			return fmt.Errorf("%w: binding size", ErrInvalid)
		}
	case "opacity":
		if binding.OutMin < 0 || binding.OutMin > 1 || binding.OutMax < 0 || binding.OutMax > 1 {
			return fmt.Errorf("%w: binding opacity", ErrInvalid)
		}
	case "hue":
		if binding.OutMin < -180 || binding.OutMin > 180 || binding.OutMax < -180 || binding.OutMax > 180 {
			return fmt.Errorf("%w: binding hue", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: binding target", ErrInvalid)
	}
	return nil
}

var svgPathPattern = regexp.MustCompile(`^[MmLlHhVvCcSsQqTtAaZz0-9eE+.,\-\s]+$`)

func validateSVGPath(value string) error {
	path := strings.TrimSpace(value)
	if path == "" || len(path) > 2000 {
		return fmt.Errorf("%w: path length", ErrInvalid)
	}
	if !svgPathPattern.MatchString(path) {
		return fmt.Errorf("%w: path characters", ErrInvalid)
	}
	if !strings.HasPrefix(path, "M") && !strings.HasPrefix(path, "m") {
		return fmt.Errorf("%w: path start", ErrInvalid)
	}
	commands := 0
	digits := 0
	for _, r := range path {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case strings.ContainsRune("MmLlHhVvCcSsQqTtAaZz", r):
			commands++
		}
	}
	if digits == 0 || commands > 200 {
		return fmt.Errorf("%w: path shape", ErrInvalid)
	}
	return nil
}

// collectionCapability repeats its children for every entity of one kind and
// exposes the current entity as $item. It is how data-driven worlds avoid
// hardcoding an entity count.
type collectionCapability struct{}

func (collectionCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.collection", Kind: "presentation", Version: 1,
		Title:          Localized{En: "Repeating collection", Zh: "重复集合"},
		Description:    Localized{En: "Render the child capabilities once for every thing of one kind. Use {{item.*}} inside.", Zh: "为某一类存在的每一个实例渲染一次子能力；内部可使用 {{item.*}}。"},
		AllowsChildren: true, ChildScope: "item", ChildKindProp: "kind_id",
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"kind_id": {
					Type: "string", Format: "kind", Title: Localized{En: "Kind", Zh: "种类"},
				},
				"layout": {
					Type: "string", Title: Localized{En: "Layout", Zh: "排列"},
					Enum: []string{"list", "grid", "row"},
				},
				"columns": {
					Type: "number", Title: Localized{En: "Columns", Zh: "列数"},
					Minimum: floatPointer(1), Maximum: floatPointer(6),
				},
				"empty_text": {
					Type: "string", MaxLength: 200,
					Title: Localized{En: "Empty message", Zh: "空状态文字"},
				},
			},
			Required: []string{"kind_id"},
		},
	}
}

type collectionProps struct {
	KindID    string  `json:"kind_id"`
	Layout    string  `json:"layout"`
	Columns   float64 `json:"columns"`
	EmptyText string  `json:"empty_text"`
}

func (collectionCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props collectionProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if _, ok := ctx.World.Kinds[props.KindID]; !ok {
		return nil, fmt.Errorf("%w: collection.kind_id", ErrInvalid)
	}
	if props.Layout == "" {
		props.Layout = "list"
	}
	if props.Columns == 0 {
		props.Columns = 2
	}
	if props.Layout != "list" && props.Layout != "grid" && props.Layout != "row" {
		return nil, fmt.Errorf("%w: collection.layout", ErrInvalid)
	}
	if !finite(props.Columns) || props.Columns != float64(int(props.Columns)) || props.Columns < 1 || props.Columns > 6 {
		return nil, fmt.Errorf("%w: collection.columns", ErrInvalid)
	}
	if runeLength(props.EmptyText) > 200 {
		return nil, fmt.Errorf("%w: collection.empty_text", ErrInvalid)
	}
	return json.Marshal(props)
}
