// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func openWorldCreateContent(t *testing.T) OpenWorldContent {
	t.Helper()
	return OpenWorldContent{
		Premise: "The city remembers every choice.",
		Kinds: []OpenWorldKindInput{
			{Name: "Resident", Hue: 168, Fields: []OpenWorldField{{Key: "mood", Label: "Mood", Type: OpenWorldFieldText}}},
			{Name: "Place", Hue: 214, Fields: []OpenWorldField{{Key: "light", Label: "Light", Type: OpenWorldFieldNumber}}},
		},
		Nodes: []OpenWorldNodeInput{
			{KindIndex: 0, Name: "The first resident", Text: "They heard the city first."},
			{KindIndex: 1, Name: "A square that listens", Text: "Every choice leaves light."},
		},
		Links: []OpenWorldLinkInput{{FromIndex: 0, ToIndex: 1, Label: "visits"}},
		Rules: []OpenWorldRuleInput{{
			TriggerIndex: 0,
			Effects: []OpenWorldEffectInput{
				{NodeIndex: 1, Field: "light", Op: OpenWorldOpAdd, Value: float64(1)},
				{NodeIndex: 1, Field: OpenWorldStatusField, Op: OpenWorldOpSet, Value: OpenWorldStatusDone},
			},
		}},
		Views: []string{OpenWorldViewStage, OpenWorldViewCards},
		Tags:  []string{"city", "choice"},
	}
}

func createOpenWorldState(t *testing.T, content OpenWorldContent) *OpenWorldState {
	t.Helper()
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	stateRaw, _, err := ValidateCreate(CreateInput{
		ObjectType: TypeOpenWorld, Title: "A city that changes", Content: encoded,
	})
	if err != nil {
		t.Fatalf("create open world: %v", err)
	}
	var state OpenWorldState
	if err := json.Unmarshal(stateRaw, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	return &state
}

func applyOpenWorldPlan(t *testing.T, state *OpenWorldState, action string, input map[string]any) *OpenWorldState {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	plan, err := PlanAction(TypeOpenWorld, action, raw)
	if err != nil {
		t.Fatalf("plan %s: %v", action, err)
	}
	stateRaw, _ := json.Marshal(state)
	newRaw, _, _, err := ApplyStateAction(TypeOpenWorld, stateRaw, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatalf("apply %s: %v", action, err)
	}
	var next OpenWorldState
	if err := json.Unmarshal(newRaw, &next); err != nil {
		t.Fatalf("decode next state: %v", err)
	}
	return &next
}

func TestOpenWorldCreateValidation(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	if len(state.Kinds) != 2 || len(state.Nodes) != 2 || len(state.Links) != 1 || len(state.Rules) != 1 {
		t.Fatalf("unexpected created state: %+v", state)
	}
	if state.Kinds[0].Name != "Resident" || state.Kinds[0].Hue != 168 {
		t.Fatalf("kind not preserved: %+v", state.Kinds[0])
	}
	for _, kind := range state.Kinds {
		if _, err := uuid.Parse(kind.ID); err != nil || uuid.MustParse(kind.ID).Version() != 7 {
			t.Fatalf("kind id is not uuidv7: %q", kind.ID)
		}
	}
	for _, node := range state.Nodes {
		if node.Status != OpenWorldStatusOpen {
			t.Fatalf("node status = %q", node.Status)
		}
		if node.X < openWorldMinX || node.X > openWorldMaxX || node.Y < openWorldMinY || node.Y > openWorldMaxY {
			t.Fatalf("node position out of bounds: %+v", node)
		}
	}
	if len(state.Views) != 2 || state.Views[0] != OpenWorldViewStage {
		t.Fatalf("views not preserved: %v", state.Views)
	}
	if state.Rules[0].TriggerID != state.Nodes[0].ID {
		t.Fatalf("trigger id not remapped")
	}
	if state.Rules[0].Effects[0].NodeID != state.Nodes[1].ID {
		t.Fatalf("effect id not remapped")
	}
}

func TestOpenWorldCreateRejectsInvalidReferences(t *testing.T) {
	cases := map[string]OpenWorldContent{
		"kind index": {
			Kinds: []OpenWorldKindInput{{Name: "Thing", Hue: 10}},
			Nodes: []OpenWorldNodeInput{{KindIndex: 3, Name: "Out of range"}},
		},
		"link index": {
			Kinds: []OpenWorldKindInput{{Name: "Thing", Hue: 10}},
			Nodes: []OpenWorldNodeInput{{KindIndex: 0, Name: "Only"}},
			Links: []OpenWorldLinkInput{{FromIndex: 0, ToIndex: 4}},
		},
		"rule value type": {
			Kinds: []OpenWorldKindInput{{Name: "Thing", Hue: 10, Fields: []OpenWorldField{{Key: "count", Label: "Count", Type: OpenWorldFieldNumber}}}},
			Nodes: []OpenWorldNodeInput{{KindIndex: 0, Name: "One"}},
			Rules: []OpenWorldRuleInput{{
				TriggerIndex: 0,
				Effects:      []OpenWorldEffectInput{{NodeIndex: 0, Field: "count", Op: OpenWorldOpSet, Value: "not a number"}},
			}},
		},
		"unknown field": {
			Kinds: []OpenWorldKindInput{{Name: "Thing", Hue: 10}},
			Nodes: []OpenWorldNodeInput{{KindIndex: 0, Name: "One"}},
			Rules: []OpenWorldRuleInput{{
				TriggerIndex: 0,
				Effects:      []OpenWorldEffectInput{{NodeIndex: 0, Field: "ghost", Op: OpenWorldOpSet, Value: "x"}},
			}},
		},
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			encoded, _ := json.Marshal(content)
			if _, _, err := ValidateCreate(CreateInput{
				ObjectType: TypeOpenWorld, Title: "Invalid world", Content: encoded,
			}); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestOpenWorldRuleRunsOnceOnTouch(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	planRaw, _ := json.Marshal(map[string]any{"node_id": state.Nodes[1].ID})
	plan, err := PlanAction(TypeOpenWorld, "touch_node", planRaw)
	if err != nil {
		t.Fatalf("plan touch: %v", err)
	}
	stateRaw, _ := json.Marshal(state)
	newRaw, _, _, err := ApplyStateAction(TypeOpenWorld, stateRaw, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatalf("touch: %v", err)
	}
	var next OpenWorldState
	if err := json.Unmarshal(newRaw, &next); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The square was not the rule trigger, so no effect applied. Touching the
	// resident triggers both effects on the square.
	planRaw, _ = json.Marshal(map[string]any{"node_id": next.Nodes[0].ID})
	plan, err = PlanAction(TypeOpenWorld, "touch_node", planRaw)
	if err != nil {
		t.Fatalf("plan touch resident: %v", err)
	}
	secondRaw, _ := json.Marshal(next)
	newRaw, _, _, err = ApplyStateAction(TypeOpenWorld, secondRaw, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatalf("touch resident: %v", err)
	}
	if err := json.Unmarshal(newRaw, &next); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if len(plan.Effects) != 2 {
		t.Fatalf("effects applied = %d, want 2", len(plan.Effects))
	}
	light, ok := next.Nodes[1].Values["light"].(float64)
	if !ok || light != 1 {
		t.Fatalf("light = %v, want 1", next.Nodes[1].Values["light"])
	}
	if next.Nodes[1].Status != OpenWorldStatusDone {
		t.Fatalf("square status = %q, want done", next.Nodes[1].Status)
	}
	if next.Nodes[0].Status != OpenWorldStatusDone {
		t.Fatalf("trigger status = %q, want done", next.Nodes[0].Status)
	}
}

func TestOpenWorldSetFieldTriggersRules(t *testing.T) {
	content := openWorldCreateContent(t)
	content.Rules = []OpenWorldRuleInput{{
		TriggerIndex: 1,
		Effects: []OpenWorldEffectInput{
			{NodeIndex: 0, Field: "mood", Op: OpenWorldOpSet, Value: "bright"},
		},
	}}
	state := createOpenWorldState(t, content)
	next := applyOpenWorldPlan(t, state, "set_node_field", map[string]any{
		"node_id": state.Nodes[1].ID, "field": "light", "value": float64(3),
	})
	if next.Nodes[0].Values["mood"] != "bright" {
		t.Fatalf("rule did not set mood: %v", next.Nodes[0].Values)
	}
	if next.Nodes[1].Values["light"] != float64(3) {
		t.Fatalf("field not set: %v", next.Nodes[1].Values)
	}
}

func TestOpenWorldRemoveNodeCascades(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	next := applyOpenWorldPlan(t, state, "remove_node", map[string]any{"node_id": state.Nodes[1].ID})
	if len(next.Nodes) != 1 {
		t.Fatalf("nodes = %d", len(next.Nodes))
	}
	if len(next.Links) != 0 {
		t.Fatalf("links = %d, want 0", len(next.Links))
	}
	if len(next.Rules) != 0 {
		t.Fatalf("rules = %d, want 0 (all effects targeted the removed node)", len(next.Rules))
	}
}

func TestOpenWorldRemoveKindInUseRejected(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	raw, _ := json.Marshal(map[string]any{"kind_id": state.Kinds[0].ID})
	plan, err := PlanAction(TypeOpenWorld, "remove_kind", raw)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	stateRaw, _ := json.Marshal(state)
	if _, _, _, err := ApplyStateAction(TypeOpenWorld, stateRaw, json.RawMessage(`{}`), plan, ""); err == nil {
		t.Fatalf("expected conflict for kind in use")
	}
}

func TestOpenWorldDerivedStateRegeneratesIdentity(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	child := applyOpenWorldPlan(t, state, "set_node_field", map[string]any{
		"node_id": state.Nodes[0].ID, "field": "mood", "value": "calm",
	})
	derived, err := OpenWorldDerivedState("A different city", child, map[string]bool{
		"premise": true, "structure": true, "views": true, "tags": true,
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if derived.Title != "A different city" {
		t.Fatalf("title = %q", derived.Title)
	}
	parentIDs := map[string]bool{}
	for _, kind := range state.Kinds {
		parentIDs[kind.ID] = true
	}
	for _, kind := range derived.Kinds {
		if parentIDs[kind.ID] {
			t.Fatalf("kind id was reused: %s", kind.ID)
		}
	}
	if derived.Nodes[0].Values["mood"] != "calm" {
		t.Fatalf("values not inherited: %v", derived.Nodes[0].Values)
	}
	if derived.Rules[0].TriggerID != derived.Nodes[0].ID {
		t.Fatalf("rule trigger not remapped")
	}
}

func TestOpenWorldStateBound(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	encoded, _ := json.Marshal(state)
	if err := ValidateStateBytes(TypeOpenWorld, encoded); err != nil {
		t.Fatalf("valid state rejected: %v", err)
	}
}

func presentationContent(t *testing.T) OpenWorldContent {
	t.Helper()
	return OpenWorldContent{
		Premise: "A coast that answers the light.",
		Kinds: []OpenWorldKindInput{
			{
				Name: "Beacon", Hue: 42,
				Fields: []OpenWorldField{{Key: "brightness", Label: "Brightness", Type: OpenWorldFieldNumber}},
				Appearance: &OpenWorldAppearance{
					Shape: OpenWorldShapeBeacon, Size: 96, Motion: OpenWorldMotionFlicker,
					Intensity: 55, Glow: 70, Label: OpenWorldLabelAlways, Layer: OpenWorldLayerFront,
				},
				Bindings: []OpenWorldBinding{{
					Field: "brightness", Target: OpenWorldBindingGlow,
					InMin: 0, InMax: 10, OutMin: 20, OutMax: 100,
				}},
			},
			{
				Name: "Tide", Hue: 196,
				Fields: []OpenWorldField{{Key: "height", Label: "Height", Type: OpenWorldFieldNumber}},
				Appearance: &OpenWorldAppearance{
					Shape: OpenWorldShapeWave, Size: 130, Motion: OpenWorldMotionSway,
					Intensity: 45, Glow: 25, Label: OpenWorldLabelAlways, Layer: OpenWorldLayerBack,
				},
				Bindings: []OpenWorldBinding{{
					Field: "height", Target: OpenWorldBindingScale,
					InMin: 0, InMax: 10, OutMin: 0.6, OutMax: 1.6,
				}},
			},
		},
		Nodes: []OpenWorldNodeInput{
			{KindIndex: 0, Name: "North beacon", X: floatPtr(300), Y: floatPtr(300),
				Values: map[string]any{"brightness": float64(4)}},
			{KindIndex: 1, Name: "Midnight tide", X: floatPtr(500), Y: floatPtr(700),
				Values: map[string]any{"height": float64(3)}},
		},
		Rules: []OpenWorldRuleInput{{
			TriggerIndex: 0,
			Effects: []OpenWorldEffectInput{
				{NodeIndex: 1, Field: "height", Op: OpenWorldOpAdd, Value: float64(2)},
			},
		}},
		Views: []string{OpenWorldViewScene, OpenWorldViewCards},
		Presentation: &OpenWorldPresentation{
			Scene: OpenWorldScene{
				Backdrop: OpenWorldBackdropHorizon, SkyHue: 222, GroundHue: 205,
				Horizon: 64, AccentHue: 42, Ambient: OpenWorldAmbientStars, AmbientDensity: 45,
			},
			LinkStyle: OpenWorldLinkThread,
		},
	}
}

func floatPtr(value float64) *float64 { return &value }

func TestOpenWorldPresentationAppearanceAndBindings(t *testing.T) {
	state := createOpenWorldState(t, presentationContent(t))
	if state.Presentation.Scene.Backdrop != OpenWorldBackdropHorizon ||
		state.Presentation.Scene.SkyHue != 222 || state.Presentation.LinkStyle != OpenWorldLinkThread {
		t.Fatalf("presentation not preserved: %+v", state.Presentation)
	}
	if state.Kinds[0].Appearance.Shape != OpenWorldShapeBeacon ||
		state.Kinds[0].Appearance.Size != 96 || state.Kinds[0].Appearance.Layer != OpenWorldLayerFront {
		t.Fatalf("appearance not preserved: %+v", state.Kinds[0].Appearance)
	}
	if len(state.Kinds[0].Bindings) != 1 || state.Kinds[0].Bindings[0].Target != OpenWorldBindingGlow {
		t.Fatalf("bindings not preserved: %+v", state.Kinds[0].Bindings)
	}
	if state.Nodes[0].Values["brightness"] != float64(4) {
		t.Fatalf("initial value not preserved: %+v", state.Nodes[0].Values)
	}

	next := applyOpenWorldPlan(t, state, "set_presentation", map[string]any{
		"presentation": OpenWorldPresentation{
			Scene: OpenWorldScene{
				Backdrop: OpenWorldBackdropVoid, SkyHue: 268, GroundHue: 232,
				Horizon: 50, AccentHue: 190, Ambient: OpenWorldAmbientMotes, AmbientDensity: 55,
			},
			LinkStyle: OpenWorldLinkOrbit,
		},
	})
	if next.Presentation.Scene.Backdrop != OpenWorldBackdropVoid ||
		next.Presentation.LinkStyle != OpenWorldLinkOrbit {
		t.Fatalf("set_presentation did not persist: %+v", next.Presentation)
	}

	updated := applyOpenWorldPlan(t, next, "update_kind", map[string]any{
		"kind_id": next.Kinds[0].ID,
		"appearance": OpenWorldAppearance{
			Shape: OpenWorldShapeRing, Size: 60, Motion: OpenWorldMotionPulse,
			Intensity: 70, Glow: 55, Label: OpenWorldLabelHover, Layer: OpenWorldLayerMid,
		},
		"bindings": []OpenWorldBinding{{
			Field: "brightness", Target: OpenWorldBindingScale,
			InMin: 0, InMax: 10, OutMin: 0.8, OutMax: 1.4,
		}},
	})
	if updated.Kinds[0].Appearance.Shape != OpenWorldShapeRing ||
		updated.Kinds[0].Bindings[0].Target != OpenWorldBindingScale {
		t.Fatalf("update_kind did not persist appearance: %+v", updated.Kinds[0])
	}
}

func TestOpenWorldConditionalRule(t *testing.T) {
	content := OpenWorldContent{
		Kinds: []OpenWorldKindInput{
			{Name: "Signal", Hue: 10, Fields: []OpenWorldField{
				{Key: "heard", Label: "Heard", Type: OpenWorldFieldFlag},
			}},
			{Name: "Answer", Hue: 200, Fields: []OpenWorldField{
				{Key: "text", Label: "Text", Type: OpenWorldFieldText},
			}},
		},
		Nodes: []OpenWorldNodeInput{
			{KindIndex: 0, Name: "Trigger", Values: map[string]any{"heard": false}},
			{KindIndex: 0, Name: "Source", Values: map[string]any{"heard": false}},
			{KindIndex: 1, Name: "Answer"},
		},
		Rules: []OpenWorldRuleInput{{
			TriggerIndex: 0,
			Condition: &OpenWorldConditionInput{
				NodeIndex: 1, Field: "heard", Op: OpenWorldCondEq, Value: true,
			},
			Effects: []OpenWorldEffectInput{
				{NodeIndex: 2, Field: "text", Op: OpenWorldOpSet, Value: "answered"},
			},
		}},
	}
	state := createOpenWorldState(t, content)

	// The guard is false, so touching the trigger changes nothing except the
	// trigger's own status.
	silent := applyOpenWorldPlan(t, state, "touch_node", map[string]any{"node_id": state.Nodes[0].ID})
	if silent.Nodes[2].Values["text"] != nil {
		t.Fatalf("guarded rule ran while false: %+v", silent.Nodes[2].Values)
	}

	// Set the source flag: this triggers the source's own rules (none), not
	// the guarded rule.
	armed := applyOpenWorldPlan(t, silent, "set_node_field", map[string]any{
		"node_id": silent.Nodes[1].ID, "field": "heard", "value": true,
	})
	answered := applyOpenWorldPlan(t, armed, "touch_node", map[string]any{"node_id": armed.Nodes[0].ID})
	if answered.Nodes[2].Values["text"] != "answered" {
		t.Fatalf("guarded rule did not run when true: %+v", answered.Nodes[2].Values)
	}
}

func TestOpenWorldDerivedStateCopiesPresentation(t *testing.T) {
	state := createOpenWorldState(t, presentationContent(t))
	derived, err := OpenWorldDerivedState("A different coast", state, map[string]bool{
		"premise": true, "structure": true, "views": true,
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if derived.Presentation != state.Presentation {
		t.Fatalf("presentation not inherited: %+v vs %+v", derived.Presentation, state.Presentation)
	}
	if derived.Kinds[0].Appearance != state.Kinds[0].Appearance {
		t.Fatalf("appearance not inherited: %+v", derived.Kinds[0].Appearance)
	}
	if len(derived.Kinds[0].Bindings) != 1 ||
		derived.Kinds[0].Bindings[0].Target != OpenWorldBindingGlow {
		t.Fatalf("bindings not inherited: %+v", derived.Kinds[0].Bindings)
	}
	for index := range state.Kinds {
		if derived.Kinds[index].ID == state.Kinds[index].ID {
			t.Fatalf("derived kind reused parent id")
		}
	}
	for index := range state.Nodes {
		if derived.Nodes[index].ID == state.Nodes[index].ID {
			t.Fatalf("derived node reused parent id")
		}
	}
}

func TestOpenWorldRejectsInvalidPresentation(t *testing.T) {
	cases := map[string]OpenWorldContent{
		"shape": func() OpenWorldContent {
			content := presentationContent(t)
			content.Kinds[0].Appearance = &OpenWorldAppearance{
				Shape: "lighthouse", Size: 50, Motion: OpenWorldMotionNone,
				Intensity: 50, Glow: 50, Label: OpenWorldLabelAlways, Layer: OpenWorldLayerMid,
			}
			return content
		}(),
		"binding text field": func() OpenWorldContent {
			content := presentationContent(t)
			content.Kinds[1].Bindings = []OpenWorldBinding{{
				Field: "height", Target: "script", InMin: 0, InMax: 1, OutMin: 0, OutMax: 1,
			}}
			return content
		}(),
		"condition operator": func() OpenWorldContent {
			content := presentationContent(t)
			content.Rules[0].Condition = &OpenWorldConditionInput{
				NodeIndex: 1, Field: "height", Op: "contains", Value: "x",
			}
			return content
		}(),
		"scene hue": func() OpenWorldContent {
			content := presentationContent(t)
			content.Presentation.Scene.SkyHue = 900
			return content
		}(),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			encoded, _ := json.Marshal(content)
			if _, _, err := ValidateCreate(CreateInput{
				ObjectType: TypeOpenWorld, Title: "Invalid look", Content: encoded,
			}); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestOpenWorldRemoveNodeRemovesGuardedRules(t *testing.T) {
	content := openWorldCreateContent(t)
	content.Rules = []OpenWorldRuleInput{{
		TriggerIndex: 0,
		Condition: &OpenWorldConditionInput{
			NodeIndex: 1, Field: "light", Op: OpenWorldCondGte, Value: float64(1),
		},
		Effects: []OpenWorldEffectInput{
			{NodeIndex: 0, Field: "mood", Op: OpenWorldOpSet, Value: "bright"},
		},
	}}
	state := createOpenWorldState(t, content)
	if len(state.Rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(state.Rules))
	}
	next := applyOpenWorldPlan(t, state, "remove_node", map[string]any{"node_id": state.Nodes[1].ID})
	if len(next.Rules) != 0 {
		t.Fatalf("guarded rule survived its condition target: %+v", next.Rules)
	}
	if len(next.Nodes) != 1 {
		t.Fatalf("nodes = %d", len(next.Nodes))
	}
}
