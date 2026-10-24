// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/store"
)

func TestIdempotencyLeaseTakeover(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "lease")
	body := map[string]any{
		"object_type": "collaborative", "title": "Lease takeover",
		"content": map[string]any{}, "shared_context": map[string]any{},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	key := newIdempotencyKey()
	hash, err := store.RequestHash(http.MethodPost, "/api/v1/objects", encoded)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := env.store.Idempotency().Claim(t.Context(), user.id, "/api/v1/objects", key, hash)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !claim.Reserved {
		t.Fatal("first claim was not reserved")
	}
	// Simulate a crashed holder whose ten-minute lease expired.
	env.exec(t, `
UPDATE idempotency_keys SET lease_until = now() - interval '1 minute'
 WHERE actor_user_id = $1 AND route = '/api/v1/objects' AND key = $2`,
		user.id.String(), key)

	status, response, _ := user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: body, IdempotencyKey: key,
	})
	requireStatus(t, status, http.StatusCreated, response)
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE owner_id=$1 AND state->>'title'='Lease takeover'`, user.id.String()); got != 1 {
		t.Fatalf("objects after lease takeover = %d, want 1", got)
	}
	if got := env.count(t, `
SELECT count(*) FROM idempotency_keys
 WHERE actor_user_id=$1 AND route='/api/v1/objects' AND key=$2 AND status_code=201`,
		user.id.String(), key); got != 1 {
		t.Fatal("lease takeover did not complete the idempotency record")
	}
}

func TestExpiredCompletedIdempotencyRecordIsANewRequest(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "expiredkey")
	key := newIdempotencyKey()
	oldHash, _ := store.RequestHash(http.MethodPost, "/api/v1/objects", []byte(`{"old":true}`))
	env.exec(t, `
INSERT INTO idempotency_keys (
    actor_user_id, route, key, request_hash, status_code, response_body,
    response_truncated, lease_until, created_at, completed_at, expires_at
) VALUES ($1,'/api/v1/objects',$2,$3,201,'{"id":"old"}',false,
          now() - interval '2 days', now() - interval '2 days',
          now() - interval '2 days', now() - interval '1 day')`,
		user.id.String(), key, oldHash)

	status, body, _ := user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: map[string]any{
			"object_type": "collaborative", "title": "Post expiry",
			"content": map[string]any{}, "shared_context": map[string]any{},
		},
		IdempotencyKey: key,
	})
	requireStatus(t, status, http.StatusCreated, body)
	created := decodeObject(t, body)
	if created.ID == "old" {
		t.Fatal("expired record was replayed")
	}
}

func TestIdempotencyTruncatesOversizedResponses(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "truncate")
	key := newIdempotencyKey()
	hash, _ := store.RequestHash(http.MethodPost, "/api/v1/objects/"+user.id.String()+"/derive", []byte(`{}`))
	claim, err := env.store.Idempotency().Claim(t.Context(), user.id, "/api/v1/objects/{id}/derive", key, hash)
	if err != nil || !claim.Reserved {
		t.Fatalf("claim = %+v err=%v", claim, err)
	}
	large := []byte(`{"blob":"` + strings.Repeat("x", 300*1024) + `"}`)
	if err := env.store.WithTx(t.Context(), func(tx pgx.Tx) error {
		return env.store.Idempotency().Complete(t.Context(), tx, user.id, "/api/v1/objects/{id}/derive", key, claim.LeaseUntil, 201, large)
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	replay, err := env.store.Idempotency().Claim(t.Context(), user.id, "/api/v1/objects/{id}/derive", key, hash)
	if err != nil {
		t.Fatalf("replay claim: %v", err)
	}
	if !replay.Replay || !replay.Truncated || replay.StatusCode != http.StatusOK {
		t.Fatalf("replay = %+v", replay)
	}
	var truncatedBody map[string]any
	if err := json.Unmarshal(replay.Body, &truncatedBody); err != nil {
		t.Fatalf("decode truncated body: %v", err)
	}
	if truncatedBody["truncated"] != true {
		t.Fatalf("truncated body = %s", replay.Body)
	}
}

func TestActionReplayAndCrossUserKeyIsolation(t *testing.T) {
	env := newTestEnv(t)
	userA := env.register(t, "idema")
	userB := env.register(t, "idemb")
	object := userA.createObject(t, "collaborative", "Action replay", map[string]any{
		"summary": "", "body": "", "checklist": []map[string]any{}, "tags": []string{},
	})
	key := newIdempotencyKey()
	body := map[string]any{
		"action": "set_title", "input": map[string]any{"title": "Replay action"}, "expected_version": 1,
	}
	status, first, _ := userA.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/actions", callOptions{
		Body: body, IdempotencyKey: key,
	})
	requireStatus(t, status, http.StatusOK, first)
	status, replay, headers := userA.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/actions", callOptions{
		Body: body, IdempotencyKey: key,
	})
	requireStatus(t, status, http.StatusOK, replay)
	if headers.Get("Idempotency-Replayed") != "true" {
		t.Fatal("action replay is missing Idempotency-Replayed")
	}
	if decodeObject(t, first).ID != decodeObject(t, replay).ID || decodeObject(t, replay).Version != 2 {
		t.Fatalf("action replay result = %+v", decodeObject(t, replay))
	}
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1`, object.ID); got != 2 {
		t.Fatalf("action events = %d, want 2 (created + one state change)", got)
	}
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='object.action' AND resource_id=$1`, object.ID); got != 1 {
		t.Fatalf("object.action audits = %d, want 1", got)
	}

	// The same key is scoped by actor: another user's identical request must
	// not replay the first user's object.
	crossKey := newIdempotencyKey()
	crossTitle := "Cross-user key " + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	crossBody := map[string]any{
		"object_type": "collaborative", "title": crossTitle,
		"content": map[string]any{}, "shared_context": map[string]any{},
	}
	status, firstObject, _ := userA.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: crossBody, IdempotencyKey: crossKey,
	})
	requireStatus(t, status, http.StatusCreated, firstObject)
	status, secondObject, _ := userB.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: crossBody, IdempotencyKey: crossKey,
	})
	requireStatus(t, status, http.StatusCreated, secondObject)
	if decodeObject(t, firstObject).ID == decodeObject(t, secondObject).ID {
		t.Fatal("different actors replayed the same idempotent response")
	}
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE state->>'title'=$1`, crossTitle); got != 2 {
		t.Fatalf("cross-user objects = %d, want 2", got)
	}
}
