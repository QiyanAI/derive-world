// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// CustomProgram is a bounded, declarative surface authored by the Personal AI
// when none of the fixed surfaces fits the user's intent. It is interpreted by
// the server and the client; it is never compiled to code. New forms are made
// from universal primitives (shapes, text, inputs, drag, keys, conditions,
// actions, timelines, procedural tones) rather than per-world pages.
type CustomProgram struct {
	Title      string             `json:"title,omitempty"`
	Hint       string             `json:"hint,omitempty"`
	Width      float64            `json:"width,omitempty"`
	Height     float64            `json:"height,omitempty"`
	Background spaceBackground    `json:"background"`
	Vars       map[string]any     `json:"vars,omitempty"`
	Nodes      []CustomNode       `json:"nodes"`
	Keys       []CustomKeyBinding `json:"keys,omitempty"`
	Timeline   *CustomTimeline    `json:"timeline,omitempty"`
}

type CustomNode struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	X           float64           `json:"x"`
	Y           float64           `json:"y"`
	W           float64           `json:"w,omitempty"`
	H           float64           `json:"h,omitempty"`
	R           float64           `json:"r,omitempty"`
	Rotate      float64           `json:"rotate,omitempty"`
	Scale       float64           `json:"scale,omitempty"`
	Opacity     float64           `json:"opacity,omitempty"`
	Hue         float64           `json:"hue,omitempty"`
	StrokeHue   float64           `json:"stroke_hue,omitempty"`
	StrokeWidth float64           `json:"stroke_width,omitempty"`
	Corner      float64           `json:"corner,omitempty"`
	Path        string            `json:"path,omitempty"`
	Text        string            `json:"text,omitempty"`
	Size        float64           `json:"size,omitempty"`
	Align       string            `json:"align,omitempty"`
	VisibleWhen *CustomCondition  `json:"visible_when,omitempty"`
	Bindings    []CustomBinding   `json:"bindings,omitempty"`
	BindText    *CustomTextSource `json:"bind_text,omitempty"`
	OnClick     []CustomAction    `json:"on_click,omitempty"`
	Input       *CustomInput      `json:"input,omitempty"`
	Drag        *CustomDrag       `json:"drag,omitempty"`
	ActionValue string            `json:"action_value,omitempty"`
}

type CustomBinding struct {
	Prop   string  `json:"prop"`
	Var    string  `json:"var,omitempty"`
	Entity string  `json:"entity_id,omitempty"`
	Field  string  `json:"field,omitempty"`
	InMin  float64 `json:"in_min"`
	InMax  float64 `json:"in_max"`
	OutMin float64 `json:"out_min"`
	OutMax float64 `json:"out_max"`
}

type CustomTextSource struct {
	Var    string `json:"var,omitempty"`
	Entity string `json:"entity_id,omitempty"`
	Field  string `json:"field,omitempty"`
}

type CustomInput struct {
	Var       string `json:"var"`
	Type      string `json:"type,omitempty"` // text | number
	MaxLength int    `json:"max_length,omitempty"`
}

type CustomDrag struct {
	Var  string  `json:"var"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Axis string  `json:"axis,omitempty"` // x | y
}

type CustomKeyBinding struct {
	Key     string         `json:"key"`
	Actions []CustomAction `json:"actions"`
}

type CustomCondition struct {
	Var      string            `json:"var,omitempty"`
	EntityID string            `json:"entity_id,omitempty"`
	Field    string            `json:"field,omitempty"`
	Op       string            `json:"op,omitempty"`
	Value    any               `json:"value,omitempty"`
	All      []CustomCondition `json:"all,omitempty"`
	Any      []CustomCondition `json:"any,omitempty"`
	Not      *CustomCondition  `json:"not,omitempty"`
}

type CustomAction struct {
	Type     string           `json:"type"`
	Name     string           `json:"name,omitempty"`
	Value    any              `json:"value,omitempty"`
	EntityID string           `json:"entity_id,omitempty"`
	Field    string           `json:"field,omitempty"`
	Cond     *CustomCondition `json:"cond,omitempty"`
	Then     []CustomAction   `json:"then,omitempty"`
	Else     []CustomAction   `json:"else,omitempty"`
	Freq     float64          `json:"freq,omitempty"`
	Duration float64          `json:"duration,omitempty"`
	Gain     float64          `json:"gain,omitempty"`
	Wave     string           `json:"wave,omitempty"`
}

type CustomTimeline struct {
	Duration float64       `json:"duration"`
	Loop     bool          `json:"loop"`
	Autoplay *bool         `json:"autoplay,omitempty"`
	Tracks   []CustomTrack `json:"tracks"`
}

type CustomTrack struct {
	NodeID   string      `json:"node_id"`
	Property string      `json:"property"`
	Keys     []CustomKey `json:"keys"`
}

type CustomKey struct {
	T float64 `json:"t"`
	V float64 `json:"v"`
}

const (
	maxCustomNodes       = 40
	maxCustomActions     = 8
	maxCustomActionDepth = 4
	maxCustomCondDepth   = 4
	maxCustomCondLeaves  = 8
	maxCustomTimeline    = 16
	maxCustomKeys        = 16
	maxCustomPropsBytes  = 12 * 1024
	maxCustomExpression  = 200
)

// ValidateCustom validates one custom surface program. It is also used by the
// worldseed composer and by the object-level custom_action executor.
func ValidateCustom(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var program CustomProgram
	if err := decodeStrict(raw, &program); err != nil {
		return nil, err
	}
	if runeLength(program.Title) > 80 || runeLength(program.Hint) > 200 {
		return nil, fmt.Errorf("%w: custom title/hint", ErrInvalid)
	}
	if program.Width == 0 {
		program.Width = 1600
	}
	if program.Height == 0 {
		program.Height = 1000
	}
	if !finite(program.Width) || program.Width < 320 || program.Width > 2400 ||
		!finite(program.Height) || program.Height < 240 || program.Height > 1600 {
		return nil, fmt.Errorf("%w: custom size", ErrInvalid)
	}
	if program.Background.Mode == "" {
		program.Background = spaceBackground{Mode: "gradient", Hue: 222, Hue2: 205, Lightness: 10, AccentHue: 42}
	}
	if program.Background.Mode != "gradient" && program.Background.Mode != "solid" && program.Background.Mode != "none" {
		return nil, fmt.Errorf("%w: custom.background.mode", ErrInvalid)
	}
	if !finite(program.Background.Hue) || program.Background.Hue < 0 || program.Background.Hue > 359 ||
		!finite(program.Background.Hue2) || program.Background.Hue2 < 0 || program.Background.Hue2 > 359 ||
		!finite(program.Background.AccentHue) || program.Background.AccentHue < 0 || program.Background.AccentHue > 359 ||
		!finite(program.Background.Lightness) || program.Background.Lightness < 2 || program.Background.Lightness > 40 {
		return nil, fmt.Errorf("%w: custom.background", ErrInvalid)
	}
	if len(program.Vars) > 128 {
		return nil, fmt.Errorf("%w: custom.vars", ErrInvalid)
	}
	for name, value := range program.Vars {
		if !validVariableName(name) {
			return nil, fmt.Errorf("%w: custom.vars/%s", ErrInvalid, name)
		}
		if _, ok := scalarType(value); !ok {
			return nil, fmt.Errorf("%w: custom.vars/%s", ErrInvalid, name)
		}
	}
	if len(program.Nodes) < 1 || len(program.Nodes) > maxCustomNodes {
		return nil, fmt.Errorf("%w: custom.nodes", ErrInvalid)
	}
	seen := map[string]bool{}
	for index := range program.Nodes {
		node := &program.Nodes[index]
		if !validLocalID(node.ID) || seen[node.ID] {
			return nil, fmt.Errorf("%w: custom.nodes/%d/id", ErrInvalid, index)
		}
		seen[node.ID] = true
		if err := validateCustomNode(node, ctx); err != nil {
			return nil, fmt.Errorf("%w: custom.nodes/%d: %v", ErrInvalid, index, err)
		}
	}
	if len(program.Keys) > 16 {
		return nil, fmt.Errorf("%w: custom.keys", ErrInvalid)
	}
	seenKeys := map[string]bool{}
	for index := range program.Keys {
		binding := &program.Keys[index]
		if !validKey(binding.Key) || seenKeys[binding.Key] {
			return nil, fmt.Errorf("%w: custom.keys/%d/key", ErrInvalid, index)
		}
		seenKeys[binding.Key] = true
		if len(binding.Actions) < 1 || len(binding.Actions) > maxCustomActions {
			return nil, fmt.Errorf("%w: custom.keys/%d/actions", ErrInvalid, index)
		}
		for actionIndex := range binding.Actions {
			if err := validateCustomAction(&binding.Actions[actionIndex], ctx, 1); err != nil {
				return nil, fmt.Errorf("%w: custom.keys/%d/actions/%d: %v", ErrInvalid, index, actionIndex, err)
			}
		}
	}
	if program.Timeline != nil {
		if err := validateCustomTimeline(program.Timeline, seen); err != nil {
			return nil, err
		}
	}
	return json.Marshal(program)
}

func validateCustomNode(node *CustomNode, ctx ValidateContext) error {
	switch node.Type {
	case "box", "dot", "path", "text", "input":
	default:
		return fmt.Errorf("%w: unknown type", ErrInvalid)
	}
	if !finite(node.X) || node.X < 0 || node.X > 2400 ||
		!finite(node.Y) || node.Y < 0 || node.Y > 1600 {
		return fmt.Errorf("%w: position", ErrInvalid)
	}
	if !finite(node.W) || !finite(node.H) || !finite(node.R) ||
		!finite(node.Rotate) || !finite(node.Scale) || !finite(node.Opacity) ||
		!finite(node.Hue) || !finite(node.StrokeHue) || !finite(node.StrokeWidth) ||
		!finite(node.Corner) || !finite(node.Size) {
		return fmt.Errorf("%w: numeric", ErrInvalid)
	}
	if node.Scale == 0 {
		node.Scale = 1
	}
	if node.Opacity == 0 {
		node.Opacity = 1
	}
	if node.Hue == 0 && node.StrokeHue == 0 {
		node.Hue = 198
	}
	if node.Scale < 0.1 || node.Scale > 4 || node.Opacity < 0.05 || node.Opacity > 1 ||
		node.Hue < 0 || node.Hue > 359 || node.StrokeHue < 0 || node.StrokeHue > 359 ||
		node.StrokeWidth < 0 || node.StrokeWidth > 24 || node.Corner < 0 || node.Corner > 200 ||
		node.Size < 0 || node.Size > 96 {
		return fmt.Errorf("%w: range", ErrInvalid)
	}
	if node.Type == "path" {
		if err := validateSVGPath(node.Path); err != nil {
			return fmt.Errorf("%w: path", ErrInvalid)
		}
	}
	if runeLength(node.Text) > 500 || runeLength(node.ActionValue) > 120 {
		return fmt.Errorf("%w: text", ErrInvalid)
	}
	if node.Align != "" && node.Align != "start" && node.Align != "middle" && node.Align != "end" {
		return fmt.Errorf("%w: align", ErrInvalid)
	}
	if node.VisibleWhen != nil {
		if err := validateCustomCondition(node.VisibleWhen, ctx, 1); err != nil {
			return fmt.Errorf("%w: visible_when", err)
		}
	}
	if len(node.Bindings) > 8 {
		return fmt.Errorf("%w: bindings", ErrInvalid)
	}
	for index := range node.Bindings {
		if err := validateCustomBinding(&node.Bindings[index], ctx); err != nil {
			return fmt.Errorf("%w: bindings/%d: %v", ErrInvalid, index, err)
		}
	}
	if node.BindText != nil {
		if err := validateCustomTextSource(node.BindText, ctx); err != nil {
			return err
		}
	}
	if len(node.OnClick) > maxCustomActions {
		return fmt.Errorf("%w: on_click", ErrInvalid)
	}
	for index := range node.OnClick {
		if err := validateCustomAction(&node.OnClick[index], ctx, 1); err != nil {
			return fmt.Errorf("%w: on_click/%d: %v", ErrInvalid, index, err)
		}
	}
	if node.Type == "input" && node.Input == nil {
		return fmt.Errorf("%w: input required", ErrInvalid)
	}
	if node.Input != nil {
		if node.Type != "input" {
			return fmt.Errorf("%w: input type", ErrInvalid)
		}
		if !validVariableName(node.Input.Var) {
			return fmt.Errorf("%w: input.var", ErrInvalid)
		}
		if node.Input.Type != "" && node.Input.Type != "text" && node.Input.Type != "number" {
			return fmt.Errorf("%w: input.type", ErrInvalid)
		}
		if node.Input.MaxLength < 0 || node.Input.MaxLength > 1200 {
			return fmt.Errorf("%w: input.max_length", ErrInvalid)
		}
	}
	if node.Drag != nil {
		if !validVariableName(node.Drag.Var) || node.Drag.Min >= node.Drag.Max ||
			node.Drag.Axis != "" && node.Drag.Axis != "x" && node.Drag.Axis != "y" {
			return fmt.Errorf("%w: drag", ErrInvalid)
		}
	}
	return nil
}

func validateCustomBinding(binding *CustomBinding, ctx ValidateContext) error {
	switch binding.Prop {
	case "x", "y", "w", "h", "r", "rotate", "scale", "opacity", "hue", "stroke_hue":
	default:
		return fmt.Errorf("%w: prop", ErrInvalid)
	}
	if (binding.Var == "") == (binding.Entity == "") {
		return fmt.Errorf("%w: source", ErrInvalid)
	}
	if binding.Var != "" && !validVariableName(binding.Var) {
		return fmt.Errorf("%w: var", ErrInvalid)
	}
	if binding.Entity != "" {
		if !ctx.World.HasEntity(binding.Entity) {
			return fmt.Errorf("%w: entity", ErrInvalid)
		}
		fieldType, ok := ctx.World.FieldType(binding.Entity, binding.Field)
		if !ok || (fieldType != "number" && fieldType != "flag") {
			return fmt.Errorf("%w: field", ErrInvalid)
		}
	}
	if binding.InMax <= binding.InMin {
		return fmt.Errorf("%w: range", ErrInvalid)
	}
	return nil
}

func validateCustomTextSource(source *CustomTextSource, ctx ValidateContext) error {
	if (source.Var == "") == (source.Entity == "") {
		return fmt.Errorf("%w: text source", ErrInvalid)
	}
	if source.Var != "" && !validVariableName(source.Var) {
		return fmt.Errorf("%w: text source var", ErrInvalid)
	}
	if source.Entity != "" {
		if !ctx.World.HasEntity(source.Entity) {
			return fmt.Errorf("%w: text source entity", ErrInvalid)
		}
		if _, ok := ctx.World.FieldType(source.Entity, source.Field); !ok {
			return fmt.Errorf("%w: text source field", ErrInvalid)
		}
	}
	return nil
}

func validateCustomAction(action *CustomAction, ctx ValidateContext, depth int) error {
	if depth > maxCustomActionDepth {
		return fmt.Errorf("%w: nesting", ErrInvalid)
	}
	switch action.Type {
	case "set_var", "add_var":
		name := strings.TrimSpace(action.Name)
		if !validVariableName(name) {
			return fmt.Errorf("%w: name", ErrInvalid)
		}
		if action.Value == nil {
			return fmt.Errorf("%w: value", ErrInvalid)
		}
		if raw, ok := action.Value.(string); ok && raw == "$event.value" {
			break
		}
		if expr, ok := expressionValue(action.Value); ok {
			if err := validateExpression(expr); err != nil {
				return err
			}
			break
		}
		if _, ok := scalarType(action.Value); !ok {
			return fmt.Errorf("%w: value", ErrInvalid)
		}
		if action.Type == "add_var" {
			if _, ok := action.Value.(float64); !ok {
				return fmt.Errorf("%w: add value", ErrInvalid)
			}
		}
	case "toggle_var":
		if !validVariableName(strings.TrimSpace(action.Name)) {
			return fmt.Errorf("%w: name", ErrInvalid)
		}
	case "set_field":
		if !ctx.World.HasEntity(action.EntityID) {
			return fmt.Errorf("%w: entity", ErrInvalid)
		}
		fieldType, ok := ctx.World.FieldType(action.EntityID, action.Field)
		if !ok {
			return fmt.Errorf("%w: field", ErrInvalid)
		}
		if action.Field == "status" {
			status, ok := action.Value.(string)
			if !ok || (status != "open" && status != "done") {
				return fmt.Errorf("%w: status", ErrInvalid)
			}
			break
		}
		if expr, ok := expressionValue(action.Value); ok {
			if fieldType != "number" {
				return fmt.Errorf("%w: expression field", ErrInvalid)
			}
			if err := validateExpression(expr); err != nil {
				return err
			}
			break
		}
		if action.Value == "$event.value" {
			break
		}
		if !stateValueMatches(fieldType, action.Value) {
			return fmt.Errorf("%w: value", ErrInvalid)
		}
	case "if":
		if action.Cond == nil || len(action.Then) == 0 {
			return fmt.Errorf("%w: if", ErrInvalid)
		}
		if err := validateCustomCondition(action.Cond, ctx, 1); err != nil {
			return err
		}
		if len(action.Then) > maxCustomActions || len(action.Else) > maxCustomActions {
			return fmt.Errorf("%w: if actions", ErrInvalid)
		}
		for index := range action.Then {
			if err := validateCustomAction(&action.Then[index], ctx, depth+1); err != nil {
				return err
			}
		}
		for index := range action.Else {
			if err := validateCustomAction(&action.Else[index], ctx, depth+1); err != nil {
				return err
			}
		}
	case "sound":
		if !finite(action.Freq) || action.Freq < 20 || action.Freq > 12000 ||
			!finite(action.Duration) || action.Duration < 0.03 || action.Duration > 2 ||
			(action.Gain != 0 && (!finite(action.Gain) || action.Gain < 0 || action.Gain > 0.5)) {
			return fmt.Errorf("%w: sound", ErrInvalid)
		}
		switch action.Wave {
		case "", "sine", "triangle", "square", "sawtooth":
		default:
			return fmt.Errorf("%w: wave", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: type", ErrInvalid)
	}
	return nil
}

func validateCustomCondition(condition *CustomCondition, ctx ValidateContext, depth int) error {
	if depth > maxCustomCondDepth {
		return fmt.Errorf("%w: condition depth", ErrInvalid)
	}
	if len(condition.All) > 0 || len(condition.Any) > 0 || condition.Not != nil {
		total := len(condition.All) + len(condition.Any)
		if condition.Not != nil {
			total++
		}
		if total > maxCustomCondLeaves {
			return fmt.Errorf("%w: condition leaves", ErrInvalid)
		}
		for index := range condition.All {
			if err := validateCustomCondition(&condition.All[index], ctx, depth+1); err != nil {
				return err
			}
		}
		for index := range condition.Any {
			if err := validateCustomCondition(&condition.Any[index], ctx, depth+1); err != nil {
				return err
			}
		}
		if condition.Not != nil {
			return validateCustomCondition(condition.Not, ctx, depth+1)
		}
		return nil
	}
	switch condition.Op {
	case "eq", "ne", "gt", "gte", "lt", "lte", "truthy", "falsy":
	default:
		return fmt.Errorf("%w: condition op", ErrInvalid)
	}
	if (condition.Var == "") == (condition.EntityID == "") {
		return fmt.Errorf("%w: condition source", ErrInvalid)
	}
	if condition.Var != "" && !validVariableName(condition.Var) {
		return fmt.Errorf("%w: condition var", ErrInvalid)
	}
	if condition.EntityID != "" {
		if !ctx.World.HasEntity(condition.EntityID) {
			return fmt.Errorf("%w: condition entity", ErrInvalid)
		}
		if _, ok := ctx.World.FieldType(condition.EntityID, condition.Field); !ok {
			return fmt.Errorf("%w: condition field", ErrInvalid)
		}
	}
	if condition.Op != "truthy" && condition.Op != "falsy" && condition.Value == nil {
		return fmt.Errorf("%w: condition value", ErrInvalid)
	}
	return nil
}

func validateCustomTimeline(timeline *CustomTimeline, nodes map[string]bool) error {
	if !finite(timeline.Duration) || timeline.Duration < 1 || timeline.Duration > 120 {
		return fmt.Errorf("%w: custom.timeline.duration", ErrInvalid)
	}
	if len(timeline.Tracks) > maxCustomTimeline {
		return fmt.Errorf("%w: custom.timeline.tracks", ErrInvalid)
	}
	for index := range timeline.Tracks {
		track := &timeline.Tracks[index]
		if !nodes[track.NodeID] {
			return fmt.Errorf("%w: custom.timeline.tracks/%d/node", ErrInvalid, index)
		}
		switch track.Property {
		case "x", "y", "w", "h", "r", "rotate", "scale", "opacity", "hue":
		default:
			return fmt.Errorf("%w: custom.timeline.tracks/%d/property", ErrInvalid, index)
		}
		if len(track.Keys) < 2 || len(track.Keys) > maxCustomKeys {
			return fmt.Errorf("%w: custom.timeline.tracks/%d/keys", ErrInvalid, index)
		}
		previous := -1.0
		for keyIndex := range track.Keys {
			key := &track.Keys[keyIndex]
			if !finite(key.T) || key.T < 0 || key.T > timeline.Duration || key.T <= previous ||
				!finite(key.V) {
				return fmt.Errorf("%w: custom.timeline.tracks/%d/keys/%d", ErrInvalid, index, keyIndex)
			}
			previous = key.T
		}
	}
	return nil
}

func validKey(value string) bool {
	switch value {
	case "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Space", "Enter", "Escape":
		return true
	}
	if len(value) == 1 {
		r := value[0]
		return r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
	}
	return false
}

func expressionValue(value any) (string, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	expr, ok := object["expr"].(string)
	return expr, ok
}

// ExecuteCustomEvent resolves the actions for one event and applies them. It
// is used by the object-level custom_action action, so the client never has
// authority to write state directly.
func ExecuteCustomEvent(program *CustomProgram, event CustomEvent, world WorldAccess) ([]AppliedAction, error) {
	if program == nil {
		return nil, fmt.Errorf("%w: custom program", ErrInvalid)
	}
	variables, ok := world.(VariableWorld)
	if !ok {
		return nil, fmt.Errorf("%w: variables unavailable", ErrInvalid)
	}
	switch event.Event {
	case "click":
		node := findCustomNode(program, event.NodeID)
		if node == nil {
			return nil, fmt.Errorf("%w: custom node", ErrInvalid)
		}
		return executeCustomActions(node.OnClick, event, world, variables, 1)
	case "key":
		for index := range program.Keys {
			if program.Keys[index].Key == event.Key {
				return executeCustomActions(program.Keys[index].Actions, event, world, variables, 1)
			}
		}
		return nil, fmt.Errorf("%w: custom key", ErrInvalid)
	case "input":
		node := findCustomNode(program, event.NodeID)
		if node == nil || node.Input == nil {
			return nil, fmt.Errorf("%w: custom input", ErrInvalid)
		}
		value, err := coerceInput(node.Input.Type, event.Value)
		if err != nil {
			return nil, err
		}
		if err := variables.SetVariable(node.Input.Var, value); err != nil {
			return nil, err
		}
		return []AppliedAction{{
			Capability: "core.custom", Action: "input",
			Field: node.Input.Var, Value: value,
		}}, nil
	case "drag":
		node := findCustomNode(program, event.NodeID)
		if node == nil || node.Drag == nil {
			return nil, fmt.Errorf("%w: custom drag", ErrInvalid)
		}
		number, ok := event.Value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("%w: drag value", ErrInvalid)
		}
		if number < node.Drag.Min {
			number = node.Drag.Min
		}
		if number > node.Drag.Max {
			number = node.Drag.Max
		}
		if err := variables.SetVariable(node.Drag.Var, number); err != nil {
			return nil, err
		}
		return []AppliedAction{{
			Capability: "core.custom", Action: "drag",
			Field: node.Drag.Var, Value: number,
		}}, nil
	default:
		return nil, fmt.Errorf("%w: custom event", ErrInvalid)
	}
}

// CustomEvent is the bounded input one custom surface event carries.
type CustomEvent struct {
	Event  string
	NodeID string
	Key    string
	Value  any
}

func findCustomNode(program *CustomProgram, nodeID string) *CustomNode {
	for index := range program.Nodes {
		if program.Nodes[index].ID == nodeID {
			return &program.Nodes[index]
		}
	}
	return nil
}

func coerceInput(inputType string, value any) (any, error) {
	switch inputType {
	case "number":
		switch typed := value.(type) {
		case float64:
			if math.IsNaN(typed) || math.IsInf(typed, 0) {
				return nil, fmt.Errorf("%w: input number", ErrInvalid)
			}
			return typed, nil
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return nil, fmt.Errorf("%w: input number", ErrInvalid)
			}
			return parsed, nil
		default:
			return nil, fmt.Errorf("%w: input number", ErrInvalid)
		}
	default:
		text, ok := value.(string)
		if !ok || runeLength(text) > 1200 {
			return nil, fmt.Errorf("%w: input text", ErrInvalid)
		}
		return text, nil
	}
}

func executeCustomActions(actions []CustomAction, event CustomEvent,
	world WorldAccess, variables VariableWorld, depth int) ([]AppliedAction, error) {
	if depth > maxCustomActionDepth {
		return nil, fmt.Errorf("%w: custom action depth", ErrInvalid)
	}
	applied := make([]AppliedAction, 0, len(actions))
	for index := range actions {
		action := &actions[index]
		if action.Cond != nil {
			holds, err := evaluateCustomCondition(action.Cond, world, variables)
			if err != nil {
				return nil, err
			}
			branch := action.Else
			if holds {
				branch = action.Then
			}
			nested, err := executeCustomActions(branch, event, world, variables, depth+1)
			if err != nil {
				return nil, err
			}
			applied = append(applied, nested...)
			continue
		}
		switch action.Type {
		case "set_var", "add_var":
			value, err := resolveCustomValue(action.Value, event, variables)
			if err != nil {
				return nil, err
			}
			if action.Type == "add_var" {
				base := float64(0)
				if current, ok := variables.Variable(action.Name); ok {
					if number, ok := current.(float64); ok {
						base = number
					}
				}
				amount, ok := value.(float64)
				if !ok {
					return nil, fmt.Errorf("%w: add value", ErrInvalid)
				}
				value = base + amount
			}
			if _, ok := scalarType(value); !ok {
				return nil, fmt.Errorf("%w: value", ErrInvalid)
			}
			if err := variables.SetVariable(action.Name, value); err != nil {
				return nil, err
			}
			applied = append(applied, AppliedAction{
				Capability: "core.custom", Action: action.Type,
				Field: action.Name, Value: value,
			})
		case "toggle_var":
			current, has := variables.Variable(action.Name)
			flag := false
			if has {
				flag, _ = current.(bool)
			}
			next := !flag
			if err := variables.SetVariable(action.Name, next); err != nil {
				return nil, err
			}
			applied = append(applied, AppliedAction{
				Capability: "core.custom", Action: "toggle_var",
				Field: action.Name, Value: next,
			})
		case "set_field":
			value, err := resolveCustomValue(action.Value, event, variables)
			if err != nil {
				return nil, err
			}
			if action.Field == "status" {
				status, ok := value.(string)
				if !ok || (status != "open" && status != "done") {
					return nil, fmt.Errorf("%w: status", ErrInvalid)
				}
				if err := world.SetStatus(action.EntityID, status); err != nil {
					return nil, err
				}
				applied = append(applied, AppliedAction{
					Capability: "core.custom", Action: "set_field",
					EntityID: action.EntityID, Field: "status", Value: status,
				})
				continue
			}
			if err := world.SetValue(action.EntityID, action.Field, value); err != nil {
				return nil, err
			}
			applied = append(applied, AppliedAction{
				Capability: "core.custom", Action: "set_field",
				EntityID: action.EntityID, Field: action.Field, Value: value,
			})
		case "sound":
			// Sound has no world state. The client plays it from the validated
			// program; the server accepts the action so the surface stays
			// inspectable without creating a parallel trust path.
		default:
			return nil, fmt.Errorf("%w: custom action", ErrInvalid)
		}
	}
	return applied, nil
}

func resolveCustomValue(value any, event CustomEvent, variables VariableWorld) (any, error) {
	if text, ok := value.(string); ok && text == "$event.value" {
		return event.Value, nil
	}
	if expr, ok := expressionValue(value); ok {
		number, err := evaluateExpression(expr, variables)
		if err != nil {
			return nil, err
		}
		return number, nil
	}
	return value, nil
}

func evaluateCustomCondition(condition *CustomCondition, world WorldAccess,
	variables VariableWorld) (bool, error) {
	if condition == nil {
		return true, nil
	}
	if len(condition.All) > 0 {
		for index := range condition.All {
			holds, err := evaluateCustomCondition(&condition.All[index], world, variables)
			if err != nil || !holds {
				return false, err
			}
		}
		return true, nil
	}
	if len(condition.Any) > 0 {
		for index := range condition.Any {
			holds, err := evaluateCustomCondition(&condition.Any[index], world, variables)
			if err != nil {
				return false, err
			}
			if holds {
				return true, nil
			}
		}
		return false, nil
	}
	if condition.Not != nil {
		holds, err := evaluateCustomCondition(condition.Not, world, variables)
		return !holds, err
	}
	var actual any
	if condition.Var != "" {
		if value, ok := variables.Variable(condition.Var); ok {
			actual = value
		}
	} else if condition.EntityID != "" {
		if condition.Field == "status" {
			value, _ := world.Status(condition.EntityID)
			actual = value
		} else {
			value, _ := world.Value(condition.EntityID, condition.Field)
			actual = value
		}
	}
	return compareCustom(condition.Op, actual, condition.Value)
}

func compareCustom(op string, actual, expected any) (bool, error) {
	switch op {
	case "truthy":
		return truthy(actual), nil
	case "falsy":
		return !truthy(actual), nil
	case "eq":
		return equalScalar(actual, expected), nil
	case "ne":
		return !equalScalar(actual, expected), nil
	}
	left, ok := actual.(float64)
	if !ok {
		return false, nil
	}
	right, ok := expected.(float64)
	if !ok {
		return false, nil
	}
	switch op {
	case "gt":
		return left > right, nil
	case "gte":
		return left >= right, nil
	case "lt":
		return left < right, nil
	case "lte":
		return left <= right, nil
	default:
		return false, fmt.Errorf("%w: condition op", ErrInvalid)
	}
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case float64:
		return typed != 0
	case string:
		return typed != ""
	default:
		return false
	}
}

func equalScalar(left, right any) bool {
	switch left := left.(type) {
	case nil:
		return right == nil
	case bool:
		value, ok := right.(bool)
		return ok && left == value
	case float64:
		value, ok := right.(float64)
		return ok && left == value
	case string:
		value, ok := right.(string)
		return ok && left == value
	default:
		return false
	}
}

// evaluateExpression interprets a tiny arithmetic expression over numeric
// variables. It has no calls, loops, allocation or property access.
func evaluateExpression(expression string, variables VariableWorld) (float64, error) {
	parser := &expressionParser{input: strings.TrimSpace(expression), variables: variables}
	value, err := parser.parseSum()
	if err != nil {
		return 0, err
	}
	parser.skipSpace()
	if parser.pos != len(parser.input) {
		return 0, fmt.Errorf("%w: expression", ErrInvalid)
	}
	return value, nil
}

func validateExpression(expression string) error {
	parser := &expressionParser{input: strings.TrimSpace(expression), variables: nil}
	if _, err := parser.parseSum(); err != nil {
		return err
	}
	parser.skipSpace()
	if parser.pos != len(parser.input) {
		return fmt.Errorf("%w: expression", ErrInvalid)
	}
	return nil
}

type expressionParser struct {
	input     string
	pos       int
	variables VariableWorld
	depth     int
}

func (parser *expressionParser) skipSpace() {
	for parser.pos < len(parser.input) && parser.input[parser.pos] == ' ' {
		parser.pos++
	}
}

func (parser *expressionParser) parseSum() (float64, error) {
	value, err := parser.parseProduct()
	if err != nil {
		return 0, err
	}
	for {
		parser.skipSpace()
		if parser.pos >= len(parser.input) {
			return value, nil
		}
		op := parser.input[parser.pos]
		if op != '+' && op != '-' {
			return value, nil
		}
		parser.pos++
		next, err := parser.parseProduct()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			value += next
		} else {
			value -= next
		}
	}
}

func (parser *expressionParser) parseProduct() (float64, error) {
	value, err := parser.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		parser.skipSpace()
		if parser.pos >= len(parser.input) {
			return value, nil
		}
		op := parser.input[parser.pos]
		if op != '*' && op != '/' {
			return value, nil
		}
		parser.pos++
		next, err := parser.parseFactor()
		if err != nil {
			return 0, err
		}
		if op == '*' {
			value *= next
		} else {
			if next == 0 {
				return 0, fmt.Errorf("%w: division by zero", ErrInvalid)
			}
			value /= next
		}
	}
}

func (parser *expressionParser) parseFactor() (float64, error) {
	parser.skipSpace()
	if parser.pos >= len(parser.input) {
		return 0, fmt.Errorf("%w: expression", ErrInvalid)
	}
	if parser.input[parser.pos] == '-' {
		parser.pos++
		value, err := parser.parseFactor()
		return -value, err
	}
	if parser.input[parser.pos] == '(' {
		parser.pos++
		value, err := parser.parseSum()
		if err != nil {
			return 0, err
		}
		parser.skipSpace()
		if parser.pos >= len(parser.input) || parser.input[parser.pos] != ')' {
			return 0, fmt.Errorf("%w: expression", ErrInvalid)
		}
		parser.pos++
		return value, nil
	}
	if parser.input[parser.pos] == '$' {
		start := parser.pos + 1
		parser.pos = start
		for parser.pos < len(parser.input) {
			r := parser.input[parser.pos]
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-' {
				parser.pos++
				continue
			}
			break
		}
		name := parser.input[start:parser.pos]
		if !validVariableName(name) {
			return 0, fmt.Errorf("%w: expression variable", ErrInvalid)
		}
		if parser.variables == nil {
			return 0, nil
		}
		value, ok := parser.variables.Variable(name)
		if !ok {
			return 0, nil
		}
		number, ok := value.(float64)
		if !ok {
			return 0, fmt.Errorf("%w: expression variable type", ErrInvalid)
		}
		return number, nil
	}
	start := parser.pos
	for parser.pos < len(parser.input) {
		r := parser.input[parser.pos]
		if r >= '0' && r <= '9' || r == '.' {
			parser.pos++
			continue
		}
		break
	}
	if start == parser.pos {
		return 0, fmt.Errorf("%w: expression", ErrInvalid)
	}
	number, err := strconv.ParseFloat(parser.input[start:parser.pos], 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%w: expression number", ErrInvalid)
	}
	return number, nil
}

// customCapability exposes the declarative escape hatch as a registered
// presentation module. Its program is validated by the same authority the
// object-level custom_action executor uses, so the client and server agree on
// every node, event, condition and action.
type customCapability struct{}

func (customCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.custom", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Custom surface", Zh: "自定义表面"},
		Description: Localized{En: "A safe declarative surface for forms the fixed modules do not cover: shapes, text, inputs, keys, drag, conditions, actions, timelines and tones.", Zh: "为固定模块未覆盖的形式提供的安全声明式表面：形体、文字、输入、按键、拖动、条件、动作、时间线与音调。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"title": {Type: "string", MaxLength: 80, Title: Localized{En: "Title", Zh: "标题"}},
				"nodes": {Type: "array", Title: Localized{En: "Nodes", Zh: "节点"}},
				"keys":  {Type: "array", Title: Localized{En: "Key actions", Zh: "按键动作"}},
			},
			Required: []string{"nodes"},
		},
	}
}

func (customCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	return ValidateCustom(raw, ctx)
}

// CustomProgramFor returns the validated custom program of one surface node.
// The object layer uses it to resolve a client event into server-executed
// actions; the client never supplies the action list itself.
func CustomProgramFor(experience *Experience, surfaceID string) (*CustomProgram, bool) {
	node := findViewNode(experience, surfaceID)
	if node == nil || node.Capability != "core.custom" {
		return nil, false
	}
	var program CustomProgram
	if len(node.Props) == 0 || json.Unmarshal(node.Props, &program) != nil {
		return nil, false
	}
	return &program, true
}

func findViewNode(experience *Experience, id string) *ViewNode {
	if experience == nil || experience.Root == nil {
		return nil
	}
	return findViewNodeIn(experience.Root, id)
}

func findViewNodeIn(node *ViewNode, id string) *ViewNode {
	if node == nil {
		return nil
	}
	if node.ID == id {
		return node
	}
	for index := range node.Children {
		if found := findViewNodeIn(&node.Children[index], id); found != nil {
			return found
		}
	}
	return nil
}
