// SPDX-License-Identifier: Apache-2.0
package main

// Snapshot is AI-OPS snapshot schema v1 (G00 contract §A.13.1).
type Snapshot struct {
	SchemaVersion int         `json:"schema_version"`
	CapturedAt    string      `json:"captured_at"`
	Repo          string      `json:"repo"`
	Visibility    string      `json:"visibility"`
	Metrics       Metrics     `json:"metrics"`
	DataQuality   DataQuality `json:"data_quality"`
	API           APIInfo     `json:"api"`
}

type Metrics struct {
	Stars            int       `json:"stars"`
	Forks            int       `json:"forks"`
	Watchers         int       `json:"watchers"`
	OpenIssues       int       `json:"open_issues"`
	OpenPullRequests int       `json:"open_pull_requests"`
	Contributors     int       `json:"contributors"`
	Releases         []Release `json:"releases"`
	Traffic          *Traffic  `json:"traffic"`
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
