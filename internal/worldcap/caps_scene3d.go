// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"strings"
)

// scene3DCapability renders the same world nodes in a real three-dimensional
// scene. It is one shared interpretation of bounded data, not a per-world page:
// kinds choose a primitive (or an uploaded GLB), positions come from the
// world's own x/y coordinates, and clicks run the same behaviors as every other
// surface. The client adds camera movement, gravity and simple collisions.
type scene3DCapability struct{}

func (scene3DCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.scene3d", Kind: "presentation", Version: 1,
		Title:       Localized{En: "3D world", Zh: "三维世界"},
		Description: Localized{En: "A real 3D scene with camera movement, GLB models, gravity and collisions. Clicking a thing runs its behavior.", Zh: "真实的三维场景：视角移动、GLB 模型、重力与碰撞；点击存在会运行行为。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"title": {
					Type: "string", MaxLength: 120,
					Title: Localized{En: "Title", Zh: "标题"},
				},
				"sky_hue": {
					Type: "number", Format: "color_hue", Minimum: floatPointer(0), Maximum: floatPointer(359),
					Title: Localized{En: "Sky hue", Zh: "天空色"},
				},
				"ground_hue": {
					Type: "number", Format: "color_hue", Minimum: floatPointer(0), Maximum: floatPointer(359),
					Title: Localized{En: "Ground hue", Zh: "地面色"},
				},
				"fog": {
					Type: "number", Minimum: floatPointer(0), Maximum: floatPointer(0.2),
					Title: Localized{En: "Fog density", Zh: "雾浓度"},
				},
				"camera_mode": {
					Type: "string", Title: Localized{En: "Camera", Zh: "摄像机"},
					Enum: []string{"orbit", "follow"},
				},
				"camera_distance": {
					Type: "number", Minimum: floatPointer(4), Maximum: floatPointer(40),
					Title: Localized{En: "Camera distance", Zh: "摄像机距离"},
				},
				"camera_height": {
					Type: "number", Minimum: floatPointer(0.5), Maximum: floatPointer(25),
					Title: Localized{En: "Camera height", Zh: "摄像机高度"},
				},
				"fov": {
					Type: "number", Minimum: floatPointer(35), Maximum: floatPointer(90),
					Title: Localized{En: "Field of view", Zh: "视野角度"},
				},
				"gravity": {
					Type: "number", Minimum: floatPointer(0), Maximum: floatPointer(40),
					Title: Localized{En: "Gravity", Zh: "重力"},
				},
				"ambient_light": {
					Type: "number", Minimum: floatPointer(0), Maximum: floatPointer(2),
					Title: Localized{En: "Ambient light", Zh: "环境光"},
				},
				"player_enabled": {
					Type: "boolean", Title: Localized{En: "Player can walk", Zh: "可以控制角色行走"},
				},
				"player_node_id": {
					Type: "string", Format: "node", MaxLength: 36,
					Title: Localized{En: "Player thing", Zh: "角色存在"},
				},
				"player_speed": {
					Type: "number", Minimum: floatPointer(1), Maximum: floatPointer(10),
					Title: Localized{En: "Walk speed", Zh: "移动速度"},
				},
				"player_height": {
					Type: "number", Minimum: floatPointer(0.5), Maximum: floatPointer(3),
					Title: Localized{En: "Player height", Zh: "角色高度"},
				},
				"views": {
					Type: "array", Title: Localized{En: "3D form of each kind", Zh: "每种存在的三维形态"},
					Items: &ParamProperty{
						Type: "object",
						Properties: map[string]*ParamProperty{
							"kind_id": {
								Type: "string", Format: "kind",
								Title: Localized{En: "Kind", Zh: "种类"},
							},
							"primitive": {
								Type: "string", Title: Localized{En: "Primitive", Zh: "基础形体"},
								Enum: []string{"sphere", "box", "cylinder", "cone", "torus", "model"},
							},
							"model_asset_id": {
								Type: "string", Format: "asset", AssetKind: "model", MaxLength: 36,
								Title: Localized{En: "GLB model", Zh: "GLB 模型"},
							},
							"scale": {
								Type: "number", Minimum: floatPointer(0.2), Maximum: floatPointer(10),
								Title: Localized{En: "Scale", Zh: "缩放"},
							},
							"elevation": {
								Type: "number", Minimum: floatPointer(0), Maximum: floatPointer(30),
								Title: Localized{En: "Elevation", Zh: "高度"},
							},
							"hue": {
								Type: "number", Format: "color_hue", Minimum: floatPointer(0), Maximum: floatPointer(359),
								Title: Localized{En: "Hue", Zh: "色相"},
							},
							"opacity": {
								Type: "number", Minimum: floatPointer(0.1), Maximum: floatPointer(1),
								Title: Localized{En: "Opacity", Zh: "透明度"},
							},
							"collider": {
								Type: "string", Title: Localized{En: "Collision", Zh: "碰撞"},
								Enum: []string{"none", "box", "sphere"},
							},
							"label": {
								Type: "string", Title: Localized{En: "Label", Zh: "名称"},
								Enum: []string{"always", "hover", "hidden"},
							},
							"hide_done": {
								Type: "boolean", Title: Localized{En: "Hide finished things", Zh: "隐藏已完成存在"},
							},
						},
						Required: []string{"kind_id"},
					},
				},
				"show_links": {
					Type: "boolean", Title: Localized{En: "Show relations", Zh: "显示关系"},
				},
				"activate_behavior_id": {
					Type: "string", Format: "behavior",
					Title: Localized{En: "Click behavior", Zh: "点击行为"},
				},
				"bump_behavior_id": {
					Type: "string", Format: "behavior",
					Title: Localized{En: "Collision behavior", Zh: "碰撞行为"},
				},
				"entity_actions": {
					Type: "array", Title: Localized{En: "Per-thing behaviors", Zh: "每个存在的行为"},
					Items: &ParamProperty{
						Type: "object",
						Properties: map[string]*ParamProperty{
							"entity_id": {
								Type: "string", Format: "entity",
								Title: Localized{En: "Thing", Zh: "存在"},
							},
							"behavior_id": {
								Type: "string", Format: "behavior",
								Title: Localized{En: "Behavior", Zh: "行为"},
							},
							"label": {
								Type: "string", MaxLength: 80,
								Title: Localized{En: "Label", Zh: "名称"},
							},
						},
						Required: []string{"entity_id", "behavior_id"},
					},
				},
			},
		},
	}
}

type scene3DView struct {
	KindID       string   `json:"kind_id"`
	Primitive    string   `json:"primitive"`
	ModelAssetID string   `json:"model_asset_id,omitempty"`
	Scale        float64  `json:"scale"`
	Elevation    float64  `json:"elevation"`
	Hue          *float64 `json:"hue"`
	Opacity      float64  `json:"opacity"`
	Collider     string   `json:"collider"`
	Label        string   `json:"label"`
	HideDone     bool     `json:"hide_done,omitempty"`
}

type scene3DProps struct {
	Title              string         `json:"title"`
	SkyHue             float64        `json:"sky_hue"`
	GroundHue          float64        `json:"ground_hue"`
	Fog                float64        `json:"fog"`
	CameraMode         string         `json:"camera_mode"`
	CameraDistance     float64        `json:"camera_distance"`
	CameraHeight       float64        `json:"camera_height"`
	FOV                float64        `json:"fov"`
	Gravity            float64        `json:"gravity"`
	AmbientLight       float64        `json:"ambient_light"`
	PlayerEnabled      bool           `json:"player_enabled"`
	PlayerNodeID       string         `json:"player_node_id"`
	PlayerSpeed        float64        `json:"player_speed"`
	PlayerHeight       float64        `json:"player_height"`
	Views              []scene3DView  `json:"views"`
	ShowLinks          bool           `json:"show_links"`
	ActivateBehaviorID string         `json:"activate_behavior_id"`
	BumpBehaviorID     string         `json:"bump_behavior_id"`
	EntityActions      []entityAction `json:"entity_actions,omitempty"`
}

func (scene3DCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props scene3DProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	props.Title = strings.TrimSpace(props.Title)
	if runeLength(props.Title) > 120 {
		return nil, fmt.Errorf("%w: scene3d.title", ErrInvalid)
	}
	if !finite(props.SkyHue) || props.SkyHue < 0 || props.SkyHue > 359 ||
		!finite(props.GroundHue) || props.GroundHue < 0 || props.GroundHue > 359 {
		return nil, fmt.Errorf("%w: scene3d.hue", ErrInvalid)
	}
	if !finite(props.Fog) || props.Fog < 0 || props.Fog > 0.2 {
		return nil, fmt.Errorf("%w: scene3d.fog", ErrInvalid)
	}
	if props.CameraMode == "" {
		props.CameraMode = "orbit"
	}
	if props.CameraMode != "orbit" && props.CameraMode != "follow" {
		return nil, fmt.Errorf("%w: scene3d.camera_mode", ErrInvalid)
	}
	if props.CameraDistance == 0 {
		props.CameraDistance = 14
	}
	if !finite(props.CameraDistance) || props.CameraDistance < 4 || props.CameraDistance > 40 {
		return nil, fmt.Errorf("%w: scene3d.camera_distance", ErrInvalid)
	}
	if props.CameraHeight == 0 {
		props.CameraHeight = 8
	}
	if !finite(props.CameraHeight) || props.CameraHeight < 0.5 || props.CameraHeight > 25 {
		return nil, fmt.Errorf("%w: scene3d.camera_height", ErrInvalid)
	}
	if props.FOV == 0 {
		props.FOV = 55
	}
	if !finite(props.FOV) || props.FOV < 35 || props.FOV > 90 {
		return nil, fmt.Errorf("%w: scene3d.fov", ErrInvalid)
	}
	if props.Gravity == 0 {
		props.Gravity = 20
	}
	if !finite(props.Gravity) || props.Gravity < 0 || props.Gravity > 40 {
		return nil, fmt.Errorf("%w: scene3d.gravity", ErrInvalid)
	}
	if props.AmbientLight == 0 {
		props.AmbientLight = 0.8
	}
	if !finite(props.AmbientLight) || props.AmbientLight < 0 || props.AmbientLight > 2 {
		return nil, fmt.Errorf("%w: scene3d.ambient_light", ErrInvalid)
	}
	if props.PlayerSpeed == 0 {
		props.PlayerSpeed = 4
	}
	if !finite(props.PlayerSpeed) || props.PlayerSpeed < 1 || props.PlayerSpeed > 10 {
		return nil, fmt.Errorf("%w: scene3d.player_speed", ErrInvalid)
	}
	if props.PlayerHeight == 0 {
		props.PlayerHeight = 1
	}
	if !finite(props.PlayerHeight) || props.PlayerHeight < 0.5 || props.PlayerHeight > 3 {
		return nil, fmt.Errorf("%w: scene3d.player_height", ErrInvalid)
	}
	if props.PlayerNodeID != "" {
		if !ctx.World.HasEntity(props.PlayerNodeID) {
			return nil, fmt.Errorf("%w: scene3d.player_node_id", ErrInvalid)
		}
	}
	if len(props.Views) > 8 {
		return nil, fmt.Errorf("%w: scene3d.views", ErrInvalid)
	}
	seenKinds := map[string]bool{}
	for index := range props.Views {
		view := &props.Views[index]
		if _, ok := ctx.World.Kinds[view.KindID]; !ok || seenKinds[view.KindID] {
			return nil, fmt.Errorf("%w: scene3d.views/%d/kind_id", ErrInvalid, index)
		}
		seenKinds[view.KindID] = true
		if view.Primitive == "" {
			view.Primitive = "sphere"
		}
		switch view.Primitive {
		case "sphere", "box", "cylinder", "cone", "torus":
		case "model":
			if view.ModelAssetID != "" && !validAssetID(view.ModelAssetID) {
				return nil, fmt.Errorf("%w: scene3d.views/%d/model_asset_id", ErrInvalid, index)
			}
		default:
			return nil, fmt.Errorf("%w: scene3d.views/%d/primitive", ErrInvalid, index)
		}
		if view.Scale == 0 {
			view.Scale = 1
		}
		if !finite(view.Scale) || view.Scale < 0.2 || view.Scale > 10 {
			return nil, fmt.Errorf("%w: scene3d.views/%d/scale", ErrInvalid, index)
		}
		if !finite(view.Elevation) || view.Elevation < 0 || view.Elevation > 30 {
			return nil, fmt.Errorf("%w: scene3d.views/%d/elevation", ErrInvalid, index)
		}
		if view.Opacity == 0 {
			view.Opacity = 0.95
		}
		if !finite(view.Opacity) || view.Opacity < 0.1 || view.Opacity > 1 {
			return nil, fmt.Errorf("%w: scene3d.views/%d/opacity", ErrInvalid, index)
		}
		if view.Hue == nil {
			hue := 200.0
			view.Hue = &hue
		}
		if !finite(*view.Hue) || *view.Hue < 0 || *view.Hue > 359 {
			return nil, fmt.Errorf("%w: scene3d.views/%d/hue", ErrInvalid, index)
		}
		if view.Collider == "" {
			view.Collider = "none"
		}
		if view.Collider != "none" && view.Collider != "box" && view.Collider != "sphere" {
			return nil, fmt.Errorf("%w: scene3d.views/%d/collider", ErrInvalid, index)
		}
		if view.Label == "" {
			view.Label = "always"
		}
		if view.Label != "always" && view.Label != "hover" && view.Label != "hidden" {
			return nil, fmt.Errorf("%w: scene3d.views/%d/label", ErrInvalid, index)
		}
	}
	if props.ActivateBehaviorID != "" &&
		(!validLocalID(props.ActivateBehaviorID) || !ctx.World.HasBehavior(props.ActivateBehaviorID)) {
		return nil, fmt.Errorf("%w: scene3d.activate_behavior_id", ErrInvalid)
	}
	if props.BumpBehaviorID != "" &&
		(!validLocalID(props.BumpBehaviorID) || !ctx.World.HasBehavior(props.BumpBehaviorID)) {
		return nil, fmt.Errorf("%w: scene3d.bump_behavior_id", ErrInvalid)
	}
	if err := validateEntityActions(props.EntityActions, ctx); err != nil {
		return nil, err
	}
	return json.Marshal(props)
}
