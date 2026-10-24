#!/usr/bin/env python3
"""Credential-free tests for the fail-closed G08 authorization validator.

These tests exercise only the validator's logic. They do not call GitHub and do
not authorize anything.
"""

import json
import os
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
SCRIPT = ROOT / ".github" / "scripts" / "verify_g08_authorization.py"

REPO = "QiyanAI/derive-world"
TAG = "v0.1.0"
COMMIT = "0123456789abcdef0123456789abcdef01234567"


def base_record():
    return {
        "schema": 1,
        "authorized": True,
        "repository": REPO,
        "tag": TAG,
        "commit": COMMIT,
        "scope": "public-release",
        "authorized_by": "Owner",
        "issued_at": "2026-10-24T00:00:00Z",
    }


def run(record, expected_code, label, raw=None):
    env = dict(os.environ)
    env.pop("G08_AUTHORIZATION", None)
    if raw is not None:
        env["G08_AUTHORIZATION"] = raw
    elif record is not None:
        env["G08_AUTHORIZATION"] = json.dumps(record)
    result = subprocess.run(
        [
            sys.executable,
            str(SCRIPT),
            "--repository",
            REPO,
            "--tag",
            TAG,
            "--commit",
            COMMIT,
        ],
        capture_output=True,
        text=True,
        env=env,
    )
    if result.returncode != expected_code:
        print(f"FAIL {label}: exit {result.returncode}, want {expected_code}")
        print(result.stdout)
        print(result.stderr)
        return False
    print(f"ok   {label}")
    return True


def main():
    checks = []
    checks.append(run(None, 2, "missing record"))
    checks.append(run(None, 2, "empty record", raw="   "))

    record = base_record()
    checks.append(run(record, 0, "valid record"))
    checks.append(run(None, 3, "invalid JSON", raw="{not json"))

    record = base_record()
    record["authorized"] = False
    checks.append(run(record, 3, "authorized false"))

    record = base_record()
    record["repository"] = "someone/else"
    checks.append(run(record, 3, "wrong repository"))

    record = base_record()
    record["tag"] = "v0.1.1"
    checks.append(run(record, 3, "wrong tag"))

    record = base_record()
    record["commit"] = "f" * 40
    checks.append(run(record, 3, "wrong commit"))

    record = base_record()
    record["scope"] = "private-build"
    checks.append(run(record, 3, "wrong scope"))

    record = base_record()
    record["authorized_by"] = "automation"
    checks.append(run(record, 3, "wrong authorizer"))

    record = base_record()
    record["schema"] = 2
    checks.append(run(record, 3, "wrong schema"))

    record = base_record()
    for key in ("schema", "authorized", "repository", "tag", "commit", "scope", "authorized_by"):
        record.pop(key)
    checks.append(run(record, 3, "empty object"))

    record = base_record()
    records = [record, ["not", "an", "object"]]
    checks.append(run(records[1], 3, "non-object JSON"))

    record = base_record()
    record.pop("issued_at")
    checks.append(run(record, 3, "missing issued_at"))

    record = base_record()
    record["issued_at"] = "not-a-date"
    checks.append(run(record, 3, "invalid issued_at"))

    record = base_record()
    record["expires_at"] = "2020-01-01T00:00:00Z"
    checks.append(run(record, 3, "expired record"))

    record = base_record()
    record["expires_at"] = "2099-01-01T00:00:00Z"
    checks.append(run(record, 0, "unexpired record"))

    if not all(checks):
        print(f"{checks.count(False)} of {len(checks)} checks failed")
        return 1
    print(f"all {len(checks)} authorization checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
