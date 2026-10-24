#!/usr/bin/env python3
"""Resolve and validate the release mode, tag, and version.

This is the only place workflow_dispatch inputs become release identity. It
rejects any value that is not a single, whole, expected token, so a value
containing a newline cannot inject additional GITHUB_OUTPUT entries.

Prints KEY=VALUE lines for GITHUB_OUTPUT. Unknown or malformed input exits 2.
"""

import argparse
import re
import sys

TAG_RE = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?")
VERSION_RE = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?")


def fail(message):
    print(f"release identity refused: {message}", file=sys.stderr)
    return 2


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--event", required=True, choices=("push", "workflow_dispatch"))
    parser.add_argument("--mode", default="")
    parser.add_argument("--tag", default="")
    parser.add_argument("--version", default="")
    parser.add_argument("--ref-name", default="")
    args = parser.parse_args()

    if args.event == "push":
        mode = "tag"
        tag = args.ref_name
    else:
        mode = args.mode
        tag = args.tag

    if mode not in ("dry-run", "tag"):
        return fail(f"mode must be dry-run or tag, got {mode!r}")

    if mode == "tag":
        if not TAG_RE.fullmatch(tag):
            return fail(f"tag must be a single vMAJOR.MINOR.PATCH[-rc.N] token, got {tag!r}")
        version = tag[1:]
        if not VERSION_RE.fullmatch(version):
            return fail(f"derived version is invalid: {version!r}")
    else:
        if tag:
            return fail("dry-run must not carry a tag")
        version = args.version
        if not VERSION_RE.fullmatch(version):
            return fail(f"version must be a single MAJOR.MINOR.PATCH[-rc.N] token, got {version!r}")
        tag = ""

    print(f"mode={mode}")
    print(f"tag={tag}")
    print(f"version={version}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
