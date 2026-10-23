"""Fixtures shared by the offline Homebrew validation tests."""

import hashlib
import json
import pathlib

CHECKSUMS = """\
f926117df3f9d369bfdaa1960060f1d17bec8df6b3fdc1f25f9b6a9fba18cef2  codeaf-darwin-amd64
19b55289b5d8bf3345b5e3fae0682cc75ed0a05f5207f772f9f60c6d303589bf  codeaf-darwin-arm64
1d99c52d4dbba1d62599d820bbd62342240c4585c3577fca959a16ee2a4d8f72  codeaf-linux-amd64
aee2d1b60f81d5b375b7df4db25fea580e8683880074f6896db6212022f26baa  codeaf-linux-arm64
"""

ASSET_NAMES = tuple(
    line.split()[1]
    for line in CHECKSUMS.splitlines()
    if line.strip()
)


def release_metadata(checksums: str = CHECKSUMS) -> dict:
    """Build complete release metadata and attach the supplied manifest bytes."""

    checksum_data = checksums.encode("utf-8")
    canonical_digests = {
        line.split()[1]: line.split()[0]
        for line in CHECKSUMS.splitlines()
        if line.strip()
    }

    assets = [
        {
            "name": "checksums.txt",
            "browser_download_url": (
                "https://github.com/Agent-Field/CodeAF/releases/download/"
                "v0.8.0/checksums.txt"
            ),
            "digest": (
                "sha256:"
                + hashlib.sha256(checksum_data).hexdigest()
            ),
            "size": len(checksum_data),
        }
    ]

    for name in ASSET_NAMES:
        assets.append(
            {
                "name": name,
                "browser_download_url": (
                    "https://github.com/Agent-Field/CodeAF/releases/download/"
                    f"v0.8.0/{name}"
                ),
                "digest": f"sha256:{canonical_digests[name]}",
                "size": 100,
            }
        )

    return {
        "tag_name": "v0.8.0",
        "draft": False,
        "prerelease": False,
        "assets": assets,
    }


def write_release_fixture(
    directory: pathlib.Path,
    checksums: str = CHECKSUMS,
) -> tuple[pathlib.Path, pathlib.Path]:
    """Write mock release metadata and its checksum manifest to disk."""

    directory.mkdir(parents=True, exist_ok=True)

    metadata_path = directory / "release.json"
    manifest_path = directory / "checksums.txt"

    metadata_path.write_text(
        json.dumps(release_metadata(checksums)),
        encoding="utf-8",
    )

    manifest_path.write_bytes(checksums.encode("utf-8"))

    return metadata_path, manifest_path