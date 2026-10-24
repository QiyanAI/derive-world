// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QiyanAI/derive-world/internal/config"
)

func TestG03ObjectSubresourcesUse404Parity(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03par-a")
	b := env.register(t, "g03par-b")
	object := a.createObject(t, "collaborative", "Parity object", map[string]any{
		"body": "private body",
	})
	randomID := "018f0000-0000-7000-8000-000000000099"
	paths := []string{
		"/api/v1/objects/" + object.ID,
		"/api/v1/objects/" + object.ID + "/events",
		"/api/v1/objects/" + object.ID + "/members",
		"/api/v1/objects/" + object.ID + "/lineage",
		"/api/v1/objects/" + object.ID + "/handoff",
	}
	for _, path := range paths {
		status, body, _ := b.call(t, http.MethodGet, path, callOptions{})
		requireStatus(t, status, http.StatusNotFound, body)
		requireCode(t, body, "not_found")
		if strings.Contains(string(body), "Parity object") ||
			strings.Contains(string(body), "private body") {
			t.Fatalf("%s leaked object content", path)
		}
	}
	// A random UUID and an unauthorized existing object produce the same
	// message and code; request_id is intentionally unique.
	_, missingBody, _ := b.call(t, http.MethodGet, "/api/v1/objects/"+randomID, callOptions{})
	var existingEnvelope, missingEnvelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_, existingBody, _ := b.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	if err := json.Unmarshal(existingBody, &existingEnvelope); err != nil {
		t.Fatalf("decode existing: %v", err)
	}
	if err := json.Unmarshal(missingBody, &missingEnvelope); err != nil {
		t.Fatalf("decode missing: %v", err)
	}
	if existingEnvelope.Error.Code != missingEnvelope.Error.Code ||
		existingEnvelope.Error.Message != missingEnvelope.Error.Message {
		t.Fatalf("404 parity differs: existing=%+v missing=%+v",
			existingEnvelope.Error, missingEnvelope.Error)
	}
}

func TestG03AccessPolicyCannotAuthorize(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03pol-a")
	b := env.register(t, "g03pol-b")
	object := a.createObject(t, "collaborative", "Policy object", map[string]any{
		"body": "policy body",
	})
	// ST-04 requires an actual public-policy row. The frozen schema normally
	// pins visibility=private, so temporarily remove the shape constraint,
	// create the adversarial row, prove authorization still ignores it, and
	// restore the constraint/policy in cleanup.
	var definition string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT pg_get_constraintdef(oid) FROM pg_constraint
 WHERE conname = 'living_objects_access_policy_shape'`).Scan(&definition); err != nil {
		t.Fatalf("read access_policy constraint: %v", err)
	}
	env.exec(t, `ALTER TABLE living_objects DROP CONSTRAINT living_objects_access_policy_shape`)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = env.store.Pool().Exec(ctx, `
UPDATE living_objects SET access_policy =
 '{"visibility":"private","invite_enabled":true,"derive_link_enabled":true}'::jsonb
 WHERE id = $1`, object.ID)
		_, _ = env.store.Pool().Exec(ctx,
			`ALTER TABLE living_objects ADD CONSTRAINT living_objects_access_policy_shape `+definition)
	})
	env.exec(t, `
UPDATE living_objects
   SET access_policy = '{"visibility":"public","invite_enabled":true,"derive_link_enabled":true}'::jsonb
 WHERE id = $1`, mustUUID(t, object.ID))
	status, body, _ := b.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
	requireCode(t, body, "not_found")
}

func TestG03RandomPATDoesNotAuthenticate(t *testing.T) {
	env := newTestEnvWith(t, func(cfg *config.Config) {
		cfg.RateLimitAPIPerMin = 1_000_000
	})
	client := newClient(t)
	for index := 0; index < 10_000; index++ {
		randomBytes := make([]byte, 32)
		if _, err := rand.Read(randomBytes); err != nil {
			t.Fatalf("random PAT %d: %v", index, err)
		}
		token := "dw_pat_" + base64.RawURLEncoding.EncodeToString(randomBytes)
		if len(token) != 50 {
			t.Fatalf("random PAT %d has length %d", index, len(token))
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
			env.server.URL+"/api/v1/objects", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("request %d: %v", index, err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("random PAT %d status = %d", index, response.StatusCode)
		}
		if index == 0 && response.Header.Get("X-RateLimit-Limit") == "" {
			t.Fatal("invalid bearer response is missing rate-limit headers")
		}
	}
}

func TestG03BodylessEndpointsRejectAuthoritativeFields(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03body-a")
	confirmationID := "018f0000-0000-7000-8000-0000000000aa"
	status, body, _ := a.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body:           map[string]any{"delegation_id": confirmationID},
		})
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, "invalid_request")
	if env.count(t, `SELECT count(*) FROM audit_events
 WHERE action='authz.actor_override_attempt' AND actor_user_id=$1`, a.id) != 1 {
		t.Fatal("body actor override was not audited")
	}
}

func TestG03DeriveSourceVersionFailureIsIdempotent(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03dv-a")
	object := a.createObject(t, "collaborative", "Derive idempotency object", map[string]any{
		"body": "body",
	})
	key := newIdempotencyKey()
	body := map[string]any{
		"title": "invalid source", "source_version": 99,
		"inheritance_fields": []string{"body"},
	}
	status, response, _ := a.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/derive", callOptions{
			IdempotencyKey: key, Body: body,
		})
	requireStatus(t, status, http.StatusBadRequest, response)
	requireCode(t, response, "invalid_request")
	replayStatus, replayBody, headers := a.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/derive", callOptions{
			IdempotencyKey: key, Body: body,
		})
	requireStatus(t, replayStatus, http.StatusBadRequest, replayBody)
	if headers.Get("Idempotency-Replayed") != "true" {
		t.Fatal("source_version failure was not replayed for the same key/body")
	}
	changed := map[string]any{
		"title": "changed source", "source_version": 99,
		"inheritance_fields": []string{"body"},
	}
	conflictStatus, conflictBody, _ := a.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/derive", callOptions{
			IdempotencyKey: key, Body: changed,
		})
	requireStatus(t, conflictStatus, http.StatusConflict, conflictBody)
	requireCode(t, conflictBody, "idempotency_conflict")
}
