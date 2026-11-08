// SPDX-License-Identifier: Apache-2.0
package main

import (
	"strings"
	"testing"
)

func snapshotFixture(capturedAt string, stars, forks, watchers, contributors int, downloads int) *Snapshot {
	snapshot := &Snapshot{SchemaVersion: 1, CapturedAt: capturedAt, Repo: "owner/repo", Visibility: "public"}
	snapshot.Metrics.Stars = stars
	snapshot.Metrics.Forks = forks
	snapshot.Metrics.Watchers = watchers
	snapshot.Metrics.Contributors = contributors
	snapshot.Metrics.OpenIssues = 3
	snapshot.Metrics.OpenPullRequests = 8
	snapshot.Metrics.Releases = []Release{{
		Tag:         "v0.2.0",
		PublishedAt: "2026-10-30T00:00:00Z",
		Assets:      []Asset{{Name: "archive.zip", DownloadCount: downloads}},
	}}
	snapshot.DataQuality = DataQuality{Traffic: "UNKNOWN", Contributors: "VERIFIED", Notes: []string{}}
	return snapshot
}

func TestComputeAnalyticsDeltas(t *testing.T) {
	before := snapshotFixture("2026-11-01T00:00:00Z", 2, 0, 1, 1, 10)
	after := snapshotFixture("2026-11-08T00:00:00Z", 5, 2, 3, 2, 17)
	report := ComputeAnalytics(after, before, nil)
	want := map[string]float64{"stars": 3, "forks": 2, "watchers": 2, "downloads_total": 7, "contributors": 1, "releases": 0}
	for metric, delta := range want {
		var found bool
		for _, change := range report.Changes {
			if change.Metric == metric {
				found = true
				value, ok := change.Delta.(int)
				if !ok || float64(value) != delta {
					t.Fatalf("%s delta = %v (%T), want %v", metric, change.Delta, change.Delta, delta)
				}
			}
		}
		if !found {
			t.Fatalf("%s change missing from %+v", metric, report.Changes)
		}
	}
	if report.BaselineAt != before.CapturedAt {
		t.Fatalf("baseline = %q", report.BaselineAt)
	}
	if report.TrafficQuality != "UNKNOWN" {
		t.Fatalf("traffic quality = %q", report.TrafficQuality)
	}
	if len(report.Unavailable) == 0 {
		t.Fatal("UNKNOWN traffic must be listed as unavailable")
	}
}

func TestAnalyticsNeverEstimatesUnavailableTraffic(t *testing.T) {
	snapshot := snapshotFixture("2026-11-08T00:00:00Z", 0, 0, 0, 1, 35)
	report := ComputeAnalytics(snapshot, nil, nil)
	for _, change := range report.Changes {
		if strings.HasPrefix(change.Metric, "traffic.") && change.Quality != "UNKNOWN" {
			t.Fatalf("%s must be UNKNOWN: %+v", change.Metric, change)
		}
	}
	markdown := RenderAnalyticsMarkdown(&report)
	for _, want := range []string{"UNKNOWN", "not active users", "no engagement file"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("report missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "users:") {
		t.Fatalf("report must not present a user count:\n%s", markdown)
	}
}

func TestEngagementClassification(t *testing.T) {
	engagement := &Engagement{
		SchemaVersion: 1,
		CapturedAt:    "2026-11-08T00:00:00Z",
		Issues: []EngagementItem{
			{Number: 1, Author: "alice", AuthorAssociation: "NONE", BodyLength: 200, Comments: 0, Labels: []string{"bug"}},
			{Number: 2, Author: "maintainer", AuthorAssociation: "OWNER", BodyLength: 400, Comments: 3},
			{Number: 3, Author: "bob", AuthorAssociation: "NONE", BodyLength: 10, Comments: 0},
			{Number: 4, Author: "carol", AuthorAssociation: "FIRST_TIME_CONTRIBUTOR", BodyLength: 5, Comments: 0, Labels: []string{"showcase"}},
			{Number: 5, Author: "spammer", AuthorAssociation: "NONE", BodyLength: 400, Comments: 0, Labels: []string{"spam"}},
		},
		PullRequests: []EngagementPR{
			{Number: 10, Author: "alice", AuthorAssociation: "NONE", State: "closed", Merged: true},
			{Number: 11, Author: "maintainer", AuthorAssociation: "MEMBER", State: "closed", Merged: true},
			{Number: 12, Author: "dave", AuthorAssociation: "NONE", State: "closed", Merged: false},
		},
	}
	summary := summarizeEngagement(engagement)
	if summary.MeaningfulExternalIssues != 2 {
		t.Fatalf("meaningful external issues = %d, want 2", summary.MeaningfulExternalIssues)
	}
	if summary.MergedExternalPRs != 1 {
		t.Fatalf("merged external PRs = %d, want 1", summary.MergedExternalPRs)
	}
	if summary.ShowcaseSubmissions != 1 || len(summary.ShowcaseIssueNumbers) != 1 || summary.ShowcaseIssueNumbers[0] != 4 {
		t.Fatalf("showcase = %+v", summary)
	}
	if summary.ExternalContributors != 2 {
		t.Fatalf("external contributors = %d, want 2 (alice, carol)", summary.ExternalContributors)
	}
}

func TestEngagementMetricIsUnknownWithoutExport(t *testing.T) {
	snapshot := snapshotFixture("2026-11-08T00:00:00Z", 0, 0, 0, 1, 35)
	if _, ok := MetricValue("showcase_submissions", snapshot, nil); ok {
		t.Fatal("showcase submissions must be UNKNOWN without an engagement export")
	}
	summary := summarizeEngagement(&Engagement{SchemaVersion: 1, Issues: []EngagementItem{}, PullRequests: []EngagementPR{}})
	if len(summary.Unavailable) == 0 {
		t.Fatal("an empty window must not be presented as proof that no contributor exists")
	}
}

func TestLoadSnapshotRejectsUnknownSchema(t *testing.T) {
	path := t.TempDir() + "/snapshot.json"
	if err := writeFile(path, []byte(`{"schema_version":2,"captured_at":"2026-11-08T00:00:00Z","repo":"o/r"}`), false); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSnapshot(path); err == nil {
		t.Fatal("schema 2 snapshot accepted")
	}
}
