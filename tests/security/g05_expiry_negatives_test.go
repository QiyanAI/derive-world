// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"net/http"
	"testing"
)

// TestG05ExpiredInvitationAndDelegation covers the two time-based refusals in
// the G05 negative matrix: an expired invitation never creates membership, and
// an expired delegation blocks a fresh Personal AI turn without leaving an
// assistant message, proposal, or confirmation behind.
func TestG05ExpiredInvitationAndDelegation(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g05exp-a")
	b := env.register(t, "g05exp-b")
	object := a.createObject(t, "collaborative", "Expiry object", map[string]any{
		"body": "expiry body",
	})

	invite := a.createInvitation(t, object.ID, []string{"read"}, 1)
	env.exec(t, `UPDATE invitations
   SET created_at = now() - interval '2 days',
       expires_at = now() - interval '1 day'
 WHERE id = $1`, mustUUID(t, invite.ID))

	status, body, _ := b.call(t, http.MethodGet,
		"/api/v1/invitations/"+*invite.Token, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
	requireCode(t, body, "invitation_invalid")

	status, body = b.acceptInvitation(t, *invite.Token)
	requireStatus(t, status, http.StatusNotFound, body)
	requireCode(t, body, "invitation_invalid")

	if got := env.count(t,
		`SELECT count(*) FROM object_memberships WHERE object_id=$1 AND user_id=$2 AND revoked_at IS NULL`,
		mustUUID(t, object.ID), b.id); got != 0 {
		t.Fatalf("expired invitation created %d memberships, want 0", got)
	}
	var useCount int
	if err := env.store.Pool().QueryRow(t.Context(),
		`SELECT use_count FROM invitations WHERE id=$1`, mustUUID(t, invite.ID)).Scan(&useCount); err != nil {
		t.Fatalf("query invitation use_count: %v", err)
	}
	if useCount != 0 {
		t.Fatalf("expired invitation consumed %d uses, want 0", useCount)
	}

	conversation := a.createConversation(t, "Expired delegation chat", &object.ID)
	env.exec(t, `UPDATE delegation_grants
   SET created_at = now() - interval '2 days',
       expires_at = now() - interval '1 day'
 WHERE user_id = $1 AND revoked_at IS NULL`, a.id)
	status, body = a.sendMessage(t, conversation, "hello")
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, "forbidden")

	if got := env.count(t, `SELECT count(*) FROM ai_messages WHERE conversation_id=$1`,
		mustUUID(t, conversation)); got != 0 {
		t.Fatalf("expired delegation persisted %d messages, want 0", got)
	}
	if got := env.count(t, `SELECT count(*) FROM object_proposals WHERE actor_user_id=$1`, a.id); got != 0 {
		t.Fatalf("expired delegation persisted %d proposals, want 0", got)
	}
	if got := env.count(t, `SELECT count(*) FROM confirmations WHERE user_id=$1`, a.id); got != 0 {
		t.Fatalf("expired delegation persisted %d confirmations, want 0", got)
	}
}
