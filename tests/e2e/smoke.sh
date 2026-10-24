#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
ORIGIN="${ORIGIN:-$BASE_URL}"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
JAR="$WORKDIR/user.jar"
JAR_B="$WORKDIR/other.jar"

csrf_from_jar() {
  awk '$6 == "dw_csrf" { value = $7 } END { print value }' "$1"
}

json_value() {
  python3 - "$1" "$2" <<'PY'
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

wait_ready() {
  for _ in $(seq 1 60); do
    if curl -fsS "$BASE_URL/readyz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "service did not become ready" >&2
  return 1
}

wait_ready

SUFFIX="$(date +%s)$$"
EMAIL="smoke.${SUFFIX}@example.test"
HANDLE="sm${SUFFIX: -10}"
OTHER_EMAIL="smoke.other.${SUFFIX}@example.test"
OTHER_HANDLE="so${SUFFIX: -10}"
PASSWORD="correct horse battery staple"

# 1. Register the first Digital Self.
curl -fsS -c "$JAR" -o "$WORKDIR/register.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" \
  -d "{\"email\":\"$EMAIL\",\"handle\":\"$HANDLE\",\"display_name\":\"Smoke A\",\"password\":\"$PASSWORD\"}" \
  "$BASE_URL/api/v1/auth/register"
CSRF="$(csrf_from_jar "$JAR")"
if [ -z "$CSRF" ]; then
  echo "dw_csrf cookie was not set" >&2
  exit 1
fi
if grep -q '^#HttpOnly_.*[[:space:]]dw_csrf[[:space:]]' "$JAR"; then
  echo "dw_csrf must be readable by the browser" >&2
  exit 1
fi
if ! grep -q '^#HttpOnly_.*[[:space:]]dw_session[[:space:]]' "$JAR"; then
  echo "dw_session must be HttpOnly" >&2
  exit 1
fi
USER_A="$(json_value "$WORKDIR/register.json" user.id)"

# 2. Register a second, independent Digital Self.
curl -fsS -c "$JAR_B" -o "$WORKDIR/register-b.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" \
  -d "{\"email\":\"$OTHER_EMAIL\",\"handle\":\"$OTHER_HANDLE\",\"display_name\":\"Smoke B\",\"password\":\"$PASSWORD\"}" \
  "$BASE_URL/api/v1/auth/register"
USER_B="$(json_value "$WORKDIR/register-b.json" user.id)"
if [ "$USER_A" = "$USER_B" ]; then
  echo "the two registrations returned the same user id" >&2
  exit 1
fi

# 3. Create an object, then replay the exact request with the same key.
KEY="$(python3 -c 'import uuid;print(uuid.uuid4().hex + uuid.uuid4().hex[:16])')"
BODY='{"object_type":"collaborative","title":"E2E smoke object","content":{"summary":"e2e","body":"before restart","checklist":[{"text":"first"}],"tags":["e2e","smoke"]},"shared_context":{"note":"shared"}}'
curl -fsS -b "$JAR" -c "$JAR" -D "$WORKDIR/create.headers" -o "$WORKDIR/object.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
  -H "Idempotency-Key: $KEY" -d "$BODY" "$BASE_URL/api/v1/objects"
OBJECT_ID="$(json_value "$WORKDIR/object.json" id)"
VERSION="$(json_value "$WORKDIR/object.json" version)"
[ "$VERSION" = "1" ] || { echo "new object version = $VERSION" >&2; exit 1; }

curl -fsS -b "$JAR" -c "$JAR" -D "$WORKDIR/replay.headers" -o "$WORKDIR/replay.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
  -H "Idempotency-Key: $KEY" -d "$BODY" "$BASE_URL/api/v1/objects"
REPLAY_ID="$(json_value "$WORKDIR/replay.json" id)"
[ "$REPLAY_ID" = "$OBJECT_ID" ] || { echo "idempotent replay created a second object" >&2; exit 1; }
grep -qi '^Idempotency-Replayed: true' "$WORKDIR/replay.headers" || {
  echo "replay response is missing Idempotency-Replayed" >&2
  exit 1
}

# 4. Update with an explicit expected version.
curl -fsS -b "$JAR" -c "$JAR" -o "$WORKDIR/action.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
  -H "Idempotency-Key: $(python3 -c 'import uuid;print(uuid.uuid4().hex + uuid.uuid4().hex[:16])')" \
  -d '{"action":"set_body","input":{"body":"after restart"},"expected_version":1}' \
  "$BASE_URL/api/v1/objects/$OBJECT_ID/actions"
VERSION="$(json_value "$WORKDIR/action.json" version)"
[ "$VERSION" = "2" ] || { echo "action version = $VERSION" >&2; exit 1; }

# 5. Derive an independent child from the current version.
curl -fsS -b "$JAR" -c "$JAR" -o "$WORKDIR/derive.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
  -H "Idempotency-Key: $(python3 -c 'import uuid;print(uuid.uuid4().hex + uuid.uuid4().hex[:16])')" \
  -d '{"title":"E2E child","inheritance_fields":["summary","body","tags"]}' \
  "$BASE_URL/api/v1/objects/$OBJECT_ID/derive"
CHILD_ID="$(json_value "$WORKDIR/derive.json" id)"
CHILD_VERSION="$(json_value "$WORKDIR/derive.json" version)"
[ "$CHILD_ID" != "$OBJECT_ID" ] || { echo "derive returned the parent id" >&2; exit 1; }
[ "$CHILD_VERSION" = "1" ] || { echo "child version = $CHILD_VERSION" >&2; exit 1; }

# 6. Cross-user access must be indistinguishable from a missing object.
STATUS="$(curl -sS -b "$JAR_B" -o "$WORKDIR/cross.json" -w '%{http_code}' "$BASE_URL/api/v1/objects/$OBJECT_ID")"
[ "$STATUS" = "404" ] || { echo "cross-user read status = $STATUS" >&2; exit 1; }

# 7. Restart the application container and verify persistence + session survival.
docker compose restart app
wait_ready

curl -fsS -b "$JAR" -o "$WORKDIR/reloaded.json" "$BASE_URL/api/v1/objects/$OBJECT_ID"
RELOADED_VERSION="$(json_value "$WORKDIR/reloaded.json" version)"
RELOADED_BODY="$(json_value "$WORKDIR/reloaded.json" state.body)"
[ "$RELOADED_VERSION" = "2" ] || { echo "version after restart = $RELOADED_VERSION" >&2; exit 1; }
[ "$RELOADED_BODY" = "after restart" ] || { echo "body after restart = $RELOADED_BODY" >&2; exit 1; }

curl -fsS -b "$JAR" -o "$WORKDIR/child.json" "$BASE_URL/api/v1/objects/$CHILD_ID"
[ "$(json_value "$WORKDIR/child.json" version)" = "1" ] || { echo "child did not persist" >&2; exit 1; }

curl -fsS -b "$JAR" -o "$WORKDIR/lineage.json" "$BASE_URL/api/v1/objects/$OBJECT_ID/lineage"
python3 - "$WORKDIR/lineage.json" "$CHILD_ID" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as handle:
    lineage = json.load(handle)
children = lineage.get("descendants", [])
if len(children) != 1 or children[0]["object_id"] != sys.argv[2]:
    raise SystemExit("lineage did not retain the derived child")
PY

# 8. Logout/login rotation on the restarted stack.
LOGOUT_STATUS="$(curl -sS -b "$JAR" -c "$JAR" -o /dev/null -w '%{http_code}'   -X POST -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" "$BASE_URL/api/v1/auth/logout")"
[ "$LOGOUT_STATUS" = "204" ] || { echo "logout status = $LOGOUT_STATUS" >&2; exit 1; }
ME_STATUS="$(curl -sS -b "$JAR" -o /dev/null -w '%{http_code}' "$BASE_URL/api/v1/auth/me")"
[ "$ME_STATUS" = "401" ] || { echo "me after logout = $ME_STATUS" >&2; exit 1; }

curl -fsS -b "$JAR" -c "$JAR" -o "$WORKDIR/login.json"   -H 'Content-Type: application/json' -H "Origin: $ORIGIN"   -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"   "$BASE_URL/api/v1/auth/login"
CSRF="$(csrf_from_jar "$JAR")"
[ -n "$CSRF" ] || { echo "login did not rotate dw_csrf" >&2; exit 1; }
curl -fsS -b "$JAR" -o /dev/null "$BASE_URL/api/v1/objects/$OBJECT_ID"

echo "E2E smoke passed: register, isolation, idempotent create, action, derive, restart persistence, logout/login rotation"
