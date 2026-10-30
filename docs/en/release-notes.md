# Derive World v0.2.0 — Release Notes

**Status:** public OSS release, V2 product stage.
**License:** Apache License 2.0.
**Repository:** <https://github.com/QiyanAI/derive-world>
**Release:** <https://github.com/QiyanAI/derive-world/releases/tag/v0.2.0>

Derive World is an open-source foundation for *Living Objects*: shared,
versioned objects that people and their Personal AI can read, contribute to,
derive from, and trace through an explicit lineage.

`v0.2.0` is the second public release and the V2 product stage. A user describes
a world in their own words; their Personal AI proposes a concrete plan; only a
human can approve it; the server then creates a real, runnable world that the
user can enter, keep changing in natural language, and derive into independent
new worlds. It is a self-hostable OSS release, not a production platform.

## What is new in v0.2.0

- **One-sentence world creation.** Free intent is the input. The Personal AI
  writes a compact world brief; a server-side composer expands it into real
  content, state and experience. The proposal is validated before it is stored,
  is shown as one inline card under the reply that produced it, and creates
  nothing until the human approves it.
- **Blank-world bootstrap.** An empty world can start from a natural-language
  request without hand-configuring schemas, capabilities or JSON.
- **Continuous modification.** A bound Personal AI reads the current world
  state and version, proposes one mergeable change (`batch_change` or
  `reshape_world`), and applies it only after approval. Each applied change
  increments the version exactly once, keeps existing thing ids, survives a
  refresh, and conflicts are rejected instead of overwriting.
- **Derivation.** A world can be derived into an independent child with correct
  source and version lineage. The parent is not modified.
- **Multi-form worlds.** One world can combine a time-based 2D live scene, a
  readable interactive story, an editable workspace, a visual space, uploaded
  images and video, uploaded or procedural audio with event cues, a real 3D
  scene with client-side gravity and collisions, a bounded declarative custom
  surface, and tabs that combine them. All surfaces share one content, state,
  variable, permission, version and lineage model.
- **World Stage.** The authenticated home shows real objects, visible
  derivation branches, the latest real event, pending human approvals and the
  Living Pulse instead of a synthetic dashboard.
- **Friends and direct communication.** Friend requests, direct messages,
  blocks, object discussions and object invitations. Friendship never grants
  object access by itself.
- **Explore and publication.** Owners publish one object version with a
  license and an optional derivation permission. Anonymous visitors see only
  the stored, sanitized snapshot. Contributor consent is required when other
  people contributed to that version. Revocation stops new public reads.
- **One Personal AI.** A user-level Personal AI exists before the first world,
  keeps separate private conversations per world context, and exposes one
  primary AI entry instead of competing operation windows.
- **Media with real boundaries.** Uploaded images, video, audio and binary glTF
  models are permission-scoped, validated by content, served from this server,
  and never enter object state as bytes. The release does not claim automatic
  generation of arbitrary media, video or 3D works.
- **Schema 11 (36 tables).** Migration 11 links every AI-originated
  confirmation and proposal to its conversation and exact assistant message.
  Migrations apply forward from `v0.1.0`; back up before upgrading.

## Real-model evidence and its exact scope

Real AI is an operator-configured mode that calls an OpenAI-compatible
provider with the operator's key. The operator bears the provider's costs.
Fixture mode calls no model and is always labelled `FIXTURE`; fixture output is
not real AI.

The principal real-model evidence was recorded on the release candidate
commit `4962b9b` (tree `7c1bb11`). The tagged `v0.2.0` tree differs from that
candidate by documentation updates and release fixes (a publication-consent
check for privately derived content, bounded GIF/GLB validation, preservation of
omitted values in a partial world change, and invitation revocation when a
member is removed); the real-model flows were **not** re-run on the exact
tagged commit, and the Release page names the shipped commit and tree. The
evidence is:

- **Three consecutive natural-language modifications in one world**, each
  producing one proposal, human approval, exactly one version increment,
  retained thing ids, and refresh persistence. The third turn received an empty
  provider response and the server's automatic clean-context retry succeeded
  without clearing the conversation.
- An earlier owner-authorized recheck on the pre-final code (`288057e`) covered
  one-sentence creation, AI derivation with an unchanged parent, approval
  without raw JSON, and multi-proposal conflict handling. That recheck also
  recorded a partial case where two provider responses were empty and one reply
  produced more proposals than could be applied; the extra proposals were
  refused with `409`, not applied.

The owner's personal acceptance on 2026-10-30 used the release binary and
exercised creation, modification and `world.reimagine`. During that session the
provider sometimes returned empty content; the server retried with clean
context and succeeded. Malformed world seeds were rejected safely with readable
errors and could be retried.

This is evidence for these flows on this tree with DeepSeek `deepseek-flash`.
It is not a general model or provider acceptance. Strict validation can still
refuse a malformed model plan; the refusal is safe and retryable. No paid model
calls are made by the release build, CI, or any test.

## Install and run

### Docker Compose (recommended)

Requirements: Docker Engine with Compose v2, about 2 GB of free disk, and a
browser. The service binds to loopback only.

```bash
git clone https://github.com/QiyanAI/derive-world.git
cd derive-world
cp .env.example .env
docker compose up --build -d
curl -fsS http://127.0.0.1:8080/readyz
```

Open <http://127.0.0.1:8080>. The default `.env` uses fixture mode, which needs
no model account. To use real AI, set `MODEL_MODE=real` and configure your
provider's base URL, model name and API key. The key stays in the server
environment; it is never sent to the browser.

On Windows, the supported complete local path is a source checkout with Docker
Desktop and the double-click launcher: see
[Windows self-hosting](windows.md). The released Windows zip contains the
server binary and assets, not PostgreSQL or the Compose stack.

### Platform archives

| Platform | Asset |
|---|---|
| Linux amd64 | `derive-world_0.2.0_linux_amd64.tar.gz` |
| macOS arm64 | `derive-world_0.2.0_darwin_arm64.tar.gz` |
| Windows amd64 | `derive-world_0.2.0_windows_amd64.zip` |
| Source | `derive-world_0.2.0_source.tar.gz` |

Each platform archive contains the server binary, the built web client under
`web/dist`, `capabilities.yaml`, deterministic fixture data, `.env.example`,
the bilingual README, license and governance files, the dependency manifests,
the OpenAPI contract, and the bilingual `docs/` tree. The binary reads process
environment variables; it does not load `.env` itself.

```bash
cd derive-world_0.2.0_linux_amd64
cp .env.example .env
set -a; . ./.env; set +a
./derive-world migrate
./derive-world serve
```

The server requires PostgreSQL, three secrets of at least 32 bytes each, and a
`WEB_DIST_DIR` containing `index.html`. The complete reference stack, including
the separate `content.structure` provider, is provided by Compose.

## Verification

The `v0.2.0` tag is immutable and points at the final public `main` commit. The
Release workflow builds from that exact tag with the pinned Go and Node
toolchains, builds the binaries and archives twice and compares them
byte-for-byte, creates the source archive and `SHA256SUMS`, writes
`BUILD-MANIFEST.json`, and creates a draft Release only. Publishing is a
separate, explicit Owner action.

The required checks at the tagged commit are `Go`, `Web`, `SDK`,
`Architecture`, `Contract`, `Integration`, `govulncheck`, `gitleaks`, `E2E`,
`License` and `npm audit`. E2E runs real PostgreSQL, the real Go API, the
production web build, Chromium, Firefox and a 390 px mobile project.

Verify a downloaded asset with:

```bash
gh attestation verify derive-world_0.2.0_linux_amd64.tar.gz \
  --repo QiyanAI/derive-world \
  --source-digest <the v0.2.0 tag commit SHA> \
  --signer-workflow QiyanAI/derive-world/.github/workflows/release.yml
```

`SHA256SUMS` covers every asset. SHA-256 proves file integrity; the
attestation, when present, binds the asset to this repository, commit and
workflow identity.

## Known limitations

See [Limitations](limitations.md) for the complete list. The main limits are:

- Single instance; no horizontal scaling or real-time multiplayer
  collaboration.
- No email, password reset, OAuth, email verification, or account deletion.
- One model adapter per deployment; no concurrent multi-provider routing or
  per-Personal-AI model selection.
- Real AI is verified only for the flows and provider named above, not for
  every model or provider. Strict validation may refuse a malformed plan; the
  refusal is safe and retryable.
- Media support is bounded: uploaded files with size and per-world/per-user
  budgets, simple client-side physics, and a declarative custom surface. It is
  not arbitrary media or 3D generation.
- An anonymous Explore preview shows a metadata summary, not a live composed
  experience.
- Friends and publication work on one reachable instance; a loopback address
  (`127.0.0.1`) is not an external sharing link.
- `v0.2.0` is an OSS release, not a production-ready platform; no production
  use is supported or claimed.

## Security

Report vulnerabilities privately through GitHub Private Vulnerability
Reporting: open the repository's **Security** tab and choose **Report a
vulnerability**. Do not open a public issue for a suspected vulnerability. See
[SECURITY.md](https://github.com/QiyanAI/derive-world/blob/main/SECURITY.md)
and [Security](security.md).

## Process notes

- Release tags are immutable. A published tag is never moved, deleted or
  reused; a changed candidate becomes the next version.
- Contributions use the Developer Certificate of Origin 1.1. The post-merge
  push-range DCO check is authoritative.
- The release is built and attested from its own tag. Release-candidate assets
  are never reused as release assets.
- `v0.2.0` is the V2 product-stage release. `v0.1.0` remains available and
  unchanged; its notes are preserved in
  [release-notes-v0.1.0.md](release-notes-v0.1.0.md).
