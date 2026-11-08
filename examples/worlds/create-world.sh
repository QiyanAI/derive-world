#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Create the Derive Relay base world on a local Derive World instance.
# This script registers a local test account, creates the world from
# relay-base.json, and optionally publishes it with derivation allowed.
# It never uses an external account, never posts outside the instance, and
# never reads a secret.
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
ORIGIN="${ORIGIN:-$BASE_URL}"
WORLD_FILE="${WORLD_FILE:-$(cd "$(dirname "$0")" && pwd)/relay-base.json}"
PUBLISH="${PUBLISH:-0}"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
JAR="$WORKDIR/account.jar"
PASSWORD="correct horse battery staple"

if [ ! -f "$WORLD_FILE" ]; then
  echo "world file not found: $WORLD_FILE" >&2
  exit 1
fi

for _ in $(seq 1 60); do
  if curl -fsS "$BASE_URL/readyz" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
curl -fsS "$BASE_URL/readyz" >/dev/null

SUFFIX="$(date +%s)$$"
EMAIL="relay.${SUFFIX}@example.test"
HANDLE="rl${SUFFIX: -10}"

curl -fsS -c "$JAR" -o "$WORKDIR/register.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" \
  -d "{\"email\":\"$EMAIL\",\"handle\":\"$HANDLE\",\"display_name\":\"Relay Builder\",\"password\":\"$PASSWORD\"}" \
  "$BASE_URL/api/v1/auth/register"

CSRF="$(awk '$6 == "dw_csrf" { value = $7 } END { print value }' "$JAR")"
if [ -z "$CSRF" ]; then
  echo "dw_csrf cookie was not set" >&2
  exit 1
fi

KEY="$(python3 -c 'import uuid;print(uuid.uuid4().hex + uuid.uuid4().hex[:16])')"
curl -fsS -b "$JAR" -c "$JAR" -o "$WORKDIR/world.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
  -H "Idempotency-Key: $KEY" --data-binary "@$WORLD_FILE" \
  "$BASE_URL/api/v1/objects"

OBJECT_ID="$(python3 - "$WORKDIR/world.json" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as handle:
    print(json.load(handle)["id"])
PY
)"
echo "created world: $OBJECT_ID"

if [ "$PUBLISH" = "1" ]; then
  curl -fsS -b "$JAR" -c "$JAR" -o "$WORKDIR/publication.json" \
    -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
    -H "Idempotency-Key: $(python3 -c 'import uuid;print(uuid.uuid4().hex + uuid.uuid4().hex[:16])')" \
    -d '{"derive_allowed":true,"license":"cc_by_4.0"}' \
    "$BASE_URL/api/v1/objects/$OBJECT_ID/publications"
  echo "published with derivation allowed (cc_by_4.0)"
  echo "anonymous read: $BASE_URL/api/v1/public/worlds/$OBJECT_ID"
fi

echo "open the instance, sign in as $HANDLE, and derive from this world."
