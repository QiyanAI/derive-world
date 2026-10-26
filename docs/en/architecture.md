# Architecture

Derive World is a modular monolith written in Go, with a React reference client,
an ESM SDK, and two independent Go modules for the reference capability provider
and AI-OPS metrics.

## Runtime shape

```text
Browser -> app (net/http, :8080) -> PostgreSQL 17
                         |
                         +-> capability-provider (internal network)
                         +-> OpenAI-compatible model endpoint (egress)
```

The `app` process serves both `/api/v1` and the built web client. There is no
separate frontend server in the reference deployment.

## Module boundaries

```text
web / sdk -> OpenAPI -> app -> {identity, personalai, objects, derive, trust, handoff, capability}
objects -> store
derive -> objects (controlled interface) + store
trust -> identity + objects (read-only) + store
capability -> trust + objects + model (content capabilities only)
personalai -> model + handoff + trust
audit -> store
```

`httpx`, `config`, and `model` depend only on the standard library and their
frozen dependencies. `internal/arch/TestModuleBoundaries` enforces the forbidden
edges, cycle freedom, submodule isolation, and the table-ownership matrix.

## Capability scope

This scope keeps the published `v0.1.0` 22-table schema intact and appends a
version-6 migration for the living-scene object, social, and publication
domains. It activates the Capability Broker, the five frozen capabilities,
`capability_definitions` synchronization, invocation recording, Open API
endpoints, the TypeScript SDK, and the independent `content.structure`
Provider. The Broker composes the object, Handoff,
proposal, derive, Trust Kernel, and confirmation paths; it does not create a
second authorization, object-write, derive, or confirmation mechanism.

The object aggregate is the only writer of `living_objects`,
`object_memberships`, `object_events`, `object_action_receipts`,
`object_proposals`, `invitations`, and `derivation_edges`. `internal/derive`
assembles the child state but writes it only through the objects service and now
shares one preparation/mutation path with the human endpoints. `internal/trust`
owns `delegation_grants` and `confirmations` and is the single AI authorization
decision point. `internal/personalai` owns profiles, private context,
conversations, and messages; it calls objects through a narrow proposal-writer
interface and never writes object state.

The development branch adds three read/write boundaries without a second
object store. `internal/social` owns friendships, blocks, direct human messages,
and object discussions; it never grants object permissions. `internal/publication`
owns publication records, sanitized version snapshots, contributor consents,
public derivation links, and abuse reports; anonymous reads never use the
private object endpoint. `internal/world` is a read-only projection over
authorized objects, derivation edges, and real events for the World Stage and
Living Pulse. `internal/stream` carries only authenticated refresh signals; it
never carries object state or message bodies. `internal/handoff` reads only object
data. `internal/model` owns `model_invocations`. `internal/identity` writes users,
sessions, and PATs. `internal/audit` appends audit rows in the caller's
transaction. `internal/store` owns idempotency reservations.

## HTTP pipeline

```text
RequestID -> SecurityHeaders -> AccessLog -> Recover -> CORS -> BodyLimit
-> ServeMux -> Authenticate -> RateLimit -> LoadResource -> Authorize
-> ValidateSchema -> IdempotencyLookup/Replay -> CheckVersion
-> ExecuteTransaction (business + event + audit + idempotency completion) -> Respond
```

- The access log records the matched route template, never the raw path/query.
- Errors use the closed code set and the unified envelope:
  `{"error":{"code","message","request_id","details"}}`.
- API responses use `Cache-Control: no-store`; hashed static assets are
  immutable; `index.html` is not cached.
- Unknown `/api/*` paths return a JSON 404; non-API paths fall back to the SPA
  shell.
- Authentication accepts the opaque session cookie or, on explicitly allowed
  endpoints, a Bearer PAT with the required scope. Cookie-only endpoints reject
  Bearer authentication. There is no in-process permission cache; membership,
  delegation, and confirmation state are re-read for each object/AI request.
- CSRF binds the header value to a SHA-256 hash stored on the session row and
  also checks the request Origin. PAT requests do not use CSRF but reject a
  mismatched Origin when one is supplied.
- Personal AI effective permission is membership ∩ active delegation ∩
  constraints. Proposal application and derive-confirmation approval recheck
  version, membership, and the exact delegation before writing.
- Idempotency uses a two-phase reservation: an independent transaction inserts
  the key, and the business transaction completes it with the first response.
  Replay happens only after the current request has re-authorized the caller.

## Data direction

Object authorization reads `object_memberships` through the objects service.
The Trust Kernel owns delegation and confirmation decisions and revalidates
membership, the exact delegation, and the object version before AI execution.
Domain modules do not write each other's tables. Cross-module reads use exported
Go interfaces, and cross-module writes go through the owning domain service.
External capability providers never receive database access or user tokens.

## Operational assumptions

`v0.1.0` is single-instance. Rate limiting, cleanup, and scheduling are
in-process. Correctness relies on database constraints for idempotency and
version checks. Horizontal scaling is not supported.
