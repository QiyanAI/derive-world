# Derive World local launcher (Windows)

The launcher is a small PowerShell helper for a **self-hosting administrator**
on Windows. It does not replace Docker or PostgreSQL, and it does not install
anything. It removes the need to type Compose commands for the normal local
path:

**double-click → environment check → start → readiness check → browser.**

## Requirements

- Windows 10 or 11 with Windows PowerShell 5.1 or PowerShell 7.
- Docker Desktop with Docker Compose v2, running.
- About 2 GB free disk for the images and the local database volume.
- The repository checkout containing `compose.yaml`, `.env.example`, and the
  `launcher` directory. Edition/zip copies may need
  `Unblock-File .\launcher\DeriveWorld.ps1` once.

A **normal product user** does not need the launcher. They open the instance
URL in a browser. The launcher is for the person who owns the machine or
server that runs the instance.

## Start

Double-click **`Start Derive World.cmd`** in the repository root (or
`launcher\DeriveWorld.cmd`). The menu offers:

| Option | Action |
|---|---|
| 1 | Start, wait for `/readyz`, open the browser |
| 2 | Stop (database volume preserved) |
| 3 | Restart the application container (data preserved) |
| 4 | Status and health |
| 5 | Open the product |
| 6 | **AI settings / Connect AI — open configuration file** |
| 7 | **AI settings / Connect AI — apply and verify** |
| 8 | Diagnostics with secrets redacted |
| 9 | Environment check |
| 10 | AI configuration status |
| 11 | Switch AI back to Fixture mode |
| 12 | Clean old `.env.backup-*` snapshots (keeps `.env.last-good`) |

Command-line equivalents:

```powershell
powershell -NoProfile -File .\launcher\DeriveWorld.ps1 -Command Check
powershell -NoProfile -File .\launcher\DeriveWorld.ps1 -Command Start -NoBrowser
powershell -NoProfile -File .\launcher\DeriveWorld.ps1 -Command ApplyConfig
powershell -NoProfile -File .\launcher\DeriveWorld.ps1 -Command Diagnostics
powershell -NoProfile -File .\launcher\DeriveWorld.ps1 -Command SelfTest -NoPause
```

## What is checked

Before start, the launcher reports each item with a clear cause and recovery
path: Docker CLI, Docker Engine, Compose v2, port 8080, the instance directory
and its writability, `.env` presence, the database volume, and the running
application's `/healthz`, `/readyz`, and `/api/v1/meta`. Missing software gets
an install pointer and a "run the check again" instruction; the launcher never
installs software silently.

## AI configuration: Plan B (one-click configuration file)

This implementation uses **Plan B** from the remediation specification: one
click opens this instance's `.env`, shows the exact fields to edit, and a second
step validates, recreates the application container, waits for readiness, and
reports whether the requested model mode actually became active. This is a
deliberate choice:

- A browser-based settings API would have to accept deployment-level secrets
  from an ordinary web session. That is a new, high-risk remote write surface
  and a second long-lived key configuration system.
- Plan B keeps the key on the deployment machine, outside the browser, and
  outside every public API. The specification names Plan B as a formal,
  complete delivery, not a shortcut.
- The launcher binds no network port and passes no secret on a command line.
  Compose reads the key from the local `.env` file.

### Security boundaries

- The key is never printed, logged, placed in the URL, or stored in browser
  storage. Diagnostics mask every secret value.
- `.env` is created from `.env.example` only when it does not exist. The
  launcher never overwrites an existing configuration.
- On Windows the launcher restricts `.env` to the current user, SYSTEM, and
  Administrators with `icacls`. This is a local file-permission control, not
  encryption.
- `.env` and container environment variables are **not a cryptographic vault**.
  Anyone with local administrator or Docker administrator rights can read
  them. The launcher does not claim "zero leakage".
- Ordinary members, friends, anonymous visitors, and Personal AI cannot read
  or change deployment-level model settings. There is no per-user key hosting
  in this release.
- Apply creates a timestamped `.env.backup-*` before restarting. If the new
  settings do not become healthy and active, the launcher restores the backup,
  recreates the application, and verifies readiness again. World data is never
  touched; no command uses `docker compose down -v`.

### Connection states

The launcher distinguishes: not configured, fixture mode, valid real
configuration, missing key, invalid base URL, unreachable endpoint, and TLS
verification failure. It does not make a model inference call, so it does not
prove that a specific model name is callable and does not incur provider
charges. The product's Settings page reports the deployment-wide model mode and
limits after startup.

## Data protection

Start, stop, and restart never delete the PostgreSQL volume, user worlds, or
configuration. The only destructive Compose command (`down -v`) is deliberately
absent, and the CI safety contract fails if it is added.

## Scope

This launcher is the Windows path. Other operating systems keep the existing
documented Compose workflow at the same level of support; Windows validation
does not claim an equivalent no-terminal experience elsewhere.

## Tests

`-Command SelfTest` runs the parser, validation, masking, and root-resolution
checks without Docker. CI runs it on `windows-latest` together with the
environment detection and a static safety contract. The read-only checks,
foreign-instance detection, redacted diagnostics, and status were also executed
on a real Windows 11 machine with Docker Desktop. A full launcher
start/stop/restart against the default `derive-world` project requires that no
other checkout owns that project on the same Docker engine; when another
checkout is running, the launcher correctly refuses. The final acceptance
report records that branch as `NOT_YET_VALIDATED` rather than claiming it.

## 中文说明

`Start Derive World.cmd` 是自托管管理员的本地启动入口：双击后检查 Docker、
Compose、端口、配置文件和数据库卷，启动后等待 `/readyz` 就绪并打开浏览器。

AI 配置采用任务书允许的 **方案 B**：菜单第 6 项打开本实例的 `.env` 并显示
需要填写的四个字段；第 7 项验证、重启、健康检查并确认模型模式真正生效。
配置失败会自动恢复上一份配置。API Key 不会进入浏览器、URL、日志或命令行，
但 `.env` 不是加密保险库，拥有本机或 Docker 管理员权限的人仍可读取它。

普通使用者不需要启动器，只需要浏览器访问实例地址。
