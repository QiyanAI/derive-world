// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestG03ProfileAndContextBoundsAndIsolation(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03pd-a")
	b := env.register(t, "g03pd-b")

	status, body, _ := a.call(t, http.MethodPatch, "/api/v1/ai/profile", callOptions{
		Body: map[string]any{"name": "A AI", "persona": "concise"},
	})
	requireStatus(t, status, http.StatusOK, body)
	var updated struct {
		Name    string `json:"name"`
		Persona string `json:"persona"`
	}
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if updated.Name != "A AI" || updated.Persona != "concise" {
		t.Fatalf("profile update = %+v", updated)
	}
	status, body, _ = b.call(t, http.MethodGet, "/api/v1/ai/profile", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var bProfile struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &bProfile); err != nil {
		t.Fatalf("decode B profile: %v", err)
	}
	if bProfile.Name == "A AI" {
		t.Fatal("profile update crossed users")
	}

	status, body, _ = a.call(t, http.MethodPatch, "/api/v1/ai/profile", callOptions{
		Body: map[string]any{"persona": strings.Repeat("p", 2001)},
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	status, body, _ = a.call(t, http.MethodPut, "/api/v1/ai/context", callOptions{
		Body: map[string]any{"content": strings.Repeat("c", 8200)},
	})
	requireStatus(t, status, http.StatusRequestEntityTooLarge, body)

	a.setPrivateContext(t, "MARK-PD-A")
	status, body, _ = a.call(t, http.MethodGet, "/api/v1/ai/context", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if !strings.Contains(string(body), "MARK-PD-A") {
		t.Fatal("A context missing")
	}
	status, body, _ = b.call(t, http.MethodGet, "/api/v1/ai/context", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), "MARK-PD-A") {
		t.Fatal("B read A private context")
	}
	if env.count(t, `SELECT count(*) FROM audit_events
 WHERE action='ai.profile_update' AND actor_user_id=$1`, a.id) != 1 {
		t.Fatal("profile update audit missing")
	}
}

func TestG03ConversationIsolationAndSoftDelete(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03conv-a")
	b := env.register(t, "g03conv-b")
	conversation := a.createConversation(t, "A private conversation", nil)
	messageStatus, messageBody := a.sendMessage(t, conversation, "hello")
	requireStatus(t, messageStatus, http.StatusOK, messageBody)

	status, body, _ := b.call(t, http.MethodGet,
		"/api/v1/ai/conversations/"+conversation, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
	status, body, _ = b.call(t, http.MethodDelete,
		"/api/v1/ai/conversations/"+conversation, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
	status, body, _ = a.call(t, http.MethodDelete,
		"/api/v1/ai/conversations/"+conversation, callOptions{})
	requireStatus(t, status, http.StatusNoContent, body)
	status, body, _ = a.call(t, http.MethodGet,
		"/api/v1/ai/conversations/"+conversation, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
	if env.count(t, `SELECT count(*) FROM audit_events
 WHERE action='ai.conversation_delete' AND actor_user_id=$1`, a.id) != 1 {
		t.Fatal("conversation delete audit missing")
	}
	if env.count(t, `SELECT count(*) FROM ai_messages WHERE conversation_id=$1`,
		mustUUID(t, conversation)) != 2 {
		t.Fatal("soft delete physically removed messages")
	}
}
