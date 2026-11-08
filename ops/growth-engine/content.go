// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const draftNotice = "<!-- Prepared draft — not published. Review the facts, channel rules and authorization before posting. -->"

// ContentKind is one of the fixed draft shapes. Generation is deterministic:
// it composes verified facts into a template and never calls a model.
var contentKinds = map[string]bool{
	"technical-intro": true,
	"release-notes":   true,
	"showcase":        true,
	"tutorial":        true,
	"community":       true,
	"relay":           true,
	"product-update":  true,
}

// GenerateDraft composes a draft from verified facts only. Every product claim
// carries a [[fact:ID]] marker so the verifier can trace it.
func GenerateDraft(kind string, facts *FactFile, channel Channel) (string, error) {
	if !contentKinds[kind] {
		return "", fmt.Errorf("unknown content kind %q", kind)
	}
	byID := factMap(facts)
	need := func(ids ...string) error {
		for _, id := range ids {
			if _, ok := byID[id]; !ok {
				return fmt.Errorf("required fact %q is missing from the fact file", id)
			}
		}
		return nil
	}
	var builder strings.Builder
	builder.WriteString(draftNotice + "\n\n")
	switch kind {
	case "technical-intro":
		if err := need("what-it-is", "human-approval", "lineage", "open-source", "fixture-not-real-ai", "single-instance", "not-production", "real-ai-scope"); err != nil {
			return "", err
		}
		builder.WriteString("# Derive World: objects with history, not chats with memory\n\n")
		builder.WriteString("Derive World is an open-source foundation for Living Objects: shared, versioned objects that a person and their Personal AI can read, contribute to, derive from, and trace. [[fact:what-it-is]]\n\n")
		builder.WriteString("## What is actually different\n\n")
		builder.WriteString("1. **A human and a Personal AI create together, and the human approves.** The AI can propose a change or a derive; nothing happens until a person applies it. [[fact:human-approval]]\n")
		builder.WriteString("2. **A world can be changed, played, read or operated without becoming a different product category.** One content, state, variable, permission, version and lineage model carries free-form worlds and their surfaces. [[fact:what-it-is]]\n")
		builder.WriteString("3. **Derivation keeps provenance.** A derived object is independent, and its ancestry is recorded. [[fact:lineage]]\n\n")
		builder.WriteString("## What you can run today\n\n")
		builder.WriteString("The repository is Apache-2.0 and self-hostable with Docker Compose. [[fact:open-source]] The quickstart runs in Fixture mode, which is deterministic demo output and **not real AI**. [[fact:fixture-not-real-ai]]\n\n")
		builder.WriteString("## What it is not\n\n")
		builder.WriteString("- Not a production platform, and no security certification is claimed. [[fact:not-production]]\n")
		builder.WriteString("- Not a cross-instance federation network. [[fact:single-instance]]\n")
		builder.WriteString("- Not a general provider benchmark; real-model evidence is scoped. [[fact:real-ai-scope]]\n\n")
		builder.WriteString("If you want to try it, the repository README has the supported local path.\n")
	case "release-notes":
		if err := need("release", "what-it-is", "fixture-not-real-ai", "open-source"); err != nil {
			return "", err
		}
		builder.WriteString("# Derive World release update\n\n")
		builder.WriteString("Derive World is an open-source foundation for Living Objects. [[fact:release]] [[fact:open-source]]\n\n")
		builder.WriteString("## What changed\n\n")
		builder.WriteString("- Free-intent world creation: describe a world in your own words; the Personal AI writes a compact seed and the server composes a runnable world. [[fact:what-it-is]]\n")
		builder.WriteString("- Worlds can be changed, derived and traced with a human approval gate. [[fact:human-approval]] [[fact:lineage]]\n")
		builder.WriteString("- Fixture mode remains a deterministic demo and is not real AI. [[fact:fixture-not-real-ai]]\n\n")
		builder.WriteString("## How to inspect it\n\n")
		builder.WriteString("Use the repository release tag and its notes. Verify assets with the published checksums and provenance attestation shown in the release notes.\n")
	case "showcase":
		if err := need("what-it-is", "human-approval", "lineage", "fixture-not-real-ai"); err != nil {
			return "", err
		}
		builder.WriteString("# Build something that derives\n\n")
		builder.WriteString("Derive World treats the object as the product: a Living Object has versions, members, permissions, and a traceable lineage. [[fact:what-it-is]]\n\n")
		builder.WriteString("A human and a Personal AI can co-create, but the human approves every change and derive. [[fact:human-approval]] Derived objects are independent and keep their ancestry. [[fact:lineage]]\n\n")
		builder.WriteString("To share a world, use the repository's showcase submission form. Publish only what you intend to be public, remove private context and tokens, and label the work **Fixture** or **Real AI** accurately. Fixture output is deterministic demo data, not real AI. [[fact:fixture-not-real-ai]]\n\n")
		builder.WriteString("You do not have to star, share, or submit anything to use the software.\n")
	case "tutorial":
		if err := need("fixture-not-real-ai", "open-source", "human-approval"); err != nil {
			return "", err
		}
		builder.WriteString("# Run Derive World locally and derive your first world\n\n")
		builder.WriteString("This is the supported local path from the repository README. It runs in Fixture mode, which calls no model; all AI output is deterministic demo data. [[fact:fixture-not-real-ai]]\n\n")
		builder.WriteString("```bash\n")
		builder.WriteString("git clone https://github.com/QiyanAI/derive-world.git\n")
		builder.WriteString("cd derive-world\n")
		builder.WriteString("cp .env.example .env\n")
		builder.WriteString("docker compose up --build -d\n")
		builder.WriteString("```\n\n")
		builder.WriteString("Open <http://127.0.0.1:8080>, register a local account, and create a free world. The top bar shows `FIXTURE` while the deterministic adapter is active.\n\n")
		builder.WriteString("Then change the world in your own words. The Personal AI proposes; you approve. Nothing changes until the human confirmation. [[fact:human-approval]]\n\n")
		builder.WriteString("The software is Apache-2.0. [[fact:open-source]] If you configure a real endpoint, you pay your provider directly; never commit the key.\n")
	case "community":
		if err := need("what-it-is", "lineage", "fixture-not-real-ai"); err != nil {
			return "", err
		}
		builder.WriteString("# Discussion: what should a derived world keep from its source?\n\n")
		builder.WriteString("Derive World records lineage for derived objects: a child is independent, and later edits to the parent do not propagate. [[fact:lineage]]\n\n")
		builder.WriteString("I would like to hear how other builders think about this. When you derive a world, what should stay, what should reset, and what should be explicitly re-approved? The product currently derives at the whole-object level; per-entity derivation is not implemented. [[fact:what-it-is]]\n\n")
		builder.WriteString("If you try it, please use Fixture mode first and label any screenshots as Fixture — it is deterministic demo output, not real AI. [[fact:fixture-not-real-ai]]\n\n")
		builder.WriteString("This is a genuine question, not a launch post. Answer only if you have something concrete to add.\n")
	case "relay":
		if err := need("what-it-is", "lineage", "human-approval", "fixture-not-real-ai", "single-instance"); err != nil {
			return "", err
		}
		builder.WriteString("# Derive Relay — One World. Infinite Possibilities.\n\n")
		builder.WriteString("Derive Relay is a voluntary derivation challenge. Start from one reproducible base world, derive in any direction you like, and share only what you choose to make public.\n\n")
		builder.WriteString("A Living Object is shared, versioned, and traceable. A derived world is independent and keeps its ancestry. [[fact:lineage]] Human approval is required for AI-proposed changes and derives. [[fact:human-approval]]\n\n")
		builder.WriteString("Rules:\n\n")
		builder.WriteString("1. Start from the base world in `examples/worlds/relay-base.json` and publish your derived world with derivation allowed.\n")
		builder.WriteString("2. Any form is valid: game, story, workspace, visual space, 3D scene or a custom surface. There is no fixed category to match. [[fact:what-it-is]]\n")
		builder.WriteString("3. Submit through the repository showcase issue form. Never include private context, conversations, tokens or another person's data.\n")
		builder.WriteString("4. Label the work Fixture or Real AI accurately. Fixture is deterministic demo output, not real AI. [[fact:fixture-not-real-ai]]\n")
		builder.WriteString("5. Participation is voluntary. There is no prize, no star requirement and no purchase.\n\n")
		builder.WriteString("Scope note: publication and derivation are scoped to one instance. There is no cross-instance federation or public import. [[fact:single-instance]]\n")
	case "product-update":
		if err := need("what-it-is", "fixture-not-real-ai", "open-source"); err != nil {
			return "", err
		}
		builder.WriteString("# Derive World update\n\n")
		builder.WriteString("The project continues to develop free-form worlds with one content, state, variable, permission, version and lineage model. [[fact:what-it-is]]\n\n")
		builder.WriteString("The repository is Apache-2.0; the supported local path is Docker Compose. [[fact:open-source]] Fixture mode calls no model and is not real AI. [[fact:fixture-not-real-ai]]\n\n")
		builder.WriteString("See the release notes for the exact version and verification steps.\n")
	}
	builder.WriteString("\n## Facts used\n\n")
	for _, fact := range facts.Facts {
		if strings.Contains(builder.String(), "[[fact:"+fact.ID+"]]") {
			builder.WriteString("- `" + fact.ID + "` — " + fact.Statement + "\n")
		}
	}
	builder.WriteString("\n## Channel\n\n")
	builder.WriteString("- Target: " + channel.Name + " (`" + channel.ID + "`), authorization `" + channel.Authorization + "`.\n")
	builder.WriteString("- Audience: " + channel.Audience + ".\n")
	builder.WriteString("- This draft has not been posted anywhere.\n")
	return builder.String(), nil
}

// VerificationIssue is one problem with a draft.
type VerificationIssue struct {
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type VerificationReport struct {
	SchemaVersion int                 `json:"schema_version"`
	File          string              `json:"file"`
	OK            bool                `json:"ok"`
	FactsUsed     []string            `json:"facts_used"`
	Issues        []VerificationIssue `json:"issues"`
}

var factMarker = regexp.MustCompile(`\[\[fact:([a-z0-9-]+)\]\]`)

type forbiddenClaim struct {
	pattern *regexp.Regexp
	message string
}

var forbiddenClaims = []forbiddenClaim{
	{regexp.MustCompile(`(?i)\bproduction[- ]ready\b`), "production readiness is not claimed"},
	{regexp.MustCompile(`(?i)\benterprise[- ]grade\b`), "enterprise grade is not claimed"},
	{regexp.MustCompile(`(?i)\bcertified\b`), "certification is not claimed"},
	{regexp.MustCompile(`(?i)\b(funded|funding round|investors?|backed by)\b`), "funding is not claimed"},
	{regexp.MustCompile(`(?i)\b(guarantee|guaranteed)\b`), "growth or outcomes are not guaranteed"},
	{regexp.MustCompile(`(?i)\bwe (posted|published|announced)\b`), "a draft must not claim publication"},
	{regexp.MustCompile(`(?i)\b\d[\d,]*\s*(stars|users|customers|adopters)\b`), "user or star counts are not verified and must not be claimed"},
	{regexp.MustCompile(`(?i)\bthousands of\b`), "unverified scale claim"},
	{regexp.MustCompile(`(?i)\b(paid|buy) (stars|votes|promotion)\b`), "manipulation is forbidden"},
}

var externalURL = regexp.MustCompile(`https?://[^\s)>\]]+`)
var allowedURLPrefixes = []string{
	"https://github.com/QiyanAI/derive-world",
	"https://docs.github.com/",
	"http://127.0.0.1:8080",
}

// VerifyDraft performs the fact and safety checks that stand between a
// generated draft and any human posting decision.
func VerifyDraft(path string, content string, facts *FactFile) VerificationReport {
	report := VerificationReport{SchemaVersion: 1, File: path, OK: true, FactsUsed: []string{}, Issues: []VerificationIssue{}}
	byID := factMap(facts)
	lines := strings.Split(content, "\n")
	used := map[string]bool{}
	add := func(line int, severity, message string) {
		report.Issues = append(report.Issues, VerificationIssue{Line: line, Severity: severity, Message: message})
		if severity == "error" {
			report.OK = false
		}
	}
	for index, line := range lines {
		number := index + 1
		for _, match := range factMarker.FindAllStringSubmatch(line, -1) {
			if _, ok := byID[match[1]]; !ok {
				add(number, "error", "unknown fact marker [[fact:"+match[1]+"]]")
			} else if !used[match[1]] {
				used[match[1]] = true
				report.FactsUsed = append(report.FactsUsed, match[1])
			}
		}
		for _, claim := range forbiddenClaims {
			if claim.pattern.MatchString(line) {
				add(number, "error", claim.message)
			}
		}
		lower := strings.ToLower(line)
		if (strings.Contains(lower, "federation") || strings.Contains(lower, "federated")) &&
			!strings.Contains(lower, "no ") && !strings.Contains(lower, "not ") && !strings.Contains(lower, "never") {
			add(number, "error", "cross-instance federation does not exist; only a negation may mention it")
		}
		if strings.Contains(lower, "real ai") && !strings.Contains(lower, "not real ai") &&
			!strings.Contains(lower, "not a live") && !strings.Contains(lower, "not presented") {
			add(number, "error", "a Real AI mention must state the exact scope or negate fixture confusion")
		}
		if (strings.Contains(lower, "demo") || strings.Contains(lower, "screenshot")) &&
			!strings.Contains(lower, "fixture") && !strings.Contains(lower, "not a live") {
			add(number, "warning", "demo material must be labelled Fixture or explicitly marked as not live")
		}
		for _, match := range externalURL.FindAllString(line, -1) {
			allowed := false
			for _, prefix := range allowedURLPrefixes {
				if strings.HasPrefix(match, prefix) {
					allowed = true
					break
				}
			}
			if !allowed {
				add(number, "error", "external URL is not allowlisted: "+match)
			}
		}
		for _, pattern := range secretPatterns {
			if pattern.MatchString(line) {
				add(number, "error", "possible secret material in draft")
			}
		}
	}
	if !strings.Contains(content, "not published") {
		add(1, "error", "draft is missing the 'not published' notice")
	}
	if len(report.FactsUsed) < 3 {
		add(1, "error", "a draft must cite at least three verified facts")
	}
	return report
}

func RenderVerificationText(report VerificationReport) string {
	var builder strings.Builder
	builder.WriteString("file: " + report.File + "\n")
	builder.WriteString("facts used: " + strings.Join(report.FactsUsed, ", ") + "\n")
	if len(report.Issues) == 0 {
		builder.WriteString("result: OK\n")
		return builder.String()
	}
	for _, issue := range report.Issues {
		builder.WriteString(fmt.Sprintf("%s:%d: %s: %s\n", report.File, issue.Line, issue.Severity, issue.Message))
	}
	if report.OK {
		builder.WriteString("result: OK with warnings\n")
	} else {
		builder.WriteString("result: FAILED\n")
	}
	return builder.String()
}

func runContent(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "ai" {
		return runAIContent(args[1:], stdout, stderr)
	}
	flags := newFlagSet("content", stderr)
	kind := flags.String("kind", "", "content kind")
	channelID := flags.String("channel", "", "target channel id")
	channelsPath := flags.String("channels", "state/channels.json", "channel registry")
	factsPath := flags.String("facts", "state/facts.json", "verified product facts")
	output := flags.String("out", "", "output path (default stdout)")
	force := flags.Bool("force", false, "replace an existing output file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *kind == "" || *channelID == "" {
		return errors.New("--kind and --channel are required")
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
	draft, err := GenerateDraft(*kind, facts, channel)
	if err != nil {
		return err
	}
	report := VerifyDraft(*output, draft, facts)
	if !report.OK {
		return errors.New(RenderVerificationText(report))
	}
	return writeOutput(*output, []byte(draft), stdout, *force)
}

func runVerifyContent(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("verify-content", stderr)
	path := flags.String("file", "", "draft file")
	factsPath := flags.String("facts", "state/facts.json", "verified product facts")
	asJSON := flags.Bool("json", false, "write JSON instead of text")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("--file is required")
	}
	facts, err := loadFacts(*factsPath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	report := VerifyDraft(*path, string(content), facts)
	if *asJSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if _, err := stdout.Write(append(encoded, '\n')); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(stdout, RenderVerificationText(report)); err != nil {
			return err
		}
	}
	if !report.OK {
		return errors.New("draft failed verification")
	}
	return nil
}
