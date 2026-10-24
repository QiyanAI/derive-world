// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type testPKI struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestPKI(t *testing.T) *testPKI {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "Derive World G06 Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA: %v", err)
	}
	return &testPKI{
		cert: cert, key: key,
		pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

func (p *testPKI) issue(t *testing.T, dnsNames []string, notBefore, notAfter time.Time, client bool) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	usage := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	if client {
		usage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "derive-world-provider"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  usage,
		DNSNames:     dnsNames,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, p.cert, &key.PublicKey, p.key)
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("load leaf pair: %v", err)
	}
	return pair, certPEM
}

func writeTempFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func newTLSServer(t *testing.T, pair tls.Certificate, clientCA *testPKI, requests *int32) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests != nil {
			atomic.AddInt32(requests, 1)
		}
		raw, _ := io.ReadAll(r.Body)
		var envelope struct {
			InvocationID string `json:"invocation_id"`
		}
		_ = json.Unmarshal(raw, &envelope)
		_, _ = w.Write(successBody(envelope.InvocationID))
	}))
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{pair},
	}
	if clientCA != nil {
		pool := x509.NewCertPool()
		pool.AddCert(clientCA.cert)
		server.TLS.ClientCAs = pool
		server.TLS.ClientAuth = tls.RequireAndVerifyClientCert
	}
	server.StartTLS()
	return server
}

func tlsProviderConfig(t *testing.T) *ProviderConfig {
	t.Helper()
	raw := "https://capability-provider:8081"
	parsed, err := parseProviderURL(raw, "internal", []string{"capability-provider"})
	if err != nil {
		t.Fatalf("parse provider URL: %v", err)
	}
	return &ProviderConfig{
		ProviderID: ProviderContentStructure, Transport: TransportHTTPHMAC,
		BaseURL: raw, Network: "internal", AllowedHosts: []string{"capability-provider"},
		TimeoutMS: 2000, MaxInputBytes: 65536, MaxOutputBytes: 262144,
		parsed: parsed,
	}
}

func dialTo(target string) ExternalClientOptions {
	return ExternalClientOptions{
		Resolver: fakeResolver{ips: []string{"172.18.0.2"}},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, target)
		},
	}
}

func newTLSClient(t *testing.T, provider *ProviderConfig, options ExternalClientOptions) *ExternalClient {
	t.Helper()
	client, err := NewExternalClient(provider,
		SigningKey{ID: "k1", Secret: []byte(testV2Secret)}, options)
	if err != nil {
		t.Fatalf("new TLS client: %v", err)
	}
	return client
}

func invokeTLS(t *testing.T, client *ExternalClient) error {
	t.Helper()
	_, err := client.Invoke(context.Background(), "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
		IDContentStructure, json.RawMessage(`{"object_id":"0192f0c4-5a6b-7c8d-9e0f-123456789abc","text":"x","format":"plain_text"}`))
	return err
}

func TestTLSVerifiesCustomCA(t *testing.T) {
	ca := newTestPKI(t)
	pair, _ := ca.issue(t, []string{"provider.test"},
		time.Now().Add(-time.Hour), time.Now().Add(time.Hour), false)
	var requests int32
	server := newTLSServer(t, pair, nil, &requests)
	defer server.Close()
	dir := t.TempDir()
	provider := tlsProviderConfig(t)
	provider.TLSCAFile = writeTempFile(t, dir, "ca.pem", ca.pem)
	provider.TLSServerName = "provider.test"
	if err := invokeTLS(t, newTLSClient(t, provider, dialTo(server.Listener.Addr().String()))); err != nil {
		t.Fatalf("verified TLS invoke: %v", err)
	}
	if atomic.LoadInt32(&requests) != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestTLSRejectsUntrustedCA(t *testing.T) {
	ca := newTestPKI(t)
	pair, _ := ca.issue(t, []string{"provider.test"},
		time.Now().Add(-time.Hour), time.Now().Add(time.Hour), false)
	var requests int32
	server := newTLSServer(t, pair, nil, &requests)
	defer server.Close()
	provider := tlsProviderConfig(t)
	provider.TLSServerName = "provider.test"
	err := invokeTLS(t, newTLSClient(t, provider, dialTo(server.Listener.Addr().String())))
	if err == nil {
		t.Fatal("invoke succeeded with an untrusted CA")
	}
	if atomic.LoadInt32(&requests) != 0 {
		t.Fatal("HTTP request reached the server despite TLS verification failure")
	}
}

func TestTLSRejectsWrongServerName(t *testing.T) {
	ca := newTestPKI(t)
	pair, _ := ca.issue(t, []string{"provider.test"},
		time.Now().Add(-time.Hour), time.Now().Add(time.Hour), false)
	var requests int32
	server := newTLSServer(t, pair, nil, &requests)
	defer server.Close()
	dir := t.TempDir()
	provider := tlsProviderConfig(t)
	provider.TLSCAFile = writeTempFile(t, dir, "ca.pem", ca.pem)
	provider.TLSServerName = "wrong.test"
	err := invokeTLS(t, newTLSClient(t, provider, dialTo(server.Listener.Addr().String())))
	if err == nil {
		t.Fatal("invoke succeeded with a mismatched server name")
	}
	if atomic.LoadInt32(&requests) != 0 {
		t.Fatal("HTTP request reached the server despite hostname verification failure")
	}
}

func TestTLSRejectsExpiredCertificate(t *testing.T) {
	ca := newTestPKI(t)
	pair, _ := ca.issue(t, []string{"provider.test"},
		time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour), false)
	var requests int32
	server := newTLSServer(t, pair, nil, &requests)
	defer server.Close()
	dir := t.TempDir()
	provider := tlsProviderConfig(t)
	provider.TLSCAFile = writeTempFile(t, dir, "ca.pem", ca.pem)
	provider.TLSServerName = "provider.test"
	err := invokeTLS(t, newTLSClient(t, provider, dialTo(server.Listener.Addr().String())))
	if err == nil {
		t.Fatal("invoke succeeded with an expired server certificate")
	}
	if atomic.LoadInt32(&requests) != 0 {
		t.Fatal("HTTP request reached the server despite certificate expiry")
	}
}

func TestTLSMutualAuthentication(t *testing.T) {
	ca := newTestPKI(t)
	serverPair, _ := ca.issue(t, []string{"provider.test"},
		time.Now().Add(-time.Hour), time.Now().Add(time.Hour), false)
	clientPair, clientPEM := ca.issue(t, nil,
		time.Now().Add(-time.Hour), time.Now().Add(time.Hour), true)
	var requests int32
	server := newTLSServer(t, serverPair, ca, &requests)
	defer server.Close()
	dir := t.TempDir()
	caPath := writeTempFile(t, dir, "ca.pem", ca.pem)
	certPath := writeTempFile(t, dir, "client.pem", clientPEM)
	keyDER, err := x509.MarshalECPrivateKey(clientPair.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatalf("marshal client key: %v", err)
	}
	keyPath := writeTempFile(t, dir, "client-key.pem",
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))

	t.Run("with client certificate", func(t *testing.T) {
		provider := tlsProviderConfig(t)
		provider.TLSCAFile = caPath
		provider.TLSServerName = "provider.test"
		provider.TLSClientCertFile = certPath
		provider.TLSClientKeyFile = keyPath
		if err := invokeTLS(t, newTLSClient(t, provider, dialTo(server.Listener.Addr().String()))); err != nil {
			t.Fatalf("mutual TLS invoke: %v", err)
		}
	})

	t.Run("without client certificate", func(t *testing.T) {
		before := atomic.LoadInt32(&requests)
		provider := tlsProviderConfig(t)
		provider.TLSCAFile = caPath
		provider.TLSServerName = "provider.test"
		err := invokeTLS(t, newTLSClient(t, provider, dialTo(server.Listener.Addr().String())))
		if err == nil {
			t.Fatal("mutual TLS invoke succeeded without a client certificate")
		}
		if atomic.LoadInt32(&requests) != before {
			t.Fatal("HTTP request was served without a client certificate")
		}
	})
}

func TestTLSConfigRejectsMissingPair(t *testing.T) {
	provider := tlsProviderConfig(t)
	provider.TLSClientCertFile = "client.pem"
	if _, err := buildTLSConfig(provider); err == nil {
		t.Fatal("TLS config accepted a client certificate without a key")
	}
}

func TestTLSFailureIsClassifiedAsFailed(t *testing.T) {
	ca := newTestPKI(t)
	pair, _ := ca.issue(t, []string{"provider.test"},
		time.Now().Add(-time.Hour), time.Now().Add(time.Hour), false)
	server := newTLSServer(t, pair, nil, nil)
	defer server.Close()
	provider := tlsProviderConfig(t)
	provider.TLSServerName = "provider.test"
	err := invokeTLS(t, newTLSClient(t, provider, dialTo(server.Listener.Addr().String())))
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Kind != ProviderFailed {
		t.Fatalf("TLS failure = %#v, want ProviderFailed", err)
	}
}
