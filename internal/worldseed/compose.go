// SPDX-License-Identifier: Apache-2.0
package worldseed

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/store"
	"github.com/QiyanAI/derive-world/internal/worldcap"
)

// Composition is the result of turning one seed into a create body.
type Composition struct {
	Content          objects.OpenWorldContent
	Summary          string
	Surfaces         []string
	Primary          string
	KindCount        int
	ThingCount       int
	InteractionCount int
}

// ReshapeResult is the result of merging one seed into an existing world.
type ReshapeResult struct {
	State            objects.OpenWorldState
	Summary          string
	Surfaces         []string
	Primary          string
	InteractionCount int
}

// Compose validates one seed and returns a complete create body. The returned
// body has not minted identity: kinds and things carry local refs, and the
// object create path remaps those refs inside the experience document.
func Compose(brief *Brief, title string) (*Composition, error) {
	if brief == nil {
		return nil, fmt.Errorf("%w: missing seed", ErrInvalid)
	}
	title = strings.TrimSpace(title)
	if title == "" || runeLength(title) > 200 {
		return nil, fmt.Errorf("%w: title", ErrInvalid)
	}
	if strings.TrimSpace(brief.Premise) == "" {
		return nil, fmt.Errorf("%w: premise", ErrInvalid)
	}
	if len(brief.Kinds) == 0 {
		hasStory := brief.Experience != nil && brief.Experience.Story != nil &&
			len(brief.Experience.Story.Chapters) > 0
		hasCustom := brief.Experience != nil && len(brief.Experience.Custom) > 0
		if !hasStory && !hasCustom {
			return nil, fmt.Errorf("%w: a world needs content, chapters or a custom surface", ErrInvalid)
		}
	}
	if len(brief.Kinds) > 0 && len(brief.Things) == 0 &&
		!(brief.Experience != nil && brief.Experience.Story != nil) &&
		!(brief.Experience != nil && brief.Experience.Workspace != nil) &&
		!(brief.Experience != nil && len(brief.Experience.Custom) > 0) {
		return nil, fmt.Errorf("%w: a world needs at least one thing", ErrInvalid)
	}
	world, err := authoredFromBrief(brief, title, &refSource{})
	if err != nil {
		return nil, err
	}
	experience, surfaces, primary, err := buildExperience(world, brief, nil)
	if err != nil {
		return nil, err
	}
	content := world.toContent(experience)
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("%w: encode content", ErrInvalid)
	}
	if len(encoded) > composeBudgetBytes {
		return nil, ErrTooLarge
	}
	raw, err := store.CanonicalJSON(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: encode content", ErrInvalid)
	}
	if _, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld,
		Title:      title,
		Content:    raw,
	}); err != nil {
		return nil, fmt.Errorf("%w: composed world does not validate: %v", ErrInvalid, err)
	}
	return &Composition{
		Content:          content,
		Summary:          summaryOf(brief),
		Surfaces:         surfaces,
		Primary:          primary,
		KindCount:        len(content.Kinds),
		ThingCount:       len(content.Nodes),
		InteractionCount: len(brief.Interactions),
	}, nil
}

// Reshape merges a seed into an existing open_world state and returns the full
// replacement state. Content is merged by name; existing ids are preserved.
// When the seed names an experience, the universal surfaces are rebuilt from
// the merged content while existing arbitrary forms and behaviors are kept.
func Reshape(current json.RawMessage, brief *Brief) (*ReshapeResult, error) {
	if brief == nil {
		return nil, fmt.Errorf("%w: missing seed", ErrInvalid)
	}
	var state objects.OpenWorldState
	if err := json.Unmarshal(current, &state); err != nil {
		return nil, fmt.Errorf("%w: current world", ErrInvalid)
	}
	if err := objects.ValidateOpenWorldState(&state); err != nil {
		return nil, fmt.Errorf("%w: current world", ErrInvalid)
	}
	world := authoredFromState(&state)
	if strings.TrimSpace(brief.Premise) != "" {
		world.Premise = strings.TrimSpace(brief.Premise)
	}
	if err := mergeBrief(world, brief); err != nil {
		return nil, err
	}
	preserved := inspectPreserved(state.Experience)
	if brief.Experience == nil {
		// A content-only change keeps the existing experience; new things of an
		// existing kind appear automatically through its kind view.
		state = world.toState(state.Experience)
		state.Title = world.Title
		state.Premise = world.Premise
		if err := objects.ValidateOpenWorldState(&state); err != nil {
			return nil, fmt.Errorf("%w: reshaped world does not validate: %v", ErrInvalid, err)
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			return nil, fmt.Errorf("%w: encode state", ErrInvalid)
		}
		if len(encoded) > reshapeBudgetBytes {
			return nil, ErrTooLarge
		}
		return &ReshapeResult{
			State: state, Summary: summaryOf(brief),
			Surfaces: preserved.surfaces, Primary: preserved.primary,
		}, nil
	}
	experience, surfaces, primary, err := buildExperience(world, brief, preserved)
	if err != nil {
		return nil, err
	}
	state = world.toState(experience)
	state.Title = world.Title
	if err := objects.ValidateOpenWorldState(&state); err != nil {
		return nil, fmt.Errorf("%w: reshaped world does not validate: %v", ErrInvalid, err)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("%w: encode state", ErrInvalid)
	}
	if len(encoded) > reshapeBudgetBytes {
		return nil, ErrTooLarge
	}
	return &ReshapeResult{
		State: state, Summary: summaryOf(brief),
		Surfaces: surfaces, Primary: primary,
		InteractionCount: len(brief.Interactions),
	}, nil
}

func summaryOf(brief *Brief) string {
	summary := strings.TrimSpace(brief.Premise)
	if summary == "" {
		summary = strings.TrimSpace(brief.Atmosphere)
	}
	if summary == "" {
		summary = "A world with its own kinds, relations and interactions."
	}
	return truncate(summary, 300)
}

func mergeBrief(world *authoredWorld, brief *Brief) error {
	for index := range brief.Kinds {
		seed := &brief.Kinds[index]
		kind := world.findKindByName(seed.Name)
		if kind == nil {
			hue := (198 + index*47) % 360
			if seed.Hue != nil {
				hue = *seed.Hue
			}
			kind = &authoredKind{
				ID: newID(), Name: seed.Name, Hue: hue, Motion: seed.Motion,
				Form: strings.TrimSpace(seed.Form), Solid: seed.Solid,
				Appearance: objects.DefaultOpenWorldAppearance(),
			}
			if kind.Form == "" {
				kind.Form = generatedForm(seed.Name, index)
			}
			kind.Appearance.Shape = classicShape(len(world.Kinds))
			kind.Appearance.Motion = classicMotion(seed.Motion)
			world.Kinds = append(world.Kinds, kind)
		}
		if seed.Hue != nil {
			kind.Hue = *seed.Hue
		}
		if seed.Motion != "" {
			kind.Motion = seed.Motion
			kind.Appearance.Motion = classicMotion(seed.Motion)
		}
		if strings.TrimSpace(seed.Form) != "" {
			kind.Form = strings.TrimSpace(seed.Form)
		}
		if seed.Solid {
			kind.Solid = true
		}
		for _, field := range seed.Fields {
			if fieldExists(kind, field.Label) {
				continue
			}
			if len(kind.Fields) >= 8 {
				return fmt.Errorf("%w: too many fields", ErrInvalid)
			}
			kind.Fields = append(kind.Fields, objects.OpenWorldField{
				Key: fmt.Sprintf("f%d", len(kind.Fields)), Label: field.Label, Type: field.Type,
			})
		}
	}
	for index := range brief.Things {
		seed := &brief.Things[index]
		kind := world.findKindByName(brief.Kinds[seed.Kind].Name)
		if kind == nil {
			return fmt.Errorf("%w: thing kind", ErrInvalid)
		}
		values := map[string]any{}
		for fieldIndex, field := range brief.Kinds[seed.Kind].Fields {
			key := fmt.Sprintf("f%d", fieldIndex)
			if value, ok := seed.Values[field.Label]; ok {
				values[key] = value
			} else if field.Initial != nil {
				values[key] = field.Initial
			} else {
				values[key] = zeroValue(field.Type, fieldIndex)
			}
		}
		existing := world.findNodeByName(kind.ID, seed.Name)
		if existing != nil {
			if strings.TrimSpace(seed.Text) != "" {
				existing.Text = seed.Text
			}
			if existing.Values == nil {
				existing.Values = map[string]any{}
			}
			for key, value := range values {
				existing.Values[key] = value
			}
			if seed.X != nil {
				existing.X = 80 + clampFloat(*seed.X, 0, 1)*1440
			}
			if seed.Y != nil {
				existing.Y = 90 + clampFloat(*seed.Y, 0, 1)*820
			}
			continue
		}
		if len(world.Nodes) >= 40 {
			return fmt.Errorf("%w: too many things", ErrInvalid)
		}
		node := &authoredNode{
			ID: newID(), KindID: kind.ID, Name: seed.Name, Text: seed.Text,
			Values: values, Status: objects.OpenWorldStatusOpen,
		}
		node.X, node.Y = layoutPosition(len(world.Nodes), len(world.Nodes)+1, seed.Kind, "live")
		if seed.X != nil {
			node.X = 80 + clampFloat(*seed.X, 0, 1)*1440
		}
		if seed.Y != nil {
			node.Y = 90 + clampFloat(*seed.Y, 0, 1)*820
		}
		world.Nodes = append(world.Nodes, node)
	}
	for _, relation := range brief.Relations {
		if relation.From < 0 || relation.From >= len(brief.Things) ||
			relation.To < 0 || relation.To >= len(brief.Things) {
			continue
		}
		fromID := nodeIDForSeed(world, brief, relation.From)
		toID := nodeIDForSeed(world, brief, relation.To)
		if fromID != "" && toID != "" {
			world.addLink(fromID, toID, relation.Label)
		}
	}
	return nil
}

func fieldExists(kind *authoredKind, label string) bool {
	for _, field := range kind.Fields {
		if field.Label == label {
			return true
		}
	}
	return false
}

func nodeIDForSeed(world *authoredWorld, brief *Brief, index int) string {
	if index < 0 || index >= len(brief.Things) {
		return ""
	}
	seed := brief.Things[index]
	kind := world.findKindByName(brief.Kinds[seed.Kind].Name)
	if kind == nil {
		return ""
	}
	node := world.findNodeByName(kind.ID, seed.Name)
	if node == nil {
		return ""
	}
	return node.ID
}

func inspectPreserved(experience *worldcap.Experience) *preservedExperience {
	preserved := &preservedExperience{
		surfaces: []string{"space"}, primary: "space",
		live: preservedLive{}, storyJump: map[string]string{},
	}
	if experience == nil || experience.Root == nil {
		return preserved
	}
	preserved.behaviors = append([]worldcap.Behavior(nil), experience.Behaviors...)
	if experience.Root.Capability == "core.tabs" {
		props := decodeProps(experience.Root.Props)
		if defaultTab, ok := props["default"].(string); ok && defaultTab != "" {
			preserved.primary = defaultTab
		}
		surfaces := make([]string, 0, len(experience.Root.Children))
		for index := range experience.Root.Children {
			surfaces = append(surfaces, surfaceForCapability(experience.Root.Children[index].Capability))
		}
		if len(surfaces) > 0 {
			preserved.surfaces = surfaces
			if preserved.primary == "space" && !containsString(surfaces, "space") {
				preserved.primary = surfaces[0]
			}
		}
	}
	if experience.Root.Capability != "core.tabs" {
		preserved.surfaces = []string{surfaceForCapability(experience.Root.Capability)}
		if len(preserved.surfaces) == 1 {
			preserved.primary = preserved.surfaces[0]
		}
	}
	liveNode := findCapabilityNode(experience.Root, "core.live")
	if liveNode == nil {
		liveNode = findCapabilityNode(experience.Root, "core.space")
	}
	if liveNode != nil {
		props := decodeProps(liveNode.Props)
		preserved.live.views = map[string]map[string]any{}
		for _, raw := range asSlice(props["views"]) {
			view, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			kindID, _ := view["kind_id"].(string)
			if kindID != "" {
				preserved.live.views[kindID] = view
			}
		}
		if background, ok := props["background"].(map[string]any); ok {
			preserved.live.background = background
		}
		for _, raw := range asSlice(props["entity_actions"]) {
			if action, ok := raw.(map[string]any); ok {
				preserved.live.entityActions = append(preserved.live.entityActions, action)
			}
		}
		for _, raw := range asSlice(props["solid_kinds"]) {
			if kindID, ok := raw.(string); ok {
				preserved.live.solidKinds = append(preserved.live.solidKinds, kindID)
			}
		}
		if avatar, ok := props["avatar"].(map[string]any); ok {
			preserved.live.avatar = avatar
		}
		if spawn, ok := props["spawn"].(map[string]any); ok {
			preserved.live.spawn = spawn
		}
		if speed, ok := props["speed"].(float64); ok {
			preserved.live.speed = speed
		}
		if hint, ok := props["hint"].(string); ok {
			preserved.live.hint = hint
		}
		if show, ok := props["show_links"].(bool); ok {
			preserved.live.showLinks = show
		}
		if style, ok := props["link_style"].(string); ok {
			preserved.live.linkStyle = style
		}
		if timeline, ok := props["timeline"].(map[string]any); ok {
			preserved.live.timeline = timeline
		}
		if particles, ok := props["particles"].(map[string]any); ok {
			preserved.live.particles = particles
		}
	}
	customNode := findCapabilityNode(experience.Root, "core.custom")
	if customNode != nil {
		preserved.customRaw = append(json.RawMessage(nil), customNode.Props...)
	}
	storyNode := findCapabilityNode(experience.Root, "core.story")
	if storyNode != nil {
		props := decodeProps(storyNode.Props)
		if kindID, ok := props["chapter_kind_id"].(string); ok {
			preserved.storyKindID = kindID
		}
		if variable, ok := props["progress_variable"].(string); ok {
			preserved.storyVariable = variable
		}
		for _, raw := range asSlice(props["jump_behaviors"]) {
			jump, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			entityID, _ := jump["entity_id"].(string)
			behaviorID, _ := jump["behavior_id"].(string)
			if entityID != "" && behaviorID != "" {
				preserved.storyJump[entityID] = behaviorID
			}
		}
	}
	return preserved
}

func surfaceForCapability(capability string) string {
	switch capability {
	case "core.live":
		return "live"
	case "core.story":
		return "story"
	case "core.workspace":
		return "workspace"
	case "core.custom":
		return "custom"
	case "core.space":
		return "space"
	default:
		return "space"
	}
}

func asSlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func decodeProps(raw json.RawMessage) map[string]any {
	var props map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &props) != nil {
		return map[string]any{}
	}
	return props
}

func findCapabilityNode(node *worldcap.ViewNode, capability string) *worldcap.ViewNode {
	if node == nil {
		return nil
	}
	if node.Capability == capability {
		return node
	}
	for index := range node.Children {
		if found := findCapabilityNode(&node.Children[index], capability); found != nil {
			return found
		}
	}
	return nil
}
