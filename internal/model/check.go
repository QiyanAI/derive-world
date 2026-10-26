// SPDX-License-Identifier: Apache-2.0
package model

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
		return ErrCheckUnreachable
	}
	timeout := options.Timeout
	if timeout <= 0 || timeout > 15*time.Second {
		timeout = 10 * time.Second
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(checkCtx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return ErrCheckUnreachable
	}
	request.Header.Set("Accept", "application/json")
	if options.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+options.APIKey)
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return ErrCheckUnreachable
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return nil
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return ErrCheckUnauthorized
	default:
		return fmt.Errorf("%w: %d", ErrCheckStatus, response.StatusCode)
	}
}
