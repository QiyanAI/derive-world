#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Download the latest github-metrics artifact with a bounded retry/backoff.
# The caller must have already resolved a successful metrics run id.
#
# Usage:
#   bash scripts/fetch-metrics-artifact.sh RUN_ID DEST [ATTEMPTS]
#
# Exit non-zero when the artifact is missing, expired, or still unavailable
# after the bounded attempts. Never falls back to a committed baseline.
set -euo pipefail

RUN_ID="${1:-}"
DEST="${2:-}"
ATTEMPTS="${3:-3}"

if [ -z "$RUN_ID" ] || [ -z "$DEST" ]; then
  echo "usage: fetch-metrics-artifact.sh RUN_ID DEST [ATTEMPTS]" >&2
  exit 2
fi
if ! [[ "$ATTEMPTS" =~ ^[0-9]+$ ]] || [ "$ATTEMPTS" -lt 1 ]; then
  echo "ATTEMPTS must be a positive integer" >&2
  exit 2
fi

REPO="${GITHUB_REPOSITORY:-QiyanAI/derive-world}"
TMP="${DEST}.tmp.$$"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$(dirname "$DEST")"

delay=5
for attempt in $(seq 1 "$ATTEMPTS"); do
  rm -rf "$TMP"
  if gh run download "$RUN_ID" --repo "$REPO" -n github-metrics -D "$TMP"; then
    if [ ! -f "$TMP/snapshot.json" ]; then
      echo "::error::github-metrics artifact $RUN_ID did not contain snapshot.json"
      exit 1
    fi
    python3 - "$TMP/snapshot.json" "$TMP/provenance.json" "$RUN_ID" <<'PY'
import datetime
import json
import sys

snapshot_path, out_path, run_id = sys.argv[1:4]
snapshot = json.load(open(snapshot_path, encoding="utf-8"))
quality = snapshot.get("data_quality") or {}
provenance = {
    "source_run_id": run_id,
    "artifact_name": "github-metrics",
    "snapshot_captured_at": snapshot.get("captured_at"),
    "traffic_quality": quality.get("traffic", "UNKNOWN"),
    "contributors_quality": quality.get("contributors", "UNKNOWN"),
    "downloaded_at": datetime.datetime.now(datetime.timezone.utc)
    .replace(microsecond=0)
    .isoformat()
    .replace("+00:00", "Z"),
}
with open(out_path, "w", encoding="utf-8") as handle:
    json.dump(provenance, handle, indent=2)
    handle.write("\n")
PY
    rm -rf "$DEST"
    mv "$TMP" "$DEST"
    echo "metrics artifact $RUN_ID downloaded to $DEST"
    exit 0
  fi
  echo "::warning::metrics artifact $RUN_ID download failed (attempt $attempt/$ATTEMPTS)"
  if [ "$attempt" -ge "$ATTEMPTS" ]; then
    break
  fi
  sleep "$delay"
  delay=$((delay * 3))
done

echo "::error::metrics artifact $RUN_ID unavailable after $ATTEMPTS attempts; refusing to generate a growth report"
exit 1
