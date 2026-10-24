# API

唯一契约源是 `api/openapi/openapi.yaml`（OpenAPI 3.1.0），运行中的服务在
`/api/v1/openapi.yaml` 提供该文件。API 实现数字自我会话、Living Object、对象授权、版本化动作、事件历史、Derive、来源链、邀请/Derive Link、Handoff、Personal AI 会话、Trust Kernel、AI 提议/确认、PAT、本人导出、Capability Broker、五项首发能力、OpenAPI 契约、TypeScript SDK 边界、独立 `content.structure` Provider、响应签名传输、TLS 校验、HMAC 密钥轮换与模型预算。

## 系统端点

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/healthz` | 存活探针，不访问数据库。 |
| GET | `/readyz` | 就绪探针；PostgreSQL 可用时返回 200，否则 503。 |
| GET | `/api/v1/meta` | 版本、commit、构建时间与 `model_mode`。 |
| GET | `/api/v1/openapi.yaml` | 本契约文件。 |

## 对象端点

| 方法 | 路径 | 认证 | 说明 |
|---|---|---|---|
| POST | `/api/v1/auth/register` | 公开 | 同一事务内创建用户、Personal AI 档案、私人上下文、默认委托与会话。 |
| POST | `/api/v1/auth/login` | 公开 | 轮换请求中携带的旧会话并签发新会话。 |
| POST | `/api/v1/auth/logout` | 会话 + CSRF | 撤销当前会话。 |
| GET | `/api/v1/auth/me` | 会话或 PAT | 当前用户与 Personal AI 摘要。 |
| GET | `/api/v1/objects` | 会话或 PAT（`objects:read`） | `scope=owned\|shared\|all`，签名游标分页。 |
| POST | `/api/v1/objects` | 会话或 PAT（`objects:write`）；仅 Cookie 会话需要 CSRF | 必须带 `Idempotency-Key`；写入 owner 成员记录。 |
| GET | `/api/v1/objects/{id}` | 会话或 PAT（`objects:read`） | 无活跃成员记录时返回 404。 |
| DELETE | `/api/v1/objects/{id}` | 会话 + CSRF | 仅所有者可软删除；版本不变。 |
| GET | `/api/v1/objects/{id}/events` | 会话或 PAT（`objects:read`） | `cursor` 为上一页最后的 `seq`。 |
| POST | `/api/v1/objects/{id}/actions` | 会话或 PAT（`objects:write`）；仅 Cookie 会话需要 CSRF | 必须带 `Idempotency-Key` 与 `expected_version`。 |
| GET | `/api/v1/objects/{id}/members` | 会话或 PAT（`objects:read`） | 活跃成员，不返回邮箱。 |
| POST | `/api/v1/objects/{id}/derive` | 会话或 PAT（`derive:create`）；仅 Cookie 会话需要 CSRF | 必须带 `Idempotency-Key`；不写父对象。 |
| GET | `/api/v1/objects/{id}/lineage` | 会话或 PAT（`objects:read`） | 深度 1–10，按隐私规则裁剪祖先/后代。 |
| GET | `/api/v1/objects/{id}/handoff` | 会话或 PAT（`objects:read`） | 有界、按权限裁剪的投影；不是凭证。 |
| POST/GET | `/api/v1/objects/{id}/invitations` | 会话 | 创建/列出邀请；创建需 `Idempotency-Key`。 |
| DELETE | `/api/v1/objects/{id}/invitations/{invitation_id}` | 会话 | 撤销；`invite` 只能撤销自己创建的，`manage` 可撤销全部。 |
| GET | `/api/v1/invitations/{token}` | 会话 | 预览标题、权限、有效期、剩余次数；不含对象 state。 |
| POST | `/api/v1/invitations/{token}/accept` | 会话 | 原子递增次数并精确覆盖权限。 |
| DELETE | `/api/v1/objects/{id}/members/{user_id}` | 会话 | 仅所有者；所有者不可被移除/退出。 |
| GET | `/api/v1/objects/{id}/proposals` | 会话 | 仅当前用户自己的 AI 提议。 |
| POST | `/api/v1/objects/{id}/proposals/{proposal_id}/apply` | 会话 | 人类应用；提议状态自身幂等。 |
| POST | `/api/v1/objects/{id}/proposals/{proposal_id}/deny` | 会话 | 人类拒绝。 |
| GET/PATCH | `/api/v1/ai/profile` | 会话 | 仅本人 Personal AI 档案。 |
| GET/PUT | `/api/v1/ai/context` | 会话 | 仅本人私人上下文，≤8 KiB。 |
| GET/POST | `/api/v1/ai/conversations` | 会话 | 私人会话；创建需 `Idempotency-Key`。 |
| GET/DELETE | `/api/v1/ai/conversations/{id}` | 会话 | 仅本人；删除为软删除。 |
| POST | `/api/v1/ai/conversations/{id}/messages` | 会话 | 单次 Provider 调用；需 `Idempotency-Key`。 |
| GET/POST | `/api/v1/ai/delegations` | 会话 | 仅本人委托；不含 `manage`/`invite`。 |
| DELETE | `/api/v1/ai/delegations/{id}` | 会话 | 立即撤销。 |
| GET/POST | `/api/v1/tokens` | 会话 | PAT 元数据/创建；明文只返回一次。 |
| DELETE | `/api/v1/tokens/{id}` | 会话 | 立即撤销 PAT。 |
| GET | `/api/v1/confirmations` | 会话 | 默认仅待确认。 |
| GET | `/api/v1/confirmations/{id}` | 会话 | 安全投影；过期返回 410。 |
| POST | `/api/v1/confirmations/{id}/approve` | 会话 | 需 `Idempotency-Key`；重校验后执行。 |
| POST | `/api/v1/confirmations/{id}/deny` | 会话 | 拒绝待确认动作。 |
| GET | `/api/v1/me/export` | 会话 | 仅本人 JSON 导出，每小时一次。 |

## 能力端点

| 方法 | 路径 | 认证 | 说明 |
|---|---|---|---|
| GET | `/api/v1/capabilities` | 会话或 PAT（`capabilities:invoke`） | 仅返回已启用描述符；永不含 Provider 地址或密钥。 |
| GET | `/api/v1/capabilities/{capability_id}` | 会话或 PAT（`capabilities:invoke`） | 单个已启用描述符。 |
| POST | `/api/v1/capabilities/{capability_id}/invoke` | 会话或 PAT（`capabilities:invoke` + 操作 scope） | 必须带 `Idempotency-Key`；所有执行经 Broker。 |
| GET | `/api/v1/capability-invocations/{id}` | 会话或 PAT（`capabilities:invoke`） | 仅调用者本人，且当前对对象有 `read`。 |

### 能力调用

请求正文就是该能力的冻结输入 Schema。不存在 `provider_url`、`callback_url` 或任何调用方指定的目标地址。

```http
POST /api/v1/capabilities/content.structure/invoke
Authorization: Bearer dw_pat_...
Idempotency-Key: 018f3f2f-8b7d-7a58-9f11-2f5f5f7a9b21
Content-Type: application/json

{"object_id":"018f3f2f-8b7d-7a58-9f11-2f5f5f7a9b21","text":"# 标题\n\n正文","format":"markdown"}
```

同步成功返回已校验输出：

```json
{
  "id": "018f3f30-1c2d-7e4f-8a90-123456789abc",
  "capability_id": "content.structure",
  "status": "succeeded",
  "output": {
    "items": [
      {"index": 0, "kind": "heading", "text": "标题", "level": 1},
      {"index": 1, "kind": "paragraph", "text": "正文"}
    ],
    "item_count": 2,
    "truncated": false,
    "format": "markdown"
  },
  "duration_ms": 7,
  "created_at": "2026-10-23T00:00:00Z",
  "completed_at": "2026-10-23T00:00:00.007Z"
}
```

`object.propose_change` 返回 `{"proposal_id":"...","status":"pending"}`，不修改对象；人类继续通过现有提议端点应用或拒绝。提议 input 上限为 64 KiB 规范化 JSON，与对象状态上限一致。应用受 64 KiB 对象状态限制；`object.state_changed` 事件 payload 上限为 132,352 字节，其他事件仍为 96 KiB，能力输入本身仍为 64 KiB。因此提议可以创建成功，但能装入 64 KiB 提议输入、却超出 64 KiB 状态容量的正文（例如 65,467 字节的 20,000 字符混合正文）在 apply 时返回 413，此时对象不变。V1 的两个上限同时生效：正文最多 20,000 个 Unicode code point，且完整状态最多 65,536 字节。20,000 个四字节字符（约 80,000 字节）在提议创建阶段即被 64 KiB 输入上限拒绝。

AI 发起的 `object.derive` 在等待人工确认时返回 `202`：

```json
{
  "id": "018f3f31-2d3e-7f50-9b01-123456789abc",
  "capability_id": "object.derive",
  "status": "pending",
  "confirmation_id": "018f3f32-3e4f-7a61-8c12-123456789abc",
  "created_at": "2026-10-23T00:00:01Z"
}
```

失败使用统一错误信封。执行失败带已记录的 invocation id，且永不携带 output：

```json
{
  "error": {
    "code": "capability_unknown",
    "message": "capability result is unknown",
    "request_id": "018f...",
    "details": {"invocation_id": "018f3f30-1c2d-7e4f-8a90-123456789abc"}
  }
}
```

### 能力授权

- Actor 一律由服务端会话或 PAT 产生；请求体中的 `actor_user_id`、`provider_id`、`permissions` 等权威字段被拒绝。
- PAT 必须含 `capabilities:invoke` 与操作 scope：`object.read`、`object.context`、`content.structure` 需 `objects:read`；`object.propose_change` 需 `objects:write`；`object.derive` 需 `derive:create`。scope 只是上限，不是对象授权。
- 每次调用都重新检查对象成员权限（`read`、`contribute` 或 `derive`）。不存在权限缓存，撤销后下一次请求立即失效。
- Personal AI 走同一 Broker。其有效权限是用户对象权限、有效委托与能力契约的交集。AI Derive 创建 5 分钟、单次使用、绑定 invocation/Actor/对象/版本/规范化输入的确认。

### 限额与失败语义

- `object.context` 投影被裁剪时，被裁剪字段的 `data_provenance` 条目带可选 `"truncated": true`；根级 `truncated` 仍为权威标记。仅在该字段实际被裁剪时出现。
- 输入 64 KiB、输出 256 KiB、总执行 10 秒、连接 2 秒、响应头 5 秒。
- 每用户 60 次/小时、并发 2。
- `pending` 是唯一非终态。`succeeded` 必须通过全部输出校验。`timeout` 与 `unknown` 永不显示为成功，`unknown` 永不携带 output。Broker 不自动重试；需要重试时使用新 `Idempotency-Key` 发起新调用。
- 定义在启动时从 `capabilities.yaml` 同步，不提供运行时注册 API。

### 外部 Provider 协议

Broker 调用 `POST /v1/invoke`，请求带固定信封，签名覆盖 `timestamp + "." + raw_body`（HMAC-SHA256）。Provider ID、能力 ID、契约版本、invocation ID、时间戳与签名位于 `X-DW-*` 头。Provider 拒绝超过 30 秒的时钟偏差，并对 300 秒内重放做去重。详见 [Provider 部署](providers.md)。

## 认证与 CSRF

- 注册与登录设置两个 Cookie：`dw_session`（HttpOnly）与 `dw_csrf`（浏览器可读）。
- 所有 Cookie 写请求必须带 `X-CSRF-Token`，其值等于 `dw_csrf`，并带与 `PUBLIC_BASE_URL` 同源的 `Origin`（或 `Referer`）。
- 登录与注册在带 `Origin`/`Referer` 时必须同源，并拒绝 `Sec-Fetch-Site: cross-site`。
- 会话令牌为 32 字节随机值，数据库只存 SHA-256 摘要。
- 登录轮换会话；退出立即撤销。
- 明确允许的端点可使用 `Authorization: Bearer dw_pat_...`。PAT 必须携带端点 scope，且仍通过对象成员权限。`/ai/*`、邀请、提议、确认、令牌、导出、退出、成员删除与对象删除仅限 Cookie。
- PAT 请求不要求 CSRF，但若带 `Origin` 必须同源。

## 授权与存在性隐藏

每个对象路径先在一条查询中加载对象与调用者的活跃成员记录。对象不存在、已软删除、调用者无 `read`，一律 `404 not_found`。有 `read` 但缺少 `contribute`、`derive` 或 `manage` 时返回 `403 forbidden`。

`access_policy` 只是策略配置，永不作为授权依据；数据库将 `visibility` 固定为 `private`。

应用待处理的 AI 提议时，服务端在应用事务内重新校验该提议记录的 AI 授权。若该委托已撤销或过期，或调用者当前 AI 有效权限已不含 `contribute`，返回 `403 forbidden`，写入 `authz.denied` 审计（reason `delegation_not_active`），且不写对象状态。其他有效委托不能替代被撤销的原始委托。已应用的提议仍幂等回放，已拒绝或已过期的提议仍返回 `409 conflict`。

## 版本规则

- 新对象从 `version=1` 开始。
- 每个被接受的状态动作恰好 +1。
- 动作必须提供 `expected_version`；不匹配返回 `409 version_conflict`，不写状态、不写事件。
- 成员变更与软删除不改变版本。
- Derive 不改变父对象版本，也不写父对象事件。

## 幂等

八个幂等端点为 `POST /objects`、`POST /objects/{id}/actions`、`POST /objects/{id}/invitations`、`POST /objects/{id}/derive`、`POST /ai/conversations`、`POST /ai/conversations/{id}/messages`、`POST /capabilities/{id}/invoke` 与 `POST /confirmations/{id}/approve`，均需 `Idempotency-Key`（`^[A-Za-z0-9_-]{16,128}$`）。请求哈希绑定方法、实际路径（含对象/确认 ID）与规范化 JSON 正文。提议应用由提议状态自身幂等。

- 同键同体且已完成：原样回放首次状态码与正文，并带 `Idempotency-Replayed: true`。
- 同键不同体：`409 idempotency_conflict`。
- 租约内并发同键：`409 request_in_progress`。
- 失去 `read` 或删除对象后重放：`404`；仍可读但失去 `derive`：`403`。绝不向无权调用者回放旧成功。
- 完成 24 小时后视为新请求；崩溃遗留预约在 10 分钟租约后可被接管。

## 错误

```json
{
  "error": {
    "code": "not_found",
    "message": "not found",
    "request_id": "018f...",
    "details": {}
  }
}
```

错误码与 HTTP 映射由 API 契约 §A.4.5 冻结：`invalid_request`（400）、`unauthenticated`（401）、`forbidden`（403）、`not_found`/`invitation_invalid`（404）、`conflict`/`version_conflict`/`idempotency_conflict`/`request_in_progress`（409）、`confirmation_expired`（410）、`payload_too_large`（413）、`unsupported_media_type`（415）、`rate_limited`/`budget_exceeded`（429）、`model_unavailable`/`model_invalid_output`/`capability_failed`/`capability_unknown`（502）、`model_timeout`/`capability_timeout`（504）、`capability_denied`（403）、`internal`（500）。`confirmation_required` 是 202 结果，不是错误。

## 通用约定

- 媒体类型：`application/json; charset=utf-8`。
- ID：UUIDv7，小写带连字符。
- 时间：RFC 3339 UTC。
- `X-Request-Id` 合法时接受并回显，否则生成 UUIDv7。
- API 响应 `Cache-Control: no-store`。
- 请求体上限 1 MiB；拒绝未知 JSON 字段。
- 对象状态上限 64 KiB；`shared_context` 上限 8 KiB。
- 默认关闭 CORS；仅显式列出的精确来源会被回显，且不允许凭据。

## 契约测试

`TestOpenAPI` 使用 kin-openapi 加载并校验契约，将路径与方法同实际路由表比对。新增路由若未写入契约，CI 失败。
