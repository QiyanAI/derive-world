#!/usr/bin/env python3
"""Build deterministic per-platform release archives from a checked-out tree.

This is the single packaging implementation used by the Release workflow and
by local reproduction. It must be run twice into different output directories
by the caller, and the two outputs compared byte-for-byte, before an archive
is accepted as a release artifact.

The source archive (derive-world_<version>_source.tar.gz), when present in the
output directory, is included in SHA256SUMS.
"""

import argparse
import gzip
import hashlib
import io
import os
import pathlib
import shutil
import sys
import tarfile
import time
import zipfile

PAYLOAD = [
    ("README.md", "README.md", 0o644),
    ("README.zh-CN.md", "README.zh-CN.md", 0o644),
    ("LICENSE", "LICENSE", 0o644),
    ("NOTICE", "NOTICE", 0o644),
    ("THIRD_PARTY_NOTICES.md", "THIRD_PARTY_NOTICES.md", 0o644),
    ("SECURITY.md", "SECURITY.md", 0o644),
    ("TRADEMARK.md", "TRADEMARK.md", 0o644),
    ("CONTRIBUTING.md", "CONTRIBUTING.md", 0o644),
    ("CODE_OF_CONDUCT.md", "CODE_OF_CONDUCT.md", 0o644),
    ("CHANGELOG.md", "CHANGELOG.md", 0o644),
    (".env.example", ".env.example", 0o644),
    ("capabilities.yaml", "capabilities.yaml", 0o644),
    ("go.mod", "go.mod", 0o644),
    ("go.sum", "go.sum", 0o644),
    ("package.json", "package.json", 0o644),
    ("package-lock.json", "package-lock.json", 0o644),
    ("api/openapi/openapi.yaml", "api/openapi/openapi.yaml", 0o644),
]

PAYLOAD_DIRS = ("web/dist", "tests/fixtures/model", "docs")

TARGETS = {
    "linux_amd64": ("derive-world_linux_amd64", "derive-world", "tar.gz"),
    "darwin_arm64": ("derive-world_darwin_arm64", "derive-world", "tar.gz"),
    "windows_amd64": ("derive-world_windows_amd64.exe", "derive-world.exe", "zip"),
}


def fail(message):
    print(f"package_release: {message}", file=sys.stderr)
    return 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=".")
    parser.add_argument("--out", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--epoch", required=True, type=int)
    parser.add_argument("--bin-dir", required=True)
    parser.add_argument("--source-archive", default="")
    args = parser.parse_args()

    root = pathlib.Path(args.root).resolve()
    out = pathlib.Path(args.out).resolve()
    bin_dir = pathlib.Path(args.bin_dir).resolve()
    version = args.version
    epoch = args.epoch

    if out.exists():
        shutil.rmtree(out)
    out.mkdir(parents=True)

    stage_root = out / "stage"
    stage_root.mkdir()

    payload = list(PAYLOAD)
    for rel in PAYLOAD_DIRS:
        base = root / rel
        if not base.is_dir():
            return fail(f"required payload directory is missing: {rel}")
        for path in sorted(base.rglob("*")):
            if path.is_file():
                rel_path = str(path.relative_to(root)).replace("\\", "/")
                payload.append((rel_path, rel_path, 0o644))

    def stage(target, source, staged):
        name = f"derive-world_{version}_{target}"
        stage_dir = stage_root / name
        stage_dir.mkdir(parents=True)
        binary = bin_dir / source
        if not binary.is_file():
            raise SystemExit(f"missing built binary: {binary}")
        shutil.copy2(binary, stage_dir / staged)
        os.chmod(stage_dir / staged, 0o755)
        for src, dst, mode in payload:
            dest = stage_dir / dst
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / src, dest)
            os.chmod(dest, mode)
        for item in sorted(stage_dir.rglob("*")):
            os.utime(item, (epoch, epoch))
        os.utime(stage_dir, (epoch, epoch))
        return stage_dir

    def tar_gz(stage_dir, archive):
        raw = io.BytesIO()
        with tarfile.open(fileobj=raw, mode="w", format=tarfile.PAX_FORMAT) as tf:
            for item in sorted(stage_dir.rglob("*")):
                info = tf.gettarinfo(str(item), arcname=str(item.relative_to(stage_root)))
                info.mtime = epoch
                info.uid = info.gid = 0
                info.uname = info.gname = ""
                if item.is_file():
                    with open(item, "rb") as handle:
                        tf.addfile(info, handle)
                else:
                    tf.addfile(info)
        with open(archive, "wb") as handle:
            with gzip.GzipFile(filename="", mode="wb", fileobj=handle, mtime=0, compresslevel=9) as gz:
                gz.write(raw.getvalue())

    def zip_archive(stage_dir, archive):
        date_time = time.gmtime(epoch)[:6]
        with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zf:
            for item in sorted(stage_dir.rglob("*")):
                if not item.is_file():
                    continue
                rel = str(item.relative_to(stage_root)).replace("\\", "/")
                info = zipfile.ZipInfo(rel, date_time=date_time)
                info.compress_type = zipfile.ZIP_DEFLATED
                info.external_attr = (0o755 if os.access(item, os.X_OK) else 0o644) << 16
                info.create_system = 3
                zf.writestr(info, item.read_bytes())

    archives = []
    for target, (source, staged, kind) in TARGETS.items():
        stage_dir = stage(target, source, staged)
        archive = out / f"derive-world_{version}_{target}.{kind}"
        if kind == "zip":
            zip_archive(stage_dir, archive)
        else:
            tar_gz(stage_dir, archive)
        archives.append(archive)

    source_archive = out / f"derive-world_{version}_source.tar.gz"
    if args.source_archive:
        candidate = pathlib.Path(args.source_archive).resolve()
        if not candidate.is_file():
            return fail(f"source archive is missing: {candidate}")
        shutil.copy2(candidate, source_archive)
    if source_archive.exists():
        archives.append(source_archive)

    lines = [
        f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}"
        for path in sorted(archives, key=lambda item: item.name)
    ]
    (out / "SHA256SUMS").write_text("\n".join(lines) + "\n", encoding="utf-8")
    print((out / "SHA256SUMS").read_text(encoding="utf-8"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
