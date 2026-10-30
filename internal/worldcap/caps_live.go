// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// liveCapability is one time-based 2D surface. It covers three kinds of world
// without becoming three templates:
//
//   - no avatar: a genuinely playing animation (timeline, particles, camera);
//   - avatar without a goal: a walkable space;
//   - avatar with a goal: a playable game with collection, score, reach or
//     survival objectives.
//
// The same content, state and variables back every mode. Nothing here is
// executable user code: tracks, goals and controls are bounded data.
type liveCapability struct{}

func (liveCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.live", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Living scene", Zh: "实时世界"},
		Description: Localized{En: "A time-based 2D surface: animation, walkable space, or a playable game with a goal.", Zh: "时间驱动的 2D 表面：动态画面、可行走空间，或带目标的可玩游戏。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"title":  {Type: "string", MaxLength: 80, Title: Localized{En: "Title", Zh: "标题"}},
				"hint":   {Type: "string", MaxLength: 200, Title: Localized{En: "Hint", Zh: "提示"}},
				"avatar": {Type: "object", Title: Localized{En: "Avatar (optional)", Zh: "化身（可选）"}},
				"goal":   {Type: "object", Title: Localized{En: "Goal (optional)", Zh: "目标（可选）"}},
				"views":  {Type: "array", Title: Localized{En: "Forms of each kind", Zh: "每种存在的形体"}},
			},
		},
	}
}

type liveAvatar struct {
	Path    string  `json:"path"`
	Size    float64 `json:"size"`
	Hue     float64 `json:"hue"`
	Opacity float64 `json:"opacity"`
}

type livePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type liveKey struct {
	T      float64 `json:"t"`
	V      float64 `json:"v"`
	Easing string  `json:"easing,omitempty"`
}

type liveTrack struct {
	EntityID string    `json:"entity_id"`
	Property string    `json:"property"`
	Keys     []liveKey `json:"keys"`
}

type liveTimeline struct {
	Duration float64     `json:"duration"`
	Loop     bool        `json:"loop"`
	Autoplay *bool       `json:"autoplay,omitempty"`
	Tracks   []liveTrack `json:"tracks"`
}

type liveParticles struct {
	Kind  string  `json:"kind"`
	Count float64 `json:"count"`
	Hue   float64 `json:"hue"`
	Size  float64 `json:"size"`
	Speed float64 `json:"speed"`
}

type liveGoal struct {
	Type     string  `json:"type"`
	Label    string  `json:"label,omitempty"`
	KindID   string  `json:"kind_id,omitempty"`
	Count    float64 `json:"count,omitempty"`
	EntityID string  `json:"entity_id,omitempty"`
	Field    string  `json:"field,omitempty"`
	Variable string  `json:"variable,omitempty"`
	Target   float64 `json:"target,omitempty"`
}

type liveProps struct {
	Background     spaceBackground `json:"background"`
	ShowLinks      bool            `json:"show_links"`
	LinkStyle      string          `json:"link_style"`
	Views          []spaceView     `json:"views"`
	EntityActions  []entityAction  `json:"entity_actions,omitempty"`
	Avatar         *liveAvatar     `json:"avatar,omitempty"`
	Spawn          *livePoint      `json:"spawn,omitempty"`
	Speed          float64         `json:"speed"`
	SolidKinds     []string        `json:"solid_kinds,omitempty"`
	InteractRadius float64         `json:"interact_radius"`
	Timeline       *liveTimeline   `json:"timeline,omitempty"`
	Particles      *liveParticles  `json:"particles,omitempty"`
	Goal           *liveGoal       `json:"goal,omitempty"`
	Timer          float64         `json:"timer,omitempty"`
	Hint           string          `json:"hint,omitempty"`
	Title          string          `json:"title,omitempty"`
}

func (liveCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props liveProps
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
		return nil, fmt.Errorf("%w: live.background.mode", ErrInvalid)
	}
	if !finite(props.Background.Hue) || props.Background.Hue < 0 || props.Background.Hue > 359 ||
		!finite(props.Background.Hue2) || props.Background.Hue2 < 0 || props.Background.Hue2 > 359 ||
		!finite(props.Background.AccentHue) || props.Background.AccentHue < 0 || props.Background.AccentHue > 359 ||
		!finite(props.Background.Lightness) || props.Background.Lightness < 2 || props.Background.Lightness > 40 {
		return nil, fmt.Errorf("%w: live.background", ErrInvalid)
	}
	if props.LinkStyle != "curve" && props.LinkStyle != "line" {
		return nil, fmt.Errorf("%w: live.link_style", ErrInvalid)
	}
	if err := validateLiveViews(props.Views, ctx); err != nil {
		return nil, err
	}
	if err := validateEntityActions(props.EntityActions, ctx); err != nil {
		return nil, err
	}
	if props.Avatar != nil {
		if err := validateSVGPath(props.Avatar.Path); err != nil {
			return nil, fmt.Errorf("%w: live.avatar.path", ErrInvalid)
		}
		if props.Avatar.Size == 0 {
			props.Avatar.Size = 34
		}
		if props.Avatar.Hue == 0 && props.Avatar.Opacity == 0 {
			props.Avatar.Hue = 198
			props.Avatar.Opacity = 1
		}
		if !finite(props.Avatar.Size) || props.Avatar.Size < 12 || props.Avatar.Size > 120 ||
			!finite(props.Avatar.Hue) || props.Avatar.Hue < 0 || props.Avatar.Hue > 359 ||
			!finite(props.Avatar.Opacity) || props.Avatar.Opacity < 0.1 || props.Avatar.Opacity > 1 {
			return nil, fmt.Errorf("%w: live.avatar", ErrInvalid)
		}
	}
	if props.Speed == 0 {
		props.Speed = 1.8
	}
	if !finite(props.Speed) || props.Speed < 0.5 || props.Speed > 4 {
		return nil, fmt.Errorf("%w: live.speed", ErrInvalid)
	}
	if props.InteractRadius == 0 {
		props.InteractRadius = 96
	}
	if !finite(props.InteractRadius) || props.InteractRadius < 40 || props.InteractRadius > 240 {
		return nil, fmt.Errorf("%w: live.interact_radius", ErrInvalid)
	}
	if props.Spawn != nil {
		if !finite(props.Spawn.X) || props.Spawn.X < 0 || props.Spawn.X > 1600 ||
			!finite(props.Spawn.Y) || props.Spawn.Y < 0 || props.Spawn.Y > 1000 {
			return nil, fmt.Errorf("%w: live.spawn", ErrInvalid)
		}
	}
	if len(props.SolidKinds) > 8 {
		return nil, fmt.Errorf("%w: live.solid_kinds", ErrInvalid)
	}
	seenSolid := map[string]bool{}
	for index, kindID := range props.SolidKinds {
		if _, ok := ctx.World.Kinds[kindID]; !ok || seenSolid[kindID] {
			return nil, fmt.Errorf("%w: live.solid_kinds/%d", ErrInvalid, index)
		}
		seenSolid[kindID] = true
	}
	if props.Timeline != nil {
		if err := validateTimeline(props.Timeline, ctx); err != nil {
			return nil, err
		}
	}
	if props.Particles != nil {
		if err := validateParticles(props.Particles); err != nil {
			return nil, err
		}
	}
	if props.Goal != nil {
		if err := validateGoal(props.Goal, props.Timer, ctx); err != nil {
			return nil, err
		}
	}
	if !finite(props.Timer) || props.Timer < 0 || props.Timer > 3600 {
		return nil, fmt.Errorf("%w: live.timer", ErrInvalid)
	}
	if runeLength(props.Hint) > 200 {
		return nil, fmt.Errorf("%w: live.hint", ErrInvalid)
	}
	if runeLength(props.Title) > 80 {
		return nil, fmt.Errorf("%w: live.title", ErrInvalid)
	}
	return json.Marshal(props)
}

func validateLiveViews(views []spaceView, ctx ValidateContext) error {
	if len(views) > 8 {
		return fmt.Errorf("%w: live.views", ErrInvalid)
	}
	seenKinds := map[string]bool{}
	for index := range views {
		view := &views[index]
		kind, ok := ctx.World.Kinds[view.KindID]
		if !ok || seenKinds[view.KindID] {
			return fmt.Errorf("%w: live.views/%d/kind_id", ErrInvalid, index)
		}
		seenKinds[view.KindID] = true
		if err := validateSVGPath(view.Path); err != nil {
			return fmt.Errorf("%w: live.views/%d/path", ErrInvalid, index)
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
		if !finite(view.Size) || view.Size < 12 || view.Size > 240 ||
			!finite(view.FillHue) || view.FillHue < 0 || view.FillHue > 359 ||
			!finite(view.Opacity) || view.Opacity < 0.1 || view.Opacity > 1 {
			return fmt.Errorf("%w: live.views/%d", ErrInvalid, index)
		}
		if view.Paint != "fill" && view.Paint != "stroke" {
			return fmt.Errorf("%w: live.views/%d/paint", ErrInvalid, index)
		}
		if view.Label != "always" && view.Label != "hover" && view.Label != "hidden" {
			return fmt.Errorf("%w: live.views/%d/label", ErrInvalid, index)
		}
		if view.Layer != "back" && view.Layer != "mid" && view.Layer != "front" {
			return fmt.Errorf("%w: live.views/%d/layer", ErrInvalid, index)
		}
		if !validSceneMotion(view.Motion) {
			return fmt.Errorf("%w: live.views/%d/motion", ErrInvalid, index)
		}
		if len(view.Bindings) > 4 {
			return fmt.Errorf("%w: live.views/%d/bindings", ErrInvalid, index)
		}
		seenTargets := map[string]bool{}
		for bindingIndex := range view.Bindings {
			if err := validateSpaceBinding(&view.Bindings[bindingIndex], kind, seenTargets); err != nil {
				return fmt.Errorf("%w: live.views/%d/bindings/%d", ErrInvalid, index, bindingIndex)
			}
		}
	}
	return nil
}

func validateTimeline(timeline *liveTimeline, ctx ValidateContext) error {
	if !finite(timeline.Duration) || timeline.Duration < 1 || timeline.Duration > 120 {
		return fmt.Errorf("%w: live.timeline.duration", ErrInvalid)
	}
	if len(timeline.Tracks) > 16 {
		return fmt.Errorf("%w: live.timeline.tracks", ErrInvalid)
	}
	for trackIndex := range timeline.Tracks {
		track := &timeline.Tracks[trackIndex]
		if track.EntityID != "background" && !ctx.World.HasEntity(track.EntityID) {
			return fmt.Errorf("%w: live.timeline.tracks/%d/entity_id", ErrInvalid, trackIndex)
		}
		switch track.Property {
		case "x", "y", "scale", "opacity", "rotate", "hue":
		default:
			return fmt.Errorf("%w: live.timeline.tracks/%d/property", ErrInvalid, trackIndex)
		}
		if len(track.Keys) < 2 || len(track.Keys) > 16 {
			return fmt.Errorf("%w: live.timeline.tracks/%d/keys", ErrInvalid, trackIndex)
		}
		previous := -1.0
		for keyIndex := range track.Keys {
			key := &track.Keys[keyIndex]
			if !finite(key.T) || key.T < 0 || key.T > timeline.Duration || key.T <= previous {
				return fmt.Errorf("%w: live.timeline.tracks/%d/keys/%d/t", ErrInvalid, trackIndex, keyIndex)
			}
			previous = key.T
			if !finite(key.V) || !liveValueInRange(track.Property, key.V) {
				return fmt.Errorf("%w: live.timeline.tracks/%d/keys/%d/v", ErrInvalid, trackIndex, keyIndex)
			}
			if key.Easing != "" && key.Easing != "linear" && key.Easing != "ease_in" &&
				key.Easing != "ease_out" && key.Easing != "ease_in_out" {
				return fmt.Errorf("%w: live.timeline.tracks/%d/keys/%d/easing", ErrInvalid, trackIndex, keyIndex)
			}
		}
	}
	return nil
}

func liveValueInRange(property string, value float64) bool {
	switch property {
	case "x":
		return value >= 0 && value <= 1600
	case "y":
		return value >= 0 && value <= 1000
	case "scale":
		return value >= 0.1 && value <= 4
	case "opacity":
		return value >= 0 && value <= 1
	case "rotate":
		return value >= -720 && value <= 720
	case "hue":
		return value >= 0 && value <= 359
	default:
		return false
	}
}

func validateParticles(particles *liveParticles) error {
	switch particles.Kind {
	case "rain", "motes", "stars", "snow":
	default:
		return fmt.Errorf("%w: live.particles.kind", ErrInvalid)
	}
	if !finite(particles.Count) || particles.Count < 0 || particles.Count > 120 {
		return fmt.Errorf("%w: live.particles.count", ErrInvalid)
	}
	if !finite(particles.Hue) || particles.Hue < 0 || particles.Hue > 359 ||
		!finite(particles.Size) || particles.Size < 1 || particles.Size > 6 ||
		!finite(particles.Speed) || particles.Speed < 0.2 || particles.Speed > 3 {
		return fmt.Errorf("%w: live.particles", ErrInvalid)
	}
	return nil
}

func validateGoal(goal *liveGoal, timer float64, ctx ValidateContext) error {
	goal.Label = strings.TrimSpace(goal.Label)
	if runeLength(goal.Label) > 120 {
		return fmt.Errorf("%w: live.goal.label", ErrInvalid)
	}
	switch goal.Type {
	case "collect":
		if _, ok := ctx.World.Kinds[goal.KindID]; !ok {
			return fmt.Errorf("%w: live.goal.kind_id", ErrInvalid)
		}
		if !finite(goal.Count) || goal.Count < 1 || goal.Count > 40 {
			return fmt.Errorf("%w: live.goal.count", ErrInvalid)
		}
	case "score":
		if goal.Variable != "" {
			if !validVariableName(goal.Variable) {
				return fmt.Errorf("%w: live.goal.variable", ErrInvalid)
			}
		} else {
			if !ctx.World.HasEntity(goal.EntityID) {
				return fmt.Errorf("%w: live.goal.entity_id", ErrInvalid)
			}
			fieldType, ok := ctx.World.FieldType(goal.EntityID, goal.Field)
			if !ok || fieldType != "number" {
				return fmt.Errorf("%w: live.goal.field", ErrInvalid)
			}
		}
		if !finite(goal.Target) {
			return fmt.Errorf("%w: live.goal.target", ErrInvalid)
		}
	case "reach":
		if !ctx.World.HasEntity(goal.EntityID) {
			return fmt.Errorf("%w: live.goal.entity_id", ErrInvalid)
		}
	case "survive":
		if !finite(timer) || timer < 1 {
			return fmt.Errorf("%w: live.goal.timer", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: live.goal.type", ErrInvalid)
	}
	return nil
}

var _ = math.IsNaN
