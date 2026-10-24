# 快速开始

Derive World `v0.1.0` 是 OSS MVP 的工程基础。本指南先用 Fixture 模式启动 G05 开放参考世界，走通两种活对象、两个独立用户、Handoff → 个人 AI → 人类确认 → 衍生闭环、错误状态与浏览器验收，再说明如何切换到真实 OpenAI-compatible 模型。

## 前置条件

- Docker Engine 与 Compose v2。
- 约 2 GB 可用磁盘。
- 浏览器。后续双用户验收需要两个独立的浏览器 profile。

参考栈只绑定 `127.0.0.1:8080`。

## Fixture 模式（不需要模型账户）

```bash
git clone https://github.com/QiyanAI/derive-world.git
cd derive-world
cp .env.example .env
docker compose up --build -d
docker compose ps
curl -fsS http://127.0.0.1:8080/readyz
```

打开 <http://127.0.0.1:8080>。顶部显示 `FIXTURE`；此模式下所有 AI 输出都是确定性演示数据，不是真实模型输出。

## G05 参考世界走查

1. 选择“创建账户”。注册在同一事务内创建用户、个人 AI 资料、空私人上下文、默认授权与会话。**欢迎页**确认这些真实状态。
2. 打开**世界**。第一使用清单来自服务端状态。创建**协作对象**或**互动对象**。
3. 修改文档或投票。每个被接受的动作都递增版本；过期的 `expected_version` 返回 `version_conflict`，不会静默覆盖。
4. 打开**成员与权限**，创建带明确权限的邀请。链接只显示一次。在第二个浏览器配置中，新人从该链接注册并回到同一邀请。
5. 第二个人修改对象。所有者刷新后看到持久化变化；之后移除成员，下一次读取立即失去访问。
6. 打开 **AI 协作**。固定演示模式下，快捷提示一次创建一条脚本化提议或一条衍生请求。提议在被人类应用前只是惰性数据；衍生请求是绑定版本的确认，批准后服务端执行一次并展示创建的子对象。
7. 打开**历史与谱系**。版本历史按对象版本分组；谱系裁剪不可读祖先、省略不可读后代，而不是声称不存在。
8. 用顶部切换语言。所有路由更新 `<html lang>`、文档标题与控件。390 px 宽度下导航横向滚动，谱系缩进受限。
9. 重启 app 容器并刷新，确认对象、事件、子对象与会话仍然存在。PostgreSQL 是唯一事实来源。

带预期结果的双人演示见[演示指南](demo-guide.md)，完整导览见[参考世界指南](reference-world.md)。

## 浏览器验收

阻断式套件使用两个隔离 Playwright 上下文与真实 Compose 栈。Chromium 与 Firefox 都执行完整衍生闭环；390 px 移动项目检查响应式与中英文。

```bash
npm ci
npx playwright install --with-deps chromium firefox
npm run test:e2e
```

本地如遇默认限流，可在 `.env` 设置仅测试栈使用的值，然后重建 app：

```text
RATE_LIMIT_AUTH_PER_MIN=1000
RATE_LIMIT_API_PER_MIN=600
```

CI 在工作流内应用同样覆盖，并断言 `model_mode` 为 `fixture`；生产默认值不变。

## G04 能力走查

同一栈运行 Capability Broker 与独立 `content.structure` Provider。Provider 仅在 `capnet` 内可达，不发布宿主端口。

1. 在“设置 → 个人访问令牌”创建含 `capabilities:invoke`、`objects:read`、`objects:write`、`derive:create` 的 PAT。
2. 用该 PAT 调用 `GET /api/v1/capabilities`。响应恰好列出五个已启用描述符，且不含 Provider 地址。
3. 带 `Idempotency-Key` 调用 `object.read`、`object.context`、`object.propose_change`、`object.derive` 与 `content.structure`。提议不改变对象版本；Derive 创建新子对象；`content.structure` 由外部 Provider 执行。
4. 使用相同键与正文重复调用，首次响应应带 `Idempotency-Replayed: true` 原样回放；同键不同正文应返回 `409 idempotency_conflict`。
5. 移除 PAT 的操作 scope，或把成员移出对象，确认下一次调用失败。PAT scope 永不替代对象成员权限。
6. 在 Fixture 模式让 Personal AI 执行 Derive。调用返回 `202` 与 `confirmation_id`；批准前不存在子对象。拒绝或过期后 invocation 不得为成功。

对栈运行零依赖 Node 示例：

```bash
DW_PAT=dw_pat_... node examples/sdk-node/example.mjs
```

Provider 协议、独立部署、签名、重放窗口与网络隔离规则见 [Provider 部署](providers.md)。

停止：

```bash
docker compose down
```

停止并删除本地数据库卷：

```bash
docker compose down -v
```

## Real AI 模式

编辑 `.env`：

```text
MODEL_MODE=real
MODEL_BASE_URL=https://your-openai-compatible-host
MODEL_NAME=your-model
MODEL_API_KEY=your-key
```

`MODEL_BASE_URL` 是不含 `/v1` 的根地址。适配器只会拼接一次 `/v1/chat/completions`。

```bash
docker compose up -d --force-recreate app
curl -fsS http://127.0.0.1:8080/api/v1/meta
```

响应必须为 `"model_mode":"real"`。Real 模式缺少配置时进程启动失败，不会静默降级为 Fixture。

## 本地开发

```bash
npm ci
npm run -w web build
npm run -w sdk/typescript build
go run ./cmd/server migrate
go run ./cmd/server serve
```

使用带 API 代理的 Vite 开发服务器。此时请以 `PUBLIC_BASE_URL=http://localhost:5173` 启动 Go 服务，使浏览器从 Vite 发出的 Origin 通过 Origin/CSRF 校验：

```bash
npm run -w web dev
```

## 故障排查

- **8080 端口被占用：** 同时修改 `compose.yaml` 的 loopback 映射与 `PUBLIC_BASE_URL`。
- **`migrate` 非零退出：** 查看 `docker compose logs migrate`；迁移失败时 app 不会启动。
- **`/readyz` 返回 503：** PostgreSQL 未就绪或 `DATABASE_URL` 错误。
- **Real 模式启动失败：** 会打印缺失的变量名，不打印值。
- **浏览器显示 `UNKNOWN` 模式：** Web 客户端无法访问 `/api/v1/meta`，确认 app 容器运行中。

## 范围与后续 Gate

账号、会话、Living Object、对象授权、版本化动作、事件历史、Derive、来源链、邀请/Derive Link、Handoff、Personal AI 会话、提议、确认、PAT、Capability Broker、五项能力、Open API、TypeScript SDK、独立 Provider、响应签名传输、TLS 校验、模型预算控制、双语参考世界与 Chromium + Firefox 验收套件已实现。G00–G06 已验收/关闭（G06 最终验收记录于 2026-10-24）。本树是 `v0.1.0` 发布候选；G07 工程完成，Owner 复核待定，发布 NO-GO；尚未发布公开 Release。Real AI 仅对已批准的双 Personal AI 两次 API 级调用（`e90f23a`/`9fd32c1`，DeepSeek `deepseek-flash`）记为 VERIFIED_REAL；未在最终收口 SHA 上重跑，不代表所有模型或 Provider。没有路由伪造成功。
