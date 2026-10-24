# API

The canonical contract is `api/openapi/openapi.yaml` (OpenAPI 3.1.0). The running
service serves it at `/api/v1/openapi.yaml`. The API implements Digital Self
sessions, Living Objects, object authorization, versioned actions, event history,
Derive, Lineage, invitations/Derive Links, Handoff, Personal AI conversations,
the Trust Kernel, AI proposals/confirmations, PATs, self-only export, the
Capability Broker, all five first-release capabilities, the OpenAPI contract,
the TypeScript SDK boundary, the independent `content.structure` Provider,
response-signed Provider transport, verified TLS, HMAC key rotation, and model
budgets.

## System endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` | Liveness. Returns `ok` without touching the database. |
| GET | `/readyz` | Readiness. Returns 200 only when PostgreSQL is reachable, otherwise 503. |
| GET | `/api/v1/meta` | Build version, commit, build time, and `model_mode`. |
| GET | `/api/v1/openapi.yaml` | This contract. |

## Object endpoints

| Method | Path | Authentication | Notes |
|---|---|---|---|
| POST | `/api/v1/auth/register` | Public | Creates user, Personal AI profile, private context, default delegation, and session in one transaction. |
| POST | `/api/v1/auth/login` | Public | Rotates an existing presented session and issues a new one. |
| POST | `/api/v1/auth/logout` | Session + CSRF | Revokes the current session. |
| GET | `/api/v1/auth/me` | Session or PAT | Current user and Personal AI summary. |
| GET | `/api/v1/objects` | Session or PAT (`objects:read`) | `scope=owned\|shared\|all`, signed cursor pagination. |
| POST | `/api/v1/objects` | Session or PAT (`objects:write`); CSRF only for cookie sessions | Requires `Idempotency-Key`; creates the owner membership. |
| GET | `/api/v1/objects/{id}` | Session or PAT (`objects:read`) | 404 when the caller has no active membership. |
| DELETE | `/api/v1/objects/{id}` | Session + CSRF | Owner-only soft delete; version is unchanged. |
| GET | `/api/v1/objects/{id}/events` | Session or PAT (`objects:read`) | `cursor` is the last event `seq`. |
| POST | `/api/v1/objects/{id}/actions` | Session or PAT (`objects:write`); CSRF only for cookie sessions | Requires `Idempotency-Key` and `expected_version`. |
| GET | `/api/v1/objects/{id}/members` | Session or PAT (`objects:read`) | Active members; emails are never returned. |
| POST | `/api/v1/objects/{id}/derive` | Session or PAT (`derive:create`); CSRF only for cookie sessions | Requires `Idempotency-Key`; parent is never written. |
| GET | `/api/v1/objects/{id}/lineage` | Session or PAT (`objects:read`) | Depth 1–10, privacy-clipped ancestors/descendants. |
| GET | `/api/v1/objects/{id}/handoff` | Session or PAT (`objects:read`) | Bounded, permission-trimmed projection; never a credential. |
| POST/GET | `/api/v1/objects/{id}/invitations` | Session | Create/list invitations; create requires `Idempotency-Key`. |
| DELETE | `/api/v1/objects/{id}/invitations/{invitation_id}` | Session | Revoke; `invite` may revoke only its own, `manage` any. |
| GET | `/api/v1/invitations/{token}` | Session | Preview title, permissions, expiry, remaining uses; no state. |
| POST | `/api/v1/invitations/{token}/accept` | Session | Atomic use count and exact permission overwrite. |
| DELETE | `/api/v1/objects/{id}/members/{user_id}` | Session | Owner-only removal; owner cannot be removed/leave. |
| GET | `/api/v1/objects/{id}/proposals` | Session | Caller's own AI proposals only. |
| POST | `/api/v1/objects/{id}/proposals/{proposal_id}/apply` | Session | Human application; proposal state is idempotent. |
| POST | `/api/v1/objects/{id}/proposals/{proposal_id}/deny` | Session | Human denial. |
| GET/PATCH | `/api/v1/ai/profile` | Session | Self-only Personal AI profile. |
| GET/PUT | `/api/v1/ai/context` | Session | Self-only private context, ≤8 KiB. |
| GET/POST | `/api/v1/ai/conversations` | Session | Private conversations; create requires `Idempotency-Key`. |
| GET/DELETE | `/api/v1/ai/conversations/{id}` | Session | Owner-only; delete is soft. |
| POST | `/api/v1/ai/conversations/{id}/messages` | Session | One provider call; requires `Idempotency-Key`. |
| GET/POST | `/api/v1/ai/delegations` | Session | Self-only delegation grants; no `manage`/`invite`. |
| DELETE | `/api/v1/ai/delegations/{id}` | Session | Immediate revocation. |
| GET/POST | `/api/v1/tokens` | Session | PAT metadata/create; plaintext returned once. |
| DELETE | `/api/v1/tokens/{id}` | Session | Immediate PAT revocation. |
| GET | `/api/v1/confirmations` | Session | Pending confirmations by default. |
| GET | `/api/v1/confirmations/{id}` | Session | Safe projection; expiry is 410. |
| POST | `/api/v1/confirmations/{id}/approve` | Session | Requires `Idempotency-Key`; revalidates and executes. |
| POST | `/api/v1/confirmations/{id}/deny` | Session | Deny a pending confirmation. |
| GET | `/api/v1/me/export` | Session | Self-only JSON export, one per hour. |

## Capability endpoints

| Method | Path | Authentication | Notes |
|---|---|---|---|
| GET | `/api/v1/capabilities` | Session or PAT (`capabilities:invoke`) | Enabled descriptors only; never provider addresses or secrets. |
| GET | `/api/v1/capabilities/{capability_id}` | Session or PAT (`capabilities:invoke`) | One enabled descriptor. |
| POST | `/api/v1/capabilities/{capability_id}/invoke` | Session or PAT (`capabilities:invoke` + operation scope) | Requires `Idempotency-Key`; all execution passes through the Broker. |
| GET | `/api/v1/capability-invocations/{id}` | Session or PAT (`capabilities:invoke`) | Actor-only, with current object `read`. |

### Capability invocation

The request body is exactly the capability's frozen input schema. There is no
`provider_url`, `callback_url`, or any other caller-supplied destination.

```http
POST /api/v1/capabilities/content.structure/invoke
Authorization: Bearer dw_pat_...
Idempotency-Key: 018f3f2f-8b7d-7a58-9f11-2f5f5f7a9b21
Content-Type: application/json

{"object_id":"018f3f2f-8b7d-7a58-9f11-2f5f5f7a9b21","text":"# Title\n\nBody","format":"markdown"}
```

A synchronous success returns the validated output:

```json
{
  "id": "018f3f30-1c2d-7e4f-8a90-123456789abc",
  "capability_id": "content.structure",
  "status": "succeeded",
  "output": {
    "items": [
      {"index": 0, "kind": "heading", "text": "Title", "level": 1},
      {"index": 1, "kind": "paragraph", "text": "Body"}
    ],
    "item_count": 2,
    "truncated": false,
    "format": "markdown"
  },
  "duration_ms": 7,
  "created_at": "2026-10-23T00:00:00Z",
  "completed_at": "2026-10-23T00:00:00.007Z"
}
```

`object.propose_change` returns `{"proposal_id":"...","status":"pending"}` and
does not modify the object. Proposal input is limited to 64 KiB of canonical
JSON, matching the object-state ceiling. Apply is
governed by the 64 KiB object state and, for `object.state_changed`, the
132,352-byte event payload; all other events remain
at 96 KiB, and the capability input itself remains 64 KiB. A proposal can
therefore be created successfully while apply returns 413 for a body that fits
the 64 KiB proposal input but cannot fit the 64 KiB state (for example a
20,000-character mixed body of 65,467 UTF-8 bytes); the object is then
unchanged. Both V1 limits are cumulative: the body is at most 20,000 Unicode
code points **and** the complete state is at most 65,536 bytes. 20,000
four-byte characters (about 80,000 bytes) are already rejected at proposal
creation by the 64 KiB input cap. A human applies or denies it through the
existing proposal endpoints.

An AI `object.derive` invocation returns `202` while a human confirmation is
pending:

```json
{
  "id": "018f3f31-2d3e-7f50-9b01-123456789abc",
  "capability_id": "object.derive",
  "status": "pending",
  "confirmation_id": "018f3f32-3e4f-7a61-8c12-123456789abc",
  "created_at": "2026-10-23T00:00:01Z"
}
```

Failures use the standard error envelope. Execution failures include the
recorded invocation id and never carry an output:

```json
{
  "error": {
    "code": "capability_unknown",
    "message": "capability result is unknown",
    "request_id": "018f...",
    "details": {"invocation_id": "018f3f30-1c2d-7e4f-8a90-123456789abc"}
  }
}
```

### Capability authorization

- The server derives the actor from the session or PAT. Body fields such as
  `actor_user_id`, `provider_id`, or `permissions` are rejected.
- A PAT must carry `capabilities:invoke` and the operation scope:
  `objects:read` for `object.read`, `object.context`, and `content.structure`;
  `objects:write` for `object.propose_change`; `derive:create` for
  `object.derive`. A scope is a ceiling, never object authorization.
- Object membership and the capability's required permission
  (`read`, `contribute`, or `derive`) are checked on every call. No permission
  cache exists, so revocation takes effect on the next request.
- Personal AI uses the same Broker. Its effective permissions are the
  intersection of the user's membership, the active delegation, and the
  capability contract. AI derive creates a five-minute, single-use confirmation
  bound to the invocation, actor, object, version, and canonical input.

### Limits and failure semantics

- For a truncated `object.context` projection, each trimmed `data_provenance`
  entry carries `"truncated": true` (optional); the root `truncated` flag remains
  authoritative. The marker is only present when that field was actually trimmed.
- 64 KiB input, 256 KiB output, 10 s total execution, 2 s connect, 5 s response
  headers.
- 60 calls per hour per user and 2 concurrent calls per user.
- `pending` is the only non-terminal state. `succeeded` requires a fully
  validated output. `timeout` and `unknown` are never shown as success, and
  `unknown` never carries output. The Broker does not retry; start a new call
  with a new `Idempotency-Key`.
- Definitions are synchronized from `capabilities.yaml` at startup. There is no
  runtime registration API.

### External Provider protocol

The Broker calls `POST /v1/invoke` with a fixed envelope and HMAC-SHA256
signature over `timestamp + "." + raw_body`. The Provider ID, capability ID,
contract version, invocation ID, timestamp, and signature travel in
`X-DW-*` headers. The Provider rejects clock skew over 30 seconds and replays
within 300 seconds. See [Provider deployment](providers.md).

## Authentication and CSRF

- Registration and login set two cookies: `dw_session` (HttpOnly) and
  `dw_csrf` (readable by the browser).
- Every unsafe request authenticated by cookie must send `X-CSRF-Token` equal to
  the `dw_csrf` cookie value and an `Origin` (or `Referer`) matching
  `PUBLIC_BASE_URL`.
- Login and registration require `Origin`/`Referer` same-origin when present and
  reject `Sec-Fetch-Site: cross-site`.
- Session tokens are 32-byte random values; only SHA-256 digests are stored.
- Login rotates sessions. Logout revokes immediately.
- On explicitly allowed endpoints, `Authorization: Bearer dw_pat_...` authenticates
  a PAT. The token must carry the endpoint scope and still passes object
  membership. `/ai/*`, invitations, proposals, confirmations, tokens, export,
  logout, member deletion, and object deletion are cookie-only.
- PAT requests do not require CSRF, but a supplied `Origin` must match.

## Authorization and existence hiding

Every object path first loads the object and the caller's active membership in
one query. A missing object, a soft-deleted object, and a caller without `read`
are all `404 not_found`. A caller with `read` but without the required
`contribute`, `derive`, or `manage` permission receives `403 forbidden`.

`access_policy` is policy configuration only. It is never an authorization
source; the database pins `visibility` to `private`.

Applying a pending AI proposal revalidates the AI authorization recorded on the
proposal inside the apply transaction. If that delegation was revoked or
expired, or the caller's current AI effective permission no longer includes
`contribute`, apply returns `403 forbidden` and records an `authz.denied` audit
event with reason `delegation_not_active`; no object state is written. A
different active delegation does not stand in for the revoked original. An
already applied proposal still replays idempotently, and denied or expired
proposals still return `409 conflict`.

## Versioning

- A new object starts at `version=1`.
- Each accepted state action increments the version exactly once.
- `expected_version` is mandatory for actions. A mismatch returns
  `409 version_conflict` without writing state or an event.
- Memberships and soft deletes do not change the version.
- Derive does not change the parent version and writes no parent event.

## Idempotency

The eight idempotent endpoints are `POST /objects`, `POST /objects/{id}/actions`,
`POST /objects/{id}/invitations`, `POST /objects/{id}/derive`,
`POST /ai/conversations`, `POST /ai/conversations/{id}/messages`,
`POST /capabilities/{id}/invoke`, and `POST /confirmations/{id}/approve`.
Each requires `Idempotency-Key` (`^[A-Za-z0-9_-]{16,128}$`). The request hash
binds the method, actual path (including object/confirmation ids), and
canonical JSON body. Proposal apply uses proposal status instead of a key.

- Same key and same body after success: the first status and body are replayed
  with `Idempotency-Replayed: true`.
- Same key with a different body: `409 idempotency_conflict`.
- Concurrent identical request while the lease is held: `409 request_in_progress`.
- Replay after losing `read` or deleting the object: `404`; losing `derive`
  while retaining `read`: `403`. An old success is never replayed to an
  unauthorized caller.
- Records expire 24 hours after completion; an abandoned reservation can be
  taken over after its 10-minute lease.

## Errors

```json
{
  "error": {
    "code": "not_found",
    "message": "not found",
    "request_id": "018f...",
    "details": {}
  }
}
```

The closed code set and HTTP mapping are frozen by the API contract §A.4.5:
`invalid_request` (400), `unauthenticated` (401), `forbidden` (403),
`not_found`/`invitation_invalid` (404), `conflict`/`version_conflict`/
`idempotency_conflict`/`request_in_progress` (409),
`confirmation_expired` (410), `payload_too_large` (413),
`unsupported_media_type` (415), `rate_limited`/`budget_exceeded` (429),
`model_unavailable`/`model_invalid_output`/`capability_failed`/
`capability_unknown` (502), `model_timeout`/`capability_timeout` (504),
`capability_denied` (403), and `internal` (500). `confirmation_required` is a
202 outcome, not an error.

## Conventions

- Media type: `application/json; charset=utf-8`.
- IDs: UUIDv7, lowercase with hyphens.
- Times: RFC 3339 UTC.
- Request ID: `X-Request-Id` is accepted if it matches a short safe token;
  otherwise a UUIDv7 is generated and echoed.
- API responses are `no-store`.
- Request bodies are limited to 1 MiB; unknown JSON fields are rejected.
- Object state is limited to 64 KiB; `shared_context` is limited to 8 KiB.
- CORS is off by default. If `CORS_ALLOWED_ORIGINS` is set, only exact origins
  are echoed and credentials are never allowed.

## Contract testing

`TestOpenAPI` loads and validates the embedded document with kin-openapi and
compares its paths and methods to the registered route table. A route added
without a contract entry fails CI.
