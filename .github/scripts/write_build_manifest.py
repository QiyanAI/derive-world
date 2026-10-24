#!/usr/bin/env python3
"""Write BUILD-MANIFEST.json from the artifacts and toolchain actually used.

The manifest is evidence, not decoration. Every value is read from the build
environment or computed from the files on disk. The script refuses to write a
manifest when a binary does not embed the pinned Go version or when
SHA256SUMS does not cover exactly the release archives.
"""

import argparse
import datetime
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import sys

PLATFORMS = ("linux_amd64", "darwin_arm64", "windows_amd64")


def fail(message):
    print(f"write_build_manifest: {message}", file=sys.stderr)
    return 1


def run(command):
    return subprocess.check_output(command, text=True).strip()


def first_line(command):
    return run(command).splitlines()[0]


def tool_line(name, *arguments):
    executable = shutil.which(name) or name
    return first_line([executable, *arguments])


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def parse_go_version_m(go_bin, binary):
    output = run([go_bin, "version", "-m", str(binary)])
    lines = output.splitlines()
    match = re.match(r"^.*?:\s*(go[0-9][^\s]*)$", lines[0])
    if not match:
        raise SystemExit(f"cannot parse go version -m output for {binary}: {lines[0]!r}")
    settings = {}
    for line in lines[1:]:
        line = line.strip()
        if line.startswith("build\t"):
            key, _, value = line[len("build\t"):].partition("=")
            settings[key] = value
    return {
        "go_version": match.group(1),
        "goos": settings.get("GOOS"),
        "goarch": settings.get("GOARCH"),
        "cgo_enabled": settings.get("CGO_ENABLED"),
        "trimpath": settings.get("-trimpath") == "true",
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dist", required=True)
    parser.add_argument("--bin-dir", required=True)
    parser.add_argument("--mode", required=True, choices=("dry-run", "tag"))
    parser.add_argument("--repository", required=True)
    parser.add_argument("--tag", default="")
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--tree", required=True)
    parser.add_argument("--source-date-epoch", required=True, type=int)
    parser.add_argument("--build-time", required=True)
    parser.add_argument("--workflow-run", required=True)
    parser.add_argument("--ci-run", required=True)
    parser.add_argument("--security-run", required=True)
    parser.add_argument("--e2e-run", required=True)
    parser.add_argument("--attestation-status", default="recorded_in_attestation_file")
    parser.add_argument("--authorization-sha256", default="")
    parser.add_argument("--tag-object-sha", default="")
    parser.add_argument("--tag-commit-sha", default="")
    parser.add_argument("--build-started-at", default="")
    parser.add_argument("--expected-go", default="go1.27.3")
    parser.add_argument("--expected-node", default="24.14.0")
    parser.add_argument("--go", default="go", help="go binary used for metadata checks")
    args = parser.parse_args()

    dist = pathlib.Path(args.dist).resolve()
    bin_dir = pathlib.Path(args.bin_dir).resolve()

    go_line = first_line([args.go, "version"])
    match = re.search(r"\b(go[0-9][^\s]*)", go_line)
    if not match or match.group(1) != args.expected_go:
        return fail(f"actual Go toolchain is {go_line!r}, expected {args.expected_go}")
    go_env_version = run([args.go, "env", "GOVERSION"])
    if go_env_version != args.expected_go:
        return fail(f"go env GOVERSION is {go_env_version!r}, expected {args.expected_go}")

    if args.mode == "tag" and not args.tag:
        return fail("tag mode requires --tag")
    if args.mode == "dry-run" and args.tag:
        return fail("dry-run mode must not carry a tag")
    if args.mode == "tag":
        if not re.fullmatch(r"[0-9a-f]{64}", args.authorization_sha256):
            return fail("tag mode requires the SHA-256 of the G08 authorization record")
        if not re.fullmatch(r"[0-9a-f]{40}", args.tag_object_sha):
            return fail("tag mode requires the resolved tag object SHA")
        if args.tag_commit_sha != args.commit:
            return fail("tag-commit-sha must equal the build commit")
    node_version = tool_line("node", "--version")
    if node_version != "v" + args.expected_node:
        return fail(f"actual Node is {node_version!r}, expected v{args.expected_node}")

    sums_path = dist / "SHA256SUMS"
    if not sums_path.is_file():
        return fail("SHA256SUMS is missing")
    sums = {}
    for line in sums_path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        digest, name = line.split("  ", 1)
        sums[name] = digest

    archives = sorted(
        list(dist.glob("derive-world_*.tar.gz")) + list(dist.glob("derive-world_*.zip")),
        key=lambda path: path.name,
    )
    names = {path.name for path in archives}
    if names != set(sums):
        return fail(f"SHA256SUMS covers {sorted(sums)} but artifacts are {sorted(names)}")

    binaries = []
    for platform in PLATFORMS:
        source = f"derive-world_{platform}"
        if platform == "windows_amd64":
            source += ".exe"
        binary = bin_dir / source
        if not binary.is_file():
            return fail(f"missing binary {binary}")
        meta = parse_go_version_m(args.go, binary)
        if meta["go_version"] != args.expected_go:
            return fail(f"{source} embeds {meta['go_version']}, expected {args.expected_go}")
        if not meta["trimpath"]:
            return fail(f"{source} was not built with -trimpath")
        if meta["cgo_enabled"] != "0":
            return fail(f"{source} was built with CGO_ENABLED={meta['cgo_enabled']}")
        binaries.append(
            {
                "name": source,
                "sha256": sha256(binary),
                "bytes": binary.stat().st_size,
                **meta,
            }
        )

    artifacts = [
        {"name": path.name, "sha256": sha256(path), "bytes": path.stat().st_size}
        for path in archives
    ]
    for artifact in artifacts:
        if sums[artifact["name"]] != artifact["sha256"]:
            return fail(f"SHA256SUMS entry for {artifact['name']} does not match the file")

    if not args.build_started_at:
        earliest = min(path.stat().st_mtime for path in (bin_dir / name for name in (
            "derive-world_linux_amd64", "derive-world_darwin_arm64", "derive-world_windows_amd64.exe")))
        args.build_started_at = datetime.datetime.fromtimestamp(
            earliest, datetime.timezone.utc).isoformat().replace("+00:00", "Z")
    manifest_written_at = datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z")

    manifest = {
        "schema": 1,
        "mode": args.mode,
        "repository": args.repository,
        "tag": args.tag or None,
        "version": args.version,
        "commit": args.commit,
        "tree": args.tree,
        "source_date_epoch": args.source_date_epoch,
        "build_time": args.build_time,
        "build_time_semantics": "commit timestamp used for reproducible ldflags",
        "commit_time": args.build_time,
        "build_started_at": args.build_started_at,
        "manifest_written_at": manifest_written_at,
        "tag_object_sha": args.tag_object_sha or None,
        "tag_commit_sha": args.tag_commit_sha or None,
        "authorization_sha256": args.authorization_sha256 or None,
        "workflow_run": args.workflow_run,
        "ci_run": args.ci_run,
        "security_run": args.security_run,
        "e2e_run": args.e2e_run,
        "required_checks": [
            "Go",
            "Web",
            "SDK",
            "Architecture",
            "Contract",
            "Integration",
            "govulncheck",
            "gitleaks",
            "E2E",
            "License",
            "npm audit",
        ],
        "toolchain": {
            "go": go_line,
            "go_version": go_env_version,
            "node": node_version,
            "npm": tool_line("npm", "--version"),
            "python": first_line([sys.executable, "--version"]),
            "tar": tool_line("tar", "--version"),
            "gzip": tool_line("gzip", "--version"),
        },
        "binaries": binaries,
        "artifacts": artifacts,
        "sha256sums": {"name": "SHA256SUMS", "sha256": sha256(sums_path)},
        "attestation": {
            "status": args.attestation_status,
            "record": "ATTESTATION.json",
            "note": (
                "SHA-256 is integrity-only. Attestation.json records whether build "
                "provenance was generated and verified, and binds the hashes of this "
                "manifest and SHA256SUMS."
            ),
        },
        "release": {
            "mode": args.mode,
            "draft_target": args.mode == "tag",
            "published": False,
            "note": (
                "draft_target describes the mode. The draft job is the only job that may "
                "create a draft Release, and publication is never automated."
            ),
        },
    }

    (dist / "BUILD-MANIFEST.json").write_text(
        json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    print(json.dumps(manifest, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
