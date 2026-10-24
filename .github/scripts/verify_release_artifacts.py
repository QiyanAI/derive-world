#!/usr/bin/env python3
"""Verify a release candidate's artifacts, manifest, and optional attestation.

Two modes:

  --mode dry-run  Private candidate. SHA-256 integrity, manifest binding,
                  archive contents, and embedded Go metadata are verified.
                  An attestation is expected to be recorded as unavailable.

  --mode tag      Authorized public release. Everything above plus a real
                  Sigstore build-provenance attestation must be present and
                  verify with gh attestation verify against this repository,
                  tag, commit, and signer workflow.

Any failure exits non-zero; the caller must not publish in that case.
"""

import argparse
import hashlib
import json
import pathlib
import re
import subprocess
import sys
import tarfile
import tempfile
import zipfile

PLATFORMS = {
    "linux_amd64": ("derive-world_linux_amd64", "derive-world", "tar.gz"),
    "darwin_arm64": ("derive-world_darwin_arm64", "derive-world", "tar.gz"),
    "windows_amd64": ("derive-world_windows_amd64.exe", "derive-world.exe", "zip"),
}

EXPECTED_TARGETS = {
    "linux_amd64": ("linux", "amd64"),
    "darwin_arm64": ("darwin", "arm64"),
    "windows_amd64": ("windows", "amd64"),
}

FORBIDDEN_SOURCE_NAMES = (
    "node_modules/",
    "test-results/",
    "playwright-report/",
    "docs/governance/",
)


def fail(message):
    print(f"verify_release_artifacts: {message}", file=sys.stderr)
    return 1


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def parse_go_metadata(go_bin, binary):
    output = subprocess.check_output([go_bin, "version", "-m", str(binary)], text=True)
    lines = output.splitlines()
    first = lines[0]
    match = re.match(r"^.*?:\s*(go[0-9][^\s]*)$", first)
    if not match:
        raise SystemExit(f"cannot parse go version -m output: {first!r}")
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


def archive_members(path):
    if path.suffix == ".zip":
        with zipfile.ZipFile(path) as zf:
            return zf.namelist()
    with tarfile.open(path, "r:gz") as tf:
        return tf.getnames()


def extract_member(path, member, destination):
    if path.suffix == ".zip":
        with zipfile.ZipFile(path) as zf:
            destination.write_bytes(zf.read(member))
    else:
        with tarfile.open(path, "r:gz") as tf:
            handle = tf.extractfile(member)
            if handle is None:
                raise SystemExit(f"{member} is not a regular file in {path.name}")
            destination.write_bytes(handle.read())


def verify_platform_archive(dist, version, platform, manifest, expected_go, go_bin):
    source, staged, kind = PLATFORMS[platform]
    archive = dist / f"derive-world_{version}_{platform}.{kind}"
    if not archive.is_file():
        return fail(f"missing archive {archive.name}")
    prefix = f"derive-world_{version}_{platform}/"
    members = set(archive_members(archive))
    required = {
        prefix + staged,
        prefix + "LICENSE",
        prefix + "NOTICE",
        prefix + "web/dist/index.html",
        prefix + "docs/en/release-notes.md",
        prefix + "go.mod",
    }
    missing = sorted(required - members)
    if missing:
        return fail(f"{archive.name} is missing required members: {missing}")
    if any(name.startswith(prefix + ".env") and name != prefix + ".env.example" for name in members):
        return fail(f"{archive.name} contains a .env file")

    binary_meta = next((item for item in manifest["binaries"] if item["name"] == source), None)
    if binary_meta is None:
        return fail(f"manifest has no binary entry for {source}")
    with tempfile.TemporaryDirectory() as tmp:
        extracted = pathlib.Path(tmp) / staged
        extract_member(archive, prefix + staged, extracted)
        if sha256(extracted) != binary_meta["sha256"]:
            return fail(f"{archive.name} embeds a binary whose hash does not match the manifest")
        metadata = parse_go_metadata(go_bin, extracted)
        if metadata["go_version"] != expected_go:
            return fail(f"{archive.name} embeds {metadata['go_version']}, expected {expected_go}")
        goos, goarch = EXPECTED_TARGETS[platform]
        if metadata["goos"] != goos or metadata["goarch"] != goarch:
            return fail(
                f"{archive.name} is for {metadata['goos']}/{metadata['goarch']}, expected {goos}/{goarch}"
            )
        if metadata["cgo_enabled"] != "0" or not metadata["trimpath"]:
            return fail(f"{archive.name} was not built with CGO_ENABLED=0 and -trimpath")
        if (
            metadata["goos"] != binary_meta.get("goos")
            or metadata["goarch"] != binary_meta.get("goarch")
            or metadata["cgo_enabled"] != binary_meta.get("cgo_enabled")
            or metadata["trimpath"] != binary_meta.get("trimpath")
        ):
            return fail(f"{archive.name} metadata does not match the manifest")
    return 0


def verify_source_archive(dist, version, manifest):
    archive = dist / f"derive-world_{version}_source.tar.gz"
    if not archive.is_file():
        return fail("source candidate archive is missing")
    members = archive_members(archive)
    prefix = f"derive-world_{version}/"
    required = {
        prefix + "go.mod",
        prefix + "LICENSE",
        prefix + "NOTICE",
        prefix + "web/src",
        prefix + "docs/en/release-notes.md",
        prefix + ".github/workflows/release.yml",
    }
    present = set()
    for name in members:
        present.add(name)
        present.add(name.rstrip("/"))
    missing = sorted(required - present)
    if missing:
        return fail(f"source archive is missing required members: {missing}")
    for name in members:
        normalized = name[len(prefix):] if name.startswith(prefix) else name
        if normalized == ".env" or (normalized.startswith(".env.") and normalized != ".env.example"):
            return fail(f"source archive contains an environment file {normalized!r}")
        for forbidden in FORBIDDEN_SOURCE_NAMES:
            if normalized == forbidden or normalized.startswith(forbidden):
                return fail(f"source archive contains forbidden path {normalized!r}")
        if normalized.endswith(".storageState.json"):
            return fail(f"source archive contains session state {normalized!r}")
    return 0


def verify_attestation(subjects, args):
    if not args.repository:
        return fail("attestation verification requires --repository")
    verifier = ["gh", "attestation", "verify"]
    try:
        subprocess.check_output([*verifier, "--help"], text=True, stderr=subprocess.STDOUT)
    except (OSError, subprocess.CalledProcessError):
        return fail("gh attestation verify is unavailable")

    signer = args.signer_workflow or f"{args.repository}/.github/workflows/release.yml"
    for archive in subjects:
        command = [
            *verifier,
            str(archive),
            "--repo",
            args.repository,
            "--source-digest",
            args.commit,
            "--signer-workflow",
            signer,
            "--deny-self-hosted-runners",
        ]
        # The tag-to-commit binding is enforced by the release preflight and
        # recorded in the manifest. The attestation is bound by source digest,
        # repository, and signer workflow; --source-ref is intentionally not
        # asserted because a workflow_dispatch tag run carries the branch ref.
        try:
            subprocess.check_output(command, text=True, stderr=subprocess.STDOUT)
        except subprocess.CalledProcessError as exc:
            return fail(f"attestation verification failed for {archive.name}: {exc.output.strip()}")
    print(f"attestation verified for {len(subjects)} subjects")
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dist", required=True)
    parser.add_argument("--mode", required=True, choices=("dry-run", "tag"))
    parser.add_argument("--repository", required=True)
    parser.add_argument("--tag", default="")
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--tree", required=True)
    parser.add_argument("--expected-go", default="go1.27.3")
    parser.add_argument("--expected-node", default="24.14.0")
    parser.add_argument("--require-attestation", action="store_true")
    parser.add_argument("--signer-workflow", default="")
    parser.add_argument("--go", default="go", help="go binary used for metadata checks")
    args = parser.parse_args()

    dist = pathlib.Path(args.dist).resolve()
    manifest_path = dist / "BUILD-MANIFEST.json"
    if not manifest_path.is_file():
        return fail("BUILD-MANIFEST.json is missing")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))

    expected = {
        "schema": 1,
        "mode": args.mode,
        "repository": args.repository,
        "version": args.version,
        "commit": args.commit,
        "tree": args.tree,
    }
    for key, value in expected.items():
        if manifest.get(key) != value:
            return fail(f"manifest {key} is {manifest.get(key)!r}, expected {value!r}")
    if args.mode == "tag":
        if manifest.get("tag") != args.tag:
            return fail(f"manifest tag is {manifest.get('tag')!r}, expected {args.tag!r}")
        if args.version != args.tag[1:]:
            return fail(f"version {args.version!r} does not match tag {args.tag!r}")
        if manifest.get("release", {}).get("draft_target") is not True:
            return fail("tag-mode manifest must target a draft release")
        if not re.fullmatch(r"[0-9a-f]{40}", manifest.get("tag_object_sha") or ""):
            return fail("tag-mode manifest must record the resolved tag object SHA")
        if manifest.get("tag_commit_sha") != args.commit:
            return fail("tag-mode manifest tag commit does not match the build commit")
        if not re.fullmatch(r"[0-9a-f]{64}", manifest.get("authorization_sha256") or ""):
            return fail("tag-mode manifest must bind the G08 authorization record hash")
    else:
        if manifest.get("tag") is not None:
            return fail("dry-run manifest must not claim a tag")
        if manifest.get("release", {}).get("draft_target") is not False:
            return fail("dry-run manifest must not target a draft release")
    if manifest.get("release", {}).get("published") is not False:
        return fail("manifest must record published=false")
    for key in ("workflow_run", "ci_run", "security_run", "e2e_run"):
        if not isinstance(manifest.get(key), str) or not manifest[key].startswith("https://github.com/"):
            return fail(f"manifest {key} is not a GitHub URL")
    if not str(manifest.get("build_started_at", "")).strip():
        return fail("manifest must record build_started_at")

    toolchain = manifest.get("toolchain", {})
    if toolchain.get("go_version") != args.expected_go:
        return fail(f"manifest Go version is {toolchain.get('go_version')!r}")
    if not str(toolchain.get("go", "")).startswith("go version " + args.expected_go):
        return fail(f"manifest go line is {toolchain.get('go')!r}")
    if toolchain.get("node") != "v" + args.expected_node:
        return fail(f"manifest Node version is {toolchain.get('node')!r}")

    sums_path = dist / "SHA256SUMS"
    if not sums_path.is_file():
        return fail("SHA256SUMS is missing")
    sums = {}
    for line in sums_path.read_text(encoding="utf-8").splitlines():
        if line.strip():
            digest, name = line.split("  ", 1)
            sums[name] = digest

    archives = sorted(list(dist.glob("derive-world_*.tar.gz")) + list(dist.glob("derive-world_*.zip")))
    manifest_artifacts = {item["name"]: item for item in manifest.get("artifacts", [])}
    if {path.name for path in archives} != set(sums) or {path.name for path in archives} != set(manifest_artifacts):
        return fail("archive sets disagree between the directory, SHA256SUMS, and the manifest")
    for archive in archives:
        digest = sha256(archive)
        if sums[archive.name] != digest or manifest_artifacts[archive.name]["sha256"] != digest:
            return fail(f"hash mismatch for {archive.name}")
    if manifest.get("sha256sums", {}).get("sha256") != sha256(sums_path):
        return fail("manifest sha256sums digest does not match SHA256SUMS")

    for platform in PLATFORMS:
        code = verify_platform_archive(dist, args.version, platform, manifest, args.expected_go, args.go)
        if code:
            return code
    code = verify_source_archive(dist, args.version, manifest)
    if code:
        return code

    record_path = dist / "ATTESTATION.json"
    if not record_path.is_file():
        return fail("ATTESTATION.json is missing")
    record = json.loads(record_path.read_text(encoding="utf-8"))
    if record.get("schema") != 1 or record.get("commit") != args.commit:
        return fail("ATTESTATION.json does not bind this commit")
    if args.mode == "tag" and record.get("tag") != args.tag:
        return fail("ATTESTATION.json does not bind this tag")
    if record.get("manifest_sha256") != sha256(manifest_path):
        return fail("ATTESTATION.json manifest hash does not match BUILD-MANIFEST.json")
    if record.get("sha256sums_sha256") != sha256(sums_path):
        return fail("ATTESTATION.json SHA256SUMS hash does not match")

    expected_subjects = {path.name for path in archives} | {"BUILD-MANIFEST.json", "SHA256SUMS"}
    subject_items = record.get("subjects", [])
    subject_names = {item.get("name") for item in subject_items}
    if subject_names != expected_subjects:
        return fail(f"attestation subjects {sorted(subject_names)} do not match {sorted(expected_subjects)}")
    for item in subject_items:
        subject = dist / item["name"]
        if not subject.is_file() or sha256(subject) != item.get("sha256"):
            return fail(f"attestation subject hash mismatch for {item['name']}")

    if args.mode == "dry-run":
        if record.get("status") not in ("not_applicable_dry_run", "unavailable_private_repo_without_ghas"):
            return fail(f"dry-run recorded an invalid attestation status {record.get('status')!r}")
    else:
        if record.get("status") != "generated":
            return fail(f"tag-mode release must record a generated attestation, got {record.get('status')!r}")
        code = verify_attestation([dist / name for name in sorted(expected_subjects)], args)
        if code:
            return code

    print(
        f"release artifacts verified: mode={args.mode} version={args.version} "
        f"commit={args.commit} archives={len(archives)} subjects={len(expected_subjects)}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
