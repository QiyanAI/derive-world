// SPDX-License-Identifier: Apache-2.0
package world

import "testing"

func TestRoleFromPermissions(t *testing.T) {
	cases := []struct {
		permissions []string
		want        string
	}{
		{[]string{"read", "contribute", "derive", "invite", "manage"}, "owner"},
		{[]string{"read", "contribute", "derive"}, "editor"},
		{[]string{"read"}, "viewer"},
		{[]string{"read", "invite"}, "custom"},
	}
	for _, testCase := range cases {
		if got := roleFromPermissions(testCase.permissions); got != testCase.want {
			t.Fatalf("role(%v) = %s, want %s", testCase.permissions, got, testCase.want)
		}
	}
}

func TestSummarizeUsesRealEventNames(t *testing.T) {
	if got := summarize("object.state_changed", "add_element"); got != "add_element" {
		t.Fatalf("summarize = %s", got)
	}
	if got := summarize("object.created", ""); got != "created" {
		t.Fatalf("summarize created = %s", got)
	}
}
