#!/usr/bin/env python3
"""Write ATTESTATION.json after the provenance step.

The build manifest is generated before attestation, so it cannot contain the
attestation result without invalidating itself. This file records the status,
the exact hashes of the manifest and SHA256SUMS, and every attested subject.
"""

import argparse
import hashlib
import json
import pathlib
import sys


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dist", required=True)
    parser.add_argument("--status", required=True)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--tag", default="")
    parser.add_argument("--commit", required=True)
    parser.add_argument("--workflow-run", required=True)
    parser.add_argument("--signer-workflow", default="")
    args = parser.parse_args()

    dist = pathlib.Path(args.dist).resolve()
    manifest = dist / "BUILD-MANIFEST.json"
    sums = dist / "SHA256SUMS"
    if not manifest.is_file() or not sums.is_file():
        print("write_attestation_record: manifest or SHA256SUMS is missing", file=sys.stderr)
        return 1

    archives = sorted(
        list(dist.glob("derive-world_*.tar.gz")) + list(dist.glob("derive-world_*.zip")),
        key=lambda path: path.name,
    )
    subjects = [manifest, sums, *archives]
    record = {
        "schema": 1,
        "status": args.status,
        "repository": args.repository,
        "tag": args.tag or None,
        "commit": args.commit,
        "workflow_run": args.workflow_run,
        "signer_workflow": args.signer_workflow or f"{args.repository}/.github/workflows/release.yml",
        "manifest_sha256": sha256(manifest),
        "sha256sums_sha256": sha256(sums),
        "subjects": [
            {"name": path.name, "sha256": sha256(path), "bytes": path.stat().st_size}
            for path in subjects
        ],
        "note": (
            "status=generated means GitHub build provenance was generated and verified "
            "for every subject. Other statuses record the gap and are not an attestation."
        ),
    }
    (dist / "ATTESTATION.json").write_text(
        json.dumps(record, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    print(json.dumps(record, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
