// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"testing"
)

func TestReshapeWorldAction(t *testing.T) {
	state := OpenWorldState{
		Title: "Garden", Premise: "A glass garden.",
		Kinds: []OpenWorldKind{{ID: "01a00000-0000-7000-8000-000000000001", Name: "Flower", Hue: 190}},
		Nodes: []OpenWorldNode{{
			ID: "01a00000-0000-7000-8000-000000000002", KindID: "01a00000-0000-7000-8000-000000000001",
			Name: "First flower", Status: OpenWorldStatusOpen, Values: map[string]any{}, X: 500, Y: 500,
		}},
		Views: OpenWorldAvailableViews(), Presentation: DefaultOpenWorldPresentation(),
	}
	if err := ValidateOpenWorldState(&state); err != nil {
		t.Fatalf("initial: %v", err)
	}
	replacement := state
	replacement.Premise = "A glass garden that can be walked."
	replacement.Variables = map[string]any{"storm": false}
	content, _ := json.Marshal(replacement)
	input, _ := json.Marshal(map[string]any{"note": "Make the garden walkable", "content": json.RawMessage(content)})
	plan, err := PlanAction(TypeOpenWorld, "reshape_world", input)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	stateRaw, _ := json.Marshal(state)
	updated, _, _, err := applyOpenWorldAction(stateRaw, json.RawMessage(`{}`), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var decoded OpenWorldState
	if err := json.Unmarshal(updated, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Premise != replacement.Premise || decoded.Variables["storm"] != false {
		t.Fatalf("reshape did not apply: %#v", decoded)
	}
	if plan.EventInput == nil || plan.EventInput["note"] != "Make the garden walkable" {
		t.Fatalf("event input summary missing: %#v", plan.EventInput)
	}
}

func TestReshapeWorldPreservesTitle(t *testing.T) {
	state := OpenWorldState{
		Title: "Garden", Premise: "A glass garden.",
		Views: OpenWorldAvailableViews(), Presentation: DefaultOpenWorldPresentation(),
	}
	replacement := state
	replacement.Title = "Hijacked"
	content, _ := json.Marshal(replacement)
	input, _ := json.Marshal(map[string]any{"content": json.RawMessage(content)})
	plan, err := PlanAction(TypeOpenWorld, "reshape_world", input)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	stateRaw, _ := json.Marshal(state)
	updated, _, _, err := applyOpenWorldAction(stateRaw, json.RawMessage(`{}`), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var decoded OpenWorldState
	_ = json.Unmarshal(updated, &decoded)
	if decoded.Title != "Garden" {
		t.Fatalf("title changed: %q", decoded.Title)
	}
}
