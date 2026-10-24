# 安全

## 报告

GitHub 私密漏洞报告已对本公开仓库启用。请在仓库的 Security 页面选择 Report a vulnerability。不要为疑似漏洞开公开 Issue。范围与流程见 `SECURITY.zh-CN.md`。

## 本构建的默认值

- 服务默认绑定 `127.0.0.1:8080`。
- Compose 仅发布 loopback，PostgreSQL 与参考 Provider 位于内部网络。
- `app` 与 `capability-provider` 容器以 UID/GID 10001 运行，根文件系统只读、能力全部丢弃、`no-new-privileges`；PostgreSQL 与一次性迁移容器使用其标准镜像。
- 不提交生产密钥；`.env` 被忽略。
- 安全头：CSP `default-src 'self'`、`frame-ancestors 'none'`、nosniff、no-referrer 与严格的 Permissions-Policy。
- 请求体上限 1 MiB；拒绝未知 JSON 字段。
- 访问日志只记录路由模板、加盐 IP 哈希，不记录 Cookie、Authorization、正文或对象内容。
- 模型 API Key 只来自 `MODEL_API_KEY`，不入库、不入日志、不下发浏览器。

## 能力与 Provider 控制

- 密码使用 Argon2id（`m=64 MiB, t=3, p=2`，16 字节 salt，32 字节 key），永不写日志、审计或响应。
- 会话与 CSRF 令牌为 32 字节随机值，数据库只存 SHA-256 摘要。登录轮换会话，退出同步撤销。
- Cookie 写请求必须同时通过会话绑定 CSRF 哈希与同源 `Origin`/`Referer`。登录与注册拒绝跨站请求。
- 每个对象请求在一条查询中加载对象与活跃成员记录。无 `read` 返回 404，而不是 403。
- `access_policy` 永不参与授权；数据库将 `visibility` 固定为 `private`。
- 状态动作必须带 `expected_version`，并使用行锁与条件更新。冲突不写状态、不写事件。
- Derive 同时要求 `read` 与 `derive`，历史版本只从不可变事件快照读取，且绝不写父对象。
- 审计与业务写入同一事务；审计失败回滚业务。拒绝路径使用独立短事务，因为没有业务变更可加入。
- 幂等请求在回放前重新授权。失去 `read` 或删除对象返回 404；仍可读但失去 `derive` 返回 403。绝不向无权调用者回放旧 200。
- 缓存 Personal AI 回合仅在当前成员与活跃 AI 委托仍允许对象 `read` 时回放。精确委托绑定保留给执行路径（提议应用与确认批准）；只读缓存回合不能执行任何动作。
- 对象状态在落库前按冻结模板 Schema 与 64 KiB / 8 KiB 限额校验。
- 邀请与 Derive Link 使用 32 字节随机令牌，只存 SHA-256；默认仅授予 `read`，邀请权限不得超过邀请者当前权限，禁止 `manage`；强制有效期（≤30 天）与原子次数；不存在/已撤销/已过期/已用尽统一 404，防枚举。
- PAT 使用 `dw_pat_` 前缀，只存 SHA-256，scope 封闭；Cookie-only 端点拒绝 Bearer。PAT scope 是端点能力上限，不替代对象授权。
- Personal AI 有效权限 = 成员权限 ∩ 活跃委托 ∩ 约束，并剔除仅人类权限。没有权限缓存；每个 AI 回合与确认批准都重新读取成员与委托。
- AI 状态修改只有在服务端 Schema 与权限校验通过后才写入 `object_proposals`。人类负责应用或拒绝；AI 不能应用/拒绝提议、创建邀请、移除成员、删除对象或修改 `shared_context`。
- 应用提议时在应用事务内重新校验该提议记录的精确委托。即使人类仍保有 `contribute` 或另行存在有效委托，原始委托被撤销或过期后，旧提议也不得产生新状态；Trust Kernel 持有该委托行的共享锁直至提交。
- Derive 必须经过五分钟、单次使用、绑定规范化 payload 哈希与期望版本的人工确认。批准前重新校验人类成员权限、精确 AI 委托与版本。
- Handoff 是基于对象快照与过滤事件的只读投影，不是凭证，不落库，绝不读取私人上下文、会话、消息、委托、令牌或会话表。
- `/me/export` 仅导出本人数据，排除密码哈希、令牌哈希、其他用户数据与私人内容。
- 凭据、CSRF 值、私人上下文、消息内容与 AI 输入不会写入对象事件或审计 metadata；审计 metadata 按键脱敏并限制 4 KiB。

## Real AI 与预算控制

- `http_hmac_v2` Provider 传输校验响应 HMAC：覆盖请求时间戳、invocation id、HTTP 状态与响应体精确字节的 SHA-256。缺失、畸形、密钥错误、跨 Invocation、重放或被篡改的签名一律产生未知结果，输出绝不入库，审计原因为 `provider_integrity_failed`。
- 响应校验使用常量时间比较。Provider 回显请求 key id 且必须匹配，因此签名响应不能被移动到其他 Invocation 或密钥。
- 公网 Provider 必须使用 `http_hmac_v2` 与 HTTPS。TLS 证书与主机名校验始终开启（最低 TLS 1.2）。内部 Provider 可配置私有 CA 与可选的客户端证书以实现双向 TLS。对明文 `http://` Provider 配置 TLS 会在启动时失败。
- HMAC 轮换支持当前 key id 与可选的旧密钥 RFC3339 截止时间。旧密钥只在截止前被接受，截止后无需重启即失效。旧密钥与当前密钥相同、key id 重复或截止时间已过都会导致启动失败。
- 模型调用在 Provider 调用之前受单次输入上限、每用户每日调用/字符预算、部署级每日调用/字符硬上限与运维停止开关约束。Real 模式强制正的全局调用上限。配额拒绝以 `rate.limit_hit` 与原因写入审计，不发起 Provider 调用；内存计数在重启后从 `model_invocations` 重新计数。实际尝试 Provider 调用前，运行时会先写入持久预占行；即使进程崩溃、业务事务回滚或最终更新失败，该行仍被计入，不会漏记可能产生费用的调用。冻结的 status 域用 `failed`/`in_flight` 表示进行中的预占，结果确定后替换为最终状态。
- 模型适配器禁用环境代理、HTTPS 要求 TLS 1.2、响应体上限 1 MiB、输出 Token 限制在配置上限内、上游 429 映射为 `rate_limited`，且绝不自动重试。
- 模型厂商中立性由结构约束与架构测试共同保证：业务层只依赖 `Provider` 接口，可选的请求路径/JSON/Thinking 设置只存在于部署配置；Personal AI、信任、对象、Derive、Handoff、能力与审计代码中不编译任何厂商主机、模型 ID 或 API Key。未启用可选配置时，默认 OpenAI-compatible 请求保持不变。多 Provider 并发路由与按 Personal AI 独立选择模型属于后续工作，`v0.1.0` 不作此声明。

## 尚未实现

`v0.1.0` 是首个公开版本。已批准的双 Personal AI 两次 API 级调用（DeepSeek `deepseek-flash`）已完成，Real AI 仅对该范围记为 **VERIFIED_REAL**；未在发布 SHA 上重跑，不构成普遍模型/Provider 验收。旧 `http_hmac` 传输仅保留用于内部兼容，不签名响应，不得用于公网 Provider。

## 能力安全控制

- 能力定义仅来自配置；启动时校验五个冻结 ID、绑定、Schema、Provider URL 与 HMAC 密钥。缺失、禁用、额外或重绑定能力均拒绝启动。
- 每次调用都经唯一 Broker：认证、PAT scope 上限、对象成员、AI 委托、输入 Schema、幂等、限流/并发、执行、输出 Schema 与语义校验、审计。
- 外部 Provider 只收到 invocation id、能力 id、契约版本与已校验输入；永不接收 Cookie、PAT、CSRF、私人上下文、会话、委托或数据库凭据。
- 出站客户端精确匹配 allow-list host，每次调用解析并分类全部地址，拒绝 loopback/link-local/元数据/CGNAT/组播/保留地址，只连接已验证 IP，禁止重定向与环境代理。
- 请求 HMAC-SHA256 覆盖 timestamp + "." + raw body；Provider 拒绝 >30 秒偏差，常量时间验签，并对 300 秒内重放去重。
- 输出 ≤256 KiB，且在落库或返回前通过 JSON Schema 与能力语义校验。`timeout`/`unknown` 永不成功，`unknown` 永不携带 output，不自动重试。
- `input_redacted` ≤2 KiB，永不包含 text、body、summary、prompt 或 title 原文。

## 模型数据流

Real AI 模式下，`AI_INCLUDE_PRIVATE_CONTEXT=true` 会把用户私人上下文、当前 Handoff 投影与最近会话消息发送给部署者配置的模型服务商。服务商的数据保留不受本系统控制；设为 `false` 可关闭私人上下文。Fixture 模式不调用模型服务商。`MODEL_MAX_INPUT_CHARS`、每日预算与 `MODEL_GLOBAL_DAILY_CALLS` 在发送前限制这些调用的数量与成本；`MODEL_KILL_SWITCH=true` 会拒绝全部调用。

## 不可信内容

对象内容是不可信数据，以 `<untrusted_object_content>` 包裹，绝不作为指令执行。只有通过 Schema 校验的 `proposed_actions` 才进入提议/确认流程，且每个动作都由服务端重新授权。自然语言输出与对象文本中的内嵌 JSON 都不能创建动作。
