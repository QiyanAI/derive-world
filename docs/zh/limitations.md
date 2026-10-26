# 限制

这是 `v0.1.0` 公开 OSS MVP，也是首个公开版本。已批准的双 Personal AI 两次 API 级调用
（DeepSeek `deepseek-flash`）仅对该范围记为 **VERIFIED_REAL**；
未在发布 SHA 上重跑，不构成普遍模型/Provider 验收。数字自我会话、Living Object、对象授权、版本化动作、事件历史、
Derive、来源链、审计、幂等、邀请与 Derive Link、Handoff、Personal AI 会话、
Trust Kernel、AI 提议/确认、PAT、本人导出、Capability Broker、五项首发能力、
Open API、TypeScript SDK、独立 `content.structure` Provider、响应签名传输、
TLS 校验、HMAC 密钥轮换与模型预算控制已实现。这是可自托管的 OSS MVP，不是生产平台。

## v0.1.0 已实现

- 本地注册、登录、会话轮换、退出与会话绑定 CSRF。
- 注册时同事务创建 Personal AI 档案与空私人上下文。
- 协作与互动两种 Living Object。
- 依据活跃成员记录进行所有者/成员授权；无 `read` 一律 404。
- 显式 `expected_version`、行锁与 `409 version_conflict`。
- 追加写对象事件与 `seq` 游标。
- 从当前版本或不可变历史快照衍生。
- 独立子对象状态、清单/选项 ID 重生成、投票清零与 `derivation_edges` 来源。
- 来源链深度上限与不可读祖先/后代隐私裁剪。
- 审计同事务、创建/动作/衍生的幂等与软删除。
- 一次性邀请/Derive Link：权限上限、有效期、次数、撤销、原子接受，以及重邀时权限精确覆盖。
- 成员移除与所有者保护；移除后立即 404。
- Handoff 投影：32 KiB 预算、L1–L4 裁剪、固定事件摘要字段、来源标注，且不含私人 AI 数据。
- 私人 Personal AI 会话与消息、Fixture/Real 模式、一次回合一次模型调用、结构化动作解析与模型计量。
- Trust Kernel 委托求交、立即撤销、确认创建/批准/拒绝/过期、规范化 payload 哈希与版本重校验。
- AI 状态修改提议与人类应用/拒绝/过期；AI 衍生只能通过五分钟人工确认。
- 个人访问令牌（封闭 scopes，Cookie-only 端点排除）。
- 仅本人 `/me/export`，每小时一次。
- 双语参考世界：私人上下文、档案、委托、邀请、Handoff 预览、AI 会话、提议、确认、令牌、导出。
- Capability Broker：配置注册、启动 Schema 校验、数据库同步、幂等、限流/并发与调用审计。
- 五项冻结能力：`object.read`、`object.context`、`object.propose_change`、`object.derive`、`content.structure`。
- PAT 可用的能力列表/详情/调用与 invocation 查询 Open API，以及 OpenAPI 3.1 与零依赖 TypeScript SDK。
- 独立、仅标准库的参考 Provider：内部网络 HTTP+HMAC、重放防护、SSRF 防御与输出校验。
- `http_hmac_v2` 响应签名绑定请求时间戳、Invocation、HTTP 状态与响应体精确哈希；未签名或被篡改的响应视为未知结果，输出绝不入库。
- TLS 校验（最低 TLS 1.2）、私有 CA 与双向 TLS，以及对明文 Provider URL 配置 TLS 的启动拒绝。
- 有界 HMAC 密钥轮换：当前密钥与带 RFC3339 截止时间的旧密钥；过期、重复或相同旧密钥按失败关闭。
- 单次模型输入/输出上限、每用户每日预算、部署级每日硬上限、运维停止开关、调用前拒绝、审计与重启后从 `model_invocations` 重新计数。
- 阻断式 Chromium + Firefox Playwright 套件，两个独立用户、真实 PostgreSQL/API，以及 390 px 移动项目。

## 尚未实现

- 普遍的多 Provider 验收。Real AI 仅对已批准的双 Personal AI 两次 API 级调用（DeepSeek `deepseek-flash`）记为 VERIFIED_REAL；未在发布 SHA 上重跑，不代表所有模型或 Provider 均已验收。
- 多 Provider 并发路由、按用户或按 Personal AI 选择模型、本地模型托管。

参考客户端展示可用的界面。没有伪造成功的占位路由；Real AI 仍是发布前硬门禁。

## 产品限制

- 仅单实例；不支持横向扩展。
- 每个部署只激活一个模型适配器。Provider 接口与部署配置是厂商中立的，但更换模型仍需修改部署配置，不能同时运行多个 Provider。
- 不支持同时协同编辑。对象工作区使用认证 SSE 刷新信号并带轮询降级；过期写入返回版本冲突，而不是静默覆盖。
- 好友、直接消息、公开世界与公开衍生均限于同一实例；不支持跨实例联邦或公开导入。
- `v0.1.0` 不发邮件、不做密码找回、不做 OAuth、不验证邮箱、不提供账号删除。
- `v0.1.0` 不提供独立产品官网或原生移动客户端。
- Fixture 是确定性演示输出，不得表述为真实 AI。
- Compose 是参考部署，不是生产编排。
- 默认 Compose 栈在内部明文 `capnet` 上运行 Provider，但响应已签名。TLS 与双向 TLS 已实现、已测试并可通过配置启用，默认参考栈未启用。公网 Provider 必须使用 HTTPS 与 `http_hmac_v2`。
- 旧 `http_hmac` 传输不签名响应，仅为内部兼容保留。
- 模型预算使用单实例内存计数，在每个 UTC 日首次调用时从 `model_invocations` 重新载入。实际尝试 Provider 调用前会写入持久预占行并计入预算；进程崩溃、业务回滚或最终更新失败都不会漏记可能产生费用的调用。多个 app 副本仍不共享内存并发/计数状态；持久行防止漏记费用，但不提供多副本协调。
- `v0.1.0` 没有历史对象状态读取端点；历史是事件与版本摘要，从旧版本衍生使用不可变快照。
- GitHub Dependency Review 在公开 Pull Request 上运行，是发布门禁的一部分；要求的检查同时覆盖 govulncheck、gitleaks、npm audit 与 License。
- 不发布预构建容器镜像；Compose 从源码构建。
- `v0.1.0` 发行资产由公开发布工作流生成 GitHub 构建来源证明；请使用 `gh attestation verify` 针对本仓库、Tag 与工作流验签。

## 数据限制

- 已发布的 `v0.1.0` 为 22 表 Schema。本开发分支新增版本 6 迁移，包含社交、公开、场景、举报与公开衍生的表（共 30 表）；旧迁移不被重写。
- 对象状态上限 64 KiB；`shared_context` 上限 8 KiB。
- AI 提议 input 存储上限 64 KiB。提议创建与应用适用不同限额：应用受 64 KiB 状态约束；`object.state_changed` 事件 payload 上限为 132,352 字节，其他事件仍为 96 KiB。
- 20,000 是 Unicode **code point** 上限，不是字节保证。按负责人 V1 解释，20,000 code point 的正文上限与 65,536 字节的完整状态上限**同时生效**。能装入状态的 20,000 个 ASCII、中文或混合字符可以应用；20,000 个四字节 Emoji（约 80,000 字节）超出状态上限，返回 `413 payload_too_large` 且无副作用。20,001 个 ASCII 即使字节数低于 64 KiB，也返回 `400 invalid_request`。
- 大小限额按 PostgreSQL `jsonb` 文本计量；包含 `&`、`<`、`>` 或 U+2028/U+2029 的合法正文不会因 JSON HTML 转义而被错误拒绝。
- 能力输入上限 64 KiB；外部 Provider 的 HTTP 正文上限为该输入加精确的 126 字节签名信封。
- 审计 metadata 上限 4 KiB，拒绝嵌套值。
- 幂等响应上限 256 KiB。
- 备份为手工逻辑转储，不做 PITR。
- 删除 `dw_pgdata` 卷会销毁本地数据。

## 诚实状态

不声称生产就绪、安全认证、用户采用、Star、下载量或融资。`v0.1.0` 是首个公开版本。发布证据保存在独立的私有研发档案中；本树只包含产品与发布文档。已批准的双 Personal AI 两次 API 级调用（DeepSeek `deepseek-flash`）完成了恰好两次调用（A 764/51 Token，B 804/49 Token；非高峰约 US$0.00030，高峰上界 US$0.00059），仅对该范围记为 VERIFIED_REAL；未在发布 SHA 上重跑。
