# GROW Phase II — AI Growth Operator

The operator is a real model client with a hard spend gate, not a rule script
with an “AI” label. It is model-neutral: the registry speaks two wire protocols
and names no vendor in code.

## Architecture

```
owner budget file ─┐
provider registry ─┼─► ModelClient ─► OpenAI-compatible / Anthropic-compatible endpoint
credential env ────┘        │
                            ├─ reservation → usage ledger (before request)
                            ├─ bounded response (1 MiB, timeout, no redirects)
                            └─ settlement → usage ledger (after request)

public sources ─► research collect ─► untrusted items ─► research analyze
                                                          │ evidence-quote check
                                                          ▼
snapshots ─────► analytics ─────────► opportunities ◄──── AI proposals
                                            │
experiments ────────────────────────────────┼─► strategy (review-only)
                                            │
facts ─────────► content ai ─► deterministic verify ─► draft (never auto-posted)
```

## Provider registry

`state/providers.json` is empty by default. Add entries like these (no keys):

```json
{
  "schema_version": 1,
  "providers": [
    {
      "id": "generic-openai",
      "protocol": "openai",
      "base_url": "https://your-endpoint.example/v1",
      "model": "your-model",
      "api_key_env": "DGE_MODEL_API_KEY",
      "max_output_tokens": 2000,
      "timeout_seconds": 60,
      "json_mode": false,
      "input_price_per_million_usd": 0,
      "output_price_per_million_usd": 0
    },
    {
      "id": "generic-anthropic",
      "protocol": "anthropic",
      "base_url": "https://your-endpoint.example",
      "model": "your-model",
      "api_key_env": "DGE_MODEL_API_KEY",
      "max_output_tokens": 2000,
      "timeout_seconds": 60,
      "json_mode": false,
      "input_price_per_million_usd": 0,
      "output_price_per_million_usd": 0
    }
  ]
}
```

- `protocol: openai` posts to `{base_url}/chat/completions`.
- `protocol: anthropic` posts to `{base_url}/v1/messages`.
- Base URLs must be HTTPS unless they are loopback test endpoints.
- The credential is read from `api_key_env`; it is never stored or printed.
- Prices are optional. Set them to make the cost cap meaningful; zero prices
  still count calls and tokens.

## Budget authorization

A call is refused unless an owner-created budget file is supplied. Example:

```json
{
  "schema_version": 1,
  "authorized_by": "owner name",
  "authorized_at": "2026-11-08",
  "expires_at": "2026-11-15",
  "max_calls": 20,
  "max_input_chars": 200000,
  "max_output_tokens": 40000,
  "max_cost_usd": 1.0,
  "purposes": ["research", "content", "strategy"]
}
```

The file is a spend gate, not a security boundary: the owner controls it and
the credential. It is never committed. The engine refuses on:

- an expired date or an unauthorized purpose;
- a call over `max_calls`;
- input over `max_input_chars` or output over `max_output_tokens`;
- a worst-case estimate over the remaining `max_cost_usd`.

A reservation is written to `var/growth-engine/usage.json` before the HTTP
request; a failed call stays counted.

## Research

`state/sources.json` lists public sources. None is enabled by default. Supported
kinds: `rss`, `atom`, `html`, `github_search`.

```bash
go run . research collect --sources state/sources.json --out reviews/research.json
go run . research analyze --input reviews/research.json --out reviews/research-ai.json \
  --provider generic-openai --budget /secure/dge-budget.json
```

Collect:

- checks `robots.txt` for non-API sources and refuses a disallowed path;
- caps each response at 2 MiB and each source at 20 items;
- records per-source status and never turns a failure into an empty success;
- treats every page as untrusted data.

Analyze:

- sends only truncated item text and verified product facts;
- requires JSON with `source_url` and `evidence_quote`;
- accepts a proposal only when the URL matches a fetched item and the quote is
  an exact substring of that item;
- rejects keyword-only matches and short relevance statements;
- writes proposals to a review file. Nothing is added to state automatically.

The repository contains a real collection from 2026-11-08
(`reviews/2026-11-08-research.json`, 20 public GitHub items). No AI analysis was
run because no budget or credential was authorized.

## Content

```bash
go run . content ai --kind technical-intro --channel github --date 2026-11-08 \
  --provider generic-openai --budget /secure/dge-budget.json --out drafts/ai/intro.md
```

The model must cite `[[fact:id]]` markers. The deterministic verifier remains
authoritative: unknown facts, unsupported claims, missing Fixture labels,
external URLs and secret-shaped strings reject the draft, and a rejected draft
is not written. The output carries provider/model/token/cost metadata and a
“not published” notice. It is never auto-published.

## Strategy

The pipeline can synthesize a review-only plan from metrics, scored
opportunities and experiment records. Every evidence id must exist in the
input; every stop id must name a running experiment. The plan can recommend, but
cannot execute, publish or change the ledger.

```bash
go run . pipeline --snapshot baseline/snapshot-2026-11-07.json --out-dir reviews \
  --provider generic-openai --budget /secure/dge-budget.json
```

## Scheduling

- `growth-daily.yml` runs the deterministic plan daily and uploads a manifest.
- `growth-ai.yml` is skipped unless `DGE_AI_ENABLED=1`. When enabled it writes a
  non-secret provider config from `DGE_AI_PROVIDER_JSON`, a budget from
  `DGE_AI_BUDGET_JSON`, and reads the key from `DGE_MODEL_API_KEY`. It runs the
  plan pipeline and uploads results; it never publishes.
- A local scheduler can run the same commands with a budget file kept outside
  the repository.

## Implemented and observed status

| Capability | Status |
|---|---|
| OpenAI-compatible client | Implemented; tested against a local mock endpoint |
| Anthropic-compatible client | Implemented; tested against a local mock endpoint |
| Budget/call/cost refusal | Implemented and tested |
| Real public research collection | **Actually run** on 2026-11-08 (GitHub search) |
| AI research analysis | Implemented and mock-tested; **no authorized external call yet** |
| AI content generation | Implemented and mock-tested; **no authorized external call yet** |
| AI strategy synthesis | Implemented and mock-tested; **no authorized external call yet** |
| Scheduled AI run | Implemented, disabled by default; needs owner variables/secret |
| Fixed-rule analysis | Remains as the deterministic fallback and verifier |

## What this is not

- Not an autonomous agent with unrestricted tools or self-modified goals.
- Not a system that calls a paid model without a budget file.
- Not a system that publishes without a human approval artifact.
- Not a claim that any external platform result was achieved.
