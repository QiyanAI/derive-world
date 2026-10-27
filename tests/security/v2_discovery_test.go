// SPDX-License-Identifier: Apache-2.0
//go:build integration

package security_test

import (
    "encoding/json"
    "net/http"
    "net/url"
    "testing"
)

func TestV2FriendDiscoveryIsExplicitlyOptIn(t *testing.T) {
    env := newTestEnv(t)
    alice := env.register(t, "v2-searcher")
    bob := env.register(t, "v2-target")
    carol := env.register(t, "v2-private")
    _ = carol

    search := func(query string) []struct {
        UserID string `json:"user_id"`
        Handle string `json:"handle"`
        DisplayName string `json:"display_name"`
    } {
        t.Helper()
        status, body, _ := alice.call(t, http.MethodGet,
            "/api/v1/social/discovery?q="+url.QueryEscape(query), callOptions{})
        if status != http.StatusOK { t.Fatalf("search status = %d body=%s", status, body) }
        var response struct {
            Items []struct {
                UserID string `json:"user_id"`
                Handle string `json:"handle"`
                DisplayName string `json:"display_name"`
            } `json:"items"`
        }
        if err := json.Unmarshal(body, &response); err != nil { t.Fatal(err) }
        return response.Items
    }

    if len(search("v2-target")) != 0 { t.Fatal("default-private account was discovered") }
    status, body, _ := bob.call(t, http.MethodGet, "/api/v1/social/discovery/me", callOptions{})
    if status != http.StatusOK || string(body) == "" { t.Fatalf("default status %d: %s", status, body) }
    var preference map[string]bool
    if err := json.Unmarshal(body, &preference); err != nil { t.Fatal(err) }
    if preference["discoverable"] { t.Fatal("discoverable should default false") }

    status, body, _ = bob.call(t, http.MethodPut, "/api/v1/social/discovery/me", callOptions{
        Body: map[string]any{"discoverable": true},
    })
    if status != http.StatusOK { t.Fatalf("opt in status %d: %s", status, body) }
    matches := search("v2-target")
    if len(matches) != 1 || matches[0].UserID != bob.id.String() {
        t.Fatalf("opt-in results = %+v", matches)
    }
    if len(search("@"+bob.handle)) != 1 { t.Fatal("opt-in handle search failed") }
    if len(search("v2-private")) != 0 { t.Fatal("private account leaked") }
    if len(search("%")) != 0 { t.Fatal("wildcard enumeration enabled") }

    status, body, _ = alice.call(t, http.MethodPost, "/api/v1/social/blocks", callOptions{
        Body: map[string]any{"blocked_user_id": bob.id.String()},
    })
    if status != http.StatusCreated && status != http.StatusOK {
        t.Fatalf("block status = %d body = %s", status, body)
    }
    if len(search("v2-target")) != 0 { t.Fatal("blocked user leaked into search") }
    status, body, _ = alice.call(t, http.MethodDelete,
        "/api/v1/social/blocks/"+bob.id.String(), callOptions{})
    if status != http.StatusNoContent && status != http.StatusOK {
        t.Fatalf("unblock status %d: %s", status, body)
    }
    if len(search("v2-target")) != 1 { t.Fatal("unblock did not restore opt-in search") }

    status, body, _ = bob.call(t, http.MethodPut, "/api/v1/social/discovery/me", callOptions{
        Body: map[string]any{"discoverable": false},
    })
    if status != http.StatusOK { t.Fatalf("opt out status %d: %s", status, body) }
    if len(search("v2-target")) != 0 { t.Fatal("opt out not enforced") }
}
