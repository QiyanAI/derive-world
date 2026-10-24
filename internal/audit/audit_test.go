// SPDX-License-Identifier: Apache-2.0
package audit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestActionRegistryIsClosedAndUnique(t *testing.T) {
	actions := AllActions()
	if len(actions) != 49 {
		t.Fatalf("action count = %d, want 49", len(actions))
	}
	seen := map[Action]bool{}
	for _, action := range actions {
		if seen[action] {
			t.Fatalf("duplicate action %q", action)
		}
		seen[action] = true
		if !IsRegistered(action) {
			t.Fatalf("registered action %q not found", action)
		}
	}
	if IsRegistered("object.unknown") {
		t.Fatal("unregistered action was accepted")
	}
}

func TestMetadataRedactionAndLimits(t *testing.T) {
	encoded, err := normalizeMetadata(map[string]any{
		"action":   "set_title",
		"password": "hunter2",
		"token":    "secret",
		"nested":   map[string]any{"x": 1},
	})
	if err == nil {
		t.Fatal("nested metadata was accepted")
	}
	_ = encoded

	encoded, err = normalizeMetadata(map[string]any{
		"action":          "set_title",
		"password":        "hunter2",
		"token":           "secret",
		"private_context": "MARK",
		"from_version":    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"password", "token", "private_context"} {
		if decoded[key] != "[REDACTED]" {
			t.Fatalf("%s = %v", key, decoded[key])
		}
	}
	if decoded["action"] != "set_title" || decoded["from_version"].(float64) != 1 {
		t.Fatalf("safe metadata changed: %v", decoded)
	}

	if _, err := normalizeMetadata(map[string]any{"blob": strings.Repeat("x", 5000)}); err == nil {
		t.Fatal("oversized metadata was accepted")
	}
}

func TestEventValidation(t *testing.T) {
	userID := uuid.New()
	profileID := uuid.New()
	delegationID := uuid.New()
	base := Event{
		Actor:        UserActor(userID),
		Action:       ActionObjectCreate,
		ResourceType: "living_object",
		Decision:     "allow",
		RequestID:    "request-0001",
	}
	if err := validate(base); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	system := base
	system.Actor = SystemActor()
	if err := validate(system); err != nil {
		t.Fatalf("valid system event rejected: %v", err)
	}
	system.Actor.DelegationID = &delegationID
	if err := validate(system); err == nil {
		t.Fatal("system actor with delegation was accepted")
	}
	badUser := base
	badUser.Actor.AIProfileID = &profileID
	if err := validate(badUser); err == nil {
		t.Fatal("user actor with AI profile was accepted")
	}
	badAction := base
	badAction.Action = "not.registered"
	if err := validate(badAction); err == nil {
		t.Fatal("unregistered action was accepted")
	}
	badDecision := base
	badDecision.Decision = "maybe"
	if err := validate(badDecision); err == nil {
		t.Fatal("invalid decision was accepted")
	}
	badRequest := base
	badRequest.RequestID = "short"
	if err := validate(badRequest); err == nil {
		t.Fatal("short request id was accepted")
	}
	badIP := base
	badIP.IPHash = []byte{1, 2, 3}
	if err := validate(badIP); err == nil {
		t.Fatal("short IP hash was accepted")
	}
}
