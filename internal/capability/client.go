// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"strconv"
	"strings"
	"time"
)

// Resolver is the DNS boundary. Production uses net.DefaultResolver; tests may
// inject a deterministic resolver without weakening address classification.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// ExternalClientOptions carries test-only network injection. Production
// callers leave every field nil.
type ExternalClientOptions struct {
	Resolver  Resolver
	Transport http.RoundTripper
	// DialContext replaces only the final TCP dial. The production address
	// classification and TLS verification still run. Tests use it to reach a
	// loopback httptest server while the Provider host stays synthetic.
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
}

// maxEnvelopeOverhead is the exact JSON overhead of the D06 §3.5 invocation
// envelope for content.structure: the fixed keys/values plus one UUIDv7.
// It is not an arbitrary margin; a unit test recomputes it from the fixed
// fields. The Provider accepts MAX_BODY_BYTES + this exact overhead so a
// business input at the frozen 64 KiB limit remains deliverable.
const maxEnvelopeOverhead = 126

// ExternalClient speaks the frozen D06 §3.5 HTTP+HMAC protocol. It never
// accepts a URL from a request, never follows redirects, and never uses an
// environment proxy.
type ExternalClient struct {
	provider        *ProviderConfig
	host            string
	port            string
	secret          []byte
	keyID           string
	responseSigning bool
	resolver        Resolver
	client          *http.Client
	maxBody         int
	timeout         time.Duration
}

// ProviderFailureKind is the broker-visible classification of a Provider
// failure. It maps directly onto the invocation state machine.
type ProviderFailureKind int

const (
	ProviderFailed ProviderFailureKind = iota
	ProviderTimedOut
	ProviderUnknownResult
)

// ProviderFailure describes a definite failure, a timeout, or an unknown
// result. AuthFailure marks a Provider-side HMAC/clock rejection and
// IntegrityFailure marks a missing or invalid signed response; both are
// audited as integration-security events.
type ProviderFailure struct {
	Kind             ProviderFailureKind
	Status           int
	AuthFailure      bool
	IntegrityFailure bool
	Cause            error
}

func (f *ProviderFailure) Error() string {
	if f.Cause != nil {
		return f.Cause.Error()
	}
	return "provider failure"
}

// ProviderResult is a complete, envelope-valid Provider success response.
type ProviderResult struct {
	Output     json.RawMessage
	DurationMS int
	HTTPStatus int
}

// NewExternalClient validates the configured endpoint and builds a client.
func NewExternalClient(provider *ProviderConfig, key SigningKey, options ExternalClientOptions) (*ExternalClient, error) {
	if provider == nil {
		return nil, fmt.Errorf("%w: provider configuration is required", ErrConfigInvalid)
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	parsed := provider.parsed
	if parsed == nil {
		return nil, fmt.Errorf("%w: provider URL was not validated", ErrConfigInvalid)
	}
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if err := checkPort(port); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConfigInvalid, err)
	}
	resolver := options.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	transport := options.Transport
	if transport == nil {
		created, err := securedTransport(provider, resolver, options)
		if err != nil {
			return nil, err
		}
		transport = created
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &ExternalClient{
		provider: provider, host: host, port: port,
		secret: append([]byte(nil), key.Secret...), keyID: key.ID,
		responseSigning: provider.Transport == TransportHTTPHMACV2,
		resolver:        resolver,
		client:          client, maxBody: provider.MaxOutputBytes,
		timeout: time.Duration(provider.TimeoutMS) * time.Millisecond,
	}, nil
}

func securedTransport(provider *ProviderConfig, resolver Resolver, options ExternalClientOptions) (*http.Transport, error) {
	dialer := &net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}
	dial := dialer.DialContext
	if options.DialContext != nil {
		dial = options.DialContext
	}
	tlsConfig, err := buildTLSConfig(provider)
	if err != nil {
		return nil, err
	}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if !strings.EqualFold(host, provider.parsed.Hostname()) {
				return nil, fmt.Errorf("dial host %q does not match the configured provider", host)
			}
			addresses, err := resolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(addresses) == 0 {
				return nil, errors.New("provider host resolved to no addresses")
			}
			for _, address := range addresses {
				if err := classifyAddress(address.IP, provider.Network); err != nil {
					return nil, err
				}
			}
			var lastErr error
			for _, address := range addresses {
				conn, dialErr := dial(ctx, network, net.JoinHostPort(address.IP.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
		TLSClientConfig:        tlsConfig,
		TLSHandshakeTimeout:    2 * time.Second,
		ResponseHeaderTimeout:  5 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		MaxIdleConns:           4,
		MaxIdleConnsPerHost:    4,
		MaxConnsPerHost:        4,
		IdleConnTimeout:        60 * time.Second,
		ForceAttemptHTTP2:      false,
		DisableCompression:     true,
	}, nil
}

func checkPort(port string) error {
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return errors.New("port is invalid")
	}
	return nil
}

// Invoke sends one signed request and classifies the outcome. The returned
// output is not yet schema-validated; the Broker does that before any state
// transition.
func (c *ExternalClient) Invoke(ctx context.Context, invocationID, capabilityID string, input json.RawMessage) (*ProviderResult, error) {
	if len(input) > c.provider.MaxInputBytes {
		return nil, &ProviderFailure{Kind: ProviderFailed, Cause: errors.New("capability input exceeds max_input_bytes")}
	}
	body, overhead, err := buildProviderEnvelope(invocationID, capabilityID, input)
	if err != nil {
		return nil, &ProviderFailure{Kind: ProviderFailed, Cause: err}
	}
	if overhead != maxEnvelopeOverhead {
		return nil, &ProviderFailure{Kind: ProviderFailed,
			Cause: fmt.Errorf("unexpected provider envelope overhead %d", overhead)}
	}
	if len(body) > c.provider.MaxInputBytes+maxEnvelopeOverhead {
		return nil, &ProviderFailure{Kind: ProviderFailed, Cause: errors.New("provider envelope exceeds its bound")}
	}
	if err := c.preflight(ctx); err != nil {
		return nil, &ProviderFailure{Kind: ProviderFailed, Cause: err}
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	signature := signRequest(c.secret, timestamp, body)

	url := *c.provider.parsed
	url.Path = "/v1/invoke"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url.String(), bytes.NewReader(body))
	if err != nil {
		return nil, &ProviderFailure{Kind: ProviderFailed, Cause: err}
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("X-DW-Provider-Id", c.provider.ProviderID)
	request.Header.Set("X-DW-Capability-Id", capabilityID)
	request.Header.Set("X-DW-Contract-Version", ContractVersion)
	request.Header.Set("X-DW-Invocation-Id", invocationID)
	request.Header.Set("X-DW-Timestamp", timestamp)
	request.Header.Set("X-DW-Signature", signature)
	request.Header.Set("X-DW-Key-Id", c.keyID)
	if c.responseSigning {
		request.Header.Set("X-DW-Response-Signature-Version", ResponseSignatureVersion)
	}
	request.Header.Set("User-Agent", "derive-world-broker/0.1.0")

	var wroteRequest, firstByte bool
	trace := &httptrace.ClientTrace{
		WroteRequest:         func(httptrace.WroteRequestInfo) { wroteRequest = true },
		GotFirstResponseByte: func() { firstByte = true },
	}
	request = request.WithContext(httptrace.WithClientTrace(ctx, trace))

	started := time.Now()
	response, err := c.client.Do(request)
	elapsed := int(time.Since(started).Milliseconds())
	if err != nil {
		return nil, c.classifyTransportError(ctx, err, wroteRequest, firstByte)
	}
	defer response.Body.Close()

	// Read the bounded body before classifying the status so a v2 response can
	// be verified over exactly the bytes the Provider signed.
	limited := io.LimitReader(response.Body, int64(c.maxBody)+1)
	payload, readErr := io.ReadAll(limited)
	if readErr != nil {
		if c.responseSigning || response.StatusCode == http.StatusOK {
			return nil, &ProviderFailure{Kind: ProviderUnknownResult, Cause: readErr}
		}
		return nil, &ProviderFailure{
			Kind: ProviderFailed, Status: response.StatusCode,
			AuthFailure: response.StatusCode == http.StatusUnauthorized,
			Cause:       fmt.Errorf("provider returned status %d", response.StatusCode),
		}
	}
	if len(payload) > c.maxBody {
		// The response violates the frozen output bound. The output is never
		// parsed or stored; this is a definite contract failure, not an
		// accepted-but-untrusted result.
		return nil, &ProviderFailure{Kind: ProviderFailed,
			Cause: errors.New("provider response exceeds max_output_bytes")}
	}
	if c.responseSigning {
		if err := c.verifyResponseSignature(response.StatusCode, timestamp, invocationID, payload, response.Header); err != nil {
			return nil, &ProviderFailure{Kind: ProviderUnknownResult,
				IntegrityFailure: true, Cause: err}
		}
	}
	if response.StatusCode != http.StatusOK {
		return nil, &ProviderFailure{
			Kind: ProviderFailed, Status: response.StatusCode,
			AuthFailure: response.StatusCode == http.StatusUnauthorized,
			Cause:       fmt.Errorf("provider returned status %d", response.StatusCode),
		}
	}
	var envelope struct {
		InvocationID    string          `json:"invocation_id"`
		CapabilityID    string          `json:"capability_id"`
		ContractVersion string          `json:"contract_version"`
		Status          string          `json:"status"`
		Output          json.RawMessage `json:"output"`
		DurationMS      int             `json:"duration_ms"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		// A 200 with a complete body but malformed/truncated JSON cannot prove
		// that the Provider did not execute the request.
		return nil, &ProviderFailure{Kind: ProviderUnknownResult, Cause: fmt.Errorf("provider response is not valid JSON: %w", err)}
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return nil, &ProviderFailure{Kind: ProviderUnknownResult, Cause: errors.New("provider response contains more than one JSON value")}
	} else if !errors.Is(err, io.EOF) {
		return nil, &ProviderFailure{Kind: ProviderUnknownResult, Cause: err}
	}
	if envelope.InvocationID != invocationID ||
		envelope.CapabilityID != capabilityID ||
		envelope.ContractVersion != ContractVersion {
		return nil, &ProviderFailure{Kind: ProviderFailed, Cause: errors.New("provider response envelope does not match the request")}
	}
	if envelope.Status != "succeeded" {
		return nil, &ProviderFailure{Kind: ProviderFailed, Cause: errors.New("provider did not report success")}
	}
	if len(envelope.Output) == 0 {
		return nil, &ProviderFailure{Kind: ProviderFailed, Cause: errors.New("provider success response has no output")}
	}
	if envelope.DurationMS < 0 {
		envelope.DurationMS = elapsed
	}
	return &ProviderResult{
		Output:     append(json.RawMessage(nil), envelope.Output...),
		DurationMS: envelope.DurationMS, HTTPStatus: http.StatusOK,
	}, nil
}

// verifyResponseSignature enforces the v2 response profile: a versioned,
// invocation-bound HMAC over the status, request timestamp, invocation id,
// and body hash, keyed by the same key id the request used.
func (c *ExternalClient) verifyResponseSignature(status int, timestamp, invocationID string, body []byte, headers http.Header) error {
	if headers.Get("X-DW-Response-Signature-Version") != ResponseSignatureVersion {
		return errors.New("provider response is not signed with the v2 profile")
	}
	if keyID := headers.Get("X-DW-Key-Id"); keyID != c.keyID {
		return errors.New("provider response key id does not match the request")
	}
	signature := headers.Get("X-DW-Response-Signature")
	if signature == "" || signature != strings.ToLower(signature) {
		return errors.New("provider response signature is missing or not lower-case hex")
	}
	provided, err := hex.DecodeString(signature)
	if err != nil || len(provided) != sha256.Size {
		return errors.New("provider response signature has an invalid encoding")
	}
	expected, err := hex.DecodeString(responseSignature(c.secret, status, timestamp, invocationID, body))
	if err != nil {
		return errors.New("provider response signature could not be computed")
	}
	if subtle.ConstantTimeCompare(provided, expected) != 1 {
		return errors.New("provider response signature does not match")
	}
	return nil
}

func (c *ExternalClient) classifyTransportError(ctx context.Context, err error, wroteRequest, firstByte bool) error {
	if firstByte {
		return &ProviderFailure{Kind: ProviderUnknownResult, Cause: err}
	}
	if !wroteRequest {
		return &ProviderFailure{Kind: ProviderFailed, Cause: err}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || os.IsTimeout(err) {
		return &ProviderFailure{Kind: ProviderTimedOut, Cause: err}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &ProviderFailure{Kind: ProviderTimedOut, Cause: err}
	}
	// The request was fully written. The Provider may have executed it even
	// though no complete response arrived, so the result cannot be called failed.
	return &ProviderFailure{Kind: ProviderUnknownResult, Cause: err}
}

func (c *ExternalClient) preflight(ctx context.Context) error {
	addresses, err := c.resolver.LookupIPAddr(ctx, c.host)
	if err != nil {
		return fmt.Errorf("resolve provider host: %w", err)
	}
	if len(addresses) == 0 {
		return errors.New("provider host resolved to no addresses")
	}
	for _, address := range addresses {
		if err := classifyAddress(address.IP, c.provider.Network); err != nil {
			return err
		}
	}
	return nil
}

// buildProviderEnvelope writes the exact D06 envelope around the already
// validated input. It deliberately avoids json.Marshal's HTML escaping so the
// input bytes, HMAC input, and size accounting are identical.
func buildProviderEnvelope(invocationID, capabilityID string, input json.RawMessage) ([]byte, int, error) {
	if strings.ContainsAny(invocationID, `"\`) || strings.ContainsAny(capabilityID, `"\`) {
		return nil, 0, errors.New("invocation or capability id contains an unsafe character")
	}
	prefix := `{"invocation_id":"` + invocationID + `","capability_id":"` + capabilityID +
		`","contract_version":"` + ContractVersion + `","input":`
	overhead := len(prefix) + 1
	body := make([]byte, 0, len(prefix)+len(input)+1)
	body = append(body, prefix...)
	body = append(body, input...)
	body = append(body, '}')
	return body, overhead, nil
}

func signRequest(secret []byte, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

var (
	cgnatNetwork     = mustCIDR("100.64.0.0/10")
	protocolNetwork  = mustCIDR("192.0.0.0/24")
	benchmarkNetwork = mustCIDR("198.18.0.0/15")
	reservedNetwork  = mustCIDR("240.0.0.0/4")
	zeroNetwork      = mustCIDR("0.0.0.0/8")
)

func mustCIDR(value string) *net.IPNet {
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	return network
}

// classifyAddress implements the D06 §3.6.3 table. Loopback is rejected even
// for internal providers; CGNAT, link-local, metadata, multicast, and
// reserved ranges are always rejected.
func classifyAddress(ip net.IP, network string) error {
	if ip == nil {
		return errors.New("provider address is not resolvable")
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("provider address %s is not permitted", ip)
	}
	if zeroNetwork.Contains(ip) || cgnatNetwork.Contains(ip) ||
		protocolNetwork.Contains(ip) || benchmarkNetwork.Contains(ip) ||
		reservedNetwork.Contains(ip) {
		return fmt.Errorf("provider address %s is reserved", ip)
	}
	if ip.IsPrivate() {
		if network != "internal" {
			return fmt.Errorf("provider address %s is private", ip)
		}
		return nil
	}
	if !ip.IsGlobalUnicast() {
		return fmt.Errorf("provider address %s is not global unicast", ip)
	}
	return nil
}

// validateLiteralAddress applies the same table to configuration literals.
func validateLiteralAddress(ip net.IP, network string) error {
	return classifyAddress(ip, network)
}
