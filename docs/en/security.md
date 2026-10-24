# Security

## Reporting

GitHub Private Vulnerability Reporting is enabled for this public repository.
Use the repository's Security tab and choose Report a vulnerability. Do not
open a public issue for a suspected vulnerability. See `SECURITY.md` for scope
and process.

## Defaults in this build

- The server binds to `127.0.0.1:8080` by default.
- Compose publishes only loopback and keeps PostgreSQL and the reference
  provider on internal networks.
- The `app` and `capability-provider` containers run as UID/GID 10001, with a
  read-only root filesystem, dropped capabilities, and `no-new-privileges`.
  The PostgreSQL and one-shot migration containers use their standard images.
- No production secret is committed. `.env` is ignored.
- Security headers: CSP `default-src 'self'`, `frame-ancestors 'none'`,
  nosniff, no-referrer, and a restrictive Permissions-Policy.
- Request bodies are limited to 1 MiB; JSON unknown fields are rejected.
- Access logs record the route template, a peppered IP hash, and never cookies,
  authorization headers, bodies, or object content.
- Model API keys come only from `MODEL_API_KEY`; they are never stored in the
  database, logged, or sent to the browser.

## Capability and provider controls

- Passwords use Argon2id (`m=64 MiB, t=3, p=2`, 16-byte salt, 32-byte key) and
  are never logged, audited, or returned.
- Session and CSRF tokens are 32-byte random values; only SHA-256 digests are
  stored. Sessions rotate on login and are revoked synchronously on logout.
- Cookie sessions require a session-bound CSRF hash plus a same-origin
  `Origin`/`Referer`. Login and registration reject cross-site requests.
- Every object request loads the object and the caller's active membership in
  one query. No `read` is a 404, never a 403.
- `access_policy` is never consulted for authorization; the database pins
  `visibility` to `private`.
- State actions require `expected_version` and use a row lock plus a conditional
  update. Conflicts write no state and no event.
- Derive requires both `read` and `derive`, reads historical versions only from
  immutable event snapshots, and never writes the parent object.
- Audit rows are written in the same transaction as business changes; audit
  failure rolls the business transaction back. Denial audits use a short
  independent transaction because there is no business change to join.
- Idempotent requests are re-authorized before replay. Losing `read` or deleting
  the object returns 404; losing `derive` while retaining `read` returns 403.
  An old 200 is never replayed to an unauthorized caller.
- A cached Personal AI turn is replayed only while the current membership and
  an active AI delegation still permit object `read`. Exact-delegation binding
  is reserved for execution paths (proposal apply and confirmation approval); a
  read-only cached turn cannot execute anything.
- Object state is validated against the frozen template schema and the 64 KiB /
  8 KiB limits before persistence.
- Invitations and Derive Links use 32-byte random tokens, store only SHA-256,
  default to `read` only, cap permissions at the inviter's current permissions,
  exclude `manage`, enforce expiry (≤30 days) and atomic use counts, and return
  one non-enumerable 404 for missing/revoked/expired/exhausted tokens.
- PATs use the `dw_pat_` prefix, store only SHA-256, enforce closed scopes, and
  are rejected on cookie-only endpoints. A PAT scope is an endpoint ceiling, not
  object authorization.
- Personal AI effective permission is membership ∩ active delegation ∩
  constraints, with human-only permissions removed. There is no permission
  cache; every AI turn and confirmation approval re-reads membership and
  delegation rows.
- AI state changes enter `object_proposals` only after server-side schema and
  permission checks. Humans apply or deny them; AI cannot apply, deny, invite,
  remove members, delete objects, or modify `shared_context`.
- Applying a proposal revalidates the exact delegation recorded on it inside
  the apply transaction. Revoking or expiring that grant blocks the pending
  proposal even when the human keeps `contribute` or another delegation is
  active; the Trust Kernel holds a share lock on the grant until commit.
- Derive requires a five-minute, single-use confirmation bound to a canonical
  payload hash and the expected object version. Approval rechecks human
  membership, the exact AI delegation, and the version before executing.
- Handoff is a read-only projection generated from an object snapshot and
  filtered events. It is never a credential, is not stored, and never reads
  private context, conversations, messages, delegations, tokens, or sessions.
- `/me/export` is self-only and excludes password hashes, token hashes, other
  users' data, and other users' private content.
- Credentials, CSRF values, private context, message content, and AI input are
  never written to object events or audit metadata. Audit metadata is redacted
  by key and limited to 4 KiB.
- Capability definitions are configuration-only and validated at startup.
  Missing, disabled, extra, or re-bound capabilities, invalid schemas, unsafe
  provider URLs, and weak HMAC secrets fail closed before the service starts.
- Every capability call passes one Broker: authentication, PAT scope ceilings,
  object membership, AI delegation, input schema, idempotency, rate/concurrency
  limits, execution, output schema and semantic checks, and audit. There is no
  runtime registration API or caller-supplied provider URL.
- External providers receive only the invocation id, capability id, contract
  version, and validated input. They never receive cookies, PATs, CSRF tokens,
  private AI context, sessions, delegations, or database credentials.
- The outbound client requires an exact allow-list host, resolves and classifies
  every address before each call, rejects loopback/link-local/metadata/CGNAT/
  multicast/reserved ranges, dials only a validated IP, disables redirects and
  environment proxies, and enforces 2 s connect, 5 s header, and 10 s total
  limits.
- Provider requests are signed with HMAC-SHA256 over timestamp + "." + raw body.
  The Provider rejects clock skew over 30 seconds, checks constant-time
  signatures, and deduplicates replays for 300 seconds.
- Provider output is bounded to 256 KiB and must pass JSON Schema and
  capability semantics before it is stored or returned. `timeout` and `unknown`
  are never success; `unknown` never carries output. The Broker never retries
  automatically.
- Capability input summaries are redacted (`input_redacted` ≤2 KiB) and never
  contain text, body, summary, prompt, or title values.

## Real AI and budget controls

- The `http_hmac_v2` Provider transport verifies an HMAC-signed response over
  the request timestamp, invocation id, HTTP status, and SHA-256 of the exact
  response body. Missing, malformed, wrong-key, cross-invocation, replayed, or
  tampered signatures produce an unknown result with no stored output and an
  audit reason of `provider_integrity_failed`.
- Response verification uses constant-time comparison. The request key id is
  echoed by the Provider and must match; the signed response can therefore not
  be moved to another invocation or key.
- Public Providers require `http_hmac_v2` and HTTPS. TLS certificate and
  hostname verification are always enabled (TLS 1.2 minimum). Internal
  Providers may use a configured private CA and an optional client certificate
  for mutual TLS. TLS settings on a plaintext `http://` Provider fail at
  startup.
- HMAC rotation supports a current key id and an optional previous key with an
  RFC3339 deadline. The previous key is accepted only before the deadline and
  is rejected afterwards without a restart. A previous secret equal to the
  current secret, a duplicate key id, or an expired deadline fails startup.
- Model calls are bounded before the provider call by a per-call input ceiling,
  a per-user daily call/character budget, a deployment-wide daily call/
  character hard stop, and an operator kill switch. Real mode forces a positive
  global call cap. Quota refusals are audited as `rate.limit_hit` with a reason
  and make no provider call; the in-memory counters reseed from
  `model_invocations` after a restart.
- The model adapter disables environment proxies, requires TLS 1.2 for HTTPS,
  caps the response body at 1 MiB, clamps output tokens to the configured
  ceiling, maps upstream 429 to `rate_limited`, and never retries a call
  automatically.
- Model vendor neutrality is enforced by construction and by an architecture
  test: the business layer uses only the `Provider` interface, optional
  request-path/JSON/thinking settings live in deployment configuration, and no
  vendor host, model id, or API key is compiled into Personal AI, trust,
  object, derive, Handoff, capability, or audit code. The default
  OpenAI-compatible request is unchanged when the optional profile is not
  enabled. Concurrent multi-provider routing and per-Personal-AI model
  selection are future work, not claimed in `v0.1.0`.

## Not yet implemented

`v0.1.0` is the first public release. The approved
two-call API-level dual Personal AI run (DeepSeek
`deepseek-flash`) completed and Real AI is **VERIFIED_REAL** for that exact
scope; it was not re-run on the release SHA and no general model/provider
acceptance is claimed. The legacy `http_hmac` transport remains available only
for internal compatibility; it does not sign responses and must not be used for
a public Provider.

## Model data flow

In Real AI mode, `AI_INCLUDE_PRIVATE_CONTEXT=true` sends the user's private
context, the current Handoff projection, and recent conversation messages to the
deployment's configured model provider. Provider retention is outside this
system's control; set the flag to `false` to omit private context. Fixture mode
never calls a provider. `MODEL_MAX_INPUT_CHARS`, the daily budgets, and
`MODEL_GLOBAL_DAILY_CALLS` bound the volume and cost of those calls before they
are sent; `MODEL_KILL_SWITCH=true` refuses all of them. Before a provider
request, the runtime writes a durable one-call reservation to
`model_invocations`; the reserved row is counted even if the process crashes,
the business transaction rolls back, or the final record update fails. The
frozen status domain represents the in-flight row as `failed`/`in_flight`; it
is replaced with the final outcome when known.

## Untrusted content

Object content is untrusted data. It is wrapped as
`<untrusted_object_content>` and never treated as an instruction. Only
schema-valid `proposed_actions` enter the proposal/confirmation path, and every
action is re-authorized by the server. Natural-language output and JSON embedded
inside object text cannot create actions.
