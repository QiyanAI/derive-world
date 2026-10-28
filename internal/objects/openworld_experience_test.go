// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/QiyanAI/derive-world/internal/worldcap"
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

func TestOpenWorldCreateWithExperience(t *testing.T) {
	content := openWorldCreateContent(t)
	content.Experience = &worldcap.Experience{
		Version: 1,
		Root: &worldcap.ViewNode{
			ID: "root", Capability: "core.text",
			Props: json.RawMessage(`{"text":"{{world.title}}","size":18,"weight":"regular","tone":"default","align":"left"}`),
		},
	}
	state := createOpenWorldState(t, content)
	if state.Experience == nil || state.Experience.Root == nil {
		t.Fatalf("create path dropped the experience")
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

func TestOpenWorldBehaviorRunsOneRulePass(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	content := map[string]any{
		"protocol": 1,
		"root": map[string]any{
			"id": "root", "capability": "core.button",
			"props": map[string]any{"label": "Calm", "behavior_id": "calm", "tone": "primary"},
		},
		"behaviors": []any{map[string]any{
			"id": "calm", "name": "Calm",
			"actions": []any{map[string]any{
				"capability": "core.state", "action": "set",
				"params": map[string]any{"entity_id": state.Nodes[0].ID, "field": "mood", "value": "calm"},
			}},
		}},
	}
	withExperience := applyOpenWorldPlan(t, state, "set_experience", map[string]any{"experience": content})
	planRaw := experienceTestJSON(t, map[string]any{
		"behavior_id": "calm", "entity_id": state.Nodes[0].ID,
	})
	plan, err := PlanAction(TypeOpenWorld, "invoke_behavior", planRaw)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	stateRaw := experienceTestJSON(t, withExperience)
	newRaw, _, _, err := ApplyStateAction(TypeOpenWorld, stateRaw, []byte(`{}`), plan, "")
	if err != nil {
		t.Fatalf("invoke behavior: %v", err)
	}
	var next OpenWorldState
	if err := json.Unmarshal(newRaw, &next); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if next.Nodes[0].Values["mood"] != "calm" {
		t.Fatalf("behavior did not set mood: %v", next.Nodes[0].Values)
	}
	if next.Nodes[1].Values["light"] != float64(1) {
		t.Fatalf("legacy rule did not run after behavior: %v", next.Nodes[1].Values)
	}
	if len(plan.Effects) < 2 {
		t.Fatalf("behavior + rule effects = %+v", plan.Effects)
	}
}

func TestOpenWorldExperienceBlocksEventFieldRemoval(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	content := map[string]any{
		"protocol": 1,
		"root": map[string]any{
			"id": "root", "capability": "core.text",
			"props": map[string]any{"text": "{{world.title}}", "size": 18, "weight": "regular", "tone": "default", "align": "left"},
		},
		"behaviors": []any{map[string]any{
			"id": "set-mood", "name": "Set mood",
			"actions": []any{map[string]any{
				"capability": "core.state", "action": "set",
				"params": map[string]any{
					"entity_id": "$event.entity", "field": "mood", "value": "calm",
				},
			}},
		}},
	}
	withExperience := applyOpenWorldPlan(t, state, "set_experience", map[string]any{"experience": content})
	planRaw := experienceTestJSON(t, map[string]any{
		"kind_id": state.Kinds[0].ID,
		"fields":  []any{},
	})
	plan, err := PlanAction(TypeOpenWorld, "update_kind", planRaw)
	if err != nil {
		t.Fatalf("plan update_kind: %v", err)
	}
	stateRaw := experienceTestJSON(t, withExperience)
	if _, _, _, err := ApplyStateAction(TypeOpenWorld, stateRaw, []byte(`{}`), plan, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("remove event-scoped field error = %v, want ErrConflict", err)
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
	if _, _, _, err := ApplyStateAction(TypeOpenWorld, stateRaw, []byte(`{}`), plan, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("remove referenced entity error = %v, want ErrConflict", err)
	}
}

func TestOpenWorldExperienceBlocksReferencedKindRemoval(t *testing.T) {
	state := createOpenWorldState(t, openWorldCreateContent(t))
	content := map[string]any{
		"protocol": 1,
		"root": map[string]any{
			"id": "root", "capability": "core.collection",
			"props": map[string]any{"kind_id": state.Kinds[0].ID, "layout": "list", "columns": 1, "empty_text": ""},
			"children": []any{
				map[string]any{
					"id": "row", "capability": "core.text",
					"props": map[string]any{"text": "{{item.name}}", "size": 18, "weight": "regular", "tone": "default", "align": "left"},
				},
			},
		},
	}
	next := applyOpenWorldPlan(t, state, "set_experience", map[string]any{"experience": content})
	for len(next.Nodes) > 0 {
		next = applyOpenWorldPlan(t, next, "remove_node", map[string]any{"node_id": next.Nodes[0].ID})
	}
	planRaw := experienceTestJSON(t, map[string]any{"kind_id": next.Kinds[0].ID})
	plan, err := PlanAction(TypeOpenWorld, "remove_kind", planRaw)
	if err != nil {
		t.Fatalf("plan remove_kind: %v", err)
	}
	stateRaw := experienceTestJSON(t, next)
	if _, _, _, err := ApplyStateAction(TypeOpenWorld, stateRaw, []byte(`{}`), plan, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("remove referenced kind error = %v, want ErrConflict", err)
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
