// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const strategyReply = `{"summary":"Make the measurement gap the first priority.","actions":[{"action":"Add a fine-grained metrics token","rationale":"Traffic is UNKNOWN, so no channel can be compared.","evidence":["OBS-001"],"horizon_days":7,"risk":"Low: read-only permission."}],"stop":[],"next_experiments":[]}`

func pipelineArgs(t *testing.T, dir string) []string {
	t.Helper()
	return []string{
		"--snapshot", "baseline/snapshot-2026-11-07.json",
		"--facts", "state/facts.json",
		"--sources", "state/sources.json",
		"--channels", "state/channels.json",
		"--experiments", "state/experiments.json",
		"--out-dir", dir,
	}
}

func TestPipelineDeterministicPlanMakesNoModelCall(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := runPipeline(pipelineArgs(t, dir), &stdout, &stderr); err != nil {
		t.Fatalf("pipeline: %v\nstderr=%s", err, stderr.String())
	}
	manifest := loadManifest(t, filepath.Join(dir, "2026-11-07-pipeline.json"))
	if manifest.Strategy != nil {
		t.Fatal("deterministic pipeline produced an AI strategy")
	}
	if manifest.Usage != nil {
		t.Fatal("deterministic pipeline recorded model usage")
	}
	foundSkipped := false
	for _, stage := range manifest.Stages {
		if stage.Name == "strategy" && stage.Status == "skipped" {
			foundSkipped = true
		}
	}
	if !foundSkipped {
		t.Fatalf("stages = %+v", manifest.Stages)
	}
	if !strings.Contains(strings.Join(manifest.NotPerformed, " "), "No external content was published") {
		t.Fatalf("not performed = %v", manifest.NotPerformed)
	}
}

func TestPipelineAIPlanUsesBudgetAndStaysReviewOnly(t *testing.T) {
	server := openAIServer(t, func(map[string]any) string { return strategyReply })
	defer server.Close()
	dir := t.TempDir()
	provider := testProvider(server.URL, "openai")
	providerPath := writeJSONTemp(t, dir, "providers.json", ProviderFile{SchemaVersion: 1, Providers: []Provider{provider}})
	budgetPath := writeJSONTemp(t, dir, "budget.json", futureBudget())
	usagePath := filepath.Join(dir, "usage.json")
	t.Setenv("DGE_TEST_KEY", "test-key")
	args := append(pipelineArgs(t, dir),
		"--providers", providerPath, "--provider", "test-provider",
		"--budget", budgetPath, "--usage", usagePath)
	var stdout, stderr bytes.Buffer
	if err := runPipeline(args, &stdout, &stderr); err != nil {
		t.Fatalf("pipeline: %v\nstderr=%s", err, stderr.String())
	}
	manifest := loadManifest(t, filepath.Join(dir, "2026-11-07-pipeline.json"))
	if manifest.Strategy == nil || len(manifest.Strategy.Actions) != 1 {
		t.Fatalf("strategy = %+v", manifest.Strategy)
	}
	if manifest.Usage == nil || manifest.Usage.Calls != 1 {
		t.Fatalf("usage = %+v", manifest.Usage)
	}
	usage, err := loadUsage(usagePath)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Calls != 1 {
		t.Fatalf("ledger calls = %d", usage.Calls)
	}
	for _, stage := range manifest.Stages {
		if stage.Name == "publish" && stage.Status != "skipped" {
			t.Fatalf("publish stage = %+v", stage)
		}
	}
}

func TestPipelineRejectsUnknownStrategyEvidence(t *testing.T) {
	reply := `{"summary":"x","actions":[{"action":"a","rationale":"r","evidence":["NOPE"],"horizon_days":1,"risk":"l"}],"stop":[],"next_experiments":[]}`
	server := openAIServer(t, func(map[string]any) string { return reply })
	defer server.Close()
	dir := t.TempDir()
	provider := testProvider(server.URL, "openai")
	providerPath := writeJSONTemp(t, dir, "providers.json", ProviderFile{SchemaVersion: 1, Providers: []Provider{provider}})
	budgetPath := writeJSONTemp(t, dir, "budget.json", futureBudget())
	t.Setenv("DGE_TEST_KEY", "test-key")
	args := append(pipelineArgs(t, dir),
		"--providers", providerPath, "--provider", "test-provider",
		"--budget", budgetPath, "--usage", filepath.Join(dir, "usage.json"))
	var stdout, stderr bytes.Buffer
	if err := runPipeline(args, &stdout, &stderr); err == nil {
		t.Fatal("pipeline accepted evidence that does not exist")
	}
	manifest := loadManifest(t, filepath.Join(dir, "2026-11-07-pipeline.json"))
	foundError := false
	for _, stage := range manifest.Stages {
		if stage.Name == "strategy" && stage.Status == "error" {
			foundError = true
		}
	}
	if !foundError {
		t.Fatalf("stages = %+v", manifest.Stages)
	}
}

func TestPipelineStopSwitch(t *testing.T) {
	dir := t.TempDir()
	stop := filepath.Join(dir, "STOP")
	if err := os.WriteFile(stop, []byte("stop"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := append(pipelineArgs(t, dir), "--stop-file", stop)
	if err := runPipeline(args, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "stop") {
		t.Fatalf("stop error = %v", err)
	}
}

func TestValidateStrategyBounds(t *testing.T) {
	opportunities := []Opportunity{{ID: "OBS-001"}}
	experiments := &ExperimentFile{SchemaVersion: 1, Experiments: []Experiment{{ID: "EXP-01", Status: "proposed"}}}
	plan := StrategyPlan{Summary: "x", Actions: []StrategyAction{{
		Action: "a", Rationale: "r", Evidence: []string{"OBS-001"}, HorizonDays: 7, Risk: "l",
	}}}
	if err := validateStrategy(&plan, opportunities, experiments); err != nil {
		t.Fatal(err)
	}
	plan.Stop = []string{"EXP-01"}
	if err := validateStrategy(&plan, opportunities, experiments); err == nil {
		t.Fatal("stopping a non-running experiment was accepted")
	}
}

func loadManifest(t *testing.T, path string) PipelineManifest {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest PipelineManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}
