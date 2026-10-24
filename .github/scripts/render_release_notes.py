#!/usr/bin/env python3
"""Render release notes from the verified manifest and attestation record.

A static candidate-status document must never be used as a release body: it
would claim the wrong tag, version, and attestation state. Every value here
comes from the manifest for this exact tag.
"""

import argparse
import json
import pathlib
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dist", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    args = parser.parse_args()

    dist = pathlib.Path(args.dist).resolve()
    manifest = json.loads((dist / "BUILD-MANIFEST.json").read_text(encoding="utf-8"))
    record = json.loads((dist / "ATTESTATION.json").read_text(encoding="utf-8"))

    if manifest.get("tag") != args.tag or manifest.get("version") != args.version:
        print("render_release_notes: manifest tag/version does not match the release", file=sys.stderr)
        return 1
    if manifest.get("commit") != args.commit:
        print("render_release_notes: manifest commit does not match the release", file=sys.stderr)
        return 1
    if args.version != args.tag[1:]:
        print("render_release_notes: version does not match the tag", file=sys.stderr)
        return 1

    attestation = record.get("status")
    if attestation == "generated":
        provenance = (
            "GitHub build-provenance attestations were generated and verified for every "
            "asset, the build manifest, and `SHA256SUMS`."
        )
    else:
        provenance = "No build-provenance attestation is attached to this release."

    assets = "\n".join(
        f"- `{item['name']}` — SHA-256 `{item['sha256']}`" for item in manifest.get("artifacts", [])
    )
    base = f"https://github.com/{args.repository}"

    notes = f"""# Derive World {args.tag}

Built from commit `{manifest['commit']}` (tree `{manifest['tree']}`) by
[GitHub Actions run]({manifest['workflow_run']}).

## Assets

{assets}

## Verification

`SHA256SUMS` covers every asset. {provenance}

SHA-256 proves file integrity; the attestation, when present, binds the assets
to this repository, commit, and release workflow identity.

## Requirements and limits

See [Limitations]({base}/blob/{args.tag}/docs/en/limitations.md) and
[Quick start]({base}/blob/{args.tag}/docs/en/quickstart.md). This release is
the `v0.1.0` OSS MVP, not a production-ready platform.

## License

Apache License 2.0. See `LICENSE` and `NOTICE`.
"""
    pathlib.Path(args.out).write_text(notes, encoding="utf-8")
    print(notes)
    return 0


if __name__ == "__main__":
    sys.exit(main())
