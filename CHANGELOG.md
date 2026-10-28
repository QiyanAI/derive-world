# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- A capability-composed world experience layer for `open_world_v1`. A world may
  store an optional `experience` document containing a tree of registered
  presentation capabilities (`core.stack`, `core.text`, `core.space`,
  `core.collection`, `core.table`, `core.field`, `core.button`, `core.choice`,
  `core.meter`) and reusable behavior capabilities (`core.state`,
  `core.transfer`). The server validates each module's props and actions,
  executes one bounded pass, and records applied effects; the client renders
  the same document through registered modules. `GET
  /api/v1/world-capabilities` exposes the catalog that drives the product
  palette and generated forms. Adding a module requires no change to the world
  core protocol, and the classic scene/stage/cards/timeline views remain
  selectable for the same world.
- A data-defined world experience runtime for `open_world_v1`: a declarative
  scene (backdrop, hues, horizon, ambient particles, relation style), per-kind
  appearance (shape, size, motion, glow, label, layer), bounded field-to-visual
  bindings, and optional comparison guards on rules. One shared SVG scene
  renders every world; there are no per-world pages, components, or content
  branches. Creators edit the scene, forms, bindings, rule guards, and explicit
  positions through the product, and every change is versioned, persisted, and
  inherited with `structure` on derivation.
- A World Stage with visible objects, real derivation branches, pending human
  approvals, and a Living Pulse built only from real events.
- A third native object type, `living_scene_v1`: structured ideas, steps,
  scenes, choices, and outcomes with versioned add/update/remove/move actions,
  step completion, tags, and scene export.
- A fourth native object type, `open_world_v1`: creators define their own kinds
  of things, the things themselves, labelled relations, and bounded one-pass
  rules. It ships stage, card, and timeline views, server-interpreted effects,
  versioned actions, structure-aware derivation, and publication support.
  Behaviour is data, never executable code.
- A universal Personal AI: one user-level identity is available before the
  first world, keeps private conversations separate per world context, and can
  propose a complete open world through the existing confirmation system. The
  world is created only after a human approves it, and the approval, actor,
  confirmation, and object are recorded together.
- A dedicated `/worlds/:id` free-world experience with real interaction,
  persistence, structure editing, and derivation, plus an explicit AI context
  switch between the current world and the general conversation.
- A version-8 migration extending only the closed type/template/derive and
  confirmation domains; no existing data, table, or history is rewritten.
- A Derive Lens with authorized source, version history, visible branches,
  inheritance fields, and pending AI proposals.
- Friendships, blocks, direct human messages, object discussions, and
  authenticated SSE refresh signals with a polling fallback.
- Opt-in publication of one object version, contributor consent, anonymous
  Explore discovery, public lineage, independent private public-derivation,
  revocation, and basic abuse reports.
- A version-7 migration (31 tables), registered social/publication audit
  actions, privacy-aware account discovery, and user-private World Stage layout
  state. Historical migrations and the published `v0.1.0` tag are unchanged.
- Runtime diagnostics, a non-completion model connection check, and explicit
  daily/hourly/concurrency/global limit reporting in Settings.

### Fixed

- Scrolling inside the World Stage inspector no longer zooms the canvas,
  closes the panel, switches worlds, or drags nodes. The canvas now excludes
  every real stage overlay through an explicit `data-dw-stage-ui` marker, and
  a dedicated pan layer keeps touch gestures separate from native panel
  scrolling on phones and trackpads. Covered by Chromium, Firefox, and mobile
  browser regression tests.

### Security

- Anonymous public reads use only the stored snapshot and never the private
  object endpoint.
- Public derivation re-checks the active publication and derivation permission
  inside the final write transaction.
- Per-user buckets for publication, social writes, friend requests, reports,
  and model checks supplement the global API limits.
- New negative tests cover private-object anonymity, unreadable versions and
  lens data, view-only publication derivation, revocation, contributor consent,
  blocking, and report idempotency.

## [0.1.0] - 2026-10-25

### Added

- Living Objects with immutable, versioned snapshots and explicit ownership.
- Two object experiences in the Reference World, with invitation and Handoff
  flows between Personal AI identities.
- Derive, lineage, event history, and audit views over derived objects.
- Personal AI conversations with proposals, human confirmation, and a
  deny-by-default Trust Kernel.
- A provider-neutral model adapter: deterministic fixture mode by default, or
  one operator-configured OpenAI-compatible provider.
- Capability providers over HTTP with HMAC-signed requests and responses.
- A Go HTTP API, a zero-dependency TypeScript SDK, and an OpenAPI 1.0 contract.
- A bilingual (English and Simplified Chinese) responsive web client.
- Single-host self-hosting with Docker Compose, PostgreSQL 17, and migrations.
- Apache-2.0 licensing, NOTICE, trademark policy, third-party notices, and
  Developer Certificate of Origin contribution checks.

### Security

- Argon2id password hashing, cookie and CSRF protection, and personal access
  tokens with scoped authority.
- Membership and delegation checks on every object action.
- Idempotency, cursor signing, rate limits, and audit records for state changes.
- Provider transport with response signing, TLS verification, key rotation, and
  per-user and deployment-wide model budgets.
- Secret scanning, dependency vulnerability scanning, license checks, and
  reproducible release builds in CI.

### Known limitations

- Single-instance deployment; no horizontal scaling or real-time collaboration.
- One active model adapter per deployment; no multi-provider routing.
- No email, password reset, OAuth, email verification, or account deletion.
- No historical object-state read endpoint; derive from an older version uses an
  immutable snapshot.
- Fixture output is deterministic demo data, not real AI.
- No prebuilt container images; Compose builds from source.

[0.1.0]: https://github.com/QiyanAI/derive-world/releases/tag/v0.1.0
