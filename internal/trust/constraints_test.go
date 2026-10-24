// SPDX-License-Identifier: Apache-2.0
package trust

import (
	"strings"
	"testing"
)

func TestParseConstraintsDefaultsAndBounds(t *testing.T) {
	constraints, err := ParseConstraints(nil)
	if err != nil {
		t.Fatalf("default constraints: %v", err)
	}
	if constraints.MaxAIActionsPerHour != 20 || len(constraints.RequireConfirmation) != 2 {
		t.Fatalf("default = %+v", constraints)
	}
	for _, raw := range []string{
		`{"unknown":true}`,
		`{"max_ai_actions_per_hour":0}`,
		`{"max_ai_actions_per_hour":201}`,
		`{"require_confirmation":["not.an.action"]}`,
		`{"require_confirmation":["derive.create","derive.create"]}`,
		`[]`,
	} {
		if _, err := ParseConstraints([]byte(raw)); err == nil {
			t.Errorf("constraints %s should be rejected", raw)
		}
	}
}

func TestEffectiveRequiresDeriveConfirmationAlways(t *testing.T) {
	effective := &Effective{
		Permissions:         []string{"read", "contribute", "derive"},
		RequireConfirmation: map[string]bool{},
	}
	if !effective.RequiresConfirmation(ConfirmationDeriveCreate) {
		t.Fatal("derive must always require confirmation")
	}
	if !effective.RequiresConfirmation(ConfirmationCapabilityInvoke) {
		t.Fatal("side-effecting capability must always require confirmation")
	}
	if effective.RequiresConfirmation("object.propose_change") {
		t.Fatal("proposal creation is not a confirmation action")
	}
}

func TestConstraintsMarshalIsCanonical(t *testing.T) {
	constraints, err := ParseConstraints([]byte(`{"max_ai_actions_per_hour":5,"require_confirmation":["capability.invoke","derive.create"]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	encoded, err := constraints.MarshalCanonical()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"max_ai_actions_per_hour":5`) {
		t.Fatalf("canonical constraints = %s", text)
	}
	if strings.Index(text, `"capability.invoke"`) > strings.Index(text, `"derive.create"`) {
		t.Fatalf("require_confirmation is not sorted: %s", text)
	}
}
