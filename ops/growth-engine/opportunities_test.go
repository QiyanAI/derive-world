// SPDX-License-Identifier: Apache-2.0
package main

import "testing"

func TestKeywordOnlyResearchIsRejected(t *testing.T) {
	file := &ResearchFile{SchemaVersion: 1, Records: []ResearchRecord{
		{
			ID:            "R-1",
			Title:         "AI tool community",
			SourceURL:     "https://example.com/community",
			RetrievedAt:   "2026-11-08",
			Audience:      "AI enthusiasts",
			Relevance:     "It mentions AI and Derive World also mentions AI, so this is clearly a fit for promotion.",
			Action:        "Post a link.",
			ExpectedValue: "Exposure.",
			Risk:          "Unknown.",
			KeywordOnly:   true,
			KeywordHint:   "AI",
		},
	}}
	accepted, rejected := researchOpportunities(file)
	if len(accepted) != 0 || len(rejected) != 1 {
		t.Fatalf("accepted=%d rejected=%d", len(accepted), len(rejected))
	}
	if rejected[0].Decision != "rejected" {
		t.Fatalf("decision = %q", rejected[0].Decision)
	}
}

func TestResearchWithoutSourceIsRejected(t *testing.T) {
	file := &ResearchFile{SchemaVersion: 1, Records: []ResearchRecord{
		{ID: "R-2", Title: "No source", RetrievedAt: "2026-11-08", Relevance: "x", Action: "a", ExpectedValue: "e", Risk: "r"},
		{ID: "R-3", Title: "Undated", SourceURL: "https://example.com/x", Relevance: "x", Action: "a", ExpectedValue: "e", Risk: "r"},
		{ID: "R-4", Title: "Short relevance", SourceURL: "https://example.com/x", RetrievedAt: "2026-11-08", Relevance: "it fits", Action: "a", ExpectedValue: "e", Risk: "r"},
	}}
	accepted, rejected := researchOpportunities(file)
	if len(accepted) != 0 || len(rejected) != 3 {
		t.Fatalf("accepted=%d rejected=%d", len(accepted), len(rejected))
	}
}

func TestScoringThresholds(t *testing.T) {
	high := Opportunity{}
	high.Scores.Relevance, high.Scores.Evidence, high.Scores.Reach = 100, 100, 100
	high.Scores.Risk, high.Scores.Confidence = 0, 100
	ScoreOpportunity(&high)
	if high.Decision != "pursue" || high.Score != 100 {
		t.Fatalf("high = %+v", high)
	}
	low := Opportunity{}
	low.Scores.Relevance, low.Scores.Evidence, low.Scores.Reach = 10, 10, 10
	low.Scores.Risk, low.Scores.Confidence = 90, 10
	ScoreOpportunity(&low)
	if low.Decision != "skip" {
		t.Fatalf("low = %+v", low)
	}
}

func TestInternalOpportunitiesCiteTheirSource(t *testing.T) {
	snapshot := snapshotFixture("2026-11-07T06:48:25Z", 0, 0, 0, 1, 35)
	items := internalOpportunities("baseline/snapshot.json", snapshot, "", nil, nil)
	if len(items) == 0 {
		t.Fatal("no internal opportunities derived")
	}
	for _, item := range items {
		if item.Source == "" {
			t.Fatalf("%s has no source", item.ID)
		}
		if item.Relevance == "" || item.Action == "" || item.Risk == "" {
			t.Fatalf("%s is incomplete: %+v", item.ID, item)
		}
		if item.Decision == "" {
			t.Fatalf("%s has no decision", item.ID)
		}
	}
}
