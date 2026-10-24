// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/httpx"
)

func TestAuditFailureRollsBackAllWritePaths(t *testing.T) {
	t.Run("action", func(t *testing.T) {
		env := newTestEnv(t)
		user := env.register(t, "auditaction")
		object := user.createObject(t, "collaborative", "Audit action", map[string]any{
			"summary": "", "body": "", "checklist": []map[string]any{}, "tags": []string{},
		})
		env.blockAuditAction(t, "object.action")
		status, body, _ := user.postAction(t, object.ID, "set_title", map[string]any{"title": "Blocked"}, 1)
		requireStatus(t, status, http.StatusInternalServerError, body)
		status, body, _ = user.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
		requireStatus(t, status, http.StatusOK, body)
		reloaded := decodeObject(t, body)
		if reloaded.Version != 1 || jsonField[string](t, reloaded.State, "title") != "Audit action" {
			t.Fatalf("audit failure changed object: %+v", reloaded)
		}
		if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1`, object.ID); got != 1 {
			t.Fatalf("audit failure left %d events, want 1", got)
		}
	})

	t.Run("derive", func(t *testing.T) {
		env := newTestEnv(t)
		user := env.register(t, "auditderive")
		parent := user.createObject(t, "collaborative", "Audit derive parent", map[string]any{
			"summary": "s", "body": "", "checklist": []map[string]any{}, "tags": []string{},
		})
		env.blockAuditAction(t, "object.derive")
		title := "Audit derive child " + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
		status, body, _, _ := deriveCall(t, user, parent.ID, map[string]any{
			"title": title, "inheritance_fields": []string{"summary"},
		}, newIdempotencyKey())
		requireStatus(t, status, http.StatusInternalServerError, body)
		if got := env.count(t, `SELECT count(*) FROM living_objects WHERE state->>'title'=$1`, title); got != 0 {
			t.Fatalf("audit failure left %d children", got)
		}
		if got := env.count(t, `SELECT count(*) FROM derivation_edges WHERE parent_object_id=$1`, parent.ID); got != 0 {
			t.Fatalf("audit failure left %d edges", got)
		}
	})

	t.Run("delete", func(t *testing.T) {
		env := newTestEnv(t)
		user := env.register(t, "auditdelete")
		object := user.createObject(t, "collaborative", "Audit delete", map[string]any{
			"summary": "", "body": "", "checklist": []map[string]any{}, "tags": []string{},
		})
		env.blockAuditAction(t, "object.delete")
		status, body, _ := user.call(t, http.MethodDelete, "/api/v1/objects/"+object.ID, callOptions{})
		requireStatus(t, status, http.StatusInternalServerError, body)
		if got := env.count(t, `SELECT count(*) FROM living_objects WHERE id=$1 AND status='active'`, object.ID); got != 1 {
			t.Fatal("audit failure deleted the object")
		}
		if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1 AND event_type='object.deleted'`, object.ID); got != 0 {
			t.Fatal("audit failure left a delete event")
		}
	})

	t.Run("register", func(t *testing.T) {
		env := newTestEnv(t)
		env.blockAuditAction(t, "auth.register")
		origin := env.origin
		suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
		email := "auditreg." + suffix + "@example.test"
		status, body, _ := env.call(t, newClient(t), http.MethodPost, "/api/v1/auth/register", callOptions{
			Origin: &origin,
			Body: map[string]any{
				"email": email, "handle": "ar" + suffix,
				"display_name": "Audit Register", "password": "correct horse battery staple",
			},
		})
		requireStatus(t, status, http.StatusInternalServerError, body)
		if got := env.count(t, `SELECT count(*) FROM users WHERE email=$1`, email); got != 0 {
			t.Fatal("audit failure left a user row")
		}
	})

	t.Run("logout", func(t *testing.T) {
		env := newTestEnv(t)
		user := env.register(t, "auditlogout")
		env.blockAuditAction(t, "auth.logout")
		status, body, _ := user.call(t, http.MethodPost, "/api/v1/auth/logout", callOptions{})
		requireStatus(t, status, http.StatusInternalServerError, body)
		status, body, _ = user.call(t, http.MethodGet, "/api/v1/auth/me", callOptions{})
		requireStatus(t, status, http.StatusOK, body)
	})
}

func TestAuditFailureRollsBackBusinessTransaction(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "auditrollback")
	const constraint = "test_block_object_create"
	env.exec(t, `ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS `+constraint)
	env.exec(t, `ALTER TABLE audit_events ADD CONSTRAINT `+constraint+` CHECK (action <> 'object.create') NOT VALID`)
	t.Cleanup(func() {
		_, _ = env.store.Pool().Exec(context.Background(), `ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS `+constraint)
	})

	title := "Audit rollback " + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	key := newIdempotencyKey()
	payload := map[string]any{
		"object_type": "collaborative", "title": title,
		"content": map[string]any{}, "shared_context": map[string]any{},
	}
	status, body, _ := user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: key, Body: payload,
	})
	requireStatus(t, status, http.StatusInternalServerError, body)
	requireCode(t, body, string(httpx.CodeInternal))
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE state->>'title'=$1 AND creator_id=$2`, title, user.id.String()); got != 0 {
		t.Fatalf("audit failure left %d objects, want 0", got)
	}
	if got := env.count(t, `SELECT count(*) FROM object_events e JOIN living_objects o ON o.id=e.object_id WHERE o.state->>'title'=$1`, title); got != 0 {
		t.Fatalf("audit failure left %d events, want 0", got)
	}

	// An internal failure must not be cached as a permanent 500. Remove the
	// injected failure, expire the lease, and retry the same key/body.
	env.exec(t, `ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS `+constraint)
	env.exec(t, `
UPDATE idempotency_keys SET lease_until = now() - interval '1 minute'
 WHERE actor_user_id = $1 AND route = '/api/v1/objects' AND key = $2`,
		user.id.String(), key)
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: key, Body: payload,
	})
	requireStatus(t, status, http.StatusCreated, body)
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE state->>'title'=$1 AND creator_id=$2`, title, user.id.String()); got != 1 {
		t.Fatalf("retry after audit recovery created %d objects, want 1", got)
	}
}

func TestIdempotentReplayAfterPermissionLoss(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "replayloss")
	object := user.createObject(t, "collaborative", "Replay loss", map[string]any{
		"summary": "", "body": "", "checklist": []map[string]any{}, "tags": []string{},
	})
	objectID := mustUUID(t, object.ID)

	actionKey := newIdempotencyKey()
	status, body, _ := user.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/actions", callOptions{
		IdempotencyKey: actionKey,
		Body: map[string]any{
			"action": "set_title", "input": map[string]any{"title": "Replay loss v2"}, "expected_version": 1,
		},
	})
	requireStatus(t, status, http.StatusOK, body)
	eventsBefore := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1`, object.ID)

	env.revokeMembership(t, objectID, user.id)
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/actions", callOptions{
		IdempotencyKey: actionKey,
		Body: map[string]any{
			"action": "set_title", "input": map[string]any{"title": "Replay loss v2"}, "expected_version": 1,
		},
	})
	requireStatus(t, status, http.StatusNotFound, body)
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id=$1`, object.ID); got != eventsBefore {
		t.Fatal("denied replay wrote an event")
	}

	// Restore read but not derive: replay must be re-authorized before claiming.
	env.addMembership(t, objectID, user.id, []string{"read"})
	beforeEdges := env.count(t, `SELECT count(*) FROM derivation_edges WHERE parent_object_id=$1`, object.ID)
	deriveKey := newIdempotencyKey()
	status, body, _, _ = deriveCall(t, user, object.ID, map[string]any{
		"title": "Replay derive", "inheritance_fields": []string{"summary"},
	}, deriveKey)
	requireStatus(t, status, http.StatusForbidden, body)
	if got := env.count(t, `SELECT count(*) FROM derivation_edges WHERE parent_object_id=$1`, object.ID); got != beforeEdges {
		t.Fatal("denied derive created an edge")
	}

	// Deleted objects never replay an old success.
	env.addMembership(t, objectID, user.id, []string{"read", "contribute", "derive", "invite", "manage"})
	status, body, _ = user.call(t, http.MethodDelete, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusNoContent, body)
	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects/"+object.ID+"/actions", callOptions{
		IdempotencyKey: actionKey,
		Body: map[string]any{
			"action": "set_title", "input": map[string]any{"title": "Replay loss v2"}, "expected_version": 2,
		},
	})
	requireStatus(t, status, http.StatusNotFound, body)
	status, body, _, _ = deriveCall(t, user, object.ID, map[string]any{
		"title": "Deleted derive", "inheritance_fields": []string{"summary"},
	}, newIdempotencyKey())
	requireStatus(t, status, http.StatusNotFound, body)
}

func TestDeriveEdgeFailureRollsBackChild(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "deriveatomic")
	parent := user.createObject(t, "collaborative", "Atomic parent", map[string]any{
		"summary": "atomic", "body": "", "checklist": []map[string]any{}, "tags": []string{},
	})
	const constraint = "test_block_derive"
	env.exec(t, `ALTER TABLE derivation_edges DROP CONSTRAINT IF EXISTS `+constraint)
	env.exec(t, `ALTER TABLE derivation_edges ADD CONSTRAINT `+constraint+` CHECK (false) NOT VALID`)
	t.Cleanup(func() {
		_, _ = env.store.Pool().Exec(context.Background(), `ALTER TABLE derivation_edges DROP CONSTRAINT IF EXISTS `+constraint)
	})

	title := "Atomic child " + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	status, body, _, _ := deriveCall(t, user, parent.ID, map[string]any{
		"title": title, "inheritance_fields": []string{"summary"},
	}, newIdempotencyKey())
	requireStatus(t, status, http.StatusInternalServerError, body)
	requireCode(t, body, string(httpx.CodeInternal))
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE state->>'title'=$1`, title); got != 0 {
		t.Fatalf("edge failure left %d child objects", got)
	}
	if got := env.count(t, `SELECT count(*) FROM derivation_edges WHERE parent_object_id=$1`, parent.ID); got != 0 {
		t.Fatalf("edge failure left %d edges", got)
	}
	if got := env.count(t, `SELECT count(*) FROM object_events WHERE object_id IN (SELECT id FROM living_objects WHERE state->>'title'=$1)`, title); got != 0 {
		t.Fatalf("edge failure left %d child events", got)
	}
}

func TestRestartPersistence(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "restart")
	object := user.createObject(t, "collaborative", "Survives restart", map[string]any{
		"summary": "persisted", "body": "", "checklist": []map[string]any{}, "tags": []string{},
	})
	status, body, updated := user.postAction(t, object.ID, "set_body", map[string]any{"body": "after restart"}, 1)
	requireStatus(t, status, http.StatusOK, body)
	if updated.Version != 2 {
		t.Fatalf("version = %d", updated.Version)
	}
	status, body, child, _ := deriveCall(t, user, object.ID, map[string]any{
		"title": "Restart child", "inheritance_fields": []string{"body"},
	}, newIdempotencyKey())
	requireStatus(t, status, http.StatusCreated, body)

	// A second app instance with a fresh pool represents a process restart.
	restarted := newTestEnv(t)
	relogin, status, body := restarted.login(t, user.email, user.password)
	requireStatus(t, status, http.StatusOK, body)
	status, body, _ = relogin.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	reloaded := decodeObject(t, body)
	if reloaded.Version != 2 || jsonField[string](t, reloaded.State, "body") != "after restart" {
		t.Fatalf("reloaded object = %+v", reloaded)
	}
	status, body, _ = relogin.call(t, http.MethodGet, "/api/v1/objects/"+object.ID+"/lineage", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var lineage struct {
		Descendants []map[string]any `json:"descendants"`
	}
	if err := json.Unmarshal(body, &lineage); err != nil {
		t.Fatalf("decode lineage: %v", err)
	}
	if len(lineage.Descendants) != 1 || lineage.Descendants[0]["object_id"] != child.ID {
		t.Fatalf("persisted descendants = %v", lineage.Descendants)
	}
}

func TestEventAndObjectPagination(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "pagination")
	object := user.createObject(t, "collaborative", "Paging", map[string]any{
		"summary": "", "body": "", "checklist": []map[string]any{}, "tags": []string{},
	})
	version := 1
	for _, title := range []string{"p2", "p3", "p4", "p5"} {
		status, body, _ := user.postAction(t, object.ID, "set_title", map[string]any{"title": title}, version)
		requireStatus(t, status, http.StatusOK, body)
		version++
	}

	seen := map[int64]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		path := "/api/v1/objects/" + object.ID + "/events?limit=2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		status, body, _ := user.call(t, http.MethodGet, path, callOptions{})
		requireStatus(t, status, http.StatusOK, body)
		var response struct {
			Items []struct {
				Seq int64 `json:"seq"`
			} `json:"items"`
			NextCursor *string `json:"next_cursor"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatalf("decode events page: %v", err)
		}
		for _, item := range response.Items {
			if seen[item.Seq] {
				t.Fatalf("event seq %d repeated", item.Seq)
			}
			seen[item.Seq] = true
		}
		if response.NextCursor == nil {
			break
		}
		cursor = *response.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("event count = %d, want 5", len(seen))
	}

	for index := 0; index < 3; index++ {
		user.createObject(t, "collaborative", "List page "+uuid.NewString(), map[string]any{
			"summary": "", "body": "", "checklist": []map[string]any{}, "tags": []string{},
		})
	}
	seenObjects := map[string]bool{}
	cursor = ""
	for page := 0; page < 10; page++ {
		path := "/api/v1/objects?scope=owned&limit=2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		status, body, _ := user.call(t, http.MethodGet, path, callOptions{})
		requireStatus(t, status, http.StatusOK, body)
		var response struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			NextCursor *string `json:"next_cursor"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatalf("decode object page: %v", err)
		}
		for _, item := range response.Items {
			if seenObjects[item.ID] {
				t.Fatalf("object %s repeated", item.ID)
			}
			seenObjects[item.ID] = true
		}
		if response.NextCursor == nil {
			break
		}
		cursor = *response.NextCursor
		if page == 0 {
			status, body, _ := user.call(t, http.MethodGet, "/api/v1/objects?scope=shared&cursor="+url.QueryEscape(cursor), callOptions{})
			requireStatus(t, status, http.StatusBadRequest, body)
		}
	}
	if len(seenObjects) != 4 {
		t.Fatalf("object list count = %d, want 4", len(seenObjects))
	}
}

func TestFrozenSizeLimits(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "limits")

	longText := strings.Repeat("a", 500)
	checklist := make([]map[string]any, 0, 100)
	for index := 0; index < 100; index++ {
		checklist = append(checklist, map[string]any{"text": longText, "done": false})
	}
	status, body, _ := user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"object_type": "collaborative", "title": "Too large",
			"content": map[string]any{
				"summary":   strings.Repeat("s", 2000),
				"body":      strings.Repeat("b", 20000),
				"checklist": checklist,
				"tags":      []string{},
			},
			"shared_context": map[string]any{},
		},
	})
	requireStatus(t, status, http.StatusRequestEntityTooLarge, body)
	requireCode(t, body, string(httpx.CodePayloadTooLarge))
	if got := env.count(t, `SELECT count(*) FROM living_objects WHERE state->>'title'='Too large'`); got != 0 {
		t.Fatal("oversized state was persisted")
	}

	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"object_type": "collaborative", "title": "Shared too large",
			"content":        map[string]any{},
			"shared_context": map[string]any{"blob": strings.Repeat("x", 9000)},
		},
	})
	requireStatus(t, status, http.StatusRequestEntityTooLarge, body)

	status, body, _ = user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"object_type": "interactive", "title": "One option",
			"content":        map[string]any{"options": []map[string]any{{"label": "only"}}},
			"shared_context": map[string]any{},
		},
	})
	requireStatus(t, status, http.StatusBadRequest, body)
}

func TestCredentialsAreNeverStoredInPlaintext(t *testing.T) {
	env := newTestEnv(t)
	user := env.register(t, "credentials")
	serverURL, err := url.Parse(env.server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	var sessionToken string
	for _, cookie := range user.client.Jar.Cookies(serverURL) {
		if cookie.Name == "dw_session" {
			sessionToken = cookie.Value
		}
	}
	if sessionToken == "" {
		t.Fatal("session cookie is missing")
	}
	sum := sha256.Sum256([]byte(sessionToken))
	var tokenHash []byte
	var csrfHash []byte
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT token_hash, csrf_hash FROM user_sessions WHERE user_id=$1 ORDER BY created_at DESC LIMIT 1`, user.id.String()).
		Scan(&tokenHash, &csrfHash); err != nil {
		t.Fatalf("read session hashes: %v", err)
	}
	if len(tokenHash) != 32 || len(csrfHash) != 32 {
		t.Fatalf("session hashes must be 32 bytes, got %d/%d", len(tokenHash), len(csrfHash))
	}
	if string(tokenHash) != string(sum[:]) {
		t.Fatal("session token hash does not match the cookie token")
	}
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE metadata::text LIKE '%' || $1 || '%'`, user.password); got != 0 {
		t.Fatalf("password appeared in %d audit rows", got)
	}
	if got := env.count(t, `SELECT count(*) FROM audit_events WHERE metadata::text LIKE '%' || $1 || '%'`, sessionToken); got != 0 {
		t.Fatalf("session token appeared in %d audit rows", got)
	}
}
