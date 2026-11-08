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

Owner approval is an **Ed25519-signed** artifact. A plain JSON file is not an
approval and is rejected.

### One-time owner key

```bash
go run . approvals keygen --id owner-2026 --private ~/.dge/owner-2026.key \
  --keys state/approval-keys.json --date 2026-11-08
```

The private key is written with mode 0600 outside the repository and is never
printed. The public key is registered in `state/approval-keys.json` (public
data, safe to commit). Keep the private key offline; do not give it to the AI
or to CI.

### Sign one exact payload

```json
{
  "schema_version": 2,
  "approval_id": "unique-id-for-this-post",
  "channel": "devto",
  "action": "publish",
  "content_sha256": "<hash printed by the dry run>",
  "approved_by": "owner name",
  "approved_at": "2026-11-08",
  "expires_at": "2026-11-15",
  "key_id": "owner-2026"
}
```

```bash
go run . approvals sign --file payload.json --key ~/.dge/owner-2026.key \
  --out /secure/approvals/intro-devto.json
go run . approvals verify --file /secure/approvals/intro-devto.json \
  --keys state/approval-keys.json --channel devto --hash <hash>
```

The signature covers a canonical byte string of every payload field. Changing
the channel, hash, approver, dates or key id invalidates it.

Live:

```bash
go run . publish --channel devto --file drafts/ai/intro-devto.md \
  --approval /secure/approvals/intro-devto.json --keys state/approval-keys.json \
  --confirm --date 2026-11-08
```

### Replay and failure recovery

- An `approval_id` is consumed **before** the network call in
  `var/growth-engine/approvals.json`. Reusing it with any content is refused.
- A `pending` publish entry is written before the network call.
- On a network or platform error the entry becomes `unknown`; the approval is
  already consumed. The same content hash cannot be retried until the owner
  reconciles the platform state:

```bash
go run . publish reconcile --channel devto --hash <hash> \
  --external-id <id> --url <url> --date 2026-11-09
# or, if the platform has no post:
go run . publish reconcile --channel devto --hash <hash> --not-published --date 2026-11-09
```

- After `--not-published`, a new signed approval is required for a retry.
- A published or unknown hash is never sent twice automatically.

## Gates

1. **Verification.** The source draft must pass the deterministic verifier
   (facts, claims, Fixture labels, URLs, secret-shaped strings).
2. **Platform.** `automation_policy` must be `api_with_approval` and a posting
   API must exist. Manual platforms are refused with an instruction to post by
   hand.
3. **Account.** The recorded account must be active, carry `publish` scope, name
   the grantor, be unexpired, and record two-factor and a recovery email.
4. **Signature.** A registered, unrevoked Ed25519 public key must verify the
   exact canonical payload; channel, hash, action and expiry must match.
5. **Replay.** The approval id must not appear in the replay ledger.
6. **Deduplication.** A content hash with a pending, unknown or published
   record is refused before any network call.
7. **Credential.** The adapter reads its key from an environment variable; the
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
