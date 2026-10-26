// SPDX-License-Identifier: Apache-2.0
package model

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckConnectionFixtureMakesNoCall(t *testing.T) {
	if err := CheckConnection(context.Background(), AdapterOptions{Mode: "fixture"}); err != nil {
		t.Fatalf("fixture check = %v", err)
	}
}

func TestCheckConnectionRealStatuses(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr error
	}{
		{"ok", http.StatusOK, nil},
		{"unauthorized", http.StatusUnauthorized, ErrCheckUnauthorized},
		{"forbidden", http.StatusForbidden, ErrCheckUnauthorized},
		{"rate_limited", http.StatusTooManyRequests, ErrCheckRateLimited},
		{"unavailable", http.StatusInternalServerError, ErrCheckUnavailable},
		{"unexpected", http.StatusBadRequest, ErrCheckStatus},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models" {
					t.Errorf("check path = %s", r.URL.Path)
				}
				w.WriteHeader(testCase.status)
			}))
			defer server.Close()
			err := CheckConnection(context.Background(), AdapterOptions{
				Mode: "real", BaseURL: server.URL, APIKey: "test-key",
				Timeout: 2 * time.Second,
			})
			if testCase.wantErr == nil {
				if err != nil {
					t.Fatalf("check = %v", err)
				}
				return
			}
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("check = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestCheckConnectionDoesNotFollowRedirects(t *testing.T) {
	targetHit := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetHit = true
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	err := CheckConnection(context.Background(), AdapterOptions{
		Mode: "real", BaseURL: redirect.URL, APIKey: "test-key", Timeout: 2 * time.Second,
	})
	if !errors.Is(err, ErrCheckStatus) {
		t.Fatalf("check = %v, want unexpected status for a redirect", err)
	}
	if targetHit {
		t.Fatal("model check followed a redirect and could have forwarded the API key")
	}
}

func TestCheckConnectionInvalidURL(t *testing.T) {
	for _, value := range []string{"", "not-a-url", "ftp://example.test", "https://"} {
		err := CheckConnection(context.Background(), AdapterOptions{
			Mode: "real", BaseURL: value, APIKey: "test-key", Timeout: time.Second,
		})
		if !errors.Is(err, ErrCheckInvalidURL) {
			t.Fatalf("check(%q) = %v, want invalid URL", value, err)
		}
	}
}

func TestCheckConnectionUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := server.URL
	server.Close()
	err := CheckConnection(context.Background(), AdapterOptions{
		Mode: "real", BaseURL: baseURL, Timeout: time.Second,
	})
	if !errors.Is(err, ErrCheckUnreachable) {
		t.Fatalf("check = %v, want unreachable", err)
	}
}
