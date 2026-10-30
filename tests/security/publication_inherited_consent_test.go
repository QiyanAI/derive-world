// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A private derive copies source fields into the child. Publishing that child
// must not expose the source author's private text without their consent.
func TestDWPublicationDerivedContentRequiresSourceConsent(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "inherited-owner")
	deriver := env.register(t, "inherited-deriver")

	const secret = "TOP-SECRET-source-body"
	source := owner.createObject(t, "collaborative", "Private source", map[string]any{
		"summary": "A private source summary.",
		"body":    secret,
	})
	invitation := owner.createInvitation(t, source.ID, []string{"read", "derive"}, 1)
	status, body := deriver.acceptInvitation(t, *invitation.Token)
	requireStatus(t, status, http.StatusOK, body)

	status, body, _ = deriver.call(t, http.MethodPost,
		"/api/v1/objects/"+source.ID+"/derive", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"title":              "Derived copy",
				"inheritance_fields": []string{"summary", "body"},
			},
		})
	requireStatus(t, status, http.StatusCreated, body)
	var child objectResponse
	if err := json.Unmarshal(body, &child); err != nil {
		t.Fatalf("decode derived child: %v", err)
	}

	status, body, _ = deriver.call(t, http.MethodPost,
		"/api/v1/objects/"+child.ID+"/publications", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body:           map[string]any{"license": "cc_by_4.0"},
		})
	requireStatus(t, status, http.StatusCreated, body)
	var publication struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &publication); err != nil {
		t.Fatalf("decode publication: %v", err)
	}
	if publication.Status != "pending_consent" {
		t.Fatalf("publication status = %q, want pending_consent: the source author's private text was copied into the child", publication.Status)
	}

	anonymous := newClient(t)
	status, body, _ = env.call(t, anonymous, http.MethodGet,
		"/api/v1/public/worlds/"+publication.ID, callOptions{})
	if status == http.StatusOK && strings.Contains(string(body), secret) {
		t.Fatalf("anonymous public read exposed inherited private text: %s", body)
	}
}

// Content derived from an active public snapshot is already public under the
// publisher's license; republishing it must not require the source owner again.
func TestDWPublicationOfPublicDerivedChildNeedsNoExtraSourceConsent(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "public-source-owner")
	visitor := env.register(t, "public-deriver")

	source := owner.createObject(t, "collaborative", "Public source", map[string]any{
		"summary": "Published under a permissive license.",
		"body":    "The public snapshot body.",
	})
	status, body, _ := owner.call(t, http.MethodPost,
		"/api/v1/objects/"+source.ID+"/publications", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body:           map[string]any{"license": "cc_by_4.0", "derive_allowed": true},
		})
	requireStatus(t, status, http.StatusCreated, body)
	var publication struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &publication); err != nil {
		t.Fatalf("decode publication: %v", err)
	}
	if publication.Status != "active" {
		t.Fatalf("source publication status = %q, want active", publication.Status)
	}

	status, body, _ = visitor.call(t, http.MethodPost,
		"/api/v1/public/worlds/"+publication.ID+"/derive", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"title":              "Public branch",
				"inheritance_fields": []string{"summary", "body"},
			},
		})
	requireStatus(t, status, http.StatusCreated, body)
	var child objectResponse
	if err := json.Unmarshal(body, &child); err != nil {
		t.Fatalf("decode public-derived child: %v", err)
	}

	status, body, _ = visitor.call(t, http.MethodPost,
		"/api/v1/objects/"+child.ID+"/publications", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body:           map[string]any{"license": "cc_by_4.0"},
		})
	requireStatus(t, status, http.StatusCreated, body)
	var childPublication struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &childPublication); err != nil {
		t.Fatalf("decode child publication: %v", err)
	}
	if childPublication.Status != "active" {
		t.Fatalf("child publication status = %q, want active for already-public content", childPublication.Status)
	}
}
