// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"testing"

	"github.com/QiyanAI/derive-world/internal/worldcap"
)

func TestOpenWorldCustomActionChangesVariables(t *testing.T) {
	program := json.RawMessage(`{
		"title":"instrument","width":1200,"height":800,
		"background":{"mode":"gradient","hue":240,"hue2":265,"lightness":8,"accent_hue":42},
		"vars":{"a":false,"note":0},
		"nodes":[{"id":"pad","type":"dot","x":400,"y":400,"r":40,"hue":40,
			"on_click":[{"type":"toggle_var","name":"a"},
				{"type":"if","cond":{"var":"a","op":"truthy"},"then":[{"type":"add_var","name":"note","value":1}]}]}]
	}`)
	state := &OpenWorldState{
		Title: "instrument", Premise: "A playable star.",
		Variables: map[string]any{"a": false, "note": float64(0)},
		Experience: &worldcap.Experience{
			Version: 1,
			Root:    &worldcap.ViewNode{ID: "root", Capability: "core.custom", Props: program},
		},
	}
	if err := ValidateOpenWorldState(state); err != nil {
		t.Fatalf("state should validate: %v", err)
	}
	parsed, ok := worldcap.CustomProgramFor(state.Experience, "root")
	if !ok {
		t.Fatal("custom program not found")
	}
	applied, err := worldcap.ExecuteCustomEvent(parsed, worldcap.CustomEvent{
		Event: "click", NodeID: "pad",
	}, openWorldCapabilityWorld{state: state})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied = %#v", applied)
	}
	if state.Variables["a"] != true || state.Variables["note"] != float64(1) {
		t.Fatalf("variables = %#v", state.Variables)
	}
	if err := ValidateOpenWorldState(state); err != nil {
		t.Fatalf("state after custom action should validate: %v", err)
	}
}

func TestOpenWorldCustomActionInput(t *testing.T) {
	program := json.RawMessage(`{
		"background":{"mode":"solid","hue":220,"hue2":220,"lightness":10,"accent_hue":40},
		"vars":{"name":""},
		"nodes":[{"id":"field","type":"input","x":100,"y":100,"w":300,"h":40,"input":{"var":"name","type":"text"}}]
	}`)
	state := &OpenWorldState{
		Title: "form", Premise: "A form.",
		Variables: map[string]any{"name": ""},
		Experience: &worldcap.Experience{
			Version: 1,
			Root:    &worldcap.ViewNode{ID: "root", Capability: "core.custom", Props: program},
		},
	}
	parsed, _ := worldcap.CustomProgramFor(state.Experience, "root")
	_, err := worldcap.ExecuteCustomEvent(parsed, worldcap.CustomEvent{
		Event: "input", NodeID: "field", Value: "Ada",
	}, openWorldCapabilityWorld{state: state})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if state.Variables["name"] != "Ada" {
		t.Fatalf("name = %#v", state.Variables["name"])
	}
}

func TestCustomActionThroughObjectAction(t *testing.T) {
	program := json.RawMessage(`{
		"background":{"mode":"solid","hue":220,"hue2":220,"lightness":10,"accent_hue":40},
		"vars":{"a":false,"note":0},
		"nodes":[{"id":"pad","type":"dot","x":400,"y":400,"r":40,"hue":40,
			"on_click":[{"type":"toggle_var","name":"a"},
				{"type":"if","cond":{"var":"a","op":"truthy"},"then":[{"type":"add_var","name":"note","value":1}]}]}]
	}`)
	state := OpenWorldState{
		Title: "instrument", Premise: "A playable star.",
		Variables: map[string]any{"a": false, "note": float64(0)},
		Experience: &worldcap.Experience{
			Version: 1,
			Root:    &worldcap.ViewNode{ID: "root", Capability: "core.custom", Props: program},
		},
	}
	if err := ValidateOpenWorldState(&state); err != nil {
		t.Fatalf("state: %v", err)
	}
	stateRaw, _ := json.Marshal(state)
	input := json.RawMessage(`{"surface_id":"root","event":"click","node_id":"pad"}`)
	plan, err := PlanAction(TypeOpenWorld, "custom_action", input)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	updated, _, _, err := applyOpenWorldAction(stateRaw, json.RawMessage(`{}`), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var decoded OpenWorldState
	if err := json.Unmarshal(updated, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Variables["a"] != true || decoded.Variables["note"] != float64(1) {
		t.Fatalf("variables = %#v", decoded.Variables)
	}
	if len(plan.Effects) != 2 {
		t.Fatalf("effects = %#v", plan.Effects)
	}
}
