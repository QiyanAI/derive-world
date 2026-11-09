# GROW-06 — Automation and the first 14-day experiment

## Automation layout

| Cadence | Job | What it does |
|---|---|---|
| Daily | `.github/workflows/metrics-daily.yml` (existing) | Collects the schema-v1 snapshot and renders the metrics report as an artifact. Unchanged. |
| Weekly | `.github/workflows/growth-weekly.yml` (new) | Downloads the latest metrics artifact, optionally exports item-level public issue/PR metadata, runs `growth-engine weekly`, uploads the bundle and writes a step summary. |
| Manual | `growth-engine experiments` | Starts or evaluates an experiment against a real snapshot. Never scheduled. |

The weekly job does not collect the aggregate snapshot a second time. It does
not commit, open issues, comment or publish.

Permissions: `contents: read`, `issues: read`, `pull-requests: read`.
Timeout: 15 minutes. Concurrency: one weekly run. A missing or stale metrics
artifact is reported, not invented.

Artifact reliability: Growth Daily, Growth Weekly and Growth AI resolve the
latest successful `metrics-daily` run, then download its `github-metrics`
artifact with at most three attempts and bounded backoff (5s, 15s). If the
artifact is still unavailable, the job fails closed; it never substitutes the
committed 2026-11-07 baseline. The actual `source_run_id`,
`snapshot_captured_at`, `traffic_quality` and `contributors_quality` are written
to `data-provenance.json` and the step summary beside the report.

Stop switch: set the repository variable `DGE_STOP` to `1`, or pass
`--stop-file`. The engine aborts before writing.

## First experiment: framing and participation

**Window:** 14 days, starting on the date the owner records with
`experiments start`.

**Baseline (2026-11-07T06:48:25Z):** 0 stars, 0 forks, 1 contributor,
8 open pull requests, 35 cumulative release downloads, 0 showcase submissions
in the available export, traffic `UNKNOWN`.

### Questions the experiment must answer

1. Which introduction makes the product understandable fastest?
2. Which developers become genuinely interested?
3. Can a visitor explain what a Living Object is after the first screen?
4. Will anyone try the offline tour or the Compose path?
5. Will anyone share a derived world voluntarily?
6. Does a real external contributor appear?
7. Which channel, if any, deserves another round?

### Experiments in the ledger

| ID | Change | Metric | Keep rule | Stop rule |
|---|---|---|---|---|
| EXP-01 | README first screen and Showcase entry | `stars` | Δ ≥ 1 | Δ ≤ −1 |
| EXP-02 | Publish Derive Relay and link it | `showcase_submissions` | Δ ≥ 1 | Δ ≤ −1 |
| EXP-03 | One technical introduction on an authorized developer channel | `meaningful_external_issues` | Δ ≥ 1 | Δ ≤ −1 |

`extend` is the outcome for any delta between the rules. An inconclusive result
is not a success.

### Guardrails

- No paid promotion, no purchased stars or votes, no fake accounts, no mass
  messages.
- No experiment changes product behaviour for real users.
- No model spend is required; the engine is offline.
- Any external post needs owner authorization and a channel preflight.
- One change per experiment; keep the repository framing stable during the
  window.

## Weekly reporting

```bash
go run . weekly \
  --snapshot baseline/snapshot-2026-11-07.json \
  --before previous.json \
  --engagement /tmp/engagement.json \
  --out-dir reports
```

The report separates:

- **Verified data**: deltas present in snapshots.
- **Derived data**: totals computed from verified rows (for example cumulative
  downloads).
- **UNKNOWN**: traffic, item-level engagement, or any value the inputs do not
  contain.
- **Hypotheses**: explicitly labelled, never presented as results.

## Decision discipline

Automated tick (evaluates every running experiment, skips `UNKNOWN` metrics,
and writes a decision):

```bash
go run . experiments tick --snapshot snapshot.json --date 2026-11-21 --write
```

Or evaluate one experiment explicitly:

```bash
go run . experiments evaluate --id EXP-01 --date 2026-11-21 \
  --snapshot snapshot.json --write
```

- `keep`: the measured delta clears the keep rule; continue the approach for
  one more round.
- `stop`: the measured delta hits the stop rule; stop the approach and record
  why.
- `extend`: the result is inconclusive; extend once, and do not add more
  channels at the same time.

No experiment is marked successful without a measured delta. No channel is
scaled before it has a recorded observation.

## Expected deliverable after 14 days

A short report that answers the seven questions with one of three labels per
question: measured, inconclusive, or not observable. The next round is built
only from measured or clearly framed inconclusive results.
