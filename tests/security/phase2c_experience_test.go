// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestOpenWorldCapabilityExperience(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "p2cowner")
	other := env.register(t, "p2cother")

	status, body, _ := owner.call(t, http.MethodGet, "/api/v1/world-capabilities", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("catalog status = %d body = %s", status, body)
	}
	var catalog struct {
		Items []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatalf("catalog decode: %v", err)
	}
	seen := map[string]string{}
	for _, item := range catalog.Items {
		seen[item.ID] = item.Kind
	}
	if seen["core.meter"] != "presentation" || seen["core.transfer"] != "behavior" {
		t.Fatalf("catalog is missing composed capabilities: %+v", seen)
	}

	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"object_type": "open_world",
			"title":       "A world with a composed experience",
			"content": map[string]any{
				"premise": "A light can be raised.",
				"kinds": []any{
					map[string]any{
						"name": "Lamp", "hue": 42,
						"fields": []any{
							map[string]any{"key": "brightness", "label": "Brightness", "type": "number"},
						},
					},
				},
				"nodes": []any{
					map[string]any{"kind_index": 0, "name": "North lamp", "text": "It answers.", "values": map[string]any{"brightness": 1}},
					map[string]any{"kind_index": 0, "name": "South lamp", "text": "It waits.", "values": map[string]any{"brightness": 3}},
				},
				"links": []any{},
				"rules": []any{},
			},
			"shared_context": map[string]any{},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", status, body)
	}
	world := decodeObject(t, body)
	lampID := nodeID(t, world, 0)
	otherLampID := nodeID(t, world, 1)

	experience := map[string]any{
		"protocol": 1,
		"root": map[string]any{
			"id": "root", "capability": "core.stack",
			"props": map[string]any{"direction": "vertical", "gap": 12, "padding": 0, "align": "stretch", "tone": "panel"},
			"children": []any{
				map[string]any{
					"id": "title", "capability": "core.text",
					"props": map[string]any{"text": "{{world.title}}", "size": 24, "weight": "bold", "tone": "default", "align": "left"},
				},
				map[string]any{
					"id": "raise", "capability": "core.button",
					"props": map[string]any{"label": "Raise", "behavior_id": "raise-light", "tone": "primary"},
				},
			},
		},
		"behaviors": []any{
			map[string]any{
				"id": "raise-light", "name": "Raise the light",
				"actions": []any{
					map[string]any{
						"capability": "core.state", "action": "add",
						"params": map[string]any{"entity_id": lampID, "field": "brightness", "value": 2},
					},
					map[string]any{
						"capability": "core.transfer", "action": "move",
						"params": map[string]any{
							"from_entity": otherLampID, "to_entity": lampID,
							"field": "brightness", "amount": 1,
						},
					},
				},
			},
		},
	}

	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action":           "set_experience",
			"input":            map[string]any{"experience": experience},
			"expected_version": 1,
		},
	})
	if status != http.StatusOK {
		t.Fatalf("set_experience status = %d body = %s", status, body)
	}
	composed := decodeObject(t, body)
	stored, _ := composed.State["experience"].(map[string]any)
	if stored["protocol"] != float64(1) || stored["root"] == nil {
		t.Fatalf("experience not persisted: %+v", stored)
	}

	// Foreign users cannot read or invoke the world.
	status, _, _ = other.call(t, http.MethodGet, "/api/v1/objects/"+world.ID, callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("foreign read status = %d, want 404", status)
	}
	status, _, _ = other.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action":           "invoke_behavior",
			"input":            map[string]any{"behavior_id": "raise-light", "entity_id": lampID},
			"expected_version": 2,
		},
	})
	if status != http.StatusNotFound {
		t.Fatalf("foreign action status = %d, want 404", status)
	}

	// A real invocation runs both the state and the newly registered transfer
	// capability in one bounded pass.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action":           "invoke_behavior",
			"input":            map[string]any{"behavior_id": "raise-light", "entity_id": lampID},
			"expected_version": 2,
		},
	})
	if status != http.StatusOK {
		t.Fatalf("invoke_behavior status = %d body = %s", status, body)
	}
	invoked := decodeObject(t, body)
	if got := nodeValue(t, invoked, 0, "brightness"); got != float64(4) {
		t.Fatalf("target brightness = %v, want 4", got)
	}
	if got := nodeValue(t, invoked, 1, "brightness"); got != float64(2) {
		t.Fatalf("source brightness = %v, want 2", got)
	}

	// The HTTP actor-override guard rejects a nested "version" key even inside
	// an experience document. The wire protocol is named "protocol" so this
	// guard can stay unconditional.
	nestedVersion := map[string]any{
		"protocol": 1,
		"version":  1,
		"root": map[string]any{
			"id": "root", "capability": "core.text",
			"props": map[string]any{"text": "x", "size": 18, "weight": "regular", "tone": "default", "align": "left"},
		},
	}
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action":           "set_experience",
			"input":            map[string]any{"experience": nestedVersion},
			"expected_version": 3,
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("nested version status = %d body = %s", status, body)
	}

	// Invalid experience references are rejected before persistence.
	invalid := map[string]any{
		"protocol": 1,
		"root": map[string]any{
			"id": "root", "capability": "core.button",
			"props": map[string]any{"label": "Missing", "behavior_id": "ghost", "tone": "primary"},
		},
	}
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action":           "set_experience",
			"input":            map[string]any{"experience": invalid},
			"expected_version": 3,
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("invalid experience status = %d body = %s", status, body)
	}

	// Derive with structure; experience references must be remapped to child ids.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/derive", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"title":              "A derived composed world",
			"source_version":     3,
			"inheritance_fields": []string{"premise", "structure", "views"},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("derive status = %d body = %s", status, body)
	}
	child := decodeObject(t, body)
	childExperience, _ := child.State["experience"].(map[string]any)
	if childExperience["root"] == nil {
		t.Fatalf("child experience missing")
	}
	childLampID := nodeID(t, child, 0)
	encoded, err := json.Marshal(childExperience)
	if err != nil {
		t.Fatalf("marshal child experience: %v", err)
	}
	if !bytes.Contains(encoded, []byte(childLampID)) || bytes.Contains(encoded, []byte(lampID)) {
		t.Fatalf("child experience was not remapped: %s", encoded)
	}
	if child.Lineage == nil || child.Lineage.Parent.ObjectID != world.ID {
		t.Fatalf("child lineage = %+v", child.Lineage)
	}
}
