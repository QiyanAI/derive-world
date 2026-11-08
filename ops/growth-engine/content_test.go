// SPDX-License-Identifier: Apache-2.0
package main

import (
	"strings"
	"testing"
)

func TestEveryDraftGeneratesAndVerifies(t *testing.T) {
	facts, err := loadFacts("state/facts.json")
	if err != nil {
		t.Fatal(err)
	}
	channels, err := loadChannels("state/channels.json")
	if err != nil {
		t.Fatal(err)
	}
	github, ok := findChannel(channels, "github")
	if !ok {
		t.Fatal("github channel missing")
	}
	for kind := range contentKinds {
		draft, err := GenerateDraft(kind, facts, github)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		report := VerifyDraft(kind, draft, facts)
		if !report.OK {
			t.Fatalf("%s failed verification: %+v", kind, report.Issues)
		}
		if len(report.FactsUsed) < 3 {
			t.Fatalf("%s cites %d facts", kind, len(report.FactsUsed))
		}
		if !strings.Contains(draft, "not published") {
			t.Fatalf("%s is missing the publication notice", kind)
		}
	}
}

// syntheticToken builds a secret-shaped string at runtime so the test source
// never contains a literal token pattern that the history secret scan flags.
func syntheticToken(prefix string) string {
	return prefix + strings.Repeat("a", 26)
}

func TestVerifierRejectsUnsupportedClaims(t *testing.T) {
	facts, err := loadFacts("state/facts.json")
	if err != nil {
		t.Fatal(err)
	}
	base := "<!-- Prepared draft — not published. -->\n\nA [[fact:what-it-is]] B [[fact:lineage]] C [[fact:human-approval]]\n"
	cases := map[string]string{
		"production-ready": "This is production-ready.",
		"user count":       "We have 1200 users.",
		"funding":          "We are funded.",
		"federation":       "This supports cross-instance federation.",
		"publication":      "We posted this on Hacker News.",
		"external URL":     "See https://example.com/launch",
		"secret":           "token " + syntheticToken("dw_pat_"),
		"real ai":          "This is real AI.",
	}
	for name, line := range cases {
		report := VerifyDraft(name, base+line+"\n", facts)
		if report.OK {
			t.Fatalf("%s was accepted", name)
		}
	}
	negative := base + "This is not a production platform, and there is no cross-instance federation.\n"
	if report := VerifyDraft("negative", negative, facts); !report.OK {
		t.Fatalf("a true negative claim was rejected: %+v", report.Issues)
	}
}

func TestVerifierRequiresKnownFactsAndNotice(t *testing.T) {
	facts, err := loadFacts("state/facts.json")
	if err != nil {
		t.Fatal(err)
	}
	report := VerifyDraft("bad", "A [fact:does-not-exist] B [fact:what-it-is] C [fact:lineage]\n", facts)
	if report.OK {
		t.Fatal("unknown fact marker accepted")
	}
	report = VerifyDraft("bad2", "not published, but no markers at all", facts)
	if report.OK {
		t.Fatal("a draft with no fact markers accepted")
	}
}

func TestRedaction(t *testing.T) {
	value := redact("token " + syntheticToken("dw_pat_") + " and " + syntheticToken("ghp_"))
	if strings.Contains(value, "dw_pat_") || strings.Contains(value, "ghp_") {
		t.Fatalf("secret survived redaction: %s", value)
	}
}
