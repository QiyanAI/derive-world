// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"strings"
	"testing"

	"github.com/QiyanAI/derive-world/internal/handoff"
)

func TestPromptBudgetShape(t *testing.T) {
	system, _, chars, err := buildPrompt(nil, nil, (*handoff.Projection)(nil), nil, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(system, "A world brief has this shape") {
		t.Fatal("the full world brief guide leaked into ordinary chat")
	}
	// The compact contract is deliberately always present: a follow-up turn
	// such as "批准通过" has no creation keyword and must still know the
	// envelope, the surface vocabulary and the approval rules. It is ~0.9 KB,
	// an order of magnitude below the 4.6 KB full guide that stays gated.
	if !strings.Contains(system, "WORLD BRIEF CONTRACT") {
		t.Fatal("the compact world brief contract is missing from an ordinary turn")
	}
	if chars > 3000 {
		t.Fatalf("ordinary chat prompt is %d chars", chars)
	}
	createSystem, _, _, err := buildPrompt(nil, nil, (*handoff.Projection)(nil), nil, "x", "create")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(createSystem, "A world brief has this shape") {
		t.Fatal("creation intent did not include the world brief guide")
	}
	// Any follow-up in a global conversation keeps the full schema. The real
	// 12:04 failure was exactly this case: the approval turn had no guide and
	// the model invented a non-existent brief shape.
	history := []Message{{Role: "user", Content: "伦敦贝克街的街景"}}
	followSystem, _, _, err := buildPrompt(nil, nil, (*handoff.Projection)(nil), history, "批准通过", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(followSystem, "A world brief has this shape") {
		t.Fatal("a follow-up turn lost the full world brief guide")
	}
}
