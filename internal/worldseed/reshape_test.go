// SPDX-License-Identifier: Apache-2.0
package worldseed

import (
	"encoding/json"
	"testing"

	"github.com/QiyanAI/derive-world/internal/objects"
)

func composeState(t *testing.T, raw string, title string) objects.OpenWorldState {
	t.Helper()
	brief := decodeBrief(t, raw)
	composition, err := Compose(brief, title)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	encoded, err := json.Marshal(composition.Content)
	if err != nil {
		t.Fatal(err)
	}
	stateRaw, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld, Title: title, Content: encoded,
	})
	if err != nil {
		t.Fatalf("validate create: %v", err)
	}
	var state objects.OpenWorldState
	if err := json.Unmarshal(stateRaw, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

// A reshape that only changes content must never rewrite the world's rules,
// views or tags. These belong to the world, not to the surface being rebuilt.
func TestReshapePreservesWorldMetadata(t *testing.T) {
	state := composeState(t, `{
		"premise":"A small workshop with one bench.",
		"kinds":[{"name":"Bench","fields":[{"label":"Count","type":"number"},{"label":"Note","type":"text"}]}],
		"things":[{"kind":0,"name":"The bench","values":{"Count":2,"Note":"sturdy"}}]
	}`, "Workshop")
	state.Tags = []string{"calm", "handmade"}
	state.Views = []string{objects.OpenWorldViewScene, objects.OpenWorldViewCards}
	current, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	brief := decodeBrief(t, `{
		"premise":"A small workshop with one bench and a shelf.",
		"kinds":[{"name":"Shelf"}],
		"things":[{"kind":0,"name":"A high shelf"}]
	}`)
	result, err := Reshape(current, brief)
	if err != nil {
		t.Fatalf("reshape: %v", err)
	}
	if len(result.State.Tags) != 2 || result.State.Tags[0] != "calm" {
		t.Fatalf("tags lost: %#v", result.State.Tags)
	}
	if len(result.State.Views) != 2 || result.State.Views[1] != objects.OpenWorldViewCards {
		t.Fatalf("views lost: %#v", result.State.Views)
	}
	if len(result.State.Nodes) != 2 {
		t.Fatalf("content merge failed: %d nodes", len(result.State.Nodes))
	}
}

// Re-declaring fields in a different order must write values to the field with
// the matching label, not to the field with the matching position.
func TestReshapeMapsFieldValuesByLabel(t *testing.T) {
	state := composeState(t, `{
		"premise":"A garden with one tree.",
		"kinds":[{"name":"Tree","fields":[{"label":"Count","type":"number"},{"label":"Note","type":"text"}]}],
		"things":[{"kind":0,"name":"The tree","values":{"Count":2,"Note":"old"}}]
	}`, "Garden")
	current, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	brief := decodeBrief(t, `{
		"premise":"A garden with one taller tree.",
		"kinds":[{"name":"Tree","fields":[{"label":"Note","type":"text"},{"label":"Count","type":"number"}]}],
		"things":[{"kind":0,"name":"The tree","values":{"Note":"very old","Count":9}}]
	}`)
	result, err := Reshape(current, brief)
	if err != nil {
		t.Fatalf("reshape: %v", err)
	}
	kind := result.State.Kinds[0]
	fieldKeys := map[string]string{}
	for _, field := range kind.Fields {
		fieldKeys[field.Label] = field.Key
	}
	node := result.State.Nodes[0]
	if node.Values[fieldKeys["Note"]] != "very old" {
		t.Fatalf("Note written to wrong field: %#v", node.Values)
	}
	if node.Values[fieldKeys["Count"]] != float64(9) {
		t.Fatalf("Count written to wrong field: %#v", node.Values)
	}
}

// Rules are part of the world, not of a surface. The authored round-trip must
// carry them through unchanged so a reshape cannot silently delete behavior.
func TestAuthoredStateRoundTripPreservesRules(t *testing.T) {
	state := objects.OpenWorldState{
		Title: "Keep rules", Premise: "A world with one rule.",
		Kinds: []objects.OpenWorldKind{{ID: "k1", Name: "Beacon"}},
		Nodes: []objects.OpenWorldNode{{ID: "n1", KindID: "k1", Name: "Beacon"}},
		Rules: []objects.OpenWorldRule{{
			ID: "rule-1", TriggerID: "behavior-1", Effects: []objects.OpenWorldEffect{},
		}},
		Views: []string{objects.OpenWorldViewScene, objects.OpenWorldViewStage},
		Tags:  []string{"kept"},
	}
	world := authoredFromState(&state)
	restored := world.toState(nil)
	if len(restored.Rules) != 1 || restored.Rules[0].ID != "rule-1" {
		t.Fatalf("rules lost in round trip: %#v", restored.Rules)
	}
	if len(restored.Views) != 2 || restored.Views[1] != objects.OpenWorldViewStage {
		t.Fatalf("views lost in round trip: %#v", restored.Views)
	}
	if len(restored.Tags) != 1 || restored.Tags[0] != "kept" {
		t.Fatalf("tags lost in round trip: %#v", restored.Tags)
	}
}

// The space surface rejects duplicate binding targets. Two number fields plus a
// flag must still produce three unique targets.
func TestExperienceBindingsUseUniqueTargets(t *testing.T) {
	kind := &authoredKind{Fields: []objects.OpenWorldField{
		{Key: "f0", Label: "Count", Type: objects.OpenWorldFieldNumber},
		{Key: "f1", Label: "Weight", Type: objects.OpenWorldFieldNumber},
		{Key: "f2", Label: "Done", Type: objects.OpenWorldFieldFlag},
	}}
	bindings := experienceBindingsFromKind(kind)
	if len(bindings) != 3 {
		t.Fatalf("bindings = %#v", bindings)
	}
	seen := map[string]bool{}
	for _, binding := range bindings {
		target, _ := binding["target"].(string)
		if target == "" || seen[target] {
			t.Fatalf("duplicate or empty target: %#v", bindings)
		}
		seen[target] = true
	}
}
