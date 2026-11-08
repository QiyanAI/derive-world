# GROW Phase II — Approval-gated publishing

Publishing is the highest-risk action in the system. It is therefore a
separate command with three independent gates: platform policy, account
preflight and an owner approval bound to one exact payload.

## Flow

```
verified draft
   │ prepareForPublication: remove notice, fact markers and review appendix
   ▼
title + body → SHA-256
   │
   ├─ dry run: print hash and blockers; send nothing (default)
   ├─ owner writes approval JSON with that hash
   └─ --confirm: policy + account preflight + approval + dedup → adapter
```

Dry run:

```bash
go run . publish --channel devto --file drafts/ai/intro-devto.md --date 2026-11-08
```

The output includes `content_sha256`, whether the account is ready, whether an
approval is valid and `would_publish: false`. Nothing is sent.

Approval:

```json
{
  "schema_version": 1,
  "channel": "devto",
  "action": "publish",
  "content_sha256": "<hash printed by the dry run>",
  "approved_by": "owner name",
  "approved_at": "2026-11-08",
  "expires_at": "2026-11-15"
}
```

Live:

```bash
go run . publish --channel devto --file drafts/ai/intro-devto.md \
  --approval /secure/approvals/intro-devto.json --confirm --date 2026-11-08
```

## Gates

1. **Verification.** The source draft must pass the deterministic verifier
   (facts, claims, Fixture labels, URLs, secret-shaped strings).
2. **Platform.** `automation_policy` must be `api_with_approval` and a posting
   API must exist. Manual platforms are refused with an instruction to post by
   hand.
3. **Account.** The recorded account must be active, carry `publish` scope, name
   the grantor, be unexpired, and record two-factor and a recovery email.
4. **Approval.** Channel, action, hash and expiry must match. The hash covers
   the prepared title and body, not the review scaffolding.
5. **Deduplication.** A content hash already published to the channel is
   refused before any network call.
6. **Credential.** The adapter reads its key from an environment variable; the
   engine never prints it.

## Adapters

### DEV Community

- Environment: `DEVTO_API_KEY`.
- Endpoint: `POST https://dev.to/api/articles`.
- Test override: `DGE_DEVTO_API_BASE` (must be HTTPS or loopback).
- Payload: title, markdown body, published flag, tags, optional canonical URL.

### GitHub Discussions

- Environment: `DGE_GITHUB_TOKEN`, `DGE_GITHUB_REPO` (owner/name),
  `DGE_GITHUB_DISCUSSION_CATEGORY_ID`.
- Test override: `DGE_GITHUB_API_BASE`.
- Flow: resolve the repository id, then `createDiscussion` with the approved
  title and body.
- A token needs the minimum scope that can create a discussion; never use a
  broad development token.

## Manual platforms

Hacker News, Reddit and Product Hunt are `manual_only` in
`state/platforms.json`. The engine prepares material and lists owner steps; it
does not implement login, submission, CAPTCHA or vote flows.

## Audit and deduplication

- `var/growth-engine/published.json` stores channel, content hash, external id,
  URL and date. It is runtime state and is not committed.
- `var/growth-engine/audit.jsonl` receives one append-only JSON line per publish
  and per account record.
- A failed network call writes no ledger entry.
- The approval is single-payload: changing one character changes the hash and
  invalidates it.

## Authorization boundaries

- The engine never registers an account, buys promotion, asks for votes, sends
  direct messages, or comments on unrelated projects.
- First public posts require explicit owner approval by design.
- Platform rules can change; `state/platforms.json` is dated and must be
  re-checked by a human before a new channel is used.
- The publishing adapters are implemented and mock-tested. No live post was
  made, because no account, credential or approval was provided.
