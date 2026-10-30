// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDWUserSearchIsQueryScopedAndPrivacyAware(t *testing.T) {
	env := newTestEnv(t)
	alice := env.register(t, "search-alice")
	bob := env.register(t, "search-bob")
	carol := env.register(t, "search-carol")

	// A handle prefix finds the account without exposing email or other fields.
	status, body, _ := alice.call(t, http.MethodGet,
		"/api/v1/social/user-search?q="+bob.handle[:8], callOptions{})
	if status != http.StatusOK {
		t.Fatalf("handle search status = %d body = %s", status, body)
	}
	if strings.Contains(strings.ToLower(string(body)), "email") {
		t.Fatalf("user search leaked an email field: %s", body)
	}
	var found struct {
		Items []struct {
			UserID      string `json:"user_id"`
			Handle      string `json:"handle"`
			DisplayName string `json:"display_name"`
			Relation    string `json:"relation"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &found); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(found.Items) != 1 || found.Items[0].UserID != bob.id.String() ||
		found.Items[0].Relation != "none" {
		t.Fatalf("handle search items = %+v", found.Items)
	}
	if found.Items[0].Handle != bob.handle {
		t.Fatalf("handle = %q, want %q", found.Items[0].Handle, bob.handle)
	}

	// Nickname search is case-insensitive and substring-scoped.
	status, body, _ = alice.call(t, http.MethodGet,
		"/api/v1/social/user-search?q=SEARCH-BOB", callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), bob.id.String()) {
		t.Fatalf("nickname search status = %d body = %s", status, body)
	}
	if strings.Contains(string(body), carol.id.String()) {
		t.Fatalf("nickname search returned an unrelated account: %s", body)
	}

	// There is no directory: one character is rejected.
	status, _, _ = alice.call(t, http.MethodGet,
		"/api/v1/social/user-search?q=a", callOptions{})
	if status != http.StatusBadRequest {
		t.Fatalf("short query status = %d, want 400", status)
	}

	// An account can opt out of discovery; an exact handle friend request still
	// works because it is an explicit action, not discovery.
	status, body, _ = bob.call(t, http.MethodPatch, "/api/v1/social/discoverability",
		callOptions{Body: map[string]any{"discoverable": false}})
	if status != http.StatusOK {
		t.Fatalf("disable discovery status = %d body = %s", status, body)
	}
	status, body, _ = alice.call(t, http.MethodGet,
		"/api/v1/social/user-search?q=search-bob", callOptions{})
	if status != http.StatusOK || strings.Contains(string(body), bob.id.String()) {
		t.Fatalf("opted-out account still discoverable: %d %s", status, body)
	}
	status, body, _ = bob.call(t, http.MethodGet,
		"/api/v1/social/discoverability", callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), "false") {
		t.Fatalf("discoverability read = %d %s", status, body)
	}
	if count := env.count(t, `SELECT count(*) FROM audit_events
		WHERE action = 'social.discoverability_update' AND actor_user_id = $1`,
		bob.id.String()); count != 1 {
		t.Fatalf("discoverability audit count = %d, want 1", count)
	}
	status, body, _ = bob.call(t, http.MethodPatch, "/api/v1/social/discoverability",
		callOptions{Body: map[string]any{"discoverable": true}})
	if status != http.StatusOK {
		t.Fatalf("re-enable discovery status = %d body = %s", status, body)
	}

	// Friendship relation is projected without changing the search contract.
	status, body, _ = alice.call(t, http.MethodPost, "/api/v1/social/friend-requests",
		callOptions{Body: map[string]any{"handle": bob.handle}})
	if status != http.StatusCreated {
		t.Fatalf("friend request status = %d body = %s", status, body)
	}
	var request struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	status, body, _ = alice.call(t, http.MethodGet,
		"/api/v1/social/user-search?q="+bob.handle[:8], callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), `"relation":"outgoing"`) {
		t.Fatalf("outgoing relation = %d %s", status, body)
	}
	status, body, _ = bob.call(t, http.MethodPost,
		"/api/v1/social/friend-requests/"+request.ID+"/accept", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("accept status = %d body = %s", status, body)
	}
	status, body, _ = alice.call(t, http.MethodGet,
		"/api/v1/social/user-search?q="+bob.handle[:8], callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), `"relation":"friend"`) {
		t.Fatalf("friend relation = %d %s", status, body)
	}

	// Blocking hides both directions from discovery.
	if err := bob.callBlock(t, alice.id.String()); err != nil {
		t.Fatalf("block: %v", err)
	}
	status, body, _ = alice.call(t, http.MethodGet,
		"/api/v1/social/user-search?q="+bob.handle[:8], callOptions{})
	if status != http.StatusOK || strings.Contains(string(body), bob.id.String()) {
		t.Fatalf("blocked account still discoverable: %d %s", status, body)
	}

	// Unauthenticated discovery is impossible.
	anonymous := newClient(t)
	status, _, _ = env.call(t, anonymous, http.MethodGet,
		"/api/v1/social/user-search?q=search", callOptions{Origin: &env.origin})
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous search status = %d, want 401", status)
	}
}

func TestDWWorldLayoutIsPrivateUserViewState(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "layout-owner")
	visitor := env.register(t, "layout-visitor")

	scene := owner.createObject(t, "living_scene", "Layout scene", map[string]any{
		"purpose": "project",
		"premise": "Arrange the world without changing it.",
		"elements": []map[string]any{
			{"kind": "idea", "text": "First idea."},
		},
	})

	status, body, _ := owner.call(t, http.MethodGet, "/api/v1/world/layout", callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), `"items":[]`) {
		t.Fatalf("initial layout = %d %s", status, body)
	}

	status, body, _ = owner.call(t, http.MethodPut, "/api/v1/world/layout", callOptions{
		Body: map[string]any{"object_id": scene.ID, "x": 412.5, "y": 338},
	})
	if status != http.StatusOK {
		t.Fatalf("save layout status = %d body = %s", status, body)
	}
	status, body, _ = owner.call(t, http.MethodGet, "/api/v1/world/layout", callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), `"object_id":"`+scene.ID+`"`) {
		t.Fatalf("saved layout = %d %s", status, body)
	}

	// A layout write must not change the object or its version.
	status, body, _ = owner.call(t, http.MethodGet, "/api/v1/objects/"+scene.ID, callOptions{})
	if status != http.StatusOK {
		t.Fatalf("object read = %d %s", status, body)
	}
	var object struct {
		Version int    `json:"version"`
		OwnerID string `json:"owner_id"`
	}
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatalf("decode object: %v", err)
	}
	if object.Version != scene.Version || object.OwnerID != owner.id.String() {
		t.Fatalf("layout changed object: version=%d owner=%s", object.Version, object.OwnerID)
	}

	// A non-member cannot write or observe another user's layout.
	status, _, _ = visitor.call(t, http.MethodPut, "/api/v1/world/layout", callOptions{
		Body: map[string]any{"object_id": scene.ID, "x": 1, "y": 2},
	})
	if status != http.StatusNotFound {
		t.Fatalf("non-member layout status = %d, want 404", status)
	}
	status, body, _ = visitor.call(t, http.MethodGet, "/api/v1/world/layout", callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), `"items":[]`) {
		t.Fatalf("visitor layout = %d %s", status, body)
	}

	// Membership permits the visitor's own private layout, still invisible to
	// the owner.
	objectID, err := uuid.Parse(scene.ID)
	if err != nil {
		t.Fatalf("parse scene id: %v", err)
	}
	env.addMembership(t, objectID, visitor.id, []string{"read"})
	status, body, _ = visitor.call(t, http.MethodPut, "/api/v1/world/layout", callOptions{
		Body: map[string]any{"object_id": scene.ID, "x": 900, "y": 120},
	})
	if status != http.StatusOK {
		t.Fatalf("member layout status = %d body = %s", status, body)
	}
	status, body, _ = owner.call(t, http.MethodGet, "/api/v1/world/layout", callOptions{})
	if status != http.StatusOK || strings.Contains(string(body), `"x":900`) {
		t.Fatalf("owner saw visitor layout: %d %s", status, body)
	}

	// Bounds and malformed input are rejected.
	status, _, _ = owner.call(t, http.MethodPut, "/api/v1/world/layout", callOptions{
		Body: map[string]any{"object_id": scene.ID, "x": 10001, "y": 0},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("out-of-bounds layout status = %d, want 400", status)
	}
	status, _, _ = owner.call(t, http.MethodPut, "/api/v1/world/layout", callOptions{
		Body: map[string]any{"object_id": "not-a-uuid", "x": 0, "y": 0},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("bad object id layout status = %d, want 400", status)
	}

	anonymous := newClient(t)
	status, _, _ = env.call(t, anonymous, http.MethodGet, "/api/v1/world/layout",
		callOptions{Origin: &env.origin})
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous layout status = %d, want 401", status)
	}
}

func TestDWPublicationImmediateConsentApproval(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "consent-owner")
	contributor := env.register(t, "consent-contributor")

	scene := owner.createObject(t, "living_scene", "Consent race", map[string]any{
		"purpose":  "project",
		"premise":  "Approve immediately after publication.",
		"elements": []map[string]any{{"kind": "idea", "text": "First idea."}},
	})
	objectID, err := uuid.Parse(scene.ID)
	if err != nil {
		t.Fatalf("parse scene id: %v", err)
	}
	env.addMembership(t, objectID, contributor.id, []string{"read", "contribute"})
	status, body, updated := contributor.postAction(t, scene.ID, "add_element",
		map[string]any{"kind": "idea", "text": "Contributor edit."}, scene.Version)
	if status != http.StatusOK {
		t.Fatalf("contributor edit status = %d body = %s", status, body)
	}

	status, body, _ = owner.call(t, http.MethodPost,
		"/api/v1/objects/"+scene.ID+"/publications", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"source_version": updated.Version,
				"derive_allowed": true,
				"license":        "all_rights_reserved",
			},
		})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("create publication status = %d body = %s", status, body)
	}
	var publication struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &publication); err != nil {
		t.Fatalf("decode publication: %v", err)
	}
	if publication.Status != "pending_consent" {
		t.Fatalf("publication status = %q, want pending_consent", publication.Status)
	}

	status, body, _ = contributor.call(t, http.MethodGet,
		"/api/v1/publication-consents", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("pending consents status = %d body = %s", status, body)
	}
	var consents struct {
		Items []struct {
			ID            string `json:"id"`
			PublicationID string `json:"publication_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &consents); err != nil {
		t.Fatalf("decode consents: %v", err)
	}
	var consentID string
	for _, consent := range consents.Items {
		if consent.PublicationID == publication.ID {
			consentID = consent.ID
		}
	}
	if consentID == "" {
		t.Fatalf("consent for publication %s not found: %s", publication.ID, body)
	}

	status, body, _ = contributor.call(t, http.MethodPost,
		"/api/v1/publication-consents/"+consentID+"/approve", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("approve consent status = %d body = %s", status, body)
	}

	status, body, _ = owner.call(t, http.MethodGet, "/api/v1/public/worlds", callOptions{})
	if status != http.StatusOK || !strings.Contains(string(body), publication.ID) {
		t.Fatalf("approved publication not public: %d %s", status, body)
	}
}

func TestDWWorldStageProjectsVisibleSceneSummary(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "stage-summary")
	stranger := env.register(t, "stage-stranger")

	scene := owner.createObject(t, "living_scene", "Visible scene", map[string]any{
		"purpose": "story",
		"premise": "A city wakes at sunset.",
		"elements": []map[string]any{
			{"kind": "scene", "text": "The first door opens."},
			{"kind": "choice", "text": "Enter or wait."},
		},
	})
	status, body, updated := owner.postAction(t, scene.ID, "update_element",
		map[string]any{"element_id": sceneElementID(t, scene), "status": "done"}, scene.Version)
	if status != http.StatusOK {
		t.Fatalf("complete element status = %d body = %s", status, body)
	}
	_ = updated

	status, body, _ = owner.call(t, http.MethodGet, "/api/v1/world/stage", callOptions{})
	if status != http.StatusOK {
		t.Fatalf("stage status = %d body = %s", status, body)
	}
	var stage struct {
		Objects []struct {
			ID         string `json:"id"`
			Note       string `json:"note"`
			Purpose    string `json:"purpose"`
			DoneCount  int    `json:"done_count"`
			TotalCount int    `json:"total_count"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(body, &stage); err != nil {
		t.Fatalf("decode stage: %v", err)
	}
	if len(stage.Objects) != 1 {
		t.Fatalf("stage objects = %+v", stage.Objects)
	}
	got := stage.Objects[0]
	if got.ID != scene.ID || got.Note != "A city wakes at sunset." || got.Purpose != "story" ||
		got.DoneCount != 1 || got.TotalCount != 2 {
		t.Fatalf("stage projection = %+v", got)
	}

	status, body, _ = stranger.call(t, http.MethodGet, "/api/v1/world/stage", callOptions{})
	if status != http.StatusOK || strings.Contains(string(body), scene.ID) {
		t.Fatalf("stranger stage leaked object: %d %s", status, body)
	}
}

func sceneElementID(t *testing.T, scene objectResponse) string {
	t.Helper()
	elements, ok := scene.State["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("scene has no elements: %+v", scene.State)
	}
	element, ok := elements[0].(map[string]any)
	if !ok {
		t.Fatalf("element shape: %+v", elements[0])
	}
	id, ok := element["id"].(string)
	if !ok || id == "" {
		t.Fatalf("element id: %+v", element)
	}
	return id
}

// callBlock blocks a user and fails the subtest on any non-success response.
func (u *testUser) callBlock(t *testing.T, targetID string) error {
	t.Helper()
	status, body, _ := u.call(t, http.MethodPost, "/api/v1/social/blocks",
		callOptions{Body: map[string]any{"blocked_user_id": targetID}})
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("block status %d body %s", status, body)
	}
	return nil
}
