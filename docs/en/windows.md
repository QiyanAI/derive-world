# Windows self-hosting

This page covers the **self-hosting administrator** on Windows. A normal member
never needs these steps: they open the instance URL in a browser.

The supported Windows path is a source checkout with Docker Desktop and Docker
Compose v2. The released `windows_amd64` zip contains only the server binary;
it does not include PostgreSQL, the Compose stack, or the web build. Use the
checkout path below for a complete local instance.

## 1. Install the prerequisites

1. Install [Docker Desktop](https://www.docker.com/products/docker-desktop/).
   On Windows Home, allow it to install or use WSL 2
   ([WSL install guide](https://learn.microsoft.com/windows/wsl/install)).
2. Start Docker Desktop and wait until it reports that the engine is running.
3. Optional: install [Git for Windows](https://git-scm.com/download/win) if you
   want to clone and update the repository with Git.
4. Make sure the account can run Docker. If Docker Desktop asks you to join the
   `docker-users` group, sign out and in after the change.

Nothing is installed silently. The launcher only reports what is missing and
where to get it.

## 2. Get the source

Either clone the repository:

```text
git clone https://github.com/QiyanAI/derive-world.git
```

or download the source ZIP from the repository page and extract it. If Windows
marks the downloaded files as blocked, run this once in PowerShell from the
repository root:

```powershell
Get-ChildItem -Recurse | Unblock-File
```

## 3. Double-click to start

Double-click **`Start Derive World.cmd`** in the repository root.

The launcher:

1. Checks Docker, the Docker Engine, Compose v2, port 8080, the instance
   directory, `.env`, the database volume, and whether another checkout owns
   the shared Compose project.
2. Creates `.env` from `.env.example` only if `.env` does not exist. An
   existing `.env` is never overwritten.
3. Starts the stack and waits for `/readyz`.
4. Shows the version and model mode and opens the product URL.

The first build can take several minutes. If something fails, the window stays
open with a redacted diagnostic and a recovery instruction. No command ever
deletes the database volume.

### One instance per Docker engine

The Compose project name and database volume are fixed (`derive-world` and
`derive-world_dw_pgdata`). Two checkouts on the same Docker engine would share
containers, port 8080, and data. The launcher detects a foreign checkout by the
Compose labels and refuses to start, stop, or reconfigure it. Stop the other
copy from its own directory first.

## 4. First use and Fixture mode

A new `.env` uses `MODEL_MODE=fixture`. Fixture mode calls no model provider;
AI replies are deterministic demo output and are labelled `FIXTURE` in the
product. **Objects you create are real and persisted** in the local PostgreSQL
volume. You can create, edit, derive, invite, publish, and return later without
an API key.

Register the first account and enter the World Stage. There is no sample data
created behind your back. Choose **Start a project** for practical work
(steps, progress, outcomes) or **Start a story** for free creation (scenes,
choices, directions). Both use the same object and derivation mechanism.

## 5. Connect real AI (optional)

Use the launcher menu:

- **6. AI settings / Connect AI — open configuration file** opens this
  instance's real `.env` in Notepad and prints the four fields to edit.
- **7. AI settings / Connect AI — apply and verify** validates the fields,
  recreates the application container, waits for readiness, and checks that
  `/api/v1/meta` reports the requested model mode. If the new configuration
  does not become healthy, the launcher restores the previous `.env` backup
  and restarts.

Edit only:

```dotenv
MODEL_MODE=real
MODEL_BASE_URL=https://your-provider.example
MODEL_NAME=your-model-name
MODEL_API_KEY=your-api-key
```

`MODEL_BASE_URL` is the provider root without `/v1` and without a trailing
slash. The endpoint is vendor-neutral: any OpenAI-compatible provider works.

The launcher checks DNS, TCP, and TLS without making an inference call, so it
does not prove that a model name is callable and does not create provider
charges. It reports distinct states for not configured, invalid key shape,
invalid URL, unreachable endpoint, and TLS failure.

### Key handling

- The key is stored only in this instance's `.env`. It is not sent to the
  browser, stored in browser storage, written to a URL, or printed in logs.
- On Windows the launcher restricts `.env` to the current user, SYSTEM, and
  Administrators with `icacls`.
- `.env` and container environment variables are **not an encrypted vault**.
  A local administrator or Docker administrator can read them. There is no
  "zero leakage" claim.
- Ordinary members, friends, anonymous visitors, and Personal AI never receive
  deployment-level configuration.
- The launcher does not expose a public configuration API and does not accept
  API keys from a web session.

To return to the deterministic demo, set `MODEL_MODE=fixture`, then apply.

## 6. Everyday operations

- **Stop** preserves containers and the database volume.
- **Restart** recreates the application container and keeps all data.
- **Status** shows Compose services, `/healthz`, `/readyz`, version, commit, and
  model mode.
- **Diagnostics** shows only redacted information: secret values are masked,
  and DSNs, bearer tokens, cookies, and emails are filtered.
- **Open** uses `PUBLIC_BASE_URL` when it is an absolute http(s) URL; otherwise
  it uses `http://127.0.0.1:8080`.

The product URL must match `PUBLIC_BASE_URL`. If you change the port, update
both the Compose port mapping and `PUBLIC_BASE_URL`; otherwise login and other
unsafe requests are rejected by the origin check.

## 7. Data and backup

All world data lives in the Docker named volume `derive-world_dw_pgdata`.
Stop/restart/upgrade does not delete it. To make a backup, use the logical dump
procedure in [self-hosting](self-hosting.md); do not copy the live volume files
from the Docker Desktop VM.

Never use `docker compose down -v` on an instance whose data you want to keep.
The launcher has no volume-deletion action.

## 8. Troubleshooting

| Symptom | What to do |
|---|---|
| Docker CLI not found | Install Docker Desktop, reopen the launcher. |
| Engine not running | Start Docker Desktop and wait for "Engine running". |
| Port 8080 in use by a non-Docker program | Close that program; the launcher never kills processes. |
| Another checkout owns the project | Stop that copy from its own directory. Do not run two copies at once. |
| `/readyz` never becomes ready | Run Diagnostics; check the database container and the migration logs. Do not delete the volume. |
| Login or register returns 403 | Open the exact `PUBLIC_BASE_URL` origin (for example `http://127.0.0.1:8080`, not `localhost` if that differs). |
| Real mode fails at startup | Check the four `MODEL_*` fields; the launcher validates them and can restore the previous backup. |
| TLS verification failed | The provider certificate is not trusted by Windows; check the system clock, proxy, and certificate chain. |

## 9. Scope

This is the Windows self-hosting path. Linux and macOS keep the Compose
instructions in [self-hosting](self-hosting.md). The launcher does not claim an
equivalent no-terminal experience on other systems. GitHub-hosted Windows
runners cannot run the Linux Docker engine, so CI runs the launcher self-test,
environment detection, and static safety checks. On the acceptance machine,
read-only checks, foreign-instance detection, diagnostics, and status were
executed; a full launcher start/stop/restart against the default project was
blocked because another checkout owned that Compose project, and is recorded as
`NOT_YET_VALIDATED` in the acceptance report rather than claimed as passed.
