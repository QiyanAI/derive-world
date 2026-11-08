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

// ResearchFile is an optional, owner-supplied research intake. Every record
// must cite where it came from and when it was retrieved. A record that is
// only a keyword match is rejected, never scored as an opportunity.
type ResearchFile struct {
	SchemaVersion int              `json:"schema_version"`
	Records       []ResearchRecord `json:"records"`
}

type ResearchRecord struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	SourceURL     string `json:"source_url"`
	RetrievedAt   string `json:"retrieved_at"`
	Audience      string `json:"audience"`
	Relevance     string `json:"relevance"`
	Action        string `json:"action"`
	ExpectedValue string `json:"expected_value"`
	Risk          string `json:"risk"`
	Reach         int    `json:"reach"`
	RiskScore     int    `json:"risk_score"`
	Confidence    int    `json:"confidence"`
	KeywordOnly   bool   `json:"keyword_only"`
	KeywordHint   string `json:"keyword_hint"`
}

// Opportunity is one scored growth opportunity with its provenance.
type Opportunity struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Source        string `json:"source"`
	SourceType    string `json:"source_type"`
	RetrievedAt   string `json:"retrieved_at"`
	Audience      string `json:"audience"`
	Relevance     string `json:"relevance"`
	Action        string `json:"action"`
	ExpectedValue string `json:"expected_value"`
	Risk          string `json:"risk"`
	Evidence      string `json:"evidence,omitempty"`
	Scores        struct {
		Relevance  int `json:"relevance"`
		Evidence   int `json:"evidence"`
		Reach      int `json:"reach"`
		Risk       int `json:"risk"`
		Confidence int `json:"confidence"`
	} `json:"scores"`
	Score    int    `json:"score"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// ScoreOpportunity applies the same transparent, rule-based weights to every
// record, so a decision can be audited rather than argued.
func ScoreOpportunity(opportunity *Opportunity) {
	composite := 0.35*float64(opportunity.Scores.Relevance) +
		0.25*float64(opportunity.Scores.Evidence) +
		0.20*float64(opportunity.Scores.Reach) +
		0.10*float64(100-opportunity.Scores.Risk) +
		0.10*float64(opportunity.Scores.Confidence)
	opportunity.Score = int(composite + 0.5)
	switch {
	case opportunity.Score >= 70:
		opportunity.Decision = "pursue"
	case opportunity.Score >= 45:
		opportunity.Decision = "research"
	default:
		opportunity.Decision = "skip"
	}
}

func clamp(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

// internalOpportunities derives recommendations from the project's own
// verified data. Each one names the file it came from; none invents an
// external audience or an unobserved outcome.
func internalOpportunities(snapshotPath string, snapshot *Snapshot, engagementPath string, engagement *Engagement, channels *ChannelFile) []Opportunity {
	var out []Opportunity
	add := func(item Opportunity) {
		item.SourceType = "snapshot"
		item.RetrievedAt = snapshot.CapturedAt
		out = append(out, item)
	}
	if snapshot.DataQuality.Traffic == "UNKNOWN" || snapshot.Metrics.Traffic == nil {
		item := Opportunity{
			ID:            "OBS-001",
			Title:         "Make traffic observable before spending on promotion",
			Source:        snapshotPath,
			Audience:      "maintainer",
			Relevance:     "Without administration read, views and clones are UNKNOWN, so no channel can be compared and no experiment can be measured. This blocks the whole optimization loop.",
			Action:        "Add a fine-grained METRICS_TOKEN with Administration: read to the existing metrics-daily workflow. Do not broaden the development token.",
			ExpectedValue: "Turns every later channel test from unmeasurable into a comparable, 14-day observation.",
			Risk:          "Low: read-only permission; secret handled by the existing workflow secret store.",
		}
		item.Scores.Relevance, item.Scores.Evidence, item.Scores.Reach = 95, 100, 40
		item.Scores.Risk, item.Scores.Confidence = 5, 95
		item.Reason = "Data-quality note in the snapshot; existing ops/README documents the required permission."
		ScoreOpportunity(&item)
		add(item)
	}
	if engagement == nil {
		item := Opportunity{
			ID:            "OBS-002",
			Title:         "Produce item-level issue and pull-request evidence",
			Source:        snapshotPath,
			Audience:      "maintainer",
			Relevance:     "The snapshot counts open issues and PRs but cannot distinguish a meaningful contributor report from noise, so contribution growth stays UNKNOWN.",
			Action:        "Run the documented read-only engagement export and pass it to the weekly analysis. Classify only public item metadata.",
			ExpectedValue: "Makes external contribution and showcase participation a measured signal instead of a guess.",
			Risk:          "Low: public metadata only; no private repository content.",
		}
		item.Scores.Relevance, item.Scores.Evidence, item.Scores.Reach = 90, 100, 30
		item.Scores.Risk, item.Scores.Confidence = 5, 90
		item.Reason = "Engagement input absent from this run; the classification is marked UNKNOWN rather than assumed."
		ScoreOpportunity(&item)
		add(item)
	} else {
		summary := summarizeEngagement(engagement)
		if summary.ShowcaseSubmissions == 0 {
			item := Opportunity{
				ID:            "OBS-003",
				Title:         "Open the voluntary showcase path with a reproducible challenge",
				Source:        engagementPath,
				Audience:      "creators and developers who already build worlds",
				Relevance:     "No showcase submission is visible in the supplied window. The repository already provides a submission issue form and public derivation; the missing piece is a concrete, low-effort entry point.",
				Action:        "Publish the Derive Relay base world and its derivation prompts, then link the submission form. Do not solicit private worlds or pre-write submissions.",
				ExpectedValue: "A public, reproducible participation path that can produce real showcase evidence.",
				Risk:          "Low: no automated collection; submission remains voluntary and manually reviewed.",
			}
			item.Scores.Relevance, item.Scores.Evidence, item.Scores.Reach = 80, 90, 50
			item.Scores.Risk, item.Scores.Confidence = 10, 60
			item.Reason = "Derived from the engagement export and the existing showcase issue form in the repository."
			ScoreOpportunity(&item)
			add(item)
		}
	}
	if snapshot.Metrics.OpenPullRequests > 0 && snapshot.Metrics.Contributors <= 1 {
		item := Opportunity{
			ID:            "OBS-004",
			Title:         "Reduce review latency for the current pull-request queue",
			Source:        snapshotPath,
			Audience:      "external contributors evaluating whether to stay",
			Relevance:     fmt.Sprintf("%d open pull requests against %d visible contributor(s) is a first-response risk: a new contributor who waits cannot tell interest from abandonment.", snapshot.Metrics.OpenPullRequests, snapshot.Metrics.Contributors),
			Action:        "State a response window in CONTRIBUTING, triage the queue, and close or label stale items with a reason. This is a maintenance action, not a promotion.",
			ExpectedValue: "Improves contributor conversion without any new channel spend.",
			Risk:          "Low: maintenance work; do not merge unreviewed changes to hit a number.",
		}
		item.Scores.Relevance, item.Scores.Evidence, item.Scores.Reach = 75, 100, 45
		item.Scores.Risk, item.Scores.Confidence = 10, 80
		item.Reason = "Open PR and contributor counts from the snapshot."
		ScoreOpportunity(&item)
		add(item)
	}
	if snapshot.Metrics.Stars == 0 && totalDownloads(snapshot) > 0 {
		item := Opportunity{
			ID:            "OBS-005",
			Title:         "Check whether release archives are discoverable as a first experience",
			Source:        snapshotPath,
			Audience:      "visitors who reach releases but not the README",
			Relevance:     "Cumulative release-asset downloads exceed stars. That is a hypothesis about discoverability, not proof of users; downloads include maintainers and CI.",
			Action:        "Review the release notes first screen and the README entry paths. Do not claim downloads as users and do not buy promotion.",
			ExpectedValue: "A clearer first experience for the same traffic already arriving.",
			Risk:          "Low, provided the Fixture/Real AI distinction stays explicit.",
		}
		item.Scores.Relevance, item.Scores.Evidence, item.Scores.Reach = 55, 85, 50
		item.Scores.Risk, item.Scores.Confidence = 10, 35
		item.Reason = "Snapshot counts only; the causal link to discovery is unproven and labelled as a hypothesis."
		ScoreOpportunity(&item)
		add(item)
	}
	if channels != nil {
		unobserved := 0
		for _, channel := range channels.Channels {
			if len(channel.Observed) == 0 {
				unobserved++
			}
		}
		if unobserved > 0 {
			item := Opportunity{
				ID:            "OBS-006",
				Title:         "Record channel outcomes before scaling any channel",
				Source:        "state/channels.json",
				Audience:      "growth operator",
				Relevance:     fmt.Sprintf("%d of %d registered channels have no recorded observation, so the strategy layer has no evidence on which to keep or stop a channel.", unobserved, len(channels.Channels)),
				Action:        "After each authorized action, record date, material and observable result in the channel registry. Leave the result empty when nothing can be observed.",
				ExpectedValue: "Prevents effort from accumulating in channels that never worked.",
				Risk:          "Low: bookkeeping only.",
			}
			item.SourceType = "registry"
			item.RetrievedAt = channels.UpdatedAt
			item.Scores.Relevance, item.Scores.Evidence, item.Scores.Reach = 70, 90, 20
			item.Scores.Risk, item.Scores.Confidence = 5, 90
			item.Reason = "Channel registry inspection."
			ScoreOpportunity(&item)
			out = append(out, item)
		}
	}
	return out
}

func researchOpportunities(file *ResearchFile) ([]Opportunity, []Opportunity) {
	var accepted, rejected []Opportunity
	for _, record := range file.Records {
		item := Opportunity{
			ID:            record.ID,
			Title:         record.Title,
			Source:        record.SourceURL,
			SourceType:    "research",
			RetrievedAt:   record.RetrievedAt,
			Audience:      record.Audience,
			Relevance:     record.Relevance,
			Action:        record.Action,
			ExpectedValue: record.ExpectedValue,
			Risk:          record.Risk,
		}
		reject := func(reason string) {
			item.Decision = "rejected"
			item.Reason = reason
			rejected = append(rejected, item)
		}
		switch {
		case record.KeywordOnly:
			reject("keyword-only match: a shared word is not a real relationship to Derive World")
			continue
		case !strings.HasPrefix(record.SourceURL, "https://"):
			reject("source_url must be an https URL")
			continue
		case record.RetrievedAt == "":
			reject("retrieved_at is required; undated research is not evidence")
			continue
		case len(strings.TrimSpace(record.Relevance)) < 80:
			reject("relevance must state a concrete relationship in at least 80 characters")
			continue
		case strings.EqualFold(strings.TrimSpace(record.Relevance), strings.TrimSpace(record.KeywordHint)):
			reject("relevance repeats the keyword hint")
			continue
		case record.Action == "" || record.ExpectedValue == "" || record.Risk == "":
			reject("action, expected_value and risk are required")
			continue
		}
		item.Scores.Relevance = clamp(judgement(record.Relevance, 80))
		item.Scores.Evidence = 90
		item.Scores.Reach = clamp(record.Reach)
		item.Scores.Risk = clamp(record.RiskScore)
		item.Scores.Confidence = clamp(record.Confidence)
		item.Reason = "Owner-supplied research with a dated source; score is a transparent rule-based estimate, not a measured outcome."
		ScoreOpportunity(&item)
		accepted = append(accepted, item)
	}
	return accepted, rejected
}

// judgement maps the strength of a written explanation onto the relevance
// score. It is deliberately conservative: a short sentence is never a strong
// opportunity.
func judgement(text string, minimum int) int {
	length := len(strings.TrimSpace(text))
	switch {
	case length >= minimum*2:
		return 90
	case length >= minimum:
		return 75
	default:
		return 40
	}
}

func sortOpportunities(items []Opportunity) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].ID < items[j].ID
	})
}

func RenderOpportunitiesMarkdown(items, rejected []Opportunity, snapshot *Snapshot) string {
	var builder strings.Builder
	builder.WriteString("# Growth opportunities — " + snapshot.Repo + "\n\n")
	builder.WriteString("- Snapshot captured: " + snapshot.CapturedAt + "\n")
	builder.WriteString("- Method: rule-based score = 0.35 relevance + 0.25 evidence + 0.20 reach + 0.10 (100 − risk) + 0.10 confidence. Thresholds: pursue ≥ 70, research ≥ 45, skip below.\n")
	builder.WriteString("- This report authorizes no external publication. Unauthorized channels remain material-only.\n\n")
	groups := []struct {
		decision string
		title    string
	}{
		{"pursue", "Pursue now"},
		{"research", "Research further"},
		{"skip", "Not recommended now"},
	}
	for _, group := range groups {
		builder.WriteString("## " + group.title + "\n\n")
		any := false
		for _, item := range items {
			if item.Decision != group.decision {
				continue
			}
			any = true
			writeOpportunity(&builder, item)
		}
		if !any {
			builder.WriteString("None.\n\n")
		}
	}
	if len(rejected) > 0 {
		builder.WriteString("## Rejected research\n\n")
		for _, item := range rejected {
			builder.WriteString("- **" + item.ID + " — " + item.Title + "** (" + item.Source + "): " + item.Reason + ".\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## Sources\n\n")
	seen := map[string]bool{}
	for _, item := range items {
		if item.Source == "" || seen[item.Source] {
			continue
		}
		seen[item.Source] = true
		builder.WriteString("- " + item.Source + " (" + item.SourceType + ", retrieved " + item.RetrievedAt + ")\n")
	}
	if len(seen) == 0 {
		builder.WriteString("- None.\n")
	}
	return builder.String()
}

func writeOpportunity(builder *strings.Builder, item Opportunity) {
	builder.WriteString("### " + item.ID + " — " + item.Title + "\n\n")
	builder.WriteString(fmt.Sprintf("- Score: %d (relevance %d, evidence %d, reach %d, risk %d, confidence %d)\n",
		item.Score, item.Scores.Relevance, item.Scores.Evidence, item.Scores.Reach, item.Scores.Risk, item.Scores.Confidence))
	builder.WriteString("- Source: " + item.Source + " (" + item.SourceType + ", retrieved " + item.RetrievedAt + ")\n")
	builder.WriteString("- Audience: " + item.Audience + "\n")
	builder.WriteString("- Why it relates: " + item.Relevance + "\n")
	builder.WriteString("- Executable participation: " + item.Action + "\n")
	builder.WriteString("- Expected value: " + item.ExpectedValue + "\n")
	builder.WriteString("- Risk: " + item.Risk + "\n")
	builder.WriteString("- Basis: " + item.Reason + "\n\n")
}

func loadResearch(path string) (*ResearchFile, error) {
	var file ResearchFile
	if err := readJSON(path, &file); err != nil {
		return nil, err
	}
	if file.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported research schema_version %d", path, file.SchemaVersion)
	}
	return &file, nil
}

func runOpportunities(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("opportunities", stderr)
	snapshotPath := flags.String("snapshot", "", "current snapshot JSON")
	engagementPath := flags.String("engagement", "", "optional engagement JSON")
	researchPath := flags.String("research", "", "optional research intake JSON")
	channelsPath := flags.String("channels", "state/channels.json", "channel registry")
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
	var engagement *Engagement
	if *engagementPath != "" {
		engagement, err = LoadEngagement(*engagementPath)
		if err != nil {
			return err
		}
	}
	var channels *ChannelFile
	if *channelsPath != "" {
		channels, err = loadChannels(*channelsPath)
		if err != nil {
			return err
		}
	}
	items := internalOpportunities(*snapshotPath, snapshot, *engagementPath, engagement, channels)
	var rejected []Opportunity
	if *researchPath != "" {
		research, err := loadResearch(*researchPath)
		if err != nil {
			return err
		}
		accepted, refused := researchOpportunities(research)
		items = append(items, accepted...)
		rejected = append(rejected, refused...)
	}
	sortOpportunities(items)
	if *asJSON {
		payload := struct {
			SchemaVersion int           `json:"schema_version"`
			Repo          string        `json:"repo"`
			CapturedAt    string        `json:"captured_at"`
			Opportunities []Opportunity `json:"opportunities"`
			Rejected      []Opportunity `json:"rejected"`
		}{1, snapshot.Repo, snapshot.CapturedAt, items, rejected}
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		return writeOutput(*output, append(encoded, '\n'), stdout, *force)
	}
	return writeOutput(*output, []byte(redact(RenderOpportunitiesMarkdown(items, rejected, snapshot))), stdout, *force)
}
