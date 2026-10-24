// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/objects"
)

func TestObjectCreateReadAndCrossUserIsolation(t *testing.T) {
	env := newTestEnv(t)
	userA := env.register(t, "objowner")
	userB := env.register(t, "objother")

	object := userA.createObject(t, "collaborative", "Shared launch plan", map[string]any{
		"summary":   "two users, two AIs",
		"body":      "first line",
		"checklist": []map[string]any{{"text": "A creates", "done": true}},
		"tags":      []string{"launch", "derive", "launch"},
	})
	if object.Version != 1 || object.OwnerID != userA.id.String() || object.CreatorID != userA.id.String() {
		t.Fatalf("created object = %+v", object)
	}
	if object.Status != "active" || object.SchemaVersion != 1 || object.PresentationTemplate != "collaborative_v1" {
		t.Fatalf("created object metadata = %+v", object)
	}
	if len(object.Permissions) != 5 || object.Role != "owner" {
		t.Fatalf("owner permissions = %v role=%s", object.Permissions, object.Role)
	}
	if got := jsonField[string](t, object.State, "title"); got != "Shared launch plan" {
		t.Fatalf("state title = %q", got)
	}
	tags := jsonField[[]string](t, object.State, "tags")
	if len(tags) != 2 || tags[0] != "derive" || tags[1] != "launch" {
		t.Fatalf("tags were not deduplicated and sorted: %v", tags)
	}
	checklist := jsonField[[]map[string]any](t, object.State, "checklist")
	if len(checklist) != 1 || checklist[0]["id"] == nil {
		t.Fatalf("checklist = %v", checklist)
	}

	objectID := mustUUID(t, object.ID)
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE id=$1 AND version=1 AND owner_id=$2`, object.ID, userA.id.String()); got != 1 {
		t.Fatal("living_objects row missing or wrong")
	}
	if got := env.count(t, `SELECT count(*) FROM object_memberships WHERE object_id=$1 AND user_id=$2 AND revoked_at IS NULL AND permissions @> ARRAY['read','contribute','derive','invite','manage']::text[]`, object.ID, userA.id.String()); got != 1 {
		t.Fatal("owner membership is missing the full owner preset")
	}
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1 AND event_type='object.created' AND object_version=1`, object.ID); got != 1 {
		t.Fatal("object.created event missing")
	}
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE payload ?| ARRAY['private_context','ai_messages','token','password','api_key']`); got != 0 {
		t.Fatalf("object events contain sensitive keys: %d", got)
	}
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE metadata ?| ARRAY['private_context','ai_messages','token','password','api_key']`); got != 0 {
		t.Fatalf("audit metadata contains sensitive keys: %d", got)
	}
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='object.create' AND resource_id=$1 AND decision='allow'`, object.ID); got != 1 {
		t.Fatal("object.create audit missing")
	}

	status, body, _ := userA.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if decodeObject(t, body).ID != object.ID {
		t.Fatal("owner read returned a different object")
	}

	for _, path := range []string{
		"/api/v1/objects/" + object.ID,
		"/api/v1/objects/" + object.ID + "/events",
		"/api/v1/objects/" + object.ID + "/members",
		"/api/v1/objects/" + object.ID + "/lineage",
	} {
		status, body, _ = userB.call(t, http.MethodGet, path, callOptions{})
		requireStatus(t, status, http.StatusNotFound, body)
		requireCode(t, body, string(httpx.CodeNotFound))
		if strings.Contains(string(body), "Shared launch plan") || strings.Contains(string(body), `"version"`) {
			t.Fatalf("404 leaked object metadata: %s", body)
		}
	}

	status, body, _ = userB.call(t, http.MethodGet, "/api/v1/objects?scope=all", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("user B sees %d objects, want 0", len(list.Items))
	}

	status, body, _ = userA.call(t, http.MethodGet, "/api/v1/objects?scope=owned", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode owned list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0]["id"] != object.ID {
		t.Fatalf("owner list = %v", list.Items)
	}

	// No read means the sensitive-resource 404 is audited without the id.
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='authz.not_found' AND decision='deny'`); got < 4 {
		t.Fatalf("authz.not_found rows = %d, want at least 4", got)
	}
	_ = objectID
}

func TestTemplatePairingConstraint(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "templatepair")
	objectID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	_, insertErr := env.store.Pool().Exec(t.Context(), `
INSERT INTO living_objects (
    id, object_type, creator_id, owner_id, state, presentation_template
) VALUES ($1,'collaborative',$2,$2,'{}'::jsonb,'interactive_v1')`,
		objectID.String(), user.id.String())
	if insertErr == nil {
		t.Fatal("cross-paired object_type/presentation_template was accepted")
	}
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE id=$1`, objectID.String()); got != 0 {
		t.Fatal("cross-paired insert left a row")
	}
}

func TestCreateIdempotency(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "idemcreate")
	body := map[string]any{
		"object_type": "collaborative", "title": "Idempotent object",
		"content": map[string]any{"summary": "once"}, "shared_context": map[string]any{},
	}
	key := newIdempotencyKey()

	status, first, headers := user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: body, IdempotencyKey: key,
	})
	requireStatus(t, status, http.StatusCreated, first)
	if headers.Get("Idempotency-Replayed") != "" {
		t.Fatal("first response must not be marked replayed")
	}
	firstObject := decodeObject(t, first)

	status, second, headers := user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: body, IdempotencyKey: key,
	})
	requireStatus(t, status, http.StatusCreated, second)
	if headers.Get("Idempotency-Replayed") != "true" {
		t.Fatal("replay response is missing Idempotency-Replayed")
	}
	secondObject := decodeObject(t, second)
	if firstObject.ID != secondObject.ID {
		t.Fatalf("replay returned a different object: %s vs %s", firstObject.ID, secondObject.ID)
	}
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE state->>'title'='Idempotent object' AND owner_id=$1`, user.id.String()); got != 1 {
		t.Fatalf("objects with title = %d, want 1", got)
	}
	if got := env.count(t, `SELECT count(*) FROM derivation_edges WHERE child_object_id=$1`, firstObject.ID); got != 0 {
		t.Fatalf("unexpected derivation edges = %d", got)
	}

	// Same key with a different body is a conflict, not a second object.
	changed := map[string]any{
		"object_type": "collaborative", "title": "Changed body",
		"content": map[string]any{}, "shared_context": map[string]any{},
	}
	status, conflictBody, _ := user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		Body: changed, IdempotencyKey: key,
	})
	requireStatus(t, status, http.StatusConflict, conflictBody)
	requireCode(t, conflictBody, string(httpx.CodeIdempotencyConflict))

	// Missing key is a 400.
	status, missing, _ := user.call(t, http.MethodPost, "/api/v1/objects", callOptions{Body: body})
	requireStatus(t, status, http.StatusBadRequest, missing)
	requireCode(t, missing, string(httpx.CodeInvalidRequest))

	// Concurrent identical requests create exactly one object.
	concurrentBody := map[string]any{
		"object_type": "collaborative", "title": "Concurrent idempotent",
		"content": map[string]any{}, "shared_context": map[string]any{},
	}
	concurrentKey := newIdempotencyKey()
	statuses := make([]int, 2)
	bodies := make([][]byte, 2)
	var wg sync.WaitGroup
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			statuses[index], bodies[index], _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
				Body: concurrentBody, IdempotencyKey: concurrentKey,
			})
		}(index)
	}
	wg.Wait()
	for index, status := range statuses {
		if status != http.StatusCreated && status != http.StatusConflict {
			t.Fatalf("concurrent status[%d] = %d body=%s", index, status, bodies[index])
		}
	}
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE state->>'title'='Concurrent idempotent' AND owner_id=$1`, user.id.String()); got != 1 {
		t.Fatalf("concurrent objects = %d, want 1", got)
	}
}

func TestActionsVersioningAndSoftDelete(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "actions")
	object := user.createObject(t, "collaborative", "Versioned", map[string]any{
		"summary": "", "body": "", "checklist": []map[string]any{}, "tags": []string{},
	})

	status, body, updated := user.postAction(t, object.ID, "set_title", map[string]any{"title": "Version two"}, 1)
	requireStatus(t, status, http.StatusOK, body)
	if updated.Version != 2 || jsonField[string](t, updated.State, "title") != "Version two" {
		t.Fatalf("updated object = %+v", updated)
	}
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1 AND event_type='object.state_changed' AND object_version=2`, object.ID); got != 1 {
		t.Fatal("state_changed event for version 2 missing")
	}
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='object.action' AND resource_id=$1 AND metadata->>'action'='set_title'`, object.ID); got != 1 {
		t.Fatal("object.action audit missing")
	}

	// A stale expected_version must not write state or an event.
	beforeEvents := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1`, object.ID)
	beforeDenials := env.count(t, `SELECT count(*) FROM audit_events WHERE action='authz.denied' AND decision='deny' AND reason_code='version_conflict' AND resource_id=$1`, object.ID)
	status, body, _ = user.postAction(t, object.ID, "set_title", map[string]any{"title": "Stale"}, 1)
	requireStatus(t, status, http.StatusConflict, body)
	requireCode(t, body, string(httpx.CodeVersionConflict))
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1`, object.ID); got != beforeEvents {
		t.Fatal("version conflict wrote an event")
	}
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='authz.denied' AND decision='deny' AND reason_code='version_conflict' AND resource_id=$1`, object.ID); got != beforeDenials+1 {
		t.Fatalf("version conflict deny audits = %d, want %d", got, beforeDenials+1)
	}
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if got := jsonField[string](t, decodeObject(t, body).State, "title"); got != "Version two" {
		t.Fatalf("stale write changed state to %q", got)
	}

	// Missing or incomplete input is a validation error, never a zero-value write.
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body:           map[string]any{"action": "set_body", "expected_version": 2},
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, string(httpx.CodeInvalidRequest))
	status, body, _ = user.postAction(t, object.ID, "set_shared_context", map[string]any{}, 2)
	requireStatus(t, status, http.StatusBadRequest, body)
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action": "set_title", "input": map[string]any{"title": "x"}, "expected_version": 2, "extra": true,
		},
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if got := jsonField[string](t, decodeObject(t, body).State, "title"); got != "Version two" {
		t.Fatalf("missing input changed title to %q", got)
	}

	// Unknown action and unknown input keys are rejected.
	status, body, _ = user.postAction(t, object.ID, "drop_table", map[string]any{}, 2)
	requireStatus(t, status, http.StatusBadRequest, body)
	status, body, _ = user.postAction(t, object.ID, "set_title", map[string]any{"title": "x", "owner_id": "x"}, 2)
	requireStatus(t, status, http.StatusBadRequest, body)

	// set_shared_context advances the version but not state.
	status, body, updated = user.postAction(t, object.ID, "set_shared_context",
		map[string]any{"shared_context": map[string]any{"note": "explicitly shared"}}, 2)
	requireStatus(t, status, http.StatusOK, body)
	if updated.Version != 3 || jsonField[string](t, updated.State, "title") != "Version two" {
		t.Fatalf("shared context update = %+v", updated)
	}
	if updated.SharedContext["note"] != "explicitly shared" {
		t.Fatalf("shared context = %v", updated.SharedContext)
	}
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1 AND payload->>'shared_context_changed'='true'`, object.ID); got != 1 {
		t.Fatal("shared context event marker missing")
	}

	// Concurrent actions at the same expected version: exactly one wins.
	statuses := make([]int, 2)
	bodies := make([][]byte, 2)
	var wg sync.WaitGroup
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			statuses[index], bodies[index], _ = user.postAction(t, object.ID, "set_summary",
				map[string]any{"summary": "concurrent"}, 3)
		}(index)
	}
	wg.Wait()
	successes := 0
	for index, status := range statuses {
		switch status {
		case http.StatusOK:
			successes++
		case http.StatusConflict:
		default:
			t.Fatalf("concurrent status[%d] = %d body=%s", index, status, bodies[index])
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent successes = %d, want 1", successes)
	}
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	finalObject := decodeObject(t, body)
	if finalObject.Version != 4 {
		t.Fatalf("final version = %d, want 4", finalObject.Version)
	}

	// Soft delete keeps the version and the events.
	status, body, _ = user.call(t, http.MethodDelete, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusNoContent, body)
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE id=$1 AND status='deleted' AND version=4`, object.ID); got != 1 {
		t.Fatal("soft delete did not preserve version")
	}
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1 AND event_type='object.deleted'`, object.ID); got != 1 {
		t.Fatal("object.deleted event missing")
	}
	status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
	for _, path := range []string{"/events", "/members", "/lineage"} {
		status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+object.ID+path, callOptions{})
		requireStatus(t, status, http.StatusNotFound, body)
		if strings.Contains(string(body), "object.state_changed") {
			t.Fatalf("soft-deleted history leaked through %s: %s", path, body)
		}
	}
	status, body, _ = user.call(t, http.MethodDelete, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
}

func TestObjectAuthorizationRoles(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "roleowner")
	member := env.register(t, "rolemember")
	object := owner.createObject(t, "collaborative", "Role test", map[string]any{
		"summary": "", "body": "", "checklist": []map[string]any{}, "tags": []string{},
	})
	objectID := mustUUID(t, object.ID)

	env.addMembership(t, objectID, member.id, []string{"read"})

	status, body, _ := member.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	status, body, _ = member.call(t, http.MethodGet, "/api/v1/objects/"+object.ID+"/events", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	status, body, _ = member.call(t, http.MethodGet, "/api/v1/objects/"+object.ID+"/lineage", callOptions{})
	requireStatus(t, status, http.StatusOK, body)

	status, body, _ = member.call(t, http.MethodGet, "/api/v1/objects/"+object.ID+"/members", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var memberList struct {
		Items []objects.Member `json:"items"`
	}
	if err := json.Unmarshal(body, &memberList); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(memberList.Items) != 2 {
		t.Fatalf("members = %d, want 2", len(memberList.Items))
	}
	for _, entry := range memberList.Items {
		if strings.Contains(entry.DisplayName, "@") {
			t.Fatalf("member projection leaked an email: %q", entry.DisplayName)
		}
	}

	// Shared scope contains the object for the member and not for the owner;
	// owned scope is the mirror image.
	status, body, _ = member.call(t, http.MethodGet, "/api/v1/objects?scope=shared", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if !strings.Contains(string(body), object.ID) {
		t.Fatalf("shared scope omitted the object: %s", body)
	}
	status, body, _ = member.call(t, http.MethodGet, "/api/v1/objects?scope=owned", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), object.ID) {
		t.Fatalf("owned scope includes a shared object: %s", body)
	}
	status, body, _ = owner.call(t, http.MethodGet, "/api/v1/objects?scope=shared", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), object.ID) {
		t.Fatalf("owner shared scope includes an owned object: %s", body)
	}

	status, body, _ = member.postAction(t, object.ID, "set_title", map[string]any{"title": "nope"}, 1)
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, string(httpx.CodeForbidden))
	status, body, _ = member.postAction(t, object.ID, "set_title", map[string]any{"title": ""}, 1)
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, string(httpx.CodeForbidden))

	status, body, _ = member.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/derive", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body:           map[string]any{"title": "nope", "inheritance_fields": []string{"summary"}},
	})
	requireStatus(t, status, http.StatusForbidden, body)

	status, body, _ = member.call(t, http.MethodDelete, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusForbidden, body)

	// Revocation is immediate: the next read is indistinguishable from absent.
	env.revokeMembership(t, objectID, member.id)
	status, body, _ = member.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
	status, body, _ = member.call(t, http.MethodGet, "/api/v1/objects/"+object.ID+"/events", callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)

	// Editor can contribute and derive but cannot delete.
	env.addMembership(t, objectID, member.id, []string{"read", "contribute", "derive"})
	status, body, updated := member.postAction(t, object.ID, "set_title", map[string]any{"title": "Editor edit"}, 1)
	requireStatus(t, status, http.StatusOK, body)
	if updated.Version != 2 {
		t.Fatalf("editor action version = %d", updated.Version)
	}
	status, body, _ = member.call(t, http.MethodDelete, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusForbidden, body)

	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='authz.denied' AND decision='deny' AND resource_id=$1`, object.ID); got < 2 {
		t.Fatalf("authz.denied rows = %d, want at least 2", got)
	}
}

func TestInteractiveVotingAndReceiptReconciliation(t *testing.T) {
	env := newTestEnv(t)
	owner := env.register(t, "voteowner")
	viewer := env.register(t, "voteviewer")
	object := owner.createObject(t, "interactive", "Poll", map[string]any{
		"prompt": "pick one",
		"options": []map[string]any{
			{"label": "First"}, {"label": "Second"},
		},
	})
	options := jsonField[[]map[string]any](t, object.State, "options")
	if len(options) != 2 {
		t.Fatalf("options = %v", options)
	}
	first := options[0]["id"].(string)
	second := options[1]["id"].(string)
	if jsonField[string](t, object.State, "status") != "open" {
		t.Fatal("poll must start open")
	}
	votes := jsonField[map[string]any](t, object.State, "votes")
	if len(votes) != 2 {
		t.Fatalf("votes = %v", votes)
	}

	status, body, updated := owner.postAction(t, object.ID, "cast_vote", map[string]any{"option_id": first}, 1)
	requireStatus(t, status, http.StatusOK, body)
	if updated.Version != 2 || jsonField[map[string]any](t, updated.State, "votes")[first].(float64) != 1 {
		t.Fatalf("first vote state = %v", updated.State)
	}
	// Repeating the same option advances the version but not the count.
	status, body, updated = owner.postAction(t, object.ID, "cast_vote", map[string]any{"option_id": first}, 2)
	requireStatus(t, status, http.StatusOK, body)
	if updated.Version != 3 || jsonField[map[string]any](t, updated.State, "votes")[first].(float64) != 1 {
		t.Fatalf("repeat vote state = %v", updated.State)
	}
	// Changing the vote moves the count and keeps one receipt.
	status, body, updated = owner.postAction(t, object.ID, "cast_vote", map[string]any{"option_id": second}, 3)
	requireStatus(t, status, http.StatusOK, body)
	counts := jsonField[map[string]any](t, updated.State, "votes")
	if counts[first].(float64) != 0 || counts[second].(float64) != 1 {
		t.Fatalf("changed vote counts = %v", counts)
	}
	if got := env.count(t, `SELECT count(*) FROM object_action_receipts WHERE object_id=$1 AND user_id=$2 AND action_key='cast_vote'`, object.ID, owner.id.String()); got != 1 {
		t.Fatalf("receipts = %d, want 1", got)
	}
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE action='object.action' AND resource_id=$1 AND metadata->>'option_id'=$2`, object.ID, second); got != 1 {
		t.Fatalf("cast_vote audit metadata missing option_id: %d", got)
	}
	status, body, _ = owner.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/actions", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"action": "cast_vote", "input": map[string]any{}, "expected_version": 4,
		},
	})
	requireStatus(t, status, http.StatusBadRequest, body)
	if got := env.count(t, `
SELECT count(*)
  FROM living_objects o
 WHERE o.object_type = 'interactive'
   AND EXISTS (
     SELECT 1
       FROM jsonb_each_text(o.state->'votes') AS v(option_id, total)
       LEFT JOIN (
         SELECT payload->>'option_id' AS option_id, count(*)::int AS n
           FROM object_action_receipts
          WHERE object_id = o.id AND action_key = 'cast_vote'
          GROUP BY 1
       ) r ON r.option_id = v.option_id
      WHERE (v.total)::int <> COALESCE(r.n, 0)
   )`); got != 0 {
		t.Fatalf("vote reconciliation mismatches = %d, want 0", got)
	}

	status, body, updated = owner.postAction(t, object.ID, "close_poll", map[string]any{}, 4)
	requireStatus(t, status, http.StatusOK, body)
	if jsonField[string](t, updated.State, "status") != "closed" {
		t.Fatal("poll did not close")
	}
	status, body, _ = owner.postAction(t, object.ID, "cast_vote", map[string]any{"option_id": first}, 5)
	requireStatus(t, status, http.StatusConflict, body)
	requireCode(t, body, string(httpx.CodeConflict))

	// A viewer can read but cannot vote or close.
	env.addMembership(t, mustUUID(t, object.ID), viewer.id, []string{"read"})
	status, body, _ = viewer.postAction(t, object.ID, "cast_vote", map[string]any{"option_id": first}, 5)
	requireStatus(t, status, http.StatusForbidden, body)
	status, body, _ = viewer.postAction(t, object.ID, "close_poll", map[string]any{}, 5)
	requireStatus(t, status, http.StatusForbidden, body)
	status, body, _ = viewer.postAction(t, object.ID, "set_shared_context",
		map[string]any{"shared_context": map[string]any{"x": "y"}}, 5)
	requireStatus(t, status, http.StatusForbidden, body)

	// A collaborative-only action is rejected for an interactive template.
	status, body, _ = owner.postAction(t, object.ID, "set_title", map[string]any{"title": "nope"}, 5)
	requireStatus(t, status, http.StatusBadRequest, body)
}
