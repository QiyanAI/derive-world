# GROW-00 — Baseline audit and implementation boundary

- Repository: `QiyanAI/derive-world`, branch `feat/dge-growth-engine`
- Base commit: `96991c5` (`release: Derive World v0.2.0`)
- Product version: `v0.2.0`, schema 11, 36 tables
- Audit date: 2026-11-07 (baseline captured 06:48:25Z)
- Method: repository reading plus the existing read-only metrics artifact.
  Nothing in this record is estimated; absent data is `UNKNOWN`.

## Real baseline

Source: `ops/github-metrics` artifact, copied to
`ops/growth-engine/baseline/snapshot-2026-11-07.json`.

| Signal | Value | Quality |
|---|---|---|
| Stars | 0 | VERIFIED |
| Forks | 0 | VERIFIED |
| Watchers | 0 | VERIFIED |
| Open issues | 3 | VERIFIED (derived from `open_issues_count` minus open PRs) |
| Open pull requests | 8 | VERIFIED |
| Contributors | 1 | VERIFIED |
| Releases | 2 (`v0.1.0`, `v0.2.0`) | VERIFIED |
| Release asset downloads, cumulative | 35 | VERIFIED, but includes maintainers and CI |
| Traffic views, 14 days | UNKNOWN | Requires repository administration read |
| Traffic clones, 14 days | UNKNOWN | Same |
| Showcase submissions | UNKNOWN | No item-level export was available |
| Real users | UNKNOWN | Not derivable from stars, forks or downloads |

These are public signals. The baseline does **not** establish adoption, active
users, retention or channel performance.

## Reusable modules (do not rebuild)

| Area | Where | What already exists |
|---|---|---|
| Daily metrics | `ops/github-metrics`, `.github/workflows/metrics-daily.yml` | Schema-v1 snapshot, diff, Markdown report, UNKNOWN handling, artifact upload, 30-day retention |
| Showcase submission | `SHOWCASE.md`, `.github/ISSUE_TEMPLATE/showcase.yml` | Voluntary GitHub-native form with Fixture/Real AI labels and a public-safety checklist |
| Product demo assets | `docs/assets/` | Real product screenshots and the 17-second Fixture recording |
| Publication and derivation | `internal/publication`, `internal/derive`, `/api/v1/public/worlds/*` | Opt-in publication, public derivation, permission-clipped lineage, single-instance scope |
| Free-world creation | `internal/worldseed`, `internal/objects/openworld_*`, `internal/personalai` | Natural-language creation, composition, reimagine, human approval |
| Security controls | `internal/arch`, `.github/workflows/security.yml`, `.gitleaks.toml` | Module boundaries, frozen capabilities, secret scanning, license checks |
| Release evidence | `.github/workflows/release.yml`, release assets | Checksums, build manifest, attestation |
| SDK and API examples | `sdk/typescript`, `examples/sdk-node`, `examples/capability-provider` | Typed client, real-request example, external Provider |
| Third-party notices | `THIRD_PARTY_NOTICES.md` | Generated dependency inventory |

## Modules that needed completion

- **Presentation**: the README first screen is accurate but leads with
  infrastructure; the required headline and the three product differences were
  not expressed together. Showcase assets predate the v0.2.0 free-world flow.
- **Low-barrier entry**: the only path to see the product was Docker Compose.
  There was no static tour and no offline artifact.
- **Growth analytics**: the snapshot counts open issues and PRs but cannot
  classify them; contribution and showcase evidence was `UNKNOWN`.
- **Channel knowledge**: no committed registry of audiences, rules or
  authorization state.
- **Experiment discipline**: no ledger of hypothesis, metric, baseline and
  decision rule.
- **Derivation challenge**: no reproducible base world, prompts or participation
  entry point.

## Modules that did not exist

- `ops/growth-engine`: opportunity scoring, content preparation and
  verification, channel preflight, experiment state machine, weekly report.
- Weekly automation that reuses the daily metrics artifact rather than
  collecting a second time.
- A static, offline product tour.
- A CI-validated example world for a derivation challenge.

## GROW stage gap map

| Stage | Have | Build | Hard constraints |
|---|---|---|---|
| GROW-01 presentation | Accurate README, SHOWCASE, assets | Headline block, three differences, tour link, relay link, v0.2.0 labels | No claim beyond v0.2.0; no fixture-as-real-AI |
| GROW-02 entry | Docker path, screenshots | Offline tour with labelled simulation, deployment notes | No paid hosting, no domain purchase, no false URL |
| GROW-03 operator | Snapshots, metrics report | Offline engine, scoring, drafts, verification, weekly run | No paid model calls, no auto-posting, no secrets |
| GROW-04 outreach | SDK, API docs, examples | Two-audience material, channel strategy, preflight | Owner authorization per external channel |
| GROW-05 native | Publication, derivation, lineage | Reproducible base world, relay prompts, showcase link | Single instance; no federation claim; no fake submissions |
| GROW-06 automation | Daily metrics workflow | Weekly analysis workflow, 14-day plan, stop switch | Read-only permissions; no duplicate collection |

## Constraints confirmed in code and docs

- Single instance: `docs/en/limitations.md` (“Single instance; no horizontal
  scaling”) and (“There is no cross-instance federation or public import”).
- Fixture is not real AI: `README.md`, `docs/en/limitations.md`.
- Human approval: `README.md` (“only a human can approve”), proposal and
  confirmation code in `internal/trust` and `internal/objects`.
- Frozen capability set: `capabilities.yaml` (five IDs, configuration-only).
- Module boundaries: `internal/arch/arch_test.go` (`TestModuleBoundaries`).
- Independent module isolation: the same test asserts zero requirements for
  `ops/github-metrics` and `examples/capability-provider`; this change adds
  `ops/growth-engine` to that list.
- Secret scanning: `.gitleaks.toml`, `.github/workflows/security.yml`.
- Release integrity: `.github/workflows/release.yml`.

## UNKNOWN data

- 14-day traffic views and clones.
- Unique visitors, active users, retention, and conversion from view to use.
- Whether any external person actually ran a release archive.
- Item-level issues and PRs at audit time (no export was supplied).
- Showcase submissions and their reproducibility.
- Results of any external channel, because none was used under this
  authorization.

Every one of these stays `UNKNOWN` in reports until a real observation exists.

## Proposed boundary for this implementation

1. Never modify the product runtime, schema, capabilities, permissions or
   approval model.
2. Reuse the daily metrics artifact; add only a weekly analysis job.
3. Keep the growth operator offline, deterministic and zero-cost by default.
4. Prepare content and plans; do not post without owner authorization.
5. Make every claim traceable to a repository path or a measured signal.
