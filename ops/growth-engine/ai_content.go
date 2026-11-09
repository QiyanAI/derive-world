// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const aiContentSystemPrompt = `You write precise, honest promotional drafts for an open-source project.
Use ONLY the verified facts supplied by the user. Cite a fact after each product claim with its marker, for example: [[fact:what-it-is]].
Rules:
- Start with one Markdown H1 title.
- Never invent users, stars, downloads, funding, partnerships, benchmarks or platform rules.
- Never say production-ready, enterprise-grade, certified, guaranteed, or real AI without the exact scope.
- State that Fixture output is deterministic demo data and not real AI whenever you mention a demo or screenshot.
- Never claim the draft was published.
- Follow the target channel's rules and audience.
- Keep the draft under 900 words. Do not add a facts appendix; the tool will append one.
- The server rejects any draft with fewer than THREE DISTINCT verified fact ids. You must include at least three different [[fact:id]] markers from the supplied list, each attached to a real claim.
- Prefer one marker from each of: what-it-is, human-approval, lineage, open-source, fixture-not-real-ai, not-production. Repeating one id does not count as three.`

func runAIContent(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("content ai", stderr)
	kind := flags.String("kind", "", "content kind")
	channelID := flags.String("channel", "", "target channel id")
	channelsPath := flags.String("channels", "state/channels.json", "channel registry")
	factsPath := flags.String("facts", "state/facts.json", "verified product facts")
	providersPath := flags.String("providers", "state/providers.json", "provider registry")
	providerID := flags.String("provider", "", "provider id")
	budgetPath := flags.String("budget", "", "owner budget authorization")
	usagePath := flags.String("usage", "var/growth-engine/usage.json", "usage ledger")
	usageGitDir := flags.String("usage-git-dir", "", "git work tree containing the shared usage ledger")
	usageGitBranch := flags.String("usage-git-branch", "dge-state", "git branch used by --usage-git-dir")
	output := flags.String("out", "", "output path")
	force := flags.Bool("force", false, "replace an existing draft")
	date := flags.String("date", "", "draft date YYYY-MM-DD")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *kind == "" || *channelID == "" || *date == "" {
		return errors.New("--kind, --channel and --date are required")
	}
	if !contentKinds[*kind] {
		return fmt.Errorf("unknown content kind %q", *kind)
	}
	facts, err := loadFacts(*factsPath)
	if err != nil {
		return err
	}
	channels, err := loadChannels(*channelsPath)
	if err != nil {
		return err
	}
	channel, ok := findChannel(channels, *channelID)
	if !ok {
		return fmt.Errorf("unknown channel %q", *channelID)
	}
	if !channelFits(channel, *kind) {
		return fmt.Errorf("channel %q does not accept content kind %q", channel.ID, *kind)
	}
	client, err := loadModelClient(*providersPath, *providerID, *budgetPath, *usagePath, *usageGitDir, *usageGitBranch, "content")
	if err != nil {
		return err
	}
	var factLines strings.Builder
	for _, fact := range facts.Facts {
		factLines.WriteString("- " + fact.ID + ": " + fact.Statement + "\n")
	}
	user := "CONTENT KIND: " + *kind + "\n" +
		"CHANNEL: " + channel.Name + " (" + channel.ID + ")\n" +
		"AUDIENCE: " + channel.Audience + "\n" +
		"CHANNEL RULES:\n"
	for _, rule := range channel.Rules {
		user += "- " + rule + "\n"
	}
	user += "\nVERIFIED FACTS (the only product claims you may make):\n" + factLines.String() +
		"\nYour draft MUST contain at least three DISTINCT markers such as " +
		"[[fact:what-it-is]], [[fact:human-approval]], [[fact:lineage]] or " +
		"[[fact:open-source]]. Only use IDs from the list above. " +
		"\nWrite the draft now. Today is " + *date + "."
	response, err := client.Complete(context.Background(), ModelRequest{
		Purpose:         "content",
		System:          aiContentSystemPrompt,
		User:            user,
		MaxOutputTokens: 2000,
		JSON:            false,
	})
	if err != nil {
		return err
	}
	draft := strings.TrimSpace(response.Text)
	if !strings.Contains(draft, "not published") {
		draft = draftNotice + "\n\n" + draft
	}
	meta := fmt.Sprintf("\n\n<!-- AI draft: provider=%s model=%s date=%s input_tokens=%d output_tokens=%d estimated_cost_usd=%.6f. Not published. -->\n",
		response.Provider, response.Model, *date, response.InputTokens, response.OutputTokens, response.CostUSD)
	draft = strings.TrimSpace(draft) + meta
	factAppendix := "\n## Facts used\n\n"
	for _, fact := range facts.Facts {
		if strings.Contains(draft, "[[fact:"+fact.ID+"]]") {
			factAppendix += "- `" + fact.ID + "` — " + fact.Statement + "\n"
		}
	}
	if factAppendix == "\n## Facts used\n\n" {
		return errors.New("model draft cites no verified fact; refusing to write it")
	}
	draft += factAppendix + "\n## Channel\n\n- Target: " + channel.Name + " (`" + channel.ID + "`), authorization `" + channel.Authorization + "`.\n- This draft has not been posted anywhere.\n"
	verification := VerifyDraft(*output, draft, facts)
	if !verification.OK {
		return fmt.Errorf("AI draft failed verification:\n%s", RenderVerificationText(verification))
	}
	if *output == "" {
		*output = filepath.Join("drafts", "ai", *kind+"-"+channel.ID+".md")
	}
	if err := writeOutput(*output, []byte(draft), stdout, *force); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote verified AI draft %s (%d calls, %.6f USD)\n", *output, 1, response.CostUSD)
	return nil
}
