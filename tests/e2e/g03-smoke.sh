#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
ORIGIN="${ORIGIN:-$BASE_URL}"
RESTART_APP="${RESTART_APP:-0}"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
JAR_A="$WORKDIR/a.jar"
JAR_B="$WORKDIR/b.jar"

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

idempotency_key() {
  python3 -c 'import uuid;print(uuid.uuid4().hex + uuid.uuid4().hex[:16])'
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
EMAIL_A="g03.a.${SUFFIX}@example.test"
HANDLE_A="g3a${SUFFIX: -9}"
EMAIL_B="g03.b.${SUFFIX}@example.test"
HANDLE_B="g3b${SUFFIX: -9}"
PASSWORD="correct horse battery staple"

register() {
  local jar="$1" email="$2" handle="$3" name="$4"
  curl -fsS -c "$jar" -o "$WORKDIR/register-$handle.json" \
    -H 'Content-Type: application/json' -H "Origin: $ORIGIN" \
    -d "{\"email\":\"$email\",\"handle\":\"$handle\",\"display_name\":\"$name\",\"password\":\"$PASSWORD\"}" \
    "$BASE_URL/api/v1/auth/register"
}

register "$JAR_A" "$EMAIL_A" "$HANDLE_A" "G03 A"
register "$JAR_B" "$EMAIL_B" "$HANDLE_B" "G03 B"
CSRF_A="$(csrf_from_jar "$JAR_A")"
CSRF_B="$(csrf_from_jar "$JAR_B")"
USER_A="$(json_value "$WORKDIR/register-$HANDLE_A.json" user.id)"
USER_B="$(json_value "$WORKDIR/register-$HANDLE_B.json" user.id)"
[ "$USER_A" != "$USER_B" ] || { echo "A and B share an id" >&2; exit 1; }

# A's private context contains a sentinel that must never reach B.
curl -fsS -b "$JAR_A" -c "$JAR_A" -o /dev/null \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_A" \
  -X PUT -d '{"content":"MARK-G03-PRIVATE-A"}' "$BASE_URL/api/v1/ai/context"

# Create the shared object.
curl -fsS -b "$JAR_A" -c "$JAR_A" -o "$WORKDIR/object.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_A" \
  -H "Idempotency-Key: $(idempotency_key)" \
  -d '{"object_type":"collaborative","title":"G03 Compose object","content":{"summary":"compose","body":"shared body"},"shared_context":{"note":"shared"}}' \
  "$BASE_URL/api/v1/objects"
OBJECT_ID="$(json_value "$WORKDIR/object.json" id)"

# A creates a Derive Link (read+contribute+derive), B accepts.
curl -fsS -b "$JAR_A" -c "$JAR_A" -o "$WORKDIR/invite.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_A" \
  -H "Idempotency-Key: $(idempotency_key)" \
  -d '{"permissions":["read","contribute","derive"],"max_uses":1,"expires_in_days":7}' \
  "$BASE_URL/api/v1/objects/$OBJECT_ID/invitations"
TOKEN="$(json_value "$WORKDIR/invite.json" token)"

curl -fsS -b "$JAR_B" -c "$JAR_B" -o "$WORKDIR/preview.json" \
  "$BASE_URL/api/v1/invitations/$TOKEN"
[ "$(json_value "$WORKDIR/preview.json" remaining_uses)" = "1" ] || {
  echo "invitation preview remaining uses mismatch" >&2; exit 1;
}
curl -fsS -b "$JAR_B" -c "$JAR_B" -o "$WORKDIR/accept.json" \
  -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_B" \
  -X POST "$BASE_URL/api/v1/invitations/$TOKEN/accept"
[ "$(json_value "$WORKDIR/accept.json" role_effective)" = "editor" ] || {
  echo "unexpected accepted role" >&2; exit 1;
}

# B's Handoff must contain shared state but not A's private context or token.
curl -fsS -b "$JAR_B" -o "$WORKDIR/handoff.json" "$BASE_URL/api/v1/objects/$OBJECT_ID/handoff"
python3 - "$WORKDIR/handoff.json" "$TOKEN" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as handle:
    text = handle.read()
handoff = json.loads(text)
if "MARK-G03-PRIVATE-A" in text:
    raise SystemExit("handoff leaked private context")
if sys.argv[2] in text:
    raise SystemExit("handoff leaked the invitation token")
if handoff["permitted_actions"] != ["read", "contribute", "derive"]:
    raise SystemExit("handoff permitted_actions mismatch")
if handoff["permitted_object_context"]["state"]["body"] != "shared body":
    raise SystemExit("handoff omitted shared state")
PY

# B's AI uses the deterministic fixture structured turn.
curl -fsS -b "$JAR_B" -c "$JAR_B" -o "$WORKDIR/conversation.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_B" \
  -H "Idempotency-Key: $(idempotency_key)" \
  -d "{\"title\":\"G03 AI\",\"object_id\":\"$OBJECT_ID\"}" \
  "$BASE_URL/api/v1/ai/conversations"
CONVERSATION_ID="$(json_value "$WORKDIR/conversation.json" id)"
MESSAGE_KEY="$(idempotency_key)"
curl -fsS -b "$JAR_B" -c "$JAR_B" -o "$WORKDIR/turn.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_B" \
  -H "Idempotency-Key: $MESSAGE_KEY" \
  -d '{"content":"propose"}' \
  "$BASE_URL/api/v1/ai/conversations/$CONVERSATION_ID/messages"
PROPOSAL_ID="$(json_value "$WORKDIR/turn.json" proposed_actions.0.proposal_id)"
CONFIRMATION_ID="$(json_value "$WORKDIR/turn.json" proposed_actions.1.confirmation_id)"
[ -n "$PROPOSAL_ID" ] && [ -n "$CONFIRMATION_ID" ] || {
  echo "fixture AI did not create a proposal and confirmation" >&2; exit 1;
}

# Approve the AI derive, then apply the human proposal.
curl -fsS -b "$JAR_B" -c "$JAR_B" -o "$WORKDIR/child.json" \
  -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_B" \
  -H "Idempotency-Key: $(idempotency_key)" \
  -X POST "$BASE_URL/api/v1/confirmations/$CONFIRMATION_ID/approve"
CHILD_ID="$(json_value "$WORKDIR/child.json" id)"
[ "$(json_value "$WORKDIR/child.json" version)" = "1" ] || {
  echo "confirmed child version is not 1" >&2; exit 1;
}

curl -fsS -b "$JAR_B" -c "$JAR_B" -o "$WORKDIR/apply.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_B" \
  -d '{"expected_version":1}' \
  "$BASE_URL/api/v1/objects/$OBJECT_ID/proposals/$PROPOSAL_ID/apply"
[ "$(json_value "$WORKDIR/apply.json" object.version)" = "2" ] || {
  echo "proposal apply did not increment the object version" >&2; exit 1;
}
[ "$(json_value "$WORKDIR/apply.json" object.state.body)" = "Fixture proposed body" ] || {
  echo "proposal body was not applied" >&2; exit 1;
}

if [ "$RESTART_APP" = "1" ]; then
  docker compose restart app
  wait_ready
  curl -fsS -b "$JAR_B" -o "$WORKDIR/after-restart.json" \
    "$BASE_URL/api/v1/objects/$OBJECT_ID"
  [ "$(json_value "$WORKDIR/after-restart.json" version)" = "2" ] || {
    echo "object did not persist across restart" >&2; exit 1;
  }
  curl -fsS -b "$JAR_B" -o "$WORKDIR/child-restart.json" \
    "$BASE_URL/api/v1/objects/$CHILD_ID"
  [ "$(json_value "$WORKDIR/child-restart.json" version)" = "1" ] || {
    echo "child did not persist across restart" >&2; exit 1;
  }
fi

# PAT scope and cookie-only boundaries.
curl -fsS -b "$JAR_A" -c "$JAR_A" -o "$WORKDIR/token.json" \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_A" \
  -d '{"name":"g03 e2e","scopes":["objects:read"],"expires_in_days":1}' \
  "$BASE_URL/api/v1/tokens"
PAT="$(json_value "$WORKDIR/token.json" plaintext)"
TOKEN_ID="$(json_value "$WORKDIR/token.json" id)"
[ "${PAT#dw_pat_}" != "$PAT" ] || { echo "PAT prefix missing" >&2; exit 1; }
curl -fsS -H "Authorization: Bearer $PAT" -o /dev/null \
  "$BASE_URL/api/v1/objects/$OBJECT_ID"
AI_STATUS="$(curl -sS -H "Authorization: Bearer $PAT" -o /dev/null -w '%{http_code}' \
  "$BASE_URL/api/v1/ai/profile")"
[ "$AI_STATUS" = "401" ] || { echo "PAT reached a cookie-only endpoint: $AI_STATUS" >&2; exit 1; }
curl -fsS -b "$JAR_A" -c "$JAR_A" -o /dev/null -X DELETE \
  -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_A" \
  "$BASE_URL/api/v1/tokens/$TOKEN_ID"
PAT_STATUS="$(curl -sS -H "Authorization: Bearer $PAT" -o /dev/null -w '%{http_code}' \
  "$BASE_URL/api/v1/objects/$OBJECT_ID")"
[ "$PAT_STATUS" = "401" ] || { echo "revoked PAT status = $PAT_STATUS" >&2; exit 1; }

# Revoking B blocks the next content read and the old message idempotency key.
curl -fsS -b "$JAR_A" -c "$JAR_A" -o /dev/null -X DELETE \
  -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_A" \
  "$BASE_URL/api/v1/objects/$OBJECT_ID/members/$USER_B"
HANDOFF_STATUS="$(curl -sS -b "$JAR_B" -o /dev/null -w '%{http_code}' \
  "$BASE_URL/api/v1/objects/$OBJECT_ID/handoff")"
[ "$HANDOFF_STATUS" = "404" ] || { echo "revoked handoff status = $HANDOFF_STATUS" >&2; exit 1; }
REPLAY_STATUS="$(curl -sS -b "$JAR_B" -o /dev/null -w '%{http_code}' \
  -H 'Content-Type: application/json' -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF_B" \
  -H "Idempotency-Key: $MESSAGE_KEY" -d '{"content":"propose"}' \
  "$BASE_URL/api/v1/ai/conversations/$CONVERSATION_ID/messages")"
[ "$REPLAY_STATUS" = "404" ] || { echo "revoked idempotency replay status = $REPLAY_STATUS" >&2; exit 1; }

echo "G03 E2E passed: invitation, handoff, AI proposal/confirmation, authoritative execution, PAT scope, revocation"
