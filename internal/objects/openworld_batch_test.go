// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"testing"
)

func batchTestState(t *testing.T) (OpenWorldState, json.RawMessage) {
	t.Helper()
	state := OpenWorldState{
		Title: "Garden", Premise: "A glass garden.",
		Kinds: []OpenWorldKind{{
			ID: "01a00000-0000-7000-8000-000000000001", Name: "Flower", Hue: 190,
		}},
		Nodes: []OpenWorldNode{{
			ID:     "01a00000-0000-7000-8000-000000000002",
			KindID: "01a00000-0000-7000-8000-000000000001",
			Name:   "First flower", Status: OpenWorldStatusOpen,
			Values: map[string]any{}, X: 500, Y: 500,
		}},
		Views: OpenWorldAvailableViews(), Presentation: DefaultOpenWorldPresentation(),
	}
	if err := ValidateOpenWorldState(&state); err != nil {
		t.Fatalf("initial state: %v", err)
	}
	encoded, _ := json.Marshal(state)
	return state, encoded
}

// One batch is one atomic state change: four compatible edits produce one
// version increment in the caller, not four competing proposals.
func TestBatchChangeAppliesCompatibleEdits(t *testing.T) {
	_, stateRaw := batchTestState(t)
	kindID := "01a00000-0000-7000-8000-000000000001"
	input, _ := json.Marshal(map[string]any{
		"note": "Add two flowers and rename the garden",
		"actions": []any{
			map[string]any{"action": "set_title", "input": map[string]any{"title": "New Garden"}},
			map[string]any{"action": "set_premise", "input": map[string]any{"premise": "A garden that grew."}},
			map[string]any{"action": "add_node", "input": map[string]any{"kind_id": kindID, "name": "Second flower"}},
			map[string]any{"action": "add_node", "input": map[string]any{"kind_id": kindID, "name": "Third flower"}},
		},
	})
	plan, err := PlanAction(TypeOpenWorld, "batch_change", input)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Batch) != 4 {
		t.Fatalf("batch plans = %d, want 4", len(plan.Batch))
	}
	if !ReferencesExist(TypeOpenWorld, stateRaw, plan) {
		t.Fatal("a batch that only references existing kinds was rejected")
	}
	updated, _, _, err := applyOpenWorldAction(stateRaw, json.RawMessage(`{}`), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var decoded OpenWorldState
	if err := json.Unmarshal(updated, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Title != "New Garden" || decoded.Premise != "A garden that grew." {
		t.Fatalf("batch did not apply title/premise: %#v", decoded)
	}
	if len(decoded.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(decoded.Nodes))
	}
	if decoded.Nodes[0].ID != "01a00000-0000-7000-8000-000000000002" {
		t.Fatal("the existing node id was not retained")
	}
	if plan.EventInput == nil || plan.EventInput["action_count"] != 4 {
		t.Fatalf("batch event summary = %#v", plan.EventInput)
	}
}

func TestBatchChangeRejectsNonComposableActions(t *testing.T) {
	cases := map[string]map[string]any{
		"nested batch":   {"actions": []any{map[string]any{"action": "batch_change", "input": map[string]any{"actions": []any{}}}}},
		"whole reshape":  {"actions": []any{map[string]any{"action": "reshape_world", "input": map[string]any{"content": map[string]any{}}}}},
		"custom program": {"actions": []any{map[string]any{"action": "custom_action", "input": map[string]any{"surface_id": "x", "event": "click"}}}},
		"empty actions":  {"actions": []any{}},
	}
	for name, input := range cases {
		raw, _ := json.Marshal(input)
		if _, err := PlanAction(TypeOpenWorld, "batch_change", raw); err == nil {
			t.Fatalf("%s was accepted by the batch planner", name)
		}
	}
}

func TestBatchChangeReferencesAreCheckedSequentially(t *testing.T) {
	_, stateRaw := batchTestState(t)
	input, _ := json.Marshal(map[string]any{
		"actions": []any{
			map[string]any{"action": "add_node", "input": map[string]any{
				"kind_id": "01a00000-0000-7000-8000-0000000000ff", "name": "Missing kind",
			}},
		},
	})
	plan, err := PlanAction(TypeOpenWorld, "batch_change", input)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if ReferencesExist(TypeOpenWorld, stateRaw, plan) {
		t.Fatal("a batch referencing a missing kind was accepted")
	}
}
