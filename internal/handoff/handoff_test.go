// SPDX-License-Identifier: Apache-2.0
package handoff

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPermittedActionsNeverIncludeManageOrInvite(t *testing.T) {
	got := permittedActions([]string{"manage", "invite", "derive", "read", "contribute"})
	if strings.Join(got, ",") != "read,contribute,derive" {
		t.Fatalf("permitted actions = %v", got)
	}
}

func TestEventSummaryStripsPayloadAndCapsRunes(t *testing.T) {
	payload := []byte(`{"action":"set_body","body":"secret object content"}`)
	summary := eventSummary("object.state_changed", payload)
	if summary != "set_body" {
		t.Fatalf("summary = %q", summary)
	}
	long := strings.Repeat("x", 400)
	summary = eventSummary("object.state_changed", []byte(`{"action":"`+long+`"}`))
	if len([]rune(summary)) != maxSummaryRunes {
		t.Fatalf("summary length = %d", len([]rune(summary)))
	}
}

func TestTrimStateBodyMarksTruncation(t *testing.T) {
	body := strings.Repeat("b", maxStateBodyBytes+100)
	raw, err := json.Marshal(map[string]any{"title": "t", "body": body})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	trimmed, changed, err := trimStateBody(raw)
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if !changed {
		t.Fatal("large body was not trimmed")
	}
	var decoded map[string]any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		t.Fatalf("decode trimmed state: %v", err)
	}
	value, _ := decoded["body"].(string)
	if len(value) > maxStateBodyBytes+len("\n[truncated]") {
		t.Fatalf("trimmed body length = %d", len(value))
	}
	if !strings.HasSuffix(value, "[truncated]") {
		t.Fatal("truncation marker missing")
	}
}

func TestTrimSharedContextKeepsOnlyTitleAndSummary(t *testing.T) {
	raw := []byte(`{"title":"T","summary":"S","secret":"do not project"}`)
	trimmed, err := trimSharedContext(raw)
	if err != nil {
		t.Fatalf("trim shared: %v", err)
	}
	if strings.Contains(string(trimmed), "secret") {
		t.Fatalf("trimmed shared context = %s", trimmed)
	}
	var decoded map[string]any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		t.Fatalf("decode shared: %v", err)
	}
	if decoded["title"] != "T" || decoded["summary"] != "S" || len(decoded) != 2 {
		t.Fatalf("trimmed shared = %v", decoded)
	}
}

func TestTailEventsKeepsMostRecent(t *testing.T) {
	events := []RecentEvent{{Seq: 1}, {Seq: 2}, {Seq: 3}, {Seq: 4}}
	got := tailEvents(events, 2)
	if len(got) != 2 || got[0].Seq != 3 || got[1].Seq != 4 {
		t.Fatalf("tail events = %v", got)
	}
}
