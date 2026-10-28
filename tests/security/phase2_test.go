// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

type confirmationResponse struct {
	ID                string  `json:"id"`
	Action            string  `json:"action"`
	ResourceType      string  `json:"resource_type"`
	Status            string  `json:"status"`
	PermissionSummary string  `json:"permission_summary"`
	TargetObjectID    *string `json:"target_object_id"`
	Effect            *struct {
		Title      string `json:"title"`
		Summary    string `json:"summary"`
		ObjectType string `json:"object_type"`
		KindCount  int    `json:"kind_count"`
		NodeCount  int    `json:"node_count"`
		RuleCount  int    `json:"rule_count"`
	} `json:"effect"`
}

func TestPersonalAICreatesOpenWorldThroughHumanConfirmation(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "phase2ai")

	status, body, _ := user.call(t, http.MethodPost, "/api/v1/ai/conversations", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body:           map[string]any{"title": "General"},
	})
	if status != http.StatusCreated {
		t.Fatalf("create conversation status = %d body = %s", status, body)
	}
	var conversation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &conversation); err != nil {
		t.Fatalf("decode conversation: %v", err)
	}

	status, body, _ = user.call(t, http.MethodPost,
		"/api/v1/ai/conversations/"+conversation.ID+"/messages", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body:           map[string]any{"content": "Create my first world"},
		})
	if status != http.StatusOK {
		t.Fatalf("send message status = %d body = %s", status, body)
	}
	var turn struct {
		ProposedActions []struct {
			Kind           string `json:"kind"`
			ConfirmationID string `json:"confirmation_id"`
			Action         string `json:"action"`
		} `json:"proposed_actions"`
	}
	if err := json.Unmarshal(body, &turn); err != nil {
		t.Fatalf("decode turn: %v", err)
	}
	if len(turn.ProposedActions) != 1 || turn.ProposedActions[0].Action != "world.create" ||
		turn.ProposedActions[0].ConfirmationID == "" {
		t.Fatalf("unexpected proposed actions: %+v", turn.ProposedActions)
	}

	// The proposal alone creates nothing.
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/world/stage", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("stage status = %d body = %s", status, body)
	}
	var stage struct {
		Objects []json.RawMessage `json:"objects"`
	}
	if err := json.Unmarshal(body, &stage); err != nil {
		t.Fatalf("decode stage: %v", err)
	}
	if len(stage.Objects) != 0 {
		t.Fatalf("a proposal created %d objects", len(stage.Objects))
	}

	status, body, _ = user.call(t, http.MethodGet, "/api/v1/confirmations?status=pending", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("list confirmations status = %d body = %s", status, body)
	}
	var list struct {
		Items []confirmationResponse `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode confirmations: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("confirmations = %d, want 1", len(list.Items))
	}
	pending := list.Items[0]
	if pending.Action != "world.create" || pending.ResourceType != "personal_ai" ||
		pending.Effect == nil || pending.Effect.ObjectType != "open_world" ||
		pending.Effect.NodeCount < 1 || pending.Effect.Title == "" {
		t.Fatalf("unexpected confirmation: %+v", pending)
	}

	status, body, _ = user.call(t, http.MethodPost,
		"/api/v1/confirmations/"+pending.ID+"/approve", callOptions{
			IdempotencyKey: newIdempotencyKey(),
		})
	if status != http.StatusOK {
		t.Fatalf("approve status = %d body = %s", status, body)
	}
	created := decodeObject(t, body)
	if created.ObjectType != "open_world" || created.PresentationTemplate != "open_world_v1" {
		t.Fatalf("created object = %+v", created)
	}
	nodes, _ := created.State["nodes"].([]any)
	if len(nodes) != 3 {
		t.Fatalf("created nodes = %d, want 3", len(nodes))
	}
	kinds, _ := created.State["kinds"].([]any)
	if len(kinds) != 2 {
		t.Fatalf("created kinds = %d, want 2", len(kinds))
	}

	// The object is real and readable through the normal endpoint.
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+created.ID, callOptions{})
	if status != http.StatusOK {
		t.Fatalf("get created object status = %d body = %s", status, body)
	}
	loaded := decodeObject(t, body)
	if loaded.Version != 1 {
		t.Fatalf("loaded version = %d", loaded.Version)
	}
}

func TestOpenWorldActionsDeriveAndIsolation(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "phase2owner")
	other := env.register(t, "phase2other")

	status, body, _ := owner.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"object_type": "open_world",
			"title":       "A world of tides",
			"content": map[string]any{
				"premise": "The tide remembers.",
				"kinds": []any{
					map[string]any{"name": "Tide", "hue": 200, "fields": []any{
						map[string]any{"key": "level", "label": "Level", "type": "number"},
					}},
				},
				"nodes": []any{
					map[string]any{"kind_index": 0, "name": "Morning tide", "text": "First."},
					map[string]any{"kind_index": 0, "name": "Evening tide", "text": "Second."},
				},
				"links": []any{
					map[string]any{"from_index": 0, "to_index": 1, "label": "becomes"},
				},
				"rules": []any{
					map[string]any{"trigger_index": 0, "effects": []any{
						map[string]any{"node_index": 1, "field": "level", "op": "set", "value": 4},
					}},
				},
				"views": []any{"stage", "cards", "timeline"},
				"tags":  []any{"tide"},
			},
			"shared_context": map[string]any{},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create open world status = %d body = %s", status, body)
	}
	world := decodeObject(t, body)

	// The other user cannot read a private open world.
	status, _, _ = other.call(t, http.MethodGet, "/api/v1/objects/"+world.ID, callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("foreign object status = %d, want 404", status)
	}

	// Interact with the trigger node; the rule changes the other node.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action":           "touch_node",
			"input":            map[string]any{"node_id": nodeID(t, world, 0)},
			"expected_version": 1,
		},
	})
	if status != http.StatusOK {
		t.Fatalf("touch status = %d body = %s", status, body)
	}
	afterTouch := decodeObject(t, body)
	if afterTouch.Version != 2 {
		t.Fatalf("version = %d, want 2", afterTouch.Version)
	}
	if nodeStatus(t, afterTouch, 0) != "done" {
		t.Fatalf("trigger status = %q", nodeStatus(t, afterTouch, 0))
	}
	if nodeValue(t, afterTouch, 1, "level") != float64(4) {
		t.Fatalf("rule did not set level: %+v", afterTouch.State["nodes"])
	}

	// A derived world keeps the structure but gets fresh identity.
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+world.ID+"/derive", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"title":              "Another tide",
			"source_version":     2,
			"inheritance_fields": []string{"premise", "structure", "views", "tags"},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("derive status = %d body = %s", status, body)
	}
	child := decodeObject(t, body)
	if child.ObjectType != "open_world" || child.Version != 1 {
		t.Fatalf("child = %+v", child)
	}
	parentKinds, _ := afterTouch.State["kinds"].([]any)
	childKinds, _ := child.State["kinds"].([]any)
	if len(childKinds) != len(parentKinds) {
		t.Fatalf("child kinds = %d, parent = %d", len(childKinds), len(parentKinds))
	}
	parentID := parentKinds[0].(map[string]any)["id"]
	childID := childKinds[0].(map[string]any)["id"]
	if parentID == childID {
		t.Fatalf("derived kind reused the parent id")
	}
	if child.Lineage == nil || child.Lineage.Parent.ObjectID != world.ID {
		t.Fatalf("child lineage = %+v", child.Lineage)
	}
}

func nodeID(t *testing.T, object objectResponse, index int) string {
	t.Helper()
	nodes, _ := object.State["nodes"].([]any)
	if index >= len(nodes) {
		t.Fatalf("node %d missing", index)
	}
	id, _ := nodes[index].(map[string]any)["id"].(string)
	return id
}

func nodeStatus(t *testing.T, object objectResponse, index int) string {
	t.Helper()
	nodes, _ := object.State["nodes"].([]any)
	status, _ := nodes[index].(map[string]any)["status"].(string)
	return status
}

func nodeValue(t *testing.T, object objectResponse, index int, key string) any {
	t.Helper()
	nodes, _ := object.State["nodes"].([]any)
	values, _ := nodes[index].(map[string]any)["values"].(map[string]any)
	return values[key]
}
