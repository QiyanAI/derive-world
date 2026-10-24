// SPDX-License-Identifier: Apache-2.0
package objects

import "testing"

// C-G04-04 widens only object.state_changed. This unit test pins the Go-side
// scope; the PostgreSQL CHECK of the same shape is exercised by
// TestG04ObjectEventPayloadCheckBoundaries in the integration suite.
func TestEventPayloadLimitScope(t *testing.T) {
	if got := eventPayloadLimit("object.state_changed"); got != 132352 {
		t.Fatalf("object.state_changed limit = %d, want 132352", got)
	}
	for _, eventType := range []string{
		"object.created", "object.member_added", "object.member_removed",
		"object.invitation_created", "object.invitation_revoked",
		"object.derived", "object.deleted",
		"ai.proposal_created", "ai.proposal_applied",
	} {
		if got := eventPayloadLimit(eventType); got != 96*1024 {
			t.Errorf("%s limit = %d, want %d", eventType, got, 96*1024)
		}
	}
}

// The guard is exercised at its own boundary: compact size 132,352 is accepted
// for object.state_changed, 132,353 is rejected, while every other event keeps
// the 98,304 boundary.
func TestExceedsEventPayloadLimitBoundary(t *testing.T) {
	cases := []struct {
		eventType string
		size      int
		want      bool
	}{
		{"object.state_changed", 132351, false},
		{"object.state_changed", 132352, false},
		{"object.state_changed", 132353, true},
		{"object.created", 98304, false},
		{"object.created", 98305, true},
	}
	for _, item := range cases {
		if got := exceedsEventPayloadLimit(item.eventType, item.size); got != item.want {
			t.Errorf("exceedsEventPayloadLimit(%q, %d) = %v, want %v",
				item.eventType, item.size, got, item.want)
		}
	}
}
