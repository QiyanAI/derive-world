// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
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

	OpenWorldViewStage    = "stage"
	OpenWorldViewCards    = "cards"
	OpenWorldViewTimeline = "timeline"

	OpenWorldStatusField = "status"
)

const (
	maxOpenWorldKinds          = 8
	maxOpenWorldNodes          = 40
	maxOpenWorldLinks          = 80
	maxOpenWorldRules          = 24
	maxOpenWorldKindFields     = 8
	maxOpenWorldRuleEffects    = 8
	maxOpenWorldKindNameRunes  = 60
	maxOpenWorldNodeNameRunes  = 120
	maxOpenWorldNodeTextRunes  = 1200
	maxOpenWorldFieldKeyRunes  = 40
	maxOpenWorldFieldLabelRune = 80
	maxOpenWorldLinkLabelRune  = 80
	maxOpenWorldViews          = 3
	openWorldWidth             = 1600.0
	openWorldHeight            = 1000.0
	openWorldMinX              = 40.0
	openWorldMaxX              = openWorldWidth - 40
	openWorldMinY              = 40.0
	openWorldMaxY              = openWorldHeight - 60
)

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
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Hue    int              `json:"hue"`
	Fields []OpenWorldField `json:"fields"`
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
	ID        string            `json:"id"`
	TriggerID string            `json:"trigger_id"`
	Effects   []OpenWorldEffect `json:"effects"`
}

// OpenWorldState is the open_world_v1 template. Ordering is array order.
type OpenWorldState struct {
	Title   string          `json:"title"`
	Premise string          `json:"premise"`
	Kinds   []OpenWorldKind `json:"kinds"`
	Nodes   []OpenWorldNode `json:"nodes"`
	Links   []OpenWorldLink `json:"links"`
	Rules   []OpenWorldRule `json:"rules"`
	Views   []string        `json:"views"`
	Tags    []string        `json:"tags"`
}

// OpenWorldAvailableViews is the frozen presentation vocabulary. A world
// chooses which compatible views it exposes; the state model is independent
// of any one rendering.
func OpenWorldAvailableViews() []string {
	return []string{OpenWorldViewStage, OpenWorldViewCards, OpenWorldViewTimeline}
}

// OpenWorldAvailableFields is the derive inheritance whitelist. Structure is a
// single unit because nodes, links, and rules only make sense together.
func OpenWorldAvailableFields() []string {
	return []string{"premise", "structure", "views", "tags"}
}

// OpenWorldKindInput is the create-time kind shape; ids are generated.
type OpenWorldKindInput struct {
	Name   string           `json:"name"`
	Hue    int              `json:"hue"`
	Fields []OpenWorldField `json:"fields"`
}

// OpenWorldNodeInput references a kind by its index in the content arrays.
type OpenWorldNodeInput struct {
	KindIndex int      `json:"kind_index"`
	Name      string   `json:"name"`
	Text      string   `json:"text"`
	X         *float64 `json:"x"`
	Y         *float64 `json:"y"`
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

// OpenWorldRuleInput references its trigger and effect nodes by index.
type OpenWorldRuleInput struct {
	TriggerIndex int                    `json:"trigger_index"`
	Effects      []OpenWorldEffectInput `json:"effects"`
}

// OpenWorldContent is the strict create body for open_world.
type OpenWorldContent struct {
	Premise string               `json:"premise"`
	Kinds   []OpenWorldKindInput `json:"kinds"`
	Nodes   []OpenWorldNodeInput `json:"nodes"`
	Links   []OpenWorldLinkInput `json:"links"`
	Rules   []OpenWorldRuleInput `json:"rules"`
	Views   []string             `json:"views"`
	Tags    []string             `json:"tags"`
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
	case OpenWorldViewStage, OpenWorldViewCards, OpenWorldViewTimeline:
		return true
	default:
		return false
	}
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
	views, err := normalizeOpenWorldViews(state.Views)
	if err != nil {
		return err
	}
	state.Views = views
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
		Title:   title,
		Premise: "",
		Kinds:   make([]OpenWorldKind, 0),
		Nodes:   make([]OpenWorldNode, 0),
		Links:   make([]OpenWorldLink, 0),
		Rules:   make([]OpenWorldRule, 0),
		Views:   OpenWorldAvailableViews(),
		Tags:    make([]string, 0),
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
		kindIDs := make(map[string]string, len(parent.Kinds))
		for _, kind := range parent.Kinds {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, err
			}
			kindIDs[kind.ID] = id.String()
			fields := make([]OpenWorldField, len(kind.Fields))
			copy(fields, kind.Fields)
			child.Kinds = append(child.Kinds, OpenWorldKind{
				ID: id.String(), Name: kind.Name, Hue: kind.Hue, Fields: fields,
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
			child.Rules = append(child.Rules, OpenWorldRule{
				ID: id.String(), TriggerID: nodeIDs[rule.TriggerID], Effects: effects,
			})
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
