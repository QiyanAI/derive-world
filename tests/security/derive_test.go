// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/QiyanAI/derive-world/internal/httpx"
)

func deriveCall(t *testing.T, user *testUser, objectID string, body map[string]any, idemKey string) (int, []byte, objectResponse, string) {
	t.Helper()
	status, response, headers := user.call(t, http.MethodPost, "/api/v1/objects/"+objectID+"/derive", callOptions{
		Body: body, IdempotencyKey: idemKey,
	})
	var object objectResponse
	if status == http.StatusCreated {
		if err := json.Unmarshal(response, &object); err != nil {
			t.Fatalf("decode derived object: %v", err)
		}
	}
	return status, response, object, headers.Get("Idempotency-Replayed")
}

func TestDeriveIndependentChildAndHistoricalSnapshot(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "derive")
	parent := user.createObject(t, "collaborative", "Parent", map[string]any{
		"summary":   "parent summary",
		"body":      "old body",
		"checklist": []map[string]any{{"text": "item one"}},
		"tags":      []string{"origin"},
	})

	// Advance to version 2 so a historical source version is distinguishable.
	status, body, parentV2 := user.postAction(t, parent.ID, "set_body", map[string]any{"body": "new body"}, 1)
	requireStatus(t, status, http.StatusOK, body)
	if parentV2.Version != 2 {
		t.Fatalf("parent version = %d", parentV2.Version)
	}

	snapshotBefore := struct {
		Version   int
		State     string
		UpdatedAt time.Time
	}{}
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT version, state::text, updated_at FROM living_objects WHERE id=$1`, parent.ID).
		Scan(&snapshotBefore.Version, &snapshotBefore.State, &snapshotBefore.UpdatedAt); err != nil {
		t.Fatalf("snapshot parent: %v", err)
	}
	parentEventsBefore := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1`, parent.ID)

	key := newIdempotencyKey()
	status, body, child, replayed := deriveCall(t, user, parent.ID, map[string]any{
		"title":              "Historical branch",
		"source_version":     1,
		"inheritance_fields": []string{"summary", "body", "checklist", "tags"},
	}, key)
	requireStatus(t, status, http.StatusCreated, body)
	if replayed != "" {
		t.Fatal("first derive must not be marked replayed")
	}
	if child.Version != 1 || child.OwnerID != user.id.String() || child.ID == parent.ID {
		t.Fatalf("child object = %+v", child)
	}
	if jsonField[string](t, child.State, "title") != "Historical branch" {
		t.Fatalf("child title = %v", child.State["title"])
	}
	if jsonField[string](t, child.State, "body") != "old body" {
		t.Fatalf("historical derive did not use version 1 body: %v", child.State["body"])
	}
	if len(child.SharedContext) != 0 {
		t.Fatalf("child inherited shared context: %v", child.SharedContext)
	}
	if child.Lineage == nil || child.Lineage.Parent.ObjectID != parent.ID || child.Lineage.Parent.Version != 1 {
		t.Fatalf("child lineage = %+v", child.Lineage)
	}

	parentChecklist := jsonField[[]map[string]any](t, parentV2.State, "checklist")
	childChecklist := jsonField[[]map[string]any](t, child.State, "checklist")
	if len(parentChecklist) != 1 || len(childChecklist) != 1 {
		t.Fatalf("checklist parent=%v child=%v", parentChecklist, childChecklist)
	}
	if parentChecklist[0]["id"] == childChecklist[0]["id"] {
		t.Fatal("child reused the parent checklist item id")
	}

	if got := env.count(t, `SELECT count(*) FROM derivation_edges WHERE child_object_id=$1 AND parent_object_id=$2 AND parent_version=1`, child.ID, parent.ID); got != 1 {
		t.Fatalf("derivation edge = %d, want 1", got)
	}
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1 AND event_type='object.derived' AND object_version=1`, child.ID); got != 1 {
		t.Fatal("child object.derived event missing")
	}
	if got := env.count(t, `SELECT count(*) FROM object_memberships WHERE object_id=$1 AND revoked_at IS NULL`, child.ID); got != 1 {
		t.Fatalf("child memberships = %d, want 1", got)
	}
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='object.derive' AND resource_id=$1 AND decision='allow'`, child.ID); got != 1 {
		t.Fatal("object.derive audit missing")
	}

	// Parent is byte-for-byte unchanged and has no new events.
	snapshotAfter := struct {
		Version   int
		State     string
		UpdatedAt time.Time
	}{}
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT version, state::text, updated_at FROM living_objects WHERE id=$1`, parent.ID).
		Scan(&snapshotAfter.Version, &snapshotAfter.State, &snapshotAfter.UpdatedAt); err != nil {
		t.Fatalf("snapshot parent after: %v", err)
	}
	if snapshotBefore.Version != snapshotAfter.Version ||
		snapshotBefore.State != snapshotAfter.State ||
		!snapshotBefore.UpdatedAt.Equal(snapshotAfter.UpdatedAt) {
		t.Fatalf("parent changed during derive: before=%+v after=%+v", snapshotBefore, snapshotAfter)
	}
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1`, parent.ID); got != parentEventsBefore {
		t.Fatal("derive wrote a parent event")
	}

	// Idempotent replay returns the same child and does not create a second edge.
	status, body, replayChild, replayed := deriveCall(t, user, parent.ID, map[string]any{
		"title":              "Historical branch",
		"source_version":     1,
		"inheritance_fields": []string{"summary", "body", "checklist", "tags"},
	}, key)
	requireStatus(t, status, http.StatusCreated, body)
	if replayed != "true" {
		t.Fatal("derive replay is missing Idempotency-Replayed")
	}
	if replayChild.ID != child.ID {
		t.Fatalf("derive replay child = %s, want %s", replayChild.ID, child.ID)
	}
	if got := env.count(t, `SELECT count(*) FROM derivation_edges WHERE parent_object_id=$1`, parent.ID); got != 1 {
		t.Fatalf("edges after replay = %d, want 1", got)
	}

	// Parent and child keep independent state and history after derivation.
	status, body, parentAfter := user.postAction(t, parent.ID, "set_summary",
		map[string]any{"summary": "parent moved on"}, 2)
	requireStatus(t, status, http.StatusOK, body)
	if parentAfter.Version != 3 {
		t.Fatalf("parent version after independence edit = %d", parentAfter.Version)
	}
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+child.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	childReloaded := decodeObject(t, body)
	if childReloaded.Version != 1 || jsonField[string](t, childReloaded.State, "body") != "old body" {
		t.Fatalf("parent edit changed the child: %+v", childReloaded)
	}
	if childReloaded.Lineage == nil || childReloaded.Lineage.Parent.ObjectID != parent.ID ||
		childReloaded.Lineage.Parent.Version != 1 {
		t.Fatalf("GET did not project lineage: %+v", childReloaded.Lineage)
	}
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1`, child.ID); got != 1 {
		t.Fatalf("child events after parent edit = %d, want 1", got)
	}

	status, body, childAfter := user.postAction(t, child.ID, "set_title",
		map[string]any{"title": "Child moved on"}, 1)
	requireStatus(t, status, http.StatusOK, body)
	if childAfter.Version != 2 {
		t.Fatalf("child version after edit = %d", childAfter.Version)
	}
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+parent.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	parentReloaded := decodeObject(t, body)
	if parentReloaded.Version != 3 || jsonField[string](t, parentReloaded.State, "title") != "Parent" {
		t.Fatalf("child edit changed the parent: %+v", parentReloaded)
	}

	// Current-version derive uses the latest body.
	status, body, currentChild, _ := deriveCall(t, user, parent.ID, map[string]any{
		"title":              "Current branch",
		"inheritance_fields": []string{"body"},
	}, newIdempotencyKey())
	requireStatus(t, status, http.StatusCreated, body)
	if jsonField[string](t, currentChild.State, "body") != "new body" {
		t.Fatalf("current derive body = %v", currentChild.State["body"])
	}
	if jsonField[string](t, currentChild.State, "summary") != "" {
		t.Fatal("unselected summary was inherited")
	}
	if currentChild.Lineage == nil || currentChild.Lineage.Parent.Version != 3 {
		t.Fatalf("current child lineage = %+v", currentChild.Lineage)
	}

	// Lineage reads the child's ancestor and the parent's descendant.
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+child.ID+"/lineage", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var childLineage struct {
		Ancestors []map[string]any `json:"ancestors"`
	}
	if err := json.Unmarshal(body, &childLineage); err != nil {
		t.Fatalf("decode child lineage: %v", err)
	}
	if len(childLineage.Ancestors) != 1 || childLineage.Ancestors[0]["object_id"] != parent.ID {
		t.Fatalf("child ancestors = %v", childLineage.Ancestors)
	}
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+parent.ID+"/lineage", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var parentLineage struct {
		Descendants []map[string]any `json:"descendants"`
	}
	if err := json.Unmarshal(body, &parentLineage); err != nil {
		t.Fatalf("decode parent lineage: %v", err)
	}
	if len(parentLineage.Descendants) != 2 {
		t.Fatalf("parent descendants = %d, want 2", len(parentLineage.Descendants))
	}
}

func TestDeriveValidationAndAuthorization(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "deriveowner")
	nonMember := env.register(t, "derivenonmember")
	viewer := env.register(t, "deriveviewer")
	parent := owner.createObject(t, "collaborative", "Validation parent", map[string]any{
		"summary": "s", "body": "b", "checklist": []map[string]any{}, "tags": []string{},
	})
	env.addMembership(t, mustUUID(t, parent.ID), viewer.id, []string{"read"})

	cases := []struct {
		name string
		body map[string]any
		code int
	}{
		{"title field", map[string]any{"title": "x", "inheritance_fields": []string{"title"}}, http.StatusBadRequest},
		{"unknown field", map[string]any{"title": "x", "inheritance_fields": []string{"votes"}}, http.StatusBadRequest},
		{"empty fields", map[string]any{"title": "x", "inheritance_fields": []string{}}, http.StatusBadRequest},
		{"duplicate fields", map[string]any{"title": "x", "inheritance_fields": []string{"summary", "summary"}}, http.StatusBadRequest},
		{"source zero", map[string]any{"title": "x", "source_version": 0, "inheritance_fields": []string{"summary"}}, http.StatusBadRequest},
		{"source future", map[string]any{"title": "x", "source_version": 2, "inheritance_fields": []string{"summary"}}, http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			status, body, _, _ := deriveCall(t, owner, parent.ID, testCase.body, newIdempotencyKey())
			requireStatus(t, status, testCase.code, body)
			requireCode(t, body, string(httpx.CodeInvalidRequest))
		})
	}

	status, body, _, _ := deriveCall(t, nonMember, parent.ID, map[string]any{
		"title": "x", "inheritance_fields": []string{"summary"},
	}, newIdempotencyKey())
	requireStatus(t, status, http.StatusNotFound, body)

	status, body, _, _ = deriveCall(t, viewer, parent.ID, map[string]any{
		"title": "x", "inheritance_fields": []string{"summary"},
	}, newIdempotencyKey())
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, string(httpx.CodeForbidden))

	// Interactive derivation must inherit options.
	poll := owner.createObject(t, "interactive", "Poll parent", map[string]any{
		"prompt": "prompt", "options": []map[string]any{{"label": "A"}, {"label": "B"}},
	})
	status, body, _, _ = deriveCall(t, owner, poll.ID, map[string]any{
		"title": "No options", "inheritance_fields": []string{"prompt"},
	}, newIdempotencyKey())
	requireStatus(t, status, http.StatusBadRequest, body)

	status, body, child, _ := deriveCall(t, owner, poll.ID, map[string]any{
		"title": "Poll branch", "inheritance_fields": []string{"prompt", "options"},
	}, newIdempotencyKey())
	requireStatus(t, status, http.StatusCreated, body)
	if jsonField[string](t, child.State, "status") != "open" {
		t.Fatal("derived poll must start open")
	}
	parentOptions := jsonField[[]map[string]any](t, poll.State, "options")
	childOptions := jsonField[[]map[string]any](t, child.State, "options")
	if len(childOptions) != len(parentOptions) {
		t.Fatalf("child options = %v", childOptions)
	}
	seen := map[string]bool{}
	for index := range parentOptions {
		if parentOptions[index]["id"] == childOptions[index]["id"] {
			t.Fatal("derived poll reused an option id")
		}
		seen[childOptions[index]["id"].(string)] = true
	}
	votes := jsonField[map[string]any](t, child.State, "votes")
	if len(votes) != len(childOptions) {
		t.Fatalf("child votes = %v", votes)
	}
	for optionID := range votes {
		if !seen[optionID] {
			t.Fatalf("vote key %s is not a child option", optionID)
		}
		if votes[optionID].(float64) != 0 {
			t.Fatalf("child vote %s = %v, want 0", optionID, votes[optionID])
		}
	}
}

func TestLineagePrivacyClippingAndDepth(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "lineageowner")
	sharer := env.register(t, "lineagesharer")
	observer := env.register(t, "lineageobserver")

	parent := owner.createObject(t, "collaborative", "Root", map[string]any{
		"summary": "root", "body": "", "checklist": []map[string]any{}, "tags": []string{},
	})
	env.addMembership(t, mustUUID(t, parent.ID), sharer.id, []string{"read", "derive"})
	status, body, child, _ := deriveCall(t, sharer, parent.ID, map[string]any{
		"title": "Shared child", "inheritance_fields": []string{"summary"},
	}, newIdempotencyKey())
	requireStatus(t, status, http.StatusCreated, body)

	// Observer can read only the child: the ancestor must be redacted.
	env.addMembership(t, mustUUID(t, child.ID), observer.id, []string{"read"})
	status, body, _ = observer.call(t, http.MethodGet, "/api/v1/objects/"+child.ID+"/lineage", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var redactedLineage struct {
		Ancestors []map[string]any `json:"ancestors"`
	}
	if err := json.Unmarshal(body, &redactedLineage); err != nil {
		t.Fatalf("decode redacted lineage: %v", err)
	}
	if len(redactedLineage.Ancestors) != 1 {
		t.Fatalf("ancestors = %v", redactedLineage.Ancestors)
	}
	ancestor := redactedLineage.Ancestors[0]
	if ancestor["redacted"] != true {
		t.Fatalf("ancestor was not redacted: %v", ancestor)
	}
	for _, key := range []string{"object_id", "title", "version"} {
		if _, present := ancestor[key]; present {
			t.Fatalf("redacted ancestor leaked %s: %v", key, ancestor)
		}
	}

	// Observer can read only the parent: the unreadable child subtree is
	// omitted entirely, not represented as a redacted placeholder.
	env.revokeMembership(t, mustUUID(t, child.ID), observer.id)
	env.addMembership(t, mustUUID(t, parent.ID), observer.id, []string{"read"})
	status, body, _ = observer.call(t, http.MethodGet, "/api/v1/objects/"+parent.ID+"/lineage", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if !json.Valid(body) {
		t.Fatalf("invalid lineage JSON: %s", body)
	}
	var clipped struct {
		Descendants []map[string]any `json:"descendants"`
	}
	if err := json.Unmarshal(body, &clipped); err != nil {
		t.Fatalf("decode clipped lineage: %v", err)
	}
	if len(clipped.Descendants) != 0 {
		t.Fatalf("unreadable descendant leaked: %v", clipped.Descendants)
	}
	if string(body) == "" {
		t.Fatal("empty lineage response")
	}

	// Eleven levels under a fresh root: depth 10 reports truncated and omits
	// level 11.
	depthRoot := owner.createObject(t, "collaborative", "Depth root", map[string]any{
		"summary": "root", "body": "", "checklist": []map[string]any{}, "tags": []string{},
	})
	current := depthRoot
	for index := 0; index < 11; index++ {
		status, response, created, _ := deriveCall(t, owner, current.ID, map[string]any{
			"title": "depth", "inheritance_fields": []string{"summary"},
		}, newIdempotencyKey())
		requireStatus(t, status, http.StatusCreated, response)
		current = created
	}
	status, body, _ = owner.call(t, http.MethodGet, "/api/v1/objects/"+depthRoot.ID+"/lineage?depth=10", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var deep struct {
		Descendants []map[string]any `json:"descendants"`
		Truncated   bool             `json:"truncated"`
		MaxDepth    int              `json:"max_depth"`
	}
	if err := json.Unmarshal(body, &deep); err != nil {
		t.Fatalf("decode deep lineage: %v", err)
	}
	if !deep.Truncated || deep.MaxDepth != 10 {
		t.Fatalf("deep lineage truncated=%v max_depth=%d", deep.Truncated, deep.MaxDepth)
	}
	if len(deep.Descendants) != 1 {
		t.Fatalf("root descendants = %d, want 1 root child", len(deep.Descendants))
	}
}

func TestDeriveDoesNotInheritMembershipOrPermissions(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "deriveparentowner")
	member := env.register(t, "deriveparentmember")
	parent := owner.createObject(t, "collaborative", "Membership parent", map[string]any{
		"summary": "parent", "body": "", "checklist": []map[string]any{}, "tags": []string{},
	})
	parentID := mustUUID(t, parent.ID)
	env.addMembership(t, parentID, member.id, []string{"read", "derive"})

	status, body, child, _ := deriveCall(t, member, parent.ID, map[string]any{
		"title": "Member child", "inheritance_fields": []string{"summary"},
	}, newIdempotencyKey())
	requireStatus(t, status, http.StatusCreated, body)
	if child.OwnerID != member.id.String() {
		t.Fatalf("child owner = %s, want %s", child.OwnerID, member.id)
	}
	if got := env.count(t, `SELECT count(*) FROM object_memberships WHERE object_id=$1 AND revoked_at IS NULL`, child.ID); got != 1 {
		t.Fatalf("child memberships = %d, want 1", got)
	}
	if got := env.count(t, `SELECT count(*) FROM object_memberships WHERE object_id=$1 AND user_id=$2 AND permissions @> ARRAY['read','contribute','derive','invite','manage']::text[]`, child.ID, member.id.String()); got != 1 {
		t.Fatalf("child owner membership missing full owner preset: %d", got)
	}
	if got := env.count(t, `SELECT count(*) FROM object_memberships WHERE object_id=$1 AND user_id=$2`, child.ID, owner.id.String()); got != 0 {
		t.Fatal("parent owner inherited a child membership")
	}
	if got := env.count(t, `SELECT count(*) FROM object_memberships WHERE object_id=$1 AND revoked_at IS NULL`, parent.ID); got != 2 {
		t.Fatalf("parent memberships changed to %d", got)
	}
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1 AND event_type='object.derived' AND actor_user_id=$2`, child.ID, member.id.String()); got != 1 {
		t.Fatal("child derived event actor is not the deriving member")
	}

	// The parent owner is not implicitly authorized on the child.
	status, body, _ = owner.call(t, http.MethodGet, "/api/v1/objects/"+child.ID, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)

	// Losing read on the parent hides the source metadata from the child view
	// but does not remove the child's own access.
	status, body, _ = member.call(t, http.MethodGet, "/api/v1/objects/"+child.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if decodeObject(t, body).Lineage == nil {
		t.Fatal("readable parent lineage is missing before revocation")
	}
	env.revokeMembership(t, parentID, member.id)
	status, body, _ = member.call(t, http.MethodGet, "/api/v1/objects/"+child.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if decodeObject(t, body).Lineage != nil {
		t.Fatal("unreadable parent lineage was leaked on the child view")
	}
}
