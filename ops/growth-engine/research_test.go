// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseRobots(t *testing.T) {
	rules := parseRobots("User-agent: *\nDisallow: /private\nDisallow: /tmp\n\nUser-agent: bot\nDisallow: /all")
	if rules.allows("/public") != true || rules.allows("/private/x") != false {
		t.Fatalf("rules = %+v", rules)
	}
	if rules.allows("/all") != true {
		t.Fatal("another user-agent's rules were applied")
	}
}

func TestCollectResearchHonoursRobotsAndParsesFeeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "User-agent: *\nDisallow: /private")
		case "/feed":
			fmt.Fprint(w, `<?xml version="1.0"?><rss><channel><item><title>Agent runtime</title><link>https://example.com/a</link><description>An open-source agent runtime with tool use.</description></item><item><title>Personal AI notes</title><link>https://example.com/b</link><description>Research on consenting personal assistants.</description></item></channel></rss>`)
		case "/page":
			fmt.Fprint(w, `<html><head><title>AI Creators</title><meta name="description" content="A community for AI creators"></head><body><h1>Featured worlds</h1><a href="/threads/1">A long discussion about interactive worlds</a></body></html>`)
		case "/private/feed":
			fmt.Fprint(w, `<rss><channel><item><title>blocked</title></item></channel></rss>`)
		case "/search/repositories":
			fmt.Fprint(w, `{"items":[{"full_name":"example/agent","html_url":"https://github.com/example/agent","description":"An agent framework","stargazers_count":42,"updated_at":"2026-11-01T00:00:00Z"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	file := &SourceFile{SchemaVersion: 1, Sources: []Source{
		{ID: "feed", Name: "Feed", Kind: "rss", URL: server.URL + "/feed", Audience: "devs", Why: "test", Enabled: true},
		{ID: "page", Name: "Page", Kind: "html", URL: server.URL + "/page", Audience: "creators", Why: "test", Enabled: true},
		{ID: "private", Name: "Private", Kind: "rss", URL: server.URL + "/private/feed", Audience: "devs", Why: "test", Enabled: true},
		{ID: "gh", Name: "Search", Kind: "github_search", URL: server.URL, Query: "ai agents", Audience: "devs", Why: "test", Enabled: true},
		{ID: "off", Name: "Off", Kind: "html", URL: server.URL + "/page", Audience: "devs", Why: "test", Enabled: false},
	}}
	report := collectResearch(context.Background(), file, time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), 0)
	if len(report.Items) < 4 {
		t.Fatalf("items = %d: %+v", len(report.Items), report.Items)
	}
	status := map[string]string{}
	for _, source := range report.Sources {
		status[source.ID] = source.Status
	}
	if status["feed"] != "ok" || status["page"] != "ok" || status["gh"] != "ok" {
		t.Fatalf("status = %v", status)
	}
	if status["private"] != "error" {
		t.Fatalf("robots-disallowed source status = %s", status["private"])
	}
	if status["off"] != "disabled" {
		t.Fatalf("disabled source status = %s", status["off"])
	}
}

func TestAnalyzeResearchRejectsHallucinatedEvidence(t *testing.T) {
	item := ResearchItem{
		ID: "s-1", SourceID: "s", Title: "AI agent community",
		URL:     "https://example.com/community",
		Summary: "A community of developers building autonomous agents and tools.",
	}
	modelReply := `{"opportunities":[
		{"title":"Agent developers","source_url":"https://example.com/community","evidence_quote":"developers building autonomous agents","audience":"agent developers","relevance":"The community builds autonomous agents, which overlaps with the project's Personal AI proposal and approval model, so a technical introduction is a real fit.","action":"Publish one technical introduction in an existing thread.","expected_value":"Reach developers who already work on agent runtimes.","risk":"Low; follow community rules.","reach":60,"risk_score":10,"confidence":70,"keyword_only":false},
		{"title":"Hallucinated claim","source_url":"https://example.com/community","evidence_quote":"this text is not on the page","audience":"x","relevance":"This relevance sentence is deliberately longer than eighty characters so only the quote fails validation.","action":"Post something.","expected_value":"Nothing.","risk":"High.","reach":10,"risk_score":90,"confidence":10,"keyword_only":false}
	]}`
	server := openAIServer(t, func(map[string]any) string { return modelReply })
	defer server.Close()
	dir := t.TempDir()
	client := NewModelClient(testProvider(server.URL, "openai"), futureBudget(),
		&Usage{SchemaVersion: 1}, filepath.Join(dir, "usage.json"), "test-key", time.Now)
	facts, err := loadFacts("state/facts.json")
	if err != nil {
		t.Fatal(err)
	}
	report := &ResearchReport{SchemaVersion: 1, Date: "2026-11-08", Items: []ResearchItem{item}}
	if err := analyzeResearch(context.Background(), report, facts, client); err != nil {
		t.Fatal(err)
	}
	if len(report.Proposals) != 1 || len(report.Rejected) != 1 {
		t.Fatalf("proposals=%d rejected=%d", len(report.Proposals), len(report.Rejected))
	}
	if report.Proposals[0].Evidence == "" || report.Proposals[0].Decision == "" {
		t.Fatalf("proposal = %+v", report.Proposals[0])
	}
	if !strings.Contains(report.Rejected[0].Reason, "evidence_quote") {
		t.Fatalf("rejection reason = %q", report.Rejected[0].Reason)
	}
}

func TestStripJSONFences(t *testing.T) {
	if got := stripJSONFences("```json\n{\"a\":1}\n```"); got != `{"a":1}` {
		t.Fatalf("got %q", got)
	}
	if got := stripJSONFences(`{"a":1}`); got != `{"a":1}` {
		t.Fatalf("got %q", got)
	}
}

func TestResearchReportJSONShape(t *testing.T) {
	report := ResearchReport{SchemaVersion: 1, Date: "2026-11-08", Items: []ResearchItem{}, Proposals: []Opportunity{}, Rejected: []Opportunity{}, Sources: []SourceResult{}, Notes: []string{}}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"items":[]`, `"proposals":[]`, `"rejected":[]`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("missing %s: %s", want, encoded)
		}
	}
}
