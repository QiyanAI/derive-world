#!/usr/bin/env python3
"""Credential-free tests for release identity validation.

The critical case is a workflow_dispatch value carrying a newline: it must be
rejected so it cannot inject extra GITHUB_OUTPUT entries.
"""

import os
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
SCRIPT = ROOT / ".github" / "scripts" / "resolve_release_identity.py"


def run(args, expected_code, label, expect=None):
    result = subprocess.run(
        [sys.executable, str(SCRIPT), *args],
        capture_output=True,
        text=True,
        env=dict(os.environ),
    )
    ok = result.returncode == expected_code
    if ok and expect is not None:
        ok = expect in result.stdout
    if not ok:
        print(f"FAIL {label}: exit {result.returncode}, want {expected_code}")
        print(result.stdout)
        print(result.stderr)
        return False
    print(f"ok   {label}")
    return True


def main():
    checks = []
    checks.append(run(
        ["--event", "workflow_dispatch", "--mode", "dry-run", "--version", "0.1.0-rc.1"],
        0, "dry-run version", expect="version=0.1.0-rc.1\n",
    ))
    checks.append(run(
        ["--event", "workflow_dispatch", "--mode", "tag", "--tag", "v0.1.0"],
        0, "dispatch tag", expect="tag=v0.1.0\n",
    ))
    checks.append(run(
        ["--event", "push", "--ref-name", "v0.1.0"],
        0, "push tag", expect="mode=tag\n",
    ))
    checks.append(run(
        ["--event", "workflow_dispatch", "--mode", "dry-run", "--version", "0.1.0-rc.1\nmode=tag\ntag=v0.1.0"],
        2, "newline injection in version",
    ))
    checks.append(run(
        ["--event", "workflow_dispatch", "--mode", "dry-run", "--version", "0.1.0-rc.1\rmode=tag"],
        2, "carriage-return injection in version",
    ))
    checks.append(run(
        ["--event", "workflow_dispatch", "--mode", "tag", "--tag", "v0.1.0\nmode=tag"],
        2, "newline injection in tag",
    ))
    checks.append(run(
        ["--event", "workflow_dispatch", "--mode", "dry-run", "--version", "0.1.0-rc.1\n"],
        2, "trailing newline in version",
    ))
    checks.append(run(
        ["--event", "workflow_dispatch", "--mode", "publish", "--version", "0.1.0"],
        2, "unknown mode",
    ))
    checks.append(run(
        ["--event", "workflow_dispatch", "--mode", "dry-run", "--version", "v0.1.0"],
        2, "version with leading v",
    ))
    checks.append(run(
        ["--event", "workflow_dispatch", "--mode", "tag", "--tag", "v0.1"],
        2, "incomplete tag",
    ))
    checks.append(run(
        ["--event", "push", "--ref-name", "main"],
        2, "push of a non-tag ref",
    ))
    checks.append(run(
        ["--event", "workflow_dispatch", "--mode", "dry-run", "--version", "0.1.0", "--tag", "v0.1.0"],
        2, "dry-run with tag",
    ))

    if not all(checks):
        print(f"{checks.count(False)} of {len(checks)} checks failed")
        return 1
    print(f"all {len(checks)} identity checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
