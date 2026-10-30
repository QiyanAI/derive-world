# Windows 自托管

本页面向 **自托管管理员**。普通成员不需要这些步骤，只需用浏览器访问实例地址。

Windows 上受支持的路径是：源代码检出 + Docker Desktop + Docker Compose v2。
发布包里的 `windows_amd64` 压缩包只有服务端二进制，不包含 PostgreSQL、Compose
栈或 Web 构建；要运行完整本地实例，请使用下面的代码检出方式。

## 1. 安装依赖

1. 安装 [Docker Desktop](https://www.docker.com/products/docker-desktop/)。
   Windows 家庭版可让它安装或使用 WSL 2
   （[WSL 安装说明](https://learn.microsoft.com/windows/wsl/install)）。
2. 启动 Docker Desktop，等待其显示引擎正在运行。
3. 可选：安装 [Git for Windows](https://git-scm.com/download/win)，便于克隆和更新。
4. 确认当前账户可以运行 Docker。如果 Docker Desktop 提示加入 `docker-users`
   组，请在修改后注销并重新登录。

启动器不会静默安装任何软件，只会说明缺少什么、为什么需要、到哪里安装。

## 2. 获取源代码

克隆仓库：

```text
git clone https://github.com/QiyanAI/derive-world.git
```

或从仓库页面下载源码压缩包并解压。如果 Windows 将下载文件标记为阻止，在仓库
根目录的 PowerShell 中执行一次：

```powershell
Get-ChildItem -Recurse | Unblock-File
```

## 3. 双击启动

双击仓库根目录的 **`Start Derive World.cmd`**。

启动器会：

1. 检查 Docker、Docker Engine、Compose v2、8080 端口、实例目录、`.env`、
   数据库卷，以及是否有另一个检出占用了共享的 Compose 项目。
2. 仅在 `.env` 不存在时从 `.env.example` 创建；已有 `.env` 永远不会被覆盖。
3. 启动服务并等待 `/readyz` 就绪。
4. 显示版本和模型模式，并打开产品地址。

首次构建可能需要数分钟。失败时窗口会保留，显示已脱敏的诊断和恢复说明。任何
命令都不会删除数据库卷。

### 一个 Docker 引擎只运行一个实例

Compose 项目名和数据库卷是固定的（`derive-world` 和
`derive-world_dw_pgdata`）。同一 Docker 引擎上的两个检出会共享容器、8080
端口和数据。启动器会通过 Compose 标签识别其他检出，并拒绝启动、停止或修改它。
请先在对方自己的目录中停止。

## 4. 首次使用与 Fixture 模式

新建的 `.env` 使用 `MODEL_MODE=fixture`。Fixture 模式不会调用任何模型服务；
AI 回复是确定性的演示输出，并在产品中标记为 `FIXTURE`。**你创建的对象是真实
的，会保存在本地 PostgreSQL 卷中**。没有 API Key 也可以创建、编辑、衍生、
邀请、公开并再次进入。

注册第一个账号后进入世界舞台。系统不会在用户背后创建示例数据。选择
**开始一个项目**（步骤、进展、结果）或 **开始一个故事**（场景、选择、方向），
两者使用同一套对象与衍生机制。

## 5. 连接真实 AI（可选）

使用启动器菜单：

- **6. AI settings / Connect AI — 打开配置文件**：用记事本打开本实例真实的
  `.env`，并显示需要填写的四个字段。
- **7. AI settings / Connect AI — 应用并验证**：验证字段、重建应用容器、等待
  就绪，并检查 `/api/v1/meta` 是否报告请求的模型模式。如果新配置未能健康运行，
  启动器会恢复上一份 `.env` 备份并重启。

只修改：

```dotenv
MODEL_MODE=real
MODEL_BASE_URL=https://your-provider.example
MODEL_NAME=your-model-name
MODEL_API_KEY=your-api-key
```

`MODEL_BASE_URL` 是服务商根地址，**不含** `/v1`，结尾也不要带斜杠。模型接入
保持厂商中立：任何 OpenAI-compatible 服务均可。

启动器只检查 DNS、TCP 和 TLS，不发起推理调用，因此不会证明某个模型名真实可调用，
也不会产生费用。它会区分未配置、密钥格式错误、地址错误、网络不可达和 TLS 失败。

### 密钥处理

- 密钥只保存在本实例的 `.env` 中，不发送给浏览器、不写入浏览器存储、不进入
  URL、不打印到日志。
- Windows 上启动器用 `icacls` 将 `.env` 限制为当前用户、SYSTEM 和
  Administrators。
- `.env` 和容器环境变量**不是加密保险库**。本机管理员或 Docker 管理员仍可读取。
  不声称“绝对零泄露”。
- 普通成员、好友、匿名访问者和 Personal AI 永远无法获得部署级配置。
- 启动器不开放公网配置接口，也不接受来自网页会话的 API Key。

要返回确定性演示，把 `MODEL_MODE` 改为 `fixture` 后应用。

## 6. 日常操作

- **停止**：保留容器和数据库卷。
- **重启**：重建应用容器，保留全部数据。
- **状态**：显示 Compose 服务、`/healthz`、`/readyz`、版本、提交和模型模式。
- **诊断**：只显示脱敏信息；密钥值被遮盖，DSN、Bearer、Cookie 和邮箱会被过滤。
- **打开**：`PUBLIC_BASE_URL` 是绝对 http(s) 地址时使用它，否则使用
  `http://127.0.0.1:8080`。

产品地址必须与 `PUBLIC_BASE_URL` 一致。修改端口时，必须同时修改 Compose 端口
映射和 `PUBLIC_BASE_URL`；否则登录及其他写操作会因来源校验被拒绝。

## 7. 数据与备份

所有世界数据都在 Docker 命名卷 `derive-world_dw_pgdata` 中。停止、重启和升级
不会删除它。备份请使用[自托管文档](self-hosting.md)中的逻辑导出流程；不要从
Docker Desktop 虚拟机中直接复制运行中的卷文件。

不要对需要保留数据的实例执行 `docker compose down -v`。启动器没有删除卷的操作。

## 8. 故障排查

| 现象 | 处理 |
|---|---|
| 找不到 Docker CLI | 安装 Docker Desktop，重新打开启动器。 |
| 引擎未运行 | 启动 Docker Desktop，等待“Engine running”。 |
| 8080 被非 Docker 程序占用 | 关闭该程序；启动器不会结束进程。 |
| 另一个检出占用了项目 | 在对方目录中停止；不要同时运行两个副本。 |
| `/readyz` 一直不就绪 | 运行诊断，检查数据库容器和迁移日志；不要删除卷。 |
| 登录或注册返回 403 | 打开与 `PUBLIC_BASE_URL` 完全一致的来源，例如 `http://127.0.0.1:8080`，而不是其他域名。 |
| Real 模式启动失败 | 检查四个 `MODEL_*` 字段；启动器会验证并可恢复上一份备份。 |
| TLS 验证失败 | 检查系统时间、代理和证书链。 |

## 9. 范围

这是 Windows 自托管路径。Linux 和 macOS 继续使用
[自托管文档](self-hosting.md)中的 Compose 说明。启动器不声称其他系统具有同等
免终端体验。GitHub 托管的 Windows 运行器无法提供 Linux Docker 引擎，因此 CI
只运行启动器自检、环境检测和静态安全检查。在验收机器上已执行只读检查、
其他实例检测、诊断和状态；由于另一个代码检出占用了默认 Compose 项目，启动器
的完整启动/停止/重启未被强行执行，最终报告中标记为 `NOT_YET_VALIDATED`，
不会冒充通过。
