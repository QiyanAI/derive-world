// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func aiContentFixture(t *testing.T, reply string) (providerPath, budgetPath, usagePath string) {
	t.Helper()
	dir := t.TempDir()
	provider := testProvider("https://provider.example/v1", "openai")
	providerPath = writeJSONTemp(t, dir, "providers.json", ProviderFile{SchemaVersion: 1, Providers: []Provider{provider}})
	budgetPath = writeJSONTemp(t, dir, "budget.json", futureBudget())
	usagePath = filepath.Join(dir, "usage.json")
	t.Setenv("DGE_TEST_KEY", "test-key")
	server := openAIServer(t, func(map[string]any) string { return reply })
	t.Cleanup(server.Close)
	// The provider file points at the mock server.
	provider.BaseURL = server.URL
	providerPath = writeJSONTemp(t, dir, "providers.json", ProviderFile{SchemaVersion: 1, Providers: []Provider{provider}})
	return providerPath, budgetPath, usagePath
}

func TestAIContentProducesVerifiedDraft(t *testing.T) {
	reply := "# Objects with history\n\nDerive World is an open-source foundation for Living Objects. [[fact:what-it-is]]\n\nA derived world is an independent child from an immutable snapshot. [[fact:lineage]]\n\nOnly a human can approve an AI proposal. [[fact:human-approval]]\n"
	providerPath, budgetPath, usagePath := aiContentFixture(t, reply)
	out := filepath.Join(t.TempDir(), "draft.md")
	var stdout, stderr bytes.Buffer
	err := runAIContent([]string{
		"--kind", "technical-intro", "--channel", "github",
		"--providers", providerPath, "--provider", "test-provider",
		"--budget", budgetPath, "--usage", usagePath,
		"--out", out, "--date", "2026-11-08",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}
	content, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := loadFacts("state/facts.json")
	if err != nil {
		t.Fatal(err)
	}
	if report := VerifyDraft(out, string(content), facts); !report.OK {
		t.Fatalf("draft failed verification: %+v", report.Issues)
	}
	if !strings.Contains(string(content), "AI draft: provider=test-provider") {
		t.Fatal("draft metadata is missing")
	}
}

func TestAIContentRejectsUnsupportedClaim(t *testing.T) {
	reply := "# Objects with history\n\nDerive World is production-ready. [[fact:what-it-is]]\n\nA derived world is independent. [[fact:lineage]]\n\nA human approves. [[fact:human-approval]]\n"
	providerPath, budgetPath, usagePath := aiContentFixture(t, reply)
	out := filepath.Join(t.TempDir(), "draft.md")
	var stdout, stderr bytes.Buffer
	err := runAIContent([]string{
		"--kind", "technical-intro", "--channel", "github",
		"--providers", providerPath, "--provider", "test-provider",
		"--budget", budgetPath, "--usage", usagePath,
		"--out", out, "--date", "2026-11-08",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("unsupported claim was accepted")
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Fatal("a rejected AI draft was written")
	}
}
