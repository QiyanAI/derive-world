#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Export public issue and pull-request metadata for the growth engine.
#
# This is read-only and item-level. It complements ops/github-metrics (which
# owns the daily aggregate snapshot); it does not replace or duplicate it.
# It never prints a token and never writes outside the output path.
#
# Usage:
#   REPO=QiyanAI/derive-world bash scripts/engagement-export.sh engagement.json
#
# Requires an authenticated gh CLI with read access to the repository.
set -euo pipefail

REPO="${REPO:-QiyanAI/derive-world}"
OUT="${1:-}"
if [ -z "$OUT" ]; then
  echo "usage: REPO=owner/name bash scripts/engagement-export.sh OUTPUT.json" >&2
  exit 2
fi
if [ -e "$OUT" ]; then
  echo "refusing to overwrite $OUT; remove it first" >&2
  exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

gh api "repos/$REPO/issues?state=all&per_page=100" --paginate > "$TMP/issues.json"
gh api "repos/$REPO/pulls?state=all&per_page=100" --paginate > "$TMP/pulls.json"

CAPTURED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
python3 - "$TMP/issues.json" "$TMP/pulls.json" "$OUT" "$CAPTURED_AT" <<'PY'
import json
import sys

issues_path, pulls_path, out_path, captured_at = sys.argv[1:5]


def load(path):
    with open(path, encoding="utf-8") as handle:
        text = handle.read().strip()
    if not text:
        return []
    # --paginate concatenates pages; a single JSON array per page becomes
    # adjacent arrays, so normalise both shapes.
    try:
        value = json.loads(text)
        return value if isinstance(value, list) else [value]
    except json.JSONDecodeError:
        decoder = json.JSONDecoder()
        items, index = [], 0
        while index < len(text):
            while index < len(text) and text[index].isspace():
                index += 1
            if index >= len(text):
                break
            value, index = decoder.raw_decode(text, index)
            items.extend(value if isinstance(value, list) else [value])
        return items


def body_length(item):
    body = item.get("body") or ""
    return len(body)


issues = []
for item in load(issues_path):
    if item.get("pull_request") is not None:
        continue
    issues.append({
        "number": item.get("number", 0),
        "title": item.get("title", ""),
        "author": (item.get("user") or {}).get("login", ""),
        "author_association": item.get("author_association", "NONE"),
        "state": item.get("state", "open"),
        "created_at": item.get("created_at", ""),
        "comments": item.get("comments", 0),
        "body_length": body_length(item),
        "labels": [label.get("name", "") for label in item.get("labels", [])],
    })

pulls = []
for item in load(pulls_path):
    pulls.append({
        "number": item.get("number", 0),
        "title": item.get("title", ""),
        "author": (item.get("user") or {}).get("login", ""),
        "author_association": item.get("author_association", "NONE"),
        "state": item.get("state", "open"),
        "merged": item.get("merged_at") is not None,
        "created_at": item.get("created_at", ""),
        "body_length": body_length(item),
        "labels": [label.get("name", "") for label in item.get("labels", [])],
    })

payload = {
    "schema_version": 1,
    "captured_at": captured_at,
    "issues": issues,
    "pull_requests": pulls,
}
with open(out_path, "w", encoding="utf-8") as handle:
    json.dump(payload, handle, indent=2, ensure_ascii=False)
    handle.write("\n")
print(f"wrote {len(issues)} issues and {len(pulls)} pull requests to {out_path}")
PY
