// SPDX-License-Identifier: Apache-2.0
package worldseed

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/worldcap"
)

type preservedLive struct {
	views         map[string]map[string]any
	background    map[string]any
	entityActions []map[string]any
	solidKinds    []string
	avatar        map[string]any
	spawn         map[string]any
	speed         float64
	hint          string
	showLinks     bool
	linkStyle     string
	timeline      map[string]any
	particles     map[string]any
}

type preservedExperience struct {
	surfaces      []string
	primary       string
	behaviors     []worldcap.Behavior
	live          preservedLive
	storyKindID   string
	storyVariable string
	storyJump     map[string]string
	customRaw     json.RawMessage
	audioRaw      json.RawMessage
	mediaRaw      json.RawMessage
}

type surfaceBuilder struct {
	world       *authoredWorld
	brief       *Brief
	preserved   *preservedExperience
	behaviors   []worldcap.Behavior
	behaviorIDs map[string]bool
	behaviorSeq int
	surfaces    []string
	primary     string
	chapterKind *authoredKind
	chapterIDs  []string
	storyProps  map[string]any
	err         error
}

func buildExperience(world *authoredWorld, brief *Brief,
	preserved *preservedExperience) (*worldcap.Experience, []string, string, error) {
	built := &surfaceBuilder{
		world: world, brief: brief, preserved: preserved,
		behaviorIDs: map[string]bool{},
	}
	if preserved != nil {
		built.behaviors = append(built.behaviors, preserved.behaviors...)
		for _, behavior := range preserved.behaviors {
			built.behaviorIDs[behavior.ID] = true
		}
	}
	if brief.Experience != nil {
		built.surfaces = surfacesOf(brief.Experience)
		built.primary = brief.Experience.Primary
		if preserved != nil && len(brief.Experience.Surfaces) == 0 {
			built.surfaces = unionSurfaces(preserved.surfaces, built.surfaces)
		}
	} else if preserved != nil {
		built.surfaces = append([]string(nil), preserved.surfaces...)
		built.primary = preserved.primary
	} else {
		built.surfaces = []string{"space"}
	}
	if len(built.surfaces) == 0 {
		built.surfaces = []string{"space"}
	}
	if built.primary == "" {
		built.primary = built.surfaces[0]
	}
	if built.primary != "" && !containsString(built.surfaces, built.primary) {
		return nil, nil, "", fmt.Errorf("%w: primary surface", ErrInvalid)
	}
	if containsString(built.surfaces, "story") {
		built.ensureStory()
	}
	nodes := map[string]*worldcap.ViewNode{}
	if containsString(built.surfaces, "live") {
		nodes["live"] = built.buildLive()
	}
	if containsString(built.surfaces, "story") {
		nodes["story"] = built.buildStory()
	}
	if containsString(built.surfaces, "workspace") {
		nodes["workspace"] = built.buildWorkspace()
	}
	if containsString(built.surfaces, "space") {
		nodes["space"] = built.buildSpace()
	}
	if containsString(built.surfaces, "scene3d") {
		nodes["scene3d"] = built.buildScene3D()
	}
	if containsString(built.surfaces, "media") {
		nodes["media"] = built.buildMedia()
	}
	if containsString(built.surfaces, "custom") {
		nodes["custom"] = built.buildCustom()
	}
	var audioNode *worldcap.ViewNode
	if brief.Experience != nil && brief.Experience.Audio != nil {
		audioNode = built.buildAudio()
	} else if preserved != nil && len(preserved.audioRaw) > 0 {
		audioNode = &worldcap.ViewNode{
			ID: "audio", Capability: "core.audio",
			Props: append(json.RawMessage(nil), preserved.audioRaw...),
		}
	}
	if built.err != nil {
		return nil, nil, "", built.err
	}
	if len(built.behaviors) > 24 {
		return nil, nil, "", fmt.Errorf("%w: too many behaviors", ErrTooLarge)
	}
	var root *worldcap.ViewNode
	if len(built.surfaces) == 1 {
		root = nodes[built.surfaces[0]]
		if root == nil {
			return nil, nil, "", fmt.Errorf("%w: missing surface", ErrInvalid)
		}
		root.ID = "root"
	} else {
		children := make([]worldcap.ViewNode, 0, len(built.surfaces))
		tabs := make([]map[string]any, 0, len(built.surfaces))
		for _, surface := range built.surfaces {
			node := nodes[surface]
			if node == nil {
				continue
			}
			node.ID = surface
			children = append(children, *node)
			tabs = append(tabs, map[string]any{
				"label": built.surfaceLabel(surface), "child_id": surface,
			})
		}
		root = &worldcap.ViewNode{
			ID: "root", Capability: "core.tabs",
			Props: mustJSON(map[string]any{
				"tabs": tabs, "default": built.primary,
			}),
			Children: children,
		}
	}
	if audioNode != nil {
		root.ID = "content"
		root = &worldcap.ViewNode{
			ID: "root", Capability: "core.stack",
			Props: mustJSON(map[string]any{
				"direction": "vertical", "gap": 10, "padding": 0,
				"align": "stretch", "tone": "plain",
			}),
			Children: []worldcap.ViewNode{*audioNode, *root},
		}
	}
	return &worldcap.Experience{
		Version: 1, Root: root, Behaviors: built.behaviors,
	}, built.surfaces, built.primary, nil
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func (built *surfaceBuilder) surfaceLabel(surface string) string {
	zh := isCJK(built.brief.Premise)
	switch surface {
	case "live":
		if built.brief.Experience != nil && built.brief.Experience.Live != nil &&
			built.brief.Experience.Live.Title != "" {
			return built.brief.Experience.Live.Title
		}
		if built.brief.Experience != nil && built.brief.Experience.Live != nil &&
			built.brief.Experience.Live.Goal != nil {
			if zh {
				return "游玩"
			}
			return "Play"
		}
		if zh {
			return "进入世界"
		}
		return "Enter"
	case "story":
		if built.brief.Experience != nil && built.brief.Experience.Story != nil &&
			built.brief.Experience.Story.Title != "" {
			return truncate(built.brief.Experience.Story.Title, 40)
		}
		if zh {
			return "阅读"
		}
		return "Read"
	case "workspace":
		if built.brief.Experience != nil && built.brief.Experience.Workspace != nil &&
			built.brief.Experience.Workspace.Title != "" {
			return truncate(built.brief.Experience.Workspace.Title, 40)
		}
		if zh {
			return "工作台"
		}
		return "Workbench"
	case "scene3d":
		if built.brief.Experience != nil && built.brief.Experience.Scene3D != nil &&
			built.brief.Experience.Scene3D.Title != "" {
			return truncate(built.brief.Experience.Scene3D.Title, 40)
		}
		if zh {
			return "三维世界"
		}
		return "3D world"
	case "media":
		if built.brief.Experience != nil && built.brief.Experience.Media != nil &&
			built.brief.Experience.Media.Title != "" {
			return truncate(built.brief.Experience.Media.Title, 40)
		}
		if zh {
			return "图像与视频"
		}
		return "Media"
	case "custom":
		if built.brief.Experience != nil && len(built.brief.Experience.Custom) > 0 {
			var program struct {
				Title string `json:"title"`
			}
			_ = json.Unmarshal(built.brief.Experience.Custom, &program)
			if program.Title != "" {
				return truncate(program.Title, 40)
			}
		}
		if zh {
			return "体验"
		}
		return "Experience"
	default:
		if zh {
			return "空间"
		}
		return "Space"
	}
}

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func (builder *surfaceBuilder) addBehavior(prefix, name string,
	actions []worldcap.Action) string {
	if builder.err != nil {
		return ""
	}
	id := fmt.Sprintf("%s%d", prefix, builder.behaviorSeq)
	for builder.behaviorIDs[id] {
		builder.behaviorSeq++
		id = fmt.Sprintf("%s%d", prefix, builder.behaviorSeq)
	}
	builder.behaviorSeq++
	builder.behaviorIDs[id] = true
	builder.behaviors = append(builder.behaviors, worldcap.Behavior{
		ID: id, Name: name, Actions: actions,
	})
	return id
}

func (builder *surfaceBuilder) ensureStory() {
	spec := (*StorySpec)(nil)
	if builder.brief.Experience != nil {
		spec = builder.brief.Experience.Story
	}
	if spec == nil || len(spec.Chapters) == 0 {
		builder.err = fmt.Errorf("%w: a story needs chapters", ErrInvalid)
		return
	}
	chapterKind := builder.chapterKind
	if chapterKind == nil && builder.preserved != nil && builder.preserved.storyKindID != "" {
		chapterKind = builder.world.findKindByID(builder.preserved.storyKindID)
	}
	if chapterKind == nil {
		name := strings.TrimSpace(spec.Kind)
		if name == "" {
			if isCJK(builder.brief.Premise) {
				name = "篇章"
			} else {
				name = "Chapter"
			}
		}
		chapterKind = builder.world.findKindByName(name)
		if chapterKind == nil {
			chapterKind = builder.world.addKind(name, 198, generatedForm("book", len(builder.world.Kinds)))
		}
	}
	builder.chapterKind = chapterKind
	existing := builder.world.nodesOfKind(chapterKind.ID)
	builder.chapterIDs = make([]string, 0, len(existing)+len(spec.Chapters))
	for _, node := range existing {
		builder.chapterIDs = append(builder.chapterIDs, node.ID)
	}
	newIDs := make([]string, len(spec.Chapters))
	for index := range spec.Chapters {
		chapter := &spec.Chapters[index]
		node := builder.world.addNode(chapterKind.ID, chapter.Title, chapter.Text, map[string]any{})
		node.X, node.Y = layoutPosition(len(builder.chapterIDs), len(builder.chapterIDs)+len(spec.Chapters), 0, "space")
		newIDs[index] = node.ID
		builder.chapterIDs = append(builder.chapterIDs, node.ID)
	}
	finalCount := len(builder.chapterIDs)
	for index := range spec.Chapters {
		chapter := &spec.Chapters[index]
		fromID := newIDs[index]
		for _, choice := range chapter.Choices {
			if choice.To < 0 || choice.To >= finalCount {
				builder.err = fmt.Errorf("%w: story choice target", ErrInvalid)
				return
			}
			builder.world.addLink(fromID, builder.chapterIDs[choice.To], choice.Label)
		}
	}
	if _, ok := builder.world.Variables["story.chapter"]; !ok {
		builder.world.Variables["story.chapter"] = float64(0)
	}
	jumpBehavior := builder.addBehavior("jump", "chapter",
		[]worldcap.Action{{
			Capability: "core.variable", Action: "set",
			Params: mustJSON(map[string]any{
				"name": "story.chapter", "value": "$event.value",
			}),
		}})
	if jumpBehavior == "" {
		return
	}
	jumps := make([]map[string]any, 0, len(builder.chapterIDs))
	for _, chapterID := range builder.chapterIDs {
		jumps = append(jumps, map[string]any{
			"entity_id": chapterID, "behavior_id": jumpBehavior,
		})
	}
	restart := builder.addBehavior("restart", "restart",
		[]worldcap.Action{{
			Capability: "core.variable", Action: "set",
			Params: mustJSON(map[string]any{"name": "story.chapter", "value": float64(0)}),
		}})
	if builder.err != nil {
		return
	}
	title := spec.Title
	if title == "" {
		title = builder.world.Premise
	}
	if builder.chapterKind != nil {
		builder.storyProps = map[string]any{
			"title": truncate(title, 120), "chapter_kind_id": builder.chapterKind.ID,
			"progress_variable": "story.chapter", "jump_behaviors": jumps,
			"restart_behavior_id": restart,
		}
	}
}

func (builder *surfaceBuilder) buildStory() *worldcap.ViewNode {
	props := builder.storyProps
	if props == nil {
		props = map[string]any{"chapter_kind_id": "", "progress_variable": "story.chapter"}
	}
	return &worldcap.ViewNode{
		ID: "story", Capability: "core.story", Props: mustJSON(props),
	}
}

func (builder *surfaceBuilder) buildLive() *worldcap.ViewNode {
	spec := (*LiveSpec)(nil)
	if builder.brief.Experience != nil {
		spec = builder.brief.Experience.Live
	}
	views, background, entityActions, solidKinds := builder.scene(spec)
	goal := (*GoalSpec)(nil)
	if spec != nil {
		goal = spec.Goal
	}
	avatarID := ""
	if spec != nil && spec.Avatar != nil {
		avatarID = builder.nodeIDForSeedThing(*spec.Avatar)
	}
	if avatarID == "" && goal != nil && len(builder.brief.Things) > 0 {
		avatarID = builder.nodeIDForSeedThing(0)
	}
	if avatarID == "" && (goal != nil || (spec != nil && spec.Avatar != nil)) {
		for _, node := range builder.world.Nodes {
			if builder.chapterKind == nil || node.KindID != builder.chapterKind.ID {
				avatarID = node.ID
				break
			}
		}
	}
	props := map[string]any{
		"background": background, "show_links": true, "link_style": "curve",
		"views": views, "entity_actions": entityActions,
		"speed": 1.8, "interact_radius": 96, "solid_kinds": solidKinds,
	}
	if avatarID == "" && builder.preserved != nil && builder.preserved.live.avatar != nil {
		props["avatar"] = cloneMap(builder.preserved.live.avatar)
		if builder.preserved.live.spawn != nil {
			props["spawn"] = cloneMap(builder.preserved.live.spawn)
		}
	}
	if spec != nil {
		if spec.Speed != nil {
			props["speed"] = *spec.Speed
		}
		if spec.Title != "" {
			props["title"] = spec.Title
		}
		if spec.Hint != "" {
			props["hint"] = spec.Hint
		}
		if spec.Timer != nil {
			props["timer"] = *spec.Timer
		}
	}
	if avatarID != "" {
		node := builder.world.findNodeByID(avatarID)
		if node != nil {
			path, hue := builder.avatarVisual(node)
			props["avatar"] = map[string]any{
				"path": path, "size": 34, "hue": hue, "opacity": 1,
			}
			props["spawn"] = map[string]any{"x": node.X, "y": node.Y}
		}
	}
	if goal != nil {
		props["goal"] = builder.goalProps(goal)
	}
	particles := builder.particles(spec)
	if particles != nil {
		props["particles"] = particles
	}
	timeline := builder.timeline(spec, avatarID != "")
	if timeline != nil {
		props["timeline"] = timeline
	}
	return &worldcap.ViewNode{
		ID: "live", Capability: "core.live", Props: mustJSON(props),
	}
}

func (builder *surfaceBuilder) buildMedia() *worldcap.ViewNode {
	spec := (*MediaSpec)(nil)
	if builder.brief.Experience != nil {
		spec = builder.brief.Experience.Media
	}
	if spec == nil && builder.preserved != nil && len(builder.preserved.mediaRaw) > 0 {
		return &worldcap.ViewNode{
			ID: "media", Capability: "core.media",
			Props: append(json.RawMessage(nil), builder.preserved.mediaRaw...),
		}
	}
	if spec == nil {
		spec = &MediaSpec{Kind: "image"}
	}
	kind := spec.Kind
	if kind != "video" {
		kind = "image"
	}
	if spec.ShowVariable != "" {
		builder.ensureAudioVariable(spec.ShowVariable, "truthy")
	}
	if spec.PlayVariable != "" {
		builder.ensureAudioVariable(spec.PlayVariable, "truthy")
	}
	return &worldcap.ViewNode{
		ID: "media", Capability: "core.media",
		Props: mustJSON(map[string]any{
			"kind": kind, "asset_id": "", "caption": spec.Caption,
			"fit": "contain", "controls": kind == "video", "loop": false,
			"muted": true, "autoplay": false,
			"play_variable": spec.PlayVariable, "show_variable": spec.ShowVariable,
		}),
	}
}

func (builder *surfaceBuilder) buildScene3D() *worldcap.ViewNode {
	spec := (*Scene3DSpec)(nil)
	if builder.brief.Experience != nil {
		spec = builder.brief.Experience.Scene3D
	}
	_, _, entityActions, _ := builder.scene(nil)
	specViews := map[int]Scene3DViewSpec{}
	if spec != nil {
		for _, view := range spec.Views {
			specViews[view.Kind] = view
		}
	}
	primitives := []string{"sphere", "box", "cylinder", "cone", "torus"}
	views := make([]map[string]any, 0, len(builder.world.Kinds))
	chapterKindID := ""
	if builder.chapterKind != nil {
		chapterKindID = builder.chapterKind.ID
	}
	kindIndex := 0
	for _, kind := range builder.world.Kinds {
		if kind.ID == chapterKindID {
			continue
		}
		primitive := primitives[kindIndex%len(primitives)]
		scale := 1.0
		elevation := 0.0
		collider := "none"
		hideDone := false
		if kind.Solid {
			collider = "box"
		}
		if view, ok := specViews[kindIndex]; ok {
			if view.Primitive != "" {
				primitive = view.Primitive
			}
			if view.Scale != nil {
				scale = *view.Scale
			}
			if view.Elevation != nil {
				elevation = *view.Elevation
			}
			if view.Collider != "" {
				collider = view.Collider
			}
			hideDone = view.HideDone
		}
		views = append(views, map[string]any{
			"kind_id": kind.ID, "primitive": primitive, "scale": scale,
			"elevation": elevation, "hue": float64(kind.Hue), "opacity": 0.95,
			"collider": collider, "label": "always", "hide_done": hideDone,
		})
		kindIndex++
	}
	playerIndex := -1
	if spec != nil && spec.Player != nil {
		playerIndex = *spec.Player
	} else {
		for index := range builder.brief.Things {
			node := builder.world.findNodeByID(builder.nodeIDForSeedThing(index))
			if node == nil {
				continue
			}
			kind := builder.world.findKindByID(node.KindID)
			if kind != nil && !kind.Solid {
				playerIndex = index
				break
			}
		}
		if playerIndex < 0 && len(builder.brief.Things) > 0 {
			playerIndex = 0
		}
	}
	playerNodeID := ""
	if playerIndex >= 0 {
		playerNodeID = builder.nodeIDForSeedThing(playerIndex)
	}
	skyHue := builder.world.Presentation.Scene.SkyHue
	groundHue := builder.world.Presentation.Scene.GroundHue
	fog := 0.02
	camera := "follow"
	distance := 14.0
	speed := 4.0
	gravity := 20.0
	title := truncate(builder.world.Premise, 120)
	if spec != nil {
		if spec.SkyHue != nil {
			skyHue = *spec.SkyHue
		}
		if spec.GroundHue != nil {
			groundHue = *spec.GroundHue
		}
		if spec.Fog != nil {
			fog = *spec.Fog
		}
		if spec.Camera != "" {
			camera = spec.Camera
		}
		if spec.Distance != nil {
			distance = *spec.Distance
		}
		if spec.Speed != nil {
			speed = *spec.Speed
		}
		if spec.Gravity != nil {
			gravity = *spec.Gravity
		}
		if spec.Title != "" {
			title = truncate(spec.Title, 120)
		}
	}
	activate := ""
	if len(entityActions) == 0 {
		activate = builder.senseBehavior()
	}
	return &worldcap.ViewNode{
		ID: "scene3d", Capability: "core.scene3d",
		Props: mustJSON(map[string]any{
			"title": title, "sky_hue": float64(skyHue), "ground_hue": float64(groundHue),
			"fog": fog, "camera_mode": camera, "camera_distance": distance,
			"camera_height": 8.0, "fov": 55.0, "gravity": gravity,
			"ambient_light": 0.8, "player_enabled": playerNodeID != "",
			"player_node_id": playerNodeID, "player_speed": speed, "player_height": 1.0,
			"views": views, "show_links": true, "activate_behavior_id": activate,
			"entity_actions": entityActions,
		}),
	}
}

// senseBehavior makes a 3D click mark the clicked thing done and increment a
// numeric audio cue variable. It is created only when the seed declares a
// numeric cue and no per-thing interaction already owns the click.
func (builder *surfaceBuilder) senseBehavior() string {
	if builder.brief.Experience == nil || builder.brief.Experience.Audio == nil {
		return ""
	}
	variable := ""
	for _, cue := range builder.brief.Experience.Audio.Cues {
		if cue.Mode == "increase" {
			variable = cue.Variable
			break
		}
	}
	if variable == "" {
		return ""
	}
	if declared, ok := builder.world.Variables[variable]; ok {
		if _, isNumber := declared.(float64); !isNumber {
			return ""
		}
	} else {
		builder.world.Variables[variable] = float64(0)
	}
	return builder.addBehavior("sense", "signal", []worldcap.Action{
		{
			Capability: "core.state", Action: "set",
			Params: mustJSON(map[string]any{
				"entity_id": "$event.entity", "field": "status", "value": "done",
			}),
		},
		{
			Capability: "core.variable", Action: "add",
			Params: mustJSON(map[string]any{"name": variable, "value": float64(1)}),
		},
	})
}

func (builder *surfaceBuilder) buildAudio() *worldcap.ViewNode {
	spec := builder.brief.Experience.Audio
	title := "Sound"
	if spec.Title != "" {
		title = truncate(spec.Title, 80)
	}
	tone := spec.Tone
	if tone == nil {
		frequency := 280.0
		atmosphere := strings.ToLower(builder.brief.Atmosphere)
		switch {
		case strings.Contains(atmosphere, "rain") || strings.Contains(atmosphere, "雨"):
			frequency = 220
		case strings.Contains(atmosphere, "star") || strings.Contains(atmosphere, "星"):
			frequency = 330
		case strings.Contains(atmosphere, "forest") || strings.Contains(atmosphere, "森林"):
			frequency = 196
		}
		tone = &ToneSpec{
			Frequency: &frequency, Wave: "sine",
			Volume: floatPointer(0.06), Pulse: floatPointer(0.18),
		}
	}
	volume := 0.35
	if spec.Volume != nil {
		volume = *spec.Volume
	}
	loop := true
	if spec.Loop != nil {
		loop = *spec.Loop
	}
	props := map[string]any{
		"title": title, "tone": toneMap(tone), "loop": loop,
		"volume": volume, "muted": false,
	}
	cues := make([]map[string]any, 0, len(spec.Cues))
	for index, cue := range spec.Cues {
		mode := cue.Mode
		if mode == "" {
			mode = "change"
		}
		builder.ensureAudioVariable(cue.Variable, mode)
		cueTone := cue.Tone
		if cueTone == nil {
			frequency := 440.0 + float64(index)*90
			cueTone = &ToneSpec{
				Frequency: &frequency, Wave: "triangle",
				Volume: floatPointer(0.12), Pulse: floatPointer(0),
			}
		}
		entry := map[string]any{
			"variable": cue.Variable, "mode": mode, "tone": toneMap(cueTone),
		}
		if cue.Volume != nil {
			entry["volume"] = *cue.Volume
		}
		cues = append(cues, entry)
	}
	if len(cues) > 0 {
		props["cues"] = cues
	}
	return &worldcap.ViewNode{ID: "audio", Capability: "core.audio", Props: mustJSON(props)}
}

func (builder *surfaceBuilder) ensureAudioVariable(name, mode string) {
	want := "text"
	switch mode {
	case "truthy", "falsy":
		want = "flag"
	case "increase", "decrease":
		want = "number"
	}
	existing, ok := builder.world.Variables[name]
	if !ok {
		switch want {
		case "flag":
			builder.world.Variables[name] = false
		case "number":
			builder.world.Variables[name] = float64(0)
		default:
			builder.world.Variables[name] = ""
		}
		return
	}
	if builder.err != nil {
		return
	}
	switch want {
	case "flag":
		if _, good := existing.(bool); !good {
			builder.err = fmt.Errorf("%w: audio cue variable type", ErrInvalid)
		}
	case "number":
		if _, good := existing.(float64); !good {
			builder.err = fmt.Errorf("%w: audio cue variable type", ErrInvalid)
		}
	}
}

func toneMap(tone *ToneSpec) map[string]any {
	entry := map[string]any{"wave": "sine", "volume": 0.08, "pulse": 0.0}
	if tone.Wave != "" {
		entry["wave"] = tone.Wave
	}
	if tone.Frequency != nil {
		entry["frequency"] = *tone.Frequency
	} else {
		entry["frequency"] = 280.0
	}
	if tone.Volume != nil {
		entry["volume"] = *tone.Volume
	}
	if tone.Pulse != nil {
		entry["pulse"] = *tone.Pulse
	}
	return entry
}

func floatPointer(value float64) *float64 { return &value }

func (builder *surfaceBuilder) buildSpace() *worldcap.ViewNode {
	views, background, entityActions, _ := builder.scene(nil)
	return &worldcap.ViewNode{
		ID: "space", Capability: "core.space",
		Props: mustJSON(map[string]any{
			"background": background, "show_links": true, "link_style": "curve",
			"views": views, "entity_actions": entityActions,
		}),
	}
}

func (builder *surfaceBuilder) scene(spec *LiveSpec) ([]map[string]any,
	map[string]any, []map[string]any, []string) {
	preserved := builder.preserved
	views := map[string]map[string]any{}
	if preserved != nil {
		for kindID, view := range preserved.live.views {
			views[kindID] = cloneMap(view)
		}
	}
	chapterKindID := ""
	if builder.chapterKind != nil {
		chapterKindID = builder.chapterKind.ID
	}
	viewList := make([]map[string]any, 0, len(builder.world.Kinds))
	for index, kind := range builder.world.Kinds {
		if kind.ID == chapterKindID {
			continue
		}
		view, ok := views[kind.ID]
		if !ok {
			view = map[string]any{
				"kind_id": kind.ID, "path": kind.Form,
				"size": float64(46 + index*6), "fill_hue": kind.Hue,
				"opacity": 0.92, "paint": "fill", "label": "always",
				"layer": "mid", "motion": classicMotion(kind.Motion),
			}
			if bindings := experienceBindingsFromKind(kind); len(bindings) > 0 {
				view["bindings"] = bindings
			}
		}
		view["kind_id"] = kind.ID
		viewList = append(viewList, view)
	}
	background := map[string]any{
		"mode": "gradient", "hue": builder.world.Presentation.Scene.SkyHue,
		"hue2": builder.world.Presentation.Scene.GroundHue, "lightness": 10,
		"accent_hue": builder.world.Presentation.Scene.AccentHue,
	}
	if preserved != nil && preserved.live.background != nil {
		background = cloneMap(preserved.live.background)
	}
	entityActions := make([]map[string]any, 0, 40)
	actionByEntity := map[string]bool{}
	if preserved != nil {
		for _, action := range preserved.live.entityActions {
			entityID, _ := action["entity_id"].(string)
			if entityID == "" || actionByEntity[entityID] ||
				builder.world.findNodeByID(entityID) == nil {
				continue
			}
			behaviorID, _ := action["behavior_id"].(string)
			if behaviorID != "" && !builder.behaviorIDs[behaviorID] {
				continue
			}
			entityActions = append(entityActions, cloneMap(action))
			actionByEntity[entityID] = true
		}
	}
	solidKinds := make([]string, 0, len(builder.world.Kinds))
	solidSeen := map[string]bool{}
	if preserved != nil {
		for _, kindID := range preserved.live.solidKinds {
			if builder.world.findKindByID(kindID) != nil && !solidSeen[kindID] {
				solidKinds = append(solidKinds, kindID)
				solidSeen[kindID] = true
			}
		}
	}
	for _, kind := range builder.world.Kinds {
		if kind.Solid && !solidSeen[kind.ID] {
			solidKinds = append(solidKinds, kind.ID)
			solidSeen[kind.ID] = true
		}
	}
	// Seed interactions become fixed-target behaviors. The first interaction
	// of each thing becomes its click/approach action.
	for index := range builder.brief.Interactions {
		interaction := &builder.brief.Interactions[index]
		fromID := builder.nodeIDForSeedThing(interaction.From)
		targetIndex := interaction.From
		if interaction.To != nil {
			targetIndex = *interaction.To
		}
		targetID := builder.nodeIDForSeedThing(targetIndex)
		if fromID == "" || targetID == "" {
			continue
		}
		fieldKey := builder.world.fieldKey(builder.world.findNodeByID(targetID).KindID, interaction.Field)
		if fieldKey == "" {
			continue
		}
		params := map[string]any{"entity_id": targetID, "field": fieldKey}
		if interaction.Op != objects.OpenWorldOpToggle {
			params["value"] = interaction.Value
		}
		behaviorID := builder.addBehavior("act", interaction.Verb,
			[]worldcap.Action{{
				Capability: "core.state", Action: interaction.Op, Params: mustJSON(params),
			}})
		if behaviorID == "" {
			continue
		}
		if !actionByEntity[fromID] {
			entityActions = append(entityActions, map[string]any{
				"entity_id": fromID, "behavior_id": behaviorID, "label": interaction.Verb,
			})
			actionByEntity[fromID] = true
		}
	}
	// Game goals need a reachable action: collect marks things done; score adds
	// to the score variable or numeric field.
	if spec != nil && spec.Goal != nil {
		builder.attachGoalActions(spec.Goal, &entityActions, actionByEntity, views, viewList)
	}
	return viewList, background, entityActions, solidKinds
}

func (builder *surfaceBuilder) attachGoalActions(goal *GoalSpec, entityActions *[]map[string]any,
	actionByEntity map[string]bool, views map[string]map[string]any, viewList []map[string]any) {
	_ = views
	switch goal.Type {
	case "collect":
		if goal.Kind == nil || *goal.Kind >= len(builder.brief.Kinds) {
			return
		}
		kind := builder.world.findKindByName(builder.brief.Kinds[*goal.Kind].Name)
		if kind == nil {
			return
		}
		behaviorID := builder.addBehavior("collect", "collect",
			[]worldcap.Action{{
				Capability: "core.state", Action: "set",
				Params: mustJSON(map[string]any{
					"entity_id": "$event.entity", "field": "status", "value": "done",
				}),
			}})
		if behaviorID == "" {
			return
		}
		for _, node := range builder.world.nodesOfKind(kind.ID) {
			*entityActions = append(*entityActions, map[string]any{
				"entity_id": node.ID, "behavior_id": behaviorID, "label": "collect",
			})
			actionByEntity[node.ID] = true
		}
		for index := range viewList {
			if viewList[index]["kind_id"] == kind.ID {
				viewList[index]["hide_done"] = true
			}
		}
	case "score":
		params := map[string]any{}
		action := "add"
		if goal.Variable != "" {
			params["name"] = goal.Variable
			params["value"] = float64(1)
			if _, ok := builder.world.Variables[goal.Variable]; !ok {
				builder.world.Variables[goal.Variable] = float64(0)
			}
			behaviorID := builder.addBehavior("score", "score",
				[]worldcap.Action{{
					Capability: "core.variable", Action: action, Params: mustJSON(params),
				}})
			if behaviorID == "" {
				return
			}
			for _, node := range builder.world.Nodes {
				if actionByEntity[node.ID] {
					continue
				}
				*entityActions = append(*entityActions, map[string]any{
					"entity_id": node.ID, "behavior_id": behaviorID, "label": "score",
				})
				actionByEntity[node.ID] = true
			}
			return
		}
		if goal.Entity == nil {
			return
		}
		targetID := builder.nodeIDForSeedThing(*goal.Entity)
		target := builder.world.findNodeByID(targetID)
		if target == nil {
			return
		}
		fieldKey := builder.world.fieldKey(target.KindID, goal.Field)
		if fieldKey == "" {
			return
		}
		behaviorID := builder.addBehavior("score", "score",
			[]worldcap.Action{{
				Capability: "core.state", Action: "add",
				Params: mustJSON(map[string]any{
					"entity_id": targetID, "field": fieldKey, "value": float64(1),
				}),
			}})
		if behaviorID == "" {
			return
		}
		if !actionByEntity[targetID] {
			*entityActions = append(*entityActions, map[string]any{
				"entity_id": targetID, "behavior_id": behaviorID, "label": "score",
			})
			actionByEntity[targetID] = true
		}
	}
}

func (builder *surfaceBuilder) goalProps(goal *GoalSpec) map[string]any {
	props := map[string]any{"type": goal.Type, "label": goal.Label}
	switch goal.Type {
	case "collect":
		if goal.Kind != nil && *goal.Kind < len(builder.brief.Kinds) {
			kind := builder.world.findKindByName(builder.brief.Kinds[*goal.Kind].Name)
			if kind != nil {
				props["kind_id"] = kind.ID
			}
		}
		props["count"] = float64(*goal.Count)
	case "score":
		if goal.Variable != "" {
			props["variable"] = goal.Variable
		} else if goal.Entity != nil {
			targetID := builder.nodeIDForSeedThing(*goal.Entity)
			props["entity_id"] = targetID
			target := builder.world.findNodeByID(targetID)
			if target != nil {
				props["field"] = builder.world.fieldKey(target.KindID, goal.Field)
			}
		}
		props["target"] = *goal.Target
	case "reach":
		if goal.Entity != nil {
			props["entity_id"] = builder.nodeIDForSeedThing(*goal.Entity)
		}
	}
	return props
}

func (builder *surfaceBuilder) avatarVisual(node *authoredNode) (string, int) {
	kind := builder.world.findKindByID(node.KindID)
	if kind != nil {
		return kind.Form, kind.Hue
	}
	return generatedAvatar(node.Name), 198
}

func (builder *surfaceBuilder) particles(spec *LiveSpec) map[string]any {
	if spec != nil && spec.Particles != nil {
		particles := map[string]any{"kind": spec.Particles.Kind, "count": float64(40),
			"hue": float64(200), "size": float64(2), "speed": float64(1)}
		if spec.Particles.Count != nil {
			particles["count"] = float64(*spec.Particles.Count)
		}
		if spec.Particles.Hue != nil {
			particles["hue"] = float64(*spec.Particles.Hue)
		}
		if spec.Particles.Size != nil {
			particles["size"] = *spec.Particles.Size
		}
		if spec.Particles.Speed != nil {
			particles["speed"] = *spec.Particles.Speed
		}
		return particles
	}
	atmosphere := strings.ToLower(builder.brief.Atmosphere)
	switch {
	case strings.Contains(atmosphere, "雨") || strings.Contains(atmosphere, "rain"):
		return map[string]any{"kind": "rain", "count": float64(60), "hue": float64(210), "size": float64(2), "speed": float64(1.4)}
	case strings.Contains(atmosphere, "雪") || strings.Contains(atmosphere, "snow"):
		return map[string]any{"kind": "snow", "count": float64(50), "hue": float64(200), "size": float64(2), "speed": float64(0.6)}
	case strings.Contains(atmosphere, "星") || strings.Contains(atmosphere, "star"):
		return map[string]any{"kind": "stars", "count": float64(60), "hue": float64(45), "size": float64(2), "speed": float64(0.4)}
	}
	if spec == nil {
		return map[string]any{"kind": "motes", "count": float64(40), "hue": float64(200), "size": float64(2), "speed": float64(0.5)}
	}
	return nil
}

func (builder *surfaceBuilder) timeline(spec *LiveSpec, hasAvatar bool) map[string]any {
	if spec != nil && spec.Timeline != nil {
		duration := 12.0
		if spec.Timeline.Duration != nil {
			duration = *spec.Timeline.Duration
		}
		loop := spec.Timeline.Loop
		autoplay := true
		if spec.Timeline.Autoplay != nil {
			autoplay = *spec.Timeline.Autoplay
		}
		tracks := make([]map[string]any, 0, len(spec.Timeline.Tracks))
		for _, track := range spec.Timeline.Tracks {
			entityID := builder.nodeIDForSeedThing(track.Entity)
			if entityID == "" {
				continue
			}
			keys := make([]map[string]any, 0, len(track.Keys))
			for _, key := range track.Keys {
				t := 0.0
				if key.T != nil {
					t = *key.T
				}
				entry := map[string]any{"t": t, "v": key.V}
				if key.Easing != "" {
					entry["easing"] = key.Easing
				}
				keys = append(keys, entry)
			}
			tracks = append(tracks, map[string]any{
				"entity_id": entityID, "property": track.Property, "keys": keys,
			})
		}
		return map[string]any{
			"duration": duration, "loop": loop, "autoplay": autoplay, "tracks": tracks,
		}
	}
	if hasAvatar {
		return nil
	}
	// Animation mode: derive a real, deterministic timeline from each kind's
	// declared motion, so the world visibly moves instead of being a still
	// picture with a play button.
	tracks := make([]map[string]any, 0, 16)
	chapterKindID := ""
	if builder.chapterKind != nil {
		chapterKindID = builder.chapterKind.ID
	}
	for _, node := range builder.world.Nodes {
		if len(tracks) >= 16 {
			break
		}
		if node.KindID == chapterKindID {
			continue
		}
		kind := builder.world.findKindByID(node.KindID)
		if kind == nil {
			continue
		}
		motion := classicMotion(kind.Motion)
		switch motion {
		case "float":
			tracks = append(tracks, map[string]any{
				"entity_id": node.ID, "property": "y",
				"keys": []map[string]any{
					{"t": 0, "v": node.Y}, {"t": 6, "v": clampFloat(node.Y-24, 20, 980)},
					{"t": 12, "v": node.Y},
				},
			})
		case "pulse", "breathe":
			low, high := 1.0, 1.12
			if motion == "breathe" {
				low, high = 0.94, 1.06
			}
			tracks = append(tracks, map[string]any{
				"entity_id": node.ID, "property": "scale",
				"keys": []map[string]any{
					{"t": 0, "v": low}, {"t": 6, "v": high}, {"t": 12, "v": low},
				},
			})
		case "sway":
			tracks = append(tracks, map[string]any{
				"entity_id": node.ID, "property": "x",
				"keys": []map[string]any{
					{"t": 0, "v": node.X}, {"t": 6, "v": clampFloat(node.X+18, 20, 1580)},
					{"t": 12, "v": node.X},
				},
			})
		case "orbit":
			tracks = append(tracks, map[string]any{
				"entity_id": node.ID, "property": "rotate",
				"keys": []map[string]any{
					{"t": 0, "v": 0}, {"t": 12, "v": 360},
				},
			})
		case "flicker":
			tracks = append(tracks, map[string]any{
				"entity_id": node.ID, "property": "opacity",
				"keys": []map[string]any{
					{"t": 0, "v": 1}, {"t": 4, "v": 0.5}, {"t": 8, "v": 0.9},
					{"t": 12, "v": 1},
				},
			})
		}
	}
	if len(tracks) == 0 {
		for _, node := range builder.world.Nodes {
			if len(tracks) >= 8 {
				break
			}
			tracks = append(tracks, map[string]any{
				"entity_id": node.ID, "property": "opacity",
				"keys": []map[string]any{
					{"t": 0, "v": 0.8}, {"t": 6, "v": 1}, {"t": 12, "v": 0.8},
				},
			})
		}
	}
	return map[string]any{
		"duration": 12.0, "loop": true, "autoplay": true, "tracks": tracks,
	}
}

func (builder *surfaceBuilder) buildWorkspace() *worldcap.ViewNode {
	spec := (*WorkspaceSpec)(nil)
	if builder.brief.Experience != nil {
		spec = builder.brief.Experience.Workspace
	}
	kind := builder.workspaceKind(spec)
	if kind == nil {
		builder.err = fmt.Errorf("%w: a workspace needs a kind with fields", ErrInvalid)
		return &worldcap.ViewNode{ID: "workspace", Capability: "core.workspace",
			Props: mustJSON(map[string]any{"kind_id": "", "columns": []any{}})}
	}
	columns := make([]map[string]any, 0, 8)
	seenFields := map[string]bool{}
	appendColumn := func(label string) {
		key := builder.world.fieldKey(kind.ID, label)
		if key == "" || seenFields[key] || len(columns) >= 8 {
			return
		}
		fieldType := builder.world.fieldType(kind.ID, key)
		columns = append(columns, map[string]any{
			"field": key, "label": label, "type": fieldType,
		})
		seenFields[key] = true
	}
	if spec != nil {
		for _, label := range spec.Columns {
			appendColumn(label)
		}
	}
	if len(columns) == 0 {
		for index, field := range kind.Fields {
			if index >= 3 {
				break
			}
			appendColumn(field.Label)
		}
	}
	if !seenFields["status"] {
		zh := isCJK(builder.brief.Premise)
		label := "Status"
		if zh {
			label = "状态"
		}
		columns = append(columns, map[string]any{
			"field": "status", "label": label, "type": "status",
		})
		seenFields["status"] = true
	}
	actions, actionProps := builder.workspaceActions(spec, kind)
	title := ""
	if spec != nil {
		title = spec.Title
	}
	if title == "" {
		title = kind.Name
	}
	summary := []map[string]any{}
	zh := isCJK(builder.brief.Premise)
	countLabel, doneLabel, sumLabel := "Rows", "Done", "Total"
	if zh {
		countLabel, doneLabel, sumLabel = "条目", "完成", "合计"
	}
	summary = append(summary, map[string]any{"type": "count", "label": countLabel})
	summary = append(summary, map[string]any{"type": "done", "label": doneLabel})
	for _, field := range kind.Fields {
		if field.Type == objects.OpenWorldFieldNumber {
			summary = append(summary, map[string]any{
				"type": "sum", "field": field.Key, "label": field.Label + " " + sumLabel,
			})
			break
		}
	}
	props := map[string]any{
		"title": title, "kind_id": kind.ID, "columns": columns,
		"actions": actionProps, "summary": summary,
	}
	_ = actions
	return &worldcap.ViewNode{
		ID: "workspace", Capability: "core.workspace", Props: mustJSON(props),
	}
}

func (builder *surfaceBuilder) workspaceKind(spec *WorkspaceSpec) *authoredKind {
	if spec != nil && spec.Kind != nil && *spec.Kind < len(builder.brief.Kinds) {
		return builder.world.findKindByName(builder.brief.Kinds[*spec.Kind].Name)
	}
	for _, kind := range builder.world.Kinds {
		if len(kind.Fields) > 0 {
			return kind
		}
	}
	return nil
}

func (builder *surfaceBuilder) workspaceActions(spec *WorkspaceSpec,
	kind *authoredKind) ([]worldcap.Behavior, []map[string]any) {
	zh := isCJK(builder.brief.Premise)
	addLabel, doneLabel, progressLabel, rowName := "Add row", "Done", "Progress +1", "New item"
	if zh {
		addLabel, doneLabel, progressLabel, rowName = "新增一行", "完成", "进展 +1", "新条目"
	}
	actions := make([]worldcap.Behavior, 0, 3)
	props := make([]map[string]any, 0, 3)
	appendAction := func(label, behaviorID, scope string) {
		props = append(props, map[string]any{
			"label": label, "behavior_id": behaviorID, "scope": scope,
		})
	}
	createID := builder.addBehavior("ws", addLabel, []worldcap.Action{{
		Capability: "core.create", Action: "create",
		Params: mustJSON(map[string]any{"kind_id": kind.ID, "name": rowName}),
	}})
	if createID != "" {
		appendAction(addLabel, createID, "global")
	}
	if spec != nil && len(spec.Actions) > 0 {
		for _, action := range spec.Actions {
			switch action.Type {
			case "create":
				continue
			case "done":
				id := builder.addBehavior("ws", action.Label, []worldcap.Action{{
					Capability: "core.state", Action: "toggle",
					Params: mustJSON(map[string]any{
						"entity_id": "$event.entity", "field": "status",
					}),
				}})
				if id != "" {
					appendAction(action.Label, id, "row")
				}
			case "progress":
				fieldKey := builder.world.fieldKey(kind.ID, action.Field)
				if fieldKey == "" || builder.world.fieldType(kind.ID, fieldKey) != objects.OpenWorldFieldNumber {
					continue
				}
				id := builder.addBehavior("ws", action.Label, []worldcap.Action{{
					Capability: "core.state", Action: "add",
					Params: mustJSON(map[string]any{
						"entity_id": "$event.entity", "field": fieldKey, "value": float64(1),
					}),
				}})
				if id != "" {
					appendAction(action.Label, id, "row")
				}
			}
		}
		return actions, props
	}
	doneID := builder.addBehavior("ws", doneLabel, []worldcap.Action{{
		Capability: "core.state", Action: "toggle",
		Params: mustJSON(map[string]any{
			"entity_id": "$event.entity", "field": "status",
		}),
	}})
	if doneID != "" {
		appendAction(doneLabel, doneID, "row")
	}
	for _, field := range kind.Fields {
		if field.Type != objects.OpenWorldFieldNumber {
			continue
		}
		id := builder.addBehavior("ws", progressLabel, []worldcap.Action{{
			Capability: "core.state", Action: "add",
			Params: mustJSON(map[string]any{
				"entity_id": "$event.entity", "field": field.Key, "value": float64(1),
			}),
		}})
		if id != "" {
			appendAction(field.Label+" +1", id, "row")
		}
		break
	}
	return actions, props
}

func (builder *surfaceBuilder) nodeIDForSeedThing(index int) string {
	if index < 0 || index >= len(builder.brief.Things) {
		return ""
	}
	seed := builder.brief.Things[index]
	kind := builder.world.findKindByName(builder.brief.Kinds[seed.Kind].Name)
	if kind == nil {
		return ""
	}
	node := builder.world.findNodeByName(kind.ID, seed.Name)
	if node == nil {
		return ""
	}
	return node.ID
}

func experienceBindingsFromKind(kind *authoredKind) []map[string]any {
	bindings := make([]map[string]any, 0, 4)
	for _, field := range kind.Fields {
		if len(bindings) >= 4 {
			break
		}
		switch field.Type {
		case objects.OpenWorldFieldNumber:
			target := "size"
			outMin, outMax := 0.65, 1.65
			if len(bindings) == 1 {
				target = "opacity"
				outMin, outMax = 0.35, 1
			}
			bindings = append(bindings, map[string]any{
				"field": field.Key, "target": target,
				"in_min": 0, "in_max": 10, "out_min": outMin, "out_max": outMax,
			})
		case objects.OpenWorldFieldFlag:
			bindings = append(bindings, map[string]any{
				"field": field.Key, "target": "opacity",
				"in_min": 0, "in_max": 1, "out_min": 0.45, "out_max": 1,
			})
		}
	}
	return bindings
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func clamp(value, min, max float64) float64 {
	if math.IsNaN(value) {
		return min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func unionSurfaces(left, right []string) []string {
	seen := map[string]bool{}
	union := make([]string, 0, len(left)+len(right))
	for _, item := range append(append([]string(nil), left...), right...) {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		union = append(union, item)
	}
	return union
}

func (builder *surfaceBuilder) buildCustom() *worldcap.ViewNode {
	if builder.brief.Experience == nil || len(builder.brief.Experience.Custom) == 0 {
		builder.err = fmt.Errorf("%w: missing custom program", ErrInvalid)
		return &worldcap.ViewNode{ID: "custom", Capability: "core.custom", Props: mustJSON(map[string]any{})}
	}
	raw := builder.brief.Experience.Custom
	if builder.preserved != nil && len(builder.preserved.customRaw) > 0 {
		merged, err := mergeCustomPrograms(builder.preserved.customRaw, raw)
		if err != nil {
			builder.err = err
			return &worldcap.ViewNode{ID: "custom", Capability: "core.custom", Props: mustJSON(map[string]any{})}
		}
		raw = merged
	}
	var program struct {
		Vars map[string]any `json:"vars"`
	}
	_ = json.Unmarshal(raw, &program)
	for name, value := range program.Vars {
		if _, exists := builder.world.Variables[name]; !exists {
			builder.world.Variables[name] = value
		}
	}
	ctx := worldcap.ValidateContext{World: &worldcap.Context{
		Kinds: map[string]worldcap.KindContext{}, Entities: map[string]worldcap.EntityContext{},
		Behaviors: builder.behaviorIDs, Variables: variableTypes(builder.world.Variables),
	}}
	normalized, err := worldcap.ValidateCustom(raw, ctx)
	if err != nil {
		builder.err = fmt.Errorf("%w: custom program: %v", ErrInvalid, err)
		return &worldcap.ViewNode{ID: "custom", Capability: "core.custom", Props: mustJSON(map[string]any{})}
	}
	return &worldcap.ViewNode{ID: "custom", Capability: "core.custom", Props: normalized}
}

func variableTypes(variables map[string]any) map[string]string {
	types := make(map[string]string, len(variables))
	for name, value := range variables {
		switch value.(type) {
		case string:
			types[name] = "text"
		case float64:
			types[name] = "number"
		case bool:
			types[name] = "flag"
		}
	}
	return types
}

// mergeCustomPrograms appends the seed's nodes and keys to an existing custom
// surface. Nodes with the same id replace the old definition; variables are
// merged. This is how a complex custom form is grown across turns instead of
// being frozen by one seed.
func mergeCustomPrograms(existing, incoming json.RawMessage) (json.RawMessage, error) {
	var left map[string]any
	var right map[string]any
	if err := json.Unmarshal(existing, &left); err != nil {
		return nil, fmt.Errorf("%w: existing custom program", ErrInvalid)
	}
	if err := json.Unmarshal(incoming, &right); err != nil {
		return nil, fmt.Errorf("%w: incoming custom program", ErrInvalid)
	}
	if _, ok := right["nodes"]; ok {
		nodes := asAnySlice(left["nodes"])
		byID := map[string]int{}
		for index, raw := range nodes {
			if node, ok := raw.(map[string]any); ok {
				if id, ok := node["id"].(string); ok {
					byID[id] = index
				}
			}
		}
		for _, raw := range asAnySlice(right["nodes"]) {
			node, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := node["id"].(string)
			if index, exists := byID[id]; exists && id != "" {
				nodes[index] = node
				continue
			}
			if len(nodes) >= 40 {
				return nil, fmt.Errorf("%w: custom nodes", ErrTooLarge)
			}
			if id != "" {
				byID[id] = len(nodes)
			}
			nodes = append(nodes, node)
		}
		left["nodes"] = nodes
	}
	if _, ok := right["keys"]; ok {
		keys := asAnySlice(left["keys"])
		for _, raw := range asAnySlice(right["keys"]) {
			if len(keys) >= 16 {
				return nil, fmt.Errorf("%w: custom keys", ErrTooLarge)
			}
			keys = append(keys, raw)
		}
		left["keys"] = keys
	}
	if vars, ok := right["vars"].(map[string]any); ok {
		existingVars, _ := left["vars"].(map[string]any)
		if existingVars == nil {
			existingVars = map[string]any{}
		}
		for name, value := range vars {
			existingVars[name] = value
		}
		left["vars"] = existingVars
	}
	for _, field := range []string{"title", "hint", "background", "timeline", "width", "height"} {
		if value, ok := right[field]; ok {
			left[field] = value
		}
	}
	encoded, err := json.Marshal(left)
	if err != nil {
		return nil, fmt.Errorf("%w: custom program", ErrInvalid)
	}
	return encoded, nil
}

func asAnySlice(value any) []any {
	items, _ := value.([]any)
	return items
}
