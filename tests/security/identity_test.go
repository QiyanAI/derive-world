// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/httpx"
)

func TestRegistrationCreatesCompleteDigitalSelf(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "register")

	profileCount := env.count(t, `SELECT count(*) FROM personal_ai_profiles WHERE user_id=$1`, user.id.String())
	if profileCount != 1 {
		t.Fatalf("personal_ai_profiles = %d, want 1", profileCount)
	}
	if got := env.count(t, `SELECT count(*) FROM private_ai_contexts WHERE user_id=$1`, user.id.String()); got != 1 {
		t.Fatalf("private_ai_contexts = %d, want 1", got)
	}
	var permissions []string
	var constraints []byte
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT permissions, constraints FROM delegation_grants WHERE user_id=$1`, user.id.String()).
		Scan(&permissions, &constraints); err != nil {
		t.Fatalf("read default delegation: %v", err)
	}
	if strings.Join(permissions, ",") != "read,contribute,derive" {
		t.Fatalf("default delegation permissions = %v", permissions)
	}
	if !strings.Contains(string(constraints), "require_confirmation") {
		t.Fatalf("default delegation constraints = %s", constraints)
	}
	if got := env.count(t, `SELECT count(*) FROM user_sessions WHERE user_id=$1 AND revoked_at IS NULL`, user.id.String()); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='auth.register' AND actor_user_id=$1 AND decision='allow'`, user.id.String()); got != 1 {
		t.Fatalf("auth.register audit rows = %d, want 1", got)
	}

	status, body, _ := user.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), "password_hash") || strings.Contains(string(body), "token_hash") {
		t.Fatalf("me response leaks credential fields: %s", body)
	}
	var me struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
		PersonalAI struct {
			ID string `json:"id"`
		} `json:"personal_ai"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.User.ID != user.id.String() || me.User.Email != user.email || me.PersonalAI.ID == "" {
		t.Fatalf("me response = %+v", me)
	}

	// Duplicate email is a conflict with no distinguishing text.
	origin := env.origin
	status, body, _ = env.call(t, newClient(t), http.MethodPost, "/api/v1/auth/register", callOptions{
		Origin: &origin,
		Body: map[string]any{
			"email": user.email, "handle": "other" + strings.ReplaceAll(user.id.String(), "-", "")[:12],
			"display_name": "Duplicate", "password": "correct horse battery staple",
		},
	})
	requireStatus(t, status, http.StatusConflict, body)
	requireCode(t, body, string(httpx.CodeConflict))

	// Weak password is rejected before any row exists.
	status, body, _ = env.call(t, newClient(t), http.MethodPost, "/api/v1/auth/register", callOptions{
		Origin: &origin,
		Body: map[string]any{
			"email":        "weak." + strings.ReplaceAll(user.id.String(), "-", "")[:8] + "@example.test",
			"handle":       "weak" + strings.ReplaceAll(user.id.String(), "-", "")[:8],
			"display_name": "Weak", "password": "short",
		},
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, string(httpx.CodeInvalidRequest))
}

func TestSessionIsolationLogoutAndRotation(t *testing.T) {
	env := newTestEnv(t)
	userA := env.register(t, "sessiona")
	userB := env.register(t, "sessionb")

	if userA.id == userB.id {
		t.Fatal("two registrations produced the same user id")
	}
	status, body, _ := userA.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusOK, body)

	// A forged opaque token never authenticates.
	forged := newClient(t)
	origin := env.origin
	status, body, forgedHeaders := env.call(t, forged, http.MethodGet, "/api/v1/auth/me", callOptions{
		Origin:  &origin,
		Headers: map[string]string{"Cookie": "dw_session=forged-token-value"},
	})
	requireStatus(t, status, http.StatusUnauthorized, body)
	requireCode(t, body, string(httpx.CodeUnauthenticated))
	if forgedHeaders.Get("X-RateLimit-Limit") == "" || forgedHeaders.Get("X-RateLimit-Reset") == "" {
		t.Fatal("401 response is missing rate limit headers")
	}
	if got := env.count(t, `SELECT count(*) FROM user_sessions WHERE user_id=$1`, userA.id.String()); got != 1 {
		t.Fatalf("forged cookie created a session; count = %d", got)
	}

	// Logout revokes exactly the current session and clears both cookies.
	status, body, logoutHeaders := userA.call(t, http.MethodPost, "/api/v1/auth/logout", callOptions{})
	requireStatus(t, status, http.StatusNoContent, body)
	cleared := strings.Join(logoutHeaders.Values("Set-Cookie"), "; ")
	if !strings.Contains(cleared, "dw_session=") || !strings.Contains(cleared, "dw_csrf=") ||
		!strings.Contains(cleared, "Max-Age=0") || !strings.Contains(cleared, "Expires=Thu, 01 Jan 1970") {
		t.Fatalf("logout did not clear both cookies: %s", cleared)
	}
	if logoutHeaders.Get("Cache-Control") != "no-store" {
		t.Fatalf("logout cache control = %q", logoutHeaders.Get("Cache-Control"))
	}
	status, body, _ = userA.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusUnauthorized, body)
	if got := env.count(t, `SELECT count(*) FROM user_sessions WHERE user_id=$1 AND revoked_at IS NOT NULL`, userA.id.String()); got != 1 {
		t.Fatalf("revoked sessions = %d, want 1", got)
	}

	// B's session remains valid after A's logout.
	status, body, _ = userB.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusOK, body)

	// Wrong password is uniform and audited.
	_, status, body = env.login(t, userA.email, "definitely the wrong password")
	requireStatus(t, status, http.StatusUnauthorized, body)
	requireCode(t, body, string(httpx.CodeUnauthenticated))
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='auth.login_failed' AND decision='deny'`); got < 1 {
		t.Fatal("failed login was not audited")
	}

	// Successful login rotates: the old client's cookie stops working.
	relogin, status, body := env.login(t, userA.email, userA.password)
	requireStatus(t, status, http.StatusOK, body)
	status, body, _ = relogin.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	status, body, _ = userA.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusUnauthorized, body)

	// Logging in while presenting a valid session rotates and revokes it.
	serverURL, err := url.Parse(env.server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	var presentedToken string
	for _, cookie := range relogin.client.Jar.Cookies(serverURL) {
		if cookie.Name == "dw_session" {
			presentedToken = cookie.Value
		}
	}
	if presentedToken == "" {
		t.Fatal("relogin session cookie missing")
	}
	oldClient := newClient(t)
	oldClient.Jar.SetCookies(serverURL, []*http.Cookie{{Name: "dw_session", Value: presentedToken, Path: "/"}})
	status, body, _ = relogin.call(t, http.MethodPost, "/api/v1/auth/login", callOptions{
		Body: map[string]any{"email": userA.email, "password": userA.password},
	})
	requireStatus(t, status, http.StatusOK, body)
	status, body, _ = env.call(t, oldClient, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusUnauthorized, body)
}

func TestSessionCookieAttributes(t *testing.T) {
	env := newTestEnv(t)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	origin := env.origin
	status, body, headers := env.call(t, newClient(t), http.MethodPost, "/api/v1/auth/register", callOptions{
		Origin: &origin,
		Body: map[string]any{
			"email": "cookie." + suffix + "@example.test", "handle": "c" + suffix,
			"display_name": "Cookie User", "password": "correct horse battery staple",
		},
	})
	requireStatus(t, status, http.StatusCreated, body)
	var sessionCookie, csrfCookie string
	for _, value := range headers.Values("Set-Cookie") {
		if strings.HasPrefix(value, "dw_session=") {
			sessionCookie = value
		}
		if strings.HasPrefix(value, "dw_csrf=") {
			csrfCookie = value
		}
	}
	if sessionCookie == "" || csrfCookie == "" {
		t.Fatalf("session cookies missing: %v", headers.Values("Set-Cookie"))
	}
	for _, required := range []string{"HttpOnly", "SameSite=Lax", "Path=/", "Max-Age="} {
		if !strings.Contains(sessionCookie, required) {
			t.Fatalf("dw_session missing %s: %s", required, sessionCookie)
		}
	}
	if strings.Contains(csrfCookie, "HttpOnly") {
		t.Fatalf("dw_csrf must be readable by the client: %s", csrfCookie)
	}
	if !strings.Contains(csrfCookie, "SameSite=Lax") || !strings.Contains(csrfCookie, "Path=/") {
		t.Fatalf("dw_csrf attributes: %s", csrfCookie)
	}
	if strings.Contains(string(body), `"dw_session"`) || strings.Contains(string(body), "password_hash") {
		t.Fatalf("registration body leaks credentials: %s", body)
	}
}

func TestSessionIdleExpiry(t *testing.T) {
	env := newTestEnvWith(t, func(cfg *config.Config) {
		cfg.SessionAbsoluteTTL = time.Hour
		// Keep a deterministic margin over PostgreSQL/Go clock scheduling; the
		// assertion still proves that an idle session is rejected.
		cfg.SessionIdleTTL = 4 * time.Second
	})
	user := env.register(t, "idleexpiry")
	status, body, _ := user.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	time.Sleep(6 * time.Second)
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
	requireStatus(t, status, http.StatusUnauthorized, body)
	if got := env.count(t, `SELECT count(*) FROM user_sessions WHERE user_id=$1 AND revoked_at IS NULL`, user.id.String()); got != 1 {
		t.Fatalf("idle session rows = %d, want 1 (lazy expiry preserves the row)", got)
	}
}

func TestCSRFOriginAndActorOverride(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "csrf")
	objectBody := map[string]any{
		"object_type": "collaborative", "title": "CSRF target",
		"content": map[string]any{}, "shared_context": map[string]any{},
	}

	// Missing CSRF header.
	omitted := ""
	origin := env.origin
	status, body, _ := user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: objectBody, IdempotencyKey: newIdempotencyKey(), CSRFToken: &omitted,
	})
	requireStatus(t, status, http.StatusForbidden, body)
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='auth.csrf_failed' AND decision='deny'`); got < 1 {
		t.Fatal("missing CSRF was not audited")
	}

	// Wrong CSRF header.
	wrong := "not-the-session-token"
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: objectBody, IdempotencyKey: newIdempotencyKey(), CSRFToken: &wrong,
	})
	requireStatus(t, status, http.StatusForbidden, body)

	// Cross-site Origin with the correct CSRF value.
	evil := "https://evil.example"
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: objectBody, IdempotencyKey: newIdempotencyKey(), Origin: &evil,
	})
	requireStatus(t, status, http.StatusForbidden, body)
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE state->>'title'='CSRF target'`); got != 0 {
		t.Fatalf("CSRF-rejected writes created %d objects", got)
	}

	// Cross-site login/register are rejected by Sec-Fetch-Site.
	status, body, _ = env.call(t, newClient(t), http.MethodPost, "/api/v1/auth/login", callOptions{
		Origin:  &origin,
		Headers: map[string]string{"Sec-Fetch-Site": "cross-site"},
		Body:    map[string]any{"email": user.email, "password": user.password},
	})
	requireStatus(t, status, http.StatusForbidden, body)
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='auth.origin_rejected' AND decision='deny'`); got < 1 {
		t.Fatal("cross-site login was not audited")
	}

	// Client-supplied owner_id is rejected, not silently ignored.
	raw := []byte(`{"object_type":"collaborative","title":"Override","content":{},"owner_id":"` + user.id.String() + `"}`)
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		RawBody: raw, IdempotencyKey: newIdempotencyKey(),
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, string(httpx.CodeInvalidRequest))
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='authz.actor_override_attempt' AND decision='deny'`); got < 1 {
		t.Fatal("actor override attempt was not audited")
	}
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{
		Headers: map[string]string{"X-Owner-Id": user.id.String()},
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, string(httpx.CodeInvalidRequest))
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='authz.actor_override_attempt' AND decision='deny'`); got < 2 {
		t.Fatalf("authenticated actor override audits = %d, want at least 2", got)
	}
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE state->>'title'='Override' AND creator_id=$1`, user.id.String()); got != 0 {
		t.Fatal("actor override request created an object")
	}

	// A nested authoritative field is also rejected.
	raw = []byte(`{"object_type":"collaborative","title":"Nested","content":{"summary":"x","version":9}}`)
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		RawBody: raw, IdempotencyKey: newIdempotencyKey(),
	})
	requireStatus(t, status, http.StatusBadRequest, body)
}
