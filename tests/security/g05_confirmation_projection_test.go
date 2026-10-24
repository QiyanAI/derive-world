// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

type g05ConfirmationView struct {
	ID                  string `json:"id"`
	Action              string `json:"action"`
	ResourceType        string `json:"resource_type"`
	TargetObjectID      string `json:"target_object_id"`
	TargetObjectTitle   string `json:"target_object_title"`
	TargetObjectVersion int    `json:"target_object_version"`
	BoundVersion        int    `json:"bound_version"`
	Effect              *struct {
		Title             string   `json:"title"`
		InheritanceFields []string `json:"inheritance_fields"`
		SourceVersion     *int     `json:"source_version"`
	} `json:"effect"`
}

// TestG05ConfirmationProjection verifies the G05 read projection used by the
// object workspace: a capability-backed derive confirmation identifies its
// target object and bound version, shows a bounded effect, remains owned by its
// creator, and is redacted after the creator loses object access.
func TestG05ConfirmationProjection(t *testing.T) {
	provider := &scriptedProvider{replies: []string{
		deriveReply("projected child"),
		deriveReply("other child"),
	}}
	env := newTestEnvWithProvider(t, nil, provider)
	a := env.register(t, "g05proj-a")
	b := env.register(t, "g05proj-b")

	object := a.createObject(t, "collaborative", "Projection object", map[string]any{
		"body": "projection body",
	})
	env.addMembership(t, mustUUID(t, object.ID), b.id,
		[]string{"read", "contribute", "derive"})

	conversation := b.createConversation(t, "B projection chat", &object.ID)
	confirmationID := b.createDeriveConfirmation(t, conversation, "derive")

	status, body, _ := b.call(t, http.MethodGet, "/api/v1/confirmations", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	var list struct {
		Items []g05ConfirmationView `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode confirmations: %v", err)
	}
	var found *g05ConfirmationView
	for index := range list.Items {
		if list.Items[index].ID == confirmationID {
			found = &list.Items[index]
			break
		}
	}
	if found == nil {
		t.Fatalf("confirmation %s missing from %s", confirmationID, body)
	}
	if found.ResourceType != "capability_invocation" {
		t.Fatalf("resource_type = %s, want capability_invocation", found.ResourceType)
	}
	if found.TargetObjectID != object.ID {
		t.Fatalf("target_object_id = %q, want %q", found.TargetObjectID, object.ID)
	}
	if found.TargetObjectTitle != "Projection object" {
		t.Fatalf("target_object_title = %q", found.TargetObjectTitle)
	}
	if found.BoundVersion != 1 || found.TargetObjectVersion != 1 {
		t.Fatalf("bound/live versions = %d/%d, want 1/1",
			found.BoundVersion, found.TargetObjectVersion)
	}
	if found.Effect == nil || found.Effect.Title != "projected child" {
		t.Fatalf("effect = %+v", found.Effect)
	}
	if len(found.Effect.InheritanceFields) != 1 || found.Effect.InheritanceFields[0] != "body" {
		t.Fatalf("inheritance_fields = %v", found.Effect.InheritanceFields)
	}

	// Another user never sees this confirmation.
	status, otherBody, _ := a.call(t, http.MethodGet, "/api/v1/confirmations", callOptions{})
	requireStatus(t, status, http.StatusOK, otherBody)
	var otherList struct {
		Items []g05ConfirmationView `json:"items"`
	}
	if err := json.Unmarshal(otherBody, &otherList); err != nil {
		t.Fatalf("decode other confirmations: %v", err)
	}
	for _, item := range otherList.Items {
		if item.ID == confirmationID {
			t.Fatal("another user received B's confirmation")
		}
	}

	// After membership revocation the pending row still belongs to B, but the
	// unreadable object's title, version, and effect are clipped.
	env.revokeMembership(t, mustUUID(t, object.ID), b.id)
	status, redactedBody, _ := b.call(t, http.MethodGet, "/api/v1/confirmations", callOptions{})
	requireStatus(t, status, http.StatusOK, redactedBody)
	var redactedList struct {
		Items []g05ConfirmationView `json:"items"`
	}
	if err := json.Unmarshal(redactedBody, &redactedList); err != nil {
		t.Fatalf("decode redacted confirmations: %v", err)
	}
	var redacted *g05ConfirmationView
	for index := range redactedList.Items {
		if redactedList.Items[index].ID == confirmationID {
			redacted = &redactedList.Items[index]
			break
		}
	}
	if redacted == nil {
		t.Fatalf("confirmation disappeared instead of being redacted: %s", redactedBody)
	}
	if redacted.TargetObjectTitle != "" || redacted.TargetObjectVersion != 0 {
		t.Fatalf("unreadable target leaked: %+v", redacted)
	}
	if redacted.TargetObjectID != "" || redacted.BoundVersion != 0 || redacted.Effect != nil {
		t.Fatalf("unreadable target metadata leaked: %+v", redacted)
	}
}
