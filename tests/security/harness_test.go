// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/app"
	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/model"
	"github.com/QiyanAI/derive-world/internal/store"
)

type testEnv struct {
	t       *testing.T
	app     *app.App
	store   *store.Store
	runtime *store.Store
	server  *httptest.Server
	origin  string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWith(t, nil)
}

func newTestEnvWith(t *testing.T, mutate func(*config.Config)) *testEnv {
	t.Helper()
	return newTestEnvWithProvider(t, mutate, nil)
}

func newTestEnvWithProvider(t *testing.T, mutate func(*config.Config), provider model.Provider) *testEnv {
	t.Helper()
	return newTestEnvWithCapabilities(t, mutate, provider, app.Options{})
}

func newTestEnvWithCapabilities(t *testing.T, mutate func(*config.Config), provider model.Provider,
	extra app.Options) *testEnv {
	t.Helper()
	database := openTestStore(t)
	ctx := t.Context()
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	runtimeStore, err := store.OpenWithRole(ctx, os.Getenv("DATABASE_URL"), store.AppRole)
	if err != nil {
		t.Fatalf("open runtime role store: %v", err)
	}
	t.Cleanup(runtimeStore.Close)
	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<!doctype html><html><body><div id=\"root\"></div></body></html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	cfg := &config.Config{
		AppEnv:               "test",
		PublicBaseURL:        "http://derive.test",
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		WebDistDir:           webRoot,
		CursorHMACSecret:     "test-only-cursor-hmac-secret-0000000001",
		IPHashPepper:         "test-only-ip-hash-pepper-000000000001",
		CapabilityHMACSecret: "test-only-capability-hmac-secret-000001",
		CapabilitiesConfig:   filepath.Join(securityRepoRoot(t), "capabilities.yaml"),
		Model: config.ModelConfig{
			Mode: "fixture", Timeout: time.Minute, MaxOutputTokens: 128,
		},
		LogLevel:            "error",
		LogFormat:           "json",
		RateLimitAuthPerMin: 100000,
		RateLimitAPIPerMin:  100000,
		SessionAbsoluteTTL:  7 * 24 * time.Hour,
		SessionIdleTTL:      24 * time.Hour,
		MaxRequestBytes:     1 << 20,
		ShutdownTimeout:     5 * time.Second,
	}
	if mutate != nil {
		mutate(cfg)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	extra.Config = cfg
	extra.Logger = logger
	extra.Store = runtimeStore
	if extra.Model == nil {
		extra.Model = provider
	}
	extra.Version = "integration"
	extra.Commit = "integration"
	extra.BuildTime = "integration"
	application, err := app.New(extra)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	server := httptest.NewServer(application.Handler())
	t.Cleanup(server.Close)
	return &testEnv{t: t, app: application, store: database, runtime: runtimeStore, server: server, origin: cfg.PublicBaseURL}
}

func securityRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate harness file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

type callOptions struct {
	Body           any
	RawBody        []byte
	CSRFToken      *string
	Origin         *string
	IdempotencyKey string
	Headers        map[string]string
}

type testUser struct {
	env      *testEnv
	client   *http.Client
	id       uuid.UUID
	email    string
	handle   string
	password string
	csrf     string
}

func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &http.Client{Jar: jar, Timeout: 20 * time.Second}
}

func (e *testEnv) call(t *testing.T, client *http.Client, method, path string, options callOptions) (int, []byte, http.Header) {
	t.Helper()
	var reader io.Reader
	if options.RawBody != nil {
		reader = bytes.NewReader(options.RawBody)
	} else if options.Body != nil {
		encoded, err := json.Marshal(options.Body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(t.Context(), method, e.server.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if options.Body != nil || options.RawBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if options.Origin != nil {
		request.Header.Set("Origin", *options.Origin)
	}
	if options.CSRFToken != nil {
		request.Header.Set("X-CSRF-Token", *options.CSRFToken)
	}
	if options.IdempotencyKey != "" {
		request.Header.Set("Idempotency-Key", options.IdempotencyKey)
	}
	for key, value := range options.Headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return response.StatusCode, body, response.Header.Clone()
}

func (u *testUser) call(t *testing.T, method, path string, options callOptions) (int, []byte, http.Header) {
	t.Helper()
	if options.Origin == nil {
		origin := u.env.origin
		options.Origin = &origin
	}
	if options.CSRFToken == nil && method != http.MethodGet && method != http.MethodHead {
		token := u.csrf
		options.CSRFToken = &token
	}
	return u.env.call(t, u.client, method, path, options)
}

func (e *testEnv) register(t *testing.T, label string) *testUser {
	t.Helper()
	client := newClient(t)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	email := label + "." + suffix + "@example.test"
	handle := "u" + suffix
	password := "correct horse battery staple"
	origin := e.origin
	status, body, _ := e.call(t, client, http.MethodPost, "/api/v1/auth/register", callOptions{
		Origin: &origin,
		Body: map[string]any{
			"email": email, "handle": handle,
			"display_name": "User " + label, "password": password,
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("register status = %d body = %s", status, body)
	}
	var response struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	id, err := uuid.Parse(response.User.ID)
	if err != nil {
		t.Fatalf("parse user id: %v", err)
	}
	if response.CSRFToken == "" {
		t.Fatal("registration did not return a CSRF token")
	}
	return &testUser{
		env: e, client: client, id: id, email: email, handle: handle,
		password: password, csrf: response.CSRFToken,
	}
}

func (e *testEnv) login(t *testing.T, email, password string) (*testUser, int, []byte) {
	t.Helper()
	client := newClient(t)
	origin := e.origin
	status, body, _ := e.call(t, client, http.MethodPost, "/api/v1/auth/login", callOptions{
		Origin: &origin,
		Body:   map[string]any{"email": email, "password": password},
	})
	if status != http.StatusOK {
		return nil, status, body
	}
	var response struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	id, _ := uuid.Parse(response.User.ID)
	return &testUser{
		env: e, client: client, id: id, email: email, password: password,
		csrf: response.CSRFToken,
	}, status, body
}

type objectResponse struct {
	ID                   string                 `json:"id"`
	ObjectType           string                 `json:"object_type"`
	PresentationTemplate string                 `json:"presentation_template"`
	SchemaVersion        int                    `json:"schema_version"`
	CreatorID            string                 `json:"creator_id"`
	OwnerID              string                 `json:"owner_id"`
	Version              int                    `json:"version"`
	State                map[string]any         `json:"state"`
	SharedContext        map[string]any         `json:"shared_context"`
	Status               string                 `json:"status"`
	Permissions          []string               `json:"permissions"`
	Role                 string                 `json:"role"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
	Lineage              *objectLineageResponse `json:"lineage"`
}

type objectLineageResponse struct {
	Parent struct {
		ObjectID string `json:"object_id"`
		Version  int    `json:"version"`
	} `json:"parent"`
	InheritedFields []string `json:"inherited_fields"`
}

func newIdempotencyKey() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
}

func (u *testUser) createObject(t *testing.T, objectType, title string, content map[string]any) objectResponse {
	t.Helper()
	status, body, _ := u.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"object_type":    objectType,
			"title":          title,
			"content":        content,
			"shared_context": map[string]any{},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create object status = %d body = %s", status, body)
	}
	var object objectResponse
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatalf("decode object: %v", err)
	}
	return object
}

func (u *testUser) postAction(t *testing.T, objectID, action string, input map[string]any, expectedVersion int) (int, []byte, objectResponse) {
	t.Helper()
	status, body, _ := u.call(t, http.MethodPost, "/api/v1/objects/"+objectID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action": action, "input": input, "expected_version": expectedVersion,
		},
	})
	var object objectResponse
	if status == http.StatusOK {
		if err := json.Unmarshal(body, &object); err != nil {
			t.Fatalf("decode action object: %v", err)
		}
	}
	return status, body, object
}

func (e *testEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var value int
	if err := e.store.Pool().QueryRow(t.Context(), query, args...).Scan(&value); err != nil {
		t.Fatalf("count query: %v\n%s", err, query)
	}
	return value
}

func (e *testEnv) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := e.store.Pool().Exec(t.Context(), query, args...); err != nil {
		t.Fatalf("exec: %v\n%s", err, query)
	}
}

func (e *testEnv) addMembership(t *testing.T, objectID, userID uuid.UUID, permissions []string) {
	t.Helper()
	e.exec(t, `
INSERT INTO object_memberships (object_id, user_id, permissions, granted_by)
VALUES ($1,$2,$3,$2)
ON CONFLICT (object_id, user_id) DO UPDATE
   SET permissions = EXCLUDED.permissions, revoked_at = NULL, granted_by = EXCLUDED.granted_by`,
		objectID.String(), userID.String(), permissions)
}

func (e *testEnv) blockAuditAction(t *testing.T, action string) {
	t.Helper()
	constraint := "test_block_audit_" + strings.NewReplacer(".", "_", "-", "_").Replace(action)
	e.exec(t, `ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS `+constraint)
	e.exec(t, `ALTER TABLE audit_events ADD CONSTRAINT `+constraint+` CHECK (action <> '`+action+`') NOT VALID`)
	t.Cleanup(func() {
		_, _ = e.store.Pool().Exec(context.Background(), `ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS `+constraint)
	})
}

func (e *testEnv) revokeMembership(t *testing.T, objectID, userID uuid.UUID) {
	t.Helper()
	e.exec(t, `
UPDATE object_memberships SET revoked_at = GREATEST(now(), created_at)
 WHERE object_id = $1 AND user_id = $2`, objectID.String(), userID.String())
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, body)
	}
	return envelope.Error.Code
}

func decodeObject(t *testing.T, body []byte) objectResponse {
	t.Helper()
	var object objectResponse
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatalf("decode object: %v", err)
	}
	return object
}

func mustUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(value)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", value, err)
	}
	return parsed
}

func requireStatus(t *testing.T, got, want int, body []byte) {
	t.Helper()
	if got != want {
		t.Fatalf("status = %d, want %d, body=%s", got, want, body)
	}
}

func requireCode(t *testing.T, body []byte, want string) {
	t.Helper()
	if got := errorCode(t, body); got != want {
		t.Fatalf("error code = %q, want %q, body=%s", got, want, body)
	}
}

func jsonField[T any](t *testing.T, value map[string]any, key string) T {
	t.Helper()
	raw, ok := value[key]
	if !ok {
		t.Fatalf("missing key %q in %v", key, value)
	}
	encoded, _ := json.Marshal(raw)
	var out T
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("decode key %q: %v", key, err)
	}
	return out
}

var _ = fmt.Sprintf
