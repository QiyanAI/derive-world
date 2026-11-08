#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# One-click stop for every growth-engine command on this checkout.
# The engine checks var/growth-engine/STOP before doing work. No network
# request and no publish happens after the marker exists.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$ROOT/var/growth-engine"
printf 'stopped %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$ROOT/var/growth-engine/STOP"
echo "growth engine stopped: $ROOT/var/growth-engine/STOP"
echo "run scripts/resume.sh to allow work again"
