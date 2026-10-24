# Derive World v0.1.0 — Release Notes

**Status:** public OSS MVP release.
**License:** Apache License 2.0.
**Repository:** <https://github.com/QiyanAI/derive-world>
**Release:** <https://github.com/QiyanAI/derive-world/releases/tag/v0.1.0>

Derive World is an open-source foundation for *Living Objects*: shared,
versioned objects that people and their Personal AI can read, contribute to,
derive from, and trace through an explicit lineage. `v0.1.0` is a
self-hostable OSS MVP, not a production platform.

## What is included

- **Digital Self** accounts with registration, login, session rotation,
  logout, and session-bound CSRF.
- **Living Objects** in two first-class experiences: a versioned collaborative
  document and an interactive poll with vote receipts and owner close.
- **Object authorization** from active membership rows; a caller without
  `read` receives 404, not 403.
- **Versioned actions** with `expected_version`, row locks, and
  `409 version_conflict`.
- **Derive and Lineage**: independent child objects, immutable source
  snapshots, `derivation_edges`, privacy-clipped ancestry, and no parent
  writes.
- **Invitations and Derive Links** with permission bounds, expiry, use counts,
  revocation, atomic acceptance, and permission-overwrite re-invitation.
- **Handoff** projections with a 32 KiB budget, L1–L4 trimming, provenance,
  and no private AI context, conversations, tokens, or other members'
  delegations.
- **Personal AI** with private context, conversations, structured proposals,
  and human-only apply/approve.
- **Trust Kernel** delegation intersection, immediate revocation,
  confirmations, canonical payload hashes, and version rechecks.
- **Capability Broker** with five frozen capabilities: `object.read`,
  `object.context`, `object.propose_change`, `object.derive`, and
  `content.structure`.
- **Open API and TypeScript SDK**: PAT-authorized capability list/detail/invoke
  and invocation status, OpenAPI 3.1, and a typed zero-runtime-dependency SDK.
- **Reference World** in English and Chinese, with two independent users, two
  Personal AIs, invitations, Handoff, proposals, confirmations, history,
  lineage, honest error states, and a 390 px mobile project.
- **Security controls**: Argon2id passwords, SHA-256 token storage, CSRF and
  same-origin checks, `http_hmac_v2` response signing, verified TLS and mutual
  TLS, bounded HMAC key rotation, per-call and daily model budgets with a
  durable pre-call reservation in `model_invocations`, a kill switch, and no
  automatic model retry. The reservation survives a crash, business rollback,
  or failed final update and is counted on restart.

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

Open <http://127.0.0.1:8080>. Fixture mode needs no model account and labels
all AI output as `FIXTURE`; it is not real AI. Real AI mode requires the user to
configure an OpenAI-compatible endpoint and API key, and the user bears the
model provider's costs.

### Platform archives

The release provides these assets:

| Platform | Asset |
|---|---|
| Linux amd64 | `derive-world_0.1.0_linux_amd64.tar.gz` |
| macOS arm64 | `derive-world_0.1.0_darwin_arm64.tar.gz` |
| Windows amd64 | `derive-world_0.1.0_windows_amd64.zip` |
| Source | `derive-world_0.1.0_source.tar.gz` |

Each platform archive contains the server binary, the built web client under
`web/dist`, `capabilities.yaml`, the deterministic fixture data under
`tests/fixtures/model`, `.env.example`, `README.md`, `README.zh-CN.md`,
`SHOWCASE.md`, `LICENSE`, `NOTICE`, `THIRD_PARTY_NOTICES.md`, `SECURITY.md`,
`SECURITY.zh-CN.md`, `TRADEMARK.md`, `CONTRIBUTING.md`, `CONTRIBUTING.zh-CN.md`,
`CODE_OF_CONDUCT.md`, `CHANGELOG.md`, the pinned dependency manifests (`go.mod`, `go.sum`, `package.json`, `package-lock.json`), the
OpenAPI contract, and the bilingual `docs/` tree (including these release notes
and the demo assets). Extract the archive and change into its top-level
directory. The standalone binary reads process environment variables; it does
not load `.env` itself, so export the values before running:

```bash
cd derive-world_0.1.0_linux_amd64
cp .env.example .env
set -a; . ./.env; set +a
./derive-world migrate
./derive-world serve
```

On Windows the binary is `derive-world.exe`; use PowerShell or cmd to export
the variables from `.env`. Some POSIX shells on Windows rewrite absolute paths
such as `MODEL_CHAT_PATH`; leave that variable unset to use the default
`/v1/chat/completions`. The server requires PostgreSQL, the three secrets of at
least 32 bytes, and a `WEB_DIST_DIR` containing `index.html`. The archive
includes `web/dist` and `capabilities.yaml` for that reason. The reference
`content.structure` Provider is a separate process; use Compose for the
complete reference stack.

## Verification

The `v0.1.0` tag is immutable and points at the final public `main` commit.
The Release workflow builds from that exact tag with Go 1.27.3 and Node
24.14.0, builds the binaries and archives twice and compares them
byte-for-byte, creates the source archive and `SHA256SUMS`, writes
`BUILD-MANIFEST.json`, and creates a draft Release only. The draft is published
only after the asset hashes, manifest, permissions, and provenance checks pass.

`SHA256SUMS` on the Release page covers every asset. The workflow generates
GitHub OIDC/Sigstore build-provenance attestations for every archive,
`BUILD-MANIFEST.json`, and `SHA256SUMS`. Verify an asset with:

```bash
gh attestation verify derive-world_0.1.0_linux_amd64.tar.gz \
  --repo QiyanAI/derive-world \
  --source-digest <the tag's commit SHA> \
  --signer-workflow QiyanAI/derive-world/.github/workflows/release.yml
```

SHA-256 proves file integrity; the attestation binds the asset to this
repository, commit, and release workflow identity. The required checks are
`Go`, `Web`, `SDK`, `Architecture`, `Contract`, `Integration`, `govulncheck`,
`gitleaks`, and `E2E`; the release preflight also requires Security `License`
and `npm audit` at the same SHA. `E2E` runs real PostgreSQL, the real Go API,
the production web build, Chromium, Firefox, and a 390 px mobile project with
two independent users. GitHub Dependency Review runs on public pull requests;
GitHub Private Vulnerability Reporting is enabled.

## Known limitations

See [Limitations](https://github.com/QiyanAI/derive-world/blob/main/docs/en/limitations.md)
for the complete list. The main limits are:

- Single instance; no horizontal scaling or real-time collaboration.
- No email, password reset, OAuth, email verification, or account deletion.
- One model adapter per deployment; no concurrent multi-provider routing or
  per-Personal-AI model selection.
- Real AI is VERIFIED_REAL only for the approved two-call API-level run (DeepSeek `deepseek-flash`), not for every model or
  provider; it was not re-run on the release SHA.
- Historical object state has no read endpoint; derive from an older version
  uses immutable snapshots.
- No prebuilt container images; Compose builds from source.
- `v0.1.0` is an OSS MVP, not a production-ready platform; no production use is
  supported or claimed.

## Security

Report vulnerabilities privately through GitHub Private Vulnerability
Reporting: open the repository's **Security** tab and choose **Report a
vulnerability**. Do not open a public issue for a suspected vulnerability. See
[SECURITY.md](https://github.com/QiyanAI/derive-world/blob/main/SECURITY.md).

Read [Security](https://github.com/QiyanAI/derive-world/blob/main/docs/en/security.md)
for the implemented controls, model data flow, and untrusted-content handling.

## Process notes

- `v0.1.0` is immutable. Tags are never moved, deleted, or reused. If a
  candidate changes, create the next version.
- The public repository starts from a clean, DCO-compliant initial commit. The
  pre-public development history is retained in a separate private development
  archive and is not part of this repository's Git history.
- Contributions use the Developer Certificate of Origin 1.1.
- the bounded Real AI evidence was recorded on 2026-10-24. The real-AI evidence is an
  API-level, exactly two-call run to DeepSeek `deepseek-flash` ; it was not re-run on this release and is not a general
  provider claim.
- `v0.1.0` is the first public release.
- Do not reuse release-candidate assets as `v0.1.0` assets. This release is
  rebuilt and attested from its own tag.
