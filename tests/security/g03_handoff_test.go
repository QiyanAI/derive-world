// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestG03HandoffFiltersEventsAndPermissions(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03hof-a")
	b := env.register(t, "g03hof-b")
	a.setPrivateContext(t, "MARK-A handoff sentinel")
	object := a.createObject(t, "collaborative", "Handoff object", map[string]any{
		"body": "shared body",
	})
	invite := a.createInvitation(t, object.ID, []string{"read"}, 1)
	status, body := b.acceptInvitation(t, *invite.Token)
	requireStatus(t, status, http.StatusOK, body)
	// A later invitation must not appear to a viewer.
	hiddenInvite := a.createInvitation(t, object.ID, []string{"read"}, 1)
	actionStatus, actionBody, _ := a.postAction(t, object.ID, "set_body",
		map[string]any{"body": "updated shared body"}, 1)
	requireStatus(t, actionStatus, http.StatusOK, actionBody)

	status, handoffBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/handoff", callOptions{})
	requireStatus(t, status, http.StatusOK, handoffBody)
	var handoff struct {
		PermittedActions []string `json:"permitted_actions"`
		Context          struct {
			RecentEvents []struct {
				EventType string `json:"event_type"`
				Summary   string `json:"summary"`
			} `json:"recent_events"`
			State         map[string]any `json:"state"`
			SharedContext map[string]any `json:"shared_context"`
		} `json:"permitted_object_context"`
		DataProvenance []struct {
			Field     string `json:"field"`
			Truncated bool   `json:"truncated"`
		} `json:"data_provenance"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(handoffBody, &handoff); err != nil {
		t.Fatalf("decode handoff: %v", err)
	}
	if len(handoff.PermittedActions) != 1 || handoff.PermittedActions[0] != "read" {
		t.Fatalf("viewer permitted actions = %v", handoff.PermittedActions)
	}
	for _, event := range handoff.Context.RecentEvents {
		if event.EventType == "object.member_added" || event.EventType == "object.invitation_created" {
			t.Fatalf("handoff exposed filtered event %q", event.EventType)
		}
	}
	if strings.Contains(string(handoffBody), *hiddenInvite.Token) ||
		strings.Contains(string(handoffBody), "MARK-A") {
		t.Fatal("handoff leaked a token or private context")
	}
	if handoff.Context.State["body"] != "updated shared body" {
		t.Fatalf("handoff state body = %v", handoff.Context.State["body"])
	}

	// The event timeline hides invitation events from a viewer.
	eventsStatus, eventsBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/events", callOptions{})
	requireStatus(t, eventsStatus, http.StatusOK, eventsBody)
	if strings.Contains(string(eventsBody), "object.invitation_created") {
		t.Fatalf("viewer timeline exposed an invitation event: %s", eventsBody)
	}
	var auditCount int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events
 WHERE action='ai.handoff_issue' AND actor_user_id=$1 AND resource_id=$2`,
		b.id, mustUUID(t, object.ID)).Scan(&auditCount); err != nil {
		t.Fatalf("query handoff audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("handoff audit rows = %d", auditCount)
	}
	if strings.Contains(string(handoffBody), "private_ai_contexts") ||
		strings.Contains(string(handoffBody), "ai_conversations") {
		t.Fatal("handoff named a private table")
	}
}

func TestG03HandoffTruncationBudget(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03trunc-a")
	checklist := make([]map[string]any, 0, 70)
	for index := 0; index < 70; index++ {
		checklist = append(checklist, map[string]any{
			"text": strings.Repeat("x", 500), "done": false,
		})
	}
	tags := make([]string, 0, 20)
	for index := 0; index < 20; index++ {
		tags = append(tags, strings.Repeat("t", 50))
	}
	object := a.createObject(t, "collaborative", "Large handoff object", map[string]any{
		"body":      strings.Repeat("b", 20000),
		"checklist": checklist,
		"tags":      tags,
	})
	status, body, _ := a.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/handoff", callOptions{})
	requireStatus(t, status, http.StatusRequestEntityTooLarge, body)
	requireCode(t, body, "payload_too_large")

	// A moderate checklist fits after the frozen L3 body truncation.
	medium := make([]map[string]any, 0, 40)
	for index := 0; index < 40; index++ {
		medium = append(medium, map[string]any{
			"text": strings.Repeat("y", 500), "done": false,
		})
	}
	mediumObject := a.createObject(t, "collaborative", "Medium handoff object", map[string]any{
		"body":      strings.Repeat("z", 20000),
		"checklist": medium,
	})
	status, body, _ = a.call(t, http.MethodGet,
		"/api/v1/objects/"+mediumObject.ID+"/handoff", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if len(body) > 32*1024 {
		t.Fatalf("handoff bytes = %d", len(body))
	}
	var projection struct {
		Truncated bool `json:"truncated"`
		Context   struct {
			State map[string]any `json:"state"`
		} `json:"permitted_object_context"`
		DataProvenance []struct {
			Field     string `json:"field"`
			Truncated bool   `json:"truncated"`
		} `json:"data_provenance"`
	}
	if err := json.Unmarshal(body, &projection); err != nil {
		t.Fatalf("decode truncated handoff: %v", err)
	}
	if !projection.Truncated {
		t.Fatal("handoff did not report truncation")
	}
	bodyValue, _ := projection.Context.State["body"].(string)
	if len(bodyValue) > 8*1024+len("\n[truncated]") {
		t.Fatalf("state body length = %d", len(bodyValue))
	}
	found := false
	for _, item := range projection.DataProvenance {
		if item.Field == "state.body" && item.Truncated {
			found = true
		}
	}
	if !found {
		t.Fatal("state.body provenance was not marked truncated")
	}
}

func TestG03HandoffCannotBeUsedAsCredential(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03cred-a")
	b := env.register(t, "g03cred-b")
	object := a.createObject(t, "collaborative", "Credential object", map[string]any{
		"body": "body",
	})
	invite := a.createInvitation(t, object.ID, []string{"read"}, 1)
	status, body := b.acceptInvitation(t, *invite.Token)
	requireStatus(t, status, http.StatusOK, body)
	status, handoffBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/handoff", callOptions{})
	requireStatus(t, status, http.StatusOK, handoffBody)
	var handoff struct {
		HandoffID string `json:"handoff_id"`
	}
	if err := json.Unmarshal(handoffBody, &handoff); err != nil {
		t.Fatalf("decode handoff: %v", err)
	}
	// The handoff id is not accepted as a path, bearer token, or write credential.
	status, body = b.acceptInvitation(t, handoff.HandoffID)
	requireStatus(t, status, http.StatusNotFound, body)
	client := newClient(t)
	status, body, _ = env.call(t, client, http.MethodGet,
		"/api/v1/objects/"+object.ID, callOptions{
			Headers: map[string]string{"Authorization": "Bearer " + handoff.HandoffID},
		})
	requireStatus(t, status, http.StatusUnauthorized, body)
	actionStatus, actionBody, _ := b.postAction(t, object.ID, "set_body",
		map[string]any{"body": "viewer cannot write"}, 1)
	requireStatus(t, actionStatus, http.StatusForbidden, actionBody)
}

func TestG03HandoffUsesLatestTwentyBeforeFiltering(t *testing.T) {
	env := newTestEnv(t)
	a := env.register(t, "g03hof20-a")
	b := env.register(t, "g03hof20-b")
	object := a.createObject(t, "collaborative", "Latest twenty object", map[string]any{
		"body": "body",
	})
	invite := a.createInvitation(t, object.ID, []string{"read"}, 1)
	status, body := b.acceptInvitation(t, *invite.Token)
	requireStatus(t, status, http.StatusOK, body)
	for index := 0; index < 20; index++ {
		a.createInvitation(t, object.ID, []string{"read"}, 1)
	}
	status, handoffBody, _ := b.call(t, http.MethodGet,
		"/api/v1/objects/"+object.ID+"/handoff", callOptions{})
	requireStatus(t, status, http.StatusOK, handoffBody)
	var handoff struct {
		Context struct {
			RecentEvents []struct {
				EventType string `json:"event_type"`
			} `json:"recent_events"`
		} `json:"permitted_object_context"`
	}
	if err := json.Unmarshal(handoffBody, &handoff); err != nil {
		t.Fatalf("decode handoff: %v", err)
	}
	if len(handoff.Context.RecentEvents) != 0 {
		t.Fatalf("handoff reached behind the latest-20 window: %v", handoff.Context.RecentEvents)
	}
}
