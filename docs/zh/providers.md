# 独立能力 Provider

参考 Provider 在独立进程中执行 `content.structure`。它是仅使用标准库的 Go module（`examples/capability-provider`），实现冻结的 D06 HTTP+HMAC 协议。它不导入 Derive World 主模块、不连接 PostgreSQL，也不会接收用户 Cookie、PAT、CSRF 令牌、私人 AI 上下文或数据库凭据。

## 拓扑

```text
浏览器 / SDK
     |
     v
Derive World app (Broker)  --HMAC-->  capability-provider:8081
     |                                    (capnet, internal)
     +-- PostgreSQL (data, internal)
```

参考 Compose 中 Provider 只加入 `capnet`（`internal: true`），不发布任何宿主端口。app 同时加入 `capnet`、`data` 与 `edge`；db 只在 `data`。

## 启动参考栈

Provider 由默认 Compose 文件构建并启动，无需额外命令。栈就绪后：

```bash
docker compose ps capability-provider
```

`PORTS` 列必须为空。app 通过 `http://capability-provider:8081` 访问，即 `capabilities.yaml` 中的 `base_url`。

隔离检查：

```bash
docker compose exec capability-provider env
docker compose exec capability-provider sh -c 'wget -T 2 -qO- http://example.com'
```

第一条不得包含 `DATABASE_URL`；第二条必须失败，因为 Provider 位于无公网出站的内部网络。

## 在 Compose 之外运行

```bash
cd examples/capability-provider
go build -o capability-provider .
```

在仅 Derive World app 可达的主机启动：

```bash
CAPABILITY_HMAC_SECRET="$(openssl rand -base64 32)" \
LISTEN_ADDR="10.0.0.12:8081" \
./capability-provider
```

环境变量：

| 变量 | 必填 | 默认 | 规则 |
|---|---|---|---|
| `CAPABILITY_HMAC_SECRET` | 是 | — | ≥32 字节，与 Broker 完全一致。 |
| `CAPABILITY_HMAC_KEY_ID` | 否 | `k1` | 小写 key id，1..16 字符；与 Broker 一致。 |
| `CAPABILITY_HMAC_PREVIOUS_SECRET` | 否 | — | 轮换重叠旧密钥，≥32 字节且不同于当前密钥。 |
| `CAPABILITY_HMAC_PREVIOUS_KEY_ID` | 与旧密钥同用 | — | 必须不同于当前 key id。 |
| `CAPABILITY_HMAC_PREVIOUS_UNTIL` | 与旧密钥同用 | — | RFC3339 截止时间，启动时必须位于未来；截止后旧密钥失效。 |
| `REQUIRE_RESPONSE_SIGNING` | 否 | `true` | 拒绝未协商 v2 响应签名的请求。 |
| `TLS_CERT_FILE`, `TLS_KEY_FILE` | 否 | — | 同时设置时提供 HTTPS；启动时校验证书对。 |
| `TLS_CLIENT_CA_FILE` | 与客户端证书同用 | — | 校验客户端证书的 CA。 |
| `TLS_REQUIRE_CLIENT_CERT` | 否 | `false` | 要求并校验双向 TLS 客户端证书。 |
| `LISTEN_ADDR` | 否 | `:8081` | 私有网络内监听地址。 |
| `PROVIDER_ID` | 否 | `reference.content-structure` | 其他值启动失败。 |
| `CONTRACT_VERSION` | 否 | `1.0` | 其他值启动失败。 |
| `MAX_BODY_BYTES` | 否 | `65536` | 业务输入上限，1..65536。HTTP 正文上限为 `MAX_BODY_BYTES + 126`，即精确的 D06 信封开销。 |
| `REPLAY_CACHE_SIZE` | 否 | `4096` | 正整数 LRU 容量。 |

修改 `capabilities.yaml`（或 `CAPABILITIES_CONFIG` 指定的文件）并重启 app：

```yaml
providers:
  - provider_id: reference.content-structure
    transport: http_hmac_v2
    base_url: "https://provider.internal:8081"
    network: internal
    allowed_hosts:
      - provider.internal
    timeout_ms: 10000
    max_input_bytes: 65536
    max_output_bytes: 262144
    # 可选，仅限 https；相对路径以本文件目录为基准。
    tls_ca_file: provider-ca.pem
    tls_server_name: provider.internal
    tls_client_cert_file: broker-client.pem
    tls_client_key_file: broker-client-key.pem
```

`base_url` 只允许 `scheme://host[:port]`；host 必须逐字出现在 `allowed_hosts`。`network: internal` 允许私有地址与 `http`；`network: public` 要求 `https` 与公网地址。

## 协议

Broker 每次调用发送一个请求：

```http
POST /v1/invoke HTTP/1.1
Content-Type: application/json; charset=utf-8
X-DW-Provider-Id: reference.content-structure
X-DW-Capability-Id: content.structure
X-DW-Contract-Version: 1.0
X-DW-Invocation-Id: 018f3f2f-8b7d-7a58-9f11-2f5f5f7a9b21
X-DW-Timestamp: 1792713600
X-DW-Signature: 9f2c...e1
X-DW-Key-Id: k1
X-DW-Response-Signature-Version: v2
User-Agent: derive-world-broker/0.1.0

{"invocation_id":"018f3f2f-8b7d-7a58-9f11-2f5f5f7a9b21","capability_id":"content.structure","contract_version":"1.0","input":{"object_id":"018f3f2f-8b7d-7a58-9f11-2f5f5f7a9b21","text":"# 标题\n\n正文","format":"markdown"}}
```

签名为小写十六进制 `HMAC-SHA256(secret, timestamp + "." + raw body)`。对 `content.structure`，固定信封在输入外恰好增加 126 字节（固定键值加一个 UUIDv7）。`MAX_BODY_BYTES` 约束业务输入，HTTP 正文上限为 `MAX_BODY_BYTES + 126`，因此合法的 65,536 字节输入可被送达。超过 `MAX_BODY_BYTES` 的输入由 Provider 返回 413，绝不低于承诺的输入容量。Provider 在解析 JSON 前校验时间戳（±30 秒）、Provider/能力/契约/invocation 头与签名，返回：

```json
{
  "invocation_id": "018f3f2f-8b7d-7a58-9f11-2f5f5f7a9b21",
  "capability_id": "content.structure",
  "contract_version": "1.0",
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
  "duration_ms": 1
}
```

同一 `invocation_id` 与逐字节相同正文在窗口内返回缓存响应且不重复执行；同一 ID 不同正文返回 `409 invalid_request`。缓存窗口 300 秒、容量由 `REPLAY_CACHE_SIZE` 限制；重启清空缓存，但 30 秒时间窗仍限制重放。

### 响应签名（`http_hmac_v2`）

`http_hmac_v2` 保留冻结的 D06 1.0 请求信封，并增加响应签名。请求带
`X-DW-Response-Signature-Version: v2` 时，Provider 在验签后对所有响应（成功或
错误）增加：

```text
X-DW-Response-Signature-Version: v2
X-DW-Response-Signature: <小写十六进制>
X-DW-Key-Id: <用于校验请求的 key id>
```

签名为：

```text
HMAC-SHA256(secret, "v2\n" + status + "\n" + request_timestamp + "\n" +
                    invocation_id + "\n" + SHA-256(response_body) hex)
```

`request_timestamp` 是 Broker 发送的精确 `X-DW-Timestamp`。签名把响应绑定到唯一
请求与 Invocation：跨 Invocation、状态、key id 或正文被修改的重放都无法通过。
Broker 将未签名、被篡改或不匹配的 v2 响应视为 `capability_unknown`，绝不存储或
返回其输出。`REQUIRE_RESPONSE_SIGNING=true`（默认）时，未协商 v2 的请求被拒绝。

公开的 `contract_version` 仍为 `1.0`；`http_hmac_v2` 是可加的传输配置，不改变
D06 请求契约。旧 `http_hmac` 仅用于内部兼容且不签名响应；公网 Provider 必须使用
`http_hmac_v2`。

## 安全边界

- **无授权权。** Provider 没有对象成员数据，不能授予或拒绝访问；Broker 先授权再发送。
- **无数据库。** 无 DSN、无驱动，镜像不含 `psql`。
- **无用户秘密。** 只发送 invocation id、能力 id、契约版本与已校验输入。
- **无任意目标。** URL 只来自配置，请求参数不能影响它。即使 `network: internal`，也拒绝 loopback、link-local/元数据、CGNAT、组播与保留地址；私有网段仅 internal 允许。
- **无重定向与代理。** Broker 禁止重定向与环境代理，只连接已校验 IP。
- **输出有界。** 最大 256 KiB；Broker 在落库或返回前执行冻结输出 Schema 与能力语义校验。
- **不调用模型。** `content.structure` 是确定性文本分类器。

`http_hmac_v2` 对外部响应做 HMAC 签名，并绑定请求时间戳、Invocation、状态与正文哈希。旧 `http_hmac` 响应不签名，仅保留用于内部兼容，不得用于公网 Provider。HTTPS 的证书与主机名校验始终开启，Broker 不提供关闭校验的选项。

## TLS 与双向 TLS

Broker 校验每个 HTTPS Provider 的证书与主机名（最低 TLS 1.2），不提供关闭校验的选项。内部 Provider 使用私有 CA 时，将 `tls_ca_file` 设为 CA PEM；相对路径相对 `capabilities.yaml` 解析。`tls_server_name` 可为私有证书名覆盖 SNI；对公网 Provider，除非与配置主机名相同，否则拒绝。同时设置 `tls_client_cert_file` 与 `tls_client_key_file` 可向 Broker 提供客户端证书。

参考 Provider 在设置 `TLS_CERT_FILE` 与 `TLS_KEY_FILE` 时提供 TLS，并在 `TLS_REQUIRE_CLIENT_CERT=true`（配合 `TLS_CLIENT_CA_FILE`）时要求已验证的客户端证书。默认 Compose 文件为本地参考演示保留内部明文网络；面向公网的部署必须使用 HTTPS 与 `http_hmac_v2`。测试覆盖受信任私有 CA、不受信任 CA、错误服务器名、过期证书，以及双向 TLS 成功/失败。

## 测试 Provider

```bash
cd examples/capability-provider
go test ./... -count=1
```

测试覆盖确定性分类、8000 字符切分、500 项截断、HMAC 成功与失败、时间戳偏差、头不匹配、重放缓存与冲突、超大正文与不支持的格式。

## 密钥轮换

轮换使用有界双密钥重叠。双方都收到新的 `CAPABILITY_HMAC_SECRET` 与新
`CAPABILITY_HMAC_KEY_ID`；Provider 另外收到旧密钥
`CAPABILITY_HMAC_PREVIOUS_SECRET`、`CAPABILITY_HMAC_PREVIOUS_KEY_ID` 与 RFC3339
`CAPABILITY_HMAC_PREVIOUS_UNTIL` 截止时间。

1. 双方加入新密钥；旧密钥只保留为 Provider 的 previous key，并设置较短截止时间
   （例如 30 分钟）。
2. 重启 Broker 与 Provider。Broker 用新 key id 签名；Provider 接受新密钥，并在
   截止前接受旧密钥。
3. 截止后旧密钥按请求失效，无需重启。若重启时截止时间已过，或旧密钥与当前密钥
   相同，启动按失败关闭。
4. 移除旧密钥变量并重启，完成轮换。

旧密钥绝不用于签名新响应；响应使用校验请求的那把密钥签名，因此尚未完成轮换的
旧 Broker 在重叠窗口内仍能校验响应。
