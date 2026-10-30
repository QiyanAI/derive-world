# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-10-30

### Changed

- Human approvals now live directly under the assistant reply that produced
  them, in both the general Personal AI dialog and the in-world Personal AI.
  The card shows the real plan preview and one button, plus honest preparing,
  pending, creating, created, failed and expired states. A created world links
  to its real entry after a refresh; a failed or expired plan never offers a
  fake approval and instead explains the outcome and names the recovery action.
  A derive confirmation still names the source version it is bound to. While a
  world plan waits, a duplicate-plan rejection is explained instead of offering
  a retry that cannot succeed.
- The server links every AI-originated confirmation and proposal to its
  conversation and exact assistant message (migration 11), records the object an
  approval created, backfills that link for already-approved worlds, and rejects
  a second pending world plan in one conversation. Chat replies that claim an
  execution happened, or that point at a card somewhere else, are replaced by
  truthful server-authored text before they become history; saved legacy rows
  are corrected at display time.
- A short textual approval such as "批准通过" is routed to the real confirm
  button instead of spending a model turn or preparing a duplicate plan. The
  approval ledger on Digital Self and the home dashboard link to the
  originating conversation or world panel instead of rendering a second button.

### Added

- Real world media for `open_world_v1`: permission-scoped uploads of images,
  video, audio and binary glTF models; content-based validation (magic bytes,
  image re-encoding, GLB chunk and resource bounds, explicit media allowlists);
  byte-range streaming; per-world and per-user durable budgets; audit actions;
  and a deterministic procedural image provider behind the same validation,
  quota and audit path. A file's bytes never enter object state.
- `core.media`, `core.audio` and `core.scene3d` capabilities. The same world can
  show real images and video, play an ambient file or procedural tone with
  variable-driven cues, and render its own things as a 3D scene with camera
  movement, GLB models, gravity, jumping and box/sphere collisions. Clicking a
  thing runs the same behavior path as every other surface; physics transforms
  and the player position are client state, while behavior-driven changes are
  versioned and persisted.
- Asset references are state-derived: save-time validation checks active
  membership, existence and media kind; derivation copies only references in
  the inherited state; publications freeze exactly the referenced assets and
  include their uploaders in consent. Removing an in-use asset is rejected
  until a versioned experience change clears the reference.
- A version-9 migration adds `assets`, `object_asset_refs` and
  `publication_asset_refs` plus the `asset.upload` and `asset.remove` audit
  actions. Images are decoded and re-encoded to strip metadata and trailing
  payloads; GLB files must carry an embedded BIN chunk and no external or data
  URIs. Video and audio are stored byte-for-byte; their embedded metadata is a
  documented boundary.
- The web client adds lazy-loaded three.js 3D rendering, Web Audio and HTML
  audio playback with a user-gesture gate, image/video surfaces, an asset
  upload/generation panel and descriptor-driven asset pickers. The page CSP
  permits same-origin media plus the blob URLs three.js uses for GLB textures,
  while asset responses carry a stricter sandbox policy.

- Free-intent world creation and reshaping for `open_world_v1`. The Personal AI
  writes a compact world brief; a server-side composer (`internal/worldseed`)
  expands it into a complete, validated world with kinds, things, relations,
  interactions, shared variables and an experience document. Create bodies may
  use local `ref` slugs that the server remaps to minted UUIDs, so a complete
  experience can be installed at creation. A bound Personal AI uses
  `world.reimagine` to merge a brief into the current world; the server creates
  an atomic `reshape_world` proposal that a human applies.
- Universal world surfaces: `core.live` (time-based animation, walkable space,
  or a playable collect/score/reach/survive game), `core.story` (chapters as
  real things, choices as real links, persisted reading position),
  `core.workspace` (editable tables, row creation, status and summaries),
  `core.space`, `core.custom` (a safe declarative surface with shapes, text,
  inputs, keys, drag, conditions, actions, timelines and procedural tones) and
  `core.tabs` to combine surfaces in one world. World variables (<=128, <=8 KiB)
  back scores, reading positions and custom state; `core.create` adds bounded
  entities. `custom_action` interprets one validated custom event on the server.
- Describe-first product entry: the create dialog asks what to create and sends
  the user's own words to the Personal AI; the old type chooser is demoted to
  an advanced blank-creation control. A world can be changed by natural
  language from the world page and branched in one step with a visible source
  chain. Fixture output remains labelled demo data; real-model acceptance
  remains a separate, explicitly authorized gate.

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

[0.2.0]: https://github.com/QiyanAI/derive-world/releases/tag/v0.2.0
[0.1.0]: https://github.com/QiyanAI/derive-world/releases/tag/v0.1.0
