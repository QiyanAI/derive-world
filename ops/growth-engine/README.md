# Growth Engine (DGE)

Deterministic, offline growth operations for Derive World.

`ops/growth-engine` turns the read-only snapshots produced by
`ops/github-metrics`, verified product facts, a channel registry and an
experiment ledger into weekly reports, sourced opportunities and prepared
content drafts. It is an independent Go module with zero dependencies.

## What it does not do

- It never calls a model provider and produces no paid spend.
- It never posts, messages, comments, opens issues, or submits to a directory.
- It never reads or prints credentials. Reports are redacted before they are
  written.
- It never estimates a missing value. Unavailable data is `UNKNOWN`.
- It never treats stars, downloads or forks as active users.
- It never creates or modifies Derive World product data.

External publication remains a human decision on an authorized account. The
engine prepares material and refuses to treat an owner-only channel as
permitted.

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
```

Every command accepts `--out PATH` where it renders Markdown or JSON, and
`--json` where a machine-readable form exists. `weekly` requires `--out-dir`,
refuses to overwrite an existing report without `--force`, and honours
`--stop-file` or `DGE_STOP=1` as a local stop switch.

## Inputs

| Input | Owner | Notes |
|---|---|---|
| `baseline/snapshot-*.json` | `ops/github-metrics` | Schema v1. Never re-collected here. |
| `state/facts.json` | this module | Every statement names files and exact substrings; `facts verify` fails if a source stops containing the quote. |
| `state/channels.json` | this module | Audience, rules, accepted content kinds and explicit authorization per channel. |
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
- **Stop switch.** `DGE_STOP=1` or an existing `--stop-file` aborts before any
  report is written.

## Automation

`.github/workflows/growth-weekly.yml` runs after the daily metrics workflow is
available, downloads the latest `github-metrics` artifact, optionally exports
item-level public metadata with the read-only `GITHUB_TOKEN`, runs `weekly`, and
uploads the report as an artifact with a step summary. It has `contents: read`
only, no schedule overlap with the daily collection, and a hard timeout.

It does not commit to the repository and does not create issues or discussions.
Enabling any external channel remains an explicit owner action.

## Tests

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
