// SPDX-License-Identifier: Apache-2.0
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWeeklyIsReadOnlyAndSourced(t *testing.T) {
	before := snapshotFixture("2026-11-01T00:00:00Z", 0, 0, 0, 1, 0)
	after := snapshotFixture("2026-11-08T00:00:00Z", 1, 0, 0, 1, 4)
	channels, err := loadChannels("state/channels.json")
	if err != nil {
		t.Fatal(err)
	}
	experiments, err := loadExperiments("state/experiments.json")
	if err != nil {
		t.Fatal(err)
	}
	original := *experiments
	bundle, err := BuildWeekly(after, before, nil, "", true, channels, experiments, "snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.GeneratedFor != "2026-11-08" {
		t.Fatalf("date = %q", bundle.GeneratedFor)
	}
	if len(bundle.NextRound) > maxNextRound {
		t.Fatalf("next round has %d items", len(bundle.NextRound))
	}
	if len(bundle.NotPerformed) < 3 {
		t.Fatalf("not-performed list = %v", bundle.NotPerformed)
	}
	for _, item := range bundle.NextRound {
		if item.Source == "" {
			t.Fatalf("next-round item %s has no source", item.ID)
		}
	}
	if len(experiments.Experiments) != len(original.Experiments) {
		t.Fatal("weekly analysis mutated the experiment ledger")
	}
	markdown := RenderWeeklyMarkdown(&bundle)
	for _, want := range []string{"traffic: UNKNOWN", "No external content was posted", "Baseline: 2026-11-01"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("weekly report missing %q", want)
		}
	}
}

func TestWriteFileRefusesOverwriteUnlessForced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	if err := writeFile(path, []byte("one"), false); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, []byte("two"), false); err == nil {
		t.Fatal("existing report was overwritten")
	}
	if err := writeFile(path, []byte("two"), true); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "two" {
		t.Fatalf("content = %q", content)
	}
}

func TestStopSwitch(t *testing.T) {
	t.Setenv("DGE_STOP", "")
	if stopRequested("") {
		t.Fatal("stop requested without a signal")
	}
	t.Setenv("DGE_STOP", "1")
	if !stopRequested("") {
		t.Fatal("DGE_STOP=1 did not stop the run")
	}
	t.Setenv("DGE_STOP", "")
	stopFile := filepath.Join(t.TempDir(), "STOP")
	if stopRequested(stopFile) {
		t.Fatal("a missing stop file stopped the run")
	}
	if err := os.WriteFile(stopFile, []byte("stop"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !stopRequested(stopFile) {
		t.Fatal("a present stop file did not stop the run")
	}
}

func TestWeeklyRejectsInvalidCapturedAt(t *testing.T) {
	snapshot := snapshotFixture("yesterday", 0, 0, 0, 1, 0)
	if _, err := BuildWeekly(snapshot, nil, nil, "", true, nil, nil, "snapshot.json"); err == nil {
		t.Fatal("an invalid captured_at was accepted")
	}
}
