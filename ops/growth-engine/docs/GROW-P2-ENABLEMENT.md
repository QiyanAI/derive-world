# DGE enablement — exact owner steps

This is the shortest path from the current safe defaults to a running AI
operator. Nothing here has been executed by the implementation: no paid model
call was made, no account was registered and no content was published.

## 1. Enable cross-run usage state (required for scheduled AI)

The AI workflow keeps cumulative calls and cost in a dedicated `dge-state`
branch. Create it once from an authenticated owner machine:

```bash
bash ops/growth-engine/scripts/init-state-branch.sh
```

The script creates an orphan branch containing only `usage.json` (no secrets,
no product code). The AI workflow clones that branch and commits a reservation
before each model request. If the branch is missing the workflow fails with a
clear message; it never starts from a zero counter silently.

The workflow needs `contents: write` to push this state branch. The main branch
protection is unchanged and no product code is written by the workflow.

## 2. Register an owner approval key (required for automated publishing)

```bash
cd ops/growth-engine
go run . approvals keygen --id owner-2026 \
  --private ~/.dge/owner-2026.key \
  --keys state/approval-keys.json --date 2026-11-08
```

The private key stays outside the repository with mode 0600. The public key in
`state/approval-keys.json` is the only key that can authorize a post.

## 3. Configure a model provider (non-secret)

Add one provider to `state/providers.json` using an OpenAI-compatible or
Anthropic-compatible endpoint. Supply **both** input and output prices per
million tokens if you want a hard cost cap. Example:

```json
{
  "id": "owner-provider",
  "protocol": "openai",
  "base_url": "https://your-endpoint.example/v1",
  "model": "your-model",
  "api_key_env": "DGE_MODEL_API_KEY",
  "max_output_tokens": 2000,
  "timeout_seconds": 60,
  "json_mode": false,
  "input_price_per_million_usd": 0.50,
  "output_price_per_million_usd": 1.50
}
```

## 4. Authorize a cumulative budget

Create a budget file outside the repository (or put its JSON in the repository
variable for the workflow). A cost cap requires positive prices.

```json
{
  "schema_version": 1,
  "authorization_id": "dge-2026-11",
  "authorized_by": "owner",
  "authorized_at": "2026-11-08",
  "expires_at": "2026-11-15",
  "max_calls": 20,
  "max_input_chars": 200000,
  "max_output_tokens": 40000,
  "max_cost_usd": 1.0,
  "allow_without_cost_cap": false,
  "purposes": ["research", "content", "strategy"]
}
```

If prices are missing, set `max_cost_usd: 0` and
`allow_without_cost_cap: true`. The engine will enforce call/token limits but
will state that no cost cap is enforced.

## 5. Run one local AI plan (no publishing)

```bash
cd ops/growth-engine
DGE_MODEL_API_KEY=... go run . pipeline \
  --snapshot baseline/snapshot-2026-11-07.json --out-dir reviews \
  --provider owner-provider --budget /secure/dge-budget.json \
  --usage var/growth-engine/usage.json
```

Confirm the report says `Hard cost cap enforced: true` only when prices were
provided. The plan contains no publish action.

## 6. Enable the scheduled AI workflow (optional)

Set repository variables:

- `DGE_AI_ENABLED=1`
- `DGE_AI_PROVIDER_ID=owner-provider`
- `DGE_AI_PROVIDER_JSON=<the full ProviderFile JSON>` (non-secret)
- `DGE_AI_BUDGET_JSON=<the budget JSON>` (non-secret)
- `DGE_MODEL_API_KEY` as a repository secret

`growth-ai.yml` then runs the plan pipeline daily, uses the `dge-state` branch
for cumulative usage, and uploads a review artifact. It never publishes.

## 7. First publish (only after reviewing a draft)

1. Dry run and copy the printed hash.
2. Write the approval payload with a unique `approval_id`.
3. Sign it with the owner key.
4. Run `publish ... --confirm` with `--keys` and `--approval`.
5. If the result is `unknown`, reconcile with the platform id/url or
   `--not-published` before any retry.

No approval file is valid without a signature from the registered public key.

## Honest state after these steps

- Model calls are real only after steps 3–6.
- Publishing is real only after steps 2 and 7, with an owner-signed approval.
- Accounts, CAPTCHA, 2FA and platform onboarding remain owner-only.
- Hacker News, Reddit and Product Hunt remain manual.
