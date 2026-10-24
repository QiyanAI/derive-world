// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/httpx"
)

func TestAPIValidationNegatives(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "negative")
	object := user.createObject(t, "collaborative", "Negative target", map[string]any{
		"summary": "", "body": "", "checklist": []map[string]any{}, "tags": []string{},
	})
	validBody := map[string]any{
		"object_type": "collaborative", "title": "Negative create",
		"content": map[string]any{}, "shared_context": map[string]any{},
	}

	// Unauthenticated object access fails before resource loading.
	anonymous := newClient(t)
	origin := env.origin
	status, body, _ := env.call(t, anonymous, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{Origin: &origin})
	requireStatus(t, status, http.StatusUnauthorized, body)
	requireCode(t, body, string(httpx.CodeUnauthenticated))

	// Wrong media type.
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: validBody, IdempotencyKey: newIdempotencyKey(),
		Headers: map[string]string{"Content-Type": "text/plain"},
	})
	requireStatus(t, status, http.StatusUnsupportedMediaType, body)
	requireCode(t, body, string(httpx.CodeUnsupportedMediaType))

	// Malformed JSON.
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		RawBody: []byte(`{"object_type":`), IdempotencyKey: newIdempotencyKey(),
	})
	requireStatus(t, status, http.StatusBadRequest, body)

	// Body over the 1 MiB request limit.
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		RawBody: bytes.Repeat([]byte("x"), (1<<20)+1024), IdempotencyKey: newIdempotencyKey(),
	})
	requireStatus(t, status, http.StatusRequestEntityTooLarge, body)
	requireCode(t, body, string(httpx.CodePayloadTooLarge))

	// Invalid idempotency key format.
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: validBody, IdempotencyKey: "short",
	})
	requireStatus(t, status, http.StatusBadRequest, body)

	// Authoritative fields in the query string and in headers.
	queryKey := newIdempotencyKey()
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects?permissions=manage", callOptions{
		Body: validBody, IdempotencyKey: queryKey,
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, string(httpx.CodeInvalidRequest))
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: validBody, IdempotencyKey: newIdempotencyKey(),
		Headers: map[string]string{"X-Owner-Id": user.id.String()},
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='authz.actor_override_attempt' AND decision='deny'`); got < 2 {
		t.Fatalf("actor override audits = %d, want at least 2", got)
	}

	// List and lineage parameter validation.
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects?scope=nope", callOptions{})
	requireStatus(t, status, http.StatusBadRequest, body)
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects?cursor=not-a-cursor", callOptions{})
	requireStatus(t, status, http.StatusBadRequest, body)
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+object.ID+"/lineage?depth=11", callOptions{})
	requireStatus(t, status, http.StatusBadRequest, body)
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+uuid.NewString()+"/lineage", callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)

	// Idempotency keys are mandatory on action and derive.
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/actions", callOptions{
		Body: map[string]any{"action": "set_title", "input": map[string]any{"title": "x"}, "expected_version": 1},
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/derive", callOptions{
		Body: map[string]any{"title": "x", "inheritance_fields": []string{"summary"}},
	})
	requireStatus(t, status, http.StatusBadRequest, body)

	// Missing expected_version and unknown actions.
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/actions", callOptions{
		Body:           map[string]any{"action": "set_title", "input": map[string]any{"title": "x"}},
		IdempotencyKey: newIdempotencyKey(),
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/actions", callOptions{
		Body: map[string]any{
			"action": "drop_table", "input": map[string]any{}, "expected_version": 1,
		},
		IdempotencyKey: newIdempotencyKey(),
	})
	requireStatus(t, status, http.StatusBadRequest, body)

	// Validation errors carry a JSON Pointer for details.field.
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"object_type": "collaborative", "title": "Pointer",
			"content": map[string]any{"unknown": 1}, "shared_context": map[string]any{},
		},
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	var pointerEnvelope struct {
		Error struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &pointerEnvelope); err != nil {
		t.Fatalf("decode pointer envelope: %v", err)
	}
	if pointerEnvelope.Error.Details["field"] != "/content" {
		t.Fatalf("details.field = %v, want /content", pointerEnvelope.Error.Details["field"])
	}
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE state->>'title'='Pointer' AND creator_id=$1`, user.id.String()); got != 0 {
		t.Fatal("schema-invalid create persisted a row")
	}

	// Malformed object id is hidden as 404, not 400.
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/not-a-uuid", callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
}

func TestRateLimitsAndAudit(t *testing.T) {
	env := newTestEnvWith(t, func(cfg *config.Config) {
		cfg.RateLimitAuthPerMin = 2
		cfg.RateLimitAPIPerMin = 2
	})
	user := env.register(t, "ratelimit")
	origin := env.origin

	// Registration consumed one auth-IP token; a wrong login consumes the
	// second, and the next authentication request is limited.
	_, status, body := env.login(t, user.email, "wrong password value")
	requireStatus(t, status, http.StatusUnauthorized, body)
	status, body, _ = env.call(t, newClient(t), http.MethodPost, "/api/v1/auth/login", callOptions{
		Origin: &origin,
		Body:   map[string]any{"email": user.email, "password": user.password},
	})
	requireStatus(t, status, http.StatusTooManyRequests, body)
	requireCode(t, body, string(httpx.CodeRateLimited))

	// The API limiter is per user. The first two requests succeed, the third
	// is limited with the frozen headers.
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	status, body, headers := user.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if headers.Get("X-RateLimit-Limit") == "" || headers.Get("X-RateLimit-Reset") == "" {
		t.Fatal("rate limit headers are missing")
	}
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusTooManyRequests, body)
	requireCode(t, body, string(httpx.CodeRateLimited))
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='rate.limit_hit' AND decision='deny'`); got < 2 {
		t.Fatalf("rate limit audits = %d, want at least 2", got)
	}
	if !strings.Contains(string(body), "rate_limited") {
		t.Fatalf("body = %s", body)
	}
}
