// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const testV2Secret = "0123456789abcdef0123456789abcdef"

func v2Provider(t *testing.T) *ProviderConfig {
	t.Helper()
	provider := testProvider(t, 1000)
	provider.Transport = TransportHTTPHMACV2
	return provider
}

func newV2Client(t *testing.T, provider *ProviderConfig, target string) *ExternalClient {
	t.Helper()
	client, err := NewExternalClient(provider,
		SigningKey{ID: "k1", Secret: []byte(testV2Secret)},
		ExternalClientOptions{Resolver: fakeResolver{ips: []string{"172.18.0.2"}},
			Transport: dialTransport(target)})
	if err != nil {
		t.Fatalf("new v2 client: %v", err)
	}
	return client
}

func successBody(invocationID string) []byte {
	return []byte(fmt.Sprintf(
		`{"invocation_id":%q,"capability_id":"content.structure","contract_version":"1.0","status":"succeeded","output":{"items":[],"item_count":0,"truncated":false,"format":"plain_text"},"duration_ms":1}`,
		invocationID))
}

// v2TestServer signs the response according to the mutate hook. Returning an
// empty signature omits the signing headers.
func v2TestServer(t *testing.T, status int, mutate func(invocationID string, body []byte) []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var envelope struct {
			InvocationID string `json:"invocation_id"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Errorf("decode request: %v", err)
		}
		body := successBody(envelope.InvocationID)
		if status != http.StatusOK {
			body = []byte(fmt.Sprintf(`{"invocation_id":%q,"error":{"code":"capability_failed","message":"failed"}}`, envelope.InvocationID))
		}
		returned := body
		signedInvocation := envelope.InvocationID
		if mutate != nil {
			returned = mutate(envelope.InvocationID, body)
		}
		timestamp := r.Header.Get("X-DW-Timestamp")
		signature := responseSignature([]byte(testV2Secret), status, timestamp, signedInvocation, body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-DW-Response-Signature-Version", ResponseSignatureVersion)
		w.Header().Set("X-DW-Key-Id", "k1")
		w.Header().Set("X-DW-Response-Signature", signature)
		w.WriteHeader(status)
		_, _ = w.Write(returned)
	}))
}

func invokeV2(t *testing.T, client *ExternalClient, invocationID string) (*ProviderResult, error) {
	t.Helper()
	return client.Invoke(context.Background(), invocationID, IDContentStructure,
		json.RawMessage(`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","text":"x","format":"plain_text"}`))
}

func TestExternalClientV2SignedSuccess(t *testing.T) {
	server := v2TestServer(t, http.StatusOK, nil)
	defer server.Close()
	client := newV2Client(t, v2Provider(t), server.Listener.Addr().String())
	result, err := invokeV2(t, client, "0192f0c4-5a6b-7c8d-9e0f-123456789abc")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(result.Output) == 0 {
		t.Fatal("signed response produced no output")
	}
}

func TestExternalClientV2UnsignedIsUnknown(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		raw, _ := io.ReadAll(r.Body)
		var envelope struct {
			InvocationID string `json:"invocation_id"`
		}
		_ = json.Unmarshal(raw, &envelope)
		_, _ = w.Write(successBody(envelope.InvocationID))
	}))
	defer server.Close()
	client := newV2Client(t, v2Provider(t), server.Listener.Addr().String())
	_, err := invokeV2(t, client, "0192f0c4-5a6b-7c8d-9e0f-123456789abc")
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderUnknownResult || !failure.IntegrityFailure {
		t.Fatalf("failure = %#v, want unknown integrity failure", err)
	}
	if atomic.LoadInt32(&requests) != 1 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestExternalClientV2TamperedBodyIsUnknown(t *testing.T) {
	server := v2TestServer(t, http.StatusOK, func(_ string, body []byte) []byte {
		return []byte(strings.Replace(string(body), `"item_count":0`, `"item_count":9`, 1))
	})
	defer server.Close()
	client := newV2Client(t, v2Provider(t), server.Listener.Addr().String())
	_, err := invokeV2(t, client, "0192f0c4-5a6b-7c8d-9e0f-123456789abc")
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderUnknownResult || !failure.IntegrityFailure {
		t.Fatalf("failure = %#v, want unknown integrity failure", err)
	}
}

func TestExternalClientV2WrongInvocationIsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var envelope struct {
			InvocationID string `json:"invocation_id"`
		}
		_ = json.Unmarshal(raw, &envelope)
		body := successBody(envelope.InvocationID)
		timestamp := r.Header.Get("X-DW-Timestamp")
		// Sign over a different invocation id while returning this request's
		// body: the signature must not verify for the caller.
		signature := responseSignature([]byte(testV2Secret), http.StatusOK, timestamp,
			"0192f0c4-5a6b-7c8d-9e0f-123456789999", body)
		w.Header().Set("X-DW-Response-Signature-Version", ResponseSignatureVersion)
		w.Header().Set("X-DW-Key-Id", "k1")
		w.Header().Set("X-DW-Response-Signature", signature)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := newV2Client(t, v2Provider(t), server.Listener.Addr().String())
	_, err := invokeV2(t, client, "0192f0c4-5a6b-7c8d-9e0f-123456789abc")
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderUnknownResult || !failure.IntegrityFailure {
		t.Fatalf("failure = %#v, want unknown integrity failure", err)
	}
}

func TestExternalClientV2WrongKeyIDIsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var envelope struct {
			InvocationID string `json:"invocation_id"`
		}
		_ = json.Unmarshal(raw, &envelope)
		body := successBody(envelope.InvocationID)
		signature := responseSignature([]byte(testV2Secret), http.StatusOK,
			r.Header.Get("X-DW-Timestamp"), envelope.InvocationID, body)
		w.Header().Set("X-DW-Response-Signature-Version", ResponseSignatureVersion)
		w.Header().Set("X-DW-Key-Id", "other")
		w.Header().Set("X-DW-Response-Signature", signature)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := newV2Client(t, v2Provider(t), server.Listener.Addr().String())
	_, err := invokeV2(t, client, "0192f0c4-5a6b-7c8d-9e0f-123456789abc")
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderUnknownResult || !failure.IntegrityFailure {
		t.Fatalf("failure = %#v, want unknown integrity failure", err)
	}
}

func TestExternalClientV2SignedErrorIsFailed(t *testing.T) {
	server := v2TestServer(t, http.StatusInternalServerError, nil)
	defer server.Close()
	client := newV2Client(t, v2Provider(t), server.Listener.Addr().String())
	_, err := invokeV2(t, client, "0192f0c4-5a6b-7c8d-9e0f-123456789abc")
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderFailed || failure.Status != 500 {
		t.Fatalf("failure = %#v, want failed 500", err)
	}
}

func TestExternalClientV2ResponseReplayIsUnknown(t *testing.T) {
	firstID := "0192f0c4-5a6b-7c8d-9e0f-123456789abc"
	secondID := "0192f0c4-5a6b-7c8d-9e0f-123456789abd"
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		raw, _ := io.ReadAll(r.Body)
		var envelope struct {
			InvocationID string `json:"invocation_id"`
		}
		_ = json.Unmarshal(raw, &envelope)
		// Always return the first invocation's signed response.
		body := successBody(firstID)
		signature := responseSignature([]byte(testV2Secret), http.StatusOK,
			r.Header.Get("X-DW-Timestamp"), firstID, body)
		w.Header().Set("X-DW-Response-Signature-Version", ResponseSignatureVersion)
		w.Header().Set("X-DW-Key-Id", "k1")
		w.Header().Set("X-DW-Response-Signature", signature)
		if call == 1 {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := newV2Client(t, v2Provider(t), server.Listener.Addr().String())
	if _, err := invokeV2(t, client, firstID); err != nil {
		t.Fatalf("first invoke: %v", err)
	}
	_, err := invokeV2(t, client, secondID)
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderUnknownResult || !failure.IntegrityFailure {
		t.Fatalf("replayed response failure = %#v, want unknown integrity failure", err)
	}
}

func TestResponseSignatureGolden(t *testing.T) {
	got := responseSignature([]byte(testV2Secret), http.StatusOK, "1700000000",
		"0192f0c4-5a6b-7c8d-9e0f-123456789abc", []byte(`{"a":1}`))
	want := "374ac9214815b0dc1ee8651e52e9580f99a5d27e639527c4f01042a5a90ff86f"
	if got != want {
		t.Fatalf("response signature = %s, want %s", got, want)
	}
}

func TestSigningKeyValidation(t *testing.T) {
	for _, key := range []SigningKey{
		{ID: "", Secret: []byte(testV2Secret)},
		{ID: "K1", Secret: []byte(testV2Secret)},
		{ID: "k1", Secret: []byte("short")},
	} {
		if err := key.Validate(); !errors.Is(err, ErrConfigInvalid) {
			t.Fatalf("key %+v validation error = %v", key, err)
		}
	}
}

func TestExternalClientV2DoesNotSetPlainV1Headers(t *testing.T) {
	var sawVersion, sawKeyID bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawVersion = r.Header.Get("X-DW-Response-Signature-Version") == ResponseSignatureVersion
		sawKeyID = r.Header.Get("X-DW-Key-Id") == "k1"
		raw, _ := io.ReadAll(r.Body)
		var envelope struct {
			InvocationID string `json:"invocation_id"`
		}
		_ = json.Unmarshal(raw, &envelope)
		body := successBody(envelope.InvocationID)
		signature := responseSignature([]byte(testV2Secret), http.StatusOK,
			r.Header.Get("X-DW-Timestamp"), envelope.InvocationID, body)
		w.Header().Set("X-DW-Response-Signature-Version", ResponseSignatureVersion)
		w.Header().Set("X-DW-Key-Id", "k1")
		w.Header().Set("X-DW-Response-Signature", signature)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := newV2Client(t, v2Provider(t), server.Listener.Addr().String())
	if _, err := invokeV2(t, client, "0192f0c4-5a6b-7c8d-9e0f-123456789abc"); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !sawVersion || !sawKeyID {
		t.Fatalf("request headers version=%v key_id=%v", sawVersion, sawKeyID)
	}
}
