# 为 Derive World 贡献

[English](CONTRIBUTING.md) | 中文

感谢你的兴趣。本仓库是公开的 `v0.1.0` OSS MVP。它包括 Digital Self 会话、Living Objects、对象授权、版本化动作、事件历史、Derive、来源链、审计、幂等、邀请与 Derive Link、Handoff、Personal AI 会话、Trust Kernel、提议与人类确认、个人访问令牌、本人导出、Capability Broker、五项首发能力、Open API、TypeScript SDK 与双语参考世界。

## Developer Certificate of Origin (DCO)

本项目使用 Developer Certificate of Origin 1.1（DCO），不使用 CLA。每个提交都必须签名：

```bash
git commit -s -m "your message"
```

生成的 trailer 必须是：

```text
Signed-off-by: Your Name <your@email>
```

贡献按 Apache License 2.0 接受。签名即表示你证明 <https://developercertificate.org/> 中的 DCO。

### 历史

本仓库从干净、符合 DCO 的首次提交开始。公开前的开发历史保留在独立的私有研发档案中，不属于本仓库的 Git 历史。这里不适用任何 DCO 例外。

## 工作流

1. 除非是琐碎修复，先开 issue 描述改动。
2. Fork 仓库，并从 `main` 创建主题分支。
3. 保持改动小而聚焦。`v0.1.0` 的依赖集冻结在 `go.mod`、`go.sum` 与 `package-lock.json`；没有契约理由与维护者审查，不要新增依赖。
4. 运行本地检查：

```bash
gofmt -l .
go vet ./...
go test ./...
npm ci
npm run typecheck
npm run build
```

5. 开 Pull Request。`main` 只接受 squash merge，要求线性历史。每个分支提交都必须签名：

   ```text
   Signed-off-by: Your Name <your@email>
   ```

   GitHub 的 squash 提交信息取自分支提交信息（仓库设置 `squash_merge_commit_message=COMMIT_MESSAGES`），可在合并时编辑。请核对生成的信息，必要时编辑，使 squash 提交保留与提交作者匹配的 `Signed-off-by` trailer。CI 会检查 PR 中的每个提交、每次推送到 `main` 引入的每个提交，以及签名/作者邮箱匹配。推送范围检查在合并后运行，属于检测性而非预防性：丢失的 squash trailer 不能靠后续签名提交修复。失败必须披露并按范围处理，绝不隐藏或谎称已修复。

   本仓库使用 squash-only 合并、线性历史与上述 DCO trailer 检查。

## 发布流程

- Release Tag 不可变。发布需要维护者授权。
- 分支规则要求 Pull Request、线性历史、squash merge 与必需检查。单维护者期间要求 0 个批准；如果第二位维护者获得写权限，将批准要求提高到 1。
- Release 归档由 `Release` 工作流从授权 Tag 构建，确定性打包，并附带 `SHA256SUMS` 与构建清单。`workflow_dispatch` dry-run 从 `main` 构建，永不创建 Tag、Release 或来源证明。Tag 模式要求准确的不可变 Tag 位于当前 `main` 顶端、推送运行检查全绿，以及失败关闭的发布授权记录。工作流只创建 Draft；发布是单独的人工操作。
- 在授权 Tag 上，工作流为每个归档、`BUILD-MANIFEST.json` 与 `SHA256SUMS` 生成并校验 GitHub 构建来源证明，并写入 `ATTESTATION.json`。
- 不要移动、删除或复用 Tag。候选变化时，创建下一个版本。
- 不发布预构建容器镜像；Compose 从源码构建。

## 提交信息

使用简短的祈使句标题与解释原因的主体。引用相关 issue。提交信息或测试夹具中不要包含密钥。

## 代码风格

- Go：标准库优先；`gofmt`；不使用 Web 框架；错误带上下文包装；所有源文件带 `SPDX-License-Identifier: Apache-2.0`。
- TypeScript：`strict`；除非有说明，否则不用 `any`；首个版本不引入外部 UI、状态、i18n 或日期库。
- CSS：原生 CSS 与设计令牌；不使用 Tailwind 或 CSS-in-JS。
- 文档：面向用户的英文与中文文档必须保持同步。

## 安全

不要为漏洞开公开 issue。使用 `SECURITY.md` 描述的私密漏洞报告渠道。

## 许可

贡献即表示你同意你的贡献按 Apache-2.0 许可。不要贡献你无法按这些条款许可的代码。
