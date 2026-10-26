# 架构

Derive World 是 Go 编写的模块化单体，配 React 参考客户端、ESM SDK，以及两个独立 Go module：参考能力 Provider 与 AI-OPS 指标采集。

## 运行时形态

```text
Browser -> app (net/http, :8080) -> PostgreSQL 17
                         |
                         +-> capability-provider（内部网络）
                         +-> OpenAI-compatible 模型端点（egress）
```

`app` 同时提供 `/api/v1` 与构建后的 Web 客户端。参考部署没有独立前端服务器。

## 模块边界

```text
web / sdk -> OpenAPI -> app -> {identity, personalai, objects, derive, trust, handoff, capability}
objects -> store
derive -> objects（受控接口）+ store
trust -> identity + objects（只读）+ store
capability -> trust + objects + model（仅内容能力）
personalai -> model + handoff + trust
audit -> store
```

`httpx`、`config`、`model` 只依赖标准库及其冻结依赖。`internal/arch/TestModuleBoundaries` 强制禁止边、无环、子 module 隔离与表归属矩阵。

## 能力范围

本范围保留已发布 `v0.1.0` 的 22 表 Schema，并追加版本 6 迁移以支持活场景对象、社交与公开领域。它激活 Capability Broker、五项冻结能力、`capability_definitions` 同步、调用记录、Open API、TypeScript SDK 与独立 `content.structure` Provider。Broker 复用对象、Handoff、提议、Derive、Trust Kernel 与确认路径，不建立第二套授权、对象写入、衍生或确认机制。

本开发分支新增三个边界，且不建立第二套对象存储：`internal/social` 拥有好友、屏蔽、人与人直接消息与对象讨论，但不授予对象权限；`internal/publication` 拥有发布记录、净化后的版本快照、贡献者同意、公开衍生关系与举报，匿名读取绝不使用私人对象接口；`internal/world` 是面向世界舞台与世界脉搏的只读投影；`internal/stream` 只发送认证刷新信号，不携带对象状态或消息正文。

对象聚合是 `living_objects`、`object_memberships`、`object_events`、`object_action_receipts`、`object_proposals`、`invitations`、`derivation_edges` 的唯一写入者。`internal/derive` 只组装子对象状态，并通过 objects 服务写入；人类端点与确认执行共享同一套准备/变更路径。`internal/trust` 拥有 `delegation_grants` 与 `confirmations`，是唯一 AI 授权裁定点。`internal/personalai` 拥有档案、私人上下文、会话与消息，只能通过窄接口调用 objects 提议写入，绝不写对象状态。`internal/handoff` 只读对象数据。`internal/model` 拥有 `model_invocations`。`internal/identity` 写用户、会话与 PAT；`internal/audit` 在调用方事务内追加审计；`internal/store` 拥有幂等预约表。


## HTTP 管线

```text
RequestID -> SecurityHeaders -> AccessLog -> Recover -> CORS -> BodyLimit
-> ServeMux -> Authenticate -> RateLimit -> LoadResource -> Authorize
-> ValidateSchema -> IdempotencyLookup/Replay -> CheckVersion
-> ExecuteTransaction（业务 + 事件 + 审计 + 幂等完成）-> Respond
```

- 访问日志记录匹配到的路由模板，绝不记录原始 path/query。
- 错误使用封闭错误码与统一信封：
  `{"error":{"code","message","request_id","details"}}`。
- API 响应 `Cache-Control: no-store`；带 hash 的静态资源长缓存；`index.html` 不缓存。
- 未知 `/api/*` 返回 JSON 404；非 API 路径回退到 SPA 壳。

## 数据方向

Trust Kernel 是唯一 AI 授权裁定者。领域模块不写彼此拥有的表；跨模块读取经导出的 Go 接口，跨模块写入经拥有方领域服务。Personal AI 的有效权限 = 成员权限 ∩ 活跃委托 ∩ 约束；提议应用与确认批准前重新校验版本、成员与精确委托。外部能力 Provider 不获得数据库访问或用户令牌。

## 运维假设

`v0.1.0` 仅支持单实例。限流、清理与调度在进程内；正确性依赖数据库约束（幂等、版本检查）。不支持水平扩展。
