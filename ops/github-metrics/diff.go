// SPDX-License-Identifier: Apache-2.0
package main

import "fmt"

// SnapshotDiff is a deterministic comparison of two schema-v1 snapshots.
type SnapshotDiff struct {
	Repo        string      `json:"repo"`
	BeforeAt    string      `json:"before_captured_at"`
	AfterAt     string      `json:"after_captured_at"`
	Changes     []Change    `json:"changes"`
	DataQuality DataQuality `json:"after_data_quality"`
}

// Change is one metric delta. Unknown data is never estimated.
type Change struct {
	Metric string `json:"metric"`
	Before any    `json:"before"`
	After  any    `json:"after"`
	Delta  any    `json:"delta"`
}

// ComputeDiff compares two snapshots.
func ComputeDiff(before, after *Snapshot) SnapshotDiff {
	diff := SnapshotDiff{
		Repo:        after.Repo,
		BeforeAt:    before.CapturedAt,
		AfterAt:     after.CapturedAt,
		Changes:     make([]Change, 0, 10),
		DataQuality: after.DataQuality,
	}
	addInt := func(metric string, beforeValue, afterValue int) {
		diff.Changes = append(diff.Changes, Change{
			Metric: metric,
			Before: beforeValue,
			After:  afterValue,
			Delta:  afterValue - beforeValue,
		})
	}
	addInt("stars", before.Metrics.Stars, after.Metrics.Stars)
	addInt("forks", before.Metrics.Forks, after.Metrics.Forks)
	addInt("watchers", before.Metrics.Watchers, after.Metrics.Watchers)
	addInt("open_issues", before.Metrics.OpenIssues, after.Metrics.OpenIssues)
	addInt("open_pull_requests", before.Metrics.OpenPullRequests, after.Metrics.OpenPullRequests)

	if before.DataQuality.Contributors == "UNKNOWN" || after.DataQuality.Contributors == "UNKNOWN" {
		diff.Changes = append(diff.Changes, Change{Metric: "contributors", Before: "UNKNOWN", After: "UNKNOWN", Delta: "UNKNOWN"})
	} else {
		addInt("contributors", before.Metrics.Contributors, after.Metrics.Contributors)
	}

	addInt("releases", len(before.Metrics.Releases), len(after.Metrics.Releases))
	addInt("downloads_total", totalDownloads(before), totalDownloads(after))

	if before.Metrics.Traffic == nil || after.Metrics.Traffic == nil {
		diff.Changes = append(diff.Changes,
			Change{Metric: "traffic.views_14d", Before: "UNKNOWN", After: "UNKNOWN", Delta: "UNKNOWN"},
			Change{Metric: "traffic.clones_14d", Before: "UNKNOWN", After: "UNKNOWN", Delta: "UNKNOWN"},
		)
	} else {
		addInt("traffic.views_14d", before.Metrics.Traffic.Views14d.Count, after.Metrics.Traffic.Views14d.Count)
		addInt("traffic.clones_14d", before.Metrics.Traffic.Clones14d.Count, after.Metrics.Traffic.Clones14d.Count)
	}
	return diff
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

// DiffText is a compact human-readable summary used by the CLI.
func DiffText(diff SnapshotDiff) string {
	output := fmt.Sprintf("%s: %s -> %s\n", diff.Repo, diff.BeforeAt, diff.AfterAt)
	for _, change := range diff.Changes {
		output += fmt.Sprintf("- %s: %v -> %v (%v)\n", change.Metric, change.Before, change.After, change.Delta)
	}
	return output
}
