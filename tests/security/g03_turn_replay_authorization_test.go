// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// B-01: cached Personal AI turn replay authorization.
//
// D05-20 #4 requires the current request's LoadResource + Authorize before a
// cached response is replayed; D05-16 says revoking one delegation removes
// only that delegation's coverage, while other active delegations still
// apply. ReplayAIAllowed therefore requires current object read through the
// current membership and any active delegation. Exact-delegation binding is
// required on execution paths (proposal apply and confirmation approval),
// which are tested separately; a cached AI turn is a read-only response.
//
// Every replay is also checked for no new model call, message pair, model
// invocation, or object version change.

func sendTurnMessage(t *testing.T, user *testUser, conversationID, key, content string) (int, []byte, http.Header) {
	t.Helper()
	return user.call(t, http.MethodPost,
		"/api/v1/ai/conversations/"+conversationID+"/messages",
		callOptions{IdempotencyKey: key, Body: map[string]any{"content": content}})
}

func turnMessageCount(t *testing.T, env *testEnv, conversationID string) int {
	t.Helper()
	return env.count(t, `
SELECT count(*) FROM ai_messages WHERE conversation_id = $1`,
		mustUUID(t, conversationID))
}

func modelInvocationCount(t *testing.T, env *testEnv, userID uuid.UUID) int {
	t.Helper()
	return env.count(t, `
SELECT count(*) FROM model_invocations WHERE user_id = $1`, userID)
}

func turnObjectVersion(t *testing.T, env *testEnv, objectID string) int {
	t.Helper()
	var version int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT version FROM living_objects WHERE id = $1`,
		mustUUID(t, objectID)).Scan(&version); err != nil {
		t.Fatalf("read object version: %v", err)
	}
	return version
}

func assertReplayedTurn(t *testing.T, env *testEnv, user *testUser, conversationID, objectID, key string,
	first []byte, provider *scriptedProvider, wantMessages, wantInvocations, wantVersion int) {
	t.Helper()
	status, replay, headers := sendTurnMessage(t, user, conversationID, key, "hello")
	requireStatus(t, status, http.StatusOK, replay)
	if headers.Get("Idempotency-Replayed") != "true" {
		t.Fatal("replay header missing")
	}
	var firstValue, replayValue any
	if err := json.Unmarshal(first, &firstValue); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if err := json.Unmarshal(replay, &replayValue); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if !reflect.DeepEqual(firstValue, replayValue) {
		t.Fatal("replay body differs semantically from the first response")
	}
	if got := turnMessageCount(t, env, conversationID); got != wantMessages {
		t.Fatalf("messages after replay = %d, want %d", got, wantMessages)
	}
	if got := modelInvocationCount(t, env, user.id); got != wantInvocations {
		t.Fatalf("model invocations after replay = %d, want %d", got, wantInvocations)
	}
	if got := provider.requestCount(); got != wantInvocations {
		t.Fatalf("provider calls after replay = %d, want %d", got, wantInvocations)
	}
	if got := turnObjectVersion(t, env, objectID); got != wantVersion {
		t.Fatalf("object version after replay = %d, want %d", got, wantVersion)
	}
	if got := env.count(t, `
SELECT count(*) FROM idempotency_keys
 WHERE actor_user_id = $1 AND route = '/api/v1/ai/conversations/{id}/messages' AND key = $2`,
		user.id, key); got != 1 {
		t.Fatalf("idempotency rows = %d, want 1", got)
	}
}

func TestG03TurnReplayAuthorization(t *testing.T) {
	t.Run("original delegation active", func(t *testing.T) {
		provider := &scriptedProvider{replies: []string{`{"reply":"first turn","proposed_actions":[]}`}}
		env := newTestEnvWithProvider(t, nil, provider)
		a := env.register(t, "g03replayactive")
		object := a.createObject(t, "collaborative", "Replay active", map[string]any{"body": "body"})
		conversation := a.createConversation(t, "Replay active chat", &object.ID)
		key := newIdempotencyKey()
		status, first, headers := sendTurnMessage(t, a, conversation, key, "hello")
		requireStatus(t, status, http.StatusOK, first)
		if headers.Get("Idempotency-Replayed") != "" {
			t.Fatal("first turn was marked as a replay")
		}
		assertReplayedTurn(t, env, a, conversation, object.ID, key, first, provider, 2, 1, 1)
	})

	t.Run("original revoked with no other authorization", func(t *testing.T) {
		provider := &scriptedProvider{replies: []string{`{"reply":"first turn","proposed_actions":[]}`}}
		env := newTestEnvWithProvider(t, nil, provider)
		a := env.register(t, "g03replayrevoked")
		object := a.createObject(t, "collaborative", "Replay revoked", map[string]any{"body": "body"})
		conversation := a.createConversation(t, "Replay revoked chat", &object.ID)
		key := newIdempotencyKey()
		status, first, _ := sendTurnMessage(t, a, conversation, key, "hello")
		requireStatus(t, status, http.StatusOK, first)

		for _, item := range a.listDelegations(t) {
			if item.Status == "active" {
				a.revokeDelegation(t, item.ID)
			}
		}
		status, body, _ := sendTurnMessage(t, a, conversation, key, "hello")
		requireStatus(t, status, http.StatusNotFound, body)
		requireCode(t, body, "not_found")
		if got := turnMessageCount(t, env, conversation); got != 2 {
			t.Fatalf("messages after denied replay = %d, want 2", got)
		}
		if got := provider.requestCount(); got != 1 {
			t.Fatalf("denied replay made %d provider calls, want 1", got)
		}
		if got := turnObjectVersion(t, env, object.ID); got != 1 {
			t.Fatalf("denied replay changed object version to %d", got)
		}
	})

	t.Run("original revoked with a new read delegation", func(t *testing.T) {
		provider := &scriptedProvider{replies: []string{`{"reply":"first turn","proposed_actions":[]}`}}
		env := newTestEnvWithProvider(t, nil, provider)
		a := env.register(t, "g03replaynewread")
		object := a.createObject(t, "collaborative", "Replay new read", map[string]any{"body": "body"})
		conversation := a.createConversation(t, "Replay new read chat", &object.ID)
		key := newIdempotencyKey()
		status, first, _ := sendTurnMessage(t, a, conversation, key, "hello")
		requireStatus(t, status, http.StatusOK, first)

		for _, item := range a.listDelegations(t) {
			if item.Status == "active" {
				a.revokeDelegation(t, item.ID)
			}
		}
		status, body := a.createDelegation(t, map[string]any{"permissions": []string{"read"}})
		requireStatus(t, status, http.StatusCreated, body)

		// D05-20 requires current read, not the exact historical delegation.
		// The cached turn is read-only; proposal/confirmation execution keeps
		// its exact-delegation revalidation.
		assertReplayedTurn(t, env, a, conversation, object.ID, key, first, provider, 2, 1, 1)
	})

	t.Run("delegation expired and reauthorized", func(t *testing.T) {
		provider := &scriptedProvider{replies: []string{`{"reply":"first turn","proposed_actions":[]}`}}
		env := newTestEnvWithProvider(t, nil, provider)
		a := env.register(t, "g03replayexpired")
		object := a.createObject(t, "collaborative", "Replay expired", map[string]any{"body": "body"})
		conversation := a.createConversation(t, "Replay expired chat", &object.ID)
		key := newIdempotencyKey()
		status, first, _ := sendTurnMessage(t, a, conversation, key, "hello")
		requireStatus(t, status, http.StatusOK, first)

		env.exec(t, `
UPDATE delegation_grants
   SET created_at = now() - interval '2 days',
       expires_at = now() - interval '1 day'
 WHERE user_id = $1 AND revoked_at IS NULL`, a.id)
		status, body, _ := sendTurnMessage(t, a, conversation, key, "hello")
		requireStatus(t, status, http.StatusNotFound, body)
		requireCode(t, body, "not_found")

		status, body = a.createDelegation(t, map[string]any{"permissions": []string{"read"}})
		requireStatus(t, status, http.StatusCreated, body)
		assertReplayedTurn(t, env, a, conversation, object.ID, key, first, provider, 2, 1, 1)
	})

	t.Run("membership read lost", func(t *testing.T) {
		provider := &scriptedProvider{replies: []string{`{"reply":"first turn","proposed_actions":[]}`}}
		env := newTestEnvWithProvider(t, nil, provider)
		a := env.register(t, "g03replayowner")
		b := env.register(t, "g03replayreader")
		object := a.createObject(t, "collaborative", "Replay membership", map[string]any{"body": "body"})
		invite := a.createInvitation(t, object.ID, []string{"read"}, 1)
		status, body := b.acceptInvitation(t, *invite.Token)
		requireStatus(t, status, http.StatusOK, body)
		conversation := b.createConversation(t, "Replay membership chat", &object.ID)
		key := newIdempotencyKey()
		status, first, _ := sendTurnMessage(t, b, conversation, key, "hello")
		requireStatus(t, status, http.StatusOK, first)
		if got := provider.requestCount(); got != 1 {
			t.Fatalf("provider calls = %d", got)
		}

		removeStatus, removeBody, _ := a.call(t, http.MethodDelete,
			"/api/v1/objects/"+object.ID+"/members/"+b.id.String(), callOptions{})
		requireStatus(t, removeStatus, http.StatusNoContent, removeBody)
		status, body, _ = sendTurnMessage(t, b, conversation, key, "hello")
		requireStatus(t, status, http.StatusNotFound, body)
		requireCode(t, body, "not_found")
		if got := provider.requestCount(); got != 1 {
			t.Fatalf("denied replay made %d provider calls", got)
		}
		if got := turnMessageCount(t, env, conversation); got != 2 {
			t.Fatalf("messages after denied replay = %d", got)
		}
		if got := turnObjectVersion(t, env, object.ID); got != 1 {
			t.Fatalf("object version after denied replay = %d", got)
		}
	})

	t.Run("cross user and cross profile", func(t *testing.T) {
		provider := &scriptedProvider{replies: []string{
			`{"reply":"A turn","proposed_actions":[]}`,
			`{"reply":"B turn","proposed_actions":[]}`,
		}}
		env := newTestEnvWithProvider(t, nil, provider)
		a := env.register(t, "g03replaycrossa")
		b := env.register(t, "g03replaycrossb")
		object := a.createObject(t, "collaborative", "Replay cross", map[string]any{"body": "body"})
		invite := a.createInvitation(t, object.ID, []string{"read"}, 1)
		status, body := b.acceptInvitation(t, *invite.Token)
		requireStatus(t, status, http.StatusOK, body)
		conversationA := a.createConversation(t, "A chat", &object.ID)
		key := newIdempotencyKey()
		status, firstA, _ := sendTurnMessage(t, a, conversationA, key, "hello")
		requireStatus(t, status, http.StatusOK, firstA)

		// B cannot replay A's conversation, even with the same key.
		status, body, _ = sendTurnMessage(t, b, conversationA, key, "hello")
		requireStatus(t, status, http.StatusNotFound, body)
		if got := provider.requestCount(); got != 1 {
			t.Fatalf("cross-user replay made %d provider calls", got)
		}

		// The same key under B's actor scope is a new turn, not A's replay.
		conversationB := b.createConversation(t, "B chat", &object.ID)
		status, firstB, headers := sendTurnMessage(t, b, conversationB, key, "hello")
		requireStatus(t, status, http.StatusOK, firstB)
		if headers.Get("Idempotency-Replayed") != "" {
			t.Fatal("B's turn was incorrectly marked as A's replay")
		}
		if got := provider.requestCount(); got != 2 {
			t.Fatalf("provider calls after B's turn = %d, want 2", got)
		}
		if got := modelInvocationCount(t, env, a.id); got != 1 {
			t.Fatalf("A invocations = %d", got)
		}
		if got := modelInvocationCount(t, env, b.id); got != 1 {
			t.Fatalf("B invocations = %d", got)
		}
		if got := turnMessageCount(t, env, conversationA); got != 2 {
			t.Fatalf("A messages = %d", got)
		}

		// A cannot select B's profile in the message body.
		profileStatus, profileBody, _ := a.call(t, http.MethodPost,
			"/api/v1/ai/conversations/"+conversationA+"/messages",
			callOptions{IdempotencyKey: newIdempotencyKey(), Body: map[string]any{
				"content": "hello", "ai_profile_id": b.profileID(t),
			}})
		requireStatus(t, profileStatus, http.StatusBadRequest, profileBody)
		requireCode(t, profileBody, "invalid_request")
		if got := provider.requestCount(); got != 2 {
			t.Fatalf("cross-profile attempt made %d provider calls", got)
		}
	})

	t.Run("concurrent revocation and replay", func(t *testing.T) {
		provider := &scriptedProvider{replies: []string{`{"reply":"first turn","proposed_actions":[]}`}}
		env := newTestEnvWithProvider(t, nil, provider)
		a := env.register(t, "g03replayrace")
		object := a.createObject(t, "collaborative", "Replay race", map[string]any{"body": "body"})
		conversation := a.createConversation(t, "Replay race chat", &object.ID)
		key := newIdempotencyKey()
		status, first, _ := sendTurnMessage(t, a, conversation, key, "hello")
		requireStatus(t, status, http.StatusOK, first)

		tx, err := env.store.Pool().Begin(t.Context())
		if err != nil {
			t.Fatalf("begin lock tx: %v", err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		rows, err := tx.Query(t.Context(), `
SELECT id FROM delegation_grants
 WHERE user_id = $1 AND revoked_at IS NULL
   FOR UPDATE`, a.id.String())
		if err != nil {
			t.Fatalf("lock delegations: %v", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				t.Fatalf("scan delegation: %v", err)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate delegations: %v", err)
		}

		type outcome struct {
			status int
			body   []byte
		}
		result := make(chan outcome, 1)
		go func() {
			status, body, _ := sendTurnMessage(t, a, conversation, key, "hello")
			result <- outcome{status: status, body: body}
		}()

		deadline := time.Now().Add(5 * time.Second)
		for {
			select {
			case early := <-result:
				t.Fatalf("replay was not blocked by the in-flight revocation: %d %s",
					early.status, early.body)
			default:
			}
			var blocked int
			if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM pg_stat_activity
 WHERE datname = current_database()
   AND wait_event_type = 'Lock'
   AND query ILIKE '%delegation_grants%'`).Scan(&blocked); err != nil {
				t.Fatalf("inspect waiting queries: %v", err)
			}
			if blocked > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("replay did not block on the delegation lock")
			}
			time.Sleep(25 * time.Millisecond)
		}

		if _, err := tx.Exec(t.Context(), `
UPDATE delegation_grants
   SET revoked_at = GREATEST(now(), created_at)
 WHERE user_id = $1 AND revoked_at IS NULL`, a.id.String()); err != nil {
			t.Fatalf("revoke delegations: %v", err)
		}
		if err := tx.Commit(t.Context()); err != nil {
			t.Fatalf("commit revocation: %v", err)
		}

		select {
		case got := <-result:
			requireStatus(t, got.status, http.StatusNotFound, got.body)
			requireCode(t, got.body, "not_found")
		case <-time.After(10 * time.Second):
			t.Fatal("replay did not finish after revocation committed")
		}
		if got := provider.requestCount(); got != 1 {
			t.Fatalf("race replay made %d provider calls", got)
		}
		if got := turnMessageCount(t, env, conversation); got != 2 {
			t.Fatalf("race replay wrote messages: %d", got)
		}
		if got := turnObjectVersion(t, env, object.ID); got != 1 {
			t.Fatalf("race replay changed object version to %d", got)
		}
	})
}
