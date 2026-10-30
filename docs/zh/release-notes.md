# Derive World v0.2.0 — 发行说明

**状态：** 公开 OSS 发行版，V2 产品阶段。
**许可证：** Apache License 2.0。
**仓库：** <https://github.com/QiyanAI/derive-world>
**发行页：** <https://github.com/QiyanAI/derive-world/releases/tag/v0.2.0>

Derive World 是一个面向 *Living Object（活对象）* 的开源基础：共享、有版本的对象，人和自己的 Personal AI 可以读取、贡献、衍生，并沿着显式来源链追溯。

`v0.2.0` 是第二个公开版本，也是 V2 产品阶段。用户用自己的话描述一个世界；Personal AI 提出一份具体方案；只有人类可以批准；随后服务器创建真实可运行的世界，用户可以进入、继续用自然语言改变它，并衍生为独立的新世界。它是可自托管的 OSS 发行版，不是生产平台。

## v0.2.0 新增内容

- **一句话创造世界。** 输入是自由意图。Personal AI 写出一份紧凑的世界种子，服务器工坊将其展开为真实内容、状态与体验。方案在写入前完成校验，以产生它的回复正下方的唯一内联卡片呈现，人类批准前不创建任何东西。
- **空白世界启动。** 空世界可以直接用自然语言开始，不需要手工配置 Schema、能力或 JSON。
- **连续修改。** 绑定的 Personal AI 读取当前世界状态与版本，提出一个可合并的修改（`batch_change` 或 `reshape_world`），批准后才应用。每次应用恰好增加一个版本、保留原有事物 id、刷新后保持；冲突会被拒绝而不是覆盖。
- **衍生。** 世界可以衍生为独立子世界，来源与版本关系正确保存，原世界不被修改。
- **多形态世界。** 同一个世界可以组合：基于时间的二维实时场景、可读互动故事、可编辑工作台、可视关系空间、上传的图像与视频、上传或程序生成的音频与事件提示、带客户端重力与碰撞的真实三维场景、受控声明式自定义表面，以及组合它们的标签页。所有表面共享同一份内容、状态、变量、权限、版本与来源链。
- **世界舞台。** 登录后的首页展示真实对象、可见衍生分支、最近真实事件、待人类批准事项与世界脉搏，而不是合成仪表盘。
- **好友与直接交流。** 好友请求、直接消息、屏蔽、对象讨论与对象邀请；好友关系本身不授予对象访问权。
- **探索与公开。** 所有者可以按版本公开，并附带许可证与可选的衍生许可。匿名访客只看到已存储的脱敏快照；有其他贡献者时需要贡献者同意。撤销后停止新的公开读取。
- **统一 Personal AI。** 用户级 Personal AI 在第一个世界之前就存在，在不同世界上下文中保持隔离的私人会话，并且只有一个主要 AI 入口，而不是多个相互冲突的操作窗口。
- **有真实边界的媒体。** 上传的图像、视频、音频与二进制 glTF 模型按权限隔离、按内容校验、由本服务器提供，字节永不进入对象状态。本发行版不声称能够自动生成任意媒体、视频或三维作品。
- **Schema 11（36 表）。** 迁移 11 将每个 AI 发起的确认与提议关联到会话和确切的助手消息。迁移可从 `v0.1.0` 向前应用；升级前请备份。

## 真实模型证据与确切范围

真实 AI 是运营者配置的模式，用运营者的密钥调用 OpenAI 兼容服务；费用由运营者承担。Fixture 模式不调用模型，始终标注 `FIXTURE`；Fixture 输出不是真实 AI。

主要真实模型证据记录在发布候选提交 `4962b9b`（树 `7c1bb11`）上。带标签的 `v0.2.0` 树与该候选相比，增加了文档更新与发布修复（私有衍生内容的发布同意检查、GIF/GLB 有界校验、部分世界修改保留未提及的字段值、移除成员时撤销其邀请）；真实模型流程**未**在确切的标签提交上重跑，发行页会标明实际交付的提交与树。证据是：

- **同一世界连续三次自然语言修改**：每次产生一个方案、人类批准、恰好一个版本增量、保留原有事物 id、刷新后保持。第三次提供方返回空内容，服务器的自动干净上下文重试在不清空会话的情况下成功。
- 更早在最终代码之前（`288057e`）的经所有者授权复验覆盖了一句话创建、原世界不变的 AI 衍生、无原始 JSON 的批准，以及多方案冲突处理。该复验也记录了一个部分案例：两次提供方返回空内容，另一次回复产生了超过可应用数量的方案；多余方案以 `409` 被拒绝，未被应用。

所有者于 2026-10-30 使用发布二进制进行个人验收，实际执行了创建、修改与 `world.reimagine`。该会话中提供方有时返回空内容；服务器用干净上下文重试并成功。畸形的世界种子被安全拒绝，给出可读错误并可重试。

这些证据针对该源码树与 DeepSeek `deepseek-flash` 的这些流程，不构成对所有模型或 Provider 的普遍验收。严格校验仍可能拒绝畸形的模型方案；拒绝是安全且可重试的。发布构建、CI 与任何测试都不发起付费模型调用。

## 安装与运行

### Docker Compose（推荐）

要求：Docker Engine 与 Compose v2、约 2 GB 可用磁盘、浏览器。服务只绑定本机回环地址。

```bash
git clone https://github.com/QiyanAI/derive-world.git
cd derive-world
cp .env.example .env
docker compose up --build -d
curl -fsS http://127.0.0.1:8080/readyz
```

打开 <http://127.0.0.1:8080>。默认 `.env` 使用 Fixture 模式，不需要模型账号。要使用真实 AI，设置 `MODEL_MODE=real`，并配置你的 Provider 地址、模型名与 API Key。密钥只保存在服务端环境变量中，不会发送到浏览器。

Windows 上受支持的完整本地路径是源码检出 + Docker Desktop + 双击启动器：见 [Windows 自托管](windows.md)。发布的 Windows zip 包含服务器二进制与资源，不包含 PostgreSQL 或 Compose 栈。

### 平台归档

| 平台 | 资产 |
|---|---|
| Linux amd64 | `derive-world_0.2.0_linux_amd64.tar.gz` |
| macOS arm64 | `derive-world_0.2.0_darwin_arm64.tar.gz` |
| Windows amd64 | `derive-world_0.2.0_windows_amd64.zip` |
| 源码 | `derive-world_0.2.0_source.tar.gz` |

每个平台归档包含服务器二进制、`web/dist` 下的构建产物、`capabilities.yaml`、确定性 Fixture 数据、`.env.example`、双语 README、许可证与治理文件、依赖清单、OpenAPI 契约与双语 `docs/` 目录。二进制读取进程环境变量，不自行加载 `.env`。

```bash
cd derive-world_0.2.0_linux_amd64
cp .env.example .env
set -a; . ./.env; set +a
./derive-world migrate
./derive-world serve
```

服务器需要 PostgreSQL、三个至少 32 字节的密钥，以及包含 `index.html` 的 `WEB_DIST_DIR`。包含独立 `content.structure` Provider 的完整参考栈由 Compose 提供。

## 验证

`v0.2.0` 标签不可变，指向最终的公开 `main` 提交。Release 工作流从该标签构建，使用锁定的 Go 与 Node 工具链，两次构建二进制与归档并逐字节比较，生成源码归档与 `SHA256SUMS`，写入 `BUILD-MANIFEST.json`，只创建草稿 Release。公开发布是所有者另行明确执行的动作。

标记提交上的必需检查为 `Go`、`Web`、`SDK`、`Architecture`、`Contract`、`Integration`、`govulncheck`、`gitleaks`、`E2E`、`License` 与 `npm audit`。E2E 使用真实 PostgreSQL、真实 Go API、生产 Web 构建、Chromium、Firefox 与 390 px 移动项目。

验证下载资产：

```bash
gh attestation verify derive-world_0.2.0_linux_amd64.tar.gz \
  --repo QiyanAI/derive-world \
  --source-digest <v0.2.0 标签提交 SHA> \
  --signer-workflow QiyanAI/derive-world/.github/workflows/release.yml
```

`SHA256SUMS` 覆盖每个资产。SHA-256 证明文件完整性；存在证明时，证明把资产绑定到本仓库、提交与工作流身份。

## 已知限制

完整列表见[限制](limitations.md)。主要限制：

- 单实例；不支持水平扩展或实时多人协作。
- 不发邮件、不做密码找回、不做 OAuth、不验证邮箱、不提供账号删除。
- 每个部署一个模型适配器；不支持并发多 Provider 路由或按 Personal AI 选择模型。
- 真实 AI 仅对上述流程与 Provider 验证，不构成对所有模型或 Provider 的验收。严格校验可能拒绝畸形方案；拒绝安全且可重试。
- 媒体支持有边界：上传文件与按世界/按用户配额、简单客户端物理、受控声明式自定义表面；不是任意媒体或三维生成。
- 匿名 Explore 预览展示元数据摘要，不是实时组合体验。
- 好友与公开功能在同一个可达实例上工作；`127.0.0.1` 不是外部分享链接。
- `v0.2.0` 是 OSS 发行版，不是生产平台；不支持也不声称生产使用。

## 安全

请通过 GitHub Private Vulnerability Reporting 私下报告漏洞：打开仓库 **Security** 标签页并选择 **Report a vulnerability**。不要为疑似漏洞开公开 Issue。见 [SECURITY.md](https://github.com/QiyanAI/derive-world/blob/main/SECURITY.md) 与[安全](security.md)。

## 流程说明

- 发行标签不可变。已发布的标签不会被移动、删除或复用；候选变化进入下一个版本。
- 贡献使用 Developer Certificate of Origin 1.1。合并后 push-range DCO 检查是权威检查。
- 发行版从自己的标签构建与证明；发布候选资产不得复用为发行资产。
- `v0.2.0` 是 V2 产品阶段发行版。`v0.1.0` 继续可用且不变；其说明保存在 [release-notes-v0.1.0.md](release-notes-v0.1.0.md)。
