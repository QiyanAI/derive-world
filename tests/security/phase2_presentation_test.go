// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"net/http"
	"testing"
)

func TestOpenWorldPresentationConditionAndDerivation(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "p2lookowner")
	other := env.register(t, "p2lookother")

	status, body, _ := owner.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"object_type": "open_world",
			"title":       "A coast with a condition",
			"content": map[string]any{
				"premise": "The light answers only a high tide.",
				"presentation": map[string]any{
					"scene": map[string]any{
						"backdrop": "horizon", "sky_hue": 222, "ground_hue": 205,
						"horizon": 62, "accent_hue": 42, "ambient": "stars", "ambient_density": 40,
					},
					"link_style": "thread",
				},
				"kinds": []any{
					map[string]any{
						"name": "Lamp", "hue": 42,
						"fields": []any{map[string]any{"key": "brightness", "label": "Brightness", "type": "number"}},
						"appearance": map[string]any{
							"shape": "beacon", "size": 96, "motion": "flicker", "intensity": 55,
							"glow": 60, "label": "always", "layer": "front",
						},
						"bindings": []any{map[string]any{
							"field": "brightness", "target": "glow",
							"in_min": 0, "in_max": 10, "out_min": 0, "out_max": 100,
						}},
					},
					map[string]any{
						"name": "Water", "hue": 196,
						"fields": []any{map[string]any{"key": "level", "label": "Level", "type": "number"}},
						"appearance": map[string]any{
							"shape": "wave", "size": 130, "motion": "sway", "intensity": 45,
							"glow": 25, "label": "always", "layer": "back",
						},
						"bindings": []any{map[string]any{
							"field": "level", "target": "scale",
							"in_min": 0, "in_max": 10, "out_min": 0.7, "out_max": 1.5,
						}},
					},
				},
				"nodes": []any{
					map[string]any{"kind_index": 0, "name": "Lamp", "text": "", "values": map[string]any{"brightness": 3}},
					map[string]any{"kind_index": 1, "name": "Water", "text": "", "values": map[string]any{"level": 1}},
				},
				"links": []any{},
				"rules": []any{
					map[string]any{"trigger_index": 0, "effects": []any{
						map[string]any{"node_index": 1, "field": "level", "op": "add", "value": 2},
					}},
					map[string]any{
						"trigger_index": 0,
						"condition": map[string]any{
							"node_index": 1, "field": "level", "op": "gte", "value": 5,
						},
						"effects": []any{
							map[string]any{"node_index": 0, "field": "brightness", "op": "add", "value": 10},
						},
					},
				},
				"views": []any{"scene", "cards"},
			},
			"shared_context": map[string]any{},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", status, body)
	}
	world := decodeObject(t, body)
	if world.State["presentation"] == nil {
		t.Fatalf("presentation missing from state")
	}

	// First touch: the water rises to 3 but the >=5 guard stays closed.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action":           "touch_node",
			"input":            map[string]any{"node_id": nodeID(t, world, 0)},
			"expected_version": 1,
		},
	})
	if status != http.StatusOK {
		t.Fatalf("first touch status = %d body = %s", status, body)
	}
	first := decodeObject(t, body)
	if got := nodeValue(t, first, 1, "level"); got != float64(3) {
		t.Fatalf("level after first touch = %v, want 3", got)
	}
	if got := nodeValue(t, first, 0, "brightness"); got != float64(3) {
		t.Fatalf("brightness crossed a closed guard: %v", got)
	}

	// Second touch: the water reaches 5 and the same rule now runs.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action":           "touch_node",
			"input":            map[string]any{"node_id": nodeID(t, first, 0)},
			"expected_version": 2,
		},
	})
	if status != http.StatusOK {
		t.Fatalf("second touch status = %d body = %s", status, body)
	}
	second := decodeObject(t, body)
	if got := nodeValue(t, second, 1, "level"); got != float64(5) {
		t.Fatalf("level after second touch = %v, want 5", got)
	}
	if got := nodeValue(t, second, 0, "brightness"); got != float64(13) {
		t.Fatalf("guarded effect = %v, want 13", got)
	}

	// Change the world's own presentation as data.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action": "set_presentation",
			"input": map[string]any{
				"presentation": map[string]any{
					"scene": map[string]any{
						"backdrop": "void", "sky_hue": 268, "ground_hue": 232,
						"horizon": 50, "accent_hue": 190, "ambient": "motes", "ambient_density": 55,
					},
					"link_style": "orbit",
				},
			},
			"expected_version": 3,
		},
	})
	if status != http.StatusOK {
		t.Fatalf("set_presentation status = %d body = %s", status, body)
	}
	presented := decodeObject(t, body)
	presentation, _ := presented.State["presentation"].(map[string]any)
	scene, _ := presentation["scene"].(map[string]any)
	if scene["backdrop"] != "void" || presentation["link_style"] != "orbit" {
		t.Fatalf("presentation not persisted: %+v", presentation)
	}

	// A world's look is ownable state, not a shared global.
	status, _, _ = other.call(t, http.MethodGet, "/api/v1/objects/"+world.ID, callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("foreign read status = %d, want 404", status)
	}

	// Update one kind's appearance through the same action path.
	kinds, _ := presented.State["kinds"].([]any)
	firstKind, _ := kinds[0].(map[string]any)
	kindID, _ := firstKind["id"].(string)
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action": "update_kind",
			"input": map[string]any{
				"kind_id": kindID,
				"appearance": map[string]any{
					"shape": "ring", "size": 60, "motion": "pulse", "intensity": 70,
					"glow": 55, "label": "hover", "layer": "mid",
				},
			},
			"expected_version": 4,
		},
	})
	if status != http.StatusOK {
		t.Fatalf("update_kind status = %d body = %s", status, body)
	}
	updated := decodeObject(t, body)
	updatedKinds, _ := updated.State["kinds"].([]any)
	updatedFirst, _ := updatedKinds[0].(map[string]any)
	appearance, _ := updatedFirst["appearance"].(map[string]any)
	if appearance["shape"] != "ring" || appearance["motion"] != "pulse" {
		t.Fatalf("appearance not persisted: %+v", appearance)
	}

	// Invalid presentation is rejected before idempotency/version changes.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action": "set_presentation",
			"input": map[string]any{
				"presentation": map[string]any{
					"scene": map[string]any{
						"backdrop": "void", "sky_hue": 900, "ground_hue": 232,
						"horizon": 50, "accent_hue": 190, "ambient": "motes", "ambient_density": 55,
					},
					"link_style": "orbit",
				},
			},
			"expected_version": 5,
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("invalid presentation status = %d body = %s", status, body)
	}

	// Derive copies the experience as part of structure, with fresh identity.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/derive", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"title":              "Another coast",
			"source_version":     5,
			"inheritance_fields": []string{"premise", "structure", "views"},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("derive status = %d body = %s", status, body)
	}
	child := decodeObject(t, body)
	childPresentation, _ := child.State["presentation"].(map[string]any)
	childScene, _ := childPresentation["scene"].(map[string]any)
	if childScene["backdrop"] != "void" || childPresentation["link_style"] != "orbit" {
		t.Fatalf("child presentation not inherited: %+v", childPresentation)
	}
	childKinds, _ := child.State["kinds"].([]any)
	childFirst, _ := childKinds[0].(map[string]any)
	childAppearance, _ := childFirst["appearance"].(map[string]any)
	if childAppearance["shape"] != "ring" {
		t.Fatalf("child appearance not inherited: %+v", childAppearance)
	}
	if childFirst["id"] == firstKind["id"] {
		t.Fatalf("child reused parent kind id")
	}
	if child.Lineage == nil || child.Lineage.Parent.ObjectID != world.ID {
		t.Fatalf("child lineage = %+v", child.Lineage)
	}
}
