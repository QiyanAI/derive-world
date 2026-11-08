#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
rm -f "$ROOT/var/growth-engine/STOP"
echo "growth engine resumed: $ROOT/var/growth-engine/STOP removed"
