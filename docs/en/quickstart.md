# Quick Start

Derive World is an open, self-hostable world of Living Objects, Personal AI,
and independent derivation. This guide starts the reference stack in Fixture
mode and walks through the World Stage, the four Living Object types
(collaborative, interactive, living scene, free world), the universal Personal
AI, two independent users, direct friendship and messaging, opt-in
publication, public derivation, error states, and the browser acceptance
suite. It then shows how to switch to a real
OpenAI-compatible model.

## Prerequisites

- Docker Engine with Compose v2.
- About 2 GB of free disk.
- A browser. Two independent browser profiles are needed for the later
  two-user acceptance scripts.

The reference stack binds only to `127.0.0.1:8080`.

## Fixture mode (no model account)

```bash
git clone https://github.com/QiyanAI/derive-world.git
cd derive-world
cp .env.example .env
docker compose up --build -d
docker compose ps
curl -fsS http://127.0.0.1:8080/readyz
```

Open <http://127.0.0.1:8080>. The header shows `FIXTURE`; all AI output in this
mode is deterministic demo data, not real model output.

### Who needs what

| Entry point | Needs Docker or PostgreSQL? | What it can do |
|---|---|---|
| Public visitor | No | Browse Explore and read active public snapshots; sign in to derive where allowed. |
| Registered user | No | Create and edit objects, use Personal AI, collaborate with friends, publish and derive. |
| Self-hoster | Yes | Run PostgreSQL, migrations, the Go service, the capability provider, and the web build. |

The Go binary is not a standalone desktop app. It needs PostgreSQL, a built
`WEB_DIST_DIR`, and (for Real AI) a configured provider. A container image
built once skips repeated compilation, not the database.

### Diagnostics

`GET /healthz` proves the process is alive. `GET /readyz` checks the database
and capability readiness. After signing in, **Settings → Runtime and limits**
and `GET /api/v1/system/status` show the model mode, database and capability
state, public base URL, daily and hourly AI limits, burst capacity,
concurrency, and the deployment-wide stop. API keys are never returned.

For another device, terminate HTTPS at a reverse proxy and set
`PUBLIC_BASE_URL` to that public origin. A `localhost` invitation link is not a
remote link. Keep the default loopback binding unless you have completed the
security review in [self-hosting.md](self-hosting.md).

## Reference World walkthrough

1. Choose **Create account**. Registration creates the user, Personal AI
   profile, empty private context, default delegation, and session in one
   transaction. The **welcome** page confirms that real state.
2. Open **World**. The World Stage shows your real objects, visible derivation
   branches, and the latest real change for each object. Create a **Living
   scene** (project or story), a **Collaborative object**, or an **Interactive
   object**.
3. Edit the document, vote in the poll, or work directly in a living scene:
   change its premise, add ideas/steps/scenes/choices/outcomes, mark steps done,
   reorder, and remove elements. Each accepted action increments the version; a
   stale `expected_version` returns `version_conflict` instead of overwriting.
   Open the **Lens** tab to inspect real versions, visible branches, and
   inheritable fields.
4. Open **People & access** and create an invitation with explicit permissions.
   The link is shown once. In a second browser profile, a new person registers
   from that link and returns to the same invitation.
5. The second person edits the object. The owner refreshes and sees the
   persisted change; removing the member later ends access on the next read.
6. Open **AI collaboration**. In fixture mode the quick prompts create one
   scripted proposal or one derive request. A proposal is inert until a human
   applies it. A derive request is a version-bound confirmation; approving it
   executes once on the server and shows the created child.
7. Open **History & lineage**. Version history groups actions by object version.
   Lineage clips unreadable ancestors and omits unreadable descendants instead
   of claiming they do not exist.
8. Open **Friends**. Send a request by handle, accept it in the second profile,
   and exchange direct human messages. Friendship alone does not grant object
   access; use the object's **Access** tab for that. Blocking stops new friend
   contact and messages but does not silently revoke existing object access.
9. Open **Publish** on an object you own. Publish a version with or without
   derivation permission. If other people contributed to that version, the
   publication stays `pending_consent` until every contributor approves. Only
   then does it appear in **Explore**. A logged-in visitor can create an
   independent private branch only when the owner allowed it; revoking the
   publication stops new reads and derivations.
10. Switch language with the header toggle. Every route updates `<html lang>`,
    the document title, and all controls. At 390 px the navigation scrolls
    horizontally and lineage indentation is capped.
11. Restart the app container, reload, and confirm the object, events, child, and
    session still exist. PostgreSQL is the source of truth.

The two-user walkthrough with expected results is in the
[Demo guide](demo-guide.md); the full tour is in the
[Reference World guide](reference-world.md).

## Browser acceptance

The blocking suite uses two isolated Playwright contexts and the real Compose
stack. Chromium and Firefox each execute the complete Derive Loop; a 390 px
mobile project checks responsive and EN/ZH behaviour.

```bash
npm ci
npx playwright install --with-deps chromium firefox
npm run test:e2e
```

If a local run trips the default rate limits, set test-stack-only values in
`.env` before recreating the app:

```text
RATE_LIMIT_AUTH_PER_MIN=1000
RATE_LIMIT_API_PER_MIN=600
```

CI applies the same overrides inside the workflow and asserts `model_mode` is
`fixture`. Production defaults are unchanged.

## Capability walkthrough

The same stack runs the Capability Broker and the independent
`content.structure` Provider. The Provider is reachable only on `capnet`; it
publishes no host port.

1. Create a personal access token in **Settings → Personal access tokens** with
   `capabilities:invoke`, `objects:read`, `objects:write`, and `derive:create`.
2. Call `GET /api/v1/capabilities` with the token. The response lists exactly
   five enabled descriptors and no provider address.
3. Invoke `object.read`, `object.context`, `object.propose_change`,
   `object.derive`, and `content.structure` with an `Idempotency-Key`. The
   proposal does not change the object version; the derive creates a new child;
   `content.structure` is executed by the external Provider.
4. Repeat an invocation with the same key and body. The first response is
   replayed with `Idempotency-Replayed: true`. Reuse the key with a different
   body and confirm `409 idempotency_conflict`.
5. Remove the token's operation scope, or remove the member from the object,
   and confirm the next call fails. A PAT scope never substitutes for object
   membership.
6. Ask Personal AI to derive in Fixture mode. The call returns `202` with a
   `confirmation_id`; no child exists until approval. Denying or letting it
   expire leaves the invocation non-successful.

Run the zero-dependency Node example against the stack:

```bash
DW_PAT=dw_pat_... node examples/sdk-node/example.mjs
```

The Provider protocol, standalone deployment, signing, replay window, and
network isolation rules are documented in [Provider deployment](providers.md).

Stop the stack:

```bash
docker compose down
```

Stop and remove the local database volume:

```bash
docker compose down -v
```

## Real AI mode

Edit `.env` and set:

```text
MODEL_MODE=real
MODEL_BASE_URL=https://your-openai-compatible-host
MODEL_NAME=your-model
MODEL_API_KEY=your-key
```

`MODEL_BASE_URL` is a root address without `/v1`. The adapter appends
`/v1/chat/completions` exactly once.

```bash
docker compose up -d --force-recreate app
curl -fsS http://127.0.0.1:8080/api/v1/meta
```

The response must report `"model_mode":"real"`. Missing Real-mode settings make
the process fail at startup; it never silently falls back to Fixture. Real AI
uses your own provider account: you supply the endpoint and key, and the
provider bills you directly. Never commit the key.

## Local development

```bash
npm ci
npm run -w web build
npm run -w sdk/typescript build
# Start a local PostgreSQL first, for example:
# docker run --rm -d --name dw-dev-db -p 5432:5432 #   -e POSTGRES_USER=derive -e POSTGRES_PASSWORD=dw_local_only #   -e POSTGRES_DB=derive_world postgres:17-alpine
cp .env.example .env
set -a; . ./.env; set +a
go run ./cmd/server migrate
go run ./cmd/server serve
```

For the Vite dev server with an API proxy, start the Go service with
`PUBLIC_BASE_URL=http://localhost:5173` so the browser Origin from Vite is
accepted by the Origin/CSRF checks:

```bash
npm run -w web dev
```

## Troubleshooting

- **Port 8080 is busy:** change the loopback mapping in `compose.yaml` and
  `PUBLIC_BASE_URL` together.
- **`migrate` exits non-zero:** inspect `docker compose logs migrate`. The app
  does not start unless migration succeeds.
- **`/readyz` returns 503:** a required dependency, usually PostgreSQL, is not
  ready or `DATABASE_URL` is wrong; inspect `docker compose logs migrate app`.
- **Real mode fails at startup:** the missing variable name is printed; values
  are never printed.
- **Browser shows `UNKNOWN` mode:** the web client cannot reach
  `/api/v1/meta`; confirm the app container is running.

## Scope and later versions

Accounts, sessions, Living Objects, object authorization, versioned actions,
event history, Derive, Lineage, invitations/Derive Links, Handoff, Personal AI
conversations, proposals, confirmations, PATs, the Capability Broker, all five
capabilities, the Open API, the TypeScript SDK, the independent Provider, the
bilingual Reference World, and the Chromium + Firefox acceptance suite are
implemented. `v0.1.0` is the first public release.
Real AI is VERIFIED_REAL only for the approved two-call API-level run (DeepSeek `deepseek-flash`), not for every model or
provider; it was not re-run on the release SHA. No route fabricates success.
See [Showcase and sharing](../../SHOWCASE.md) for the three launch examples and
how to share your own Living Object.
