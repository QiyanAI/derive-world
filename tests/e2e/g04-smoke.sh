#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# G04 Compose E2E: real Broker, real independent Provider, real HTTP, real
# PostgreSQL. This script does not modify the database directly to perform the
# developer flow; SQL is used only to verify audit evidence.
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
ORIGIN="${ORIGIN:-$BASE_URL}"
RESTART_APP="${RESTART_APP:-1}"
PYTHON="${PYTHON:-python3}"
if ! command -v "$PYTHON" >/dev/null 2>&1; then
  PYTHON=python
fi
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
OUT="${G04_OUT:-$WORKDIR}"
mkdir -p "$OUT"
JAR="$WORKDIR/a.jar"
JAR_B="$WORKDIR/b.jar"

json_value() {
  "$PYTHON" - "$1" "$2" <<'PY'
import json
import sys
path = sys.argv[2].split(".")
with open(sys.argv[1], encoding="utf-8") as handle:
    value = json.load(handle)
for part in path:
    value = value[int(part)] if isinstance(value, list) else value[part]
print(value)
PY
}

idempotency_key() {
  "$PYTHON" -c 'import uuid;print(uuid.uuid4().hex + uuid.uuid4().hex[:16])'
}

csrf_from_jar() {
  awk '$6 == "dw_csrf" { value = $7 } END { print value }' "$1"
}

wait_ready() {
  for _ in $(seq 1 90); do
    if curl -fsS "$BASE_URL/readyz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "service did not become ready" >&2
  return 1
}

require_status() {
  local expected="$1" actual="$2" body_file="$3" label="$4"
  if [ "$expected" != "$actual" ]; then
    echo "$label: status $actual, want $expected" >&2
    cat "$body_file" >&2 || true
    exit 1
  fi
}

invoke_capability() {
  local capability="$1" body="$2" key="$3" output="$4"
  curl -sS -o "$output" -w '%{http_code}' \
    -H "Authorization: Bearer $PAT" \
    -H 'Content-Type: application/json' \
    -H "Idempotency-Key: $key" \
    -d "$body" \
    "$BASE_URL/api/v1/capabilities/$capability/invoke"
}

wait_ready

SUFFIX="$(date +%s)$$"
EMAIL="g04.${SUFFIX}@example.test"
HANDLE="g4${SUFFIX: -9}"
PASSWORD="correct horse battery staple"

curl -fsS -c "$JAR" -o "$WORKDIR/register.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" \
  -d "{\"email\":\"$EMAIL\",\"handle\":\"$HANDLE\",\"display_name\":\"G04 E2E\",\"password\":\"$PASSWORD\"}" \
  "$BASE_URL/api/v1/auth/register"
CSRF="$(csrf_from_jar "$JAR")"
USER_ID="$(json_value "$WORKDIR/register.json" user.id)"

curl -fsS -b "$JAR" -c "$JAR" -o "$WORKDIR/object.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
  -H "Idempotency-Key: $(idempotency_key)" \
  -d '{"object_type":"collaborative","title":"G04 Compose object","content":{"summary":"compose summary","body":"compose body"},"shared_context":{"note":"shared"}}' \
  "$BASE_URL/api/v1/objects"
OBJECT_ID="$(json_value "$WORKDIR/object.json" id)"
OBJECT_VERSION="$(json_value "$WORKDIR/object.json" version)"

curl -fsS -b "$JAR" -c "$JAR" -o "$WORKDIR/token.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
  -d '{"name":"g04 e2e","scopes":["capabilities:invoke","objects:read","objects:write","derive:create"],"expires_in_days":1}' \
  "$BASE_URL/api/v1/tokens"
PAT="$(json_value "$WORKDIR/token.json" plaintext)"

curl -fsS -H "Authorization: Bearer $PAT" -o "$WORKDIR/capabilities.json" \
  "$BASE_URL/api/v1/capabilities"
"$PYTHON" - "$WORKDIR/capabilities.json" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as handle:
    text = handle.read()
payload = json.loads(text)
if len(payload["items"]) != 5:
    raise SystemExit("capability list does not contain five descriptors")
if "base_url" in text or "secret" in text:
    raise SystemExit("capability list leaked provider configuration")
PY

STATUS="$(invoke_capability object.read \
  "{\"object_id\":\"$OBJECT_ID\"}" "$(idempotency_key)" "$WORKDIR/read.json")"
require_status 200 "$STATUS" "$WORKDIR/read.json" "object.read"
[ "$(json_value "$WORKDIR/read.json" output.object.id)" = "$OBJECT_ID" ] || {
  echo "object.read returned a different object" >&2; exit 1;
}

STATUS="$(invoke_capability object.context \
  "{\"object_id\":\"$OBJECT_ID\"}" "$(idempotency_key)" "$WORKDIR/context.json")"
require_status 200 "$STATUS" "$WORKDIR/context.json" "object.context"
[ "$(json_value "$WORKDIR/context.json" output.object_id)" = "$OBJECT_ID" ] || {
  echo "object.context returned a different object" >&2; exit 1;
}

STATUS="$(invoke_capability object.propose_change \
  "{\"object_id\":\"$OBJECT_ID\",\"action\":\"set_summary\",\"input\":{\"summary\":\"via capability\"},\"expected_version\":$OBJECT_VERSION}" \
  "$(idempotency_key)" "$WORKDIR/propose.json")"
require_status 200 "$STATUS" "$WORKDIR/propose.json" "object.propose_change"
PROPOSAL_ID="$(json_value "$WORKDIR/propose.json" output.proposal_id)"
[ -n "$PROPOSAL_ID" ] || { echo "missing proposal id" >&2; exit 1; }

STATUS="$(invoke_capability object.derive \
  "{\"object_id\":\"$OBJECT_ID\",\"title\":\"G04 derived child\",\"inheritance_fields\":[\"summary\"]}" \
  "$(idempotency_key)" "$WORKDIR/derive.json")"
require_status 200 "$STATUS" "$WORKDIR/derive.json" "object.derive"
CHILD_ID="$(json_value "$WORKDIR/derive.json" output.child_object_id)"
curl -fsS -H "Authorization: Bearer $PAT" -o "$WORKDIR/child.json" \
  "$BASE_URL/api/v1/objects/$CHILD_ID"
[ "$(json_value "$WORKDIR/child.json" version)" = "1" ] || {
  echo "derived child version is not 1" >&2; exit 1;
}
curl -fsS -H "Authorization: Bearer $PAT" -o "$WORKDIR/child-events.json"   "$BASE_URL/api/v1/objects/$CHILD_ID/events"
"$PYTHON" - "$WORKDIR/child-events.json" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as handle:
    payload = json.load(handle)
if not payload["items"] or payload["items"][0]["event_type"] != "object.derived":
    raise SystemExit("derived child event is missing")
PY

CONTENT_BODY="{\"object_id\":\"$OBJECT_ID\",\"text\":\"# Provider E2E\\n\\nA paragraph.\",\"format\":\"markdown\"}"
CONTENT_KEY="$(idempotency_key)"
STATUS="$(invoke_capability content.structure "$CONTENT_BODY" "$CONTENT_KEY" "$WORKDIR/content.json")"
require_status 200 "$STATUS" "$WORKDIR/content.json" "content.structure"
CONTENT_INVOCATION="$(json_value "$WORKDIR/content.json" id)"
[ "$(json_value "$WORKDIR/content.json" output.item_count)" = "2" ] || {
  echo "content.structure item_count mismatch" >&2; exit 1;
}

curl -fsS -H "Authorization: Bearer $PAT" -o "$WORKDIR/invocation.json" \
  "$BASE_URL/api/v1/capability-invocations/$CONTENT_INVOCATION"
[ "$(json_value "$WORKDIR/invocation.json" status)" = "succeeded" ] || {
  echo "invocation query did not report success" >&2; exit 1;
}
[ "$(json_value "$WORKDIR/invocation.json" provider_id)" = "reference.content-structure" ] || {
  echo "invocation was not executed by the external provider" >&2; exit 1;
}

HEADERS="$WORKDIR/replay.headers"
STATUS="$(curl -sS -D "$HEADERS" -o "$WORKDIR/replay.json" -w '%{http_code}' \
  -H "Authorization: Bearer $PAT" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $CONTENT_KEY" -d "$CONTENT_BODY" \
  "$BASE_URL/api/v1/capabilities/content.structure/invoke")"
require_status 200 "$STATUS" "$WORKDIR/replay.json" "content.structure replay"
grep -qi '^Idempotency-Replayed: true' "$HEADERS" || {
  echo "replay header missing" >&2; exit 1;
}

# A second user without membership cannot read the object.
SUFFIX_B="$(date +%s)$$b"
curl -fsS -c "$JAR_B" -o "$WORKDIR/register-b.json"   -H 'Content-Type: application/json' -H "Origin: $ORIGIN"   -d "{\"email\":\"g04.b.${SUFFIX_B}@example.test\",\"handle\":\"g4b${SUFFIX_B: -9}\",\"display_name\":\"G04 B\",\"password\":\"$PASSWORD\"}"   "$BASE_URL/api/v1/auth/register"
CSRF_B="$(csrf_from_jar "$JAR_B")"
STATUS="$(curl -sS -b "$JAR_B" -o "$WORKDIR/cross-user.json" -w '%{http_code}'   -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_B"   -H "Idempotency-Key: $(idempotency_key)"   -d "{\"object_id\":\"$OBJECT_ID\"}"   "$BASE_URL/api/v1/capabilities/object.read/invoke")"
require_status 404 "$STATUS" "$WORKDIR/cross-user.json" "cross-user capability read"

# A PAT without capabilities:invoke is rejected at the route boundary.
curl -fsS -b "$JAR" -c "$JAR" -o "$WORKDIR/readonly-token.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
  -d '{"name":"read only","scopes":["objects:read"],"expires_in_days":1}' \
  "$BASE_URL/api/v1/tokens"
READONLY_PAT="$(json_value "$WORKDIR/readonly-token.json" plaintext)"
STATUS="$(curl -sS -o "$WORKDIR/denied.json" -w '%{http_code}' \
  -H "Authorization: Bearer $READONLY_PAT" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(idempotency_key)" \
  -d "{\"object_id\":\"$OBJECT_ID\"}" \
  "$BASE_URL/api/v1/capabilities/object.read/invoke")"
require_status 403 "$STATUS" "$WORKDIR/denied.json" "PAT without capabilities scope"

# Remove the operation scope from a second PAT; the Broker must deny it.
curl -fsS -b "$JAR" -c "$JAR" -o "$WORKDIR/caps-token.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
  -d '{"name":"caps only","scopes":["capabilities:invoke"],"expires_in_days":1}' \
  "$BASE_URL/api/v1/tokens"
CAPS_PAT="$(json_value "$WORKDIR/caps-token.json" plaintext)"
STATUS="$(curl -sS -o "$WORKDIR/scope-denied.json" -w '%{http_code}' \
  -H "Authorization: Bearer $CAPS_PAT" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(idempotency_key)" \
  -d "{\"object_id\":\"$OBJECT_ID\"}" \
  "$BASE_URL/api/v1/capabilities/object.read/invoke")"
require_status 403 "$STATUS" "$WORKDIR/scope-denied.json" "PAT without operation scope"

# Provider isolation: no published port, no database DSN, no internet egress.
PROVIDER_CONTAINER="$(docker compose ps -q capability-provider)"
[ -n "$PROVIDER_CONTAINER" ] || { echo "provider container is missing" >&2; exit 1; }
BINDINGS="$(docker inspect "$PROVIDER_CONTAINER" --format '{{json .HostConfig.PortBindings}}')"
[ "$BINDINGS" = "{}" ] || { echo "provider publishes host ports: $BINDINGS" >&2; exit 1; }
if docker port "$PROVIDER_CONTAINER" 2>/dev/null | grep -q .; then
  echo "provider publishes a host port" >&2; exit 1;
fi
if docker compose exec -T capability-provider env | grep -q '^DATABASE_URL='; then
  echo "provider received DATABASE_URL" >&2; exit 1;
fi
if docker compose exec -T capability-provider wget -T 2 -qO- http://example.com >/dev/null 2>&1; then
  echo "provider has internet egress" >&2; exit 1;
fi

# Direct Provider protocol E2E over the internal network.
SECRET="$(awk -F= '$1 == "CAPABILITY_HMAC_SECRET" { print substr($0, index($0, "=") + 1) }' .env)"
DIRECT_INVOCATION="$("$PYTHON" -c 'import os,time,uuid;ms=int(time.time()*1000);b=bytearray(os.urandom(16));b[0:6]=ms.to_bytes(6,"big");b[6]=(b[6]&15)|112;b[8]=(b[8]&63)|128;print(uuid.UUID(bytes=bytes(b)))')"
DIRECT_BODY="{\"invocation_id\":\"$DIRECT_INVOCATION\",\"capability_id\":\"content.structure\",\"contract_version\":\"1.0\",\"input\":{\"object_id\":\"$OBJECT_ID\",\"text\":\"# Direct\\n\\nBody\",\"format\":\"markdown\"}}"
TIMESTAMP="$(date +%s)"
SIGNATURE="$("$PYTHON" - "$SECRET" "$TIMESTAMP" "$DIRECT_BODY" <<'PY'
import hashlib
import hmac
import sys
secret, timestamp, body = sys.argv[1], sys.argv[2], sys.argv[3]
print(hmac.new(secret.encode(), (timestamp + "." + body).encode(), hashlib.sha256).hexdigest())
PY
)"

direct_header() {
  printf '%s' "$1"
}

docker compose exec -T app wget -qO- \
  --header="$(direct_header 'Content-Type: application/json')" \
  --header="$(direct_header "X-DW-Provider-Id: reference.content-structure")" \
  --header="$(direct_header "X-DW-Capability-Id: content.structure")" \
  --header="$(direct_header 'X-DW-Contract-Version: 1.0')" \
  --header="$(direct_header 'X-DW-Response-Signature-Version: v2')" \
  --header="$(direct_header "X-DW-Invocation-Id: $DIRECT_INVOCATION")" \
  --header="$(direct_header "X-DW-Timestamp: $TIMESTAMP")" \
  --header="$(direct_header "X-DW-Signature: $SIGNATURE")" \
  --post-data="$DIRECT_BODY" \
  "http://capability-provider:8081/v1/invoke" > "$WORKDIR/direct.json"
[ "$(json_value "$WORKDIR/direct.json" status)" = "succeeded" ] || {
  echo "direct provider invocation failed" >&2; exit 1;
}

if docker compose exec -T app wget -qO- \
  --header="$(direct_header 'Content-Type: application/json')" \
  --header="$(direct_header "X-DW-Provider-Id: reference.content-structure")" \
  --header="$(direct_header "X-DW-Capability-Id: content.structure")" \
  --header="$(direct_header 'X-DW-Contract-Version: 1.0')" \
  --header="$(direct_header 'X-DW-Response-Signature-Version: v2')" \
  --header="$(direct_header "X-DW-Invocation-Id: $DIRECT_INVOCATION")" \
  --header="$(direct_header "X-DW-Timestamp: $TIMESTAMP")" \
  --header="$(direct_header "X-DW-Signature: $(printf '0%.0s' $(seq 1 64))")" \
  --post-data="$DIRECT_BODY" \
  "http://capability-provider:8081/v1/invoke" >/dev/null 2>&1; then
  echo "provider accepted an invalid signature" >&2; exit 1;
fi

# Replay with the same id and identical body returns the cached success.
docker compose exec -T app wget -qO- \
  --header="$(direct_header 'Content-Type: application/json')" \
  --header="$(direct_header "X-DW-Provider-Id: reference.content-structure")" \
  --header="$(direct_header "X-DW-Capability-Id: content.structure")" \
  --header="$(direct_header 'X-DW-Contract-Version: 1.0')" \
  --header="$(direct_header 'X-DW-Response-Signature-Version: v2')" \
  --header="$(direct_header "X-DW-Invocation-Id: $DIRECT_INVOCATION")" \
  --header="$(direct_header "X-DW-Timestamp: $TIMESTAMP")" \
  --header="$(direct_header "X-DW-Signature: $SIGNATURE")" \
  --post-data="$DIRECT_BODY" \
  "http://capability-provider:8081/v1/invoke" > "$WORKDIR/direct-replay.json"
[ "$(json_value "$WORKDIR/direct-replay.json" status)" = "succeeded" ] || {
  echo "provider replay was not cached" >&2; exit 1;
}

if [ "${RUN_SDK:-0}" = "1" ]; then
  # Use an independent developer identity: the SDK example is a separate
  # burst from the E2E caller and must fit the frozen 10-call burst.
  JAR_SDK="$WORKDIR/sdk.jar"
  SDK_SUFFIX="${SUFFIX}sdk"
  SDK_HANDLE="g4s${SUFFIX: -8}"
  curl -fsS -c "$JAR_SDK" -o "$WORKDIR/sdk-register.json"     -H 'Content-Type: application/json' -H "Origin: $ORIGIN"     -d "{\"email\":\"g04.sdk.${SDK_SUFFIX}@example.test\",\"handle\":\"$SDK_HANDLE\",\"display_name\":\"G04 SDK\",\"password\":\"$PASSWORD\"}"     "$BASE_URL/api/v1/auth/register"
  CSRF_SDK="$(csrf_from_jar "$JAR_SDK")"
  curl -fsS -b "$JAR_SDK" -c "$JAR_SDK" -o "$WORKDIR/sdk-object.json"     -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_SDK"     -H "Idempotency-Key: $(idempotency_key)"     -d '{"object_type":"collaborative","title":"SDK object","content":{"summary":"sdk summary","body":"sdk body"},"shared_context":{"note":"sdk"}}'     "$BASE_URL/api/v1/objects"
  SDK_OBJECT_ID="$(json_value "$WORKDIR/sdk-object.json" id)"
  curl -fsS -b "$JAR_SDK" -c "$JAR_SDK" -o "$WORKDIR/sdk-token.json"     -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_SDK"     -d '{"name":"sdk example","scopes":["capabilities:invoke","objects:read","objects:write","derive:create"],"expires_in_days":1}'     "$BASE_URL/api/v1/tokens"
  SDK_PAT="$(json_value "$WORKDIR/sdk-token.json" plaintext)"
  DW_PAT="$SDK_PAT" DW_OBJECT_ID="$SDK_OBJECT_ID" DW_BASE_URL="$BASE_URL"     node examples/sdk-node/example.mjs > "$WORKDIR/sdk.log" 2>&1 || {
      cat "$WORKDIR/sdk.log" >&2
      echo "SDK example failed" >&2
      exit 1
    }
  grep -q 'object.read' "$WORKDIR/sdk.log" || { echo "SDK read output missing" >&2; exit 1; }
  grep -q 'content.structure' "$WORKDIR/sdk.log" || { echo "SDK provider output missing" >&2; exit 1; }
  grep -q 'capability invocation' "$WORKDIR/sdk.log" || { echo "SDK invocation lookup missing" >&2; exit 1; }
fi

# Real Provider large-input E2E: exact 64 KiB business input and +1 byte.
LARGE_INPUT="$WORKDIR/large-input.json"
LARGE_BODY="$WORKDIR/large-body.json"
LARGE_OVER_BODY="$WORKDIR/large-over-body.json"
"$PYTHON" - "$LARGE_INPUT" "$LARGE_BODY" "$LARGE_OVER_BODY" "$OBJECT_ID" <<'PY'
import json, sys
input_path, body_path, over_path, object_id = sys.argv[1:5]
prefix = '{"object_id":"%s","text":"' % object_id
suffix = '","format":"markdown"}'
def build(target):
    slot = target - len(prefix.encode()) - len(suffix.encode())
    base = "汉"
    per = len(base.encode())
    count = slot // per
    pad = slot - count * per
    text = base * count + "a" * pad
    text_json = json.dumps(text, ensure_ascii=False)[1:-1]
    payload = prefix + text_json + suffix
    assert len(payload.encode()) == target, (len(payload.encode()), target)
    return payload
exact = build(65536)
over = build(65537)
open(input_path, "w", encoding="utf-8").write(exact)
prefix_env = '{"invocation_id":"' + "0192f0c4-5a6b-7c8d-9e0f-123456789abc" + '","capability_id":"content.structure","contract_version":"1.0","input":'
over_prefix_env = '{"invocation_id":"' + "0192f0c4-5a6b-7c8d-9e0f-123456789abd" + '","capability_id":"content.structure","contract_version":"1.0","input":'
open(body_path, "w", encoding="utf-8").write(prefix_env + exact + "}")
open(over_path, "w", encoding="utf-8").write(over_prefix_env + over + "}")
PY
LARGE_INVOCATION="0192f0c4-5a6b-7c8d-9e0f-123456789abc"
LARGE_TIMESTAMP="$(date +%s)"
LARGE_SIG="$("$PYTHON" - "$SECRET" "$LARGE_TIMESTAMP" "$LARGE_BODY" <<'PY'
import hashlib, hmac, sys
body = open(sys.argv[3], "rb").read()
print(hmac.new(sys.argv[1].encode(), (sys.argv[2].encode() + b"." + body), hashlib.sha256).hexdigest())
PY
)"
large_wget() {
  local body_file="$1" signature="$2" invocation="$3" output="$4"
  MSYS_NO_PATHCONV=1 docker compose exec -T app sh -c 'cat > /tmp/dw-large-body.json' < "$body_file"
  MSYS_NO_PATHCONV=1 docker compose exec -T app wget -qO- \
    --header="Content-Type: application/json" \
    --header="X-DW-Provider-Id: reference.content-structure" \
    --header="X-DW-Capability-Id: content.structure" \
    --header="X-DW-Contract-Version: 1.0" \
    --header="X-DW-Response-Signature-Version: v2" \
    --header="X-DW-Invocation-Id: $invocation" \
    --header="X-DW-Timestamp: $LARGE_TIMESTAMP" \
    --header="X-DW-Signature: $signature" \
    --post-file=/tmp/dw-large-body.json \
    "http://capability-provider:8081/v1/invoke" > "$output"
}
{ echo "large_input_bytes=$(wc -c < "$LARGE_INPUT")"; \
  echo "large_envelope_bytes=$(wc -c < "$LARGE_BODY")"; \
  echo "large_envelope_overhead=$(( $(wc -c < "$LARGE_BODY") - $(wc -c < "$LARGE_INPUT") ))"; } > "$OUT/provider-large.txt"
large_wget "$LARGE_BODY" "$LARGE_SIG" "$LARGE_INVOCATION" "$OUT/provider-large-response.json"
[ "$(json_value "$OUT/provider-large-response.json" status)" = "succeeded" ] || {
  echo "large provider invocation failed" >&2; exit 1;
}
large_wget "$LARGE_BODY" "$LARGE_SIG" "$LARGE_INVOCATION" "$OUT/provider-large-replay.json"
cmp -s "$OUT/provider-large-response.json" "$OUT/provider-large-replay.json" || {
  echo "large provider replay was not byte-identical" >&2; exit 1;
}
{ echo "large_replay_byte_identical=true"; } >> "$OUT/provider-large.txt"
if large_wget "$LARGE_BODY" "0000000000000000000000000000000000000000000000000000000000000000" "$LARGE_INVOCATION" "$WORKDIR/large-bad.json" 2>/dev/null; then
  echo "large_bad_signature_exit=0" >> "$OUT/provider-large.txt"
else
  echo "large_bad_signature_exit=1" >> "$OUT/provider-large.txt"
fi
OVER_INVOCATION="0192f0c4-5a6b-7c8d-9e0f-123456789abd"
OVER_SIG="$("$PYTHON" - "$SECRET" "$LARGE_TIMESTAMP" "$LARGE_OVER_BODY" <<'PY'
import hashlib, hmac, sys
body = open(sys.argv[3], "rb").read()
print(hmac.new(sys.argv[1].encode(), (sys.argv[2].encode() + b"." + body), hashlib.sha256).hexdigest())
PY
)"
if large_wget "$LARGE_OVER_BODY" "$OVER_SIG" "$OVER_INVOCATION" "$WORKDIR/large-over-response.json" 2>/dev/null; then
  echo "large_over_limit_exit=0" >> "$OUT/provider-large.txt"
else
  echo "large_over_limit_exit=1" >> "$OUT/provider-large.txt"
fi

# Audit verification (evidence only; the flow above used only public APIs).
AUDIT_COUNT="$(docker compose exec -T db psql -U derive -d derive_world -tAc \
  "SELECT count(*) FROM audit_events WHERE action = 'capability.invoke' AND actor_user_id = '$USER_ID'")"
[ "$AUDIT_COUNT" -ge 5 ] || {
  echo "capability.invoke audit count = $AUDIT_COUNT" >&2; exit 1;
}

if [ "$RESTART_APP" = "1" ]; then
  docker compose restart app >/dev/null
  wait_ready
  curl -fsS -H "Authorization: Bearer $PAT" -o "$WORKDIR/after-restart.json" \
    "$BASE_URL/api/v1/objects/$OBJECT_ID"
  [ "$(json_value "$WORKDIR/after-restart.json" version)" = "$OBJECT_VERSION" ] || {
    echo "object version changed across restart" >&2; exit 1;
  }
  curl -fsS -H "Authorization: Bearer $PAT" -o "$WORKDIR/child-after-restart.json" \
    "$BASE_URL/api/v1/objects/$CHILD_ID"
  [ "$(json_value "$WORKDIR/child-after-restart.json" owner_id)" = "$USER_ID" ] || {
    echo "derived child did not persist across restart" >&2; exit 1;
  }
  curl -fsS -H "Authorization: Bearer $PAT" -o "$WORKDIR/invocation-after-restart.json" \
    "$BASE_URL/api/v1/capability-invocations/$CONTENT_INVOCATION"
  [ "$(json_value "$WORKDIR/invocation-after-restart.json" status)" = "succeeded" ] || {
    echo "invocation did not persist across restart" >&2; exit 1;
  }
fi

echo "G04 E2E passed: five capabilities, external provider, replay, PAT scopes, isolation, direct HMAC, audit, restart persistence"
