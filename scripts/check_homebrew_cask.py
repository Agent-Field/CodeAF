#!/usr/bin/env python3
"""Check the Homebrew Cask against the latest official stable release."""

from __future__ import annotations

import argparse
import pathlib
import re
import sys

from homebrew_release import (
    ReleaseValidationError,
    load_release_inputs,
    validate_release_manifest,
)

ROOT = pathlib.Path(__file__).resolve().parents[1]
DEFAULT_CASK = ROOT / "homebrew" / "Casks" / "codeaf.rb"
OFFICIAL_REPOSITORY = "Agent-Field/codeaf"
CASK_ASSETS = {
    "arm": "codeaf-darwin-arm64",
    "intel": "codeaf-darwin-amd64",
}
VERSION_PATTERN = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
SHA256_BLOCK_PATTERN = re.compile(
    r'^\s*sha256\s+arm:\s*"([0-9a-fA-F]{64})",\s*\n'
    r'\s*intel:\s*"([0-9a-fA-F]{64})"\s*$',
    re.MULTILINE,
)
ARCH_URL = (
    r'^\s*url\s+"https://github\.com/Agent-Field/codeaf/releases/download/'
    r'v#\{version\}/codeaf-darwin-#\{Hardware::CPU\.arm\? \? '
    r'"arm64" : "amd64"\}"\s*$'
)
ARCH_BINARY = (
    r'^\s*binary\s+"codeaf-darwin-#\{Hardware::CPU\.arm\? \? '
    r'"arm64" : "amd64"\}",\s*target:\s*"codeaf"\s*$'
)


class ValidationError(ReleaseValidationError):
    """An actionable Homebrew Cask validation failure."""


def validate(
    cask_path: pathlib.Path,
    release: dict,
    checksums_data: bytes,
) -> str:
    try:
        source = cask_path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ValidationError(
            f"Cask file does not exist or cannot be read: {cask_path}"
        ) from exc

    try:
        release_version, assets, checksums = validate_release_manifest(
            release, checksums_data
        )
    except ReleaseValidationError as exc:
        raise ValidationError(str(exc)) from exc

    if not re.search(r'^\s*cask\s+"codeaf"\s+do\s*$', source, re.MULTILINE):
        raise ValidationError('Cask must declare cask "codeaf"')
    if not re.search(r'^\s*name\s+"codeaf"\s*$', source, re.MULTILINE):
        raise ValidationError('Cask name must be "codeaf"')
    if not re.search(r'^\s*desc\s+"[^"]+"\s*$', source, re.MULTILINE):
        raise ValidationError("Cask must declare a non-empty description")
    if not re.search(
        r'^\s*homepage\s+"https://github\.com/Agent-Field/codeaf"\s*$',
        source,
        re.MULTILINE,
    ):
        raise ValidationError("Cask homepage must be the official codeaf repository")

    versions = re.findall(r'^\s*version\s+"([^"]+)"\s*$', source, re.MULTILINE)
    if len(versions) != 1 or not VERSION_PATTERN.fullmatch(versions[0]):
        raise ValidationError("Cask must declare exactly one stable x.y.z version")
    version = versions[0]
    if version != release_version:
        raise ValidationError(
            f"Cask version {version} is stale or mismatched; "
            f"latest stable release is v{release_version}"
        )

    sha256_blocks = list(SHA256_BLOCK_PATTERN.finditer(source))
    if len(sha256_blocks) != 1:
        raise ValidationError(
            "Cask must declare one arm and one intel SHA-256 in its sha256 block"
        )
    cask_hashes = dict(zip(("arm", "intel"), sha256_blocks[0].groups()))
    for architecture, asset_name in CASK_ASSETS.items():
        actual_checksum = cask_hashes[architecture].lower()
        if actual_checksum != checksums[asset_name]:
            raise ValidationError(
                f"Cask {architecture} SHA-256 for {asset_name} does not match "
                "the official release checksums"
            )
        if asset_name not in assets:
            raise ValidationError(f"GitHub release is missing required asset {asset_name}")

    if len(re.findall(ARCH_URL, source, re.MULTILINE)) != 1:
        raise ValidationError(
            "Cask URL must select the official macOS arm64 or amd64 asset"
        )
    if len(re.findall(ARCH_BINARY, source, re.MULTILINE)) != 1:
        raise ValidationError(
            'Cask must link the architecture-specific executable as "codeaf"'
        )
    if not re.search(
        r"^\s*postflight\s+do\s*$[\s\S]*?"
        r"^\s*set_permissions\s+0755,\s*staged_path\s*/\s*"
        r'"codeaf-darwin-\*"\s*$[\s\S]*?^\s*end\s*$',
        source,
        re.MULTILINE,
    ):
        raise ValidationError(
            "Cask must make the downloaded raw executable runnable in postflight"
        )
    if not re.search(
        r"^\s*livecheck\s+do\s*$[\s\S]*?"
        r"^\s*strategy\s+:github_latest\s*$[\s\S]*?^\s*end\s*$",
        source,
        re.MULTILINE,
    ):
        raise ValidationError("Cask must track the latest stable GitHub release")
    if not re.search(r"^\s*test\s+do\s*$", source, re.MULTILINE):
        raise ValidationError("Cask must define a test block")
    if not re.search(
        r'shell_output\("\#\{bin\}/codeaf --version"\)', source
    ) or not re.search(r'assert_match\s+"codeaf "', source):
        raise ValidationError("Cask test must verify codeaf --version output")

    return version


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cask", type=pathlib.Path, default=DEFAULT_CASK)
    parser.add_argument(
        "--release-json",
        type=pathlib.Path,
        help="use supplied GitHub release JSON instead of the live latest-release API",
    )
    parser.add_argument(
        "--checksums-file",
        type=pathlib.Path,
        help="use a supplied checksums.txt instead of downloading the release manifest",
    )
    args = parser.parse_args()

    try:
        release, checksums_data, is_live = load_release_inputs(
            args.release_json, args.checksums_file
        )
        version = validate(args.cask, release, checksums_data)
    except ValidationError as exc:
        print(f"Homebrew Cask check failed: {exc}", file=sys.stderr)
        return 1

    release_source = "latest stable GitHub release" if is_live else "release fixture"
    print(
        f"Homebrew Cask codeaf {version} matches the {release_source}, "
        "its official checksums, and both macOS architecture assets."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
