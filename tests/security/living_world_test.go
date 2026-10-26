// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDWSceneLifecycleDeriveAndLens(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "scene")

	scene := owner.createObject(t, "living_scene", "Launch plan", map[string]any{
		"purpose": "project",
		"premise": "Ship a small useful product.",
		"elements": []map[string]any{
			{"kind": "idea", "text": "Pick one user problem."},
			{"kind": "step", "text": "Interview five users."},
		},
		"tags": []string{"plan"},
	})
	if scene.ObjectType != "living_scene" || scene.PresentationTemplate != "living_scene_v1" {
		t.Fatalf("scene type/template = %s/%s", scene.ObjectType, scene.PresentationTemplate)
	}

	status, body, updated := owner.postAction(t, scene.ID, "add_element",
		map[string]any{"kind": "choice", "text": "Choose the first release scope."}, scene.Version)
	if status != http.StatusOK {
		t.Fatalf("add_element status = %d body = %s", status, body)
	}
	if updated.Version != 2 {
		t.Fatalf("version after add = %d, want 2", updated.Version)
	}
	elements, _ := updated.State["elements"].([]any)
	if len(elements) != 3 {
		t.Fatalf("elements = %d, want 3", len(elements))
	}
	elementID := elements[2].(map[string]any)["id"].(string)

	status, body, updated = owner.postAction(t, scene.ID, "update_element",
		map[string]any{"element_id": elementID, "status": "done"}, updated.Version)
	if status != http.StatusOK {
		t.Fatalf("update_element status = %d body = %s", status, body)
	}
	status, body, updated = owner.postAction(t, scene.ID, "move_element",
		map[string]any{"element_id": elementID, "position": 0}, updated.Version)
	if status != http.StatusOK {
		t.Fatalf("move_element status = %d body = %s", status, body)
	}

	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+scene.ID+"/derive", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"title":              "Launch plan — alternate",
			"source_version":     updated.Version,
			"inheritance_fields": []string{"premise", "elements", "tags"},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("derive scene status = %d body = %s", status, body)
	}
	var child objectResponse
	if err := json.Unmarshal(body, &child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	if child.ObjectType != "living_scene" || child.Lineage == nil ||
		child.Lineage.Parent.Version != updated.Version {
		t.Fatalf("child scene/lineage = %+v", child)
	}
	childElements, _ := child.State["elements"].([]any)
	if len(childElements) != 3 {
		t.Fatalf("child elements = %d, want 3", len(childElements))
	}
	if childElements[0].(map[string]any)["id"] == elementID {
		t.Fatal("derived elements must receive new server ids")
	}

	status, body, _ = owner.call(t, http.MethodGet, "/api/v1/objects/"+scene.ID+"/lens", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("lens status = %d body = %s", status, body)
	}
	var lens struct {
		Versions          []map[string]any `json:"versions"`
		Children          []map[string]any `json:"children"`
		InheritanceFields []string         `json:"inheritance_fields"`
	}
	if err := json.Unmarshal(body, &lens); err != nil {
		t.Fatalf("decode lens: %v", err)
	}
	if len(lens.Versions) != updated.Version {
		t.Fatalf("lens versions = %d, want %d", len(lens.Versions), updated.Version)
	}
	if len(lens.Children) != 1 || lens.Children[0]["object_id"] != child.ID {
		t.Fatalf("lens children = %+v, want child %s", lens.Children, child.ID)
	}
	if strings.Join(lens.InheritanceFields, ",") != "premise,elements,tags" {
		t.Fatalf("inheritance fields = %v", lens.InheritanceFields)
	}

	status, body, _ = owner.call(t, http.MethodGet,
		fmt.Sprintf("/api/v1/objects/%s/versions/1", scene.ID), callOptions{})
	if status != http.StatusOK {
		t.Fatalf("version status = %d body = %s", status, body)
	}
	var historical struct {
		Version int            `json:"version"`
		State   map[string]any `json:"state"`
	}
	if err := json.Unmarshal(body, &historical); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	if historical.Version != 1 {
		t.Fatalf("historical version = %d", historical.Version)
	}
	historicalElements, _ := historical.State["elements"].([]any)
	if len(historicalElements) != 2 {
		t.Fatalf("historical elements = %d, want 2", len(historicalElements))
	}

	status, body, _ = owner.postAction(t, scene.ID, "add_element",
		map[string]any{"kind": "not_a_kind", "text": "invalid"}, updated.Version)
	if status != http.StatusBadRequest {
		t.Fatalf("invalid scene kind status = %d body = %s", status, body)
	}

	stranger := env.register(t, "scene-stranger")
	status, _, _ = stranger.call(t, http.MethodGet,
		fmt.Sprintf("/api/v1/objects/%s/versions/1", scene.ID), callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("stranger version status = %d, want 404", status)
	}
	status, _, _ = stranger.call(t, http.MethodGet,
		"/api/v1/objects/"+scene.ID+"/lens", callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("stranger lens status = %d, want 404", status)
	}

	// The 33rd element must be rejected by the scene bound.
	for index := 0; index < 29; index++ {
		status, body, updated = owner.postAction(t, scene.ID, "add_element",
			map[string]any{"kind": "idea", "text": fmt.Sprintf("idea %d", index)}, updated.Version)
		if status != http.StatusOK {
			t.Fatalf("add element %d status = %d body = %s", index, status, body)
		}
	}
	status, body, _ = owner.postAction(t, scene.ID, "add_element",
		map[string]any{"kind": "idea", "text": "overflow"}, updated.Version)
	if status != http.StatusConflict {
		t.Fatalf("33rd element status = %d body = %s", status, body)
	}
}

func TestDWSocialFriendshipMessagesAndBlocks(t *testing.T) {
	env := newTestEnv(t)
	alice := env.register(t, "alice")
	bob := env.register(t, "bob")
	carol := env.register(t, "carol")

	status, body, _ := alice.call(t, http.MethodPost, "/api/v1/social/friend-requests", callOptions{
		Body: map[string]any{"handle": bob.handle},
	})
	if status != http.StatusCreated {
		t.Fatalf("send friend request status = %d body = %s", status, body)
	}
	var request struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	status, body, _ = bob.call(t, http.MethodGet, "/api/v1/social/friend-requests?direction=incoming", callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), alice.handle) {
		t.Fatalf("list incoming status = %d body = %s", status, body)
	}
	status, body, _ = bob.call(t, http.MethodPost,
		"/api/v1/social/friend-requests/"+request.ID+"/accept", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("accept status = %d body = %s", status, body)
	}

	clientMessageID := uuid.NewString()
	status, body, _ = alice.call(t, http.MethodPost, "/api/v1/social/messages", callOptions{
		Body: map[string]any{
			"to_user_id": bob.id.String(), "body": "Want to build this together?",
			"client_message_id": clientMessageID,
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("send message status = %d body = %s", status, body)
	}
	var firstMessage struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &firstMessage); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	status, body, _ = alice.call(t, http.MethodPost, "/api/v1/social/messages", callOptions{
		Body: map[string]any{
			"to_user_id": bob.id.String(), "body": "Want to build this together?",
			"client_message_id": clientMessageID,
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("replayed message status = %d body = %s", status, body)
	}
	var replay struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &replay)
	if replay.ID != firstMessage.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, firstMessage.ID)
	}
	status, body, _ = alice.call(t, http.MethodPost, "/api/v1/social/messages", callOptions{
		Body: map[string]any{
			"to_user_id": bob.id.String(), "body": "different intent",
			"client_message_id": clientMessageID,
		},
	})
	if status != http.StatusConflict {
		t.Fatalf("reused client message id status = %d body = %s", status, body)
	}

	status, body, _ = bob.call(t, http.MethodGet,
		"/api/v1/social/messages?with="+alice.id.String(), callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), firstMessage.ID) {
		t.Fatalf("list messages status = %d body = %s", status, body)
	}
	status, body, _ = bob.call(t, http.MethodPost, "/api/v1/social/messages/read", callOptions{
		Body: map[string]any{"with_user_id": alice.id.String()},
	})
	if status != http.StatusOK {
		t.Fatalf("mark read status = %d body = %s", status, body)
	}

	status, body, _ = carol.call(t, http.MethodPost, "/api/v1/social/messages", callOptions{
		Body: map[string]any{
			"to_user_id": bob.id.String(), "body": "stranger message",
			"client_message_id": uuid.NewString(),
		},
	})
	if status != http.StatusForbidden {
		t.Fatalf("stranger message status = %d body = %s", status, body)
	}

	status, body, _ = bob.call(t, http.MethodPost, "/api/v1/social/blocks", callOptions{
		Body: map[string]any{"blocked_user_id": alice.id.String()},
	})
	if status != http.StatusNoContent {
		t.Fatalf("block status = %d body = %s", status, body)
	}
	status, body, _ = alice.call(t, http.MethodPost, "/api/v1/social/messages", callOptions{
		Body: map[string]any{
			"to_user_id": bob.id.String(), "body": "after block",
			"client_message_id": uuid.NewString(),
		},
	})
	if status != http.StatusForbidden {
		t.Fatalf("message after block status = %d body = %s", status, body)
	}
	status, body, _ = bob.call(t, http.MethodDelete,
		"/api/v1/social/blocks/"+alice.id.String(), callOptions{})
	if status != http.StatusNoContent {
		t.Fatalf("unblock status = %d body = %s", status, body)
	}
	status, body, _ = alice.call(t, http.MethodPost, "/api/v1/social/messages", callOptions{
		Body: map[string]any{
			"to_user_id": bob.id.String(), "body": "after unblock",
			"client_message_id": uuid.NewString(),
		},
	})
	if status != http.StatusForbidden {
		t.Fatalf("message after unblock status = %d body = %s (friendship must stay removed)", status, body)
	}

	// Restore the friendship, then verify removal ends messaging.
	status, body, _ = alice.call(t, http.MethodPost, "/api/v1/social/friend-requests", callOptions{
		Body: map[string]any{"handle": bob.handle},
	})
	if status != http.StatusCreated {
		t.Fatalf("re-request status = %d body = %s", status, body)
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode re-request: %v", err)
	}
	status, body, _ = bob.call(t, http.MethodPost,
		"/api/v1/social/friend-requests/"+request.ID+"/accept", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("re-accept status = %d body = %s", status, body)
	}
	status, body, _ = alice.call(t, http.MethodDelete,
		"/api/v1/social/friends/"+bob.id.String(), callOptions{})
	if status != http.StatusNoContent {
		t.Fatalf("remove friend status = %d body = %s", status, body)
	}
	status, body, _ = alice.call(t, http.MethodPost, "/api/v1/social/messages", callOptions{
		Body: map[string]any{
			"to_user_id": bob.id.String(), "body": "after removal",
			"client_message_id": uuid.NewString(),
		},
	})
	if status != http.StatusForbidden {
		t.Fatalf("message after friend removal status = %d body = %s", status, body)
	}
}

func TestDWObjectDiscussionRequiresMembership(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "discussion-owner")
	stranger := env.register(t, "discussion-stranger")
	object := owner.createObject(t, "collaborative", "Discussion object", map[string]any{})

	status, body, _ := owner.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/discussions", callOptions{
			Body: map[string]any{
				"body": "First human note.", "client_message_id": uuid.NewString(),
			},
		})
	if status != http.StatusCreated {
		t.Fatalf("post discussion status = %d body = %s", status, body)
	}
	status, body, _ = owner.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/discussions", callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), "First human note.") {
		t.Fatalf("list discussion status = %d body = %s", status, body)
	}
	status, _, _ = stranger.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/discussions", callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("stranger discussion read status = %d, want 404", status)
	}
	status, _, _ = stranger.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/discussions", callOptions{
			Body: map[string]any{
				"body": "unauthorized", "client_message_id": uuid.NewString(),
			},
		})
	if status != http.StatusNotFound {
		t.Fatalf("stranger discussion write status = %d, want 404", status)
	}
}

func TestDWPublicationPublicReadDeriveAndRevoke(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "publisher")
	visitor := env.register(t, "visitor")
	object := owner.createObject(t, "collaborative", "Public launch", map[string]any{
		"summary": "A controlled public snapshot.",
		"body":    "Private body that is published.",
	})

	status, body, _ := owner.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/publications", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"derive_allowed": true, "license": "cc_by_4.0",
			},
		})
	if status != http.StatusCreated {
		t.Fatalf("create publication status = %d body = %s", status, body)
	}
	var publication struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &publication); err != nil {
		t.Fatalf("decode publication: %v", err)
	}
	if publication.Status != "active" {
		t.Fatalf("publication status = %s, want active", publication.Status)
	}

	anonymous := newClient(t)
	status, body, _ = env.call(t, anonymous, http.MethodGet,
		"/api/v1/public/worlds/"+publication.ID, callOptions{})
	if status != http.StatusOK {
		t.Fatalf("anonymous public read status = %d body = %s", status, body)
	}
	if strings.Contains(string(body), "shared_context") ||
		strings.Contains(string(body), "memberships") ||
		strings.Contains(string(body), "private_ai") {
		t.Fatalf("public snapshot leaked private fields: %s", body)
	}
	status, _, _ = env.call(t, anonymous, http.MethodGet,
		"/api/v1/objects/"+object.ID, callOptions{})
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous private object status = %d, want 401", status)
	}

	status, body, _ = visitor.call(t, http.MethodPost,
		"/api/v1/public/worlds/"+publication.ID+"/derive", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"title":              "My independent branch",
				"inheritance_fields": []string{"summary", "body"},
			},
		})
	if status != http.StatusCreated {
		t.Fatalf("public derive status = %d body = %s", status, body)
	}
	var child objectResponse
	if err := json.Unmarshal(body, &child); err != nil {
		t.Fatalf("decode public child: %v", err)
	}
	if child.Lineage == nil || child.Lineage.Parent.ObjectID != publication.ID {
		t.Fatalf("public lineage must reference the publication, got %+v", child.Lineage)
	}
	status, body, _ = visitor.call(t, http.MethodGet, "/api/v1/objects/"+child.ID, callOptions{})
	if status != http.StatusOK {
		t.Fatalf("child owner read status = %d body = %s", status, body)
	}
	status, _, _ = owner.call(t, http.MethodGet, "/api/v1/objects/"+child.ID, callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("source owner read child status = %d, want 404 (child is private)", status)
	}

	status, body, _ = visitor.call(t, http.MethodPost,
		"/api/v1/public/worlds/"+publication.ID+"/report", callOptions{
			Body: map[string]any{"reason": "Test abuse report."},
		})
	if status != http.StatusNoContent {
		t.Fatalf("report status = %d body = %s", status, body)
	}
	status, body, _ = visitor.call(t, http.MethodPost,
		"/api/v1/public/worlds/"+publication.ID+"/report", callOptions{
			Body: map[string]any{"reason": "A repeated report."},
		})
	if status != http.StatusNoContent {
		t.Fatalf("repeated report status = %d body = %s", status, body)
	}
	if reports := env.count(t, `
SELECT count(*) FROM publication_reports WHERE publication_id = $1`,
		publication.ID); reports != 1 {
		t.Fatalf("report rows = %d, want 1", reports)
	}
	status, _, _ = env.call(t, anonymous, http.MethodPost,
		"/api/v1/public/worlds/"+publication.ID+"/report", callOptions{
			Body: map[string]any{"reason": "anonymous report"},
		})
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous report status = %d, want 401", status)
	}

	status, body, _ = owner.call(t, http.MethodDelete,
		"/api/v1/publications/"+publication.ID, callOptions{})
	if status != http.StatusNoContent {
		t.Fatalf("revoke status = %d body = %s", status, body)
	}
	status, _, _ = env.call(t, anonymous, http.MethodGet,
		"/api/v1/public/worlds/"+publication.ID, callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("revoked public read status = %d, want 404", status)
	}
	status, _, _ = visitor.call(t, http.MethodPost,
		"/api/v1/public/worlds/"+publication.ID+"/derive", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"title": "Post-revoke branch", "inheritance_fields": []string{"summary"},
			},
		})
	if status != http.StatusNotFound {
		t.Fatalf("post-revoke derive status = %d, want 404", status)
	}
}

func TestDWPublicationContributorConsent(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "consent-owner")
	contributor := env.register(t, "consent-contributor")
	object := owner.createObject(t, "collaborative", "Shared work", map[string]any{
		"summary": "Owner start.",
	})
	contributorUUID, _ := uuid.Parse(contributor.id.String())
	objectUUID, _ := uuid.Parse(object.ID)
	env.addMembership(t, objectUUID, contributorUUID, []string{"read", "contribute", "derive"})

	status, body, _ := contributor.postAction(t, object.ID, "set_summary",
		map[string]any{"summary": "Contributor wrote this."}, object.Version)
	if status != http.StatusOK {
		t.Fatalf("contributor action status = %d body = %s", status, body)
	}

	status, body, _ = owner.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/publications", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"derive_allowed": false, "license": "all_rights_reserved",
			},
		})
	if status != http.StatusCreated {
		t.Fatalf("publish with contributor status = %d body = %s", status, body)
	}
	var publication struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &publication); err != nil {
		t.Fatalf("decode publication: %v", err)
	}
	if publication.Status != "pending_consent" {
		t.Fatalf("publication status = %s, want pending_consent", publication.Status)
	}
	anonymous := newClient(t)
	status, _, _ = env.call(t, anonymous, http.MethodGet,
		"/api/v1/public/worlds/"+publication.ID, callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("pending publication anonymous status = %d, want 404", status)
	}

	status, body, _ = contributor.call(t, http.MethodGet,
		"/api/v1/publication-consents", callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), publication.ID) {
		t.Fatalf("consent list status = %d body = %s", status, body)
	}
	var consentList struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &consentList); err != nil {
		t.Fatalf("decode consents: %v", err)
	}
	if len(consentList.Items) != 1 {
		t.Fatalf("consents = %d, want 1", len(consentList.Items))
	}
	status, body, _ = contributor.call(t, http.MethodPost,
		"/api/v1/publication-consents/"+consentList.Items[0].ID+"/approve", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("approve consent status = %d body = %s", status, body)
	}
	status, body, _ = env.call(t, anonymous, http.MethodGet,
		"/api/v1/public/worlds/"+publication.ID, callOptions{})
	if status != http.StatusOK {
		t.Fatalf("approved publication status = %d body = %s", status, body)
	}
	visitor := env.register(t, "consent-visitor")
	status, body, _ = visitor.call(t, http.MethodPost,
		"/api/v1/public/worlds/"+publication.ID+"/derive", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"title": "Forbidden branch", "inheritance_fields": []string{"summary"},
			},
		})
	if status != http.StatusNotFound {
		t.Fatalf("view-only publication derive status = %d body = %s, want 404", status, body)
	}

	// A declined contributor blocks public visibility.
	object2 := owner.createObject(t, "collaborative", "Declined work", map[string]any{})
	object2UUID, _ := uuid.Parse(object2.ID)
	env.addMembership(t, object2UUID, contributorUUID, []string{"read", "contribute"})
	contributor.postAction(t, object2.ID, "set_summary", map[string]any{"summary": "contribution"}, object2.Version)
	status, body, _ = owner.call(t, http.MethodPost,
		"/api/v1/objects/"+object2.ID+"/publications", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"derive_allowed": false, "license": "all_rights_reserved",
			},
		})
	if status != http.StatusCreated {
		t.Fatalf("second publish status = %d body = %s", status, body)
	}
	var publication2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &publication2); err != nil {
		t.Fatalf("decode second publication: %v", err)
	}
	status, body, _ = contributor.call(t, http.MethodGet, "/api/v1/publication-consents", callOptions{})
	if err := json.Unmarshal(body, &consentList); err != nil {
		t.Fatalf("decode second consents: %v", err)
	}
	if len(consentList.Items) != 1 {
		t.Fatalf("second consents = %d, want 1", len(consentList.Items))
	}
	status, body, _ = contributor.call(t, http.MethodPost,
		"/api/v1/publication-consents/"+consentList.Items[0].ID+"/decline", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("decline consent status = %d body = %s", status, body)
	}
	status, _, _ = env.call(t, anonymous, http.MethodGet,
		"/api/v1/public/worlds/"+publication2.ID, callOptions{})
	if status != http.StatusNotFound {
		t.Fatalf("declined publication status = %d, want 404", status)
	}
}

func TestDWStreamSendsReadyAndRefreshSignals(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "stream")

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, env.server.URL+"/api/v1/stream", nil)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	response, err := user.client.Do(request)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", response.StatusCode)
	}
	reader := bufio.NewReader(response.Body)
	if line, err := reader.ReadString('\n'); err != nil || !strings.Contains(line, "retry:") {
		t.Fatalf("stream retry line = %q err = %v", line, err)
	}
	foundReady := false
	for index := 0; index < 6; index++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read ready event: %v", err)
		}
		if strings.Contains(line, "event: ready") {
			foundReady = true
			break
		}
	}
	if !foundReady {
		t.Fatal("stream did not send a ready event")
	}

	user.createObject(t, "collaborative", "Stream trigger", map[string]any{})
	foundRefresh := false
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.Contains(line, "event: refresh") {
			foundRefresh = true
			break
		}
	}
	if !foundRefresh {
		t.Fatal("stream did not deliver a refresh signal after a real write")
	}
}

func TestDWSystemStatusReportsNonSecretFacts(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "status")
	status, body, _ := user.call(t, http.MethodGet, "/api/v1/system/status", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("status endpoint = %d body = %s", status, body)
	}
	var payload struct {
		ModelMode  string `json:"model_mode"`
		DatabaseOK bool   `json:"database_ok"`
		Quota      struct {
			DailyCalls int `json:"daily_calls"`
		} `json:"quota"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if payload.ModelMode != "fixture" || !payload.DatabaseOK {
		t.Fatalf("status payload = %+v", payload)
	}
	if strings.Contains(string(body), "api_key") || strings.Contains(string(body), "password") {
		t.Fatalf("status leaked a secret: %s", body)
	}
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/system/model-check", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("model check = %d body = %s", status, body)
	}
	var check struct {
		Status string `json:"status"`
		Mode   string `json:"mode"`
	}
	if err := json.Unmarshal(body, &check); err != nil {
		t.Fatalf("decode model check: %v", err)
	}
	if check.Status != "fixture" || check.Mode != "fixture" {
		t.Fatalf("model check = %+v, want fixture mode without a provider call", check)
	}
}

func TestLivingWorldAuditCoverage(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "audit-owner")
	friend := env.register(t, "audit-friend")
	scene := owner.createObject(t, "living_scene", "Audit scene", map[string]any{
		"purpose": "project", "premise": "Audit every write.",
	})
	status, body, _ := owner.postAction(t, scene.ID, "add_element",
		map[string]any{"kind": "idea", "text": "One real element."}, scene.Version)
	if status != http.StatusOK {
		t.Fatalf("scene action status = %d body = %s", status, body)
	}
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+scene.ID+"/derive", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"title": "Audit child", "inheritance_fields": []string{"premise", "elements"},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("derive status = %d body = %s", status, body)
	}
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+scene.ID+"/discussions", callOptions{
		Body: map[string]any{"body": "Human note.", "client_message_id": uuid.NewString()},
	})
	if status != http.StatusCreated {
		t.Fatalf("discussion status = %d body = %s", status, body)
	}

	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/social/friend-requests", callOptions{
		Body: map[string]any{"handle": friend.handle},
	})
	if status != http.StatusCreated {
		t.Fatalf("friend request status = %d body = %s", status, body)
	}
	var request struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	status, body, _ = friend.call(t, http.MethodPost,
		"/api/v1/social/friend-requests/"+request.ID+"/accept", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("accept status = %d body = %s", status, body)
	}
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/social/messages", callOptions{
		Body: map[string]any{
			"to_user_id": friend.id.String(), "body": "Audit message.",
			"client_message_id": uuid.NewString(),
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("message status = %d body = %s", status, body)
	}

	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+scene.ID+"/publications",
		callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body:           map[string]any{"derive_allowed": true, "license": "cc_by_4.0"},
		})
	if status != http.StatusCreated {
		t.Fatalf("publication status = %d body = %s", status, body)
	}
	var publication struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &publication); err != nil {
		t.Fatalf("decode publication: %v", err)
	}
	status, body, _ = friend.call(t, http.MethodPost,
		"/api/v1/public/worlds/"+publication.ID+"/derive", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"title": "Audit public child", "inheritance_fields": []string{"premise"},
			},
		})
	if status != http.StatusCreated {
		t.Fatalf("public derive status = %d body = %s", status, body)
	}
	status, body, _ = friend.call(t, http.MethodPost,
		"/api/v1/public/worlds/"+publication.ID+"/report", callOptions{
			Body: map[string]any{"reason": "Audit report."},
		})
	if status != http.StatusNoContent {
		t.Fatalf("report status = %d body = %s", status, body)
	}
	status, body, _ = owner.call(t, http.MethodDelete,
		"/api/v1/publications/"+publication.ID, callOptions{})
	if status != http.StatusNoContent {
		t.Fatalf("revoke status = %d body = %s", status, body)
	}

	required := []string{
		"object.create", "object.action", "object.derive",
		"social.friend_request", "social.friend_accept", "social.message_send",
		"social.discussion_post", "publication.create", "publication.activate",
		"publication.derive", "publication.report", "publication.revoke",
	}
	for _, action := range required {
		if count := env.count(t, `SELECT count(*) FROM audit_events WHERE action = $1`, action); count < 1 {
			t.Errorf("audit action %s has no rows", action)
		}
	}
}

func TestDWWorldStageAndPulseUseRealState(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "stage")
	object := user.createObject(t, "living_scene", "Stage object", map[string]any{
		"purpose": "story", "premise": "A real premise.",
	})

	status, body, _ := user.call(t, http.MethodGet, "/api/v1/world/stage", callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), object.ID) {
		t.Fatalf("stage status = %d body = %s", status, body)
	}
	var stage struct {
		Objects []map[string]any `json:"objects"`
		Edges   []map[string]any `json:"edges"`
	}
	if err := json.Unmarshal(body, &stage); err != nil {
		t.Fatalf("decode stage: %v", err)
	}
	if len(stage.Objects) != 1 {
		t.Fatalf("stage objects = %d, want 1", len(stage.Objects))
	}
	if stage.Objects[0]["latest_event"] == nil {
		t.Fatal("stage object must carry its real latest event")
	}

	status, body, _ = user.call(t, http.MethodGet, "/api/v1/pulse", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("pulse status = %d body = %s", status, body)
	}
	var pulse struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &pulse); err != nil {
		t.Fatalf("decode pulse: %v", err)
	}
	if len(pulse.Items) == 0 {
		t.Fatal("pulse must contain the real object creation event")
	}
	if pulse.Items[0]["object_id"] != object.ID {
		t.Fatalf("pulse object = %v, want %s", pulse.Items[0]["object_id"], object.ID)
	}
}
