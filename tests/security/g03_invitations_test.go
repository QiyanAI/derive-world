// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func (u *testUser) rawCreateInvitation(t *testing.T, objectID, key string, body map[string]any) (int, []byte, http.Header) {
	t.Helper()
	return u.call(t, http.MethodPost, "/api/v1/objects/"+objectID+"/invitations", callOptions{
		IdempotencyKey: key,
		Body:           body,
	})
}

func TestG03InvitationDefaultOverrideAndRevocation(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03inv-a")
	b := env.register(t, "g03inv-b")
	object := a.createObject(t, "collaborative", "Invitation object", map[string]any{
		"body": "shared",
	})

	// Default is exactly read, never editor.
	defaultInvite := a.createInvitation(t, object.ID, nil, 1)
	if len(defaultInvite.Permissions) != 1 || defaultInvite.Permissions[0] != "read" {
		t.Fatalf("default permissions = %v", defaultInvite.Permissions)
	}
	var storedDefault []string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT permissions FROM invitations WHERE id = $1`,
		mustUUID(t, defaultInvite.ID)).Scan(&storedDefault); err != nil {
		t.Fatalf("query default permissions: %v", err)
	}
	if len(storedDefault) != 1 || storedDefault[0] != "read" {
		t.Fatalf("stored default permissions = %v", storedDefault)
	}
	status, body := b.acceptInvitation(t, *defaultInvite.Token)
	requireStatus(t, status, http.StatusOK, body)

	// Remove B, then re-invite as editor, then remove and re-invite read.
	removeStatus, removeBody, _ := a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/members/"+b.id.String(), callOptions{})
	requireStatus(t, removeStatus, http.StatusNoContent, removeBody)

	editorInvite := a.createInvitation(t, object.ID,
		[]string{"read", "contribute", "derive"}, 1)
	status, body = b.acceptInvitation(t, *editorInvite.Token)
	requireStatus(t, status, http.StatusOK, body)
	var editorAccept struct {
		Permissions []string `json:"permissions"`
		Role        string   `json:"role_effective"`
	}
	if err := json.Unmarshal(body, &editorAccept); err != nil {
		t.Fatalf("decode editor accept: %v", err)
	}
	if editorAccept.Role != "editor" {
		t.Fatalf("editor role = %q", editorAccept.Role)
	}
	removeStatus, removeBody, _ = a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/members/"+b.id.String(), callOptions{})
	requireStatus(t, removeStatus, http.StatusNoContent, removeBody)

	readInvite := a.createInvitation(t, object.ID, []string{"read"}, 2)
	status, body = b.acceptInvitation(t, *readInvite.Token)
	requireStatus(t, status, http.StatusOK, body)
	var readAccept struct {
		Permissions []string `json:"permissions"`
		Role        string   `json:"role_effective"`
	}
	if err := json.Unmarshal(body, &readAccept); err != nil {
		t.Fatalf("decode read accept: %v", err)
	}
	if readAccept.Role != "viewer" || len(readAccept.Permissions) != 1 ||
		readAccept.Permissions[0] != "read" {
		t.Fatalf("re-invited permissions = %v role=%s", readAccept.Permissions, readAccept.Role)
	}

	// Already-member accept is idempotent and does not consume a use.
	previewStatus, previewBody, _ := b.call(t, http.MethodGet,
		"/api/v1/invitations/"+*readInvite.Token, callOptions{})
	requireStatus(t, previewStatus, http.StatusOK, previewBody)
	var preview struct {
		AlreadyMember bool `json:"already_member"`
	}
	if err := json.Unmarshal(previewBody, &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !preview.AlreadyMember {
		t.Fatal("preview did not report an existing membership")
	}
	status, body = b.acceptInvitation(t, *readInvite.Token)
	requireStatus(t, status, http.StatusOK, body)
	var repeat struct {
		AlreadyMember bool `json:"already_member"`
	}
	if err := json.Unmarshal(body, &repeat); err != nil {
		t.Fatalf("decode repeat accept: %v", err)
	}
	if !repeat.AlreadyMember {
		t.Fatal("repeat accept was not idempotent")
	}
	var useCount int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT use_count FROM invitations WHERE id = $1`,
		mustUUID(t, readInvite.ID)).Scan(&useCount); err != nil {
		t.Fatalf("query use count: %v", err)
	}
	if useCount != 1 {
		t.Fatalf("already-member accept consumed a use: %d", useCount)
	}

	// Revoking B immediately removes read access.
	removeStatus, removeBody, _ = a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/members/"+b.id.String(), callOptions{})
	requireStatus(t, removeStatus, http.StatusNoContent, removeBody)
	hiddenStatus, hiddenBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, hiddenStatus, http.StatusNotFound, hiddenBody)
}

func TestG03InvitationLimitsExhaustionAndPreview(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03lim-a")
	b := env.register(t, "g03lim-b")
	c := env.register(t, "g03lim-c")
	d := env.register(t, "g03lim-d")
	object := a.createObject(t, "collaborative", "Limited object", map[string]any{
		"body":    "secret body value",
		"summary": "summary value",
	})

	// manage cannot be invited and bounds are enforced before storage.
	manageKey := newIdempotencyKey()
	status, body, _ := a.rawCreateInvitation(t, object.ID, manageKey,
		map[string]any{"permissions": []string{"read", "manage"}})
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, "invalid_request")
	status, body, _ = a.rawCreateInvitation(t, object.ID, newIdempotencyKey(),
		map[string]any{"max_uses": 101})
	requireStatus(t, status, http.StatusBadRequest, body)
	status, body, _ = a.rawCreateInvitation(t, object.ID, newIdempotencyKey(),
		map[string]any{"expires_in_days": 31})
	requireStatus(t, status, http.StatusBadRequest, body)

	invite := a.createInvitation(t, object.ID, []string{"read"}, 2)
	// Preview does not expose object state.
	previewStatus, previewBody, _ := b.call(t, http.MethodGet,
		"/api/v1/invitations/"+*invite.Token, callOptions{})
	requireStatus(t, previewStatus, http.StatusOK, previewBody)
	published := string(previewBody)
	for _, forbidden := range []string{"secret body value", "summary value", *invite.Token} {
		if strings.Contains(published, forbidden) {
			t.Fatalf("invitation preview leaked %q: %s", forbidden, published)
		}
	}
	var preview struct {
		Title         string   `json:"title"`
		Permissions   []string `json:"permissions"`
		RemainingUses int      `json:"remaining_uses"`
	}
	if err := json.Unmarshal(previewBody, &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.Title != "Limited object" || preview.RemainingUses != 2 {
		t.Fatalf("preview = %+v", preview)
	}

	status, body = b.acceptInvitation(t, *invite.Token)
	requireStatus(t, status, http.StatusOK, body)
	status, body = c.acceptInvitation(t, *invite.Token)
	requireStatus(t, status, http.StatusOK, body)
	status, body = d.acceptInvitation(t, *invite.Token)
	requireStatus(t, status, http.StatusNotFound, body)
	requireCode(t, body, "invitation_invalid")
	var useCount int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT use_count FROM invitations WHERE id = $1`,
		mustUUID(t, invite.ID)).Scan(&useCount); err != nil {
		t.Fatalf("query use count: %v", err)
	}
	if useCount != 2 {
		t.Fatalf("use_count = %d", useCount)
	}

	// Revocation invalidates both preview and accept.
	second := a.createInvitation(t, object.ID, []string{"read"}, 1)
	revokeStatus, revokeBody, _ := a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/invitations/"+second.ID, callOptions{})
	requireStatus(t, revokeStatus, http.StatusNoContent, revokeBody)
	previewStatus, previewBody, _ = b.call(t, http.MethodGet,
		"/api/v1/invitations/"+*second.Token, callOptions{})
	requireStatus(t, previewStatus, http.StatusNotFound, previewBody)
	status, body = d.acceptInvitation(t, *second.Token)
	requireStatus(t, status, http.StatusNotFound, body)

	// Unknown tokens are indistinguishable from revoked/expired/exhausted.
	unknownStatus, unknownBody, _ := d.call(t, http.MethodGet,
		"/api/v1/invitations/"+strings.Repeat("A", 43), callOptions{})
	requireStatus(t, unknownStatus, http.StatusNotFound, unknownBody)
	requireCode(t, unknownBody, "invitation_invalid")
}

func TestG03InvitationPermissionSubsetAndCreatorBoundaries(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03sub-a")
	b := env.register(t, "g03sub-b")
	object := a.createObject(t, "collaborative", "Subset object", map[string]any{
		"body": "body",
	})
	// Give B invite+read only. B must not be able to invite contribute.
	invite := a.createInvitation(t, object.ID, []string{"read", "invite"}, 1)
	status, body := b.acceptInvitation(t, *invite.Token)
	requireStatus(t, status, http.StatusOK, body)
	status, body, _ = b.rawCreateInvitation(t, object.ID, newIdempotencyKey(),
		map[string]any{"permissions": []string{"read", "contribute"}})
	requireStatus(t, status, http.StatusForbidden, body)
	requireCode(t, body, "forbidden")

	// B can create an invitation with only the permissions B holds.
	own := b.createInvitation(t, object.ID, []string{"read", "invite"}, 1)
	// B cannot revoke A's invitation, but can revoke B's own.
	status, body, _ = b.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/invitations/"+invite.ID, callOptions{})
	requireStatus(t, status, http.StatusNotFound, body)
	status, body, _ = b.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/invitations/"+own.ID, callOptions{})
	requireStatus(t, status, http.StatusNoContent, body)

	// A can revoke any invitation.
	again := a.createInvitation(t, object.ID, []string{"read"}, 1)
	status, body, _ = a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID+"/invitations/"+again.ID, callOptions{})
	requireStatus(t, status, http.StatusNoContent, body)
}

func TestG03InvitationIdempotencyRedactsToken(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03idem-a")
	object := a.createObject(t, "collaborative", "Idem object", map[string]any{
		"body": "body",
	})
	key := newIdempotencyKey()
	body := map[string]any{
		"permissions": []string{"read"}, "max_uses": 1, "expires_in_days": 7,
	}
	status, firstBody, _ := a.rawCreateInvitation(t, object.ID, key, body)
	requireStatus(t, status, http.StatusCreated, firstBody)
	var first invitationResponse
	if err := json.Unmarshal(firstBody, &first); err != nil {
		t.Fatalf("decode first invitation: %v", err)
	}
	if first.Token == nil || *first.Token == "" {
		t.Fatal("first response had no token")
	}
	status, replayBody, headers := a.rawCreateInvitation(t, object.ID, key, body)
	requireStatus(t, status, http.StatusOK, replayBody)
	if headers.Get("Idempotency-Replayed") != "true" {
		t.Fatal("replay header missing")
	}
	var replay struct {
		Token         *string `json:"token"`
		TokenRedacted bool    `json:"token_redacted"`
	}
	if err := json.Unmarshal(replayBody, &replay); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if replay.Token != nil || !replay.TokenRedacted {
		t.Fatalf("replay token = %v redacted=%v", replay.Token, replay.TokenRedacted)
	}
	if strings.Contains(string(replayBody), *first.Token) {
		t.Fatal("replay leaked the plaintext token")
	}
	if env.count(t, `SELECT count(*) FROM invitations WHERE object_id=$1`,
		mustUUID(t, object.ID)) != 1 {
		t.Fatal("idempotent replay created a second invitation")
	}
}

func TestG03DeriveLinkPermissions(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03dl-a")
	b := env.register(t, "g03dl-b")
	object := a.createObject(t, "collaborative", "Derive Link", map[string]any{
		"body": "link body",
	})
	invite := a.createInvitation(t, object.ID, []string{"read", "derive"}, 1)
	status, body := b.acceptInvitation(t, *invite.Token)
	requireStatus(t, status, http.StatusOK, body)
	// read+derive permits derive but not state actions.
	deriveStatus, deriveBody, _ := b.call(t, http.MethodPost,
		"/api/v1/objects/"+object.ID+"/derive", callOptions{
			IdempotencyKey: newIdempotencyKey(),
			Body: map[string]any{
				"title": "Derived via link", "inheritance_fields": []string{"body"},
			},
		})
	requireStatus(t, deriveStatus, http.StatusCreated, deriveBody)
	actionStatus, actionBody, _ := b.postAction(t, object.ID, "set_body",
		map[string]any{"body": "should fail"}, 1)
	requireStatus(t, actionStatus, http.StatusForbidden, actionBody)
}

func TestG03ManageAloneDoesNotCreateInvitation(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03minv-a")
	b := env.register(t, "g03minv-b")
	object := a.createObject(t, "collaborative", "Manage-only object", map[string]any{
		"body": "body",
	})
	// The DB permits a manage+read row; only the API normally creates manage for
	// the owner. The frozen create-invitation check requires `invite`, not manage.
	env.addMembership(t, mustUUID(t, object.ID), b.id, []string{"read", "manage"})
	inviteStatus, inviteBody, _ := b.rawCreateInvitation(t, object.ID,
		newIdempotencyKey(), map[string]any{"permissions": []string{"read"}})
	requireStatus(t, inviteStatus, http.StatusForbidden, inviteBody)
	requireCode(t, inviteBody, "forbidden")
}
