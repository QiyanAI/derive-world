// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"strings"
)

// RenderReport produces the required Markdown report. It always states data
// caveats and UNKNOWN values; it never turns downloads or stars into users.
func RenderReport(snapshot *Snapshot, diff *SnapshotDiff, title string) string {
	if title == "" {
		title = snapshot.Repo + " GitHub Snapshot"
	}
	var builder strings.Builder
	builder.WriteString("## " + title + "\n\n")
	builder.WriteString("- Captured: " + snapshot.CapturedAt + "\n")
	builder.WriteString("- Repository: " + snapshot.Repo + " (" + snapshot.Visibility + ")\n")
	if diff != nil {
		builder.WriteString("- Baseline: " + diff.BeforeAt + "\n")
	}

	builder.WriteString("\n### Metrics\n\n")
	writeMetric := func(name string, value int) {
		if diff != nil {
			if change, ok := findChange(*diff, name); ok {
				builder.WriteString(fmt.Sprintf("- %s: %d (%v vs baseline)\n", name, value, change.Delta))
				return
			}
		}
		builder.WriteString(fmt.Sprintf("- %s: %d\n", name, value))
	}
	writeMetric("Stars", snapshot.Metrics.Stars)
	writeMetric("Forks", snapshot.Metrics.Forks)
	writeMetric("Watchers", snapshot.Metrics.Watchers)
	writeMetric("Open Issues", snapshot.Metrics.OpenIssues)
	writeMetric("Open Pull Requests", snapshot.Metrics.OpenPullRequests)
	if snapshot.DataQuality.Contributors == "UNKNOWN" {
		builder.WriteString("- Contributors: UNKNOWN\n")
	} else {
		writeMetric("Contributors", snapshot.Metrics.Contributors)
	}
	builder.WriteString(fmt.Sprintf("- Releases: %d\n", len(snapshot.Metrics.Releases)))
	builder.WriteString(fmt.Sprintf("- Total asset downloads: %d\n", totalDownloads(snapshot)))
	for _, release := range snapshot.Metrics.Releases {
		for _, asset := range release.Assets {
			builder.WriteString(fmt.Sprintf("  - %s / %s: %d downloads\n", release.Tag, asset.Name, asset.DownloadCount))
		}
	}

	builder.WriteString("\n### Traffic\n\n")
	if snapshot.Metrics.Traffic == nil || snapshot.DataQuality.Traffic == "UNKNOWN" {
		builder.WriteString("- Views (14d): UNKNOWN\n- Clones (14d): UNKNOWN\n")
	} else {
		builder.WriteString(fmt.Sprintf("- Views (14d): %d (%d unique)\n", snapshot.Metrics.Traffic.Views14d.Count, snapshot.Metrics.Traffic.Views14d.Uniques))
		builder.WriteString(fmt.Sprintf("- Clones (14d): %d (%d unique)\n", snapshot.Metrics.Traffic.Clones14d.Count, snapshot.Metrics.Traffic.Clones14d.Uniques))
	}

	builder.WriteString("\n### Data quality\n\n")
	builder.WriteString("- traffic: " + snapshot.DataQuality.Traffic + "\n")
	builder.WriteString("- contributors: " + snapshot.DataQuality.Contributors + "\n")
	for _, note := range snapshot.DataQuality.Notes {
		builder.WriteString("- " + note + "\n")
	}
	if len(snapshot.DataQuality.Notes) == 0 {
		builder.WriteString("- No additional notes.\n")
	}

	builder.WriteString("\n### Caveats\n\n")
	builder.WriteString("- Stars, forks, and downloads are public signals, not active users or adoption.\n")
	builder.WriteString("- Maintainer and CI downloads cannot be excluded from public download counts.\n")
	builder.WriteString("- Traffic requires repository push/administration read; when unavailable it is UNKNOWN, never estimated.\n")
	builder.WriteString("- Report generated from the GitHub REST API; no paid promotion or model API spend is involved.\n")

	builder.WriteString("\n### Source\n\n")
	builder.WriteString("- API: GitHub REST, read-only\n")
	builder.WriteString(fmt.Sprintf("- Rate limit remaining at capture: %d\n", snapshot.API.RateLimitRemaining))
	return builder.String()
}

func findChange(diff SnapshotDiff, metric string) (Change, bool) {
	for _, change := range diff.Changes {
		if change.Metric == metric {
			return change, true
		}
	}
	return Change{}, false
}
