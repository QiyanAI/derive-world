# Derive World v0.1.0 — Release Notes

**Status:** release candidate; the `v0.1.0-rc.1` label is planned, **not
created**. No Git tag or GitHub Release exists.
**License:** Apache License 2.0.
**Repository:** private until the public-release gate; no GitHub Release has
been published.

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
all AI output as `FIXTURE`; it is not real AI.

### Platform archives

The release builds three archives:

| Platform | Asset |
|---|---|
| Linux amd64 | `derive-world_0.1.0-rc.1_linux_amd64.tar.gz` |
| macOS arm64 | `derive-world_0.1.0-rc.1_darwin_arm64.tar.gz` |
| Windows amd64 | `derive-world_0.1.0-rc.1_windows_amd64.zip` |

Each archive contains the server binary, the built web client under `web/dist`,
`capabilities.yaml`, the deterministic fixture data under `tests/fixtures/model`,
`.env.example`, `README.md`, `README.zh-CN.md`, `LICENSE`, `NOTICE`, `THIRD_PARTY_NOTICES.md`,
`SECURITY.md`, `TRADEMARK.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
`CHANGELOG.md`, the pinned dependency manifests (`go.mod`, `go.sum`,
`package.json`, `package-lock.json`), the OpenAPI contract, and the
bilingual `docs/` tree (including the release notes and demo assets). Extract
the archive and change into its top-level directory. The standalone binary reads
process environment variables; it does not load `.env` itself, so export the
values before running:

```bash
cd derive-world_0.1.0-rc.1_linux_amd64
cp .env.example .env
set -a; . ./.env; set +a
./derive-world migrate
./derive-world serve
```

On Windows the binary is `derive-world.exe`; use PowerShell or cmd to export
the variables from `.env`. Some POSIX shells on Windows rewrite absolute paths
such as `MODEL_CHAT_PATH`; leave that variable unset to use the default
`/v1/chat/completions`. The server requires PostgreSQL, the
three secrets of at least 32 bytes, and a `WEB_DIST_DIR` containing
`index.html`. The archive includes `web/dist` and `capabilities.yaml` for that
reason. The reference `content.structure` Provider is a separate process; use
Compose for the complete reference stack.

## Verification

The release workflow is prepared to build from an immutable tag. **No Git tag
or GitHub Release exists for this candidate**; the owner withheld tag creation
by the owner. The candidate dry-run built the three platform archives, a
source candidate archive, and `SHA256SUMS` from the final main SHA, and the
archives were built twice and compared byte-for-byte. The final main SHA, CI
run URLs, `SHA256SUMS`, and build manifest are recorded in the release
evidence package.
The required checks are `Go`, `Web`, `SDK`, `Architecture`, `Contract`,
`Integration`, `govulncheck`, `gitleaks`, and `E2E`. The release preflight also
requires the Security `License` and `npm audit` jobs at the same SHA. The `E2E`
job runs real PostgreSQL, the real Go API, the production web build, and
Chromium, Firefox, and a 390 px mobile project with two independent users.
Release verification scope: the workflow builds a source candidate archive
(`derive-world_<version>_source.tar.gz`) from the exact commit and includes it
in `SHA256SUMS`; the GitHub automatic source archive is additional, not a
substitute. The `docs/assets` demo material is 21 PNG screenshots plus 2 WebM
recordings with synthetic `example.test` data; the candidate version is
`0.1.0-rc.1` (a label, not a tag; the final `v0.1.0` is created only at the authorized public release).

SHA-256 proves file integrity; it does not by itself prove the issuer's
identity. **No GitHub build-provenance attestation has been generated for this
private candidate.** The public-release workflow is prepared to generate and
verify an attestation on a future authorized public tag run; that attestation
is unverified and not claimed. The draft/dry-run status is not a published
release.

## Known limitations

See [Limitations](https://github.com/QiyanAI/derive-world/blob/main/docs/en/limitations.md) for the complete list. The main limits are:

- Single instance; no horizontal scaling or real-time collaboration.
- No email, password reset, OAuth, email verification, or account deletion.
- One model adapter per deployment; no concurrent multi-provider routing or
  per-Personal-AI model selection.
- Real AI is VERIFIED_REAL only for the approved two-call API-level run at
  `e90f23a`/`9fd32c1` (DeepSeek `deepseek-flash`), not for every model or
  provider; it was not re-run on the final closure SHA.
- Historical object state has no read endpoint; derive from an older version
  uses immutable snapshots.
- GitHub Dependency review is not available on the private plan; govulncheck,
  gitleaks, npm audit, and License cover the private candidate. Dependency
  review runs when the repository is public.
- No prebuilt container images; Compose builds from source.
- The repository remains private and no public Release has been published.
- Build provenance attestation is not generated for this private candidate; the
  public-release workflow is prepared but has not run. The attestation is
  unverified and not claimed.

## Security

Report vulnerabilities privately as described in
[SECURITY.md](https://github.com/QiyanAI/derive-world/blob/main/SECURITY.md).
Do not open a public issue for a suspected vulnerability. GitHub Private
Vulnerability Reporting will be enabled at the public-release gate.

Read [Security](https://github.com/QiyanAI/derive-world/blob/main/docs/en/security.md)
for the implemented controls, model data flow, and untrusted-content handling.

## Process notes

- `v0.1.0-rc.N` tags are immutable release candidates. The final `v0.1.0`
  release requires explicit owner authorization.
- **G06 final acceptance.** The owner recorded final acceptance of G06 on
  2026-10-24. Real-AI evidence is an API-level, exactly two-call run to
  DeepSeek `deepseek-flash` at `e90f23a`/`9fd32c1`; it was not re-run on this
  candidate and is not a general provider claim. The candidate adds durable
  pre-call model-call accounting plus documentation, demo assets, test, and UI
  changes; it does not change model-adapter, authorization, schema, API path,
  permission, capability, identity, or dependency semantics.
- Contributions use the Developer Certificate of Origin 1.1. The pre-public
  development history is retained in a private repository and is not part of
  the public source history. History is never rewritten to hide a process
  deviation.
- G07 is **engineering complete; owner review pending; release NO-GO**. G07 is
  not self-accepted.
- Do not reuse release-candidate assets as final `v0.1.0` assets. The final
  release is rebuilt and attested from its own tag.
