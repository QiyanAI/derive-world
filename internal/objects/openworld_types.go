// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/worldcap"
)

// open_world is a declarative world model. Users define their own kinds of
// things, the things themselves, the links between them, and small rules that
// describe how one change leads to another. Nothing here is executable code:
// every rule is a bounded data structure interpreted by the server, so a world
// can be saved, reloaded, shared, and derived without trusting its content.
const (
	TypeOpenWorld     = "open_world"
	TemplateOpenWorld = "open_world_v1"

	OpenWorldStatusOpen = "open"
	OpenWorldStatusDone = "done"

	OpenWorldFieldText   = "text"
	OpenWorldFieldNumber = "number"
	OpenWorldFieldFlag   = "flag"

	OpenWorldOpSet    = "set"
	OpenWorldOpAdd    = "add"
	OpenWorldOpToggle = "toggle"

	OpenWorldViewScene    = "scene"
	OpenWorldViewStage    = "stage"
	OpenWorldViewCards    = "cards"
	OpenWorldViewTimeline = "timeline"

	OpenWorldStatusField = "status"

	// The presentation vocabulary is a closed, interpreted set of visual
	// primitives. It is shared by every open world: a world chooses values,
	// never code, and the client renders the same primitives for all worlds.
	OpenWorldShapeOrb    = "orb"
	OpenWorldShapeBeacon = "beacon"
	OpenWorldShapeWave   = "wave"
	OpenWorldShapeShard  = "shard"
	OpenWorldShapeRing   = "ring"
	OpenWorldShapeGlyph  = "glyph"
	OpenWorldShapeBloom  = "bloom"
	OpenWorldShapeThread = "thread"

	OpenWorldMotionNone    = "none"
	OpenWorldMotionPulse   = "pulse"
	OpenWorldMotionFloat   = "float"
	OpenWorldMotionSway    = "sway"
	OpenWorldMotionFlicker = "flicker"
	OpenWorldMotionBreathe = "breathe"
	OpenWorldMotionOrbit   = "orbit"

	OpenWorldLabelAlways = "always"
	OpenWorldLabelHover  = "hover"
	OpenWorldLabelHidden = "hidden"

	OpenWorldLayerBack  = "back"
	OpenWorldLayerMid   = "mid"
	OpenWorldLayerFront = "front"

	OpenWorldBindingScale   = "scale"
	OpenWorldBindingGlow    = "glow"
	OpenWorldBindingOpacity = "opacity"
	OpenWorldBindingHue     = "hue_shift"
	OpenWorldBindingSpeed   = "speed"

	OpenWorldBackdropHorizon = "horizon"
	OpenWorldBackdropVoid    = "void"
	OpenWorldBackdropLoom    = "loom"

	OpenWorldAmbientNone   = "none"
	OpenWorldAmbientStars  = "stars"
	OpenWorldAmbientMotes  = "motes"
	OpenWorldAmbientRain   = "rain"
	OpenWorldAmbientEmbers = "embers"

	OpenWorldLinkThread = "thread"
	OpenWorldLinkArc    = "arc"
	OpenWorldLinkOrbit  = "orbit"
	OpenWorldLinkHidden = "hidden"

	OpenWorldCondEq     = "eq"
	OpenWorldCondNe     = "ne"
	OpenWorldCondGt     = "gt"
	OpenWorldCondGte    = "gte"
	OpenWorldCondLt     = "lt"
	OpenWorldCondLte    = "lte"
	OpenWorldCondTruthy = "truthy"
	OpenWorldCondFalsy  = "falsy"
)

const (
	maxOpenWorldKinds          = 8
	maxOpenWorldNodes          = 40
	maxOpenWorldLinks          = 80
	maxOpenWorldRules          = 24
	maxOpenWorldKindFields     = 8
	maxOpenWorldRuleEffects    = 8
	maxOpenWorldBindings       = 4
	maxOpenWorldKindNameRunes  = 60
	maxOpenWorldNodeNameRunes  = 120
	maxOpenWorldNodeTextRunes  = 1200
	maxOpenWorldFieldKeyRunes  = 40
	maxOpenWorldFieldLabelRune = 80
	maxOpenWorldLinkLabelRune  = 80
	maxOpenWorldViews          = 4
	openWorldMinSize           = 12.0
	openWorldMaxSize           = 200.0
	openWorldMinHorizon        = 0
	openWorldMaxHorizon        = 100
	openWorldWidth             = 1600.0
	openWorldHeight            = 1000.0
	openWorldMinX              = 40.0
	openWorldMaxX              = openWorldWidth - 40
	openWorldMinY              = 40.0
	openWorldMaxY              = openWorldHeight - 60

	maxOpenWorldVariables        = 32
	maxOpenWorldVariableKeyRunes = 40
	maxOpenWorldVariableTextRune = 1200
	maxOpenWorldVariablesBytes   = 8 * 1024
)

// DefaultOpenWorldAppearance is the neutral look of a kind that has not chosen
// one. It is data, not a branch on any particular world.
func DefaultOpenWorldAppearance() OpenWorldAppearance {
	return OpenWorldAppearance{
		Shape:     OpenWorldShapeOrb,
		Size:      48,
		Motion:    OpenWorldMotionFloat,
		Intensity: 50,
		Glow:      30,
		Label:     OpenWorldLabelAlways,
		Layer:     OpenWorldLayerMid,
	}
}

// DefaultOpenWorldPresentation is the neutral scene of a world that has not
// chosen one. Every value remains user-editable through the product.
func DefaultOpenWorldPresentation() OpenWorldPresentation {
	return OpenWorldPresentation{
		Scene: OpenWorldScene{
			Backdrop:       OpenWorldBackdropHorizon,
			SkyHue:         222,
			GroundHue:      205,
			Horizon:        62,
			AccentHue:      42,
			Ambient:        OpenWorldAmbientStars,
			AmbientDensity: 35,
		},
		LinkStyle: OpenWorldLinkThread,
	}
}

// OpenWorldAppearance is one kind's declarative visual identity. The client
// interprets these values; it never loads an image, font, or script from a
// world. Both the coastal and the abstract demo worlds use this same shape.
type OpenWorldAppearance struct {
	Shape     string  `json:"shape"`
	Size      float64 `json:"size"`
	Motion    string  `json:"motion"`
	Intensity int     `json:"intensity"`
	Glow      int     `json:"glow"`
	Label     string  `json:"label"`
	Layer     string  `json:"layer"`
}

// OpenWorldBinding maps one declared field value onto one visual property.
// It is a bounded linear map, not an expression language.
type OpenWorldBinding struct {
	Field  string  `json:"field"`
	Target string  `json:"target"`
	InMin  float64 `json:"in_min"`
	InMax  float64 `json:"in_max"`
	OutMin float64 `json:"out_min"`
	OutMax float64 `json:"out_max"`
}

// OpenWorldScene is the world's own environment definition.
type OpenWorldScene struct {
	Backdrop       string `json:"backdrop"`
	SkyHue         int    `json:"sky_hue"`
	GroundHue      int    `json:"ground_hue"`
	Horizon        int    `json:"horizon"`
	AccentHue      int    `json:"accent_hue"`
	Ambient        string `json:"ambient"`
	AmbientDensity int    `json:"ambient_density"`
}

// OpenWorldPresentation is the whole visual layer of one world.
type OpenWorldPresentation struct {
	Scene     OpenWorldScene `json:"scene"`
	LinkStyle string         `json:"link_style"`
}

// OpenWorldCondition is an optional guard on a rule. It reads current values
// only; it cannot call anything, loop, or run user code.
type OpenWorldCondition struct {
	NodeID string `json:"node_id"`
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value,omitempty"`
}

// OpenWorldField is one user-defined value slot on a kind of thing.
type OpenWorldField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

// OpenWorldKind is a user-defined category of thing in a world. There is no
// server-side fixed taxonomy: "city", "rumour", "tide", "character" and
// anything else are equally valid.
type OpenWorldKind struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Hue        int                 `json:"hue"`
	Fields     []OpenWorldField    `json:"fields"`
	Appearance OpenWorldAppearance `json:"appearance"`
	Bindings   []OpenWorldBinding  `json:"bindings,omitempty"`
}

// OpenWorldNode is one concrete thing. Values are keyed by the fields declared
// on its kind.
type OpenWorldNode struct {
	ID     string         `json:"id"`
	KindID string         `json:"kind_id"`
	Name   string         `json:"name"`
	Text   string         `json:"text"`
	Values map[string]any `json:"values"`
	Status string         `json:"status"`
	X      float64        `json:"x"`
	Y      float64        `json:"y"`
}

// OpenWorldLink is a labelled relation between two things.
type OpenWorldLink struct {
	ID    string `json:"id"`
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

// OpenWorldEffect is one declarative consequence of a rule.
type OpenWorldEffect struct {
	NodeID string `json:"node_id"`
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
}

// OpenWorldRule applies its effects in order when its trigger node changes.
// Rules never chain: one interaction runs one pass so a world cannot loop.
type OpenWorldRule struct {
	ID        string              `json:"id"`
	TriggerID string              `json:"trigger_id"`
	Condition *OpenWorldCondition `json:"condition,omitempty"`
	Effects   []OpenWorldEffect   `json:"effects"`
}

// OpenWorldState is the open_world_v1 template. Ordering is array order.
type OpenWorldState struct {
	Title        string                `json:"title"`
	Premise      string                `json:"premise"`
	Kinds        []OpenWorldKind       `json:"kinds"`
	Nodes        []OpenWorldNode       `json:"nodes"`
	Links        []OpenWorldLink       `json:"links"`
	Rules        []OpenWorldRule       `json:"rules"`
	Views        []string              `json:"views"`
	Tags         []string              `json:"tags"`
	Presentation OpenWorldPresentation `json:"presentation"`
	// Variables are bounded world-wide values shared by every experience
	// surface. They are not entity fields: a story can keep its reading
	// position, a game its score, and a workspace its summary inputs without
	// inventing a fake entity for each value.
	Variables map[string]any `json:"variables,omitempty"`
	// Experience is the optional capability-composed experience. Worlds that
	// predate it omit the field and keep using views/presentation exactly as
	// before. Its presence never changes the core entity/field/rule model.
	Experience *worldcap.Experience `json:"experience,omitempty"`
}

// OpenWorldAvailableViews is the frozen presentation vocabulary. A world
// chooses which compatible views it exposes; the state model is independent
// of any one rendering.
func OpenWorldAvailableViews() []string {
	return []string{OpenWorldViewScene, OpenWorldViewStage, OpenWorldViewCards, OpenWorldViewTimeline}
}

// OpenWorldAvailableFields is the derive inheritance whitelist. Structure is a
// single unit because nodes, links, and rules only make sense together.
func OpenWorldAvailableFields() []string {
	return []string{"premise", "structure", "views", "tags"}
}

// OpenWorldKindInput is the create-time kind shape; ids are generated.
// Ref is an optional local slug that lets an experience document submitted in
// the same create body refer to a kind before the server mints its UUID. The
// server remaps every declared reference from Ref to the generated id.
type OpenWorldKindInput struct {
	Ref        string               `json:"ref,omitempty"`
	Name       string               `json:"name"`
	Hue        int                  `json:"hue"`
	Fields     []OpenWorldField     `json:"fields"`
	Appearance *OpenWorldAppearance `json:"appearance"`
	Bindings   []OpenWorldBinding   `json:"bindings"`
}

// OpenWorldNodeInput references a kind by its index in the content arrays.
// Ref is the same local-slug mechanism used by OpenWorldKindInput.
type OpenWorldNodeInput struct {
	Ref       string         `json:"ref,omitempty"`
	KindIndex int            `json:"kind_index"`
	Name      string         `json:"name"`
	Text      string         `json:"text"`
	X         *float64       `json:"x"`
	Y         *float64       `json:"y"`
	Values    map[string]any `json:"values"`
}

// ValidOpenWorldCreateRef reports whether a create-time local ref is a bounded
// slug. Refs never become object identity; they exist only until remapping.
func ValidOpenWorldCreateRef(value string) bool {
	if len(value) < 1 || len(value) > 40 {
		return false
	}
	for index, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && index > 0:
		case (r == '_' || r == '-' || r == '.') && index > 0:
		default:
			return false
		}
	}
	return true
}

// OpenWorldLinkInput references nodes by their content indexes.
type OpenWorldLinkInput struct {
	FromIndex int    `json:"from_index"`
	ToIndex   int    `json:"to_index"`
	Label     string `json:"label"`
}

// OpenWorldEffectInput references a node by its content index.
type OpenWorldEffectInput struct {
	NodeIndex int    `json:"node_index"`
	Field     string `json:"field"`
	Op        string `json:"op"`
	Value     any    `json:"value"`
}

// OpenWorldConditionInput references its node by content index.
type OpenWorldConditionInput struct {
	NodeIndex int    `json:"node_index"`
	Field     string `json:"field"`
	Op        string `json:"op"`
	Value     any    `json:"value"`
}

// OpenWorldRuleInput references its trigger, condition, and effect nodes by
// index.
type OpenWorldRuleInput struct {
	TriggerIndex int                      `json:"trigger_index"`
	Condition    *OpenWorldConditionInput `json:"condition"`
	Effects      []OpenWorldEffectInput   `json:"effects"`
}

// OpenWorldContent is the strict create body for open_world.
type OpenWorldContent struct {
	Premise      string                 `json:"premise"`
	Kinds        []OpenWorldKindInput   `json:"kinds"`
	Nodes        []OpenWorldNodeInput   `json:"nodes"`
	Links        []OpenWorldLinkInput   `json:"links"`
	Rules        []OpenWorldRuleInput   `json:"rules"`
	Views        []string               `json:"views"`
	Tags         []string               `json:"tags"`
	Presentation *OpenWorldPresentation `json:"presentation"`
	Variables    map[string]any         `json:"variables,omitempty"`
	Experience   *worldcap.Experience   `json:"experience"`
}

func validOpenWorldFieldType(value string) bool {
	switch value {
	case OpenWorldFieldText, OpenWorldFieldNumber, OpenWorldFieldFlag:
		return true
	default:
		return false
	}
}

func validOpenWorldView(value string) bool {
	switch value {
	case OpenWorldViewScene, OpenWorldViewStage, OpenWorldViewCards, OpenWorldViewTimeline:
		return true
	default:
		return false
	}
}

func validOpenWorldShape(value string) bool {
	switch value {
	case OpenWorldShapeOrb, OpenWorldShapeBeacon, OpenWorldShapeWave, OpenWorldShapeShard,
		OpenWorldShapeRing, OpenWorldShapeGlyph, OpenWorldShapeBloom, OpenWorldShapeThread:
		return true
	default:
		return false
	}
}

func validOpenWorldMotion(value string) bool {
	switch value {
	case OpenWorldMotionNone, OpenWorldMotionPulse, OpenWorldMotionFloat, OpenWorldMotionSway,
		OpenWorldMotionFlicker, OpenWorldMotionBreathe, OpenWorldMotionOrbit:
		return true
	default:
		return false
	}
}

func validOpenWorldLabel(value string) bool {
	switch value {
	case OpenWorldLabelAlways, OpenWorldLabelHover, OpenWorldLabelHidden:
		return true
	default:
		return false
	}
}

func validOpenWorldLayer(value string) bool {
	switch value {
	case OpenWorldLayerBack, OpenWorldLayerMid, OpenWorldLayerFront:
		return true
	default:
		return false
	}
}

func validOpenWorldBindingTarget(value string) bool {
	switch value {
	case OpenWorldBindingScale, OpenWorldBindingGlow, OpenWorldBindingOpacity,
		OpenWorldBindingHue, OpenWorldBindingSpeed:
		return true
	default:
		return false
	}
}

func validOpenWorldBackdrop(value string) bool {
	switch value {
	case OpenWorldBackdropHorizon, OpenWorldBackdropVoid, OpenWorldBackdropLoom:
		return true
	default:
		return false
	}
}

func validOpenWorldAmbient(value string) bool {
	switch value {
	case OpenWorldAmbientNone, OpenWorldAmbientStars, OpenWorldAmbientMotes,
		OpenWorldAmbientRain, OpenWorldAmbientEmbers:
		return true
	default:
		return false
	}
}

func validOpenWorldLinkStyle(value string) bool {
	switch value {
	case OpenWorldLinkThread, OpenWorldLinkArc, OpenWorldLinkOrbit, OpenWorldLinkHidden:
		return true
	default:
		return false
	}
}

func validOpenWorldConditionOp(value string) bool {
	switch value {
	case OpenWorldCondEq, OpenWorldCondNe, OpenWorldCondGt, OpenWorldCondGte,
		OpenWorldCondLt, OpenWorldCondLte, OpenWorldCondTruthy, OpenWorldCondFalsy:
		return true
	default:
		return false
	}
}

// normalizeOpenWorldAppearance validates a look and fills the neutral default
// only when no shape was chosen at all. A partially filled look is rejected;
// zero is never silently reinterpreted as a user choice.
func normalizeOpenWorldAppearance(input OpenWorldAppearance) (OpenWorldAppearance, error) {
	if input.Shape == "" {
		return DefaultOpenWorldAppearance(), nil
	}
	if !validOpenWorldShape(input.Shape) ||
		!validOpenWorldMotion(input.Motion) ||
		!validOpenWorldLabel(input.Label) ||
		!validOpenWorldLayer(input.Layer) {
		return OpenWorldAppearance{}, fmt.Errorf("%w: appearance", ErrInvalidRequest)
	}
	if !finiteOpenWorldNumber(input.Size) || input.Size < openWorldMinSize || input.Size > openWorldMaxSize {
		return OpenWorldAppearance{}, fmt.Errorf("%w: appearance.size", ErrInvalidRequest)
	}
	if input.Intensity < 0 || input.Intensity > 100 || input.Glow < 0 || input.Glow > 100 {
		return OpenWorldAppearance{}, fmt.Errorf("%w: appearance.glow", ErrInvalidRequest)
	}
	return input, nil
}

func validateOpenWorldBindingShape(binding OpenWorldBinding) error {
	if binding.Field == "" || !validOpenWorldBindingTarget(binding.Target) {
		return fmt.Errorf("%w: binding", ErrInvalidRequest)
	}
	if !finiteOpenWorldNumber(binding.InMin) || !finiteOpenWorldNumber(binding.InMax) ||
		!finiteOpenWorldNumber(binding.OutMin) || !finiteOpenWorldNumber(binding.OutMax) {
		return fmt.Errorf("%w: binding.range", ErrInvalidRequest)
	}
	if binding.InMax <= binding.InMin {
		return fmt.Errorf("%w: binding.in_range", ErrInvalidRequest)
	}
	switch binding.Target {
	case OpenWorldBindingScale, OpenWorldBindingSpeed:
		if binding.OutMin < 0.2 || binding.OutMin > 3 || binding.OutMax < 0.2 || binding.OutMax > 3 {
			return fmt.Errorf("%w: binding.out_range", ErrInvalidRequest)
		}
	case OpenWorldBindingGlow:
		if binding.OutMin < 0 || binding.OutMin > 100 || binding.OutMax < 0 || binding.OutMax > 100 {
			return fmt.Errorf("%w: binding.out_range", ErrInvalidRequest)
		}
	case OpenWorldBindingOpacity:
		if binding.OutMin < 0 || binding.OutMin > 1 || binding.OutMax < 0 || binding.OutMax > 1 {
			return fmt.Errorf("%w: binding.out_range", ErrInvalidRequest)
		}
	case OpenWorldBindingHue:
		if binding.OutMin < -180 || binding.OutMin > 180 || binding.OutMax < -180 || binding.OutMax > 180 {
			return fmt.Errorf("%w: binding.out_range", ErrInvalidRequest)
		}
	}
	return nil
}

// normalizeOpenWorldBindings validates the binding list against the kind's
// declared fields. Only numeric and flag fields can drive visuals; text is
// never interpreted as a style or expression.
func normalizeOpenWorldBindings(bindings []OpenWorldBinding, fields map[string]OpenWorldField) ([]OpenWorldBinding, error) {
	if len(bindings) == 0 {
		return nil, nil
	}
	if len(bindings) > maxOpenWorldBindings {
		return nil, fmt.Errorf("%w: bindings", ErrInvalidRequest)
	}
	out := make([]OpenWorldBinding, 0, len(bindings))
	targets := make(map[string]bool, len(bindings))
	for _, binding := range bindings {
		if err := validateOpenWorldBindingShape(binding); err != nil {
			return nil, err
		}
		field, ok := fields[binding.Field]
		if !ok || field.Type == OpenWorldFieldText {
			return nil, fmt.Errorf("%w: binding.field", ErrInvalidRequest)
		}
		if targets[binding.Target] {
			return nil, fmt.Errorf("%w: binding.target", ErrInvalidRequest)
		}
		targets[binding.Target] = true
		out = append(out, binding)
	}
	return out, nil
}

// normalizeOpenWorldPresentation validates the scene and fills defaults only
// when the entire scene is absent.
func normalizeOpenWorldPresentation(input OpenWorldPresentation) (OpenWorldPresentation, error) {
	if input.Scene.Backdrop == "" {
		out := DefaultOpenWorldPresentation()
		if input.LinkStyle != "" {
			if !validOpenWorldLinkStyle(input.LinkStyle) {
				return OpenWorldPresentation{}, fmt.Errorf("%w: presentation.link_style", ErrInvalidRequest)
			}
			out.LinkStyle = input.LinkStyle
		}
		return out, nil
	}
	scene := input.Scene
	if !validOpenWorldBackdrop(scene.Backdrop) || !validOpenWorldAmbient(scene.Ambient) {
		return OpenWorldPresentation{}, fmt.Errorf("%w: presentation.scene", ErrInvalidRequest)
	}
	if scene.SkyHue < 0 || scene.SkyHue > 359 || scene.GroundHue < 0 || scene.GroundHue > 359 ||
		scene.AccentHue < 0 || scene.AccentHue > 359 {
		return OpenWorldPresentation{}, fmt.Errorf("%w: presentation.hue", ErrInvalidRequest)
	}
	if scene.Horizon < openWorldMinHorizon || scene.Horizon > openWorldMaxHorizon {
		return OpenWorldPresentation{}, fmt.Errorf("%w: presentation.horizon", ErrInvalidRequest)
	}
	if scene.AmbientDensity < 0 || scene.AmbientDensity > 100 {
		return OpenWorldPresentation{}, fmt.Errorf("%w: presentation.ambient_density", ErrInvalidRequest)
	}
	linkStyle := input.LinkStyle
	if linkStyle == "" {
		linkStyle = OpenWorldLinkThread
	}
	if !validOpenWorldLinkStyle(linkStyle) {
		return OpenWorldPresentation{}, fmt.Errorf("%w: presentation.link_style", ErrInvalidRequest)
	}
	return OpenWorldPresentation{Scene: scene, LinkStyle: linkStyle}, nil
}

// validateOpenWorldCondition checks a rule guard against the world's own
// declared fields. It is deliberately small: comparisons only.
func validateOpenWorldCondition(state *OpenWorldState, condition *OpenWorldCondition, path string) error {
	node := findOpenWorldNode(state, condition.NodeID)
	if node == nil {
		return fmt.Errorf("%w: %s.node_id", ErrInvalidRequest, path)
	}
	if !validOpenWorldConditionOp(condition.Op) {
		return fmt.Errorf("%w: %s.op", ErrInvalidRequest, path)
	}
	if condition.Field == OpenWorldStatusField {
		if condition.Op != OpenWorldCondEq && condition.Op != OpenWorldCondNe {
			return fmt.Errorf("%w: %s.op", ErrInvalidRequest, path)
		}
		value, ok := condition.Value.(string)
		if !ok || (value != OpenWorldStatusOpen && value != OpenWorldStatusDone) {
			return fmt.Errorf("%w: %s.value", ErrInvalidRequest, path)
		}
		return nil
	}
	field := openWorldFieldFor(state, node, condition.Field)
	if field == nil {
		return fmt.Errorf("%w: %s.field", ErrInvalidRequest, path)
	}
	switch field.Type {
	case OpenWorldFieldText:
		if condition.Op != OpenWorldCondEq && condition.Op != OpenWorldCondNe {
			return fmt.Errorf("%w: %s.op", ErrInvalidRequest, path)
		}
		value, ok := condition.Value.(string)
		if !ok || utf8.RuneCountInString(value) > maxOpenWorldNodeTextRunes {
			return fmt.Errorf("%w: %s.value", ErrInvalidRequest, path)
		}
	case OpenWorldFieldNumber:
		switch condition.Op {
		case OpenWorldCondEq, OpenWorldCondNe, OpenWorldCondGt, OpenWorldCondGte,
			OpenWorldCondLt, OpenWorldCondLte:
		default:
			return fmt.Errorf("%w: %s.op", ErrInvalidRequest, path)
		}
		value, ok := condition.Value.(float64)
		if !ok || !finiteOpenWorldNumber(value) {
			return fmt.Errorf("%w: %s.value", ErrInvalidRequest, path)
		}
	case OpenWorldFieldFlag:
		switch condition.Op {
		case OpenWorldCondEq, OpenWorldCondNe:
			if _, ok := condition.Value.(bool); !ok {
				return fmt.Errorf("%w: %s.value", ErrInvalidRequest, path)
			}
		case OpenWorldCondTruthy, OpenWorldCondFalsy:
		default:
			return fmt.Errorf("%w: %s.op", ErrInvalidRequest, path)
		}
	}
	return nil
}

func runeCountAtMost(value string, limit int) bool {
	return utf8.RuneCountInString(strings.TrimSpace(value)) <= limit
}

func normalizeOpenWorldViews(input []string) ([]string, error) {
	if len(input) == 0 {
		return OpenWorldAvailableViews(), nil
	}
	if len(input) > maxOpenWorldViews {
		return nil, fmt.Errorf("%w: /views", ErrInvalidRequest)
	}
	seen := make(map[string]bool, len(input))
	out := make([]string, 0, len(input))
	for _, view := range input {
		if !validOpenWorldView(view) || seen[view] {
			return nil, fmt.Errorf("%w: /views", ErrInvalidRequest)
		}
		seen[view] = true
		out = append(out, view)
	}
	return out, nil
}

func finiteOpenWorldNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validateOpenWorldCoordinate(value float64, min, max float64, path string) error {
	if !finiteOpenWorldNumber(value) || value < min || value > max {
		return fmt.Errorf("%w: %s", ErrInvalidRequest, path)
	}
	return nil
}

// validateOpenWorldValue checks one user value against its declared field.
func validateOpenWorldValue(field OpenWorldField, value any, path string) error {
	if value == nil {
		return fmt.Errorf("%w: %s", ErrInvalidRequest, path)
	}
	switch field.Type {
	case OpenWorldFieldText:
		text, ok := value.(string)
		if !ok || utf8.RuneCountInString(text) > maxOpenWorldNodeTextRunes {
			return fmt.Errorf("%w: %s", ErrInvalidRequest, path)
		}
	case OpenWorldFieldNumber:
		number, ok := value.(float64)
		if !ok || !finiteOpenWorldNumber(number) {
			return fmt.Errorf("%w: %s", ErrInvalidRequest, path)
		}
	case OpenWorldFieldFlag:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%w: %s", ErrInvalidRequest, path)
		}
	default:
		return fmt.Errorf("%w: %s", ErrInvalidRequest, path)
	}
	return nil
}

// ValidateOpenWorldState checks ids, references, value types, and rule shapes.
// It is the only authority for whether an open world may be persisted.
func ValidateOpenWorldState(state *OpenWorldState) error {
	if runeLength(strings.TrimSpace(state.Title)) < 1 || runeLength(state.Title) > maxTitleRunes {
		return fmt.Errorf("%w: /title", ErrInvalidRequest)
	}
	if runeLength(state.Premise) > maxScenePremiseRunes {
		return fmt.Errorf("%w: /premise", ErrInvalidRequest)
	}
	presentation, err := normalizeOpenWorldPresentation(state.Presentation)
	if err != nil {
		return err
	}
	state.Presentation = presentation
	if len(state.Kinds) > maxOpenWorldKinds {
		return fmt.Errorf("%w: /kinds", ErrInvalidRequest)
	}
	kindByID := make(map[string]*OpenWorldKind, len(state.Kinds))
	fieldByKind := make(map[string]map[string]OpenWorldField, len(state.Kinds))
	for index := range state.Kinds {
		kind := &state.Kinds[index]
		if _, err := uuid.Parse(kind.ID); err != nil || kindByID[kind.ID] != nil {
			return fmt.Errorf("%w: /kinds/%d/id", ErrInvalidRequest, index)
		}
		if uuid.MustParse(kind.ID).Version() != 7 {
			return fmt.Errorf("%w: /kinds/%d/id", ErrInvalidRequest, index)
		}
		if runeLength(strings.TrimSpace(kind.Name)) < 1 || runeLength(kind.Name) > maxOpenWorldKindNameRunes {
			return fmt.Errorf("%w: /kinds/%d/name", ErrInvalidRequest, index)
		}
		if kind.Hue < 0 || kind.Hue > 359 {
			return fmt.Errorf("%w: /kinds/%d/hue", ErrInvalidRequest, index)
		}
		if len(kind.Fields) > maxOpenWorldKindFields {
			return fmt.Errorf("%w: /kinds/%d/fields", ErrInvalidRequest, index)
		}
		fields := make(map[string]OpenWorldField, len(kind.Fields))
		for fieldIndex, field := range kind.Fields {
			key := strings.TrimSpace(field.Key)
			if key == "" || runeLength(key) > maxOpenWorldFieldKeyRunes || fields[key].Key != "" {
				return fmt.Errorf("%w: /kinds/%d/fields/%d/key", ErrInvalidRequest, index, fieldIndex)
			}
			if key == OpenWorldStatusField {
				return fmt.Errorf("%w: /kinds/%d/fields/%d/key", ErrInvalidRequest, index, fieldIndex)
			}
			if runeLength(field.Label) > maxOpenWorldFieldLabelRune {
				return fmt.Errorf("%w: /kinds/%d/fields/%d/label", ErrInvalidRequest, index, fieldIndex)
			}
			if !validOpenWorldFieldType(field.Type) {
				return fmt.Errorf("%w: /kinds/%d/fields/%d/type", ErrInvalidRequest, index, fieldIndex)
			}
			fields[key] = field
		}
		appearance, err := normalizeOpenWorldAppearance(kind.Appearance)
		if err != nil {
			return fmt.Errorf("%w: /kinds/%d/appearance", ErrInvalidRequest, index)
		}
		bindings, err := normalizeOpenWorldBindings(kind.Bindings, fields)
		if err != nil {
			return fmt.Errorf("%w: /kinds/%d/bindings", ErrInvalidRequest, index)
		}
		kind.Appearance = appearance
		kind.Bindings = bindings
		kindByID[kind.ID] = kind
		fieldByKind[kind.ID] = fields
	}
	if len(state.Nodes) > maxOpenWorldNodes {
		return fmt.Errorf("%w: /nodes", ErrInvalidRequest)
	}
	nodeByID := make(map[string]*OpenWorldNode, len(state.Nodes))
	for index := range state.Nodes {
		node := &state.Nodes[index]
		if _, err := uuid.Parse(node.ID); err != nil || nodeByID[node.ID] != nil {
			return fmt.Errorf("%w: /nodes/%d/id", ErrInvalidRequest, index)
		}
		if uuid.MustParse(node.ID).Version() != 7 {
			return fmt.Errorf("%w: /nodes/%d/id", ErrInvalidRequest, index)
		}
		kind := kindByID[node.KindID]
		if kind == nil {
			return fmt.Errorf("%w: /nodes/%d/kind_id", ErrInvalidRequest, index)
		}
		if runeLength(strings.TrimSpace(node.Name)) < 1 || runeLength(node.Name) > maxOpenWorldNodeNameRunes {
			return fmt.Errorf("%w: /nodes/%d/name", ErrInvalidRequest, index)
		}
		if runeLength(node.Text) > maxOpenWorldNodeTextRunes {
			return fmt.Errorf("%w: /nodes/%d/text", ErrInvalidRequest, index)
		}
		if node.Status != OpenWorldStatusOpen && node.Status != OpenWorldStatusDone {
			return fmt.Errorf("%w: /nodes/%d/status", ErrInvalidRequest, index)
		}
		if err := validateOpenWorldCoordinate(node.X, openWorldMinX, openWorldMaxX,
			fmt.Sprintf("/nodes/%d/x", index)); err != nil {
			return err
		}
		if err := validateOpenWorldCoordinate(node.Y, openWorldMinY, openWorldMaxY,
			fmt.Sprintf("/nodes/%d/y", index)); err != nil {
			return err
		}
		if node.Values == nil {
			node.Values = map[string]any{}
		}
		fields := fieldByKind[node.KindID]
		for key, value := range node.Values {
			field, ok := fields[key]
			if !ok {
				return fmt.Errorf("%w: /nodes/%d/values/%s", ErrInvalidRequest, index, key)
			}
			if err := validateOpenWorldValue(field, value,
				fmt.Sprintf("/nodes/%d/values/%s", index, key)); err != nil {
				return err
			}
		}
		nodeByID[node.ID] = node
	}
	if len(state.Links) > maxOpenWorldLinks {
		return fmt.Errorf("%w: /links", ErrInvalidRequest)
	}
	seenLinks := make(map[string]bool, len(state.Links))
	seenPairs := make(map[string]bool, len(state.Links))
	for index, link := range state.Links {
		if _, err := uuid.Parse(link.ID); err != nil {
			return fmt.Errorf("%w: /links/%d/id", ErrInvalidRequest, index)
		}
		if uuid.MustParse(link.ID).Version() != 7 || seenLinks[link.ID] {
			return fmt.Errorf("%w: /links/%d/id", ErrInvalidRequest, index)
		}
		seenLinks[link.ID] = true
		if nodeByID[link.From] == nil || nodeByID[link.To] == nil || link.From == link.To {
			return fmt.Errorf("%w: /links/%d/endpoints", ErrInvalidRequest, index)
		}
		pair := link.From + "\x00" + link.To
		if seenPairs[pair] {
			return fmt.Errorf("%w: /links/%d/endpoints", ErrInvalidRequest, index)
		}
		seenPairs[pair] = true
		if runeLength(link.Label) > maxOpenWorldLinkLabelRune {
			return fmt.Errorf("%w: /links/%d/label", ErrInvalidRequest, index)
		}
	}
	if len(state.Rules) > maxOpenWorldRules {
		return fmt.Errorf("%w: /rules", ErrInvalidRequest)
	}
	seenRules := make(map[string]bool, len(state.Rules))
	for index, rule := range state.Rules {
		if _, err := uuid.Parse(rule.ID); err != nil {
			return fmt.Errorf("%w: /rules/%d/id", ErrInvalidRequest, index)
		}
		if uuid.MustParse(rule.ID).Version() != 7 || seenRules[rule.ID] {
			return fmt.Errorf("%w: /rules/%d/id", ErrInvalidRequest, index)
		}
		seenRules[rule.ID] = true
		if nodeByID[rule.TriggerID] == nil {
			return fmt.Errorf("%w: /rules/%d/trigger_id", ErrInvalidRequest, index)
		}
		if rule.Condition != nil {
			if err := validateOpenWorldCondition(state, rule.Condition,
				fmt.Sprintf("/rules/%d/condition", index)); err != nil {
				return err
			}
		}
		if len(rule.Effects) == 0 || len(rule.Effects) > maxOpenWorldRuleEffects {
			return fmt.Errorf("%w: /rules/%d/effects", ErrInvalidRequest, index)
		}
		seenTargets := make(map[string]bool, len(rule.Effects))
		for effectIndex, effect := range rule.Effects {
			node := nodeByID[effect.NodeID]
			if node == nil {
				return fmt.Errorf("%w: /rules/%d/effects/%d/node_id", ErrInvalidRequest, index, effectIndex)
			}
			targetKey := effect.NodeID + "\x00" + effect.Field
			if seenTargets[targetKey] {
				return fmt.Errorf("%w: /rules/%d/effects/%d/field", ErrInvalidRequest, index, effectIndex)
			}
			seenTargets[targetKey] = true
			if effect.Field == OpenWorldStatusField {
				if effect.Op != OpenWorldOpSet && effect.Op != OpenWorldOpToggle {
					return fmt.Errorf("%w: /rules/%d/effects/%d/op", ErrInvalidRequest, index, effectIndex)
				}
				if effect.Op == OpenWorldOpSet {
					value, _ := effect.Value.(string)
					if value != OpenWorldStatusOpen && value != OpenWorldStatusDone {
						return fmt.Errorf("%w: /rules/%d/effects/%d/value", ErrInvalidRequest, index, effectIndex)
					}
				}
				continue
			}
			fields := fieldByKind[node.KindID]
			field, ok := fields[effect.Field]
			if !ok {
				return fmt.Errorf("%w: /rules/%d/effects/%d/field", ErrInvalidRequest, index, effectIndex)
			}
			switch field.Type {
			case OpenWorldFieldText:
				if effect.Op != OpenWorldOpSet {
					return fmt.Errorf("%w: /rules/%d/effects/%d/op", ErrInvalidRequest, index, effectIndex)
				}
			case OpenWorldFieldNumber:
				if effect.Op != OpenWorldOpSet && effect.Op != OpenWorldOpAdd {
					return fmt.Errorf("%w: /rules/%d/effects/%d/op", ErrInvalidRequest, index, effectIndex)
				}
			case OpenWorldFieldFlag:
				if effect.Op != OpenWorldOpSet && effect.Op != OpenWorldOpToggle {
					return fmt.Errorf("%w: /rules/%d/effects/%d/op", ErrInvalidRequest, index, effectIndex)
				}
			}
			if effect.Op != OpenWorldOpToggle {
				if err := validateOpenWorldValue(field, effect.Value,
					fmt.Sprintf("/rules/%d/effects/%d/value", index, effectIndex)); err != nil {
					return err
				}
			}
		}
	}
	if err := validateOpenWorldVariables(state.Variables); err != nil {
		return err
	}
	if err := validateOpenWorldExperience(state); err != nil {
		return err
	}
	views, err := normalizeOpenWorldViews(state.Views)
	if err != nil {
		return err
	}
	state.Views = views
	if err := normalizeOpenWorldTags(state); err != nil {
		return err
	}
	return nil
}

// validateOpenWorldVariables keeps the shared surface state small and typed.
// Variables are deliberately not a second entity system: they are flat scalar
// values with a fixed key grammar, bounded count and bounded total bytes.
func validateOpenWorldVariables(variables map[string]any) error {
	if len(variables) == 0 {
		return nil
	}
	if len(variables) > maxOpenWorldVariables {
		return fmt.Errorf("%w: /variables", ErrInvalidRequest)
	}
	encoded, err := json.Marshal(variables)
	if err != nil || len(encoded) > maxOpenWorldVariablesBytes {
		return fmt.Errorf("%w: /variables", ErrInvalidRequest)
	}
	for name, value := range variables {
		if !ValidOpenWorldCreateRef(name) || runeLength(name) > maxOpenWorldVariableKeyRunes {
			return fmt.Errorf("%w: /variables/%s", ErrInvalidRequest, name)
		}
		switch typed := value.(type) {
		case string:
			if runeLength(typed) > maxOpenWorldVariableTextRune {
				return fmt.Errorf("%w: /variables/%s", ErrInvalidRequest, name)
			}
		case float64:
			if math.IsNaN(typed) || math.IsInf(typed, 0) {
				return fmt.Errorf("%w: /variables/%s", ErrInvalidRequest, name)
			}
		case bool:
		default:
			return fmt.Errorf("%w: /variables/%s", ErrInvalidRequest, name)
		}
	}
	return nil
}

func normalizeOpenWorldTags(state *OpenWorldState) error {
	if len(state.Tags) > maxTags {
		return fmt.Errorf("%w: /tags", ErrInvalidRequest)
	}
	seenTags := make(map[string]bool, len(state.Tags))
	for index, tag := range state.Tags {
		if runeLength(strings.TrimSpace(tag)) < 1 || runeLength(tag) > maxTagRunes || seenTags[tag] {
			return fmt.Errorf("%w: /tags/%d", ErrInvalidRequest, index)
		}
		seenTags[tag] = true
	}
	return nil
}

// OpenWorldDerivedState builds an independent child world. Structure is copied
// as one unit and every id is regenerated so the child never shares identity
// with its parent.
func OpenWorldDerivedState(title string, parent *OpenWorldState, selected map[string]bool) (*OpenWorldState, error) {
	child := &OpenWorldState{
		Title:        title,
		Premise:      "",
		Kinds:        make([]OpenWorldKind, 0),
		Nodes:        make([]OpenWorldNode, 0),
		Links:        make([]OpenWorldLink, 0),
		Rules:        make([]OpenWorldRule, 0),
		Views:        OpenWorldAvailableViews(),
		Tags:         make([]string, 0),
		Presentation: DefaultOpenWorldPresentation(),
	}
	if selected["premise"] {
		child.Premise = parent.Premise
	}
	if selected["views"] {
		child.Views = append([]string(nil), parent.Views...)
	}
	if selected["tags"] {
		child.Tags = append(child.Tags, parent.Tags...)
	}
	if selected["structure"] {
		// Presentation is part of the structure unit: visual bindings refer to
		// the same field and node identity as the rules, so the look travels
		// with the structure that it explains.
		child.Presentation = parent.Presentation
		kindIDs := make(map[string]string, len(parent.Kinds))
		for _, kind := range parent.Kinds {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, err
			}
			kindIDs[kind.ID] = id.String()
			fields := make([]OpenWorldField, len(kind.Fields))
			copy(fields, kind.Fields)
			bindings := make([]OpenWorldBinding, len(kind.Bindings))
			copy(bindings, kind.Bindings)
			child.Kinds = append(child.Kinds, OpenWorldKind{
				ID: id.String(), Name: kind.Name, Hue: kind.Hue, Fields: fields,
				Appearance: kind.Appearance, Bindings: bindings,
			})
		}
		nodeIDs := make(map[string]string, len(parent.Nodes))
		for _, node := range parent.Nodes {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, err
			}
			nodeIDs[node.ID] = id.String()
			values := make(map[string]any, len(node.Values))
			for key, value := range node.Values {
				values[key] = value
			}
			child.Nodes = append(child.Nodes, OpenWorldNode{
				ID: id.String(), KindID: kindIDs[node.KindID], Name: node.Name,
				Text: node.Text, Values: values, Status: node.Status,
				X: node.X, Y: node.Y,
			})
		}
		for _, link := range parent.Links {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, err
			}
			child.Links = append(child.Links, OpenWorldLink{
				ID: id.String(), From: nodeIDs[link.From], To: nodeIDs[link.To],
				Label: link.Label,
			})
		}
		for _, rule := range parent.Rules {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, err
			}
			effects := make([]OpenWorldEffect, len(rule.Effects))
			for index, effect := range rule.Effects {
				effects[index] = OpenWorldEffect{
					NodeID: nodeIDs[effect.NodeID], Field: effect.Field,
					Op: effect.Op, Value: effect.Value,
				}
			}
			var condition *OpenWorldCondition
			if rule.Condition != nil {
				condition = &OpenWorldCondition{
					NodeID: nodeIDs[rule.Condition.NodeID], Field: rule.Condition.Field,
					Op: rule.Condition.Op, Value: rule.Condition.Value,
				}
			}
			child.Rules = append(child.Rules, OpenWorldRule{
				ID: id.String(), TriggerID: nodeIDs[rule.TriggerID],
				Condition: condition, Effects: effects,
			})
		}
		if parent.Experience != nil {
			experience, err := worldcap.RemapExperience(parent.Experience, kindIDs, nodeIDs)
			if err != nil {
				return nil, err
			}
			child.Experience = experience
		}
		if len(parent.Variables) > 0 {
			child.Variables = make(map[string]any, len(parent.Variables))
			for name, value := range parent.Variables {
				child.Variables[name] = value
			}
		}
	}
	if err := ValidateOpenWorldState(child); err != nil {
		return nil, err
	}
	return child, nil
}

// DecodeOpenWorldState is used by projections that need a typed view.
func DecodeOpenWorldState(raw json.RawMessage) (*OpenWorldState, error) {
	var state OpenWorldState
	if err := decodeStrict(raw, &state); err != nil {
		return nil, fmt.Errorf("%w: state", ErrInvalidRequest)
	}
	return &state, nil
}
