#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# One-time owner setup: create the dge-state branch that holds the cumulative
# model usage ledger across GitHub Actions runs. The branch contains no secret
# and no product code. Run once with an authenticated gh CLI.
set -euo pipefail

REPO="${REPO:-QiyanAI/derive-world}"
BRANCH="${BRANCH:-dge-state}"

gh auth setup-git
if git ls-remote --exit-code --heads "https://github.com/$REPO.git" "$BRANCH" >/dev/null 2>&1; then
  echo "state branch already exists: $BRANCH"
  exit 0
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
git clone --depth 1 "https://github.com/$REPO.git" "$TMP/state"
cd "$TMP/state"
git checkout --orphan "$BRANCH"
git rm -rf . >/dev/null 2>&1 || true
cat > usage.json <<'JSON'
{
  "schema_version": 2,
  "authorization_id": "",
  "calls": 0,
  "input_tokens": 0,
  "output_tokens": 0,
  "cost_usd": 0,
  "cost_cap_enforced": false,
  "updated_at": ""
}
JSON
git add usage.json
git -c user.name="growth-engine[bot]" -c user.email="growth-engine@users.noreply.github.com" \
  commit -m "dge: initialize cumulative usage state"
git push origin "$BRANCH"
echo "created $BRANCH with usage.json"
echo "The AI workflow will clone this branch and commit reservations before each model call."
