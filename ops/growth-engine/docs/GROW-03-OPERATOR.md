# GROW-03 — AI Growth Operator (offline, deterministic)

The operator is a set of deterministic analyzers. “AI” here means the system
reasons over structured evidence and produces reviewable reports; it does not
call a model, and it cannot act externally.

## Pipeline

```
github-metrics snapshot (daily, existing)
        │
        ├── analytics ──────────────► net change, data quality, contribution summary
        ├── opportunities ──────────► scored, sourced growth options
        ├── facts + content ────────► prepared drafts with fact markers
        ├── channels ───────────────► fit and authorization preflight
        ├── experiments ────────────► measured decision per hypothesis
        └── weekly ─────────────────► one reviewable bundle
```

Every stage is a pure function over files. Re-running on the same inputs
produces byte-identical Markdown, which is what makes review and diffing
practical.

## Opportunity intelligence

`opportunities.go` combines two sources:

1. **Derived internal opportunities.** Every record names the file it came from
   (a snapshot or the channel registry). Examples: unobservable traffic,
   missing contribution evidence, a review-latency risk, an unmeasured channel.
2. **Owner-supplied research.** `--research intake.json` accepts dated, sourced
   records. A record is rejected when it is keyword-only, has no `https`
   source, has no retrieval date, repeats the keyword hint, or does not state a
   concrete relationship in at least 80 characters.

Scoring is fixed and published:

```
score = 0.35·relevance + 0.25·evidence + 0.20·reach + 0.10·(100 − risk) + 0.10·confidence
pursue ≥ 70 · research ≥ 45 · skip < 45
```

The report does not authorize publication.

### Research intake schema

```json
{
  "schema_version": 1,
  "records": [
    {
      "id": "EXT-001",
      "title": "Short label",
      "source_url": "https://…",
      "retrieved_at": "2026-11-08",
      "audience": "Who is there",
      "relevance": "A concrete explanation of the real relationship to Derive World, at least 80 characters.",
      "action": "One executable participation step",
      "expected_value": "What it could produce",
      "risk": "What could go wrong",
      "reach": 50,
      "risk_score": 20,
      "confidence": 60,
      "keyword_only": false,
      "keyword_hint": ""
    }
  ]
}
```

## Content intelligence

`content` renders one of seven kinds: `technical-intro`, `release-notes`,
`showcase`, `tutorial`, `community`, `relay`, `product-update`.

Generation uses only facts from `state/facts.json`. Each claim carries a
`[[fact:ID]]` marker. The generator refuses a kind when a required fact is
missing.

`verify-content` enforces:

- every marker resolves to a verified fact;
- at least three facts are cited;
- the draft carries the “not published” notice;
- no production-ready, enterprise-grade, certified, funding, guarantee,
  user-count, star-count, paid-promotion or publication claim;
- any mention of federation is a negation;
- any mention of Real AI states the exact scope or negates fixture confusion;
- any demo or screenshot is labelled Fixture;
- external URLs are allowlisted (`github.com/QiyanAI/derive-world`,
  `docs.github.com`, loopback);
- no secret-shaped string appears.

## Channel intelligence

`state/channels.json` records audience, rules, accepted kinds, cadence,
authorization and observed outcomes for GitHub, Hacker News, Reddit, Product
Hunt, technical blogs and ecosystem lists.

```bash
go run . channels
go run . channels check --channel github --kind technical-intro
go run . channels check --channel hacker-news --kind technical-intro   # exits non-zero
```

`maintainer-controlled` means repository-native actions still subject to
platform rules. `owner-account-required` means the material can be prepared but
the run must not post. `not-authorized` means do not use the channel.

## Experiment engine

Each experiment declares one primary metric, a baseline, a window and a decision
rule. Lifecycle:

```
proposed --start--> running --evaluate--> keep | stop | extend
```

- Evaluation refuses a metric that is `UNKNOWN` in the snapshot.
- Evaluation refuses the same snapshot twice.
- `keep` and `stop` complete the experiment; `extend` keeps it running.
- The weekly report is read-only; it never mutates the ledger.

Supported metrics: `stars`, `forks`, `watchers`, `downloads_total`,
`open_issues`, `open_pull_requests`, `contributors`,
`meaningful_external_issues`, `merged_external_prs`, `showcase_submissions`.

## Growth analytics rules

- Public signals and item-level engagement are separate sections.
- Traffic and contribution values are `UNKNOWN` unless actually present.
- Downloads are never described as users.
- `meaningful_external_issues` excludes spam/invalid/duplicate/wontfix
  labels, one-line drive-bys without conversation, and internal authors.
- `merged_external_prs` counts only merged PRs from non-owner, non-member,
  non-collaborator authors.
- An empty engagement window is not evidence that no contributor exists.

## Permissions and secret handling

- The engine reads files only. It makes no network request.
- `analytics` and `opportunities` read snapshots produced elsewhere.
- `scripts/engagement-export.sh` is the only network-capable artifact; it uses
  the ambient `gh` authentication, is read-only, and prints no token.
- Reports pass through `redact()` before writing.
- The weekly workflow grants `contents: read` only.

## Stop switch and duplicate protection

- `DGE_STOP=1` or `--stop-file PATH` aborts before writing.
- Output files are written atomically and are not overwritten without
  `--force`. A repeated scheduled run for the same snapshot cannot silently
  create a second copy.
