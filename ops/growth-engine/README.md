# Growth Engine (DGE)

Offline growth operations and a budget-gated, model-neutral AI Growth Operator
for Derive World.

`ops/growth-engine` turns the read-only snapshots produced by
`ops/github-metrics`, verified product facts, dated public research, a channel
registry, an account registry and an experiment ledger into weekly reports,
sourced opportunities, prepared content and review-only AI plans. It is an
independent Go module with zero dependencies.

Phase II adds real model calls through a provider registry
(`openai`-compatible or `anthropic`-compatible), a spend authorization file, a
usage ledger, public research collection with robots.txt and evidence-quote
validation, the Channel Account Manager, and approval-gated publish adapters
for DEV Community and GitHub Discussions.

## Defaults and safety

- **Zero paid calls by default.** No provider is configured in
  `state/providers.json`. A call requires `--provider`, a valid owner budget
  file and a matching credential environment variable.
- **Budget before request, cumulative across runs.** Purpose, call count,
  input size, output size and worst-case cost are checked before the HTTP
  request. A reservation is committed to a shared git state branch first, so a
  fresh Actions runner or a crash cannot reset or hide a possibly billable call.
- **Cost cap is only claimed when it is real.** A hard cost cap requires both
  provider prices to be positive. Missing pricing is reported as
  `cost cap NOT enforced`, never as a spend ceiling; such a run requires
  `allow_without_cost_cap: true`.
- **No silent publishing.** The pipeline is plan-only. Live publishing needs an
  active account record, `publish` scope, a passing verification, a content-hash
  approval **signed with the owner's Ed25519 key**, a single-use approval id and
  a deduplication/recovery ledger.
- **Manual platforms stay manual.** Hacker News, Reddit and Product Hunt have
  no automated publisher; material is prepared for a human.
- **No credentials in state.** Passwords, one-time codes, recovery codes and
  API keys are never read, requested or stored. Model keys come from an
  environment variable named in the provider registry.
- **Untrusted input.** Fetched pages are data, never instructions. AI research
  proposals are accepted only when their evidence quote is an exact substring
  of the fetched item.
- **No estimates.** Unavailable values stay `UNKNOWN`; downloads are not users.

## Commands

Run from `ops/growth-engine`:

```bash
go run . facts verify
go run . analytics --snapshot baseline/snapshot-2026-11-07.json
go run . opportunities --snapshot baseline/snapshot-2026-11-07.json
go run . channels
go run . content --kind technical-intro --channel github
go run . verify-content --file drafts/technical-intro-github.md
go run . experiments list
go run . weekly --snapshot baseline/snapshot-2026-11-07.json --out-dir reports
go run . tour-check

# Phase II
go run . research collect --sources state/sources.json --out reviews/research.json
go run . research analyze --input reviews/research.json --out reviews/research-ai.json \
  --provider <id> --budget <owner-budget.json>
go run . content ai --kind technical-intro --channel github --date 2026-11-08 \
  --provider <id> --budget <owner-budget.json>
go run . accounts list
go run . accounts prepare --channel github
go run . accounts check --channel github
go run . accounts preflight --channel github --scope publish
go run . accounts owner-todo --out docs/GROW-P2-OWNER-TODO.md
go run . approvals keygen --id owner-2026 --private ~/.dge/owner-2026.key \
  --keys state/approval-keys.json --date 2026-11-08
go run . approvals sign --file payload.json --key ~/.dge/owner-2026.key --out approval.json
go run . approvals verify --file approval.json --keys state/approval-keys.json \
  --channel devto --hash <hash>
go run . publish --channel devto --file drafts/ai/technical-intro-devto.md --date 2026-11-08
go run . publish --channel devto --file ... --approval approval.json \
  --keys state/approval-keys.json --confirm --date ...
go run . publish reconcile --channel devto --hash <hash> --not-published --date ...
go run . pipeline --snapshot baseline/snapshot-2026-11-07.json --out-dir reviews \
  --provider <id> --budget <owner-budget.json> --usage-git-dir .state
```

`publish` is a dry run by default. `--confirm` with a valid approval artifact
performs the network action. AI commands refuse before the request when the
budget is absent, expired, out of purposes, out of calls or over the cost cap.

## Inputs

| Input | Owner | Notes |
|---|---|---|
| `baseline/snapshot-*.json` | `ops/github-metrics` | Schema v1. Never re-collected here. |
| `state/facts.json` | this module | Every statement names files and exact substrings; `facts verify` fails if a source stops containing the quote. |
| `state/channels.json` | this module | Audience, rules, accepted content kinds and authorization per channel. |
| `state/platforms.json` | this module | Dated platform rules, registration requirements, owner-only steps, API policy. |
| `state/accounts.json` | this module | Non-secret account state, scopes, security facts, owner steps. |
| `state/sources.json` | this module | Public research sources; none enabled by default. |
| `state/providers.json` | this module | Model-neutral provider registry; empty by default. |
| owner budget file | owner | Spend authorization; never committed. See GROW-P2-AI-OPERATOR. |
| `state/experiments.json` | this module | Hypothesis, metric, baseline, window and decision rule per experiment. |
| engagement export | `scripts/engagement-export.sh` | Optional item-level public issue/PR metadata. |

### Engagement export

The daily metrics workflow owns aggregate collection. Item-level contribution
evidence is a separate, optional read-only export:

```bash
REPO=QiyanAI/derive-world bash scripts/engagement-export.sh /tmp/engagement.json
go run . analytics --snapshot baseline/snapshot-2026-11-07.json --engagement /tmp/engagement.json
```

The export contains public issue/PR numbers, titles, author logins,
association, state, merge flag, comment count, body length and labels. It
contains no private repository data and no credentials. Classification rules
live in `analytics.go`; a one-line drive-by comment is not counted as a
meaningful contribution.

## Outputs

- `reports/<date>-weekly.md` — the reviewable weekly report.
- `reports/<date>-weekly.json` — the same bundle for automation.
- `reports/<date>-analytics.json` — net change and data quality.
- `drafts/<kind>-<channel>.md` — prepared, fact-marked drafts. Each one starts
  with a "not published" notice and lists the facts it used.

Committed reports and drafts are examples produced from the real
2026-11-07 baseline; they are not claims of growth.

## Safety model

- **Authorization is data.** `channels check --channel X --kind Y` fails for a
  channel that needs an owner account, even when the content fits.
- **Facts are executable evidence.** A draft may only use a fact whose sources
  still exist and still contain the quoted text. The verifier also rejects
  production, certification, funding, user-count, star-count, guarantee,
  federation and publication claims, unknown fact markers, external URLs
  outside an allowlist, and secret-shaped strings.
- **Experiments cannot succeed on `UNKNOWN`.** Evaluation refuses a metric that
  the snapshot cannot observe, and refuses to evaluate the same snapshot twice.
- **No silent caps.** The weekly report lists unavailable data and everything
  the run did not do.
- **Stop switch.** `scripts/stop.sh` creates `var/growth-engine/STOP`, which
  every command checks by default. `DGE_STOP=1` or `--stop-file PATH` also
  abort. `scripts/resume.sh` removes the marker.

## Automation

`.github/workflows/growth-weekly.yml` runs after the daily metrics workflow is
available, downloads the latest `github-metrics` artifact, optionally exports
item-level public metadata with the read-only `GITHUB_TOKEN`, runs `weekly`, and
uploads the report as an artifact with a step summary. It has `contents: read`
only, no schedule overlap with the daily collection, and a hard timeout.

It does not commit to the repository and does not create issues or discussions.
Enabling any external channel remains an explicit owner action.

`growth-daily.yml` runs the plan-only pipeline daily against the previous
metrics artifact and uploads a run manifest. `growth-ai.yml` is a disabled-by-
default AI schedule: it runs only when the repository variable
`DGE_AI_ENABLED=1`, a non-secret provider configuration variable and a
call/cost budget variable are set, and the model key is a repository secret.
It never publishes. It has `contents: write` only to clone and push the
`dge-state` cumulative-usage branch created once with
`scripts/init-state-branch.sh`; the product main branch is unchanged.

## Tests

```bash
go test ./... -count=1
go vet ./...
gofmt -l .
```

The suite verifies real facts against the repository, the committed example
world, the static tour, engagement classification, experiment state transitions,
secret redaction, path safety, stop-switch behaviour and report determinism.
Phase II adds mock-provider tests for both wire protocols, budget and expiry
refusal, cost caps, redirect refusal, robots.txt, feed/HTML/GitHub parsing,
evidence-quote validation, AI draft verification, account preflight and audit,
approval-gated DEV/GitHub publishing, duplicate suppression, and the pipeline's
no-publish boundary.

## Documentation

- [GROW-00 audit](docs/GROW-00-AUDIT.md)
- [Operator guide](docs/GROW-03-OPERATOR.md)
- [Developer outreach](docs/GROW-04-DEVELOPER-OUTREACH.md)
- [Derive Relay](docs/GROW-05-DERIVE-RELAY.md)
- [14-day experiment](docs/GROW-06-EXPERIMENT.md)
- [Phase II AI operator](docs/GROW-P2-AI-OPERATOR.md)
- [Phase II accounts](docs/GROW-P2-ACCOUNTS.md)
- [Phase II publishing](docs/GROW-P2-PUBLISHING.md)
- [Owner action list](docs/GROW-P2-OWNER-TODO.md)
- [Enablement steps](docs/GROW-P2-ENABLEMENT.md)

```bash
go test ./... -count=1
go vet ./...
gofmt -l .
```

The suite verifies real facts against the repository, the committed example
world, the static tour, engagement classification, experiment state transitions,
secret redaction, path safety, stop-switch behaviour and report determinism.

## Documentation

- [GROW-00 audit](docs/GROW-00-AUDIT.md)
- [Operator guide](docs/GROW-03-OPERATOR.md)
- [Developer outreach](docs/GROW-04-DEVELOPER-OUTREACH.md)
- [Derive Relay](docs/GROW-05-DERIVE-RELAY.md)
- [14-day experiment](docs/GROW-06-EXPERIMENT.md)
