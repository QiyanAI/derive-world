# Limitations

This is the `v0.1.0` public OSS MVP, the first public
release. The approved two-call API-level dual Personal AI run (DeepSeek `deepseek-flash`) is **VERIFIED_REAL** for that
exact scope; it was not re-run on the release SHA and is not a general
model/provider acceptance. Digital Self sessions, Living Objects, object
authorization, versioned actions, event history, Derive, Lineage, audit,
idempotency, invitations and Derive Links, Handoff, Personal AI conversations,
the Trust Kernel, AI proposals and human confirmations, PATs, self-only export,
the Capability Broker, all five first-release capabilities, the Open API, the
TypeScript SDK, the independent `content.structure` Provider, response-signed
Provider transport, verified TLS, HMAC key rotation, and model budgets are
implemented. This is a self-hostable OSS MVP, not a production platform.

## Implemented in v0.1.0

- Local registration, login, session rotation, logout, and session-bound CSRF.
- Personal AI profile and private context with self-only read/update.
- Collaborative and interactive Living Objects.
- Owner/member authorization from active membership rows; default 404 for
  callers without `read`.
- Explicit `expected_version`, row locks, and `409 version_conflict`.
- Append-only object events with a `seq` cursor.
- Derive from the current version or an immutable historical snapshot.
- Independent child state, regenerated checklist/option ids, zeroed votes, and
  `derivation_edges` lineage.
- Lineage depth limit and privacy clipping for unreadable ancestors/descendants.
- Same-transaction audit, idempotent create/action/derive, and soft delete.
- One-time invitations/Derive Links with permission bounds, expiry, use counts,
  revocation, atomic acceptance, and permission-overwrite re-invitation.
- Member removal with owner protection and immediate 404 effects.
- Handoff projections with a 32 KiB budget, L1–L4 trimming, fixed event summary
  fields, provenance, and no private AI data.
- Private Personal AI conversations and messages, fixture/real model modes,
  one-call-per-turn runtime, structured-action parsing, and model metering.
- Trust Kernel delegation intersection, immediate revocation, confirmation
  create/approve/deny/expiry, canonical payload hashes, and version rechecks.
- AI state-change proposals with human apply/deny and expiration; AI derive only
  through a five-minute human confirmation.
- Personal access tokens with closed scopes and cookie-only endpoint exclusions.
- Self-only `/me/export` with a one-per-hour limit.
- Bilingual Reference World: private context, profile, delegations, invitations,
  Handoff preview, AI chat, proposals, confirmations, tokens, export.
- Capability Broker with configuration-only registration, startup schema
  validation, database synchronization, idempotency, rate/concurrency limits,
  and invocation audit.
- Five frozen capabilities: `object.read`, `object.context`,
  `object.propose_change`, `object.derive`, and `content.structure`.
- PAT-authorized Open API endpoints for capability list/detail/invoke and
  invocation status, plus OpenAPI 3.1 and a typed zero-dependency TypeScript SDK.
- An independent, stdlib-only reference Provider over HTTP+HMAC on an internal
  Compose network, with replay protection, SSRF defenses, and output validation.
- `http_hmac_v2` response signing bound to the request timestamp, invocation id,
  HTTP status, and exact response-body hash; unsigned or tampered responses are
  unknown results and their output is never stored.
- Verified TLS (TLS 1.2 minimum), private CA and mutual TLS support, and startup
  rejection of TLS settings on plaintext Provider URLs.
- Bounded HMAC key rotation with a current key and a previous key with an
  RFC3339 deadline; expired, duplicate, or identical previous keys fail closed.
- Per-call model input/output ceilings, per-user daily budgets, a deployment-wide
  daily hard stop, an operator kill switch, pre-call refusal, audit, and restart
  reseeding from `model_invocations`.
- A blocking Chromium + Firefox Playwright suite with two independent users
  against real PostgreSQL and the real API, plus a 390 px mobile project.

## Not implemented yet

- A general multi-provider acceptance. Real AI is VERIFIED_REAL only for the
  approved two-call API-level run, not for every model or
  provider; it was not re-run on the release SHA.
- Concurrent multi-provider routing, per-user or per-Personal-AI model
  selection, and local model hosting.

The reference client exposes the working surfaces. There are no placeholder
routes that pretend to succeed. Real AI remains a bounded, separately verified
scope.

## Product limits

- Single instance; no horizontal scaling.
- One model adapter is active per deployment. The provider interface and
  deployment configuration are vendor-neutral, but choosing a different model
  still means changing deployment configuration, not running two providers at
  once.
- No simultaneous co-editing. The object workspace uses an authenticated SSE
  refresh signal with a polling fallback; stale writes return a version
  conflict instead of overwriting.
- Friends, direct messages, public worlds, and public derivation are scoped to
  one instance. There is no cross-instance federation or public import.
- No email, password reset, OAuth, email verification, or account deletion in
  `v0.1.0`.
- No standalone product website or native mobile client is provided in `v0.1.0`.
- Fixture mode is deterministic demo output and must never be presented as real
  AI.
- Compose is a reference deployment, not production orchestration.
- The default Compose stack runs the Provider over the internal plaintext
  `capnet` network with signed responses. TLS and mutual TLS are implemented,
  tested, and available through configuration, but are not enabled in the
  default reference stack. Public Providers require HTTPS and `http_hmac_v2`.
- The legacy `http_hmac` transport has no response signature and is retained
  only for internal compatibility.
- Model budgets use in-memory counters for single-instance concurrency, seeded
  from `model_invocations` at the first call of each UTC day. Before a provider
  request, a durable one-call reservation is written to `model_invocations`
  and counted; a crash, business rollback, or failed final update leaves the
  reservation counted, so a possible billable call is not forgotten. Multiple
  app replicas still do not share the in-memory concurrency/counter state; the
  durable rows prevent forgotten charges, not multi-replica coordination.
- Historical object state can be read with `GET /api/v1/objects/{id}/versions/{version}`
  only while the caller currently holds read permission. Derive-from-an-older-
  version uses the same immutable snapshots.
- Revoking a publication stops new public reads and derivations. It cannot
  delete copies that were already downloaded, screenshotted, cached, or
  independently derived.
- GitHub Dependency Review runs on public pull requests as part of the
  release gate. The required checks also cover govulncheck, gitleaks, npm
  audit, and License.
- No prebuilt container images are published; Compose builds from source.
- The `v0.1.0` release assets carry GitHub build-provenance attestations,
  generated by the public release workflow; verify them with
  `gh attestation verify` against this repository, tag, and workflow.

## Data limits

- The published `v0.1.0` has a 22-table schema. This development branch adds a
  version-6 migration with social, publication, scene, report, and public
  derivation tables (30 tables total). Earlier migrations are not rewritten.
- Object state is capped at 64 KiB; `shared_context` at 8 KiB.
- AI proposal input storage is capped at 64 KiB.
  Proposal creation and apply have distinct limits: apply obeys the 64 KiB
  state bound, and `object.state_changed` has a 132,352-byte event-payload
  bound while all other events remain at 96 KiB.
- 20,000 characters is a Unicode **code point** limit, not a byte guarantee.
  Under the owner's V1 interpretation, the
  20,000-code-point body limit and the 65,536-byte whole-state limit apply
  **simultaneously**. 20,000 ASCII, Chinese, or mixed characters that fit the
  state can be applied; 20,000 four-byte Emoji (about 80,000 bytes) exceed the
  state limit and are rejected with `413 payload_too_large` and no side
  effects. 20,001 ASCII is rejected with `400 invalid_request` even though its
  bytes are below 64 KiB.
- Size limits are measured on the PostgreSQL `jsonb` text form. A legal body
  containing `&`, `<`, `>`, or U+2028/U+2029 is not rejected by Go/JSON HTML
  escaping.
- A capability input is capped at 64 KiB; the external Provider's HTTP body
  cap is that input plus the exact 126-byte signed envelope.
- Audit metadata is capped at 4 KiB and rejects nested values.
- Idempotency responses are capped at 256 KiB.
- Backups are manual logical dumps; there is no PITR.
- Deleting the `dw_pgdata` volume destroys local data.

## Honest status

No claim of production readiness, security certification, user adoption, stars,
downloads, or funding is made. This is the first public release. Release evidence is retained in the separate private
development archive; this tree contains the product and release documentation.
The approved two-call API-level dual Personal AI run
(DeepSeek `deepseek-flash`) performed exactly two calls (A 764/51 tokens, B
804/49 tokens; ≈US$0.00030 off-peak, US$0.00059 peak upper bound) and is
recorded as VERIFIED_REAL for that exact scope; it was not re-run on the
release SHA.
