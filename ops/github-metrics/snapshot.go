// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type repoResponse struct {
	FullName        string `json:"full_name"`
	Visibility      string `json:"visibility"`
	StargazersCount int    `json:"stargazers_count"`
	ForksCount      int    `json:"forks_count"`
	Subscribers     int    `json:"subscribers_count"`
	WatchersCount   int    `json:"watchers_count"`
	OpenIssuesCount int    `json:"open_issues_count"`
}

type releaseResponse struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name          string `json:"name"`
		DownloadCount int    `json:"download_count"`
	} `json:"assets"`
}

type trafficResponse struct {
	Count   int `json:"count"`
	Uniques int `json:"uniques"`
}

func validateRepo(repo string) error {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("repo must be owner/name")
	}
	for _, part := range parts {
		for _, char := range part {
			if !(char == '-' || char == '_' || char == '.' ||
				(char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
				(char >= '0' && char <= '9')) {
				return errors.New("repo contains invalid characters")
			}
		}
	}
	return nil
}

// TakeSnapshot collects schema v1 metrics. Traffic and contributors are marked
// UNKNOWN rather than estimated when the API does not grant access.
func TakeSnapshot(ctx context.Context, client *Client, repo string, now time.Time) (*Snapshot, error) {
	if err := validateRepo(repo); err != nil {
		return nil, err
	}
	base := "/repos/" + repo

	var repository repoResponse
	header, err := client.get(ctx, base, &repository)
	if err != nil {
		return nil, fmt.Errorf("repository: %w", err)
	}
	remaining := rateLimitRemaining(header)

	var pulls []struct {
		Number int `json:"number"`
	}
	pullHeader, err := client.get(ctx, base+"/pulls?state=open&per_page=100", &pulls)
	if err != nil {
		return nil, fmt.Errorf("pull requests: %w", err)
	}
	openPullRequests := pageCount(pullHeader, 100, len(pulls))

	snapshot := &Snapshot{
		SchemaVersion: 1,
		CapturedAt:    now.UTC().Format(time.RFC3339),
		Repo:          repo,
		Visibility:    repository.Visibility,
		Metrics: Metrics{
			Stars:            repository.StargazersCount,
			Forks:            repository.ForksCount,
			Watchers:         watcherCount(repository),
			OpenPullRequests: openPullRequests,
			Releases:         make([]Release, 0),
			Traffic:          nil,
		},
		DataQuality: DataQuality{
			Traffic:      "UNKNOWN",
			Contributors: "UNKNOWN",
			Notes:        make([]string, 0),
		},
		API: APIInfo{RateLimitRemaining: remaining},
	}
	if snapshot.Visibility == "" {
		snapshot.Visibility = "private"
	}
	snapshot.Metrics.OpenIssues = repository.OpenIssuesCount - openPullRequests
	if snapshot.Metrics.OpenIssues < 0 {
		snapshot.Metrics.OpenIssues = 0
	}

	contributors, contributorHeader, contributorErr := countContributors(ctx, client, base)
	if contributorErr != nil {
		snapshot.DataQuality.Notes = append(snapshot.DataQuality.Notes, "contributors: "+contributorErr.Error())
	} else {
		snapshot.Metrics.Contributors = contributors
		snapshot.DataQuality.Contributors = "VERIFIED"
		remaining = rateLimitRemaining(contributorHeader)
	}

	var releases []releaseResponse
	releaseHeader, err := client.get(ctx, base+"/releases?per_page=100", &releases)
	if err != nil {
		return nil, fmt.Errorf("releases: %w", err)
	}
	remaining = rateLimitRemaining(releaseHeader)
	for _, release := range releases {
		assets := make([]Asset, 0, len(release.Assets))
		for _, asset := range release.Assets {
			assets = append(assets, Asset{Name: asset.Name, DownloadCount: asset.DownloadCount})
		}
		snapshot.Metrics.Releases = append(snapshot.Metrics.Releases, Release{
			Tag:         release.TagName,
			PublishedAt: release.PublishedAt,
			Assets:      assets,
		})
	}

	traffic, trafficErr := fetchTraffic(ctx, client, base)
	if trafficErr != nil {
		snapshot.DataQuality.Notes = append(snapshot.DataQuality.Notes,
			"traffic requires push/admin read; METRICS_TOKEN not provided or lacks permission: "+trafficErr.Error())
	} else {
		snapshot.Metrics.Traffic = traffic
		snapshot.DataQuality.Traffic = "VERIFIED"
	}

	snapshot.API.RateLimitRemaining = remaining
	return snapshot, nil
}

func watcherCount(repository repoResponse) int {
	if repository.Subscribers > 0 {
		return repository.Subscribers
	}
	return repository.WatchersCount
}

func countContributors(ctx context.Context, client *Client, base string) (int, http.Header, error) {
	var contributors []struct {
		Login string `json:"login"`
	}
	header, err := client.get(ctx, base+"/contributors?per_page=1&anon=false", &contributors)
	if err != nil {
		return 0, header, err
	}
	if contributors == nil {
		return 0, header, nil
	}
	return pageCount(header, 1, len(contributors)), header, nil
}

func fetchTraffic(ctx context.Context, client *Client, base string) (*Traffic, error) {
	var views trafficResponse
	if _, err := client.get(ctx, base+"/traffic/views", &views); err != nil {
		return nil, err
	}
	var clones trafficResponse
	if _, err := client.get(ctx, base+"/traffic/clones", &clones); err != nil {
		return nil, err
	}
	return &Traffic{
		Views14d:  TrafficCount{Count: views.Count, Uniques: views.Uniques},
		Clones14d: TrafficCount{Count: clones.Count, Uniques: clones.Uniques},
	}, nil
}
