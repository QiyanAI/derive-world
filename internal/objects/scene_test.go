// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestValidateCreateLivingScene(t *testing.T) {
	state, shared, err := ValidateCreate(CreateInput{
		ObjectType: TypeLivingScene,
		Title:      "Launch plan",
		Content: json.RawMessage(`{
			"purpose":"project",
			"premise":"Ship one useful thing.",
			"elements":[
				{"kind":"idea","text":"Find a real problem."},
				{"kind":"step","text":"Interview users."}
			],
			"tags":["b","a","a"]
		}`),
	})
	if err != nil {
		t.Fatalf("validate create: %v", err)
	}
	if string(shared) != `{}` {
		t.Fatalf("shared context = %s, want {}", shared)
	}
	var scene LivingSceneState
	if err := json.Unmarshal(state, &scene); err != nil {
		t.Fatalf("decode scene: %v", err)
	}
	if scene.Title != "Launch plan" || scene.Purpose != ScenePurposeProject {
		t.Fatalf("scene header = %+v", scene)
	}
	if len(scene.Elements) != 2 || scene.Elements[0].Status != SceneStatusOpen {
		t.Fatalf("elements = %+v", scene.Elements)
	}
	if strings.Join(scene.Tags, ",") != "a,b" {
		t.Fatalf("tags = %v, want sorted unique", scene.Tags)
	}
}

func TestLivingSceneActions(t *testing.T) {
	raw, err := json.Marshal(LivingSceneState{
		Title:   "Story",
		Purpose: ScenePurposeStory,
		Premise: "A door opens.",
		Elements: []SceneElement{
			{ID: mustV7(t), Kind: SceneKindScene, Text: "The door opens.", Status: SceneStatusOpen},
		},
		Tags: []string{},
	})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	plan, err := PlanAction(TypeLivingScene, "add_element",
		json.RawMessage(`{"kind":"choice","text":"Walk through."}`))
	if err != nil {
		t.Fatalf("plan add: %v", err)
	}
	raw, _, _, err = ApplyStateAction(TypeLivingScene, raw, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatalf("apply add: %v", err)
	}
	var state LivingSceneState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if len(state.Elements) != 2 || state.Elements[1].Kind != SceneKindChoice {
		t.Fatalf("elements after add = %+v", state.Elements)
	}
	elementID := state.Elements[1].ID

	plan, err = PlanAction(TypeLivingScene, "update_element",
		json.RawMessage(`{"element_id":"`+elementID+`","status":"done"}`))
	if err != nil {
		t.Fatalf("plan update: %v", err)
	}
	raw, _, _, err = ApplyStateAction(TypeLivingScene, raw, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatalf("apply update: %v", err)
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode updated state: %v", err)
	}
	if state.Elements[1].Status != SceneStatusDone {
		t.Fatalf("element status = %s", state.Elements[1].Status)
	}

	plan, err = PlanAction(TypeLivingScene, "move_element",
		json.RawMessage(`{"element_id":"`+elementID+`","position":0}`))
	if err != nil {
		t.Fatalf("plan move: %v", err)
	}
	raw, _, _, err = ApplyStateAction(TypeLivingScene, raw, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatalf("apply move: %v", err)
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode moved state: %v", err)
	}
	if state.Elements[0].ID != elementID {
		t.Fatalf("move did not place element first: %+v", state.Elements)
	}

	plan, err = PlanAction(TypeLivingScene, "remove_element",
		json.RawMessage(`{"element_id":"`+elementID+`"}`))
	if err != nil {
		t.Fatalf("plan remove: %v", err)
	}
	raw, _, _, err = ApplyStateAction(TypeLivingScene, raw, json.RawMessage(`{}`), plan, "")
	if err != nil {
		t.Fatalf("apply remove: %v", err)
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode removed state: %v", err)
	}
	if len(state.Elements) != 1 {
		t.Fatalf("elements after remove = %d", len(state.Elements))
	}
}

func TestLivingSceneRejectsInvalidInput(t *testing.T) {
	if _, _, err := ValidateCreate(CreateInput{
		ObjectType: TypeLivingScene,
		Title:      "Bad",
		Content:    json.RawMessage(`{"purpose":"other"}`),
	}); err == nil {
		t.Fatal("invalid purpose was accepted")
	}
	if _, err := PlanAction(TypeLivingScene, "add_element",
		json.RawMessage(`{"kind":"message","text":"x"}`)); err == nil {
		t.Fatal("invalid kind was accepted")
	}
	if _, err := PlanAction(TypeLivingScene, "move_element",
		json.RawMessage(`{"element_id":"`+mustV7(t)+`","position":-1}`)); err == nil {
		t.Fatal("negative position was accepted")
	}
}

func mustV7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("new uuid: %v", err)
	}
	return id.String()
}
