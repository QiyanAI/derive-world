// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client is a minimal GitHub REST client. It never logs the token.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient creates a client with a bounded timeout.
func NewClient(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

// HTTPError is a non-2xx GitHub response.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("github status %d: %s", e.Status, e.Message)
}

func (c *Client) get(ctx context.Context, path string, out any) (http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "derive-world-ai-ops/0.1.0")
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}

	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = response.Status
		}
		return response.Header, &HTTPError{Status: response.StatusCode, Message: message}
	}
	if out != nil && response.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(out); err != nil {
			return response.Header, err
		}
	}
	return response.Header, nil
}

func rateLimitRemaining(header http.Header) int {
	value := header.Get("X-RateLimit-Remaining")
	if value == "" {
		return 0
	}
	remaining, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return remaining
}

// pageCount returns the item count using GitHub's Link header when present.
func pageCount(header http.Header, perPage, currentLen int) int {
	link := header.Get("Link")
	if link == "" || currentLen == 0 {
		return currentLen
	}
	for _, part := range strings.Split(link, ",") {
		if !strings.Contains(part, `rel="last"`) {
			continue
		}
		start := strings.Index(part, "&page=")
		if start < 0 {
			start = strings.Index(part, "?page=")
			if start < 0 {
				continue
			}
			start += len("?page=")
		} else {
			start += len("&page=")
		}
		end := start
		for end < len(part) && part[end] >= '0' && part[end] <= '9' {
			end++
		}
		last, err := strconv.Atoi(part[start:end])
		if err != nil || last < 1 {
			continue
		}
		return (last-1)*perPage + currentLen
	}
	return currentLen
}
