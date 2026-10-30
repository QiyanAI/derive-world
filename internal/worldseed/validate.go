// SPDX-License-Identifier: Apache-2.0
package worldseed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/worldcap"
)

const (
	maxBriefBytes       = 8 * 1024
	maxSeedKinds        = 8
	maxSeedThings       = 40
	maxSeedRelations    = 80
	maxSeedInteractions = 24
	maxSeedFields       = 8
	maxPremiseRunes     = 1200
	maxAtmosphereRunes  = 300
	maxEssenceRunes     = 240
	maxKindNameRunes    = 60
	maxThingNameRunes   = 120
	maxThingTextRunes   = 1200
	maxFieldLabelRunes  = 40
	maxRelationRunes    = 80
	maxVerbRunes        = 80
	maxHintRunes        = 200
	maxFormRunes        = 2000
	maxSeedStringBytes  = 8 * 1024
	composeBudgetBytes  = 12 * 1024
	reshapeBudgetBytes  = 56 * 1024
	minWalkSpeed        = 0.5
	maxWalkSpeed        = 4
)

func decodeStrict(raw json.RawMessage, destination any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return fmt.Errorf("%w: empty document", ErrInvalid)
	}
	if len(trimmed) > maxSeedStringBytes {
		return fmt.Errorf("%w: document too large", ErrTooLarge)
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%w: trailing data", ErrInvalid)
	}
	return nil
}

func runeLength(value string) int { return utf8.RuneCountInString(value) }

func boundedText(value string, max int, path string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if runeLength(trimmed) > max {
		return "", fmt.Errorf("%w: %s", ErrInvalid, path)
	}
	return trimmed, nil
}

func requiredText(value string, max int, path string) (string, error) {
	trimmed, err := boundedText(value, max, path)
	if err != nil {
		return "", err
	}
	if trimmed == "" {
		return "", fmt.Errorf("%w: %s", ErrInvalid, path)
	}
	return trimmed, nil
}

func validMotion(value string) bool {
	switch value {
	case "none", "pulse", "float", "sway", "flicker", "breathe", "orbit":
		return true
	default:
		return false
	}
}

func validSurface(value string) bool {
	switch value {
	case "live", "space", "story", "workspace", "custom", "scene3d", "media":
		return true
	default:
		return false
	}
}

// ValidSurface is the public form of the surface vocabulary. Personal AI uses
// it to explain a rejected brief without importing the compiler internals.
func ValidSurface(value string) bool { return validSurface(value) }

// surfaceDiagnostic keeps a rejected value short and printable so the server
// log names what failed without storing model text verbatim.
func surfaceDiagnostic(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.ReplaceAll(trimmed, "\n", " ")
	trimmed = strings.ReplaceAll(trimmed, "\r", " ")
	if runeLength(trimmed) > 40 {
		trimmed = string([]rune(trimmed)[:40]) + "…"
	}
	return "(" + trimmed + ")"
}

func surfacesOf(experience *Experience) []string {
	if experience == nil {
		return []string{"space"}
	}
	if len(experience.Surfaces) > 0 {
		return append([]string(nil), experience.Surfaces...)
	}
	if experience.Primary != "" {
		return []string{experience.Primary}
	}
	surfaces := make([]string, 0, 3)
	if experience.Live != nil {
		surfaces = append(surfaces, "live")
	}
	if experience.Story != nil {
		surfaces = append(surfaces, "story")
	}
	if experience.Workspace != nil {
		surfaces = append(surfaces, "workspace")
	}
	if experience.Scene3D != nil {
		surfaces = append(surfaces, "scene3d")
	}
	if experience.Media != nil {
		surfaces = append(surfaces, "media")
	}
	if len(experience.Custom) > 0 {
		surfaces = append(surfaces, "custom")
	}
	if len(surfaces) == 0 {
		surfaces = append(surfaces, "space")
	}
	return surfaces
}

func validateExperienceSpec(brief *Brief) error {
	experience := brief.Experience
	surfaces := surfacesOf(experience)
	if len(surfaces) > 7 {
		return fmt.Errorf("%w: experience/surfaces", ErrInvalid)
	}
	seen := map[string]bool{}
	for index, surface := range surfaces {
		if !validSurface(surface) {
			return fmt.Errorf("%w: experience/surfaces/%d %s", ErrInvalid, index, surfaceDiagnostic(surface))
		}
		if seen[surface] {
			return fmt.Errorf("%w: experience/surfaces/%d %s (duplicate)", ErrInvalid, index, surfaceDiagnostic(surface))
		}
		seen[surface] = true
	}
	if experience.Primary != "" {
		if !validSurface(experience.Primary) || !seen[experience.Primary] {
			return fmt.Errorf("%w: experience/primary", ErrInvalid)
		}
	}
	if experience.Live != nil {
		var err error
		if experience.Live.Title, err = boundedText(experience.Live.Title, 80, "experience/live/title"); err != nil {
			return err
		}
		if experience.Live.Hint, err = boundedText(experience.Live.Hint, maxHintRunes, "experience/live/hint"); err != nil {
			return err
		}
		if experience.Live.Avatar != nil && len(brief.Things) > 0 &&
			(*experience.Live.Avatar < 0 || *experience.Live.Avatar >= len(brief.Things)) {
			return fmt.Errorf("%w: experience/live/avatar", ErrInvalid)
		}
		if experience.Live.Speed != nil &&
			(!finite(*experience.Live.Speed) || *experience.Live.Speed < minWalkSpeed || *experience.Live.Speed > maxWalkSpeed) {
			return fmt.Errorf("%w: experience/live/speed", ErrInvalid)
		}
		if experience.Live.Timer != nil &&
			(!finite(*experience.Live.Timer) || *experience.Live.Timer < 0 || *experience.Live.Timer > 3600) {
			return fmt.Errorf("%w: experience/live/timer", ErrInvalid)
		}
		if goal := experience.Live.Goal; goal != nil {
			if err := validateGoalSpec(goal, brief, experience.Live.Timer); err != nil {
				return err
			}
		}
		if particles := experience.Live.Particles; particles != nil {
			if err := validateParticleSpec(particles); err != nil {
				return err
			}
		}
		if timeline := experience.Live.Timeline; timeline != nil {
			if err := validateTimelineSpec(timeline, brief); err != nil {
				return err
			}
		}
	}
	if experience.Story != nil {
		var err error
		if experience.Story.Title, err = boundedText(experience.Story.Title, 120, "experience/story/title"); err != nil {
			return err
		}
		if experience.Story.Kind, err = boundedText(experience.Story.Kind, 60, "experience/story/kind"); err != nil {
			return err
		}
		if len(experience.Story.Chapters) < 1 || len(experience.Story.Chapters) > 40 {
			return fmt.Errorf("%w: experience/story/chapters", ErrInvalid)
		}
		for index := range experience.Story.Chapters {
			chapter := &experience.Story.Chapters[index]
			if chapter.Title, err = requiredText(chapter.Title, 120,
				fmt.Sprintf("experience/story/chapters/%d/title", index)); err != nil {
				return err
			}
			if chapter.Text, err = boundedText(chapter.Text, maxThingTextRunes,
				fmt.Sprintf("experience/story/chapters/%d/text", index)); err != nil {
				return err
			}
			if len(chapter.Choices) > 8 {
				return fmt.Errorf("%w: experience/story/chapters/%d/choices", ErrInvalid, index)
			}
			for choiceIndex := range chapter.Choices {
				choice := &chapter.Choices[choiceIndex]
				if choice.Label, err = requiredText(choice.Label, maxVerbRunes,
					fmt.Sprintf("experience/story/chapters/%d/choices/%d/label", index, choiceIndex)); err != nil {
					return err
				}
				if choice.To < 0 || choice.To >= len(experience.Story.Chapters) {
					return fmt.Errorf("%w: experience/story/chapters/%d/choices/%d/to", ErrInvalid, index, choiceIndex)
				}
			}
		}
	}
	if experience.Workspace != nil {
		var err error
		if experience.Workspace.Title, err = boundedText(experience.Workspace.Title, 120,
			"experience/workspace/title"); err != nil {
			return err
		}
		if len(brief.Kinds) == 0 {
			return fmt.Errorf("%w: experience/workspace requires kinds", ErrInvalid)
		}
		kindIndex := 0
		if experience.Workspace.Kind != nil {
			kindIndex = *experience.Workspace.Kind
		}
		if kindIndex < 0 || kindIndex >= len(brief.Kinds) {
			return fmt.Errorf("%w: experience/workspace/kind", ErrInvalid)
		}
		kind := brief.Kinds[kindIndex]
		if len(experience.Workspace.Columns) > 8 {
			return fmt.Errorf("%w: experience/workspace/columns", ErrInvalid)
		}
		for columnIndex, label := range experience.Workspace.Columns {
			if !kindHasFieldLabel(kind, label) {
				return fmt.Errorf("%w: experience/workspace/columns/%d", ErrInvalid, columnIndex)
			}
		}
		if len(experience.Workspace.Actions) > 8 {
			return fmt.Errorf("%w: experience/workspace/actions", ErrInvalid)
		}
		for actionIndex := range experience.Workspace.Actions {
			action := &experience.Workspace.Actions[actionIndex]
			if action.Label, err = requiredText(action.Label, maxVerbRunes,
				fmt.Sprintf("experience/workspace/actions/%d/label", actionIndex)); err != nil {
				return err
			}
			switch action.Type {
			case "create", "done":
				action.Field = ""
			case "progress":
				if action.Field == "" {
					for _, field := range kind.Fields {
						if field.Type == objects.OpenWorldFieldNumber {
							action.Field = field.Label
							break
						}
					}
				}
				if action.Field == "" || !kindHasNumberField(kind, action.Field) {
					return fmt.Errorf("%w: experience/workspace/actions/%d/field", ErrInvalid, actionIndex)
				}
			default:
				return fmt.Errorf("%w: experience/workspace/actions/%d/type", ErrInvalid, actionIndex)
			}
		}
	}
	if experience.Scene3D != nil {
		spec := experience.Scene3D
		var err error
		if spec.Title, err = boundedText(spec.Title, 120, "experience/scene3d/title"); err != nil {
			return err
		}
		if !validHue(spec.SkyHue) || !validHue(spec.GroundHue) {
			return fmt.Errorf("%w: experience/scene3d/hue", ErrInvalid)
		}
		if spec.Fog != nil && (!finite(*spec.Fog) || *spec.Fog < 0 || *spec.Fog > 0.2) {
			return fmt.Errorf("%w: experience/scene3d/fog", ErrInvalid)
		}
		if spec.Camera != "" && spec.Camera != "orbit" && spec.Camera != "follow" {
			return fmt.Errorf("%w: experience/scene3d/camera", ErrInvalid)
		}
		if spec.Distance != nil && (!finite(*spec.Distance) || *spec.Distance < 4 || *spec.Distance > 40) {
			return fmt.Errorf("%w: experience/scene3d/distance", ErrInvalid)
		}
		if spec.Player != nil && (*spec.Player < 0 || *spec.Player >= len(brief.Things)) {
			return fmt.Errorf("%w: experience/scene3d/player", ErrInvalid)
		}
		if spec.Speed != nil && (!finite(*spec.Speed) || *spec.Speed < 1 || *spec.Speed > 10) {
			return fmt.Errorf("%w: experience/scene3d/speed", ErrInvalid)
		}
		if spec.Gravity != nil && (!finite(*spec.Gravity) || *spec.Gravity < 0 || *spec.Gravity > 40) {
			return fmt.Errorf("%w: experience/scene3d/gravity", ErrInvalid)
		}
		if len(spec.Views) > maxSeedKinds {
			return fmt.Errorf("%w: experience/scene3d/views", ErrInvalid)
		}
		seenKinds := map[int]bool{}
		for index := range spec.Views {
			view := &spec.Views[index]
			if view.Kind < 0 || view.Kind >= len(brief.Kinds) || seenKinds[view.Kind] {
				return fmt.Errorf("%w: experience/scene3d/views/%d/kind", ErrInvalid, index)
			}
			seenKinds[view.Kind] = true
			if view.Primitive != "" {
				switch view.Primitive {
				case "sphere", "box", "cylinder", "cone", "torus":
				default:
					return fmt.Errorf("%w: experience/scene3d/views/%d/primitive", ErrInvalid, index)
				}
			}
			if view.Scale != nil && (!finite(*view.Scale) || *view.Scale < 0.2 || *view.Scale > 10) {
				return fmt.Errorf("%w: experience/scene3d/views/%d/scale", ErrInvalid, index)
			}
			if view.Elevation != nil && (!finite(*view.Elevation) || *view.Elevation < 0 || *view.Elevation > 30) {
				return fmt.Errorf("%w: experience/scene3d/views/%d/elevation", ErrInvalid, index)
			}
			if view.Collider != "" && view.Collider != "none" && view.Collider != "box" && view.Collider != "sphere" {
				return fmt.Errorf("%w: experience/scene3d/views/%d/collider", ErrInvalid, index)
			}
		}
	}
	if experience.Audio != nil {
		if err := validateAudioSpec(experience.Audio); err != nil {
			return err
		}
	}
	if experience.Media != nil {
		media := experience.Media
		var err error
		if media.Title, err = boundedText(media.Title, 80, "experience/media/title"); err != nil {
			return err
		}
		if media.Kind == "" {
			media.Kind = "image"
		}
		if media.Kind != "image" && media.Kind != "video" {
			return fmt.Errorf("%w: experience/media/kind", ErrInvalid)
		}
		if media.Caption, err = boundedText(media.Caption, 300, "experience/media/caption"); err != nil {
			return err
		}
		if media.ShowVariable != "" && !validVariableName(media.ShowVariable) {
			return fmt.Errorf("%w: experience/media/show_variable", ErrInvalid)
		}
		if media.PlayVariable != "" {
			if !validVariableName(media.PlayVariable) {
				return fmt.Errorf("%w: experience/media/play_variable", ErrInvalid)
			}
			if media.Kind != "video" {
				return fmt.Errorf("%w: experience/media/play_variable", ErrInvalid)
			}
		}
	}
	if len(experience.Custom) > 0 {
		var program struct {
			Vars map[string]any `json:"vars"`
		}
		_ = json.Unmarshal(experience.Custom, &program)
		ctx := worldcap.ValidateContext{World: &worldcap.Context{
			Kinds:     map[string]worldcap.KindContext{},
			Entities:  map[string]worldcap.EntityContext{},
			Behaviors: map[string]bool{}, Variables: map[string]string{},
		}}
		for name, value := range program.Vars {
			switch value.(type) {
			case string:
				ctx.World.Variables[name] = "text"
			case float64:
				ctx.World.Variables[name] = "number"
			case bool:
				ctx.World.Variables[name] = "flag"
			}
		}
		if _, err := worldcap.ValidateCustom(experience.Custom, ctx); err != nil {
			return fmt.Errorf("%w: experience/custom: %v", ErrInvalid, err)
		}
	}
	return nil
}

func kindHasFieldLabel(kind Kind, label string) bool {
	for _, field := range kind.Fields {
		if field.Label == label {
			return true
		}
	}
	return false
}

func kindHasNumberField(kind Kind, label string) bool {
	for _, field := range kind.Fields {
		if field.Label == label && field.Type == objects.OpenWorldFieldNumber {
			return true
		}
	}
	return false
}

func validateGoalSpec(goal *GoalSpec, brief *Brief, timer *float64) error {
	var err error
	if goal.Label, err = boundedText(goal.Label, 120, "experience/live/goal/label"); err != nil {
		return err
	}
	switch goal.Type {
	case "collect":
		if goal.Kind == nil || *goal.Kind < 0 || *goal.Kind >= len(brief.Kinds) {
			return fmt.Errorf("%w: experience/live/goal/kind", ErrInvalid)
		}
		if goal.Count == nil || *goal.Count < 1 || *goal.Count > 40 {
			return fmt.Errorf("%w: experience/live/goal/count", ErrInvalid)
		}
	case "score":
		if goal.Variable != "" {
			if !validVariableName(goal.Variable) {
				return fmt.Errorf("%w: experience/live/goal/variable", ErrInvalid)
			}
		} else {
			if goal.Entity == nil || *goal.Entity < 0 || *goal.Entity >= len(brief.Things) {
				return fmt.Errorf("%w: experience/live/goal/entity", ErrInvalid)
			}
			targetKind := brief.Kinds[brief.Things[*goal.Entity].Kind]
			if !kindHasNumberField(targetKind, goal.Field) {
				return fmt.Errorf("%w: experience/live/goal/field", ErrInvalid)
			}
		}
		if goal.Target == nil || !finite(*goal.Target) {
			return fmt.Errorf("%w: experience/live/goal/target", ErrInvalid)
		}
	case "reach":
		if goal.Entity == nil || *goal.Entity < 0 || *goal.Entity >= len(brief.Things) {
			return fmt.Errorf("%w: experience/live/goal/entity", ErrInvalid)
		}
	case "survive":
		if timer == nil || *timer < 1 {
			return fmt.Errorf("%w: experience/live/goal/timer", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: experience/live/goal/type", ErrInvalid)
	}
	return nil
}

func validateParticleSpec(particles *ParticleSpec) error {
	switch particles.Kind {
	case "rain", "motes", "stars", "snow":
	default:
		return fmt.Errorf("%w: experience/live/particles/kind", ErrInvalid)
	}
	if particles.Count != nil && (*particles.Count < 0 || *particles.Count > 120) {
		return fmt.Errorf("%w: experience/live/particles/count", ErrInvalid)
	}
	if particles.Hue != nil && (*particles.Hue < 0 || *particles.Hue > 359) {
		return fmt.Errorf("%w: experience/live/particles/hue", ErrInvalid)
	}
	if particles.Size != nil && (!finite(*particles.Size) || *particles.Size < 1 || *particles.Size > 6) {
		return fmt.Errorf("%w: experience/live/particles/size", ErrInvalid)
	}
	if particles.Speed != nil && (!finite(*particles.Speed) || *particles.Speed < 0.2 || *particles.Speed > 3) {
		return fmt.Errorf("%w: experience/live/particles/speed", ErrInvalid)
	}
	return nil
}

func validateTimelineSpec(timeline *TimelineSpec, brief *Brief) error {
	duration := 12.0
	if timeline.Duration != nil {
		duration = *timeline.Duration
	}
	if !finite(duration) || duration < 1 || duration > 120 {
		return fmt.Errorf("%w: experience/live/timeline/duration", ErrInvalid)
	}
	if len(timeline.Tracks) > 16 {
		return fmt.Errorf("%w: experience/live/timeline/tracks", ErrInvalid)
	}
	for trackIndex := range timeline.Tracks {
		track := &timeline.Tracks[trackIndex]
		if track.Entity < 0 || track.Entity >= len(brief.Things) {
			return fmt.Errorf("%w: experience/live/timeline/tracks/%d/entity", ErrInvalid, trackIndex)
		}
		switch track.Property {
		case "x", "y", "scale", "opacity", "rotate", "hue":
		default:
			return fmt.Errorf("%w: experience/live/timeline/tracks/%d/property", ErrInvalid, trackIndex)
		}
		if len(track.Keys) < 2 || len(track.Keys) > 16 {
			return fmt.Errorf("%w: experience/live/timeline/tracks/%d/keys", ErrInvalid, trackIndex)
		}
		previous := -1.0
		for keyIndex := range track.Keys {
			key := &track.Keys[keyIndex]
			t := 0.0
			if key.T != nil {
				t = *key.T
			}
			if !finite(t) || t < 0 || t > duration || t <= previous {
				return fmt.Errorf("%w: experience/live/timeline/tracks/%d/keys/%d/t", ErrInvalid, trackIndex, keyIndex)
			}
			previous = t
			if key.Easing != "" && key.Easing != "linear" && key.Easing != "ease_in" &&
				key.Easing != "ease_out" && key.Easing != "ease_in_out" {
				return fmt.Errorf("%w: experience/live/timeline/tracks/%d/keys/%d/easing", ErrInvalid, trackIndex, keyIndex)
			}
		}
	}
	return nil
}

func validFieldType(value string) bool {
	switch value {
	case objects.OpenWorldFieldText, objects.OpenWorldFieldNumber, objects.OpenWorldFieldFlag:
		return true
	default:
		return false
	}
}

func validHue(value *int) bool {
	return value == nil || (*value >= 0 && *value <= 359)
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func scalarMatches(fieldType string, value any) bool {
	switch fieldType {
	case objects.OpenWorldFieldText:
		_, ok := value.(string)
		return ok
	case objects.OpenWorldFieldNumber:
		number, ok := value.(float64)
		return ok && finite(number)
	case objects.OpenWorldFieldFlag:
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

func (brief *Brief) normalize() error {
	var err error
	if brief.Premise, err = boundedText(brief.Premise, maxPremiseRunes, "premise"); err != nil {
		return err
	}
	if brief.Atmosphere, err = boundedText(brief.Atmosphere, maxAtmosphereRunes, "atmosphere"); err != nil {
		return err
	}
	if brief.Palette != nil {
		if !validHue(brief.Palette.SkyHue) || !validHue(brief.Palette.GroundHue) ||
			!validHue(brief.Palette.AccentHue) {
			return fmt.Errorf("%w: palette hue", ErrInvalid)
		}
	}
	if len(brief.Kinds) > maxSeedKinds {
		return fmt.Errorf("%w: kinds", ErrInvalid)
	}
	if len(brief.Things) > maxSeedThings {
		return fmt.Errorf("%w: things", ErrInvalid)
	}
	if len(brief.Relations) > maxSeedRelations {
		return fmt.Errorf("%w: relations", ErrInvalid)
	}
	if len(brief.Interactions) > maxSeedInteractions {
		return fmt.Errorf("%w: interactions", ErrInvalid)
	}
	kindNames := make(map[string]bool, len(brief.Kinds))
	for index := range brief.Kinds {
		kind := &brief.Kinds[index]
		if kind.Name, err = requiredText(kind.Name, maxKindNameRunes, fmt.Sprintf("kinds/%d/name", index)); err != nil {
			return err
		}
		key := strings.ToLower(kind.Name)
		if kindNames[key] {
			return fmt.Errorf("%w: duplicate kind name", ErrInvalid)
		}
		kindNames[key] = true
		if kind.Essence, err = boundedText(kind.Essence, maxEssenceRunes, fmt.Sprintf("kinds/%d/essence", index)); err != nil {
			return err
		}
		if !validHue(kind.Hue) {
			return fmt.Errorf("%w: kinds/%d/hue", ErrInvalid, index)
		}
		if kind.Motion != "" && !validMotion(kind.Motion) {
			return fmt.Errorf("%w: kinds/%d/motion", ErrInvalid, index)
		}
		if kind.Form != "" {
			kind.Form = strings.TrimSpace(kind.Form)
			if runeLength(kind.Form) > maxFormRunes || worldcap.ValidateSVGPath(kind.Form) != nil {
				return fmt.Errorf("%w: kinds/%d/form", ErrInvalid, index)
			}
		}
		if len(kind.Fields) > maxSeedFields {
			return fmt.Errorf("%w: kinds/%d/fields", ErrInvalid, index)
		}
		labels := make(map[string]bool, len(kind.Fields))
		for fieldIndex := range kind.Fields {
			field := &kind.Fields[fieldIndex]
			if field.Label, err = requiredText(field.Label, maxFieldLabelRunes,
				fmt.Sprintf("kinds/%d/fields/%d/label", index, fieldIndex)); err != nil {
				return err
			}
			if labels[field.Label] {
				return fmt.Errorf("%w: duplicate field label", ErrInvalid)
			}
			labels[field.Label] = true
			if !validFieldType(field.Type) {
				return fmt.Errorf("%w: kinds/%d/fields/%d/type", ErrInvalid, index, fieldIndex)
			}
			if field.Initial != nil && !scalarMatches(field.Type, field.Initial) {
				return fmt.Errorf("%w: kinds/%d/fields/%d/initial", ErrInvalid, index, fieldIndex)
			}
		}
	}
	if len(brief.Kinds) == 0 {
		if len(brief.Things) > 0 || len(brief.Relations) > 0 || len(brief.Interactions) > 0 {
			return fmt.Errorf("%w: things without kinds", ErrInvalid)
		}
	}
	thingNames := make(map[string]bool, len(brief.Things))
	for index := range brief.Things {
		thing := &brief.Things[index]
		if thing.Kind < 0 || thing.Kind >= len(brief.Kinds) {
			return fmt.Errorf("%w: things/%d/kind", ErrInvalid, index)
		}
		if thing.Name, err = requiredText(thing.Name, maxThingNameRunes, fmt.Sprintf("things/%d/name", index)); err != nil {
			return err
		}
		key := fmt.Sprintf("%d:%s", thing.Kind, strings.ToLower(thing.Name))
		if thingNames[key] {
			return fmt.Errorf("%w: duplicate thing name in kind", ErrInvalid)
		}
		thingNames[key] = true
		if thing.Text, err = boundedText(thing.Text, maxThingTextRunes, fmt.Sprintf("things/%d/text", index)); err != nil {
			return err
		}
		if thing.X != nil && (!finite(*thing.X) || *thing.X < 0 || *thing.X > 1) {
			return fmt.Errorf("%w: things/%d/x", ErrInvalid, index)
		}
		if thing.Y != nil && (!finite(*thing.Y) || *thing.Y < 0 || *thing.Y > 1) {
			return fmt.Errorf("%w: things/%d/y", ErrInvalid, index)
		}
		if len(thing.Values) > 0 {
			kind := brief.Kinds[thing.Kind]
			fieldTypes := make(map[string]string, len(kind.Fields))
			for _, field := range kind.Fields {
				fieldTypes[field.Label] = field.Type
			}
			for label, value := range thing.Values {
				fieldType, ok := fieldTypes[label]
				if !ok || !scalarMatches(fieldType, value) {
					return fmt.Errorf("%w: things/%d/values/%s", ErrInvalid, index, label)
				}
			}
		}
	}
	for index := range brief.Relations {
		relation := &brief.Relations[index]
		if relation.From < 0 || relation.From >= len(brief.Things) ||
			relation.To < 0 || relation.To >= len(brief.Things) || relation.From == relation.To {
			return fmt.Errorf("%w: relations/%d", ErrInvalid, index)
		}
		if relation.Label, err = boundedText(relation.Label, maxRelationRunes,
			fmt.Sprintf("relations/%d/label", index)); err != nil {
			return err
		}
	}
	for index := range brief.Interactions {
		interaction := &brief.Interactions[index]
		if interaction.From < 0 || interaction.From >= len(brief.Things) {
			return fmt.Errorf("%w: interactions/%d/from", ErrInvalid, index)
		}
		targetIndex := interaction.From
		if interaction.To != nil {
			if *interaction.To < 0 || *interaction.To >= len(brief.Things) {
				return fmt.Errorf("%w: interactions/%d/to", ErrInvalid, index)
			}
			targetIndex = *interaction.To
		}
		if interaction.Verb, err = requiredText(interaction.Verb, maxVerbRunes,
			fmt.Sprintf("interactions/%d/verb", index)); err != nil {
			return err
		}
		if interaction.Hint, err = boundedText(interaction.Hint, maxHintRunes,
			fmt.Sprintf("interactions/%d/hint", index)); err != nil {
			return err
		}
		targetKind := brief.Kinds[brief.Things[targetIndex].Kind]
		fieldType, fieldOK := "status", interaction.Field == "" || interaction.Field == "status"
		if !fieldOK {
			for _, field := range targetKind.Fields {
				if field.Label == interaction.Field {
					fieldType, fieldOK = field.Type, true
					break
				}
			}
		}
		if !fieldOK {
			return fmt.Errorf("%w: interactions/%d/field", ErrInvalid, index)
		}
		switch interaction.Op {
		case objects.OpenWorldOpSet:
			if fieldType == "status" {
				value, ok := interaction.Value.(string)
				if !ok || (value != objects.OpenWorldStatusOpen && value != objects.OpenWorldStatusDone) {
					return fmt.Errorf("%w: interactions/%d/value", ErrInvalid, index)
				}
			} else if !scalarMatches(fieldType, interaction.Value) {
				return fmt.Errorf("%w: interactions/%d/value", ErrInvalid, index)
			}
		case objects.OpenWorldOpAdd:
			if fieldType != objects.OpenWorldFieldNumber {
				return fmt.Errorf("%w: interactions/%d/op", ErrInvalid, index)
			}
			number, ok := interaction.Value.(float64)
			if !ok || !finite(number) {
				return fmt.Errorf("%w: interactions/%d/value", ErrInvalid, index)
			}
		case objects.OpenWorldOpToggle:
			if fieldType != "status" && fieldType != objects.OpenWorldFieldFlag {
				return fmt.Errorf("%w: interactions/%d/op", ErrInvalid, index)
			}
			if interaction.Value != nil {
				return fmt.Errorf("%w: interactions/%d/value", ErrInvalid, index)
			}
		default:
			return fmt.Errorf("%w: interactions/%d/op", ErrInvalid, index)
		}
	}
	if brief.Experience != nil {
		if err := validateExperienceSpec(brief); err != nil {
			return err
		}
	}
	return nil
}

func validVariableName(value string) bool {
	if value == "" || len(value) > 40 {
		return false
	}
	for index, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case (r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') && index > 0:
		default:
			return false
		}
	}
	return true
}

func validateAudioSpec(spec *AudioSpec) error {
	var err error
	if spec.Title, err = boundedText(spec.Title, 80, "experience/audio/title"); err != nil {
		return err
	}
	if spec.Tone != nil {
		if err := validateToneSpec(spec.Tone, "experience/audio/tone"); err != nil {
			return err
		}
	}
	if spec.Volume != nil && (!finite(*spec.Volume) || *spec.Volume < 0 || *spec.Volume > 1) {
		return fmt.Errorf("%w: experience/audio/volume", ErrInvalid)
	}
	if len(spec.Cues) > 8 {
		return fmt.Errorf("%w: experience/audio/cues", ErrInvalid)
	}
	for index := range spec.Cues {
		cue := &spec.Cues[index]
		if !validVariableName(cue.Variable) {
			return fmt.Errorf("%w: experience/audio/cues/%d/variable", ErrInvalid, index)
		}
		if cue.Mode == "" {
			cue.Mode = "change"
		}
		switch cue.Mode {
		case "truthy", "falsy", "change", "increase", "decrease":
		default:
			return fmt.Errorf("%w: experience/audio/cues/%d/mode", ErrInvalid, index)
		}
		if cue.Tone != nil {
			if err := validateToneSpec(cue.Tone, fmt.Sprintf("experience/audio/cues/%d/tone", index)); err != nil {
				return err
			}
		}
		if cue.Volume != nil && (!finite(*cue.Volume) || *cue.Volume < 0 || *cue.Volume > 1) {
			return fmt.Errorf("%w: experience/audio/cues/%d/volume", ErrInvalid, index)
		}
	}
	return nil
}

func validateToneSpec(tone *ToneSpec, path string) error {
	if tone.Frequency != nil && (!finite(*tone.Frequency) || *tone.Frequency < 40 || *tone.Frequency > 8000) {
		return fmt.Errorf("%w: %s/frequency", ErrInvalid, path)
	}
	if tone.Wave != "" && tone.Wave != "sine" && tone.Wave != "square" &&
		tone.Wave != "triangle" && tone.Wave != "sawtooth" {
		return fmt.Errorf("%w: %s/wave", ErrInvalid, path)
	}
	if tone.Volume != nil && (!finite(*tone.Volume) || *tone.Volume < 0 || *tone.Volume > 0.5) {
		return fmt.Errorf("%w: %s/volume", ErrInvalid, path)
	}
	if tone.Pulse != nil && (!finite(*tone.Pulse) || *tone.Pulse < 0 || *tone.Pulse > 4) {
		return fmt.Errorf("%w: %s/pulse", ErrInvalid, path)
	}
	return nil
}
