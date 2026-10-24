// SPDX-License-Identifier: Apache-2.0
package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/model"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<!doctype html><html><body><div id=\"root\"></div></body></html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(webRoot, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webRoot, "assets", "app.js"), []byte("console.log('dw');"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	cfg := &config.Config{
		AppEnv:             "test",
		WebDistDir:         webRoot,
		MaxRequestBytes:    1 << 20,
		IPHashPepper:       "test-pepper",
		TrustProxyHeaders:  false,
		CORSAllowedOrigins: nil,
		Model: config.ModelConfig{
			Mode:            "fixture",
			Timeout:         time.Minute,
			MaxOutputTokens: 128,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, err := New(Options{
		Config:    cfg,
		Logger:    logger,
		Model:     model.NewFixture(""),
		Version:   "test-version",
		Commit:    "test-commit",
		BuildTime: "test-time",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return application
}

func perform(app *App, method, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	app.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestHealthz(t *testing.T) {
	recorder := perform(newTestApp(t), http.MethodGet, "/healthz")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if recorder.Body.String() != "ok\n" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("content type = %q", got)
	}
}

func TestMeta(t *testing.T) {
	recorder := perform(newTestApp(t), http.MethodGet, "/api/v1/meta")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body metaResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version != "test-version" || body.ModelMode != "fixture" {
		t.Fatalf("meta = %+v", body)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q", got)
	}
}

func TestReadyzWithoutStore(t *testing.T) {
	recorder := perform(newTestApp(t), http.MethodGet, "/readyz")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "unavailable" {
		t.Fatalf("body = %v", body)
	}
}

func TestStaticIndexAndSPAFallback(t *testing.T) {
	app := newTestApp(t)
	for _, target := range []string{"/", "/objects/abc", "/settings"} {
		recorder := perform(app, http.MethodGet, target)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d", target, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), `id="root"`) {
			t.Fatalf("%s did not serve index.html", target)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s cache control = %q", target, got)
		}
	}
}

func TestHashedAssetCache(t *testing.T) {
	recorder := perform(newTestApp(t), http.MethodGet, "/assets/app.js")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("cache control = %q", got)
	}
}

func TestAPINotFoundIsJSON(t *testing.T) {
	recorder := perform(newTestApp(t), http.MethodGet, "/api/v1/does-not-exist")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body struct {
		Error httpx.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != httpx.CodeNotFound {
		t.Fatalf("code = %s", body.Error.Code)
	}
}

func TestMethodNotAllowedIsJSON(t *testing.T) {
	recorder := perform(newTestApp(t), http.MethodPost, "/healthz")
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body struct {
		Error httpx.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != httpx.CodeInvalidRequest {
		t.Fatalf("code = %s", body.Error.Code)
	}
}

func TestSecurityHeadersOnAPI(t *testing.T) {
	recorder := perform(newTestApp(t), http.MethodGet, "/api/v1/meta")
	if got := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Fatalf("CSP = %q", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q", got)
	}
}

func TestRequestIDHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-Id", "client-request-0001")
	newTestApp(t).Handler().ServeHTTP(recorder, request)
	if got := recorder.Header().Get("X-Request-Id"); got != "client-request-0001" {
		t.Fatalf("request ID = %q", got)
	}
}

func TestOpenAPIDocumentServed(t *testing.T) {
	recorder := perform(newTestApp(t), http.MethodGet, "/api/v1/openapi.yaml")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/yaml") {
		t.Fatalf("content type = %q", got)
	}
	if !strings.Contains(recorder.Body.String(), "openapi: 3.1.0") {
		t.Fatalf("unexpected OpenAPI body")
	}
}

func TestNewRequiresWebIndex(t *testing.T) {
	cfg := &config.Config{
		AppEnv:     "test",
		WebDistDir: t.TempDir(),
		Model:      config.ModelConfig{Mode: "fixture"},
	}
	if _, err := New(Options{Config: cfg}); err == nil {
		t.Fatal("expected missing index.html error")
	}
}

func TestBusinessMethodNotAllowedIsJSON(t *testing.T) {
	recorder := perform(newTestApp(t), http.MethodPut, "/api/v1/objects")
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body struct {
		Error httpx.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != httpx.CodeInvalidRequest {
		t.Fatalf("code = %s", body.Error.Code)
	}
}

func TestNormalizedOrigin(t *testing.T) {
	cases := map[string]string{
		"https://derive.example.com:443": "https://derive.example.com",
		"http://derive.example.com:80":   "http://derive.example.com",
		"http://127.0.0.1:8080":          "http://127.0.0.1:8080",
		"https://Derive.Example.com":     "https://derive.example.com",
		"http://[::1]:8080":              "http://[::1]:8080",
	}
	for input, want := range cases {
		got, ok := normalizedOrigin(input)
		if !ok || got != want {
			t.Fatalf("normalizedOrigin(%q) = %q,%v want %q", input, got, ok, want)
		}
	}
	if _, ok := normalizedOrigin("not a url"); ok {
		t.Fatal("invalid origin was accepted")
	}
}
