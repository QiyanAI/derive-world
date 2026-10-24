# Independent capability Provider

The reference Provider executes `content.structure` in a separate process. It is
a stdlib-only Go module (`examples/capability-provider`) that speaks the frozen
D06 HTTP+HMAC protocol. It never imports the Derive World main module, never
connects to PostgreSQL, and never receives a user cookie, PAT, CSRF token,
private AI context, or database credential.

## Topology

```text
browser / SDK
     |
     v
Derive World app (Broker)  --HMAC-->  capability-provider:8081
     |                                    (capnet, internal)
     +-- PostgreSQL (data, internal)
```

In the reference Compose stack the Provider joins only `capnet`, an
`internal: true` network. It publishes no host port. The app joins `capnet`,
`data`, and `edge`; the database joins only `data`.

## Run the reference stack

The Provider is built and started by the default Compose file. No separate
command is required. After the stack is healthy:

```bash
docker compose ps capability-provider
```

The `PORTS` column must be empty. The app reaches the Provider at
`http://capability-provider:8081`, which is the `base_url` in
`capabilities.yaml`.

Useful isolation checks:

```bash
docker compose exec capability-provider env
docker compose exec capability-provider sh -c 'wget -T 2 -qO- http://example.com'
```

The first command must not contain `DATABASE_URL`. The second must fail: the
Provider is on an internal network with no internet egress.

## Run the Provider outside Compose

Build the independent module:

```bash
cd examples/capability-provider
go build -o capability-provider .
```

Start it on a host reachable only from the Derive World app:

```bash
CAPABILITY_HMAC_SECRET="$(openssl rand -base64 32)" \
LISTEN_ADDR="10.0.0.12:8081" \
./capability-provider
```

Environment variables:

| Variable | Required | Default | Rule |
|---|---|---|---|
| `CAPABILITY_HMAC_SECRET` | yes | — | At least 32 bytes; identical to the Broker's value. |
| `CAPABILITY_HMAC_KEY_ID` | no | `k1` | Lowercase key identifier, 1..16 characters; identical to the Broker's value. |
| `CAPABILITY_HMAC_PREVIOUS_SECRET` | no | — | Rotation overlap key, at least 32 bytes and different from the current secret. |
| `CAPABILITY_HMAC_PREVIOUS_KEY_ID` | with previous secret | — | Must differ from the current key id. |
| `CAPABILITY_HMAC_PREVIOUS_UNTIL` | with previous secret | — | RFC3339 deadline in the future at startup. The previous key is rejected after it. |
| `REQUIRE_RESPONSE_SIGNING` | no | `true` | Refuse requests that do not negotiate the v2 signed-response profile. |
| `TLS_CERT_FILE`, `TLS_KEY_FILE` | no | — | Serve HTTPS when both are set; the pair is validated at startup. |
| `TLS_CLIENT_CA_FILE` | with client certs | — | CA used to verify client certificates. |
| `TLS_REQUIRE_CLIENT_CERT` | no | `false` | Require and verify a mutual-TLS client certificate. |
| `LISTEN_ADDR` | no | `:8081` | Listen address inside the private network. |
| `PROVIDER_ID` | no | `reference.content-structure` | Any other value is a startup error. |
| `CONTRACT_VERSION` | no | `1.0` | Any other value is a startup error. |
| `MAX_BODY_BYTES` | no | `65536` | Business input cap, 1..65536. The HTTP body cap is `MAX_BODY_BYTES + 126` for the exact D06 envelope. |
| `REPLAY_CACHE_SIZE` | no | `4096` | Positive LRU capacity. |

Point the Broker at it by editing `capabilities.yaml` (or the file named by
`CAPABILITIES_CONFIG`) and restarting the app:

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
    # Optional, https only. Relative paths resolve beside this file.
    tls_ca_file: provider-ca.pem
    tls_server_name: provider.internal
    tls_client_cert_file: broker-client.pem
    tls_client_key_file: broker-client-key.pem
```

`base_url` must use `scheme://host[:port]` only. The host must appear verbatim
in `allowed_hosts`. `network: internal` permits private addresses and `http`;
`network: public` requires `https` and a public address.

## Protocol

The Broker sends exactly one request per invocation:

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

{"invocation_id":"018f3f2f-8b7d-7a58-9f11-2f5f5f7a9b21","capability_id":"content.structure","contract_version":"1.0","input":{"object_id":"018f3f2f-8b7d-7a58-9f11-2f5f5f7a9b21","text":"# Title\n\nBody","format":"markdown"}}
```

The signature is lower-case hex `HMAC-SHA256(secret, timestamp + "." + raw
body)`. For `content.structure` the fixed envelope adds exactly 126 bytes around
the input (fixed keys/values plus one UUIDv7). `MAX_BODY_BYTES` bounds the
business input; the HTTP body cap is `MAX_BODY_BYTES + 126`, so a valid
65,536-byte input is deliverable. The Provider rejects an input over
`MAX_BODY_BYTES` with 413 and never reduces the promised input capacity. The Provider verifies the timestamp (±30 seconds), the provider,
capability, contract, and invocation headers, and the signature before parsing
JSON. It returns:

```json
{
  "invocation_id": "018f3f2f-8b7d-7a58-9f11-2f5f5f7a9b21",
  "capability_id": "content.structure",
  "contract_version": "1.0",
  "status": "succeeded",
  "output": {
    "items": [
      {"index": 0, "kind": "heading", "text": "Title", "level": 1},
      {"index": 1, "kind": "paragraph", "text": "Body"}
    ],
    "item_count": 2,
    "truncated": false,
    "format": "markdown"
  },
  "duration_ms": 1
}
```

A replay with the same `invocation_id` and byte-identical body returns the
cached response without executing again. The same id with a different body
returns `409 invalid_request`. The cache holds 300 seconds and is bounded by
`REPLAY_CACHE_SIZE`; a restart clears it, while the 30-second timestamp window
still limits replay.

### Signed responses (`http_hmac_v2`)

`http_hmac_v2` keeps the frozen D06 1.0 request envelope and adds a response
signature. For a request that sent
`X-DW-Response-Signature-Version: v2`, every Provider response (success or
error after signature verification) carries:

```text
X-DW-Response-Signature-Version: v2
X-DW-Response-Signature: <lower-case hex>
X-DW-Key-Id: <the key id used to verify the request>
```

The value is:

```text
HMAC-SHA256(secret, "v2\n" + status + "\n" + request_timestamp + "\n" +
                    invocation_id + "\n" + SHA-256(response_body) hex)
```

`request_timestamp` is the exact `X-DW-Timestamp` value the Broker sent. This
binds the response to one request and one invocation: a response replayed for a
different invocation, with a different status, key id, or body fails
verification. The Broker treats any unsigned, tampered, or mismatched v2
response as `capability_unknown`; it never stores or returns the output. A
`Provider` configured with `REQUIRE_RESPONSE_SIGNING=true` (the default) rejects
a request that does not ask for v2.

The public `contract_version` remains `1.0`; `http_hmac_v2` is an additive
transport profile, not a change to the D06 request contract. The legacy
`http_hmac` transport remains available only for internal compatibility and
does not sign responses; public Providers must use `http_hmac_v2`.

## Security boundaries

- **No authorization.** The Provider has no object membership data and cannot
  grant or deny access. The Broker authorizes before sending.
- **No database.** No DSN, no driver, no `psql` in the image.
- **No user secrets.** Only the invocation id, capability id, contract version,
  and validated input are sent.
- **No arbitrary destination.** The URL comes only from configuration. Request
  parameters cannot influence it. Loopback, link-local/metadata, CGNAT,
  multicast, and reserved addresses are rejected even for internal providers;
  private ranges are allowed only for `network: internal`.
- **No redirects or proxies.** The Broker disables redirects and environment
  proxies and dials the validated IP.
- **Bounded output.** 256 KiB maximum, validated by the Broker against the
  frozen output schema and capability semantics before it is stored or
  returned.
- **No model calls.** `content.structure` is a deterministic text classifier.

The `http_hmac_v2` response is HMAC-signed and bound to the request timestamp,
invocation id, status, and body hash. The legacy `http_hmac` response is not
signed; it is retained only for internal compatibility and must not be used for
a public Provider. TLS certificate and hostname verification are always enabled
for HTTPS; the Broker never disables verification.

## TLS and mutual TLS

The Broker verifies every HTTPS Provider certificate and hostname (TLS 1.2
minimum) and never offers an insecure-skip option. For an internal Provider
with a private CA, set `tls_ca_file` to the CA PEM; relative paths resolve
beside `capabilities.yaml`. `tls_server_name` overrides SNI for a private
certificate name and is rejected for a public Provider unless it equals the
configured host. Set `tls_client_cert_file` and `tls_client_key_file` together
to present a Broker client certificate.

The reference Provider serves TLS when `TLS_CERT_FILE` and `TLS_KEY_FILE` are
set, and requires a verified client certificate when
`TLS_REQUIRE_CLIENT_CERT=true` (with `TLS_CLIENT_CA_FILE`). The default Compose
file keeps the internal network plaintext for the local reference demo; a
public-facing deployment must use HTTPS and `http_hmac_v2`. Tests cover a
trusted private CA, an untrusted CA, a wrong server name, an expired
certificate, and mutual TLS success/failure.

## Test the Provider

```bash
cd examples/capability-provider
go test ./... -count=1
```

The tests cover deterministic classification, 8000-rune splitting, 500-item
truncation, HMAC success and failures, timestamp skew, header mismatch, replay
caching and conflict, oversized bodies, and unsupported formats.

## Key rotation

Rotation uses a bounded dual-key overlap. Both sides receive the new key as
`CAPABILITY_HMAC_SECRET` with a new `CAPABILITY_HMAC_KEY_ID`; the Provider also
receives the old key as `CAPABILITY_HMAC_PREVIOUS_SECRET` with
`CAPABILITY_HMAC_PREVIOUS_KEY_ID` and an RFC3339
`CAPABILITY_HMAC_PREVIOUS_UNTIL` deadline.

1. Add the new key to both sides; keep the old key only as the Provider's
   previous key with a short deadline (for example, 30 minutes).
2. Restart the Broker and Provider. The Broker signs with the new key id; the
   Provider accepts the new key and, until the deadline, the previous key.
3. After the deadline the previous key is rejected per request without a
   restart. A restart with an expired deadline or a previous secret equal to
   the current secret fails closed.
4. Remove the previous-key variables and restart to complete the rotation.

The old key is never used to sign a new response. Responses are signed with the
key that verified the request, so an old Broker that has not rotated still
receives a signature it can verify during the overlap.
