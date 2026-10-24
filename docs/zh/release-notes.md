# Derive World v0.1.0 — 发行说明

**状态：** 公开 OSS MVP 版本。
**许可证：** Apache License 2.0。
**仓库：** <https://github.com/QiyanAI/derive-world>
**发行：** <https://github.com/QiyanAI/derive-world/releases/tag/v0.1.0>

Derive World 是一个面向 *Living Object（活对象）* 的开源基础：共享、有版本的对象，人和自己的 Personal AI 可以读取、贡献、衍生，并沿着显式来源链追溯。`v0.1.0` 是可自托管的 OSS MVP，不是生产平台。

## 包含内容

- **数字自我**账户：注册、登录、会话轮换、退出与会话绑定 CSRF。
- **两种 Living Object**：版本化协作文档，以及带投票回执与所有者关闭的互动投票。
- **对象授权**：依据活跃成员记录；无 `read` 返回 404，而不是 403。
- **版本化动作**：`expected_version`、行锁与 `409 version_conflict`。
- **Derive 与来源链**：独立子对象、不可变源快照、`derivation_edges`、隐私裁剪的祖先/后代，且绝不修改父对象。
- **邀请与 Derive Link**：权限上限、有效期、次数、撤销、原子接受与权限精确覆盖的重邀。
- **Handoff**：32 KiB 预算、L1–L4 裁剪、来源标注，且不含私人 AI 上下文、会话、令牌或其他成员的委托。
- **Personal AI**：私人上下文、会话、结构化提议，以及仅人类可执行的应用/批准。
- **Trust Kernel**：委托求交、立即撤销、确认、规范化 payload 哈希与版本重校验。
- **Capability Broker**：五项冻结能力 `object.read`、`object.context`、`object.propose_change`、`object.derive`、`content.structure`。
- **Open API 与 TypeScript SDK**：PAT 授权的能力列表/详情/调用与 invocation 查询、OpenAPI 3.1，以及零运行时依赖的类型化 SDK。
- **双语参考世界**：两个独立用户、两个 Personal AI、邀请、Handoff、提议、确认、历史、来源链、诚实错误状态与 390 px 移动项目。
- **安全控制**：Argon2id 密码、令牌仅存 SHA-256、CSRF 与同源校验、`http_hmac_v2` 响应签名、TLS/双向 TLS 校验、有界 HMAC 密钥轮换、单次/每日模型预算（Provider 调用前在 `model_invocations` 写入持久预占行）、停止开关与不自动重试。预占行在崩溃、业务回滚或最终更新失败后仍被计入，重启后也不会漏记。

## 安装与运行

### Docker Compose（推荐）

前置：Docker Engine 与 Compose v2、约 2 GB 磁盘、浏览器。服务只绑定 loopback。

```bash
git clone https://github.com/QiyanAI/derive-world.git
cd derive-world
cp .env.example .env
docker compose up --build -d
curl -fsS http://127.0.0.1:8080/readyz
```

打开 <http://127.0.0.1:8080>。Fixture 模式不需要模型账户，所有 AI 输出标记为 `FIXTURE`；它不是真实 AI。Real AI 模式要求用户配置 OpenAI-compatible 端点与 API Key，模型服务费用由用户自行承担。

### 平台归档

本次发行提供以下资产：

| 平台 | 资产 |
|---|---|
| Linux amd64 | `derive-world_0.1.0_linux_amd64.tar.gz` |
| macOS arm64 | `derive-world_0.1.0_darwin_arm64.tar.gz` |
| Windows amd64 | `derive-world_0.1.0_windows_amd64.zip` |
| 源码 | `derive-world_0.1.0_source.tar.gz` |

每个平台归档包含服务端二进制、构建后的 Web 客户端 `web/dist`、`capabilities.yaml`、确定性 Fixture 数据 `tests/fixtures/model`、`.env.example`、`README.md`、`README.zh-CN.md`、`SHOWCASE.md`、`LICENSE`、`NOTICE`、`THIRD_PARTY_NOTICES.md`、`SECURITY.md`、`SECURITY.zh-CN.md`、`TRADEMARK.md`、`CONTRIBUTING.md`、`CONTRIBUTING.zh-CN.md`、`CODE_OF_CONDUCT.md`、`CHANGELOG.md`、锁定的依赖清单（`go.mod`、`go.sum`、`package.json`、`package-lock.json`）、OpenAPI 契约，以及双语 `docs/` 目录（含本发行说明与演示素材）。解压归档并进入顶层目录。独立二进制只读取进程环境变量，不会自动加载 `.env`；请先导出：

```bash
cd derive-world_0.1.0_linux_amd64
cp .env.example .env
set -a; . ./.env; set +a
./derive-world migrate
./derive-world serve
```

Windows 下二进制为 `derive-world.exe`；请用 PowerShell 或 cmd 导出 `.env` 变量。Windows 上的部分 POSIX shell 会重写 `MODEL_CHAT_PATH` 等绝对路径；可将其留空以使用默认 `/v1/chat/completions`。服务端需要 PostgreSQL、三个至少 32 字节的密钥，以及包含 `index.html` 的 `WEB_DIST_DIR`；归档已包含 `web/dist` 与 `capabilities.yaml`。参考 `content.structure` Provider 是独立进程；完整体验请使用 Compose。

## 验证

`v0.1.0` Tag 不可变，并指向最终的公开 `main` 提交。Release 工作流从该准确 Tag 使用 Go 1.27.3 与 Node 24.14.0 构建，对二进制与归档执行双次构建并逐字节比较，生成源码归档、`SHA256SUMS` 与 `BUILD-MANIFEST.json`，并且只创建 Draft Release。只有在资产哈希、清单、权限与来源证明全部通过后，才正式发布该 Draft。

Release 页面上的 `SHA256SUMS` 覆盖全部资产。工作流为每个归档、`BUILD-MANIFEST.json` 与 `SHA256SUMS` 生成 GitHub OIDC/Sigstore 构建来源证明。可使用以下命令验签：

```bash
gh attestation verify derive-world_0.1.0_linux_amd64.tar.gz \
  --repo QiyanAI/derive-world \
  --source-digest <该 Tag 对应的提交 SHA> \
  --signer-workflow QiyanAI/derive-world/.github/workflows/release.yml
```

SHA-256 证明文件完整性；来源证明将资产绑定到本仓库、提交与 Release 工作流身份。要求的检查为 `Go`、`Web`、`SDK`、`Architecture`、`Contract`、`Integration`、`govulncheck`、`gitleaks` 与 `E2E`；发布预检还要求同一 SHA 上的 Security `License` 与 `npm audit` 任务。`E2E` 使用真实 PostgreSQL、真实 Go API、生产 Web 构建，以及 Chromium、Firefox 与 390 px 移动项目，由两个独立用户执行完整闭环。GitHub Dependency Review 在公开 Pull Request 上运行；GitHub 私密漏洞报告已启用。

## 已知限制

完整列表见[限制](https://github.com/QiyanAI/derive-world/blob/main/docs/zh/limitations.md)。主要限制：

- 仅单实例；不支持横向扩展或实时协作。
- 不发邮件、不做密码找回、不做 OAuth、不验证邮箱、不提供账号删除。
- 每个部署只激活一个模型适配器；不支持多 Provider 并发路由或按 Personal AI 选择模型。
- Real AI 仅对已批准的双 Personal AI 两次 API 级调用（DeepSeek `deepseek-flash`）记为 VERIFIED_REAL；未在发布 SHA 上重跑，不代表所有模型或 Provider。
- 没有历史对象状态读取端点；从旧版本衍生使用不可变快照。
- 不发布预构建容器镜像；Compose 从源码构建。
- `v0.1.0` 是 OSS MVP，不是生产级平台；不支持也不声称生产使用。

## 安全

漏洞请通过 GitHub 私密漏洞报告私下提交：在仓库的 **Security** 页面选择 **Report a vulnerability**。不要为疑似漏洞开公开 issue。见 [SECURITY.md](https://github.com/QiyanAI/derive-world/blob/main/SECURITY.md)。

已实现的控制、模型数据流与不可信内容处理见[安全](https://github.com/QiyanAI/derive-world/blob/main/docs/zh/security.md)。

## 流程说明

- `v0.1.0` 不可变。Tag 永不移除、删除或复用。若候选发生变化，创建下一个版本。
- 公开仓库从干净、符合 DCO 的首次提交开始。公开前的开发历史保留在独立的私有研发档案中，不属于本仓库的 Git 历史。
- 贡献采用 Developer Certificate of Origin 1.1。
- 限定的 Real AI 证据记录于 2026-10-24。Real AI 证据为对 DeepSeek `deepseek-flash` 的恰好两次 API 级调用；本次发布未重跑，也不代表所有 Provider。
- `v0.1.0` 是首个公开版本。
- 不要把发布候选资产复用为 `v0.1.0` 资产。本次发布从自己的 Tag 重新构建并生成来源证明。
