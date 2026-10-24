// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func snapshotServer(t *testing.T, trafficOK bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "42")
		switch r.URL.Path {
		case "/repos/owner/repo":
			fmt.Fprint(w, `{"full_name":"owner/repo","visibility":"private","stargazers_count":10,"forks_count":2,"subscribers_count":3,"watchers_count":11,"open_issues_count":5}`)
		case "/repos/owner/repo/pulls":
			fmt.Fprint(w, `[{"number":1},{"number":2}]`)
		case "/repos/owner/repo/contributors":
			w.Header().Set("Link", `<https://api.github.com/repositories/1/contributors?per_page=1&page=3>; rel="last"`)
			fmt.Fprint(w, `[{"login":"alice"}]`)
		case "/repos/owner/repo/releases":
			fmt.Fprint(w, `[{"tag_name":"v0.1.0","published_at":"2026-10-22T00:00:00Z","assets":[{"name":"a.tar.gz","download_count":7}]}]`)
		case "/repos/owner/repo/traffic/views":
			if !trafficOK {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			fmt.Fprint(w, `{"count":100,"uniques":20}`)
		case "/repos/owner/repo/traffic/clones":
			if !trafficOK {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			fmt.Fprint(w, `{"count":30,"uniques":5}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestTakeSnapshotWithoutTraffic(t *testing.T) {
	server := snapshotServer(t, false)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	snapshot, err := TakeSnapshot(context.Background(), client, "owner/repo", time.Date(2026, 10, 22, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}
	if snapshot.SchemaVersion != 1 || snapshot.CapturedAt != "2026-10-22T01:02:03Z" {
		t.Fatalf("snapshot header = %+v", snapshot)
	}
	if snapshot.Metrics.Stars != 10 || snapshot.Metrics.Forks != 2 || snapshot.Metrics.Watchers != 3 {
		t.Fatalf("metrics = %+v", snapshot.Metrics)
	}
	if snapshot.Metrics.OpenIssues != 3 || snapshot.Metrics.OpenPullRequests != 2 {
		t.Fatalf("issue/PR counts = %+v", snapshot.Metrics)
	}
	if snapshot.Metrics.Contributors != 3 {
		t.Fatalf("contributors = %d", snapshot.Metrics.Contributors)
	}
	if len(snapshot.Metrics.Releases) != 1 || totalDownloads(snapshot) != 7 {
		t.Fatalf("releases = %+v", snapshot.Metrics.Releases)
	}
	if snapshot.Metrics.Traffic != nil || snapshot.DataQuality.Traffic != "UNKNOWN" {
		t.Fatalf("traffic must be UNKNOWN: %+v", snapshot.Metrics.Traffic)
	}
	if snapshot.DataQuality.Contributors != "VERIFIED" {
		t.Fatalf("contributors quality = %s", snapshot.DataQuality.Contributors)
	}
	if snapshot.API.RateLimitRemaining != 42 {
		t.Fatalf("rate limit = %d", snapshot.API.RateLimitRemaining)
	}
}

func TestTakeSnapshotWithTraffic(t *testing.T) {
	server := snapshotServer(t, true)
	defer server.Close()
	snapshot, err := TakeSnapshot(context.Background(), NewClient(server.URL, ""), "owner/repo", time.Now())
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}
	if snapshot.Metrics.Traffic == nil || snapshot.DataQuality.Traffic != "VERIFIED" {
		t.Fatalf("traffic = %+v quality = %s", snapshot.Metrics.Traffic, snapshot.DataQuality.Traffic)
	}
	if snapshot.Metrics.Traffic.Views14d.Count != 100 || snapshot.Metrics.Traffic.Clones14d.Uniques != 5 {
		t.Fatalf("traffic = %+v", snapshot.Metrics.Traffic)
	}
}

func TestTakeSnapshotRejectsBadRepo(t *testing.T) {
	_, err := TakeSnapshot(context.Background(), NewClient("https://api.github.com", ""), "not-a-repo", time.Now())
	if err == nil {
		t.Fatal("invalid repo accepted")
	}
}

func TestPageCount(t *testing.T) {
	header := http.Header{}
	header.Set("Link", `<https://api.github.com/x?per_page=100&page=2>; rel="next", <https://api.github.com/x?per_page=100&page=4>; rel="last"`)
	if got := pageCount(header, 100, 100); got != 400 {
		t.Fatalf("pageCount = %d, want 400", got)
	}
	if got := pageCount(http.Header{}, 100, 3); got != 3 {
		t.Fatalf("pageCount no link = %d", got)
	}
}

func TestComputeDiffAndReport(t *testing.T) {
	before := &Snapshot{
		CapturedAt: "2026-10-21T00:00:00Z",
		Repo:       "owner/repo",
		Metrics: Metrics{
			Stars: 8, Forks: 1, Watchers: 2, OpenIssues: 1, OpenPullRequests: 1,
			Contributors: 2, Releases: []Release{},
		},
		DataQuality: DataQuality{Traffic: "UNKNOWN", Contributors: "VERIFIED", Notes: []string{}},
	}
	after := &Snapshot{
		CapturedAt: "2026-10-22T00:00:00Z",
		Repo:       "owner/repo",
		Visibility: "private",
		Metrics: Metrics{
			Stars: 10, Forks: 2, Watchers: 3, OpenIssues: 1, OpenPullRequests: 2,
			Contributors: 3,
			Releases:     []Release{{Tag: "v0.1.0", Assets: []Asset{{Name: "a", DownloadCount: 7}}}},
		},
		DataQuality: DataQuality{Traffic: "UNKNOWN", Contributors: "VERIFIED", Notes: []string{"traffic unavailable"}},
		API:         APIInfo{RateLimitRemaining: 42},
	}
	diff := ComputeDiff(before, after)
	if change, ok := findChange(diff, "stars"); !ok || change.Delta != 2 {
		t.Fatalf("stars change = %+v", change)
	}
	report := RenderReport(after, &diff, "")
	for _, want := range []string{"## owner/repo GitHub Snapshot", "Stars: 10", "UNKNOWN", "not active users", "traffic unavailable"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

func TestSnapshotJSONShape(t *testing.T) {
	snapshot := &Snapshot{
		SchemaVersion: 1,
		CapturedAt:    "2026-10-22T00:00:00Z",
		Repo:          "owner/repo",
		Visibility:    "private",
		Metrics:       Metrics{Releases: []Release{}, Traffic: nil},
		DataQuality:   DataQuality{Traffic: "UNKNOWN", Contributors: "UNKNOWN", Notes: []string{}},
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"schema_version":1`, `"traffic":null`, `"releases":[]`, `"notes":[]`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("snapshot JSON missing %s: %s", want, data)
		}
	}
}
