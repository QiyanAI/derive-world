# Derive World

**Everything Can Derive. The First Living Web.**

Derive World is an open-source foundation for *Living Objects*: shared,
versioned objects that people and their personal AI can read, contribute to,
derive from, and trace through an explicit lineage. The first release targets a
small, self-hostable OSS MVP (`v0.1.0`) rather than a complete platform.

> **Status: G00–G06 — accepted/closed (G06 final acceptance recorded
> 2026-10-24). G07 — engineering complete; Owner review pending; release NO-GO.
> Real AI — VERIFIED_REAL only for the approved two-call API-level run at
> `e90f23a`/`9fd32c1` (DeepSeek `deepseek-flash`, synthetic data); it was not
> re-run on the final closure SHA, and no general model/provider claim is made.**
> This tree is the `v0.1.0`
> release candidate. The release evidence records the final main SHA,
> the CI runs (including real PostgreSQL and Chromium/Firefox/mobile browser
> acceptance), the planned `v0.1.0-rc.1` label (**not created**), and the
> dry-run artifact hashes. **No Git tag or GitHub Release exists for this
> candidate.** The repository remains private; the public-launch gate requires
> explicit owner authorization. See the [release notes](docs/en/release-notes.md)
> and [CHANGELOG](CHANGELOG.md).
> Fixture output is still not real AI. The frozen 22-table schema and the five
> capability descriptors are unchanged. Nothing here is a production release.

## The Reference World

| | |
|---|---|
| ![Landing](docs/assets/g05-landing.png) | ![World](docs/assets/g05-world-populated.png) |

- **World** — identity, owned and shared objects, real onboarding progress,
  pending approvals, and how the pieces connect.
- **Objects** — collaborative documents and interactive polls with versioned
  actions, members, invitations, Handoff, AI collaboration, history, and lineage.
- **Personal AI** — profile, private context, conversations, and a Handoff
  preview that never impersonates another user's AI.
- **Digital Self** — the consent ledger: delegations, revocation, and the human
  approvals that only you can give.
- **Settings** — language, tokens, export, deployment mode, and documented limits.

A two-user tour with expected results is in the
[Demo guide](docs/en/demo-guide.md). The full tour is in the
[Reference World guide](docs/en/reference-world.md).

## Two modes

| Mode | What it needs | What it proves |
|---|---|---|
| `fixture` | No model account or API key | Deterministic, repeatable UI and API behaviour. All AI output is labelled `FIXTURE`. |
| `real` | An OpenAI-compatible endpoint, model name, and API key | A real model call through `{MODEL_BASE_URL}/v1/chat/completions`. Costs are billed by your model provider. |

Fixture mode is a deterministic demo, **not** real AI. It must never be
presented as real model intelligence.

## Quick Start (Fixture, Docker Compose)

Requirements: Docker Engine with Compose v2, about 2 GB of free disk, and a
browser. This is the supported local path; the service binds to loopback only.

```bash
git clone https://github.com/QiyanAI/derive-world.git
cd derive-world
cp .env.example .env
docker compose up --build -d
docker compose ps
curl -fsS http://127.0.0.1:8080/readyz
```

The Compose build identifies as `0.1.0-rc.1`; `DW_COMMIT` and `DW_BUILD_TIME`
remain `unknown` unless you set them. Release archives inject the tagged commit
and commit-derived build time.

Open <http://127.0.0.1:8080>. The top bar shows `FIXTURE` while the deterministic
adapter is active. Register a local account, create a collaborative or
interactive object, invite a second person, apply an AI proposal, approve a
derive, and inspect the lineage. The `.env.example` values are development-only
placeholders; they are not production secrets.

To stop the stack:

```bash
docker compose down
```

To stop and delete the local database volume:

```bash
docker compose down -v
```

## Browser acceptance

The blocking browser suite drives two independent users against the real
Compose stack: real PostgreSQL, the real Go API, and the production web build.
Chromium and Firefox each execute the complete Derive Loop; a 390 px mobile
project checks responsive and EN/ZH behaviour.

```bash
npm ci
npx playwright install --with-deps chromium firefox
npm run test:e2e
```

The CI job named **E2E** runs the Compose smokes, installs both browsers, runs
the suite, and uploads traces/screenshots. It is a required status check on
`main`; there is no bypass actor. `PW_REQUIRE_FIXTURE=1` fails closed if the
stack is not in fixture mode. Rate-limit overrides are applied only inside the
test `.env`; production defaults are unchanged.

## Real AI mode

Edit `.env` and set at least:

```text
MODEL_MODE=real
MODEL_BASE_URL=https://your-openai-compatible-host
MODEL_NAME=your-model
MODEL_API_KEY=your-key
```

`MODEL_BASE_URL` is a root address **without** `/v1`. The adapter appends
`/v1/chat/completions` exactly once, including when the value ends in `/` or
`/v1`. Then recreate the app container:

```bash
docker compose up -d --force-recreate app
curl -fsS http://127.0.0.1:8080/api/v1/meta
```

The response must report `"model_mode":"real"`. If required Real-mode settings
are missing, the process fails at startup instead of silently falling back to
Fixture. Real mode is a separate controlled operation. The approved two-call
real-AI acceptance run completed exactly two `deepseek-flash` calls: A used 764
input / 51 output tokens and B used 804 / 49. The estimated cost was
≈US$0.00030 off-peak (US$0.00059 peak upper bound), below the US$0.05 cap. The
API key was never written to the repository or evidence. A further real call
requires a new owner authorization; there is no automatic retry.

Model access is vendor-neutral: the business layer uses only the `Provider`
interface. `MODEL_CHAT_PATH`, `MODEL_RESPONSE_FORMAT`, and
`MODEL_THINKING_MODE` are optional adapter settings; the default request is the
generic OpenAI-compatible one. One adapter is active per deployment; concurrent
multi-provider routing is future versioned work, not a claim of this build.

Real deployments should also set the budget controls documented in
[Self-hosting](docs/en/self-hosting.md#model-budget-and-stop-controls):
`MODEL_MAX_INPUT_CHARS`, the per-user daily limits, the deployment-wide
`MODEL_GLOBAL_DAILY_CALLS` hard stop, and `MODEL_KILL_SWITCH`. Real mode forces
a positive global call cap even when the variable is unset or zero.

## Local development

```bash
npm ci
npm run -w web build
npm run -w sdk/typescript build
go run ./cmd/server migrate
go run ./cmd/server serve
```

The Go service serves `web/dist` at `/` and exposes the API under `/api/v1`.
The web dev server proxies `/api` to the Go service when you run
`npm run -w web dev`; because the browser origin is then
`http://localhost:5173`, start the Go service with
`PUBLIC_BASE_URL=http://localhost:5173` so the Origin/CSRF checks accept the
dev server. Integration tests require `DATABASE_URL` and run with
`go test -tags=integration ./tests/...`.

## Repository layout

```text
cmd/server/                 single service entrypoint
internal/                   modular-monolith packages
api/openapi/openapi.yaml    OpenAPI 3.1 contract
web/                        React 19 + Vite reference client
sdk/typescript/             ESM SDK with zero runtime dependencies
examples/capability-provider independent stdlib-only provider
examples/sdk-node/          dependency-free SDK example
tests/security/             real-PostgreSQL integration and negative tests
tests/e2e/                  Playwright config, specs, capture, curl smokes
docs/en, docs/zh            developer documentation and the Reference World tour
docs/assets/                real screenshots and two-user recordings
ops/github-metrics/         independent AI-OPS metrics module
```

The module dependency direction is one-way and enforced by the architecture
test: `app -> domain modules -> store`, with `httpx`, `config`, and `model`
depending only on the standard library (plus their frozen dependencies).

## Security defaults

- Bind address defaults to `127.0.0.1:8080`; Compose publishes only loopback.
- PostgreSQL and the reference capability provider are on internal networks.
- The `app` and `capability-provider` containers run as non-root (UID/GID
  10001) with a read-only root filesystem and dropped capabilities; the
  PostgreSQL and one-shot migration containers use their standard images.
- No production secrets are committed. `.env` is ignored.
- CSP, nosniff, no-referrer, and Permissions-Policy headers are set by the app.
- Object content is untrusted. The server never treats model text or object
  content as an instruction; only schema-valid actions enter the proposal path.
- Object access is checked against active membership rows on every request; a
  caller without `read` receives 404, not 403. An open workspace transitions to
  access-lost on the next server read after revocation.
- Personal AI effective permission is membership ∩ active delegation ∩
  constraints. AI output never authorizes itself. Derive requires a separate
  five-minute, single-use human confirmation bound to an object version.
- Confirmations expose a bounded effect summary and are redacted after the
  caller loses read access to the target object.
- Invitation and PAT plaintext is returned once; only SHA-256 digests are
  stored. Invitation replays are redacted.
- Handoff projections are read-only, never credentials, and never include
  private context, conversations, tokens, or other members' delegations.
- Cookie writes require a session-bound CSRF token and a same-origin
  `Origin`/`Referer`. Login and registration reject cross-site requests.
- State changes require `expected_version`; conflicts never overwrite silently.
- Audit rows share the business transaction; an audit failure rolls the write
  back.
- Derive never modifies the parent and never inherits members, shared context,
  votes, or private data.
- Capability providers receive no cookies, PATs, private context, or database
  access. The Broker validates input and output schemas, enforces timeout and
  size limits, signs every outbound request, rejects metadata/link-local
  addresses always, and rejects private ranges unless the provider is
  explicitly internal.
- The `http_hmac_v2` transport verifies a signed response bound to the request
  timestamp, invocation id, status, and body hash. An unsigned, tampered,
  replayed, or wrong-key response is an unknown result and its output is never
  stored. Public providers must use v2 over verified TLS; private CAs and
  mutual TLS are supported. HMAC rotation accepts a previous key only before a
  configured RFC3339 deadline.
- Model calls have per-call input and output-token ceilings, per-user daily
  calls/characters, a deployment-wide daily call/character hard stop, and an
  operator kill switch. Quota refusals happen before the provider call, are
  audited, and survive a restart by reseeding from `model_invocations`. Each
  provider-attempted call writes a durable pre-call reservation to
  `model_invocations` before the request, so a crash, business rollback, or
  failed final update cannot hide a possible billable call.
- `timeout` and `unknown` capability results are never reported as success;
  unknown results never carry output.

Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).
This release candidate is not yet public; GitHub Private Vulnerability
Reporting will be enabled at the public-release gate. Until then, contact the
repository owner through a private GitHub channel. Do not open a public issue.

## Limitations

- Single instance only; rate limiting and scheduling are in-process.
- No email, password reset, OAuth, or account deletion in `v0.1.0`.
- No real-time collaboration; the object workspace polls every 10 seconds while
  visible and also offers explicit refresh.
- All five capabilities are exposed through the single Broker; the external
  `content.structure` Provider runs as a separate process on an internal
  network. There is no runtime capability registration or arbitrary provider
  URL.
- Real AI is VERIFIED_REAL only for the approved two-call API-level run at
  `e90f23a`/`9fd32c1` (DeepSeek `deepseek-flash`, synthetic data). It was not
  re-run on the final closure SHA and is not a general claim that every model
  or provider is accepted. One adapter is active per deployment.
- Fixture mode does not call a model. Deterministic structured fixture replies
  are demo data, never real AI.
- The reference Compose stack uses the v2 signed-response transport over its
  internal plaintext network; public providers require HTTPS. TLS and mutual
  TLS are verified by the Go client and covered by tests, not enabled by the
  default Compose file.
- The Handoff preview in the web client is a membership-level projection; the
  actual Personal AI projection is further limited by the caller's delegation.
- A legal object can exceed the 32 KiB Handoff budget; the AI tab reports that
  specific capacity failure instead of claiming an action.
- Historical object state has no read endpoint in `v0.1.0`; history is an event
  and version summary, and derive-from-an-older-version uses immutable
  snapshots. The UI states this limitation.
- Compose is a reference deployment, not a production orchestrator.
- The `v0.1.0` release candidate is not yet publicly released; the repository
  remains private until the public-release gate. No GitHub Release has been
  published.

## Documentation

- [Reference World guide](docs/en/reference-world.md)
- [Two-user demo guide](docs/en/demo-guide.md)
- [Quick Start](docs/en/quickstart.md)
- [Architecture](docs/en/architecture.md)
- [API](docs/en/api.md)
- [Provider deployment](docs/en/providers.md)
- [Self-hosting](docs/en/self-hosting.md)
- [Security](docs/en/security.md)
- [Limitations](docs/en/limitations.md)
- [Release notes](docs/en/release-notes.md)
- [中文 README](README.zh-CN.md)

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
Third-party license texts are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
The license does not grant trademark rights; see [TRADEMARK.md](TRADEMARK.md).

Contributions use the Developer Certificate of Origin; see
[CONTRIBUTING.md](CONTRIBUTING.md).
