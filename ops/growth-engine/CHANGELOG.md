# Growth Engine changelog

## 1.1.0 — 2026-11-08

DGE Phase II: a real, model-neutral AI Growth Operator with hard spend gates,
public research, channel account management and approval-gated publishing.

- Model-neutral provider registry for OpenAI-compatible and Anthropic-compatible
  endpoints, with HTTPS enforcement, timeouts, no redirects, bounded responses
  and no vendor names in code.
- Owner budget file gate: purpose, call count, input/output size and worst-case
  cost are checked before each request; a reservation is written first.
- Public research collection (RSS, Atom, HTML, GitHub search) with robots.txt,
  response caps, per-source status and untrusted-data framing.
- AI research proposals require an exact evidence quote from a fetched item;
  hallucinated sources are rejected. Proposals are review-only.
- AI content generation must cite verified fact markers and pass the existing
  deterministic verifier; rejected drafts are not written.
- AI strategy synthesis with evidence-id validation; review-only.
- Channel Account Manager with dated platform rules, account kits, security
  checklists, owner-only steps, preflight and append-only non-secret audit.
- Approval-gated DEV Community and GitHub Discussions publishers, content-hash
  approvals, duplicate suppression and a runtime ledger.
- Plan-only pipeline with stage manifest, usage delta, stop switch and no
  publish path.
- Daily deterministic workflow and a disabled-by-default AI workflow.
- 30 new tests covering providers, budgets, research, accounts, publishing and
  the pipeline.

No paid external model call was made and no content was published in the
implementation of this version.

## 1.0.0 — 2026-11-08

First implementation of the Derive World Growth Engine (DGE) under the V1.0
task book.

- GROW-00 audit record with the real 2026-11-07 baseline and an explicit
  `UNKNOWN` list.
- GROW-01/02: README first-screen positioning, three product differences, a
  static offline five-step tour with a labelled simulation, and honest labels.
- GROW-03: deterministic analytics, sourced opportunity scoring, fact-verified
  content preparation and verification, channel registry with authorization
  preflight, experiment state machine, weekly bundle, stop switch and secret
  redaction.
- GROW-04: developer and creator tracks, channel strategy, prepared drafts and
  a human publication checklist. No external post was made.
- GROW-05: a CI-validated reproducible base world and the Derive Relay
  challenge, built only on existing publication and derivation capabilities.
- GROW-06: a weekly analysis workflow that reuses the daily metrics artifact,
  a 14-day experiment plan, and an explicit data-quality separation.

The engine is offline and zero-cost by default. It does not call a model
provider, does not publish, and does not modify product data.
