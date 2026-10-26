# 快速开始

Derive World 是一个可自托管的开放世界：活对象、Personal AI 与独立衍生共同组成持续状态。本指南先用 Fixture 模式启动参考栈，走通世界舞台、三种活对象（协作、互动、活场景）、两个独立用户、好友直接交流、主动公开与公开衍生、错误状态与浏览器验收，再说明如何切换到真实 OpenAI-compatible 模型。

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

### 谁需要什么

| 入口 | 需要 Docker 或 PostgreSQL？ | 能做什么 |
|---|---|---|
| 公开访客 | 否 | 浏览探索页、阅读主动公开的快照；登录后可在许可下衍生。 |
| 已注册用户 | 否 | 创建与编辑对象、使用 Personal AI、与好友协作、公开与衍生。 |
| 自托管管理员 | 是 | 运行 PostgreSQL、迁移、Go 服务、能力提供方与 Web 构建。 |

Go 二进制不是独立桌面应用：它需要 PostgreSQL、构建好的 `WEB_DIST_DIR`，真实 AI 还需要配置提供方。预构建镜像省去编译，但不省数据库。

### 诊断

`GET /healthz` 证明进程存活；`GET /readyz` 检查数据库与能力就绪。登录后，**设置 → 运行与限额**及 `GET /api/v1/system/status` 显示模型模式、数据库与能力状态、公开基础地址、每日与每小时 AI 限额、突发容量、并发上限与部署级总开关。API Key 永不返回。

跨设备使用时，请用反向代理终止 HTTPS，并把 `PUBLIC_BASE_URL` 设为该公开地址。`localhost` 邀请链接不是远程链接。除非完成[自托管安全审查](self-hosting.md)，否则保持默认 loopback 绑定。

## 参考世界走查

1. 选择“创建账户”。注册在同一事务内创建用户、个人 AI 资料、空私人上下文、默认授权与会话。**欢迎页**确认这些真实状态。
2. 打开**世界**。世界舞台展示你的真实对象、可见衍生分支与每个对象的最近真实变化。创建**活场景**（项目或故事）、**协作对象**或**互动对象**。
3. 修改文档、投票，或直接编辑活场景：改变起点，添加想法/步骤/场景/选择/结果，标记步骤完成，调整顺序与删除元素。每个被接受的动作都递增版本；过期的 `expected_version` 返回 `version_conflict`，不会静默覆盖。打开**衍生透镜**查看真实版本、可见分支与可继承字段。
4. 打开**成员与权限**，创建带明确权限的邀请。链接只显示一次。在第二个浏览器配置中，新人从该链接注册并回到同一邀请。
5. 第二个人修改对象。所有者刷新后看到持久化变化；之后移除成员，下一次读取立即失去访问。
6. 打开 **AI 协作**。固定演示模式下，快捷提示一次创建一条脚本化提议或一条衍生请求。提议在被人类应用前只是惰性数据；衍生请求是绑定版本的确认，批准后服务端执行一次并展示创建的子对象。
7. 打开**历史与谱系**。版本历史按对象版本分组；谱系裁剪不可读祖先、省略不可读后代，而不是声称不存在。
8. 打开**好友**。按 handle 发送请求，在第二个配置中接受，并直接发送人与人消息。好友关系本身不授予对象访问权；对象访问仍需在**成员与权限**中单独授权。屏蔽会阻止新的好友联系与消息，但不会静默撤销既有对象权限。
9. 在自有对象上打开**公开**。选择一个版本发布，并决定是否允许衍生。如果该版本包含其他人的贡献，公开会保持“等待贡献者同意”，直到所有贡献者批准后才出现在**探索**。已登录访客只有在所有者允许时才能创建独立、私人的分支；撤回公开会立即停止新的读取与衍生。
10. 用顶部切换语言。所有路由更新 `<html lang>`、文档标题与控件。390 px 宽度下导航横向滚动，谱系缩进受限。
11. 重启 app 容器并刷新，确认对象、事件、子对象与会话仍然存在。PostgreSQL 是唯一事实来源。

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

## 能力走查

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

响应必须为 `"model_mode":"real"`。Real 模式缺少配置时进程启动失败，不会静默降级为 Fixture。Real AI 使用你自己的 Provider 账户：端点与 Key 由你提供，费用由 Provider 直接向你计费。绝不要提交 Key。

## 本地开发

```bash
npm ci
npm run -w web build
npm run -w sdk/typescript build
# 先启动一个本地 PostgreSQL，例如：
# docker run --rm -d --name dw-dev-db -p 5432:5432 #   -e POSTGRES_USER=derive -e POSTGRES_PASSWORD=dw_local_only #   -e POSTGRES_DB=derive_world postgres:17-alpine
cp .env.example .env
set -a; . ./.env; set +a
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
- **`/readyz` 返回 503：** 某个必需依赖（通常是 PostgreSQL）未就绪，或 `DATABASE_URL` 错误；请检查 `docker compose logs migrate app`。
- **Real 模式启动失败：** 会打印缺失的变量名，不打印值。
- **浏览器显示 `UNKNOWN` 模式：** Web 客户端无法访问 `/api/v1/meta`，确认 app 容器运行中。

## 范围与后续版本

账号、会话、Living Object、对象授权、版本化动作、事件历史、Derive、来源链、邀请/Derive Link、Handoff、Personal AI 会话、提议、确认、PAT、Capability Broker、五项能力、Open API、TypeScript SDK、独立 Provider、响应签名传输、TLS 校验、模型预算控制、双语参考世界与 Chromium + Firefox 验收套件已实现。`v0.1.0` 是首个公开版本。Real AI 仅对已批准的双 Personal AI 两次 API 级调用（DeepSeek `deepseek-flash`）记为 VERIFIED_REAL；未在发布 SHA 上重跑，不代表所有模型或 Provider。没有路由伪造成功。作品展示与分享见[展示页](../../SHOWCASE.md)。
