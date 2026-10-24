// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

const goldenID = "0192f0c4-5a6b-7c8d-9e0f-123456789abc"

func signedRequest(t *testing.T, secret []byte, keyID, version, invocationID, text string, timestamp time.Time) *http.Request {
	t.Helper()
	input, _ := json.Marshal(map[string]any{
		"object_id": goldenID,
		"text":      text,
		"format":    "plain_text",
	})
	body, _ := json.Marshal(map[string]any{
		"invocation_id": invocationID, "capability_id": fixedCapabilityID,
		"contract_version": fixedContract, "input": json.RawMessage(input),
	})
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	request := httptest.NewRequest(http.MethodPost, "/v1/invoke", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-DW-Provider-Id", fixedProviderID)
	request.Header.Set("X-DW-Capability-Id", fixedCapabilityID)
	request.Header.Set("X-DW-Contract-Version", fixedContract)
	request.Header.Set("X-DW-Invocation-Id", invocationID)
	request.Header.Set("X-DW-Timestamp", ts)
	request.Header.Set("X-DW-Signature", hex.EncodeToString(mac.Sum(nil)))
	if keyID != "" {
		request.Header.Set("X-DW-Key-Id", keyID)
	}
	if version != "" {
		request.Header.Set("X-DW-Response-Signature-Version", version)
	}
	return request
}

func runProviderRequest(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func requireResponseSignature(t *testing.T, recorder *httptest.ResponseRecorder, request *http.Request,
	secret []byte, keyID, invocationID string) {
	t.Helper()
	if got := recorder.Header().Get("X-DW-Response-Signature-Version"); got != responseSignatureVersion {
		t.Fatalf("response signature version = %q", got)
	}
	if got := recorder.Header().Get("X-DW-Key-Id"); got != keyID {
		t.Fatalf("response key id = %q, want %q", got, keyID)
	}
	want := signProviderResponse(secret, recorder.Code, request.Header.Get("X-DW-Timestamp"),
		invocationID, recorder.Body.Bytes())
	if got := recorder.Header().Get("X-DW-Response-Signature"); got != want {
		t.Fatalf("response signature = %q, want %q", got, want)
	}
}

func TestResponseSigningRequiredRejectsLegacyRequest(t *testing.T) {
	cfg := testConfig()
	cfg.requireResponseSigning = true
	request := signedRequest(t, testSecret, "k1", "", goldenID, "hello", time.Now())
	recorder := runProviderRequest(newHandler(cfg), request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	requireResponseSignature(t, recorder, request, testSecret, "k1", goldenID)
}

func TestResponseSignedWhenNegotiated(t *testing.T) {
	request := signedRequest(t, testSecret, "k1", responseSignatureVersion, goldenID, "hello", time.Now())
	recorder := runProviderRequest(newHandler(testConfig()), request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	requireResponseSignature(t, recorder, request, testSecret, "k1", goldenID)
}

func TestLegacyResponseIsUnsignedWhenNotRequired(t *testing.T) {
	request := signedRequest(t, testSecret, "k1", "", goldenID, "hello", time.Now())
	recorder := runProviderRequest(newHandler(testConfig()), request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if recorder.Header().Get("X-DW-Response-Signature") != "" {
		t.Fatal("legacy response was signed without negotiation")
	}
}

func TestPreviousKeyAcceptedBeforeDeadline(t *testing.T) {
	currentSecret := []byte("current-secret-0123456789abcdef0001")
	previousSecret := []byte("previous-secret-0123456789abcdef001")
	cfg := testConfig()
	cfg.secret = currentSecret
	cfg.keyID = "k2"
	cfg.previousSecret = previousSecret
	cfg.previousKeyID = "k1"
	cfg.previousUntil = time.Now().Add(time.Hour)
	request := signedRequest(t, previousSecret, "k1", responseSignatureVersion, goldenID, "hello", time.Now())
	recorder := runProviderRequest(newHandler(cfg), request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	requireResponseSignature(t, recorder, request, previousSecret, "k1", goldenID)
}

func TestPreviousKeyRejectedAfterDeadline(t *testing.T) {
	currentSecret := []byte("current-secret-0123456789abcdef0001")
	previousSecret := []byte("previous-secret-0123456789abcdef001")
	cfg := testConfig()
	cfg.secret = currentSecret
	cfg.keyID = "k2"
	cfg.previousSecret = previousSecret
	cfg.previousKeyID = "k1"
	cfg.previousUntil = time.Now().Add(-time.Second)
	request := signedRequest(t, previousSecret, "k1", responseSignatureVersion, goldenID, "hello", time.Now())
	recorder := runProviderRequest(newHandler(cfg), request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 after the rotation deadline", recorder.Code)
	}
}

func TestCurrentKeyStillAcceptedDuringRotation(t *testing.T) {
	currentSecret := []byte("current-secret-0123456789abcdef0001")
	previousSecret := []byte("previous-secret-0123456789abcdef001")
	cfg := testConfig()
	cfg.secret = currentSecret
	cfg.keyID = "k2"
	cfg.previousSecret = previousSecret
	cfg.previousKeyID = "k1"
	cfg.previousUntil = time.Now().Add(time.Hour)
	request := signedRequest(t, currentSecret, "k2", responseSignatureVersion, goldenID, "hello", time.Now())
	recorder := runProviderRequest(newHandler(cfg), request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	requireResponseSignature(t, recorder, request, currentSecret, "k2", goldenID)
}

func TestUnknownKeyIDRejected(t *testing.T) {
	request := signedRequest(t, testSecret, "unknown", responseSignatureVersion, goldenID, "hello", time.Now())
	recorder := runProviderRequest(newHandler(testConfig()), request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestLegacyRequestWithoutKeyIDUsesCurrentKey(t *testing.T) {
	request := signedRequest(t, testSecret, "", responseSignatureVersion, goldenID, "hello", time.Now())
	recorder := runProviderRequest(newHandler(testConfig()), request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	requireResponseSignature(t, recorder, request, testSecret, "k1", goldenID)
}

func TestReplayIsSignedForTheReplayRequest(t *testing.T) {
	handler := newHandler(testConfig())
	first := signedRequest(t, testSecret, "k1", responseSignatureVersion, goldenID, "hello", time.Now())
	firstRecorder := runProviderRequest(handler, first)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d", firstRecorder.Code)
	}
	second := signedRequest(t, testSecret, "k1", responseSignatureVersion, goldenID, "hello", time.Now())
	secondRecorder := runProviderRequest(handler, second)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("replay status = %d", secondRecorder.Code)
	}
	requireResponseSignature(t, secondRecorder, second, testSecret, "k1", goldenID)
}

func TestProviderResponseSignatureGolden(t *testing.T) {
	got := signProviderResponse(testSecret, http.StatusOK, "1700000000",
		goldenID, []byte(`{"a":1}`))
	want := "374ac9214815b0dc1ee8651e52e9580f99a5d27e639527c4f01042a5a90ff86f"
	if got != want {
		t.Fatalf("provider response signature = %s, want %s", got, want)
	}
}

func TestProviderKeyIDValidation(t *testing.T) {
	for _, value := range []string{"", "K1", "k 1", "-k1", "abcdefghijklmnopq"} {
		if validProviderKeyID(value) {
			t.Fatalf("key id %q accepted", value)
		}
	}
	for _, value := range []string{"k1", "k2-rotate", "0"} {
		if !validProviderKeyID(value) {
			t.Fatalf("key id %q rejected", value)
		}
	}
}
