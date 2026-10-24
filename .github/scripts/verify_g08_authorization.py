#!/usr/bin/env python3
"""Fail-closed validation of the Owner's G08 release authorization record.

The record is supplied by the Owner as the repository variable
G08_AUTHORIZATION. It is a machine-readable mirror of an Owner decision; it is
not an independent reviewer gate and does not authorize publication by itself.

Exit codes:
  0  the record authorizes exactly this repository, tag, commit, and scope
  2  the record is missing or empty
  3  the record is present but does not match or is malformed

Any non-zero exit must stop the release workflow before a build starts.
"""

import argparse
import datetime
import json
import os
import re
import sys

COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")
TAG_RE = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?$")


def report(code, message):
    print(f"G08 authorization refused: {message}", file=sys.stderr)
    return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--scope", default="public-release")
    args = parser.parse_args()

    raw = os.environ.get("G08_AUTHORIZATION", "").strip()
    if not raw:
        return report(2, "record is missing (repository variable G08_AUTHORIZATION is unset or empty)")

    try:
        record = json.loads(raw)
    except json.JSONDecodeError as exc:
        return report(3, f"record is not valid JSON: {exc}")

    if not isinstance(record, dict):
        return report(3, "record must be a JSON object")

    if not COMMIT_RE.match(args.commit):
        return report(3, f"expected commit is not a 40-character lowercase hex SHA: {args.commit!r}")
    if not TAG_RE.match(args.tag):
        return report(3, f"expected tag is not a release tag: {args.tag!r}")

    failures = []
    if record.get("schema") != 1:
        failures.append("schema must be 1")
    if record.get("authorized") is not True:
        failures.append("authorized must be true")
    if record.get("repository") != args.repository:
        failures.append(f"repository {record.get('repository')!r} does not match {args.repository!r}")
    if record.get("tag") != args.tag:
        failures.append(f"tag {record.get('tag')!r} does not match {args.tag!r}")
    if record.get("commit") != args.commit:
        failures.append(f"commit {record.get('commit')!r} does not match {args.commit!r}")
    if record.get("scope") != args.scope:
        failures.append(f"scope {record.get('scope')!r} does not match {args.scope!r}")
    if record.get("authorized_by") != "Owner":
        failures.append("authorized_by must be exactly 'Owner'")

    issued_at = record.get("issued_at")
    if not isinstance(issued_at, str) or not issued_at.strip():
        failures.append("issued_at is required")
    else:
        try:
            datetime.datetime.fromisoformat(issued_at.replace("Z", "+00:00"))
        except ValueError:
            failures.append("issued_at must be ISO 8601")

    expires_at = record.get("expires_at")
    if expires_at is not None:
        try:
            expires = datetime.datetime.fromisoformat(str(expires_at).replace("Z", "+00:00"))
        except ValueError:
            failures.append("expires_at must be ISO 8601 when present")
        else:
            now = datetime.datetime.now(datetime.timezone.utc)
            if expires.tzinfo is None:
                failures.append("expires_at must include a timezone")
            elif expires <= now:
                failures.append("authorization record has expired")

    if failures:
        for failure in failures:
            print(f"G08 authorization refused: {failure}", file=sys.stderr)
        return 3

    issued_at = record.get("issued_at")
    suffix = f" (issued_at {issued_at})" if isinstance(issued_at, str) and issued_at else ""
    print(
        "G08 authorization accepted for "
        f"{args.repository} {args.tag} {args.commit} scope={args.scope}{suffix}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
