// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type eventTimeline struct {
	Items []struct {
		EventType string         `json:"event_type"`
		Payload   map[string]any `json:"payload"`
	} `json:"items"`
}

func (u *testUser) events(t *testing.T, objectID string) eventTimeline {
	t.Helper()
	status, body, _ := u.call(t, http.MethodGet,
		"/api/v1/objects/"+objectID+"/events", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var timeline eventTimeline
	if err := json.Unmarshal(body, &timeline); err != nil {
		t.Fatalf("decode events: %v body=%s", err, body)
	}
	return timeline
}

func (u *testUser) requireEventBodyHides(t *testing.T, objectID, privateID string) {
	t.Helper()
	status, body, _ := u.call(t, http.MethodGet,
		"/api/v1/objects/"+objectID+"/events", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), privateID) {
		t.Fatalf("event timeline leaked private source id %s: %s", privateID, body)
	}
}

func derivedPayload(t *testing.T, timeline eventTimeline) map[string]any {
	t.Helper()
	for _, item := range timeline.Items {
		if item.EventType == "object.derived" {
			return item.Payload
		}
	}
	t.Fatal("timeline has no object.derived event")
	return nil
}

// TestDWPublicDerivePrivateSourceNotInEvents verifies that a public-derived
// child never exposes its private parent object id through the events API, the
// object read, lineage, or the user export — for the deriver, for a third
// party the child is shared with, and after the publication is withdrawn.
func TestDWPublicDerivePrivateSourceNotInEvents(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "privacy-owner")
	deriver := env.register(t, "privacy-deriver")
	third := env.register(t, "privacy-third")
	stranger := env.register(t, "privacy-stranger")

	privateObject := owner.createObject(t, "collaborative", "Private source", map[string]any{
		"summary": "A private summary.",
		"body":    "A private body that is published.",
	})

	status, body, _ := owner.call(t, http.MethodPost,
		"/api/v1/objects/"+privateObject.ID+"/publications", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"derive_allowed": true, "license": "cc_by_4.0",
			},
		})
	requireStatus(t, status, http.StatusCreated, body)
	var publication struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &publication); err != nil {
		t.Fatalf("decode publication: %v", err)
	}

	status, body, _ = deriver.call(t, http.MethodPost,
		"/api/v1/public/worlds/"+publication.ID+"/derive", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"title":              "Independent public branch",
				"inheritance_fields": []string{"summary", "body"},
			},
		})
	requireStatus(t, status, http.StatusCreated, body)
	var child objectResponse
	if err := json.Unmarshal(body, &child); err != nil {
		t.Fatalf("decode child: %v", err)
	}

	// The deriver owns the child but has no access to the private parent.
	deriver.requireEventBodyHides(t, child.ID, privateObject.ID)
	payload := derivedPayload(t, deriver.events(t, child.ID))
	if _, exists := payload["parent_object_id"]; exists {
		t.Fatalf("public source projection still carries parent_object_id: %+v", payload)
	}
	if payload["publication_id"] != publication.ID {
		t.Fatalf("public source projection publication_id = %v, want %s", payload["publication_id"], publication.ID)
	}
	if payload["public"] != true {
		t.Fatalf("public source projection public = %v, want true", payload["public"])
	}

	// A third party the child is shared with sees the same safe projection.
	invitation := deriver.createInvitation(t, child.ID, []string{"read"}, 1)
	status, body = third.acceptInvitation(t, *invitation.Token)
	requireStatus(t, status, http.StatusOK, body)
	third.requireEventBodyHides(t, child.ID, privateObject.ID)

	status, body, _ = third.call(t, http.MethodGet,
		"/api/v1/objects/"+child.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), privateObject.ID) {
		t.Fatalf("object read leaked private source id: %s", body)
	}
	if !strings.Contains(string(body), publication.ID) {
		t.Fatalf("object read must use the publication-level source: %s", body)
	}
	status, body, _ = third.call(t, http.MethodGet,
		"/api/v1/objects/"+child.ID+"/lineage", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), privateObject.ID) {
		t.Fatalf("lineage leaked private source id: %s", body)
	}
	if !strings.Contains(string(body), publication.ID) {
		t.Fatalf("lineage must carry the publication-level source: %s", body)
	}
	status, body, _ = third.call(t, http.MethodGet,
		"/api/v1/objects/"+child.ID+"/lens", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), privateObject.ID) {
		t.Fatalf("lens leaked private source id: %s", body)
	}
	status, body, _ = third.call(t, http.MethodGet,
		"/api/v1/objects/"+child.ID+"/versions/1", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), privateObject.ID) {
		t.Fatalf("historical version leaked private source id: %s", body)
	}
	status, body, _ = third.call(t, http.MethodGet,
		"/api/v1/objects/"+child.ID+"/handoff", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), privateObject.ID) {
		t.Fatalf("handoff leaked private source id: %s", body)
	}
	status, body, _ = third.call(t, http.MethodGet,
		"/api/v1/public/worlds/"+publication.ID+"/lineage", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), privateObject.ID) {
		t.Fatalf("public world lineage leaked private source id: %s", body)
	}
	for _, endpoint := range []string{"/api/v1/world/stage", "/api/v1/pulse"} {
		status, body, _ = third.call(t, http.MethodGet, endpoint, callOptions{})
		requireStatus(t, status, http.StatusOK, body)
		if strings.Contains(string(body), privateObject.ID) {
			t.Fatalf("%s leaked private source id: %s", endpoint, body)
		}
	}
	status, body, _ = third.call(t, http.MethodGet, "/api/v1/me/export", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), privateObject.ID) {
		t.Fatalf("export leaked private source id: %s", body)
	}

	// Even if the third party later gains read access to the private parent,
	// the public-derived child keeps the publication-level projection.
	env.addMembership(t, mustUUID(t, privateObject.ID), third.id, []string{"read"})
	payload = derivedPayload(t, third.events(t, child.ID))
	if _, exists := payload["parent_object_id"]; exists {
		t.Fatalf("public projection flipped to the private parent: %+v", payload)
	}
	if payload["publication_id"] != publication.ID {
		t.Fatalf("public projection lost its publication source: %+v", payload)
	}

	// An unaffiliated reader cannot read the private source object at all.
	status, _, _ = stranger.call(t, http.MethodGet,
		"/api/v1/objects/"+privateObject.ID, callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("unauthorized source read status = %d, want 404", status)
	}

	// Withdrawal must not resurrect the private relation in the timeline.
	status, body, _ = owner.call(t, http.MethodDelete,
		"/api/v1/publications/"+publication.ID, callOptions{})
	requireStatus(t, status, http.StatusNoContent, body)
	for _, reader := range []*testUser{deriver, third} {
		reader.requireEventBodyHides(t, child.ID, privateObject.ID)
		payload = derivedPayload(t, reader.events(t, child.ID))
		if _, exists := payload["parent_object_id"]; exists {
			t.Fatalf("withdrawn source projection carries parent_object_id: %+v", payload)
		}
		if _, exists := payload["publication_id"]; exists {
			t.Fatalf("withdrawn source projection still advertises an active publication: %+v", payload)
		}
		if _, exists := payload["parent_version"]; exists {
			t.Fatalf("withdrawn source projection still carries parent_version: %+v", payload)
		}
		status, body, _ = reader.call(t, http.MethodGet,
			"/api/v1/objects/"+child.ID, callOptions{})
		requireStatus(t, status, http.StatusOK, body)
		if strings.Contains(string(body), privateObject.ID) {
			t.Fatalf("object read leaked private source id after withdrawal: %s", body)
		}
		status, body, _ = reader.call(t, http.MethodGet,
			"/api/v1/objects/"+child.ID+"/lineage", callOptions{})
		requireStatus(t, status, http.StatusOK, body)
		if strings.Contains(string(body), privateObject.ID) || strings.Contains(string(body), publication.ID) {
			t.Fatalf("withdrawn lineage re-exposed a source: %s", body)
		}
	}
}

// TestDWPrivateDeriveEventsRespectParentAccess verifies that authorized
// private derivation is unchanged, while a member who only received the child
// cannot learn the private parent id from the child timeline.
func TestDWPrivateDeriveEventsRespectParentAccess(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "derive-owner")
	member := env.register(t, "derive-member")

	parent := owner.createObject(t, "collaborative", "Private parent", map[string]any{
		"summary": "Private summary.",
		"body":    "Private body.",
	})
	status, body, _ := owner.call(t, http.MethodPost,
		"/api/v1/objects/"+parent.ID+"/derive", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"title":              "Private child",
				"source_version":     parent.Version,
				"inheritance_fields": []string{"summary", "body"},
			},
		})
	requireStatus(t, status, http.StatusCreated, body)
	var child objectResponse
	if err := json.Unmarshal(body, &child); err != nil {
		t.Fatalf("decode child: %v", err)
	}

	// The parent owner may still see the real private source id.
	payload := derivedPayload(t, owner.events(t, child.ID))
	if payload["parent_object_id"] != parent.ID {
		t.Fatalf("authorized private derive parent id = %v, want %s", payload["parent_object_id"], parent.ID)
	}

	// A member with child-only access does not.
	invitation := owner.createInvitation(t, child.ID, []string{"read"}, 1)
	status, body = member.acceptInvitation(t, *invitation.Token)
	requireStatus(t, status, http.StatusOK, body)
	member.requireEventBodyHides(t, child.ID, parent.ID)
	payload = derivedPayload(t, member.events(t, child.ID))
	if _, exists := payload["parent_object_id"]; exists {
		t.Fatalf("child-only member saw parent_object_id: %+v", payload)
	}
	if _, exists := payload["publication_id"]; exists {
		t.Fatalf("private derive must not invent a publication source: %+v", payload)
	}
}
