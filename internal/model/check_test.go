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
		{"unexpected", http.StatusInternalServerError, ErrCheckStatus},
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
