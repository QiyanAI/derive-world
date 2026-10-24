// SPDX-License-Identifier: Apache-2.0
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusForClosedSet(t *testing.T) {
	cases := map[ErrorCode]int{
		CodeInvalidRequest:       http.StatusBadRequest,
		CodeUnauthenticated:      http.StatusUnauthorized,
		CodeForbidden:            http.StatusForbidden,
		CodeNotFound:             http.StatusNotFound,
		CodeConflict:             http.StatusConflict,
		CodeVersionConflict:      http.StatusConflict,
		CodeIdempotencyConflict:  http.StatusConflict,
		CodeRequestInProgress:    http.StatusConflict,
		CodeConfirmationRequired: http.StatusAccepted,
		CodeConfirmationExpired:  http.StatusGone,
		CodeInvitationInvalid:    http.StatusNotFound,
		CodeRateLimited:          http.StatusTooManyRequests,
		CodeBudgetExceeded:       http.StatusTooManyRequests,
		CodeModelUnavailable:     http.StatusBadGateway,
		CodeModelTimeout:         http.StatusGatewayTimeout,
		CodeModelInvalidOutput:   http.StatusBadGateway,
		CodeCapabilityDenied:     http.StatusForbidden,
		CodeCapabilityFailed:     http.StatusBadGateway,
		CodeCapabilityTimeout:    http.StatusGatewayTimeout,
		CodeCapabilityUnknown:    http.StatusBadGateway,
		CodePayloadTooLarge:      http.StatusRequestEntityTooLarge,
		CodeUnsupportedMediaType: http.StatusUnsupportedMediaType,
		CodeInternal:             http.StatusInternalServerError,
	}
	for code, want := range cases {
		if got := StatusFor(code); got != want {
			t.Errorf("StatusFor(%s) = %d, want %d", code, got, want)
		}
	}
}

func TestWriteErrorEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	request = request.WithContext(WithRequestID(request.Context(), "req-test-0001"))

	WriteError(recorder, request, NewError(CodeNotFound, "not found"))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	var body struct {
		Error ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != CodeNotFound || body.Error.RequestID != "req-test-0001" {
		t.Fatalf("unexpected envelope: %+v", body.Error)
	}
	if body.Error.Details == nil {
		t.Fatal("details must be an object, not null")
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Fatalf("CSP missing: %q", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q", got)
	}
	if got := recorder.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("referrer policy = %q", got)
	}
}

func TestRequestID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFromContext(r.Context()) == "" {
			t.Error("request ID missing from context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-Id", "client-request-0001")
	handler.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("X-Request-Id"); got != "client-request-0001" {
		t.Fatalf("request ID = %q", got)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-Id", "bad id with spaces")
	handler.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("X-Request-Id"); got == "bad id with spaces" || got == "" {
		t.Fatalf("invalid request ID was not replaced: %q", got)
	}
}

func TestAccessLogUsesRouteTemplate(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger("info", "json", &output)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := Chain(
		inner,
		RequestID,
		AccessLog(logger, "pepper", false),
		Recover(logger),
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/objects/very-secret-object-id", nil)
	request.Pattern = "/api/v1/objects/{id}"
	request.RemoteAddr = "192.0.2.1:12345"
	handler.ServeHTTP(recorder, request)

	logged := output.String()
	if !strings.Contains(logged, "/api/v1/objects/{id}") {
		t.Fatalf("route template missing from log: %s", logged)
	}
	if strings.Contains(logged, "very-secret-object-id") {
		t.Fatalf("raw path leaked into log: %s", logged)
	}
	if strings.Contains(logged, "192.0.2.1") {
		t.Fatalf("raw IP leaked into log: %s", logged)
	}
	if !strings.Contains(logged, `"request_id"`) {
		t.Fatalf("request_id missing: %s", logged)
	}
}

func TestLoggerRedactsSensitiveKeys(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger("info", "json", &output)
	logger.Info("test",
		"authorization", "Bearer super-secret-token",
		"object_state", "private content",
		"normal", "visible",
	)
	logged := output.String()
	if strings.Contains(logged, "super-secret-token") || strings.Contains(logged, "private content") {
		t.Fatalf("sensitive value leaked: %s", logged)
	}
	if !strings.Contains(logged, "[REDACTED]") || !strings.Contains(logged, "visible") {
		t.Fatalf("redaction output unexpected: %s", logged)
	}
}

func TestCursorRoundTripAndTamper(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	raw, err := EncodeCursor(secret, Cursor{Key: "42", ID: "018f0000-0000-7000-8000-000000000000"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeCursor(secret, raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Key != "42" {
		t.Fatalf("decoded key = %q", decoded.Key)
	}

	tampered := raw[:len(raw)-1] + "A"
	if _, err := DecodeCursor(secret, tampered); err == nil {
		t.Fatal("tampered cursor accepted")
	}
	if _, err := DecodeCursor([]byte("another-secret-another-secret-32"), raw); err == nil {
		t.Fatal("wrong secret accepted")
	}
}

func TestLimiterBurst(t *testing.T) {
	limiter := NewLimiter(2, time.Minute)
	if _, _, ok := limiter.Allow("user-1"); !ok {
		t.Fatal("first request denied")
	}
	if _, _, ok := limiter.Allow("user-1"); !ok {
		t.Fatal("second request denied")
	}
	if _, reset, ok := limiter.Allow("user-1"); ok {
		t.Fatal("third request allowed")
	} else if reset.Before(time.Now()) {
		t.Fatalf("reset hint is in the past: %v", reset)
	}
	if _, _, ok := limiter.Allow("user-2"); !ok {
		t.Fatal("independent key denied")
	}
}

func TestDecodeJSONRejectsUnknownAndLarge(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok","extra":true}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	var dst payload
	err := DecodeJSON(recorder, request, &dst, 1024)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != CodeInvalidRequest {
		t.Fatalf("unknown field error = %v", err)
	}

	request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"a very long value"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	err = DecodeJSON(recorder, request, &dst, 8)
	if !errors.As(err, &apiErr) || apiErr.Code != CodePayloadTooLarge {
		t.Fatalf("large body error = %v", err)
	}

	request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok"}`))
	request.Header.Set("Content-Type", "text/plain")
	recorder = httptest.NewRecorder()
	err = DecodeJSON(recorder, request, &dst, 1024)
	if !errors.As(err, &apiErr) || apiErr.Code != CodeUnsupportedMediaType {
		t.Fatalf("content type error = %v", err)
	}
}

func TestCORSExactOrigin(t *testing.T) {
	handler := CORS([]string{"https://example.com"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", "https://example.com")
	handler.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("credentials must not be allowed: %q", got)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected CORS header for disallowed origin: %q", got)
	}
}

func TestRecoverWritesInternalError(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger("info", "json", &output)
	handler := Chain(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		RequestID,
		AccessLog(logger, "pepper", false),
		Recover(logger),
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if !strings.Contains(output.String(), "panic_recovered") {
		t.Fatalf("panic not logged: %s", output.String())
	}
}

func TestActorContext(t *testing.T) {
	ctx := WithActor(context.Background(), Actor{UserID: "user-1", Type: "user"})
	actor := ActorFromContext(ctx)
	if actor.UserID != "user-1" || actor.Type != "user" {
		t.Fatalf("actor = %+v", actor)
	}
}

func TestHashIPIsStableAndPeppered(t *testing.T) {
	a := HashIP("pepper-a", "192.0.2.1")
	b := HashIP("pepper-b", "192.0.2.1")
	if a == b {
		t.Fatal("different peppers produced the same hash")
	}
	if len(a) != 16 {
		t.Fatalf("hash length = %d, want 16 hex chars", len(a))
	}
	if a != HashIP("pepper-a", "192.0.2.1") {
		t.Fatal("hash is not stable")
	}
}

func TestNewLoggerText(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger("debug", "text", &output)
	logger.Debug("hello", "k", "v")
	if !strings.Contains(output.String(), "hello") {
		t.Fatalf("text logger output missing: %s", output.String())
	}
}

func TestAccessLogRecordsAuthenticatedActor(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger("info", "json", &output)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		SetActor(w, Actor{UserID: "0192f0a1-7c3e-7a4b-9d21-3f5c8e0a1b2c", Type: "user"})
		w.WriteHeader(http.StatusOK)
	})
	handler := Chain(inner, RequestID, AccessLog(logger, "pepper", false))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	handler.ServeHTTP(recorder, request)

	logged := output.String()
	if !strings.Contains(logged, `"actor_user_id":"0192f0a1-7c3e-7a4b-9d21-3f5c8e0a1b2c"`) {
		t.Fatalf("actor user id missing from log: %s", logged)
	}
	if !strings.Contains(logged, `"actor_type":"user"`) {
		t.Fatalf("actor type missing from log: %s", logged)
	}
}

func TestLimiterBurstIsIndependentOfRate(t *testing.T) {
	limiter := NewLimiterWithBurst(120, time.Minute, 60)
	for index := 0; index < 60; index++ {
		if _, _, ok := limiter.Allow("user"); !ok {
			t.Fatalf("request %d was limited before the burst", index)
		}
	}
	if _, _, ok := limiter.Allow("user"); ok {
		t.Fatal("request 61 exceeded the frozen burst of 60")
	}
}
