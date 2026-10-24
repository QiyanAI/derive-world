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
	"strings"
	"sync"
	"testing"
	"time"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef")

func testConfig() config {
	return config{
		secret: testSecret, providerID: fixedProviderID, contract: fixedContract,
		maxBodyBytes: defaultMaxBody, replaySize: 128,
	}
}

type signedEnvelope struct {
	body         []byte
	invocationID string
	timestamp    time.Time
	signature    string
	providerID   string
	capabilityID string
	contract     string
}

func newEnvelope(t *testing.T, invocationID, text, format string, timestamp time.Time) signedEnvelope {
	t.Helper()
	input, _ := json.Marshal(map[string]any{
		"object_id": "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		"text":      text,
		"format":    format,
	})
	body, _ := json.Marshal(map[string]any{
		"invocation_id": invocationID, "capability_id": fixedCapabilityID,
		"contract_version": fixedContract, "input": json.RawMessage(input),
	})
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	mac := hmac.New(sha256.New, testSecret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return signedEnvelope{
		body: body, invocationID: invocationID, timestamp: timestamp,
		signature:  hex.EncodeToString(mac.Sum(nil)),
		providerID: fixedProviderID, capabilityID: fixedCapabilityID, contract: fixedContract,
	}
}

func (e signedEnvelope) request() *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/v1/invoke", bytes.NewReader(e.body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-DW-Provider-Id", e.providerID)
	request.Header.Set("X-DW-Capability-Id", e.capabilityID)
	request.Header.Set("X-DW-Contract-Version", e.contract)
	request.Header.Set("X-DW-Invocation-Id", e.invocationID)
	request.Header.Set("X-DW-Timestamp", strconv.FormatInt(e.timestamp.Unix(), 10))
	request.Header.Set("X-DW-Signature", e.signature)
	return request
}

func invoke(t *testing.T, handler http.Handler, envelope signedEnvelope) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, envelope.request())
	return recorder
}

func TestHealthz(t *testing.T) {
	recorder := httptest.NewRecorder()
	newHandler(testConfig()).ServeHTTP(recorder,
		httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestInvokeSucceeds(t *testing.T) {
	envelope := newEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		"# Title\n\nbody", "markdown", time.Now())
	recorder := invoke(t, newHandler(testConfig()), envelope)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var response successResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Status != "succeeded" || response.Output.ItemCount != 2 ||
		response.InvocationID != envelope.invocationID {
		t.Fatalf("response = %+v", response)
	}
}

func TestInvokeRejectsBadSignature(t *testing.T) {
	envelope := newEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc", "text", "plain_text", time.Now())
	envelope.signature = strings.Repeat("0", 64)
	recorder := invoke(t, newHandler(testConfig()), envelope)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestInvokeRejectsUppercaseSignature(t *testing.T) {
	envelope := newEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc", "text", "plain_text", time.Now())
	envelope.signature = strings.ToUpper(envelope.signature)
	recorder := invoke(t, newHandler(testConfig()), envelope)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestInvokeRejectsStaleTimestamp(t *testing.T) {
	envelope := newEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc", "text", "plain_text",
		time.Now().Add(-31*time.Second))
	recorder := invoke(t, newHandler(testConfig()), envelope)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestInvokeRejectsHeaderMismatch(t *testing.T) {
	envelope := newEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc", "text", "plain_text", time.Now())
	envelope.providerID = "trusted_core"
	recorder := invoke(t, newHandler(testConfig()), envelope)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestInvokeReplayIsCachedAndConflicts(t *testing.T) {
	handler := newHandler(testConfig())
	envelope := newEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc", "text", "plain_text", time.Now())
	first := invoke(t, handler, envelope)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	second := invoke(t, handler, envelope)
	if second.Code != http.StatusOK || second.Body.String() != first.Body.String() {
		t.Fatalf("replay mismatch: %d %s", second.Code, second.Body.String())
	}
	conflict := newEnvelope(t, envelope.invocationID, "different", "plain_text", time.Now())
	third := invoke(t, handler, conflict)
	if third.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d", third.Code)
	}
}

func TestInvokeConcurrentReplayExecutesOnce(t *testing.T) {
	provider := newHandler(testConfig()).(*handler)
	envelope := newEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc", "text", "plain_text", time.Now())
	start := make(chan struct{})
	type result struct {
		status int
		body   string
	}
	results := make(chan result, 2)
	var wait sync.WaitGroup
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			recorder := invoke(t, provider, envelope)
			results <- result{status: recorder.Code, body: recorder.Body.String()}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	first := true
	var body string
	for item := range results {
		if item.status != http.StatusOK {
			t.Fatalf("status = %d body = %s", item.status, item.body)
		}
		if first {
			body = item.body
			first = false
		} else if item.body != body {
			t.Fatal("concurrent replay returned different responses")
		}
	}
	if provider.executions.Load() != 1 {
		t.Fatalf("structure executed %d times", provider.executions.Load())
	}
}

func TestEnvelopeOverheadConstant(t *testing.T) {
	prefix := `{"invocation_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","capability_id":"content.structure","contract_version":"1.0","input":`
	if len(prefix)+1 != maxEnvelopeOverhead {
		t.Fatalf("envelope overhead = %d, want %d", len(prefix)+1, maxEnvelopeOverhead)
	}
}

func exactStructureInput(t *testing.T, target int, base string) []byte {
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
	if per < 1 {
		t.Fatalf("invalid base encoding")
	}
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

func rawEnvelope(t *testing.T, invocationID string, input []byte, timestamp time.Time) *http.Request {
	t.Helper()
	body := []byte(`{"invocation_id":"` + invocationID +
		`","capability_id":"content.structure","contract_version":"1.0","input":` + string(input) + `}`)
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	mac := hmac.New(sha256.New, testSecret)
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
	return request
}

func TestInvokeExactInputBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		target int
		base   string
		status int
	}{
		{"chinese under", 65535, "汉", http.StatusOK},
		{"chinese exact", 65536, "汉", http.StatusOK},
		{"chinese over", 65537, "汉", http.StatusRequestEntityTooLarge},
		{"emoji under", 65535, "😀", http.StatusOK},
		{"emoji exact", 65536, "😀", http.StatusOK},
		{"emoji over", 65537, "😀", http.StatusRequestEntityTooLarge},
		{"json newline escape exact", 65536, "\n", http.StatusOK},
		{"json control escape exact", 65536, "\x01", http.StatusOK},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			input := exactStructureInput(t, item.target, item.base)
			request := rawEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc", input, time.Now())
			recorder := httptest.NewRecorder()
			newHandler(testConfig()).ServeHTTP(recorder, request)
			if recorder.Code != item.status {
				t.Fatalf("status = %d, want %d body = %s", recorder.Code, item.status, recorder.Body.String())
			}
			if item.status == http.StatusOK {
				var response successResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if response.Status != "succeeded" || response.Output.ItemCount < 0 {
					t.Fatalf("response = %+v", response)
				}
			}
		})
	}
}

func TestInvokeLargeHMACReplayAndNegative(t *testing.T) {
	handler := newHandler(testConfig()).(*handler)
	input := exactStructureInput(t, 65536, "汉")
	timestamp := time.Now()
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, rawEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc", input, timestamp))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d body = %s", first.Code, first.Body.String())
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, rawEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc", input, timestamp))
	if second.Code != http.StatusOK || second.Body.String() != first.Body.String() {
		t.Fatalf("replay mismatch")
	}
	if handler.executions.Load() != 1 {
		t.Fatalf("executions = %d", handler.executions.Load())
	}
	bad := rawEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc", input, time.Now())
	bad.Header.Set("X-DW-Signature", strings.Repeat("0", 64))
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, bad)
	if rejected.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature status = %d", rejected.Code)
	}
}

func TestInvokeRejectsOversizedBody(t *testing.T) {
	cfg := testConfig()
	cfg.maxBodyBytes = 128
	envelope := newEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		strings.Repeat("x", 512), "plain_text", time.Now())
	recorder := invoke(t, newHandler(cfg), envelope)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestInvokeRejectsUnsupportedFormat(t *testing.T) {
	envelope := newEnvelope(t, "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		"text", "sentences", time.Now())
	recorder := invoke(t, newHandler(testConfig()), envelope)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}
