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
	// capability in one bounded pass. The same idempotency key must replay the
	// first result instead of applying transfer twice.
	invokeKey := newIdempotencyKey()
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: invokeKey,
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
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: invokeKey,
		Body: map[string]any{
			"action":           "invoke_behavior",
			"input":            map[string]any{"behavior_id": "raise-light", "entity_id": lampID},
			"expected_version": 2,
		},
	})
	if status != http.StatusOK {
		t.Fatalf("replayed invoke status = %d body = %s", status, body)
	}
	replayed := decodeObject(t, body)
	if replayed.Version != invoked.Version {
		t.Fatalf("replay version = %d, want %d", replayed.Version, invoked.Version)
	}
	if got := nodeValue(t, replayed, 0, "brightness"); got != float64(4) {
		t.Fatalf("replay applied transfer twice: %v", got)
	}

	// The event log explains the behavior and its applied effects.
	status, body, _ = owner.call(t, http.MethodGet,
		"/api/v1/objects/"+world.ID+"/events?limit=20", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("events status = %d body = %s", status, body)
	}
	var events struct {
		Items []struct {
			EventType string         `json:"event_type"`
			Payload   map[string]any `json:"payload"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &events); err != nil {
		t.Fatalf("events decode: %v", err)
	}
	foundBehaviorEvent := false
	for _, event := range events.Items {
		if event.EventType != "object.state_changed" || event.Payload["action"] != "invoke_behavior" {
			continue
		}
		effects, _ := event.Payload["effects"].([]any)
		if len(effects) >= 2 {
			foundBehaviorEvent = true
		}
	}
	if !foundBehaviorEvent {
		t.Fatalf("invoke_behavior event has no applied effects: %+v", events.Items)
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

	// A stale expected_version is a conflict, not a silent write.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action":           "set_experience",
			"input":            map[string]any{"experience": experience},
			"expected_version": 2,
		},
	})
	if status != http.StatusConflict {
		t.Fatalf("stale set_experience status = %d body = %s", status, body)
	}

	// An id from another world is not a valid reference even when the caller
	// owns both objects. Reference validation is scoped to this world.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"object_type": "open_world",
			"title":       "A different world",
			"content": map[string]any{
				"kinds": []any{map[string]any{
					"name": "Other", "hue": 10,
					"fields": []any{map[string]any{"key": "value", "label": "Value", "type": "number"}},
				}},
				"nodes": []any{map[string]any{"kind_index": 0, "name": "Other thing"}},
			},
			"shared_context": map[string]any{},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("second world status = %d body = %s", status, body)
	}
	foreign := decodeObject(t, body)
	foreignExperience := map[string]any{
		"protocol": 1,
		"root": map[string]any{
			"id": "root", "capability": "core.field",
			"props": map[string]any{"entity_id": nodeID(t, foreign, 0), "field": "value", "label": "", "control": "auto"},
		},
	}
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action":           "set_experience",
			"input":            map[string]any{"experience": foreignExperience},
			"expected_version": 3,
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("foreign reference status = %d body = %s", status, body)
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
