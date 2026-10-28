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
	if chars > 2000 {
		t.Fatalf("ordinary chat prompt is %d chars", chars)
	}
	createSystem, _, _, err := buildPrompt(nil, nil, (*handoff.Projection)(nil), nil, "x", "create")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(createSystem, "A world brief has this shape") {
		t.Fatal("creation intent did not include the world brief guide")
	}
}
