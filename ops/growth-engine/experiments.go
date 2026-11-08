// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ExperimentFile is the controlled-experiment ledger. Every experiment states
// a hypothesis, one primary metric, a baseline, a decision rule and a window.
// Nothing is recorded as a success without a measured delta.
type ExperimentFile struct {
	SchemaVersion int          `json:"schema_version"`
	Experiments   []Experiment `json:"experiments"`
}

type Experiment struct {
	ID            string              `json:"id"`
	Title         string              `json:"title"`
	Status        string              `json:"status"`
	Hypothesis    string              `json:"hypothesis"`
	Audience      string              `json:"audience"`
	Channel       string              `json:"channel"`
	Change        string              `json:"change"`
	Metric        string              `json:"metric"`
	Baseline      ExperimentBaseline  `json:"baseline"`
	WindowDays    int                 `json:"window_days"`
	DecisionRules ExperimentRules     `json:"decision_rules"`
	StartedAt     string              `json:"started_at,omitempty"`
	CompletedAt   string              `json:"completed_at,omitempty"`
	Results       []ExperimentResult  `json:"results"`
	Decision      *ExperimentDecision `json:"decision,omitempty"`
}

type ExperimentBaseline struct {
	Value      float64 `json:"value"`
	CapturedAt string  `json:"captured_at"`
	Source     string  `json:"source"`
}

type ExperimentRules struct {
	KeepAtLeast   float64 `json:"keep_at_least"`
	StopAtOrBelow float64 `json:"stop_at_or_below"`
}

type ExperimentResult struct {
	EvaluatedAt string  `json:"evaluated_at"`
	SnapshotAt  string  `json:"snapshot_captured_at"`
	Value       float64 `json:"value"`
	Delta       float64 `json:"delta"`
	Note        string  `json:"note"`
}

type ExperimentDecision struct {
	Verdict   string `json:"verdict"`
	DecidedAt string `json:"decided_at"`
	Rationale string `json:"rationale"`
}

var supportedMetrics = map[string]bool{
	"stars": true, "forks": true, "watchers": true, "downloads_total": true,
	"open_issues": true, "open_pull_requests": true, "contributors": true,
	"meaningful_external_issues": true, "merged_external_prs": true,
	"showcase_submissions": true,
}

func loadExperiments(path string) (*ExperimentFile, error) {
	var file ExperimentFile
	if err := readJSON(path, &file); err != nil {
		return nil, err
	}
	if file.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported experiment schema_version %d", path, file.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, experiment := range file.Experiments {
		if experiment.ID == "" {
			return nil, fmt.Errorf("%s: experiment with empty id", path)
		}
		if seen[experiment.ID] {
			return nil, fmt.Errorf("%s: duplicate experiment %q", path, experiment.ID)
		}
		seen[experiment.ID] = true
		switch experiment.Status {
		case "proposed", "running", "completed":
		default:
			return nil, fmt.Errorf("%s: experiment %q has unknown status %q", path, experiment.ID, experiment.Status)
		}
		if !supportedMetrics[experiment.Metric] {
			return nil, fmt.Errorf("%s: experiment %q uses unsupported metric %q", path, experiment.ID, experiment.Metric)
		}
		if experiment.Hypothesis == "" || experiment.Change == "" || experiment.WindowDays <= 0 {
			return nil, fmt.Errorf("%s: experiment %q needs hypothesis, change and a positive window", path, experiment.ID)
		}
		if experiment.DecisionRules.KeepAtLeast <= experiment.DecisionRules.StopAtOrBelow {
			return nil, fmt.Errorf("%s: experiment %q has contradictory decision rules", path, experiment.ID)
		}
	}
	return &file, nil
}

func findExperiment(file *ExperimentFile, id string) (*Experiment, error) {
	for index := range file.Experiments {
		if strings.EqualFold(file.Experiments[index].ID, id) {
			return &file.Experiments[index], nil
		}
	}
	return nil, fmt.Errorf("unknown experiment %q", id)
}

// MetricValue resolves one primary metric. It returns false when the value is
// not observable, so an experiment can never "succeed" on UNKNOWN data.
func MetricValue(metric string, snapshot *Snapshot, engagement *Engagement) (float64, bool) {
	switch metric {
	case "stars":
		return float64(snapshot.Metrics.Stars), true
	case "forks":
		return float64(snapshot.Metrics.Forks), true
	case "watchers":
		return float64(snapshot.Metrics.Watchers), true
	case "downloads_total":
		return float64(totalDownloads(snapshot)), true
	case "open_issues":
		return float64(snapshot.Metrics.OpenIssues), true
	case "open_pull_requests":
		return float64(snapshot.Metrics.OpenPullRequests), true
	case "contributors":
		if snapshot.DataQuality.Contributors == "UNKNOWN" {
			return 0, false
		}
		return float64(snapshot.Metrics.Contributors), true
	case "meaningful_external_issues", "merged_external_prs", "showcase_submissions":
		if engagement == nil {
			return 0, false
		}
		summary := summarizeEngagement(engagement)
		switch metric {
		case "meaningful_external_issues":
			return float64(summary.MeaningfulExternalIssues), true
		case "merged_external_prs":
			return float64(summary.MergedExternalPRs), true
		default:
			return float64(summary.ShowcaseSubmissions), true
		}
	}
	return 0, false
}

// StartExperiment moves a proposed experiment to running. A started
// experiment may only be evaluated inside its declared window.
func StartExperiment(experiment *Experiment, date string) error {
	if experiment.Status != "proposed" {
		return fmt.Errorf("experiment %q is %s; only a proposed experiment can start", experiment.ID, experiment.Status)
	}
	if date == "" {
		return errors.New("a start date is required")
	}
	experiment.Status = "running"
	experiment.StartedAt = date
	return nil
}

// EvaluateExperiment records one measured result and applies the declared
// decision rule. Duplicate evaluations of the same snapshot are refused.
func EvaluateExperiment(experiment *Experiment, snapshot *Snapshot, engagement *Engagement, date string) error {
	if experiment.Status != "running" {
		return fmt.Errorf("experiment %q is %s; start it before evaluating", experiment.ID, experiment.Status)
	}
	if date == "" {
		return errors.New("an evaluation date is required")
	}
	for _, result := range experiment.Results {
		if result.SnapshotAt == snapshot.CapturedAt {
			return fmt.Errorf("experiment %q already evaluated snapshot %s", experiment.ID, snapshot.CapturedAt)
		}
	}
	value, ok := MetricValue(experiment.Metric, snapshot, engagement)
	if !ok {
		return fmt.Errorf("experiment %q metric %q is UNKNOWN in this snapshot; refusing to decide", experiment.ID, experiment.Metric)
	}
	delta := value - experiment.Baseline.Value
	experiment.Results = append(experiment.Results, ExperimentResult{
		EvaluatedAt: date,
		SnapshotAt:  snapshot.CapturedAt,
		Value:       value,
		Delta:       delta,
		Note:        fmt.Sprintf("metric %s measured at %s; delta is against the declared baseline %.2f", experiment.Metric, snapshot.CapturedAt, experiment.Baseline.Value),
	})
	verdict := "extend"
	switch {
	case delta >= experiment.DecisionRules.KeepAtLeast:
		verdict = "keep"
	case delta <= experiment.DecisionRules.StopAtOrBelow:
		verdict = "stop"
	}
	rationale := fmt.Sprintf("delta %.2f; keep at or above %.2f, stop at or below %.2f",
		delta, experiment.DecisionRules.KeepAtLeast, experiment.DecisionRules.StopAtOrBelow)
	experiment.Decision = &ExperimentDecision{Verdict: verdict, DecidedAt: date, Rationale: rationale}
	if verdict == "extend" {
		experiment.Status = "running"
		return nil
	}
	experiment.Status = "completed"
	experiment.CompletedAt = date
	return nil
}

func RenderExperimentsMarkdown(file *ExperimentFile) string {
	var builder strings.Builder
	builder.WriteString("# Growth experiments\n\n")
	for _, experiment := range file.Experiments {
		builder.WriteString("## " + experiment.ID + " — " + experiment.Title + "\n\n")
		builder.WriteString("- Status: " + experiment.Status + "\n")
		builder.WriteString("- Hypothesis: " + experiment.Hypothesis + "\n")
		builder.WriteString("- Audience: " + experiment.Audience + "\n")
		builder.WriteString("- Channel: " + experiment.Channel + "\n")
		builder.WriteString("- Change: " + experiment.Change + "\n")
		builder.WriteString(fmt.Sprintf("- Primary metric: %s; baseline %.2f (%s, %s)\n", experiment.Metric, experiment.Baseline.Value, experiment.Baseline.CapturedAt, experiment.Baseline.Source))
		builder.WriteString(fmt.Sprintf("- Window: %d days; keep at Δ ≥ %.2f, stop at Δ ≤ %.2f\n", experiment.WindowDays, experiment.DecisionRules.KeepAtLeast, experiment.DecisionRules.StopAtOrBelow))
		if len(experiment.Results) == 0 {
			builder.WriteString("- Results: none recorded. No outcome is claimed.\n")
		} else {
			for _, result := range experiment.Results {
				builder.WriteString(fmt.Sprintf("- %s: %s = %.2f (Δ %.2f) against %s\n", result.EvaluatedAt, experiment.Metric, result.Value, result.Delta, result.SnapshotAt))
			}
		}
		if experiment.Decision != nil {
			builder.WriteString("- Decision: " + experiment.Decision.Verdict + " — " + experiment.Decision.Rationale + "\n")
		} else {
			builder.WriteString("- Decision: pending.\n")
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func writeExperimentState(path string, file *ExperimentFile) error {
	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(encoded, '\n'), true)
}

func runExperiments(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: growth-engine experiments <list|start|evaluate> [flags]")
	}
	switch args[0] {
	case "list":
		flags := newFlagSet("experiments list", stderr)
		statePath := flags.String("state", "state/experiments.json", "experiment ledger")
		asJSON := flags.Bool("json", false, "write JSON instead of Markdown")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		file, err := loadExperiments(*statePath)
		if err != nil {
			return err
		}
		if *asJSON {
			encoded, err := json.MarshalIndent(file, "", "  ")
			if err != nil {
				return err
			}
			_, err = stdout.Write(append(encoded, '\n'))
			return err
		}
		_, err = io.WriteString(stdout, RenderExperimentsMarkdown(file))
		return err
	case "start":
		flags := newFlagSet("experiments start", stderr)
		statePath := flags.String("state", "state/experiments.json", "experiment ledger")
		id := flags.String("id", "", "experiment id")
		date := flags.String("date", "", "start date (YYYY-MM-DD)")
		write := flags.Bool("write", false, "write the updated ledger")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" || *date == "" {
			return errors.New("--id and --date are required")
		}
		file, err := loadExperiments(*statePath)
		if err != nil {
			return err
		}
		experiment, err := findExperiment(file, *id)
		if err != nil {
			return err
		}
		if err := StartExperiment(experiment, *date); err != nil {
			return err
		}
		if *write {
			if err := writeExperimentState(*statePath, file); err != nil {
				return err
			}
		}
		fmt.Fprintf(stdout, "experiment %s started on %s (written=%t)\n", experiment.ID, *date, *write)
		return nil
	case "evaluate":
		flags := newFlagSet("experiments evaluate", stderr)
		statePath := flags.String("state", "state/experiments.json", "experiment ledger")
		id := flags.String("id", "", "experiment id")
		snapshotPath := flags.String("snapshot", "", "current snapshot JSON")
		engagementPath := flags.String("engagement", "", "optional engagement JSON")
		date := flags.String("date", "", "evaluation date (YYYY-MM-DD)")
		write := flags.Bool("write", false, "write the updated ledger")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" || *snapshotPath == "" || *date == "" {
			return errors.New("--id, --snapshot and --date are required")
		}
		file, err := loadExperiments(*statePath)
		if err != nil {
			return err
		}
		experiment, err := findExperiment(file, *id)
		if err != nil {
			return err
		}
		snapshot, err := LoadSnapshot(*snapshotPath)
		if err != nil {
			return err
		}
		var engagement *Engagement
		if *engagementPath != "" {
			engagement, err = LoadEngagement(*engagementPath)
			if err != nil {
				return err
			}
		}
		if err := EvaluateExperiment(experiment, snapshot, engagement, *date); err != nil {
			return err
		}
		if *write {
			if err := writeExperimentState(*statePath, file); err != nil {
				return err
			}
		}
		verdict := "pending"
		if experiment.Decision != nil {
			verdict = experiment.Decision.Verdict
		}
		fmt.Fprintf(stdout, "experiment %s: verdict=%s status=%s (written=%t)\n", experiment.ID, verdict, experiment.Status, *write)
		return nil
	case "tick":
		flags := newFlagSet("experiments tick", stderr)
		statePath := flags.String("state", "state/experiments.json", "experiment ledger")
		snapshotPath := flags.String("snapshot", "", "current snapshot JSON")
		engagementPath := flags.String("engagement", "", "optional engagement JSON")
		date := flags.String("date", "", "evaluation date (YYYY-MM-DD)")
		write := flags.Bool("write", false, "write accepted decisions")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *snapshotPath == "" || *date == "" {
			return errors.New("--snapshot and --date are required")
		}
		file, err := loadExperiments(*statePath)
		if err != nil {
			return err
		}
		snapshot, err := LoadSnapshot(*snapshotPath)
		if err != nil {
			return err
		}
		var engagement *Engagement
		if *engagementPath != "" {
			engagement, err = LoadEngagement(*engagementPath)
			if err != nil {
				return err
			}
		}
		decided := 0
		skipped := []string{}
		for index := range file.Experiments {
			experiment := &file.Experiments[index]
			if experiment.Status != "running" {
				continue
			}
			if err := EvaluateExperiment(experiment, snapshot, engagement, *date); err != nil {
				skipped = append(skipped, experiment.ID+": "+redact(err.Error()))
				continue
			}
			decided++
		}
		if *write && decided > 0 {
			if err := writeExperimentState(*statePath, file); err != nil {
				return err
			}
		}
		for _, item := range skipped {
			fmt.Fprintln(stderr, "skipped "+item)
		}
		fmt.Fprintf(stdout, "evaluated %d running experiment(s) (written=%t)\n", decided, *write)
		return nil
	default:
		return fmt.Errorf("unknown experiments subcommand %q", args[0])
	}
}
