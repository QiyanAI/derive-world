// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/app"
	"github.com/QiyanAI/derive-world/internal/capability"
	"github.com/QiyanAI/derive-world/internal/config"
)

type g04ProviderMode int

const (
	g04Success g04ProviderMode = iota
	g04InvalidOutput
	g04UnknownResult
	g04Slow
	g04Hold
	g04Oversized
	g04SemanticInvalid
	g04UnsignedResponse
	g04TamperedResponse
)

type g04Provider struct {
	server   *httptest.Server
	secret   []byte
	mode     atomic.Int32
	requests atomic.Int32
}

func newG04Provider(t *testing.T) *g04Provider {
	t.Helper()
	provider := &g04Provider{secret: []byte("test-only-capability-hmac-secret-000001")}
	provider.mode.Store(int32(g04Success))
	provider.server = httptest.NewServer(http.HandlerFunc(provider.handle))
	t.Cleanup(provider.server.Close)
	return provider
}

func (p *g04Provider) setMode(mode g04ProviderMode) {
	p.mode.Store(int32(mode))
}

func (p *g04Provider) appOptions() app.Options {
	return app.Options{CapabilityExternal: capability.ExternalClientOptions{
		Resolver:  g04Resolver{},
		Transport: g04Transport(p.server.Listener.Addr().String()),
	}}
}

type g04Resolver struct{}

func (g04Resolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("172.18.0.2")}}, nil
}

func g04Transport(target string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", target)
		},
	}
}

func (p *g04Provider) handle(w http.ResponseWriter, r *http.Request) {
	p.requests.Add(1)
	if r.Method != http.MethodPost || r.URL.Path != "/v1/invoke" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var envelope struct {
		InvocationID    string          `json:"invocation_id"`
		CapabilityID    string          `json:"capability_id"`
		ContractVersion string          `json:"contract_version"`
		Input           json.RawMessage `json:"input"`
	}
	looseErr := json.Unmarshal(body, &envelope)
	if !p.verify(r, body) {
		p.writeSigned(w, r, http.StatusUnauthorized, envelope.InvocationID,
			[]byte(`{"error":{"code":"invalid_request","message":"invalid request signature"}}`))
		return
	}
	if looseErr != nil {
		p.writeSigned(w, r, http.StatusBadRequest, "",
			[]byte(`{"error":{"code":"invalid_request","message":"invalid invocation envelope"}}`))
		return
	}
	mode := g04ProviderMode(p.mode.Load())
	switch mode {
	case g04UnknownResult:
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		conn, _, _ := hijacker.Hijack()
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 4096\r\n\r\n{\"invocation_id\":"))
		_ = conn.Close()
		return
	case g04Slow:
		time.Sleep(800 * time.Millisecond)
	case g04Hold:
		time.Sleep(700 * time.Millisecond)
	}
	var input struct {
		Text   string `json:"text"`
		Format string `json:"format"`
	}
	_ = json.Unmarshal(envelope.Input, &input)
	output := map[string]any{
		"items": []map[string]any{{
			"index": 0, "kind": "paragraph", "text": truncateTestText(input.Text),
		}},
		"item_count": 1, "truncated": false, "format": input.Format,
	}
	if mode == g04InvalidOutput {
		output = map[string]any{"items": []map[string]any{}}
	}
	if mode == g04SemanticInvalid {
		output = map[string]any{
			"items":      []map[string]any{{"index": 0, "kind": "paragraph", "text": "one"}},
			"item_count": 2, "truncated": false, "format": input.Format,
		}
	}
	if mode == g04Oversized {
		items := make([]map[string]any, 0, 500)
		for index := 0; index < 500; index++ {
			items = append(items, map[string]any{
				"index": index, "kind": "paragraph", "text": strings.Repeat("x", 8000),
			})
		}
		output = map[string]any{
			"items": items, "item_count": 500, "truncated": false, "format": input.Format,
		}
	}
	response := map[string]any{
		"invocation_id": envelope.InvocationID, "capability_id": envelope.CapabilityID,
		"contract_version": envelope.ContractVersion, "status": "succeeded",
		"output": output, "duration_ms": 1,
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		p.writeSigned(w, r, http.StatusInternalServerError, envelope.InvocationID,
			[]byte(`{"error":{"code":"capability_failed","message":"encode failed"}}`))
		return
	}
	switch mode {
	case g04UnsignedResponse:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(encoded)
		return
	case g04TamperedResponse:
		timestamp := r.Header.Get("X-DW-Timestamp")
		bodyHash := sha256.Sum256(encoded)
		mac := hmac.New(sha256.New, p.secret)
		mac.Write([]byte("v2\n200\n" + timestamp + "\n" + envelope.InvocationID + "\n" + hex.EncodeToString(bodyHash[:])))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-DW-Response-Signature-Version", "v2")
		w.Header().Set("X-DW-Key-Id", "k1")
		w.Header().Set("X-DW-Response-Signature", hex.EncodeToString(mac.Sum(nil)))
		tampered := bytes.Replace(encoded, []byte(`"item_count":1`), []byte(`"item_count":9`), 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(tampered)
		return
	}
	p.writeSigned(w, r, http.StatusOK, envelope.InvocationID, encoded)
}

// writeSigned implements the v2 response profile used by the Broker.
func (p *g04Provider) writeSigned(w http.ResponseWriter, r *http.Request, status int, invocationID string, body []byte) {
	timestamp := r.Header.Get("X-DW-Timestamp")
	bodyHash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, p.secret)
	mac.Write([]byte("v2\n" + strconv.Itoa(status) + "\n" + timestamp + "\n" + invocationID + "\n" + hex.EncodeToString(bodyHash[:])))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-DW-Response-Signature-Version", "v2")
	w.Header().Set("X-DW-Key-Id", "k1")
	w.Header().Set("X-DW-Response-Signature", hex.EncodeToString(mac.Sum(nil)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (p *g04Provider) verify(r *http.Request, body []byte) bool {
	timestamp := r.Header.Get("X-DW-Timestamp")
	signature := r.Header.Get("X-DW-Signature")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(seconds, 0)) > 30*time.Second {
		return false
	}
	mac := hmac.New(sha256.New, p.secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return false
	}
	if r.Header.Get("X-DW-Provider-Id") != "reference.content-structure" ||
		r.Header.Get("X-DW-Capability-Id") != "content.structure" ||
		r.Header.Get("X-DW-Contract-Version") != "1.0" {
		return false
	}
	return true
}

func truncateTestText(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return "empty"
	}
	if len(runes) > 100 {
		runes = runes[:100]
	}
	return string(runes)
}

func newG04Env(t *testing.T, mode g04ProviderMode) (*testEnv, *g04Provider) {
	t.Helper()
	provider := newG04Provider(t)
	provider.setMode(mode)
	mutate := func(cfg *config.Config) {}
	if mode == g04Slow || mode == g04Hold {
		mutate = func(cfg *config.Config) {
			cfg.CapabilitiesConfig = g04ConfigWithTimeout(t, mode)
		}
	}
	env := newTestEnvWithCapabilities(t, mutate, nil, provider.appOptions())
	return env, provider
}

func g04ConfigWithTimeout(t *testing.T, mode g04ProviderMode) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(securityRepoRoot(t), "capabilities.yaml"))
	if err != nil {
		t.Fatalf("read capabilities.yaml: %v", err)
	}
	timeout := "300"
	if mode == g04Hold {
		timeout = "2000"
	}
	text := strings.Replace(string(raw), "timeout_ms: 10000", "timeout_ms: "+timeout, 1)
	path := filepath.Join(t.TempDir(), "capabilities.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write capabilities.yaml: %v", err)
	}
	return path
}

type capabilityInvocationResponse struct {
	ID              string          `json:"id"`
	CapabilityID    string          `json:"capability_id"`
	ContractVersion string          `json:"contract_version"`
	ProviderID      string          `json:"provider_id"`
	Status          string          `json:"status"`
	Output          json.RawMessage `json:"output"`
	ErrorCode       *string         `json:"error_code"`
	DurationMS      *int            `json:"duration_ms"`
	ConfirmationID  string          `json:"confirmation_id"`
	CreatedAt       string          `json:"created_at"`
	CompletedAt     *string         `json:"completed_at"`
}

func (u *testUser) invokeCapability(t *testing.T, capabilityID string,
	input map[string]any, key string) (int, []byte, http.Header) {
	t.Helper()
	return u.call(t, http.MethodPost,
		"/api/v1/capabilities/"+capabilityID+"/invoke",
		callOptions{Body: input, IdempotencyKey: key})
}

func decodeInvocation(t *testing.T, body []byte) capabilityInvocationResponse {
	t.Helper()
	var response capabilityInvocationResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode invocation: %v", err)
	}
	return response
}

func exactContentInput(t *testing.T, target int, base string) []byte {
	t.Helper()
	prefix := `{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","text":"`
	suffix := `","format":"markdown"}`
	slot := target - len(prefix) - len(suffix)
	if slot < 1 {
		t.Fatalf("target %d is too small", target)
	}
	encodedBase, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal base: %v", err)
	}
	per := len(encodedBase) - 2
	count := slot / per
	pad := slot - count*per
	text := strings.Repeat(base, count) + strings.Repeat("a", pad)
	encodedText, err := json.Marshal(text)
	if err != nil {
		t.Fatalf("marshal text: %v", err)
	}
	if len(encodedText)-2 != slot {
		t.Fatalf("text slot = %d, want %d", len(encodedText)-2, slot)
	}
	input := []byte(prefix + string(encodedText[1:len(encodedText)-1]) + suffix)
	if len(input) != target {
		t.Fatalf("input = %d, want %d", len(input), target)
	}
	return input
}

func (e *testEnv) invocationRow(t *testing.T, invocationID string) (status string, output []byte, errorCode *string, objectID *string) {
	t.Helper()
	err := e.store.Pool().QueryRow(t.Context(), `
SELECT status, output, error_code, object_id
  FROM capability_invocations WHERE id = $1`, mustUUID(t, invocationID)).Scan(
		&status, &output, &errorCode, &objectID)
	if err != nil {
		t.Fatalf("query invocation: %v", err)
	}
	return status, output, errorCode, objectID
}

func (e *testEnv) latestInvocation(t *testing.T, userID uuid.UUID, capabilityID string) (string, string, []byte, *string) {
	t.Helper()
	var (
		id        string
		status    string
		output    []byte
		errorCode *string
	)
	err := e.store.Pool().QueryRow(t.Context(), `
SELECT id, status, output, error_code
  FROM capability_invocations
 WHERE actor_user_id = $1 AND capability_id = $2
 ORDER BY created_at DESC, id DESC
 LIMIT 1`, userID.String(), capabilityID).Scan(&id, &status, &output, &errorCode)
	if err != nil {
		t.Fatalf("query latest invocation: %v", err)
	}
	return id, status, output, errorCode
}

func (e *testEnv) countInvocations(t *testing.T, userID uuid.UUID, capabilityID string) int {
	t.Helper()
	var count int
	if err := e.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM capability_invocations
 WHERE actor_user_id = $1 AND capability_id = $2`,
		userID.String(), capabilityID).Scan(&count); err != nil {
		t.Fatalf("count invocations: %v", err)
	}
	return count
}

func (e *testEnv) auditCount(t *testing.T, action string, userID uuid.UUID) int {
	t.Helper()
	var count int
	if err := e.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events WHERE action = $1 AND actor_user_id = $2`,
		action, userID.String()).Scan(&count); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	return count
}

func decodeCapabilityError(t *testing.T, body []byte) (string, string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, body)
	}
	invocationID, _ := envelope.Error.Details["invocation_id"].(string)
	return envelope.Error.Code, invocationID
}
