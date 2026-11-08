// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"testing"
)

func experimentFixture() *Experiment {
	return &Experiment{
		ID:         "EXP-T",
		Title:      "test",
		Status:     "proposed",
		Hypothesis: "h",
		Audience:   "a",
		Channel:    "github",
		Change:     "c",
		Metric:     "stars",
		Baseline:   ExperimentBaseline{Value: 0, CapturedAt: "2026-11-07T00:00:00Z", Source: "snapshot"},
		WindowDays: 14,
		DecisionRules: ExperimentRules{
			KeepAtLeast: 1, StopAtOrBelow: -1,
		},
		Results: []ExperimentResult{},
	}
}

func TestExperimentLifecycleKeep(t *testing.T) {
	experiment := experimentFixture()
	if err := EvaluateExperiment(experiment, snapshotFixture("2026-11-08T00:00:00Z", 1, 0, 0, 1, 0), nil, "2026-11-08"); err == nil {
		t.Fatal("a proposed experiment was evaluated")
	}
	if err := StartExperiment(experiment, "2026-11-07"); err != nil {
		t.Fatal(err)
	}
	if experiment.Status != "running" {
		t.Fatalf("status = %s", experiment.Status)
	}
	if err := EvaluateExperiment(experiment, snapshotFixture("2026-11-08T00:00:00Z", 2, 0, 0, 1, 0), nil, "2026-11-08"); err != nil {
		t.Fatal(err)
	}
	if experiment.Status != "completed" || experiment.Decision == nil || experiment.Decision.Verdict != "keep" {
		t.Fatalf("experiment = %+v", experiment)
	}
	if len(experiment.Results) != 1 || experiment.Results[0].Delta != 2 {
		t.Fatalf("results = %+v", experiment.Results)
	}
}

func TestExperimentDuplicateEvaluationRefused(t *testing.T) {
	experiment := experimentFixture()
	if err := StartExperiment(experiment, "2026-11-07"); err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotFixture("2026-11-08T00:00:00Z", 2, 0, 0, 1, 0)
	if err := EvaluateExperiment(experiment, snapshot, nil, "2026-11-08"); err != nil {
		t.Fatal(err)
	}
	if err := EvaluateExperiment(experiment, snapshot, nil, "2026-11-09"); err == nil {
		t.Fatal("duplicate snapshot evaluation accepted")
	}
}

func TestExperimentUnknownMetricRefusesDecision(t *testing.T) {
	experiment := experimentFixture()
	experiment.Metric = "contributors"
	if err := StartExperiment(experiment, "2026-11-07"); err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotFixture("2026-11-08T00:00:00Z", 2, 0, 0, 0, 0)
	snapshot.DataQuality.Contributors = "UNKNOWN"
	if err := EvaluateExperiment(experiment, snapshot, nil, "2026-11-08"); err == nil {
		t.Fatal("a decision was made on UNKNOWN data")
	}
	if experiment.Decision != nil || experiment.Status != "running" {
		t.Fatalf("experiment mutated: %+v", experiment)
	}
}

func TestTickAutomaticallyCompletesRunningExperiments(t *testing.T) {
	dir := t.TempDir()
	file, err := loadExperiments("state/experiments.json")
	if err != nil {
		t.Fatal(err)
	}
	file.Experiments[0].Status = "running"
	file.Experiments[0].StartedAt = "2026-11-08"
	statePath := dir + "/experiments.json"
	if err := writeFile(statePath, mustJSON(t, file), false); err != nil {
		t.Fatal(err)
	}
	snapshot := &Snapshot{SchemaVersion: 1, CapturedAt: "2026-11-20T00:00:00Z", Repo: "QiyanAI/derive-world", Visibility: "public"}
	snapshot.Metrics.Stars = 5
	snapshot.DataQuality = DataQuality{Traffic: "UNKNOWN", Contributors: "VERIFIED", Notes: []string{}}
	snapshotPath := writeJSONTemp(t, dir, "snapshot.json", snapshot)
	var stdout, stderr bytes.Buffer
	err = runExperiments([]string{
		"tick", "--state", statePath, "--snapshot", snapshotPath,
		"--date", "2026-11-20", "--write",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	updated, err := loadExperiments(statePath)
	if err != nil {
		t.Fatal(err)
	}
	first := updated.Experiments[0]
	if first.Status != "completed" || first.Decision == nil || first.Decision.Verdict != "keep" {
		t.Fatalf("experiment = %+v", first)
	}
}

func TestExperimentExtendKeepsRunning(t *testing.T) {
	experiment := experimentFixture()
	if err := StartExperiment(experiment, "2026-11-07"); err != nil {
		t.Fatal(err)
	}
	if err := EvaluateExperiment(experiment, snapshotFixture("2026-11-08T00:00:00Z", 0, 0, 0, 1, 0), nil, "2026-11-08"); err != nil {
		t.Fatal(err)
	}
	if experiment.Status != "running" || experiment.Decision == nil || experiment.Decision.Verdict != "extend" {
		t.Fatalf("experiment = %+v", experiment)
	}
}
