// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// PipelineStage records one observable stage outcome. A stage is never marked
// complete when its inputs were absent; it is skipped with a reason.
type PipelineStage struct {
	Name   string `json:"name"`
	Status string `json:"status"` // complete | partial | skipped | error
	Detail string `json:"detail"`
	Items  int    `json:"items"`
}

type ResearchSummary struct {
	Sources   []SourceResult `json:"sources"`
	ItemCount int            `json:"item_count"`
	Proposals int            `json:"proposals"`
	Rejected  int            `json:"rejected"`
}

type PipelineManifest struct {
	SchemaVersion int              `json:"schema_version"`
	Date          string           `json:"date"`
	Mode          string           `json:"mode"`
	Stages        []PipelineStage  `json:"stages"`
	Analytics     *AnalyticsReport `json:"analytics"`
	Opportunities []Opportunity    `json:"opportunities"`
	Research      *ResearchSummary `json:"research,omitempty"`
	Strategy      *StrategyPlan    `json:"strategy,omitempty"`
	StrategyMeta  *ModelResponse   `json:"strategy_meta,omitempty"`
	Usage         *Usage           `json:"usage,omitempty"`
	UsageSummary  *UsageSummary    `json:"usage_summary,omitempty"`
	NotPerformed  []string         `json:"not_performed"`
}

func runPipeline(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("pipeline", stderr)
	snapshotPath := flags.String("snapshot", "", "current snapshot JSON")
	beforePath := flags.String("before", "", "optional baseline snapshot JSON")
	engagementPath := flags.String("engagement", "", "optional engagement JSON")
	factsPath := flags.String("facts", "state/facts.json", "verified product facts")
	sourcesPath := flags.String("sources", "state/sources.json", "research sources")
	channelsPath := flags.String("channels", "state/channels.json", "channel registry")
	experimentsPath := flags.String("experiments", "state/experiments.json", "experiment ledger")
	providersPath := flags.String("providers", "state/providers.json", "provider registry")
	providerID := flags.String("provider", "", "provider id (enables AI stages)")
	budgetPath := flags.String("budget", "", "owner budget authorization")
	usagePath := flags.String("usage", "var/growth-engine/usage.json", "usage ledger")
	usageGitDir := flags.String("usage-git-dir", "", "git work tree containing the shared usage ledger")
	usageGitBranch := flags.String("usage-git-branch", "dge-state", "git branch used by --usage-git-dir")
	outDir := flags.String("out-dir", "runs", "output directory")
	force := flags.Bool("force", false, "replace existing run files")
	stopFile := flags.String("stop-file", "", "halt if this file exists")
	noAI := flags.Bool("no-ai", false, "force deterministic mode even if a provider is set")
	minInterval := flags.Duration("min-interval", 0, "delay between research sources")
	mode := flags.String("mode", "plan", "plan only; publishing is a separate approved command")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *snapshotPath == "" {
		return errors.New("--snapshot is required")
	}
	if *mode != "plan" {
		return errors.New("pipeline mode must be plan; publishing requires the separate publish command with an approval artifact")
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
	facts, err := loadFacts(*factsPath)
	if err != nil {
		return err
	}
	channels, err := loadChannels(*channelsPath)
	if err != nil {
		return err
	}
	var experiments *ExperimentFile
	if *experimentsPath != "" {
		experiments, err = loadExperiments(*experimentsPath)
		if err != nil {
			return err
		}
	}
	date, err := weeklyDate(snapshot.CapturedAt)
	if err != nil {
		return err
	}
	manifest := PipelineManifest{
		SchemaVersion: 1,
		Date:          date,
		Mode:          "plan",
		Stages:        []PipelineStage{},
		NotPerformed: []string{
			"No model call was made unless an AI stage below says complete.",
			"No external content was published, messaged or submitted.",
			"External publication requires a separate approval artifact and the publish command.",
		},
	}
	aiEnabled := *providerID != "" && !*noAI
	if aiEnabled && *budgetPath == "" {
		return errors.New("--budget is required when --provider is set")
	}
	var client *ModelClient
	if aiEnabled {
		client, err = loadModelClient(*providersPath, *providerID, *budgetPath, *usagePath, *usageGitDir, *usageGitBranch, "research")
		if err != nil {
			return err
		}
	}
	var usageBefore Usage
	if client != nil {
		loaded, err := client.store.Load()
		if err != nil {
			return fmt.Errorf("load usage ledger: %w", err)
		}
		usageBefore = *loaded
	}

	// Stage 1: research collection (external, read-only, untrusted).
	var research *ResearchReport
	sourceFile, err := loadSources(*sourcesPath)
	if err != nil {
		return err
	}
	enabled := 0
	for _, source := range sourceFile.Sources {
		if source.Enabled {
			enabled++
		}
	}
	if enabled == 0 {
		manifest.Stages = append(manifest.Stages, PipelineStage{
			Name: "research.collect", Status: "skipped", Detail: "no enabled source; nothing was fetched", Items: 0,
		})
	} else {
		collectedAt, err := time.Parse(time.RFC3339, snapshot.CapturedAt)
		if err != nil {
			return fmt.Errorf("snapshot captured_at: %w", err)
		}
		collected := collectResearch(context.Background(), sourceFile, collectedAt, *minInterval)
		research = &collected
		status := "complete"
		for _, source := range collected.Sources {
			if source.Status == "error" {
				status = "partial"
			}
		}
		manifest.Stages = append(manifest.Stages, PipelineStage{
			Name: "research.collect", Status: status,
			Detail: fmt.Sprintf("%d enabled sources; all content treated as untrusted data", enabled),
			Items:  len(collected.Items),
		})
	}

	// Stage 2: verified analytics.
	analytics := ComputeAnalytics(snapshot, before, engagement)
	manifest.Analytics = &analytics
	manifest.Stages = append(manifest.Stages, PipelineStage{
		Name: "analytics", Status: "complete", Detail: "derived from read-only snapshots", Items: len(analytics.Changes),
	})

	// Stage 3: deterministic opportunities.
	opportunities := internalOpportunities(*snapshotPath, snapshot, *engagementPath, engagement, channels)
	sortOpportunities(opportunities)

	// Stage 4: AI research analysis (optional, budget-gated).
	if client != nil && research != nil && len(research.Items) > 0 {
		if err := analyzeResearch(context.Background(), research, facts, client); err != nil {
			manifest.Stages = append(manifest.Stages, PipelineStage{
				Name: "research.analyze", Status: "error", Detail: redact(err.Error()),
			})
			manifest.Opportunities = opportunities
			manifest.Research = summarizeResearch(research)
			applyUsage(&manifest, client, usageBefore)
			writePipelineOutputs(*outDir, date, &manifest, *force)
			return err
		}
		opportunities = append(opportunities, research.Proposals...)
		sortOpportunities(opportunities)
		manifest.Stages = append(manifest.Stages, PipelineStage{
			Name: "research.analyze", Status: "complete",
			Detail: "AI proposals verified against fetched evidence; review-only",
			Items:  len(research.Proposals),
		})
	} else if client != nil {
		manifest.Stages = append(manifest.Stages, PipelineStage{
			Name: "research.analyze", Status: "skipped", Detail: "no collected items to analyze",
		})
	} else {
		manifest.Stages = append(manifest.Stages, PipelineStage{
			Name: "research.analyze", Status: "skipped", Detail: "no provider selected; deterministic mode",
		})
	}
	manifest.Opportunities = opportunities
	if research != nil {
		manifest.Research = summarizeResearch(research)
	}

	// Stage 5: AI strategy (optional, budget-gated, review-only).
	if client != nil {
		if stopRequested(*stopFile) {
			return errors.New("growth engine stop requested")
		}
		plan, meta, err := synthesizeStrategy(context.Background(), client, &analytics, opportunities, experiments)
		if err != nil {
			manifest.Stages = append(manifest.Stages, PipelineStage{
				Name: "strategy", Status: "error", Detail: redact(err.Error()),
			})
			applyUsage(&manifest, client, usageBefore)
			writePipelineOutputs(*outDir, date, &manifest, *force)
			return err
		}
		manifest.Strategy = plan
		manifest.StrategyMeta = meta
		manifest.Stages = append(manifest.Stages, PipelineStage{
			Name: "strategy", Status: "complete", Detail: "AI plan validated against known evidence ids; review-only", Items: len(plan.Actions),
		})
		applyUsage(&manifest, client, usageBefore)
	} else {
		manifest.Stages = append(manifest.Stages, PipelineStage{
			Name: "strategy", Status: "skipped", Detail: "no provider selected; use the weekly deterministic report",
		})
	}

	// Stage 6: content is prepared by `content` or `content ai`; the pipeline
	// never generates and publishes in one run.
	manifest.Stages = append(manifest.Stages, PipelineStage{
		Name: "content", Status: "skipped", Detail: "run `content ai` separately; generated drafts are never auto-published",
	})
	manifest.Stages = append(manifest.Stages, PipelineStage{
		Name: "publish", Status: "skipped", Detail: "requires an owner approval artifact and the separate publish command",
	})

	if err := writePipelineOutputs(*outDir, date, &manifest, *force); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "pipeline plan complete: %d opportunities, %d stages; wrote %s\n",
		len(manifest.Opportunities), len(manifest.Stages), filepath.Join(*outDir, date+"-pipeline.json"))
	return nil
}

func summarizeResearch(report *ResearchReport) *ResearchSummary {
	return &ResearchSummary{
		Sources:   report.Sources,
		ItemCount: len(report.Items),
		Proposals: len(report.Proposals),
		Rejected:  len(report.Rejected),
	}
}

func usagePointer(usage Usage) *Usage {
	return &usage
}

func applyUsage(manifest *PipelineManifest, client *ModelClient, before Usage) {
	if client == nil {
		return
	}
	after, err := client.store.Load()
	if err != nil {
		manifest.NotPerformed = append(manifest.NotPerformed, "Usage ledger could not be read after the run: "+redact(err.Error()))
		return
	}
	delta := usageDelta(before, *after)
	manifest.Usage = usagePointer(delta)
	summary := summarizeUsage(after, client.budget, client.provider)
	manifest.UsageSummary = &summary
}

func writePipelineOutputs(dir, date string, manifest *PipelineManifest, force bool) error {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, date+"-pipeline.json"), append(encoded, '\n'), force); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, date+"-pipeline.md"), []byte(redact(renderPipelineMarkdown(manifest))), force)
}

func renderPipelineMarkdown(manifest *PipelineManifest) string {
	var builder strings.Builder
	builder.WriteString("# Growth pipeline — " + manifest.Date + " (" + manifest.Mode + ")\n\n")
	builder.WriteString("## Stages\n\n")
	builder.WriteString("| Stage | Status | Detail | Items |\n|---|---|---|---|\n")
	for _, stage := range manifest.Stages {
		builder.WriteString(fmt.Sprintf("| %s | %s | %s | %d |\n", stage.Name, stage.Status, stage.Detail, stage.Items))
	}
	builder.WriteString("\n## Opportunities\n\n")
	if len(manifest.Opportunities) == 0 {
		builder.WriteString("- None.\n")
	}
	for _, item := range manifest.Opportunities {
		builder.WriteString(fmt.Sprintf("- **%s** (%s, score %d) — %s\n", item.Title, item.Decision, item.Score, item.Action))
	}
	if manifest.Strategy != nil {
		builder.WriteString("\n" + renderStrategyMarkdown(manifest.Strategy, manifest.StrategyMeta) + "\n")
	}
	if manifest.Usage != nil {
		builder.WriteString(fmt.Sprintf("\n## Model usage this run\n\n- Calls: %d\n- Input tokens: %d\n- Output tokens: %d\n- Estimated cost: %.6f USD\n- Hard cost cap enforced: %t\n",
			manifest.Usage.Calls, manifest.Usage.InputTokens, manifest.Usage.OutputTokens, manifest.Usage.CostUSD, manifest.Usage.CostCapEnforced))
		if manifest.UsageSummary != nil {
			builder.WriteString(fmt.Sprintf("- Cumulative authorization: %s — %d calls used, %d remaining; estimated %.6f USD",
				manifest.UsageSummary.AuthorizationID, manifest.UsageSummary.Calls, manifest.UsageSummary.RemainingCalls, manifest.UsageSummary.CostUSD))
			if manifest.UsageSummary.CostCapEnforced {
				builder.WriteString(fmt.Sprintf(", %.6f USD remaining under the cap", manifest.UsageSummary.RemainingCostUSD))
			}
			builder.WriteString("\n")
			if manifest.UsageSummary.Note != "" {
				builder.WriteString("- **" + manifest.UsageSummary.Note + "**\n")
			}
		}
	}
	builder.WriteString("\n## Not performed\n\n")
	for _, item := range manifest.NotPerformed {
		builder.WriteString("- " + item + "\n")
	}
	return builder.String()
}
