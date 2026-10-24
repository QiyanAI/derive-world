// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestG06ModelOutputCannotExceedDelegationOrForgeTargets drives model output
// that tries to escalate beyond B's delegated permissions, target another
// object, forge a version, or carry an actor field. Only server-authorized,
// schema-valid proposals may exist, and no object state may change without a
// human action.
func TestG06ModelOutputCannotExceedDelegationOrForgeTargets(t *testing.T) {
	provider := &scriptedProvider{replies: []string{
		// Forbidden actions and a derive without delegation.
		`{"reply":"escalate","proposed_actions":[
		  {"capability_id":"object.propose_change","input":{"action":"add_member","input":{"user_id":"11111111-1111-7111-8111-111111111111"}}},
		  {"capability_id":"object.propose_change","input":{"action":"delete_object","input":{}}},
		  {"capability_id":"object.propose_change","input":{"action":"set_shared_context","input":{"shared_context":{}}}},
		  {"capability_id":"object.derive","input":{"title":"forged derive","inheritance_fields":["body"]}}
		]}`,
		// A real action aimed at a different object.
		`{"reply":"target another object","proposed_actions":[
		  {"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"stolen"},"expected_version":1,"object_id":"OTHER_OBJECT"}}
		]}`,
		// An actor field inside the action input.
		`{"reply":"forge actor","proposed_actions":[
		  {"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"forged","actor_user_id":"ACTOR"}}}
		]}`,
		// A legitimate delegated action, proving the harness is not overbroad.
		`{"reply":"legitimate","proposed_actions":[
		  {"capability_id":"object.propose_change","input":{"action":"set_body","input":{"body":"legitimate proposal"},"expected_version":1}}
		]}`,
	}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g06adv-a")
	b := env.register(t, "g06adv-b")
	object := a.createObject(t, "collaborative", "G06 adversarial parent", map[string]any{
		"body": "parent body",
	})
	other := a.createObject(t, "collaborative", "G06 other object", map[string]any{
		"body": "other body",
	})
	invitation := a.createInvitation(t, object.ID, []string{"read", "contribute"}, 1)
	joinStatus, joinBody := b.acceptInvitation(t, *invitation.Token)
	requireStatus(t, joinStatus, http.StatusOK, joinBody)
	conversation := b.createConversation(t, "G06 adversarial", &object.ID)

	provider.mu.Lock()
	provider.replies[1] = strings.ReplaceAll(provider.replies[1], "OTHER_OBJECT", other.ID)
	provider.replies[2] = strings.ReplaceAll(provider.replies[2], "ACTOR", a.id.String())
	provider.mu.Unlock()

	status, body := b.sendMessage(t, conversation, "escalate")
	requireStatus(t, status, http.StatusOK, body)
	turn := decodeTurn(t, body)
	if len(turn.ProposedActions) != 0 {
		t.Fatalf("forbidden actions were accepted: %s", body)
	}

	status, body = b.sendMessage(t, conversation, "target another object")
	requireStatus(t, status, http.StatusOK, body)
	turn = decodeTurn(t, body)
	if len(turn.ProposedActions) != 0 {
		t.Fatalf("cross-object action was accepted: %s", body)
	}

	status, body = b.sendMessage(t, conversation, "forge actor")
	requireStatus(t, status, http.StatusOK, body)
	turn = decodeTurn(t, body)
	if len(turn.ProposedActions) != 0 {
		t.Fatalf("actor-bearing action was accepted: %s", body)
	}
	if got := env.count(t, `
SELECT count(*) FROM object_proposals
 WHERE input::text LIKE '%actor_user_id%'`); got != 0 {
		t.Fatalf("actor field was persisted in %d proposals", got)
	}

	status, body = b.sendMessage(t, conversation, "legitimate")
	requireStatus(t, status, http.StatusOK, body)
	turn = decodeTurn(t, body)
	if len(turn.ProposedActions) != 1 || turn.ProposedActions[0].Kind != "proposal" {
		t.Fatalf("legitimate delegated action was not accepted: %s", body)
	}

	// Nothing executed without a human: both objects remain at version 1 and
	// no direct object action event exists.
	for _, objectID := range []string{object.ID, other.ID} {
		getStatus, getBody, _ := a.call(t, http.MethodGet, "/api/v1/objects/"+objectID, callOptions{})
		requireStatus(t, getStatus, http.StatusOK, getBody)
		if got := decodeObject(t, getBody); got.Version != 1 {
			t.Fatalf("object %s version = %d, want 1 with no human action", objectID, got.Version)
		}
	}
	if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND event_type = 'object.state_changed'`, mustUUID(t, object.ID)); got != 0 {
		t.Fatalf("model output produced %d state-change events without a human", got)
	}
	if got := env.count(t, `SELECT count(*) FROM confirmations WHERE user_id=$1`, b.id); got != 0 {
		t.Fatalf("an undelegated derive created %d confirmations", got)
	}
}

// TestG06ProfileAndActorForgeryIsRejected proves that a caller cannot select
// another user's Personal AI profile or actor through the request body.
func TestG06ProfileAndActorForgeryIsRejected(t *testing.T) {
	provider := &scriptedProvider{replies: []string{
		`{"reply":"should not run","proposed_actions":[]}`,
	}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g06forge-a")
	b := env.register(t, "g06forge-b")
	object := a.createObject(t, "collaborative", "G06 forgery parent", map[string]any{"body": "base"})
	invitation := a.createInvitation(t, object.ID, []string{"read", "contribute"}, 1)
	joinStatus, joinBody := b.acceptInvitation(t, *invitation.Token)
	requireStatus(t, joinStatus, http.StatusOK, joinBody)
	conversation := b.createConversation(t, "G06 forgery", &object.ID)

	origin := env.origin
	csrf := b.csrf
	for name, payload := range map[string]map[string]any{
		"forged ai_profile_id": {"content": "hello", "ai_profile_id": a.profileID(t)},
		"forged actor_user_id": {"content": "hello", "actor_user_id": a.id.String()},
	} {
		t.Run(name, func(t *testing.T) {
			status, body, _ := env.call(t, b.client, http.MethodPost,
				"/api/v1/ai/conversations/"+conversation+"/messages", callOptions{
					Origin: &origin, CSRFToken: &csrf,
					IdempotencyKey: newIdempotencyKey(), Body: payload,
				})
			requireStatus(t, status, http.StatusBadRequest, body)
			requireCode(t, body, "invalid_request")
		})
	}
	if provider.requestCount() != 0 {
		t.Fatalf("forged identity reached the model: %d calls", provider.requestCount())
	}
	if got := env.count(t, `SELECT count(*) FROM ai_messages WHERE conversation_id=$1`,
		mustUUID(t, conversation)); got != 0 {
		t.Fatalf("forged identity persisted %d messages", got)
	}
	var overrides int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events
 WHERE action = 'authz.actor_override_attempt' AND decision = 'deny'
   AND actor_user_id = $1`, b.id).Scan(&overrides); err != nil {
		t.Fatalf("read actor-override audit: %v", err)
	}
	if overrides == 0 {
		t.Fatal("the actor override was not audited")
	}
}
