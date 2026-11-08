// SPDX-License-Identifier: Apache-2.0
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryFactsVerify(t *testing.T) {
	facts, err := loadFacts("state/facts.json")
	if err != nil {
		t.Fatalf("load facts: %v", err)
	}
	report := verifyFacts(facts, "../..")
	if !report.Verified {
		for _, status := range report.Facts {
			for _, detail := range status.Errors {
				t.Logf("%s: %s", status.ID, detail)
			}
		}
		t.Fatal("repository facts do not verify")
	}
	if report.Checked != len(facts.Facts) {
		t.Fatalf("checked %d of %d facts", report.Checked, len(facts.Facts))
	}
}

func TestFactsDetectMissingTextAndEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("a real sentence"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := &FactFile{SchemaVersion: 1, Facts: []Fact{
		{ID: "missing-text", Statement: "x", Sources: []FactSource{{Path: "README.md", Contains: "not there"}}},
		{ID: "escape", Statement: "x", Sources: []FactSource{{Path: "../secret", Contains: "a"}}},
	}}
	report := verifyFacts(file, root)
	if report.Verified {
		t.Fatal("tampered facts verified")
	}
	if report.Facts[0].OK || report.Facts[1].OK {
		t.Fatalf("statuses = %+v", report.Facts)
	}
}

func TestLoadFactsRejectsDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "facts.json")
	body := `{"schema_version":1,"facts":[
		{"id":"a","statement":"a","sources":[{"path":"README.md","contains":"x"}]},
		{"id":"a","statement":"a","sources":[{"path":"README.md","contains":"x"}]}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFacts(path); err == nil {
		t.Fatal("duplicate fact ids accepted")
	}
}
