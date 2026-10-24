// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestG03MemberRemovalSemantics(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03mem-a")
	b := env.register(t, "g03mem-b")
	c := env.register(t, "g03mem-c")
	object := a.createObject(t, "collaborative", "Member object", map[string]any{
		"body": "body",
	})
	editor := a.createInvitation(t, object.ID, []string{"read", "contribute", "derive"}, 1)
	status, body := b.acceptInvitation(t, *editor.Token)
	requireStatus(t, status, http.StatusOK, body)
	viewer := a.createInvitation(t, object.ID, []string{"read"}, 1)
	status, body = c.acceptInvitation(t, *viewer.Token)
	requireStatus(t, status, http.StatusOK, body)

	// Owner cannot leave in v0.1.0.
	status, body, _ = a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/members/"+a.id.String(), callOptions{})
	requireStatus(t, status, http.StatusConflict, body)
	requireCode(t, body, "conflict")
	var envelope struct {
		Error struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode owner conflict: %v", err)
	}
	if envelope.Error.Details["reason"] != "owner_cannot_leave" {
		t.Fatalf("owner conflict details = %v", envelope.Error.Details)
	}

	// An editor has no manage permission.
	status, body, _ = b.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/members/"+c.id.String(), callOptions{})
	requireStatus(t, status, http.StatusForbidden, body)
	// A non-member target is hidden.
	outsider := env.register(t, "g03mem-out")
	status, body, _ = a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/members/"+outsider.id.String(), callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)

	// Owner removes the editor; access is immediate and events remain.
	status, body, _ = a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/members/"+b.id.String(), callOptions{})
	requireStatus(t, status, http.StatusNoContent, body)
	status, body, _ = b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
	if env.count(t, `SELECT count(*) FROM object_events
 WHERE object_id=$1 AND event_type='object.member_removed'`,
		mustUUID(t, object.ID)) != 1 {
		t.Fatal("member removal event missing")
	}
	if env.count(t, `SELECT count(*) FROM audit_events
 WHERE action='object.member_remove' AND resource_id=$1`,
		mustUUID(t, object.ID)) != 1 {
		t.Fatal("member removal audit missing")
	}
	var memberEvent string
	var reason string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT payload->>'member_user_id', payload->>'reason'
  FROM object_events
 WHERE object_id=$1 AND event_type='object.member_removed'
 ORDER BY seq DESC LIMIT 1`, mustUUID(t, object.ID)).Scan(&memberEvent, &reason); err != nil {
		t.Fatalf("read member_removed payload: %v", err)
	}
	if memberEvent != b.id.String() || reason != "removed" {
		t.Fatalf("member_removed payload member=%q reason=%q", memberEvent, reason)
	}
}

func TestG03ExportIsSelfOnlyAndRateLimited(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03exp-a")
	b := env.register(t, "g03exp-b")
	a.setPrivateContext(t, "MARK-EXPORT private data")
	object := a.createObject(t, "collaborative", "Export object", map[string]any{
		"body": "export body",
	})
	conversation := a.createConversation(t, "Export chat", nil)
	status, body := a.sendMessage(t, conversation, "export message")
	requireStatus(t, status, http.StatusOK, body)
	token := a.createToken(t, "export token", []string{"objects:read"})

	status, body, _ = a.call(t, http.MethodGet, "/api/v1/me/export", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	exported := string(body)
	for _, expected := range []string{"MARK-EXPORT", object.ID, conversation, token.Prefix} {
		if !strings.Contains(exported, expected) {
			t.Fatalf("export missing %q", expected)
		}
	}
	for _, forbidden := range []string{token.Plaintext, "password_hash", "token_hash", b.email} {
		if strings.Contains(exported, forbidden) {
			t.Fatalf("export leaked %q", forbidden)
		}
	}
	var auditCount int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events WHERE action='me.export' AND actor_user_id=$1`,
		a.id).Scan(&auditCount); err != nil {
		t.Fatalf("query export audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("export audit rows = %d", auditCount)
	}
	status, body, _ = a.call(t, http.MethodGet, "/api/v1/me/export", callOptions{})
	requireStatus(t, status, http.StatusTooManyRequests, body)
	requireCode(t, body, "rate_limited")
}
