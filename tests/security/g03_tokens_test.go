// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/QiyanAI/derive-world/internal/config"
)

type createdTokenResponse struct {
	ID        string   `json:"id"`
	Prefix    string   `json:"token_prefix"`
	Scopes    []string `json:"scopes"`
	Plaintext string   `json:"plaintext"`
}

func (u *testUser) createToken(t *testing.T, name string, scopes []string) createdTokenResponse {
	t.Helper()
	status, body, _ := u.call(t, http.MethodPost, "/api/v1/tokens", callOptions{
		Body: map[string]any{"name": name, "scopes": scopes, "expires_in_days": 1},
	})
	requireStatus(t, status, http.StatusCreated, body)
	var token createdTokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if !strings.HasPrefix(token.Plaintext, "dw_pat_") || len(token.Plaintext) != 50 {
		t.Fatalf("unexpected PAT format: %q", token.Plaintext)
	}
	if token.Prefix != token.Plaintext[:15] {
		t.Fatalf("token prefix = %q, plaintext prefix = %q", token.Prefix, token.Plaintext[:15])
	}
	return token
}

func (e *testEnv) bearerCall(t *testing.T, token, method, path string, options callOptions) (int, []byte, http.Header) {
	t.Helper()
	client := newClient(t)
	if options.Headers == nil {
		options.Headers = map[string]string{}
	}
	options.Headers["Authorization"] = "Bearer " + token
	if method != http.MethodGet && options.Origin == nil {
		origin := e.origin
		options.Origin = &origin
	}
	return e.call(t, client, method, path, options)
}

func TestG03PATScopesCookieOnlyAndRevocation(t *testing.T) {
	// The random-PAT loop below must be judged on authentication, not on the
	// shared anonymous rate bucket. Keep refill high enough that the limiter
	// cannot turn a 401 expectation into a 429 on a fast runner; the dedicated
	// 10,000-token test uses the same setting.
	env := newTestEnvWith(t, func(cfg *config.Config) {
		cfg.RateLimitAPIPerMin = 1_000_000
	})
	a := env.register(t, "g03pat-a")
	object := a.createObject(t, "collaborative", "PAT object", map[string]any{
		"body": "pat body",
	})

	readToken := a.createToken(t, "read only", []string{"objects:read"})
	status, body, headers := env.bearerCall(t, readToken.Plaintext, http.MethodGet,
		"/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if headers.Get("Cache-Control") != "no-store" {
		t.Fatal("PAT response was cacheable")
	}
	meStatus, meBody, _ := env.bearerCall(t, readToken.Plaintext, http.MethodGet,
		"/api/v1/auth/me", callOptions{})
	requireStatus(t, meStatus, http.StatusOK, meBody)
	var me struct {
		AuthMethod string   `json:"auth_method"`
		Scopes     []string `json:"scopes"`
		CSRFToken  string   `json:"csrf_token"`
	}
	if err := json.Unmarshal(meBody, &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.AuthMethod != "pat" || len(me.Scopes) != 1 || me.CSRFToken != "" {
		t.Fatalf("PAT me = %+v", me)
	}

	// Missing scope is a capability failure, not an anonymous request.
	status, body, _ = env.bearerCall(t, readToken.Plaintext, http.MethodPost,
		"/api/v1/objects", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"object_type": "collaborative", "title": "denied",
				"content": map[string]any{},
			},
		})
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, "forbidden")

	// Cookie-only endpoints reject Bearer authentication outright.
	status, body, _ = env.bearerCall(t, readToken.Plaintext, http.MethodGet,
		"/api/v1/ai/profile", callOptions{})
	requireStatus(t, status, http.StatusUnauthorized, body)
	status, body, _ = env.bearerCall(t, readToken.Plaintext, http.MethodGet,
		"/api/v1/tokens", callOptions{})
	requireStatus(t, status, http.StatusUnauthorized, body)

	// PAT metadata never includes the plaintext or hash.
	listStatus, listBody, _ := a.call(t, http.MethodGet, "/api/v1/tokens", callOptions{})
	requireStatus(t, listStatus, http.StatusOK, listBody)
	if strings.Contains(string(listBody), readToken.Plaintext) ||
		strings.Contains(string(listBody), "plaintext") ||
		strings.Contains(string(listBody), "token_hash") {
		t.Fatalf("token metadata leaked secret material: %s", listBody)
	}

	// Write-only PAT can create but cannot read.
	writeToken := a.createToken(t, "write only", []string{"objects:write"})
	status, body, _ = env.bearerCall(t, writeToken.Plaintext, http.MethodPost,
		"/api/v1/objects", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"object_type": "collaborative", "title": "PAT created",
				"content": map[string]any{"body": "created by pat"},
			},
		})
	requireStatus(t, status, http.StatusCreated, body)
	status, body, _ = env.bearerCall(t, writeToken.Plaintext, http.MethodGet,
		"/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusForbidden, body)

	// Revocation takes effect on the next bearer request.
	revokeStatus, revokeBody, _ := a.call(t, http.MethodDelete,
		"/api/v1/tokens/"+readToken.ID, callOptions{})
	requireStatus(t, revokeStatus, http.StatusNoContent, revokeBody)
	status, body, _ = env.bearerCall(t, readToken.Plaintext, http.MethodGet,
		"/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusUnauthorized, body)

	// Random tokens never authenticate.
	for index := 0; index < 100; index++ {
		random := "dw_pat_" + strings.Repeat("A", 43)
		status, body, _ = env.bearerCall(t, random, http.MethodGet,
			"/api/v1/objects", callOptions{})
		if status != http.StatusUnauthorized {
			t.Fatalf("random PAT %d status = %d body=%s", index, status, body)
		}
	}
}

func TestG03PATScopeDenialIsRateLimited(t *testing.T) {
	env := newTestEnvWith(t, func(cfg *config.Config) {
		cfg.RateLimitAPIPerMin = 2
	})
	a := env.register(t, "g03patrl-a")
	token := a.createToken(t, "scope denied", []string{"objects:read"})
	status, body, headers := env.bearerCall(t, token.Plaintext, http.MethodPost,
		"/api/v1/objects", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"object_type": "collaborative", "title": "denied",
				"content": map[string]any{},
			},
		})
	requireStatus(t, status, http.StatusForbidden, body)
	if headers.Get("X-RateLimit-Limit") == "" {
		t.Fatal("scope-denied response is missing rate-limit headers")
	}
	status, body, headers = env.bearerCall(t, token.Plaintext, http.MethodPost,
		"/api/v1/objects", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"object_type": "collaborative", "title": "denied again",
				"content": map[string]any{},
			},
		})
	requireStatus(t, status, http.StatusTooManyRequests, body)
	requireCode(t, body, "rate_limited")
	if headers.Get("X-RateLimit-Limit") == "" {
		t.Fatal("429 response is missing rate-limit headers")
	}
}
