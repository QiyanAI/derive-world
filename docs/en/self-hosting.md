# Self-hosting

The reference deployment is Docker Compose with four services and three
networks. Compose is a local/reference deployment, not a production orchestrator.

## Services

| Service | Role | Host access |
|---|---|---|
| `db` | PostgreSQL 17 | None; internal `data` network |
| `migrate` | One-shot goose migration | None; exits after success |
| `app` | Go server + web assets | `127.0.0.1:8080` only |
| `capability-provider` | Reference provider | None; internal `capnet` network |

`app` joins `edge` (model egress), `data`, and `capnet`. `db` and the provider
are never published on the host.

## Start and stop

```bash
cp .env.example .env
docker compose up --build -d
docker compose ps
docker compose down
```

The database uses the named volume `dw_pgdata`. Removing the volume destroys
local data:

```bash
docker compose down -v
```

## Binary release archives

When a release is published, the platform archives contain the server binary
(named `derive-world` or `derive-world.exe`), the built web client under
`web/dist`, `capabilities.yaml`, the deterministic fixture data under
`tests/fixtures/model`, `.env.example`, the license and notice files, the
pinned dependency manifests, and the bilingual docs. Extract the archive,
change into its top-level directory, copy `.env.example` to `.env`, and export
the variables. The standalone binary reads process environment variables; it
does not load `.env` itself:

```bash
cd derive-world_0.2.0_linux_amd64
cp .env.example .env
set -a; . ./.env; set +a
./derive-world migrate
./derive-world serve
```

On Windows the binary is `derive-world.exe`; use PowerShell or cmd to export
the variables from `.env`. Some POSIX shells on Windows rewrite absolute paths
such as `MODEL_CHAT_PATH`; leave it unset to use the default
`/v1/chat/completions`. The archive layout matches the defaults
`WEB_DIST_DIR=./web/dist` and `CAPABILITIES_CONFIG=./capabilities.yaml`. The
archives are server + web bundles. The reference `content.structure` Provider
and the Compose stack are built from the source repository; they are not
included in the archive. No prebuilt container image is published.

## Configuration

All application configuration comes from environment variables. `.env` is read
by Compose and is ignored by Git. Production requires HTTPS `PUBLIC_BASE_URL`,
rejects `dw_dev_only_` secrets, rejects `sslmode=disable`, and rejects the
`dw_local_only` database password.

`ASSET_DIR` stores uploaded images, video, audio and GLB models outside
PostgreSQL. It must be writable and must not live under `WEB_DIST_DIR`. Compose
mounts the named volume `dw_assets` at `/app/var/assets`. Back up that volume
together with the database; `pg_dump` alone does not include media bytes.
Deleting the volume leaves asset metadata but makes existing media unavailable.

`MODEL_MODE=fixture` needs no model account. `MODEL_MODE=real` requires
`MODEL_BASE_URL`, `MODEL_NAME`, and `MODEL_API_KEY`, and fails at startup if any
is missing.

## Model budget and stop controls

Every deployment has a pre-call budget gate. A refusal happens before any model
provider request, returns `429 budget_exceeded` (or `413 payload_too_large` for
an oversized input), and is audited as `rate.limit_hit` with a reason.

| Variable | Default | Rule |
|---|---|---|
| `MODEL_MAX_OUTPUT_TOKENS` | `2048` | Per-call output-token ceiling, 1..8192. |
| `MODEL_MAX_INPUT_CHARS` | `65536` | Per-call input ceiling in Unicode characters, 1024..65536. |
| `MODEL_DAILY_CALLS_PER_USER` | `100` | UTC-day calls per user. |
| `MODEL_DAILY_INPUT_CHARS_PER_USER` | `200000` | UTC-day input characters per user. |
| `MODEL_CONCURRENCY_PER_USER` | `2` | In-flight calls per user, 1..16. |
| `MODEL_GLOBAL_DAILY_CALLS` | `0` (fixture) | Deployment-wide UTC-day calls. Real mode substitutes `200` when the value is unset or `0`; an explicit positive value is used as-is. |
| `MODEL_GLOBAL_DAILY_INPUT_CHARS` | `0` | Deployment-wide UTC-day input characters; zero means unlimited. |
| `MODEL_KILL_SWITCH` | `false` | `true` refuses every model call before the provider request. |

The counters reseed from `model_invocations` after a restart, so restarting the
process does not reset a spent daily allowance.

Before any provider request, the runtime writes a durable one-call reservation
to `model_invocations`. The frozen status domain represents the in-flight row
as `failed`/`in_flight`; it is replaced with `succeeded`, `failed`, or
`timeout` when the outcome is known. If the process crashes, the business
transaction rolls back, or the final update fails, the reservation remains and
is counted on restart. This is a single-instance pre-call reservation; it adds
no table and changes no frozen schema.

## Model adapter profile

The business layer uses only the `model.Provider` interface. These optional
adapter settings change the HTTP request without changing Personal AI, trust,
object, derive, Handoff, capability, or audit code.

| Variable | Default | Rule |
|---|---|---|
| `MODEL_CHAT_PATH` | `/v1/chat/completions` | Absolute path appended to `MODEL_BASE_URL`; no host, query, fragment, traversal, or whitespace. |
| `MODEL_RESPONSE_FORMAT` | empty | Empty or `json_object`. Sent only when set. |
| `MODEL_THINKING_MODE` | empty | Empty, `default`, or `disabled`. `disabled` sends `{"thinking":{"type":"disabled"}}` only when explicitly set. |

The default profile is the generic OpenAI-compatible request and is unchanged.
A provider that documents a different path or optional JSON/thinking parameters
can be used by configuration alone; no vendor host or model id is compiled into
the business modules. The architecture test
`TestNoProviderVendorHardcodingOutsideModelAdapter` enforces this boundary.

Concurrent multi-provider routing, per-Personal-AI model selection, and local
model hosting are **not implemented** in `v0.2.0`. They remain future
versioned work, not a claim of this build.

## Provider transport and TLS

The reference `capabilities.yaml` uses `http_hmac_v2` on the internal
`capnet`. Responses are signed and bound to the invocation. A public Provider
must use `https` with `network: public`; the Broker verifies the certificate
and hostname and never disables verification. `tls_ca_file`,
`tls_server_name`, `tls_client_cert_file`, and `tls_client_key_file` are
available for a private CA or mutual TLS. See
[Provider deployment](providers.md#tls-and-mutual-tls) and
[Key rotation](providers.md#key-rotation).

The reference Provider requires signed responses by default. During an HMAC
rotation, the Provider accepts a previous key only before
`CAPABILITY_HMAC_PREVIOUS_UNTIL`; after that deadline the old key is rejected
without a restart.

## Database roles

Migration `00003_runtime_role.sql` creates the NOLOGIN role
`derive_world_app`. The application connects with the database user from
`DATABASE_URL`, then issues `SET ROLE derive_world_app` on every pooled
connection. The role has SELECT/INSERT/UPDATE on mutable tables, SELECT/INSERT
only on `object_events` and `audit_events`, SELECT only on `audit_actions`, and
no DELETE on any table.

When migrations run as a PostgreSQL superuser (the Compose default), the role is
granted to the connecting user automatically. A self-hoster running migrations
as a non-superuser owner must either run migrations with a role that has
`CREATEROLE`, or pre-create the role and grant it explicitly:

```text
CREATE ROLE derive_world_app NOLOGIN;
GRANT derive_world_app TO <database_user>;
```

The migration grants the role only when the current user is a superuser. If the
migration user differs from the application's connecting user, grant the role to
the connecting user as well.

The server fails at startup if the role is missing or not granted. Do not run
the application as the table owner in production; that bypasses the
append-only and no-delete guarantees.

## Backup and restore

Create a logical backup from the database service:

```text
docker compose exec db sh -c 'pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > backup.dump
```

Restore into an empty database:

```text
docker compose exec -T db sh -c 'pg_restore --clean --if-exists -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < backup.dump
```

Backups contain user data. Store them encrypted and never commit them. `v0.2.0`
does not provide WAL archiving or point-in-time recovery.

## Upgrade

1. Back up the database.
2. Pull the new source and rebuild.
3. Run Compose again; the one-shot `migrate` service applies forward-only
   migrations before `app` starts.
4. Verify `/readyz` and the current version.

## Limits

- Single instance only; do not put multiple `app` replicas behind a load
  balancer.
- No production TLS termination is built in; use a trusted reverse proxy.
- No email, OAuth, or password recovery in `v0.2.0`.
