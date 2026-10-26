// SPDX-License-Identifier: Apache-2.0
package model

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Connection status categories returned by CheckConnection. They are safe to
// show to an authenticated deployment member and never include the provider
// response body, the API key, or the raw transport error.
var (
	ErrCheckUnauthorized = errors.New("model credentials were rejected")
	ErrCheckUnreachable  = errors.New("model endpoint is unreachable")
	ErrCheckStatus       = errors.New("model endpoint returned an unexpected status")
	ErrCheckInvalidURL   = errors.New("model endpoint URL is invalid")
	ErrCheckTLS          = errors.New("model endpoint TLS verification failed")
	ErrCheckRateLimited  = errors.New("model endpoint is rate limiting checks")
	ErrCheckUnavailable  = errors.New("model endpoint is temporarily unavailable")
)

// CheckConnection verifies that a Real provider endpoint answers a models
// listing request. It never sends a prompt and therefore does not consume
// completion tokens. Fixture mode performs no network call.
func CheckConnection(ctx context.Context, options AdapterOptions) error {
	if strings.TrimSpace(options.Mode) != "real" {
		return nil
	}
	baseURL := normalizeBaseURL(options.BaseURL)
	if baseURL == "" {
		return ErrCheckInvalidURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ErrCheckInvalidURL
	}
	timeout := options.Timeout
	if timeout <= 0 || timeout > 15*time.Second {
		timeout = 10 * time.Second
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(checkCtx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return ErrCheckInvalidURL
	}
	request.Header.Set("Accept", "application/json")
	if options.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+options.APIKey)
	}
	transport := &http.Transport{
		// Match the completion client: no environment proxy, TLS 1.2 minimum,
		// and no redirects that could forward the API key to another host.
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 64 << 10,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "tls") || strings.Contains(message, "x509") {
			return ErrCheckTLS
		}
		return ErrCheckUnreachable
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return nil
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return ErrCheckUnauthorized
	case response.StatusCode == http.StatusTooManyRequests:
		return ErrCheckRateLimited
	case response.StatusCode >= 500:
		return ErrCheckUnavailable
	default:
		return fmt.Errorf("%w: %d", ErrCheckStatus, response.StatusCode)
	}
}
