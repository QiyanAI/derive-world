// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const maxNextRound = 5

// WeeklyBundle is the complete output of one scheduled analysis run.
type WeeklyBundle struct {
	SchemaVersion int             `json:"schema_version"`
	ToolVersion   string          `json:"tool_version"`
	GeneratedFor  string          `json:"generated_for"`
	Repo          string          `json:"repo"`
	Analytics     AnalyticsReport `json:"analytics"`
	Opportunities []Opportunity   `json:"opportunities"`
	Rejected      []Opportunity   `json:"rejected_research"`
	Experiments   *ExperimentFile `json:"experiments,omitempty"`
	NextRound     []Opportunity   `json:"next_round"`
	Stopped       []string        `json:"stopped"`
	NotPerformed  []string        `json:"not_performed"`
}

func weeklyDate(capturedAt string) (string, error) {
	if index := strings.Index(capturedAt, "T"); index == 10 {
		return capturedAt[:10], nil
	}
	return "", fmt.Errorf("snapshot captured_at %q is not RFC3339", capturedAt)
}

// BuildWeekly assembles the weekly report. It is read-only: it never mutates
// the experiment ledger and never posts or drafts anything automatically.
func BuildWeekly(snapshot, before *Snapshot, engagement *Engagement, engagementPath string,
	factsVerified bool, channels *ChannelFile, experiments *ExperimentFile,
	snapshotPath string) (WeeklyBundle, error) {
	date, err := weeklyDate(snapshot.CapturedAt)
	if err != nil {
		return WeeklyBundle{}, err
	}
	analytics := ComputeAnalytics(snapshot, before, engagement)
	items := internalOpportunities(snapshotPath, snapshot, engagementPath, engagement, channels)
	sortOpportunities(items)
	bundle := WeeklyBundle{
		SchemaVersion: 1,
		ToolVersion:   toolVersion,
		GeneratedFor:  date,
		Repo:          snapshot.Repo,
		Analytics:     analytics,
		Opportunities: items,
		Experiments:   experiments,
		NextRound:     []Opportunity{},
		Stopped:       []string{},
		NotPerformed: []string{
			"No model provider was called; all analysis is deterministic.",
			"No external content was posted, messaged or submitted.",
			"No stars, votes or engagement were purchased or manipulated.",
			"Private objects, conversations and Personal AI context were not read.",
		},
	}
	if !factsVerified {
		bundle.NotPerformed = append(bundle.NotPerformed,
			"Product facts did not verify; no content draft should be used from this run.")
	}
	for _, item := range items {
		if (item.Decision == "pursue" || item.Decision == "research") && len(bundle.NextRound) < maxNextRound {
			bundle.NextRound = append(bundle.NextRound, item)
		}
	}
	if experiments != nil {
		for _, experiment := range experiments.Experiments {
			if experiment.Decision != nil && experiment.Decision.Verdict == "stop" {
				bundle.Stopped = append(bundle.Stopped, experiment.ID+" — "+experiment.Title)
			}
		}
	}
	return bundle, nil
}

func RenderWeeklyMarkdown(bundle *WeeklyBundle) string {
	var builder strings.Builder
	builder.WriteString("# Weekly growth report — " + bundle.Repo + "\n\n")
	builder.WriteString("- Report date: " + bundle.GeneratedFor + "\n")
	builder.WriteString("- Snapshot: " + bundle.Analytics.CapturedAt + "\n")
	if bundle.Analytics.BaselineAt != "" {
		builder.WriteString("- Baseline: " + bundle.Analytics.BaselineAt + "\n")
	} else {
		builder.WriteString("- Baseline: none; first reading.\n")
	}
	builder.WriteString("- Engine: growth-engine " + bundle.ToolVersion + " (offline, no model calls)\n\n")

	builder.WriteString("## This week in verified signals\n\n")
	if len(bundle.Analytics.Changes) == 0 {
		builder.WriteString("- No comparison baseline. Treat every count as a point-in-time reading.\n")
	} else {
		for _, change := range bundle.Analytics.Changes {
			builder.WriteString(fmt.Sprintf("- %s: %v → %v (net %v, %s)\n", change.Metric, change.Before, change.After, change.Delta, change.Quality))
		}
	}
	builder.WriteString("\n")
	if summary := bundle.Analytics.Engagement; summary != nil {
		builder.WriteString(fmt.Sprintf("- Meaningful external issues: %d; merged external PRs: %d; showcase submissions: %d; visible external contributors: %d\n\n",
			summary.MeaningfulExternalIssues, summary.MergedExternalPRs, summary.ShowcaseSubmissions, summary.ExternalContributors))
	} else {
		builder.WriteString("- Item-level contribution evidence: UNKNOWN (no engagement export).\n\n")
	}

	builder.WriteString("## Next round (at most five)\n\n")
	if len(bundle.NextRound) == 0 {
		builder.WriteString("- None above the research threshold. Do not invent work to fill the list.\n\n")
	} else {
		for index, item := range bundle.NextRound {
			builder.WriteString(fmt.Sprintf("%d. **%s** (score %d, %s) — %s\n   Source: %s\n", index+1, item.Title, item.Score, item.Decision, item.Action, item.Source))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Experiments\n\n")
	if bundle.Experiments == nil {
		builder.WriteString("- No experiment ledger supplied.\n\n")
	} else {
		for _, experiment := range bundle.Experiments.Experiments {
			status := experiment.Status
			decision := "pending"
			if experiment.Decision != nil {
				decision = experiment.Decision.Verdict
			}
			builder.WriteString(fmt.Sprintf("- %s — %s: status %s, decision %s, metric %s\n",
				experiment.ID, experiment.Title, status, decision, experiment.Metric))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Stop doing\n\n")
	if len(bundle.Stopped) == 0 {
		builder.WriteString("- Nothing is stopped by measured evidence this week.\n\n")
	} else {
		for _, item := range bundle.Stopped {
			builder.WriteString("- " + item + "\n")
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Not performed\n\n")
	for _, item := range bundle.NotPerformed {
		builder.WriteString("- " + item + "\n")
	}
	builder.WriteString("\n## Data quality\n\n")
	builder.WriteString("- traffic: " + bundle.Analytics.TrafficQuality + "\n")
	for _, item := range bundle.Analytics.Unavailable {
		builder.WriteString("- " + item + "\n")
	}
	return builder.String()
}

func runWeekly(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("weekly", stderr)
	snapshotPath := flags.String("snapshot", "", "current snapshot JSON")
	beforePath := flags.String("before", "", "optional baseline snapshot JSON")
	engagementPath := flags.String("engagement", "", "optional engagement JSON")
	factsPath := flags.String("facts", "state/facts.json", "verified product facts")
	factsRoot := flags.String("root", "../..", "repository root for fact checks")
	channelsPath := flags.String("channels", "state/channels.json", "channel registry")
	experimentsPath := flags.String("experiments", "state/experiments.json", "experiment ledger")
	outDir := flags.String("out-dir", "", "output directory (required)")
	force := flags.Bool("force", false, "replace existing report files")
	stopFile := flags.String("stop-file", "", "halt if this file exists")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *snapshotPath == "" || *outDir == "" {
		return errors.New("--snapshot and --out-dir are required")
	}
	if stopRequested(*stopFile) {
		return errors.New("growth engine stop requested")
	}
	snapshot, err := LoadSnapshot(*snapshotPath)
	if err != nil {
		return err
	}
	var before *Snapshot
	if *beforePath != "" {
		before, err = LoadSnapshot(*beforePath)
		if err != nil {
			return err
		}
	}
	var engagement *Engagement
	if *engagementPath != "" {
		engagement, err = LoadEngagement(*engagementPath)
		if err != nil {
			return err
		}
	}
	facts, err := loadFacts(*factsPath)
	if err != nil {
		return err
	}
	factReport := verifyFacts(facts, *factsRoot)
	var channels *ChannelFile
	if *channelsPath != "" {
		channels, err = loadChannels(*channelsPath)
		if err != nil {
			return err
		}
	}
	var experiments *ExperimentFile
	if *experimentsPath != "" {
		experiments, err = loadExperiments(*experimentsPath)
		if err != nil {
			return err
		}
	}
	bundle, err := BuildWeekly(snapshot, before, engagement, *engagementPath, factReport.Verified, channels, experiments, *snapshotPath)
	if err != nil {
		return err
	}
	date := bundle.GeneratedFor
	analyticsJSON, err := json.MarshalIndent(bundle.Analytics, "", "  ")
	if err != nil {
		return err
	}
	bundleJSON, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	type output struct {
		name string
		data []byte
	}
	outputs := []output{
		{date + "-analytics.json", append(analyticsJSON, '\n')},
		{date + "-weekly.json", append(bundleJSON, '\n')},
		{date + "-weekly.md", []byte(redact(RenderWeeklyMarkdown(&bundle)))},
	}
	written := make([]string, 0, len(outputs))
	for _, item := range outputs {
		path := filepath.Join(*outDir, item.name)
		if err := writeFile(path, item.data, *force); err != nil {
			return err
		}
		written = append(written, path)
	}
	fmt.Fprintf(stdout, "wrote %d files for %s\n", len(written), date)
	for _, path := range written {
		fmt.Fprintln(stdout, "- "+path)
	}
	if !factReport.Verified {
		return errors.New("weekly report written with UNVERIFIED product facts; do not use drafts from this run")
	}
	return nil
}
