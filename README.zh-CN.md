# Derive World

**万物皆可衍生。第一个活着的 Web。**

Derive World 是一个面向 *Living Object（活对象）* 的开源基础：共享、有版本的对象，人和自己的 Personal AI 可以读取、贡献、衍生，并沿着显式来源链追溯。首个版本只做可自托管的 OSS MVP（`v0.1.0`），不是完整平台。

> **状态：G00–G06 — 已验收/关闭（G06 最终验收记录于 2026-10-24）；G07 — 工程完成，Owner 复核待定，发布 NO-GO。Real AI — 仅对已批准的双 Personal AI 两次 API 级调用（`e90f23a`/`9fd32c1`，DeepSeek `deepseek-flash`，合成数据）记为 VERIFIED_REAL；未在最终收口 SHA 上重跑，不构成对所有模型或 Provider 的普遍验收。** 本树是 `v0.1.0` 发布候选。发布证据记录最终 main SHA、CI 运行（包括真实 PostgreSQL 与 Chromium/Firefox/移动端浏览器验收）、计划中的 `v0.1.0-rc.1` 标签（**未创建**）与 dry-run 产物哈希。**本候选不存在任何 Git Tag 或 GitHub Release。** 仓库仍为私有；公开发布需要负责人明确授权。见[发行说明](docs/zh/release-notes.md)与 [CHANGELOG](CHANGELOG.md)。Fixture 输出仍不是真实 AI。冻结的 22 表 Schema 与五个能力描述符未变。这里没有任何生产发布。

## 参考世界

| | |
|---|---|
| ![首页](docs/assets/g05-landing.png) | ![世界](docs/assets/g05-world-populated.png) |

- **世界** — 身份、拥有与共享的对象、真实的第一使用进度、待批准事项与连接关系。
- **对象** — 协作文档与互动投票，包含版本化操作、成员、邀请、Handoff、AI 协作、历史与谱系。
- **个人 AI** — 资料、私人上下文、会话，以及绝不冒充其他用户 AI 的 Handoff 预览。
- **数字自我** — 同意账本：授权、撤销，以及只有你能给出的批准。
- **设置** — 语言、令牌、导出、部署模式与文档化限制。

带预期结果的双人演示见[演示指南](docs/zh/demo-guide.md)，完整导览见[参考世界指南](docs/zh/reference-world.md)。

## 两种模式

| 模式 | 需要什么 | 证明什么 |
|---|---|---|
| `fixture` | 不需要模型账户或 API Key | 确定性、可重复的 UI 与 API 行为；所有 AI 输出均标记 `FIXTURE`。 |
| `real` | OpenAI-compatible 端点、模型名、API Key | 通过 `{MODEL_BASE_URL}/v1/chat/completions` 发起真实模型调用；费用由模型服务商直接计费。 |

Fixture 是确定性演示，**不是**真实 AI，不得被描述为真实模型智能。

## 快速开始（Fixture，Docker Compose）

前置：Docker Engine 与 Compose v2、约 2 GB 磁盘、浏览器。这是受支持的本地路径；服务只绑定 loopback。

```bash
git clone https://github.com/QiyanAI/derive-world.git
cd derive-world
cp .env.example .env
docker compose up --build -d
docker compose ps
curl -fsS http://127.0.0.1:8080/readyz
```

Compose 构建标识为 `0.1.0-rc.1`；除非显式设置，`DW_COMMIT` 与 `DW_BUILD_TIME` 为 `unknown`。发行归档注入带标签的提交与由提交派生的构建时间。

打开 <http://127.0.0.1:8080>。确定性适配器启用时，顶部显示 `FIXTURE`。注册本地账号，创建协作或互动对象，邀请第二个人，应用 AI 提议，批准衍生并查看谱系。`.env.example` 中的值是仅限开发的占位值，不是生产密钥。

停止：

```bash
docker compose down
```

停止并删除本地数据库卷：

```bash
docker compose down -v
```

## 浏览器验收

阻断式浏览器套件以两个独立用户驱动真实 Compose 栈：真实 PostgreSQL、真实 Go API 与生产 Web 构建。Chromium 与 Firefox 都执行完整衍生闭环；390 px 移动项目检查响应式与中英文。

```bash
npm ci
npx playwright install --with-deps chromium firefox
npm run test:e2e
```

CI 作业 **E2E** 运行 Compose smoke、安装两种浏览器、执行套件并上传 trace/截图。它是 `main` 的必需状态检查，没有 bypass actor。`PW_REQUIRE_FIXTURE=1` 在非固定演示模式下失败关闭。限流覆盖只写入测试 `.env`，生产默认值不变。

## Real AI 模式

编辑 `.env`，至少设置：

```text
MODEL_MODE=real
MODEL_BASE_URL=https://your-openai-compatible-host
MODEL_NAME=your-model
MODEL_API_KEY=your-key
```

`MODEL_BASE_URL` 是**不含** `/v1` 的根地址。适配器只会拼接一次 `/v1/chat/completions`；即使值以 `/` 或 `/v1` 结尾也不会重复。然后重建 app 容器：

```bash
docker compose up -d --force-recreate app
curl -fsS http://127.0.0.1:8080/api/v1/meta
```

响应必须为 `"model_mode":"real"`。Real 模式缺少必需配置时进程启动失败，不会静默降级为 Fixture。Real 模式是独立的受控操作。已批准的双 Personal AI 两次真实调用完成了恰好两次 `deepseek-flash` 调用：A 使用 764 输入 / 51 输出 Token，B 使用 804 / 49。按非高峰价估算约 US$0.00030（高峰上界 US$0.00059），低于 US$0.05 上限。API Key 从未写入仓库或证据。后续真实调用需要新的负责人授权；没有自动重试。

模型接入是厂商中立的：业务层只依赖 `Provider` 接口。`MODEL_CHAT_PATH`、`MODEL_RESPONSE_FORMAT`、`MODEL_THINKING_MODE` 是可选适配器设置，默认请求仍是通用 OpenAI-compatible。每个部署只激活一个适配器；多 Provider 并发路由属于后续版本工作，不是本构建的声明。

Real 部署还应设置[自托管文档](docs/zh/self-hosting.md#模型预算与停止控制)中的预算控制：`MODEL_MAX_INPUT_CHARS`、每用户每日上限、部署级 `MODEL_GLOBAL_DAILY_CALLS` 硬上限和 `MODEL_KILL_SWITCH`。Real 模式下即使变量未设置或为 0，也会强制使用正的全局调用上限。

## 本地开发

```bash
npm ci
npm run -w web build
npm run -w sdk/typescript build
go run ./cmd/server migrate
go run ./cmd/server serve
```

Go 服务在 `/` 提供 `web/dist`，并在 `/api/v1` 下暴露 API。运行 `npm run -w web dev` 时，Vite 开发服务器把 `/api` 代理到 Go 服务；此时浏览器 Origin 是 `http://localhost:5173`，请以 `PUBLIC_BASE_URL=http://localhost:5173` 启动 Go 服务，使 Origin/CSRF 校验接受开发服务器。集成测试需要 `DATABASE_URL`，并以 `go test -tags=integration ./tests/...` 运行。

## 仓库结构

```text
cmd/server/                 唯一服务入口
internal/                   模块化单体
api/openapi/openapi.yaml    OpenAPI 3.1 契约
web/                        React 19 + Vite 参考客户端
sdk/typescript/             零运行时依赖 ESM SDK
examples/capability-provider 独立、仅标准库的 Provider
examples/sdk-node/          无依赖 SDK 示例
tests/security/             真实 PostgreSQL 集成与负例测试
tests/e2e/                  Playwright 配置、规格、截图脚本与 curl smoke
docs/en, docs/zh            开发者文档与参考世界导览
docs/assets/                真实截图与双人录屏
ops/github-metrics/         独立 AI-OPS 指标模块
```

模块依赖方向单向并由架构测试强制执行：`app -> domain modules -> store`；`httpx`、`config` 与 `model` 只依赖标准库（及其冻结依赖）。

## 安全默认

- 默认绑定 `127.0.0.1:8080`；Compose 只发布 loopback。
- PostgreSQL 与参考能力 Provider 位于内部网络。
- 容器以非 root、只读根文件系统、丢弃 capabilities 运行。
- 不提交生产密钥；`.env` 被忽略。
- 应用设置 CSP、nosniff、no-referrer 与 Permissions-Policy。
- 对象内容不可信。服务端绝不把模型文本或对象内容当作指令；只有通过 Schema 的动作进入提议路径。
- 每次请求都按有效成员行检查对象访问；没有 `read` 的调用者收到 404，而不是 403。撤销后已打开的工作区在下一次服务端读取时进入访问结束状态。
- 个人 AI 有效权限 = 成员权限 ∩ 有效授权 ∩ 约束。AI 输出不能授权自己。衍生需要单独的、五分钟、单次使用、绑定对象版本的人类确认。
- 确认只暴露有边界的效果摘要；调用者失去目标对象读取权后，投影被裁剪。
- 邀请与 PAT 明文只返回一次；只存 SHA-256 摘要。邀请重放被脱敏。
- Handoff 只读，永远不是凭证，且不包含私人上下文、会话、令牌或其他成员的授权。
- Cookie 写入需要会话绑定的 CSRF 令牌与同源 `Origin`/`Referer`。登录与注册拒绝跨站请求。
- 状态变更需要 `expected_version`；冲突绝不静默覆盖。
- 审计行与业务同事务；审计失败回滚写入。
- 衍生永不修改父对象，也不继承成员、共享上下文、投票或私人数据。
- 能力 Provider 不接收 Cookie、PAT、私人上下文或数据库访问。Broker 校验输入输出 Schema、执行超时与大小限制、签名每次出站请求，始终拒绝元数据/link-local 地址，并仅对显式标记为内部的 Provider 允许私有网段。
- `http_hmac_v2` 传输校验与请求时间戳、Invocation、状态和响应体哈希绑定的响应签名。未签名、被篡改、重放或密钥错误的响应一律视为未知结果，输出绝不入库。公网 Provider 必须使用 v2 与经过校验的 TLS；支持私有 CA 与双向 TLS。HMAC 轮换只在配置的 RFC3339 截止时间前接受旧密钥。
- 模型调用具有单次输入与输出 Token 上限、每用户每日调用/字符上限、部署级每日调用/字符硬上限，以及运维停止开关。配额拒绝发生在 Provider 调用之前，会写入审计，并在重启后从 `model_invocations` 重新计数。每次实际尝试 Provider 调用前，都会在 `model_invocations` 写入持久预占记录；进程崩溃、业务回滚或最终记录更新失败都不会漏记可能产生费用的调用。
- `timeout` 与 `unknown` 能力结果绝不报告为成功；unknown 不带输出。

漏洞请按 [SECURITY.md](SECURITY.md) 私下报告。本发布候选尚未公开；GitHub Private Vulnerability Reporting 将在公开发布门启用。在此之前，请通过 GitHub 私密渠道联系仓库所有者。不要开公开 issue。

## 限制

- 仅单实例；限流与调度在进程内。
- `v0.1.0` 不发邮件、不做密码找回、不做 OAuth、不提供账号删除。
- 无实时协作；对象工作区在页面可见时每 10 秒轮询，并提供显式刷新。
- 五项能力都经单一 Broker；外部 `content.structure` Provider 运行在内部网络的独立进程。没有运行时能力注册或任意 Provider URL。
- Real AI 仅对已批准的双 Personal AI 两次 API 级调用（`e90f23a`/`9fd32c1`，DeepSeek `deepseek-flash`，合成数据）记为 VERIFIED_REAL；未在最终收口 SHA 上重跑，这不是对所有模型或 Provider 的普遍验收声明。每个部署只激活一个适配器。
- Fixture 模式不调用模型。确定性结构化回复是演示数据，不是真实 AI。
- 参考 Compose 栈在内部明文网络上使用 v2 响应签名传输；公网 Provider 必须使用 HTTPS。TLS 与双向 TLS 由 Go 客户端校验并有测试覆盖，默认 Compose 文件不启用。
- Web 客户端的 Handoff 预览是成员级投影；个人 AI 实际投影还会受到调用者授权的进一步限制。
- 合法对象可能超过 32 KiB Handoff 预算；AI 分区会报告该容量失败，而不是声称执行了操作。
- `v0.1.0` 没有历史对象状态读取端点；历史是事件与版本摘要，从旧版本衍生使用不可变快照。界面会明确说明。
- Compose 是参考部署，不是生产编排器。
- `v0.1.0` 发布候选尚未公开发布；仓库在公开发布门前保持私有。尚未发布任何 GitHub Release。

## 文档

- [参考世界指南](docs/zh/reference-world.md)
- [双人演示指南](docs/zh/demo-guide.md)
- [快速开始](docs/zh/quickstart.md)
- [架构](docs/zh/architecture.md)
- [API](docs/zh/api.md)
- [Provider 部署](docs/zh/providers.md)
- [自托管](docs/zh/self-hosting.md)
- [安全](docs/zh/security.md)
- [限制](docs/zh/limitations.md)
- [发行说明](docs/zh/release-notes.md)
- [English README](README.md)

## 许可证

Apache License 2.0。见 [LICENSE](LICENSE) 与 [NOTICE](NOTICE)。
第三方许可证文本见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
许可证不授予商标权；见 [TRADEMARK.md](TRADEMARK.md)。

贡献使用 Developer Certificate of Origin；见 [CONTRIBUTING.md](CONTRIBUTING.md)。
