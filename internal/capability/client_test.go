// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeResolver struct {
	ips []string
}

func (f fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	out := make([]net.IPAddr, 0, len(f.ips))
	for _, value := range f.ips {
		out = append(out, net.IPAddr{IP: net.ParseIP(value)})
	}
	return out, nil
}

func testProvider(t *testing.T, timeoutMS int) *ProviderConfig {
	t.Helper()
	raw := "http://capability-provider:8081"
	parsed, err := parseProviderURL(raw, "internal", []string{"capability-provider"})
	if err != nil {
		t.Fatalf("parse provider URL: %v", err)
	}
	return &ProviderConfig{
		ProviderID: ProviderContentStructure, Transport: TransportHTTPHMAC,
		BaseURL: raw, Network: "internal", AllowedHosts: []string{"capability-provider"},
		TimeoutMS: timeoutMS, MaxInputBytes: 65536, MaxOutputBytes: 262144,
		parsed: parsed,
	}
}

func dialTransport(target string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", target)
		},
	}
}

func newTestClient(t *testing.T, timeoutMS int, target string, resolver Resolver) *ExternalClient {
	t.Helper()
	client, err := NewExternalClient(testProvider(t, timeoutMS), SigningKey{ID: "k1", Secret: []byte("0123456789abcdef0123456789abcdef")},
		ExternalClientOptions{Resolver: resolver, Transport: dialTransport(target)})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func TestClassifyAddressTable(t *testing.T) {
	cases := []struct {
		ip       string
		internal bool
		public   bool
	}{
		{"10.1.2.3", true, false},
		{"172.16.0.1", true, false},
		{"192.168.1.1", true, false},
		{"fc00::1", true, false},
		{"127.0.0.1", false, false},
		{"::1", false, false},
		{"169.254.169.254", false, false},
		{"fe80::1", false, false},
		{"100.64.0.1", false, false},
		{"192.0.0.1", false, false},
		{"198.18.0.1", false, false},
		{"224.0.0.1", false, false},
		{"240.0.0.1", false, false},
		{"0.0.0.0", false, false},
		{"8.8.8.8", true, true},
	}
	for _, item := range cases {
		ip := net.ParseIP(item.ip)
		internalErr := classifyAddress(ip, "internal")
		publicErr := classifyAddress(ip, "public")
		if (internalErr == nil) != item.internal {
			t.Errorf("%s internal allowed=%v err=%v", item.ip, item.internal, internalErr)
		}
		if (publicErr == nil) != item.public {
			t.Errorf("%s public allowed=%v err=%v", item.ip, item.public, publicErr)
		}
	}
}

func TestProviderEnvelopeOverheadIsExact(t *testing.T) {
	input := json.RawMessage(`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","text":"<&>","format":"plain_text"}`)
	body, overhead, err := buildProviderEnvelope("0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		IDContentStructure, input)
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}
	if overhead != maxEnvelopeOverhead {
		t.Fatalf("overhead = %d, want %d", overhead, maxEnvelopeOverhead)
	}
	if len(body) != len(input)+maxEnvelopeOverhead {
		t.Fatalf("body = %d input = %d", len(body), len(input))
	}
	if !bytes.Contains(body, []byte("<&>")) {
		t.Fatal("input was HTML-escaped; size accounting would be wrong")
	}
}

func TestSignRequestDeterministic(t *testing.T) {
	first := signRequest([]byte("secretsecretsecretsecretsecret12"), "1700000000", []byte(`{"a":1}`))
	second := signRequest([]byte("secretsecretsecretsecretsecret12"), "1700000000", []byte(`{"a":1}`))
	if first != second || len(first) != 64 {
		t.Fatalf("signature = %q / %q", first, second)
	}
	if strings.ToLower(first) != first {
		t.Fatal("signature is not lower-case hex")
	}
}

func TestExternalClientSuccess(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		body, _ := io.ReadAll(r.Body)
		var envelope map[string]any
		_ = json.Unmarshal(body, &envelope)
		if r.Header.Get("X-DW-Provider-Id") != ProviderContentStructure ||
			r.Header.Get("X-DW-Signature") == "" ||
			r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("unexpected headers: %v", r.Header)
		}
		fmt.Fprintf(w, `{"invocation_id":%q,"capability_id":"content.structure","contract_version":"1.0","status":"succeeded","output":{"items":[],"item_count":0,"truncated":false,"format":"plain_text"},"duration_ms":2}`,
			envelope["invocation_id"])
	}))
	defer server.Close()
	client := newTestClient(t, 1000, server.Listener.Addr().String(),
		fakeResolver{ips: []string{"172.18.0.2"}})
	result, err := client.Invoke(context.Background(), "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		IDContentStructure, json.RawMessage(`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","text":"x","format":"plain_text"}`))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(result.Output) == 0 || atomic.LoadInt32(&requests) != 1 {
		t.Fatalf("result=%+v requests=%d", result, requests)
	}
}

func TestExternalClientTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := newTestClient(t, 100, server.Listener.Addr().String(),
		fakeResolver{ips: []string{"172.18.0.2"}})
	_, err := client.Invoke(context.Background(), "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		IDContentStructure, json.RawMessage(`{"x":1}`))
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderTimedOut {
		t.Fatalf("failure = %#v", err)
	}
}

func TestExternalClientHeaderTimeoutIsTimedOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", server.Listener.Addr().String())
		},
		ResponseHeaderTimeout: 100 * time.Millisecond,
	}
	client, err := NewExternalClient(testProvider(t, 1000), SigningKey{ID: "k1", Secret: []byte("0123456789abcdef0123456789abcdef")},
		ExternalClientOptions{Resolver: fakeResolver{ips: []string{"172.18.0.2"}}, Transport: transport})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	_, err = client.Invoke(context.Background(), "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		IDContentStructure, json.RawMessage(`{"x":1}`))
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderTimedOut {
		t.Fatalf("failure = %#v", err)
	}
}

func TestExternalClientTruncatedResponseIsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("hijacking is not supported")
		}
		conn, _, _ := hijacker.Hijack()
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 200\r\n\r\n{\"incomplete\":true"))
		_ = conn.Close()
	}))
	defer server.Close()
	client := newTestClient(t, 1000, server.Listener.Addr().String(),
		fakeResolver{ips: []string{"172.18.0.2"}})
	_, err := client.Invoke(context.Background(), "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		IDContentStructure, json.RawMessage(`{"x":1}`))
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderUnknownResult {
		t.Fatalf("failure = %#v", err)
	}
}

func TestExternalClientCompleteButMalformedJSONIsUnknown(t *testing.T) {
	truncated := `{"invocation_id":"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(truncated)))
		_, _ = w.Write([]byte(truncated))
	}))
	defer server.Close()
	client := newTestClient(t, 1000, server.Listener.Addr().String(),
		fakeResolver{ips: []string{"172.18.0.2"}})
	_, err := client.Invoke(context.Background(), "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		IDContentStructure, json.RawMessage(`{"x":1}`))
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderUnknownResult {
		t.Fatalf("failure = %#v", err)
	}
}

func TestExternalClientConnectionFailureIsFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	target := server.Listener.Addr().String()
	server.Close()
	client := newTestClient(t, 1000, target, fakeResolver{ips: []string{"172.18.0.2"}})
	_, err := client.Invoke(context.Background(), "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		IDContentStructure, json.RawMessage(`{"x":1}`))
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderFailed {
		t.Fatalf("failure = %#v", err)
	}
}

func TestExternalClientRejectsPrivateResolutionForPublic(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
	}))
	defer server.Close()
	provider := testProvider(t, 1000)
	provider.Network = "public"
	client, err := NewExternalClient(provider, SigningKey{ID: "k1", Secret: []byte("0123456789abcdef0123456789abcdef")},
		ExternalClientOptions{Resolver: fakeResolver{ips: []string{"10.0.0.1"}}, Transport: dialTransport(server.Listener.Addr().String())})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	_, err = client.Invoke(context.Background(), "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		IDContentStructure, json.RawMessage(`{"x":1}`))
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderFailed {
		t.Fatalf("failure = %#v", err)
	}
	if atomic.LoadInt32(&requests) != 0 {
		t.Fatal("request reached the provider despite address rejection")
	}
}

func TestExternalClientEnvelopeMismatchIsFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"invocation_id":"0192f0c4-5a6b-7c8d-9e0f-123456789999","capability_id":"content.structure","contract_version":"1.0","status":"succeeded","output":{},"duration_ms":1}`))
	}))
	defer server.Close()
	client := newTestClient(t, 1000, server.Listener.Addr().String(),
		fakeResolver{ips: []string{"172.18.0.2"}})
	_, err := client.Invoke(context.Background(), "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		IDContentStructure, json.RawMessage(`{"x":1}`))
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderFailed {
		t.Fatalf("failure = %#v", err)
	}
}

func TestExternalClientAuthFailureIsMarked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := newTestClient(t, 1000, server.Listener.Addr().String(),
		fakeResolver{ips: []string{"172.18.0.2"}})
	_, err := client.Invoke(context.Background(), "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		IDContentStructure, json.RawMessage(`{"x":1}`))
	var failure *ProviderFailure
	if !errors.As(err, &failure) || !failure.AuthFailure {
		t.Fatalf("failure = %#v", err)
	}
}
