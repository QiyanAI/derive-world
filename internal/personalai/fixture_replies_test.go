// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/worldseed"
)

// The deterministic E2E corpus must contain actions the real server accepts.
// A scripted reply that cannot pass the same strict decoder and compiler as a
// provider reply would make the browser test pass against a fiction.
func TestRemediationFixtureRepliesCompile(t *testing.T) {
	raw, err := os.ReadFile("../../tests/fixtures/model/p0-remediation.json")
	if err != nil {
		t.Fatalf("read fixture corpus: %v", err)
	}
	var corpus struct {
		Responses []struct {
			Match string `json:"match"`
			Reply string `json:"reply"`
		} `json:"responses"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatalf("parse fixture corpus: %v", err)
	}
	if len(corpus.Responses) == 0 {
		t.Fatal("fixture corpus is empty")
	}
	for _, entry := range corpus.Responses {
		if !strings.Contains(entry.Reply, "proposed_actions") {
			continue
		}
		_, actions, status, err := parseModelOutput(entry.Reply)
		if err != nil {
			t.Fatalf("%q: parse: %v", entry.Match, err)
		}
		if status != "structured" {
			// One entry deliberately exercises the unreadable-output path.
			if entry.Match == "Show me an unreadable plan" {
				continue
			}
			t.Fatalf("%q: status=%q", entry.Match, status)
		}
		for _, action := range actions {
			switch action.CapabilityID {
			case "world.create":
				var create worldCreateActionInput
				if err := decodeSingleJSON(string(action.Input), &create); err != nil {
					t.Fatalf("%q: world.create input: %v", entry.Match, err)
				}
				if _, rejection := buildWorldCreateContent(create); rejection != nil {
					t.Fatalf("%q: world.create rejected at %s: %s", entry.Match, rejection.Stage, rejection.Detail)
				}
			case "world.reimagine":
				var reimagine worldReimagineInput
				if err := decodeSingleJSON(string(action.Input), &reimagine); err != nil {
					t.Fatalf("%q: reimagine input: %v", entry.Match, err)
				}
				seed, err := worldseed.Decode(reimagine.Brief)
				if err != nil {
					t.Fatalf("%q: reimagine brief: %v", entry.Match, err)
				}
				if _, err := worldseed.Reshape(blankWorldState(t), seed); err != nil {
					t.Fatalf("%q: reimagine reshape: %v", entry.Match, err)
				}
			case "object.propose_change":
				var proposal proposalActionInput
				if err := decodeSingleJSON(string(action.Input), &proposal); err != nil {
					t.Fatalf("%q: propose_change input: %v", entry.Match, err)
				}
				if proposal.Action == "" || len(proposal.Input) == 0 {
					t.Fatalf("%q: propose_change is empty", entry.Match)
				}
			}
		}
	}
}

func blankWorldState(t *testing.T) json.RawMessage {
	t.Helper()
	content := json.RawMessage(`{"premise":"","kinds":[],"nodes":[],"links":[],"rules":[],"views":["scene","stage","cards","timeline"],"tags":[]}`)
	state, _, err := objects.ValidateCreate(objects.CreateInput{
		ObjectType: objects.TypeOpenWorld,
		Title:      "Blank world",
		Content:    content,
	})
	if err != nil {
		t.Fatalf("blank world state: %v", err)
	}
	return state
}
