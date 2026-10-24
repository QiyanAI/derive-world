// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type invitationResponse struct {
	ID          string   `json:"id"`
	ObjectID    string   `json:"object_id"`
	Token       *string  `json:"token"`
	Permissions []string `json:"permissions"`
	MaxUses     int      `json:"max_uses"`
	UseCount    int      `json:"use_count"`
}

func (u *testUser) setPrivateContext(t *testing.T, content string) {
	t.Helper()
	status, body, _ := u.call(t, http.MethodPut, "/api/v1/ai/context", callOptions{
		Body: map[string]any{"content": content},
	})
	requireStatus(t, status, http.StatusOK, body)
}

func (u *testUser) createInvitation(t *testing.T, objectID string, permissions []string, maxUses int) invitationResponse {
	t.Helper()
	body := map[string]any{"max_uses": maxUses, "expires_in_days": 7}
	if permissions != nil {
		body["permissions"] = permissions
	}
	status, responseBody, _ := u.call(t, http.MethodPost,
		"/api/v1/objects/"+objectID+"/invitations", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body:           body,
		})
	requireStatus(t, status, http.StatusCreated, responseBody)
	var invitation invitationResponse
	if err := json.Unmarshal(responseBody, &invitation); err != nil {
		t.Fatalf("decode invitation: %v", err)
	}
	if invitation.Token == nil || *invitation.Token == "" {
		t.Fatal("creation response did not include the one-time token")
	}
	return invitation
}

func (u *testUser) acceptInvitation(t *testing.T, token string) (int, []byte) {
	t.Helper()
	status, body, _ := u.call(t, http.MethodPost,
		"/api/v1/invitations/"+token+"/accept", callOptions{})
	return status, body
}

func (u *testUser) createConversation(t *testing.T, title string, objectID *string) string {
	t.Helper()
	body := map[string]any{"title": title}
	if objectID != nil {
		body["object_id"] = *objectID
	}
	status, responseBody, _ := u.call(t, http.MethodPost, "/api/v1/ai/conversations", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body:           body,
	})
	requireStatus(t, status, http.StatusCreated, responseBody)
	var conversation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(responseBody, &conversation); err != nil {
		t.Fatalf("decode conversation: %v", err)
	}
	return conversation.ID
}

func (u *testUser) sendMessage(t *testing.T, conversationID, content string) (int, []byte) {
	t.Helper()
	status, body, _ := u.call(t, http.MethodPost,
		"/api/v1/ai/conversations/"+conversationID+"/messages", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body:           map[string]any{"content": content},
		})
	return status, body
}

func TestG03PrivateContextHandoffAndRevocation(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03a")
	b := env.register(t, "g03b")

	a.setPrivateContext(t, "MARK-A private only")
	contextStatus, contextBody, _ := b.call(t, http.MethodGet, "/api/v1/ai/context", callOptions{})
	requireStatus(t, contextStatus, http.StatusOK, contextBody)
	var bContext struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(contextBody, &bContext); err != nil {
		t.Fatalf("decode context: %v", err)
	}
	if strings.Contains(bContext.Content, "MARK-A") {
		t.Fatal("B's private context leaked A's private context")
	}

	object := a.createObject(t, "collaborative", "G03 basic", map[string]any{
		"summary": "shared summary",
		"body":    "shared body",
	})
	invitation := a.createInvitation(t, object.ID,
		[]string{"read", "contribute", "derive"}, 1)
	status, body := b.acceptInvitation(t, *invitation.Token)
	requireStatus(t, status, http.StatusOK, body)

	handoffStatus, handoffBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/handoff", callOptions{})
	requireStatus(t, handoffStatus, http.StatusOK, handoffBody)
	if strings.Contains(string(handoffBody), "MARK-A") {
		t.Fatal("handoff leaked private context")
	}
	if !strings.Contains(string(handoffBody), "shared body") {
		t.Fatal("handoff did not include the shared object state")
	}

	conversationID := b.createConversation(t, "Bound chat", &object.ID)
	messageStatus, messageBody := b.sendMessage(t, conversationID, "hello")
	requireStatus(t, messageStatus, http.StatusOK, messageBody)
	var turn struct {
		AssistantMessage struct {
			Content     string `json:"content"`
			ModelMode   string `json:"model_mode"`
			ParseStatus string `json:"parse_status"`
		} `json:"assistant_message"`
		ModelMode string `json:"model_mode"`
	}
	if err := json.Unmarshal(messageBody, &turn); err != nil {
		t.Fatalf("decode message turn: %v", err)
	}
	if turn.AssistantMessage.Content == "" || turn.ModelMode != "fixture" {
		t.Fatalf("unexpected assistant turn: %+v", turn)
	}
	if env.count(t, `SELECT count(*) FROM ai_messages WHERE conversation_id=$1`,
		mustUUID(t, conversationID)) != 2 {
		t.Fatal("message turn was not persisted")
	}

	removeStatus, removeBody, _ := a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/members/"+b.id.String(), callOptions{})
	requireStatus(t, removeStatus, http.StatusNoContent, removeBody)

	revokedHandoff, revokedBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/handoff", callOptions{})
	requireStatus(t, revokedHandoff, http.StatusNotFound, revokedBody)
	requireCode(t, revokedBody, "not_found")

	revokedMessage, revokedMessageBody := b.sendMessage(t, conversationID, "hello again")
	requireStatus(t, revokedMessage, http.StatusNotFound, revokedMessageBody)

	var tokenHashCount int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM invitations WHERE token_hash = $1`,
		sha256String(*invitation.Token)).Scan(&tokenHashCount); err != nil {
		t.Fatalf("query token hash: %v", err)
	}
	if tokenHashCount != 1 {
		t.Fatalf("token hash rows = %d", tokenHashCount)
	}
	if strings.Contains(string(revokedBody), *invitation.Token) {
		t.Fatal("invitation token appeared in a later response")
	}
}

func sha256String(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}
