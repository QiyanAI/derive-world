# 自托管

参考部署是 Docker Compose 的四服务三网络。Compose 用于本地/参考部署，不是生产编排。

## 服务

| 服务 | 作用 | 宿主访问 |
|---|---|---|
| `db` | PostgreSQL 17 | 无；内部 `data` 网络 |
| `migrate` | 一次性 goose 迁移 | 无；成功后退出 |
| `app` | Go 服务 + Web 静态资源 | 仅 `127.0.0.1:8080` |
| `capability-provider` | 参考 Provider | 无；内部 `capnet` 网络 |

`app` 加入 `edge`（模型 egress）、`data`、`capnet`。`db` 与 Provider 不发布宿主端口。

## 启动与停止

```bash
cp .env.example .env
docker compose up --build -d
docker compose ps
docker compose down
```

数据库使用命名卷 `dw_pgdata`。删除卷会销毁本地数据：

```bash
docker compose down -v
```

## 二进制发行归档

发布时，各平台归档包含服务端二进制（`derive-world` 或 `derive-world.exe`）、构建后的 Web 客户端 `web/dist`、`capabilities.yaml`、确定性 Fixture 数据 `tests/fixtures/model`、`.env.example`、许可证与声明文件、锁定的依赖清单，以及双语文档。解压归档并进入顶层目录，把 `.env.example` 复制为 `.env` 并导出变量。独立二进制只读取进程环境变量，不会自动加载 `.env`：

```bash
cd derive-world_0.1.0_linux_amd64
cp .env.example .env
set -a; . ./.env; set +a
./derive-world migrate
./derive-world serve
```

Windows 下二进制为 `derive-world.exe`；请用 PowerShell 或 cmd 导出 `.env` 变量。Windows 上的部分 POSIX shell 会重写 `MODEL_CHAT_PATH` 等绝对路径；可将其留空以使用默认 `/v1/chat/completions`。归档布局与默认值 `WEB_DIST_DIR=./web/dist`、`CAPABILITIES_CONFIG=./capabilities.yaml` 一致。归档是服务端 + Web 包；参考 `content.structure` Provider 与 Compose 栈从源码仓库构建，不包含在归档内。不发布预构建容器镜像。

## 配置

所有应用配置来自环境变量。Compose 读取 `.env`，该文件被 Git 忽略。生产要求 HTTPS `PUBLIC_BASE_URL`，拒绝 `dw_dev_only_` 密钥、`sslmode=disable` 与 `dw_local_only` 数据库密码。

`MODEL_MODE=fixture` 不需要模型账户。`MODEL_MODE=real` 要求 `MODEL_BASE_URL`、`MODEL_NAME`、`MODEL_API_KEY`，缺一即启动失败。

## 模型预算与停止控制

每次模型调用都在调用前经过预算门禁。拒绝发生在任何模型服务商请求之前，返回
`429 budget_exceeded`（输入超限时为 `413 payload_too_large`），并以
`rate.limit_hit` 与原因写入审计。

| 变量 | 默认 | 规则 |
|---|---|---|
| `MODEL_MAX_OUTPUT_TOKENS` | `2048` | 单次输出 Token 上限，1..8192。 |
| `MODEL_MAX_INPUT_CHARS` | `65536` | 单次输入 Unicode 字符上限，1024..65536。 |
| `MODEL_DAILY_CALLS_PER_USER` | `100` | 每用户 UTC 日调用数。 |
| `MODEL_DAILY_INPUT_CHARS_PER_USER` | `200000` | 每用户 UTC 日输入字符数。 |
| `MODEL_CONCURRENCY_PER_USER` | `2` | 每用户并发调用数，1..16。 |
| `MODEL_GLOBAL_DAILY_CALLS` | `0`（fixture） | 部署级 UTC 日调用数。Real 模式在未设置或为 `0` 时使用 `200`；显式正值按原值使用。 |
| `MODEL_GLOBAL_DAILY_INPUT_CHARS` | `0` | 部署级 UTC 日输入字符数；0 表示不限制。 |
| `MODEL_KILL_SWITCH` | `false` | `true` 时在服务商请求前拒绝所有模型调用。 |

计数在重启后从 `model_invocations` 重新载入，因此重启进程不会重置已消耗的每日额度。

任何 Provider 请求发出前，运行时会先向 `model_invocations` 写入一条持久预占行。冻结的 status 域用 `failed`/`in_flight` 表示进行中的预占；结果确定后替换为 `succeeded`、`failed` 或 `timeout`。若进程崩溃、业务事务回滚或最终更新失败，该预占行仍保留并在重启后计入。这是单实例的调用前预占；不新增表，也不修改冻结 Schema。

## 模型适配器配置

业务层只依赖 `model.Provider` 接口。以下可选适配器设置只改变 HTTP 请求，
不改变 Personal AI、信任、对象、Derive、Handoff、能力或审计代码。

| 变量 | 默认 | 规则 |
|---|---|---|
| `MODEL_CHAT_PATH` | `/v1/chat/completions` | 追加到 `MODEL_BASE_URL` 的绝对路径；禁止主机、查询、片段、路径穿越或空白。 |
| `MODEL_RESPONSE_FORMAT` | 空 | 空或 `json_object`；仅在设置时发送。 |
| `MODEL_THINKING_MODE` | 空 | 空、`default` 或 `disabled`；`disabled` 仅在显式设置时发送 `{"thinking":{"type":"disabled"}}`。 |

默认配置保持通用 OpenAI-compatible 请求不变。Provider 若文档要求不同路径或可选
JSON/Thinking 参数，只需部署配置即可接入；业务模块不编译任何厂商主机或模型 ID。
架构测试 `TestNoProviderVendorHardcodingOutsideModelAdapter` 强制该边界。

`v0.1.0` **未实现**多 Provider 并发路由、按 Personal AI 独立选择模型或本地模型
托管；这些属于后续版本工作，不是本构建已实现的能力。

## Provider 传输与 TLS

参考 `capabilities.yaml` 在内部 `capnet` 上使用 `http_hmac_v2`，响应经过签名并绑定 Invocation。公网 Provider 必须使用 `https` 与 `network: public`；Broker 校验证书与主机名，且不提供关闭校验的选项。私有 CA 或双向 TLS 可配置 `tls_ca_file`、`tls_server_name`、`tls_client_cert_file` 与 `tls_client_key_file`。详见 [Provider 部署](providers.md#tls-与双向-tls) 与[密钥轮换](providers.md#密钥轮换)。

参考 Provider 默认要求响应签名。HMAC 轮换期间，Provider 只在
`CAPABILITY_HMAC_PREVIOUS_UNTIL` 之前接受旧密钥；截止后旧密钥无需重启即失效。

## 数据库角色

迁移 `00003_runtime_role.sql` 创建 NOLOGIN 角色 `derive_world_app`。应用使用
`DATABASE_URL` 中的数据库用户连接，并在每个连接池连接上执行
`SET ROLE derive_world_app`。该角色对可变表具有 SELECT/INSERT/UPDATE；对
`object_events` 与 `audit_events` 只有 SELECT/INSERT；对 `audit_actions` 只有
SELECT；对任何表都没有 DELETE。

迁移以 PostgreSQL 超级用户运行时（Compose 默认），角色会自动授予连接用户。若自托管者以非超级用户的表所有者运行迁移，必须以具备 `CREATEROLE` 的角色运行迁移，或预先创建角色并显式授权：

```text
CREATE ROLE derive_world_app NOLOGIN;
GRANT derive_world_app TO <database_user>;
```

迁移仅在当前用户为超级用户时自动授予角色。若迁移用户与应用连接用户不同，还需把角色授予应用连接用户。

角色缺失或未授权时服务启动失败。生产环境不要让应用以表所有者身份运行，否则会绕过追加写与禁止删除的保证。

## 备份与恢复

从数据库服务创建逻辑备份：

```text
docker compose exec db sh -c 'pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > backup.dump
```

恢复到空数据库：

```text
docker compose exec -T db sh -c 'pg_restore --clean --if-exists -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < backup.dump
```

备份含用户数据。请加密存储，绝不提交到仓库。`v0.1.0` 不提供 WAL 归档或时间点恢复。

## 升级

1. 备份数据库。
2. 拉取新源码并重建。
3. 再次运行 Compose；一次性 `migrate` 服务在 `app` 启动前执行前向迁移。
4. 验证 `/readyz` 与当前版本。

## 限制

- 仅支持单实例；不要把多个 `app` 副本放在负载均衡后面。
- 不内置生产 TLS 终止；请使用可信反向代理。
- `v0.1.0` 不发邮件、不做 OAuth、不提供密码找回。
