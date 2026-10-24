// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type delegationResponse struct {
	ID          string   `json:"id"`
	ObjectID    *string  `json:"object_id"`
	Permissions []string `json:"permissions"`
	Status      string   `json:"status"`
}

func (u *testUser) profileID(t *testing.T) string {
	t.Helper()
	status, body, _ := u.call(t, http.MethodGet, "/api/v1/ai/profile", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var profile struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &profile); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	return profile.ID
}

func (u *testUser) createDelegation(t *testing.T, body map[string]any) (int, []byte) {
	t.Helper()
	status, response, _ := u.call(t, http.MethodPost, "/api/v1/ai/delegations", callOptions{
		Body: body,
	})
	return status, response
}

func (u *testUser) listDelegations(t *testing.T) []delegationResponse {
	t.Helper()
	status, body, _ := u.call(t, http.MethodGet, "/api/v1/ai/delegations", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var listed struct {
		Items []delegationResponse `json:"items"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decode delegations: %v", err)
	}
	return listed.Items
}

func (u *testUser) revokeDelegation(t *testing.T, delegationID string) {
	t.Helper()
	status, body, _ := u.call(t, http.MethodDelete,
		"/api/v1/ai/delegations/"+delegationID, callOptions{})
	requireStatus(t, status, http.StatusNoContent, body)
}

func TestG03DelegationSubsetProfileAndOwnership(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03del-a")
	b := env.register(t, "g03del-b")
	aProfile := a.profileID(t)
	bProfile := b.profileID(t)

	// A cannot name B's Personal AI profile.
	status, body := a.createDelegation(t, map[string]any{
		"ai_profile_id": bProfile,
		"permissions":   []string{"read"},
	})
	requireStatus(t, status, http.StatusNotFound, body)

	// Global delegation cannot carry invite or manage.
	status, body = a.createDelegation(t, map[string]any{
		"permissions": []string{"read", "invite"},
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, "invalid_request")

	// A owns this object only in the sense of membership; make B the owner and
	// invite A with read so an object-specific delegation cannot exceed read.
	object := b.createObject(t, "collaborative", "Delegation object", map[string]any{
		"body": "body",
	})
	invite := b.createInvitation(t, object.ID, []string{"read"}, 1)
	status, body = a.acceptInvitation(t, *invite.Token)
	requireStatus(t, status, http.StatusOK, body)
	status, body = a.createDelegation(t, map[string]any{
		"ai_profile_id": aProfile,
		"object_id":     object.ID,
		"permissions":   []string{"read", "contribute"},
	})
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, "forbidden")

	// A owns a second object and may create a global delegation.
	own := a.createObject(t, "collaborative", "Own object", map[string]any{"body": "own"})
	status, body = a.createDelegation(t, map[string]any{
		"ai_profile_id": aProfile,
		"object_id":     own.ID,
		"permissions":   []string{"read", "derive"},
		"constraints": map[string]any{
			"require_confirmation":    []string{"derive.create"},
			"max_ai_actions_per_hour": 10,
		},
		"expires_in_days": 7,
	})
	requireStatus(t, status, http.StatusCreated, body)
	var delegation delegationResponse
	if err := json.Unmarshal(body, &delegation); err != nil {
		t.Fatalf("decode delegation: %v", err)
	}
	if delegation.ObjectID == nil || *delegation.ObjectID != own.ID ||
		delegation.Status != "active" {
		t.Fatalf("delegation = %+v", delegation)
	}
	for _, item := range a.listDelegations(t) {
		if item.ID == delegation.ID && item.ObjectID != nil && *item.ObjectID != own.ID {
			t.Fatalf("delegation object mismatch: %+v", item)
		}
	}
	// B cannot revoke A's delegation.
	status, body, _ = b.call(t, http.MethodDelete,
		"/api/v1/ai/delegations/"+delegation.ID, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
}

func TestG03AIDelegationRevocationAndForgedActor(t *testing.T) {
	provider := &scriptedProvider{replies: []string{
		`{"reply":"derive","proposed_actions":[{"capability_id":"object.derive","input":{"title":"Revoked child","inheritance_fields":["body"]}}]}`,
		`{"reply":"derive again","proposed_actions":[{"capability_id":"object.derive","input":{"title":"Should not exist","inheritance_fields":["body"]}}]}`,
	}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g03rev-a")
	object := a.createObject(t, "collaborative", "Revocation object", map[string]any{
		"body": "body",
	})
	conversationID := a.createConversation(t, "Revocation chat", &object.ID)

	// A request body may never claim a delegation.
	status, body, _ := a.call(t, http.MethodPost,
		"/api/v1/ai/conversations/"+conversationID+"/messages", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"content":       "hello",
				"delegation_id": mustUUID(t, object.ID).String(),
			},
		})
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, "invalid_request")
	if env.count(t, `SELECT count(*) FROM audit_events
 WHERE action='authz.actor_override_attempt' AND actor_user_id=$1`,
		a.id) < 1 {
		t.Fatal("actor override was not audited")
	}
	if env.count(t, `SELECT count(*) FROM ai_messages WHERE conversation_id=$1`,
		mustUUID(t, conversationID)) != 0 {
		t.Fatal("rejected actor override persisted a message")
	}

	// Revoke every active delegation and prove the AI loses all capability.
	var defaultID string
	for _, item := range a.listDelegations(t) {
		if item.ObjectID == nil && item.Status == "active" {
			defaultID = item.ID
			a.revokeDelegation(t, item.ID)
		}
	}
	if defaultID == "" {
		t.Fatal("registration default delegation not found")
	}
	status, body = a.sendMessage(t, conversationID, "hello after revocation")
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, "forbidden")
	if env.count(t, `SELECT count(*) FROM ai_messages WHERE conversation_id=$1`,
		mustUUID(t, conversationID)) != 0 {
		t.Fatal("revoked delegation still persisted a message")
	}

	// A narrower object delegation restores only the delegated capability.
	aProfile := a.profileID(t)
	status, body = a.createDelegation(t, map[string]any{
		"ai_profile_id": aProfile,
		"object_id":     object.ID,
		"permissions":   []string{"read", "derive"},
	})
	requireStatus(t, status, http.StatusCreated, body)
	var delegation delegationResponse
	if err := json.Unmarshal(body, &delegation); err != nil {
		t.Fatalf("decode delegation: %v", err)
	}
	messageStatus, messageBody := a.sendMessage(t, conversationID, "derive now")
	requireStatus(t, messageStatus, http.StatusOK, messageBody)
	turn := decodeTurn(t, messageBody)
	if len(turn.ProposedActions) != 1 || turn.ProposedActions[0].Kind != "confirmation" {
		t.Fatalf("expected one confirmation: %s", messageBody)
	}
	confirmationID := turn.ProposedActions[0].ConfirmationID
	a.revokeDelegation(t, delegation.ID)

	approveStatus, approveBody, _ := a.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve", callOptions{
			IdempotencyKey: newIdempotencyKey(),
		})
	requireStatus(t, approveStatus, http.StatusForbidden, approveBody)
	requireCode(t, approveBody, "forbidden")
	var derivedChildren int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM derivation_edges WHERE parent_object_id = $1`,
		mustUUID(t, object.ID)).Scan(&derivedChildren); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if derivedChildren != 0 {
		t.Fatal("revoked delegation still executed a confirmed derive")
	}
	if strings.Contains(string(approveBody), "Should not exist") {
		t.Fatal("confirmation payload leaked into the error response")
	}
}

func TestG03DelegationActionBudgetIsEnforced(t *testing.T) {
	provider := &scriptedProvider{replies: []string{`{
	  "reply":"two actions",
	  "proposed_actions":[
	    {"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"one"},"expected_version":1}},
	    {"capability_id":"object.derive","input":{"title":"two","inheritance_fields":["body"]}}
	  ]}`}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g03act-a")
	object := a.createObject(t, "collaborative", "Action budget object", map[string]any{
		"body": "body",
	})
	for _, item := range a.listDelegations(t) {
		if item.ObjectID == nil && item.Status == "active" {
			a.revokeDelegation(t, item.ID)
		}
	}
	status, body := a.createDelegation(t, map[string]any{
		"object_id":   object.ID,
		"permissions": []string{"read", "contribute", "derive"},
		"constraints": map[string]any{"max_ai_actions_per_hour": 1},
	})
	requireStatus(t, status, http.StatusCreated, body)
	conversation := a.createConversation(t, "Action budget chat", &object.ID)
	messageStatus, messageBody := a.sendMessage(t, conversation, "propose")
	requireStatus(t, messageStatus, http.StatusOK, messageBody)
	turn := decodeTurn(t, messageBody)
	if len(turn.ProposedActions) != 1 {
		t.Fatalf("accepted actions = %d, want 1: %s", len(turn.ProposedActions), messageBody)
	}
	if env.count(t, `SELECT count(*) FROM object_proposals WHERE object_id=$1`,
		mustUUID(t, object.ID))+
		env.count(t, `SELECT count(*) FROM confirmations WHERE user_id=$1`, a.id) != 1 {
		t.Fatal("action budget did not cap proposals and confirmations")
	}
}

func TestG03ActionBudgetConcurrentTurns(t *testing.T) {
	provider := &scriptedProvider{replies: []string{
		deriveReply("concurrent budget one"),
		deriveReply("concurrent budget two"),
	}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g03actconc-a")
	object := a.createObject(t, "collaborative", "Concurrent action budget", map[string]any{
		"body": "body",
	})
	for _, item := range a.listDelegations(t) {
		if item.ObjectID == nil && item.Status == "active" {
			a.revokeDelegation(t, item.ID)
		}
	}
	status, body := a.createDelegation(t, map[string]any{
		"object_id":   object.ID,
		"permissions": []string{"read", "contribute", "derive"},
		"constraints": map[string]any{"max_ai_actions_per_hour": 1},
	})
	requireStatus(t, status, http.StatusCreated, body)
	conversationOne := a.createConversation(t, "Budget one", &object.ID)
	conversationTwo := a.createConversation(t, "Budget two", &object.ID)
	type outcome struct {
		status int
		body   []byte
	}
	results := make(chan outcome, 2)
	send := func(conversation string) {
		origin := env.origin
		csrf := a.csrf
		status, body, _ := env.call(t, a.client, http.MethodPost,
			"/api/v1/ai/conversations/"+conversation+"/messages", callOptions{
				Origin: &origin, CSRFToken: &csrf,
				IdempotencyKey: newIdempotencyKey(),
				Body:           map[string]any{"content": "propose"},
			})
		results <- outcome{status: status, body: body}
	}
	go send(conversationOne)
	go send(conversationTwo)
	first := <-results
	second := <-results
	if first.status != http.StatusOK || second.status != http.StatusOK {
		t.Fatalf("concurrent turn statuses = %d/%d", first.status, second.status)
	}
	if env.count(t, `SELECT count(*) FROM confirmations WHERE user_id=$1`, a.id) != 1 {
		t.Fatal("concurrent turns exceeded the action budget")
	}
}

func TestG03ReplayAfterDelegationRevocation(t *testing.T) {
	provider := &scriptedProvider{replies: []string{
		`{"reply":"first turn","proposed_actions":[]}`,
		deriveReply("replay approval child"),
	}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g03replay-a")
	object := a.createObject(t, "collaborative", "Replay object", map[string]any{
		"body": "body",
	})
	conversation := a.createConversation(t, "Replay chat", &object.ID)
	origin := env.origin
	csrf := a.csrf

	messageKey := newIdempotencyKey()
	status, body, _ := env.call(t, a.client, http.MethodPost,
		"/api/v1/ai/conversations/"+conversation+"/messages", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: messageKey,
			Body: map[string]any{"content": "hello"},
		})
	requireStatus(t, status, http.StatusOK, body)

	confirmKey := newIdempotencyKey()
	status, body, _ = env.call(t, a.client, http.MethodPost,
		"/api/v1/ai/conversations/"+conversation+"/messages", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: confirmKey,
			Body: map[string]any{"content": "propose"},
		})
	requireStatus(t, status, http.StatusOK, body)
	turn := decodeTurn(t, body)
	if len(turn.ProposedActions) != 1 || turn.ProposedActions[0].Kind != "confirmation" {
		t.Fatalf("expected a derive confirmation: %s", body)
	}
	confirmationID := turn.ProposedActions[0].ConfirmationID
	approveKey := newIdempotencyKey()
	status, body, _ = env.call(t, a.client, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: approveKey,
		})
	requireStatus(t, status, http.StatusOK, body)

	for _, item := range a.listDelegations(t) {
		if item.Status == "active" {
			a.revokeDelegation(t, item.ID)
		}
	}
	status, body, _ = env.call(t, a.client, http.MethodPost,
		"/api/v1/ai/conversations/"+conversation+"/messages", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: messageKey,
			Body: map[string]any{"content": "hello"},
		})
	requireStatus(t, status, http.StatusNotFound, body)
	status, body, _ = env.call(t, a.client, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve", callOptions{
			Origin: &origin, CSRFToken: &csrf, IdempotencyKey: approveKey,
		})
	requireStatus(t, status, http.StatusNotFound, body)
}
