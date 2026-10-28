// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"strings"
	"testing"
)

func experienceContent(state *OpenWorldState) map[string]any {
	return map[string]any{
		"protocol": 1,
		"root": map[string]any{
			"id": "root", "capability": "core.stack",
			"props": map[string]any{"direction": "vertical", "gap": 12, "padding": 0, "align": "stretch", "tone": "plain"},
			"children": []any{
				map[string]any{
					"id": "title", "capability": "core.text",
					"props": map[string]any{"text": "{{world.title}}", "size": 32, "weight": "bold", "tone": "default", "align": "left"},
				},
				map[string]any{
					"id": "light", "capability": "core.button",
					"props": map[string]any{"label": "Add light", "behavior_id": "add-light", "tone": "primary"},
				},
			},
		},
		"behaviors": []any{
			map[string]any{
				"id": "add-light", "name": "Add light",
				"actions": []any{
					map[string]any{
						"capability": "core.state", "action": "add",
						"params": map[string]any{
							"entity_id": state.Nodes[1].ID, "field": "light", "value": float64(2),
						},
					},
				},
			},
		},
	}
}

func TestOpenWorldSetExperienceAndInvokeBehavior(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	next := applyOpenWorldPlan(t, state, "set_experience", map[string]any{
		"experience": experienceContent(state),
	})
	if next.Experience == nil || next.Experience.Root == nil {
		t.Fatalf("experience not stored")
	}
	if next.Experience.Behaviors[0].ID != "add-light" {
		t.Fatalf("behavior id not preserved: %+v", next.Experience.Behaviors)
	}
	if !strings.Contains(string(next.Experience.Root.Children[0].Props), "world.title") {
		t.Fatalf("text props not normalized: %s", next.Experience.Root.Children[0].Props)
	}

	planRaw := experienceTestJSON(t, map[string]any{
		"behavior_id": "add-light", "entity_id": state.Nodes[0].ID,
	})
	plan, err := PlanAction(TypeOpenWorld, "invoke_behavior", planRaw)
	if err != nil {
		t.Fatalf("plan invoke: %v", err)
	}
	stateRaw := experienceTestJSON(t, next)
	newRaw, _, _, err := ApplyStateAction(TypeOpenWorld, stateRaw, []byte(`{}`), plan, "")
	if err != nil {
		t.Fatalf("invoke behavior: %v", err)
	}
	var invoked OpenWorldState
	if err := json.Unmarshal(newRaw, &invoked); err != nil {
		t.Fatalf("decode: %v", err)
	}
	light, ok := invoked.Nodes[1].Values["light"].(float64)
	if !ok || light != 2 {
		t.Fatalf("light = %v, want 2", invoked.Nodes[1].Values["light"])
	}
	if len(plan.Effects) != 1 || plan.Effects[0].Value != float64(2) {
		t.Fatalf("applied effects = %+v", plan.Effects)
	}
}

func TestOpenWorldExperienceBlocksReferencedRemoval(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	next := applyOpenWorldPlan(t, state, "set_experience", map[string]any{
		"experience": experienceContent(state),
	})
	planRaw := experienceTestJSON(t, map[string]any{"node_id": next.Nodes[1].ID})
	plan, err := PlanAction(TypeOpenWorld, "remove_node", planRaw)
	if err != nil {
		t.Fatalf("plan remove: %v", err)
	}
	stateRaw := experienceTestJSON(t, next)
	if _, _, _, err := ApplyStateAction(TypeOpenWorld, stateRaw, []byte(`{}`), plan, ""); err == nil {
		t.Fatalf("removed an entity referenced by the experience")
	}
}

func TestOpenWorldDeriveRemapsExperience(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	parent := applyOpenWorldPlan(t, state, "set_experience", map[string]any{
		"experience": experienceContent(state),
	})
	child, err := OpenWorldDerivedState("A different city", parent, map[string]bool{
		"premise": true, "structure": true, "views": true, "tags": true,
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if child.Experience == nil || child.Experience.Root == nil {
		t.Fatalf("experience not inherited")
	}
	if child.Experience.Root.ID != "root" || child.Experience.Behaviors[0].ID != "add-light" {
		t.Fatalf("local ids changed: %+v", child.Experience)
	}
	params := string(child.Experience.Behaviors[0].Actions[0].Params)
	if strings.Contains(params, parent.Nodes[1].ID) {
		t.Fatalf("parent entity id survived: %s", params)
	}
	if !strings.Contains(params, child.Nodes[1].ID) {
		t.Fatalf("child entity id missing: %s", params)
	}
	if child.Nodes[1].ID == parent.Nodes[1].ID {
		t.Fatalf("derived entity reused parent id")
	}
}

func TestOpenWorldExperienceCanBeCleared(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	withExperience := applyOpenWorldPlan(t, state, "set_experience", map[string]any{
		"experience": experienceContent(state),
	})
	cleared := applyOpenWorldPlan(t, withExperience, "set_experience", map[string]any{
		"experience": nil,
	})
	if cleared.Experience != nil {
		t.Fatalf("experience was not cleared: %+v", cleared.Experience)
	}
}

func TestOpenWorldRejectsInvalidExperience(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	content := experienceContent(state)
	content["behaviors"] = []any{map[string]any{
		"id": "bad", "actions": []any{map[string]any{
			"capability": "core.state", "action": "add",
			"params": map[string]any{
				"entity_id": "01a00000-0000-7000-8000-000000000099", "field": "light", "value": 1,
			},
		}},
	}}
	planRaw := experienceTestJSON(t, map[string]any{"experience": content})
	plan, err := PlanAction(TypeOpenWorld, "set_experience", planRaw)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	stateRaw := experienceTestJSON(t, state)
	if _, _, _, err := ApplyStateAction(TypeOpenWorld, stateRaw, []byte(`{}`), plan, ""); err == nil {
		t.Fatalf("invalid experience accepted")
	}
}

func TestOpenWorldExperienceActionsRequireContribute(t *testing.T) {
	for _, action := range []string{"set_experience", "invoke_behavior"} {
		permission, ok := RequiredPermission(TypeOpenWorld, action)
		if !ok || permission != PermContribute {
			t.Fatalf("%s permission = %q, %v", action, permission, ok)
		}
	}
}

func experienceTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := marshalState(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return encoded
}
