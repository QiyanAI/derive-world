// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Snapshot is the read-only schema-v1 document produced by
// ops/github-metrics. Growth analytics never re-collects these numbers; it
// derives net change and interpretation from the snapshots it is given.
type Snapshot struct {
	SchemaVersion int    `json:"schema_version"`
	CapturedAt    string `json:"captured_at"`
	Repo          string `json:"repo"`
	Visibility    string `json:"visibility"`
	Metrics       struct {
		Stars            int       `json:"stars"`
		Forks            int       `json:"forks"`
		Watchers         int       `json:"watchers"`
		OpenIssues       int       `json:"open_issues"`
		OpenPullRequests int       `json:"open_pull_requests"`
		Contributors     int       `json:"contributors"`
		Releases         []Release `json:"releases"`
		Traffic          *Traffic  `json:"traffic"`
	} `json:"metrics"`
	DataQuality DataQuality `json:"data_quality"`
	API         APIInfo     `json:"api"`
}

type Release struct {
	Tag         string  `json:"tag"`
	PublishedAt string  `json:"published_at"`
	Assets      []Asset `json:"assets"`
}

type Asset struct {
	Name          string `json:"name"`
	DownloadCount int    `json:"download_count"`
}

type Traffic struct {
	Views14d  TrafficCount `json:"views_14d"`
	Clones14d TrafficCount `json:"clones_14d"`
}

type TrafficCount struct {
	Count   int `json:"count"`
	Uniques int `json:"uniques"`
}

type DataQuality struct {
	Traffic      string   `json:"traffic"`
	Contributors string   `json:"contributors"`
	Notes        []string `json:"notes"`
}

type APIInfo struct {
	RateLimitRemaining int `json:"rate_limit_remaining"`
}

// Engagement is an optional, item-level export of public issues and pull
// requests. It is produced outside this tool (for example with gh api) and is
// never fabricated here. The schema is documented in docs/GROW-03-OPERATOR.md.
type Engagement struct {
	SchemaVersion int              `json:"schema_version"`
	CapturedAt    string           `json:"captured_at"`
	Issues        []EngagementItem `json:"issues"`
	PullRequests  []EngagementPR   `json:"pull_requests"`
}

type EngagementItem struct {
	Number            int      `json:"number"`
	Title             string   `json:"title"`
	Author            string   `json:"author"`
	AuthorAssociation string   `json:"author_association"`
	State             string   `json:"state"`
	CreatedAt         string   `json:"created_at"`
	Comments          int      `json:"comments"`
	BodyLength        int      `json:"body_length"`
	Labels            []string `json:"labels"`
}

type EngagementPR struct {
	Number            int      `json:"number"`
	Title             string   `json:"title"`
	Author            string   `json:"author"`
	AuthorAssociation string   `json:"author_association"`
	State             string   `json:"state"`
	Merged            bool     `json:"merged"`
	CreatedAt         string   `json:"created_at"`
	BodyLength        int      `json:"body_length"`
	Labels            []string `json:"labels"`
}

// MetricChange is one comparable growth metric. Quality says whether the value
// is VERIFIED, DERIVED, or UNKNOWN; an unknown value is never estimated.
type MetricChange struct {
	Metric  string `json:"metric"`
	Before  any    `json:"before"`
	After   any    `json:"after"`
	Delta   any    `json:"delta"`
	Quality string `json:"quality"`
}

// EngagementSummary is item-level, public contribution evidence.
type EngagementSummary struct {
	CapturedAt               string   `json:"captured_at"`
	MeaningfulExternalIssues int      `json:"meaningful_external_issues"`
	MergedExternalPRs        int      `json:"merged_external_pull_requests"`
	ShowcaseSubmissions      int      `json:"showcase_submissions"`
	ShowcaseIssueNumbers     []int    `json:"showcase_issue_numbers"`
	ExternalContributors     int      `json:"external_contributors"`
	Unavailable              []string `json:"unavailable"`
}

// AnalyticsReport is the complete growth analysis.
type AnalyticsReport struct {
	SchemaVersion  int                `json:"schema_version"`
	ToolVersion    string             `json:"tool_version"`
	Repo           string             `json:"repo"`
	Visibility     string             `json:"visibility"`
	CapturedAt     string             `json:"captured_at"`
	BaselineAt     string             `json:"baseline_captured_at,omitempty"`
	Changes        []MetricChange     `json:"changes"`
	OpenIssues     int                `json:"open_issues"`
	OpenPRs        int                `json:"open_pull_requests"`
	Releases       []Release          `json:"releases"`
	Downloads      int                `json:"downloads_total"`
	Engagement     *EngagementSummary `json:"engagement,omitempty"`
	TrafficQuality string             `json:"traffic_quality"`
	Unavailable    []string           `json:"unavailable"`
	Caveats        []string           `json:"caveats"`
}

func totalDownloads(snapshot *Snapshot) int {
	total := 0
	for _, release := range snapshot.Metrics.Releases {
		for _, asset := range release.Assets {
			total += asset.DownloadCount
		}
	}
	return total
}

func change(metric string, before, after int, quality string) MetricChange {
	return MetricChange{Metric: metric, Before: before, After: after, Delta: after - before, Quality: quality}
}

// ComputeAnalytics derives net growth from snapshots. It deliberately keeps
// public signals (stars, forks, downloads) separate from active-user claims,
// which this data cannot support.
func ComputeAnalytics(snapshot, before *Snapshot, engagement *Engagement) AnalyticsReport {
	report := AnalyticsReport{
		SchemaVersion:  1,
		ToolVersion:    toolVersion,
		Repo:           snapshot.Repo,
		Visibility:     snapshot.Visibility,
		CapturedAt:     snapshot.CapturedAt,
		OpenIssues:     snapshot.Metrics.OpenIssues,
		OpenPRs:        snapshot.Metrics.OpenPullRequests,
		Releases:       snapshot.Metrics.Releases,
		Downloads:      totalDownloads(snapshot),
		TrafficQuality: snapshot.DataQuality.Traffic,
		Unavailable:    []string{},
		Caveats: []string{
			"Stars, forks, watchers and downloads are public signals, not active users or adoption.",
			"Maintainer and CI downloads cannot be excluded from public download totals.",
			"An absent value is UNKNOWN; it is never estimated.",
			"Correlation in a short window is not causation; every experiment needs a stated counterfactual.",
		},
	}
	if before != nil {
		report.BaselineAt = before.CapturedAt
		report.Changes = append(report.Changes,
			change("stars", before.Metrics.Stars, snapshot.Metrics.Stars, "VERIFIED"),
			change("forks", before.Metrics.Forks, snapshot.Metrics.Forks, "VERIFIED"),
			change("watchers", before.Metrics.Watchers, snapshot.Metrics.Watchers, "VERIFIED"),
			change("downloads_total", totalDownloads(before), totalDownloads(snapshot), "DERIVED"),
			change("releases", len(before.Metrics.Releases), len(snapshot.Metrics.Releases), "VERIFIED"),
		)
		if before.DataQuality.Contributors == "UNKNOWN" || snapshot.DataQuality.Contributors == "UNKNOWN" {
			report.Changes = append(report.Changes, MetricChange{Metric: "contributors", Before: "UNKNOWN", After: "UNKNOWN", Delta: "UNKNOWN", Quality: "UNKNOWN"})
		} else {
			report.Changes = append(report.Changes, change("contributors", before.Metrics.Contributors, snapshot.Metrics.Contributors, "VERIFIED"))
		}
		if before.Metrics.Traffic == nil || snapshot.Metrics.Traffic == nil ||
			before.DataQuality.Traffic == "UNKNOWN" || snapshot.DataQuality.Traffic == "UNKNOWN" {
			report.Changes = append(report.Changes,
				MetricChange{Metric: "traffic.views_14d", Before: "UNKNOWN", After: "UNKNOWN", Delta: "UNKNOWN", Quality: "UNKNOWN"},
				MetricChange{Metric: "traffic.clones_14d", Before: "UNKNOWN", After: "UNKNOWN", Delta: "UNKNOWN", Quality: "UNKNOWN"},
			)
		} else {
			report.Changes = append(report.Changes,
				change("traffic.views_14d", before.Metrics.Traffic.Views14d.Count, snapshot.Metrics.Traffic.Views14d.Count, "VERIFIED"),
				change("traffic.clones_14d", before.Metrics.Traffic.Clones14d.Count, snapshot.Metrics.Traffic.Clones14d.Count, "VERIFIED"),
			)
		}
	}
	if snapshot.DataQuality.Traffic == "UNKNOWN" || snapshot.Metrics.Traffic == nil {
		report.Unavailable = append(report.Unavailable,
			"traffic views and clones: UNKNOWN (requires repository administration read)")
	}
	if snapshot.DataQuality.Contributors == "UNKNOWN" {
		report.Unavailable = append(report.Unavailable, "contributors: UNKNOWN")
	}
	if engagement == nil {
		report.Unavailable = append(report.Unavailable,
			"item-level issues and pull requests: UNKNOWN (no engagement file supplied)")
	} else {
		report.Engagement = summarizeEngagement(engagement)
	}
	return report
}

var noisyLabels = map[string]bool{
	"spam": true, "invalid": true, "duplicate": true, "wontfix": true, "question": true,
}

func hasNoisyLabel(labels []string) bool {
	for _, label := range labels {
		if noisyLabels[strings.ToLower(strings.TrimSpace(label))] {
			return true
		}
	}
	return false
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), want) {
			return true
		}
	}
	return false
}

func externalAssociation(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "OWNER", "MEMBER", "COLLABORATOR":
		return false
	default:
		return true
	}
}

// meaningfulIssue defines a contribution that carries information: a real
// report or a conversation, not a one-line drive-by. Showcase submissions are
// meaningful regardless of length because they are the intended participation
// path.
func meaningfulIssue(item EngagementItem) bool {
	if hasNoisyLabel(item.Labels) {
		return false
	}
	if hasLabel(item.Labels, "showcase") {
		return true
	}
	return item.BodyLength >= 120 || item.Comments >= 2
}

func mergedExternalPR(pr EngagementPR) bool {
	return pr.Merged && pr.State == "closed" && !hasNoisyLabel(pr.Labels) && externalAssociation(pr.AuthorAssociation)
}

func summarizeEngagement(engagement *Engagement) *EngagementSummary {
	summary := &EngagementSummary{
		CapturedAt:           engagement.CapturedAt,
		ShowcaseIssueNumbers: []int{},
		Unavailable:          []string{},
	}
	contributors := map[string]bool{}
	for _, issue := range engagement.Issues {
		if hasLabel(issue.Labels, "showcase") {
			summary.ShowcaseSubmissions++
			summary.ShowcaseIssueNumbers = append(summary.ShowcaseIssueNumbers, issue.Number)
		}
		if meaningfulIssue(issue) && externalAssociation(issue.AuthorAssociation) {
			summary.MeaningfulExternalIssues++
			if issue.Author != "" {
				contributors[issue.Author] = true
			}
		}
	}
	for _, pr := range engagement.PullRequests {
		if mergedExternalPR(pr) {
			summary.MergedExternalPRs++
			if pr.Author != "" {
				contributors[pr.Author] = true
			}
		}
	}
	summary.ExternalContributors = len(contributors)
	if summary.ExternalContributors == 0 {
		summary.Unavailable = append(summary.Unavailable,
			"no external contributor is visible in the supplied engagement window; this is not evidence that none exist")
	}
	sort.Ints(summary.ShowcaseIssueNumbers)
	return summary
}

// LoadSnapshot reads and validates a schema-v1 snapshot.
func LoadSnapshot(path string) (*Snapshot, error) {
	var snapshot Snapshot
	if err := readJSON(path, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported snapshot schema_version %d", path, snapshot.SchemaVersion)
	}
	if snapshot.Repo == "" || snapshot.CapturedAt == "" {
		return nil, fmt.Errorf("%s: snapshot is missing repo or captured_at", path)
	}
	return &snapshot, nil
}

func LoadEngagement(path string) (*Engagement, error) {
	var engagement Engagement
	if err := readJSON(path, &engagement); err != nil {
		return nil, err
	}
	if engagement.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported engagement schema_version %d", path, engagement.SchemaVersion)
	}
	return &engagement, nil
}

// RenderAnalyticsMarkdown is the human report. It always separates verified
// values from unavailable data and never turns downloads into users.
func RenderAnalyticsMarkdown(report *AnalyticsReport) string {
	var builder strings.Builder
	builder.WriteString("# Growth analytics — " + report.Repo + "\n\n")
	builder.WriteString("- Snapshot captured: " + report.CapturedAt + "\n")
	if report.BaselineAt != "" {
		builder.WriteString("- Baseline captured: " + report.BaselineAt + "\n")
	} else {
		builder.WriteString("- Baseline: none supplied; this is a point-in-time reading.\n")
	}
	builder.WriteString("- Visibility: " + report.Visibility + "\n\n")

	builder.WriteString("## Verified public signals\n\n")
	if len(report.Changes) == 0 {
		builder.WriteString("No comparison baseline was supplied.\n\n")
	} else {
		builder.WriteString("| Metric | Baseline | Current | Net change | Quality |\n|---|---|---|---|---|\n")
		for _, item := range report.Changes {
			builder.WriteString(fmt.Sprintf("| %s | %v | %v | %v | %s |\n",
				item.Metric, item.Before, item.After, item.Delta, item.Quality))
		}
		builder.WriteString("\n")
	}
	builder.WriteString(fmt.Sprintf("- Open issues: %d\n", report.OpenIssues))
	builder.WriteString(fmt.Sprintf("- Open pull requests: %d\n", report.OpenPRs))
	builder.WriteString(fmt.Sprintf("- Release asset downloads (cumulative): %d\n\n", report.Downloads))

	builder.WriteString("## Engagement (item level)\n\n")
	if report.Engagement == nil {
		builder.WriteString("- UNKNOWN: no engagement file was supplied. No issue or pull-request classification is claimed.\n\n")
	} else {
		summary := report.Engagement
		builder.WriteString(fmt.Sprintf("- Captured: %s\n", summary.CapturedAt))
		builder.WriteString(fmt.Sprintf("- Meaningful external issues: %d\n", summary.MeaningfulExternalIssues))
		builder.WriteString(fmt.Sprintf("- Merged external pull requests: %d\n", summary.MergedExternalPRs))
		builder.WriteString(fmt.Sprintf("- Showcase submissions: %d", summary.ShowcaseSubmissions))
		if len(summary.ShowcaseIssueNumbers) > 0 {
			builder.WriteString(" (issues ")
			for index, number := range summary.ShowcaseIssueNumbers {
				if index > 0 {
					builder.WriteString(", ")
				}
				builder.WriteString(fmt.Sprintf("#%d", number))
			}
			builder.WriteString(")")
		}
		builder.WriteString("\n")
		builder.WriteString(fmt.Sprintf("- Distinct external contributors visible: %d\n", summary.ExternalContributors))
		for _, item := range summary.Unavailable {
			builder.WriteString("- " + item + "\n")
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Not observable from this data\n\n")
	if len(report.Unavailable) == 0 {
		builder.WriteString("- None declared.\n\n")
	} else {
		for _, item := range report.Unavailable {
			builder.WriteString("- " + item + "\n")
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Interpretation limits\n\n")
	for _, caveat := range report.Caveats {
		builder.WriteString("- " + caveat + "\n")
	}
	builder.WriteString("\n## Source\n\n")
	builder.WriteString("- Snapshots: ops/github-metrics (read-only GitHub REST); no paid promotion or model spend.\n")
	builder.WriteString("- Analysis: ops/growth-engine " + report.ToolVersion + ", deterministic and offline.\n")
	return builder.String()
}

func runAnalytics(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("analytics", stderr)
	snapshotPath := flags.String("snapshot", "", "current snapshot JSON")
	beforePath := flags.String("before", "", "optional baseline snapshot JSON")
	engagementPath := flags.String("engagement", "", "optional item-level engagement JSON")
	output := flags.String("out", "", "output path (default stdout)")
	asJSON := flags.Bool("json", false, "write JSON instead of Markdown")
	force := flags.Bool("force", false, "replace an existing output file")
	stopFile := flags.String("stop-file", "", "halt if this file exists")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *snapshotPath == "" {
		return errors.New("--snapshot is required")
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
	report := ComputeAnalytics(snapshot, before, engagement)
	if *asJSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		return writeOutput(*output, append(encoded, '\n'), stdout, *force)
	}
	return writeOutput(*output, []byte(redact(RenderAnalyticsMarkdown(&report))), stdout, *force)
}
