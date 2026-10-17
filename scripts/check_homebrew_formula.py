#!/usr/bin/env python3
"""Check the Homebrew Formula against the latest official stable release."""

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
DEFAULT_FORMULA = ROOT / "homebrew" / "Formula" / "codeaf.rb"
OFFICIAL_REPOSITORY = "Agent-Field/codeaf"
ASSETS = {
    ("macos", "amd64"): "codeaf-darwin-amd64",
    ("macos", "arm64"): "codeaf-darwin-arm64",
    ("linux", "amd64"): "codeaf-linux-amd64",
    ("linux", "arm64"): "codeaf-linux-arm64",
}
VERSION_PATTERN = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
SHA256_PATTERN = re.compile(r"^[0-9a-fA-F]{64}$")
FORMULA_URL_PATTERN = re.compile(
    r"^https://github\.com/Agent-Field/codeaf/releases/download/"
    r"v#\{version\}/(codeaf-(?:darwin|linux)-(?:amd64|arm64))$"
)


ValidationError = ReleaseValidationError


def platform_mappings(source: str) -> dict[tuple[str, str], dict[str, list[str]]]:
    mappings: dict[tuple[str, str], dict[str, list[str]]] = {}
    selectors: list[tuple[int, str]] = []
    selector_pattern = re.compile(r"^(\s*)on_(macos|linux|intel|arm)\s+do\s*$")
    value_pattern = re.compile(r'^\s*(url|sha256)\s+"([^"]+)"\s*$')

    for line_number, line in enumerate(source.splitlines(), start=1):
        stripped = line.strip()
        if stripped == "end":
            indentation = len(line) - len(line.lstrip())
            while selectors and selectors[-1][0] >= indentation:
                selectors.pop()
            continue

        selector = selector_pattern.fullmatch(line)
        if selector:
            indentation = len(selector.group(1))
            selectors.append((indentation, selector.group(2)))
            continue

        value = value_pattern.fullmatch(line)
        if not value:
            continue

        active = [name for _, name in selectors]
        operating_systems = [name for name in active if name in ("macos", "linux")]
        architectures = [name for name in active if name in ("intel", "arm")]
        if len(operating_systems) != 1 or len(architectures) != 1:
            raise ValidationError(
                f"line {line_number}: {value.group(1)} must be inside one OS and one "
                "architecture selector"
            )

        operating_system = operating_systems[0]
        architecture = {"intel": "amd64", "arm": "arm64"}[architectures[0]]
        key = (operating_system, architecture)
        mapping = mappings.setdefault(key, {"url": [], "sha256": []})
        mapping[value.group(1)].append(value.group(2))

    return mappings


def validate(
    formula_path: pathlib.Path,
    release: dict,
    checksums_data: bytes,
) -> str:
    try:
        source = formula_path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ValidationError(
            f"Formula file does not exist or cannot be read: {formula_path}"
        ) from exc

    if not re.search(r"^\s*class Codeaf < Formula\s*$", source, re.MULTILINE):
        raise ValidationError("Formula must declare class Codeaf < Formula")
    if not re.search(r'^\s*desc\s+"[^"]+"\s*$', source, re.MULTILINE):
        raise ValidationError("Formula must declare a non-empty description")
    if not re.search(
        r'^\s*homepage\s+"https://github\.com/Agent-Field/codeaf"\s*$',
        source,
        re.MULTILINE,
    ):
        raise ValidationError(
            "Formula homepage must be the official codeaf repository"
        )
    if not re.search(r'^\s*license\s+"Apache-2\.0"\s*$', source, re.MULTILINE):
        raise ValidationError("Formula must declare the Apache-2.0 license")

    versions = re.findall(r'^\s*version\s+"([^"]+)"\s*$', source, re.MULTILINE)
    if len(versions) != 1 or not VERSION_PATTERN.fullmatch(versions[0]):
        raise ValidationError("Formula must declare exactly one stable x.y.z version")
    version = versions[0]

    try:
        release_version, assets, official_checksums = validate_release_manifest(
            release, checksums_data
        )
    except ReleaseValidationError as exc:
        raise ValidationError(str(exc)) from exc

    tag = release.get("tag_name")
    if tag != f"v{version}":
        raise ValidationError(
            f"Formula version {version} is stale or mismatched; "
            f"latest stable release is {tag!r}"
        )
    if release_version != version:
        raise ValidationError(
            f"Formula version {version} does not match release version {release_version}"
        )

    mappings = platform_mappings(source)
    if set(mappings) != set(ASSETS):
        expected = ", ".join(f"{os}/{arch}" for os, arch in sorted(ASSETS))
        actual = ", ".join(f"{os}/{arch}" for os, arch in sorted(mappings)) or "none"
        raise ValidationError(
            f"Formula platform mappings must be exactly {expected}; found {actual}"
        )

    for platform, asset_name in ASSETS.items():
        mapping = mappings[platform]
        for field in ("url", "sha256"):
            if len(mapping[field]) != 1:
                raise ValidationError(
                    f"Formula must declare exactly one {field} for "
                    f"{platform[0]}/{platform[1]}"
                )

        url = mapping["url"][0]
        match = FORMULA_URL_PATTERN.fullmatch(url)
        if match is None or match.group(1) != asset_name:
            raise ValidationError(
                f"Formula URL for {platform[0]}/{platform[1]} must point to the "
                f"official v#{{version}}/{asset_name} release asset"
            )

        checksum = mapping["sha256"][0]
        if not SHA256_PATTERN.fullmatch(checksum):
            raise ValidationError(
                f"Formula SHA-256 for {platform[0]}/{platform[1]} "
                "must contain 64 hex characters"
            )

        asset = assets[asset_name]
        expected_url = (
            f"https://github.com/{OFFICIAL_REPOSITORY}/releases/download/"
            f"v{version}/{asset_name}"
        )
        asset_url = asset.get("browser_download_url")
        if not isinstance(asset_url, str) or asset_url.casefold() != expected_url.casefold():
            raise ValidationError(
                f"GitHub release asset URL is not official for {asset_name}"
            )

        digest = asset.get("digest")
        if not isinstance(digest, str) or not re.fullmatch(
            r"sha256:[0-9a-fA-F]{64}", digest
        ):
            raise ValidationError(
                f"GitHub release has no valid SHA-256 digest for {asset_name}"
            )
        authoritative = digest.removeprefix("sha256:").lower()
        if checksum.lower() != authoritative:
            raise ValidationError(
                f"Formula SHA-256 for {asset_name} does not match "
                "GitHub's asset digest"
            )
        if official_checksums.get(asset_name) != authoritative:
            raise ValidationError(
                f"official checksums.txt does not agree with "
                f"GitHub's digest for {asset_name}"
            )

    if not re.search(r"^\s*def install\s*$", source, re.MULTILINE):
        raise ValidationError("Formula must define install")
    if not re.search(
        r'bin\.install\s+executables\.first\s*=>\s*"codeaf"', source
    ):
        raise ValidationError(
            "Formula must install the downloaded executable as bin/codeaf"
        )
    if not re.search(
        r'raise\s+"[^"]*expected exactly one codeaf release executable', source
    ):
        raise ValidationError(
            "Formula must fail clearly when its raw executable is missing"
        )
    if not re.search(r'chmod\s+0755,\s*bin/"codeaf"', source):
        raise ValidationError("Formula must preserve executable permissions")
    if not re.search(r"^\s*test do\s*$", source, re.MULTILINE) or not re.search(
        r'shell_output\("\#\{bin\}/codeaf --version"\)', source
    ):
        raise ValidationError("Formula test must execute codeaf --version")
    if not re.search(r'assert_match\s+"codeaf "', source):
        raise ValidationError("Formula test must verify the codeaf version output")

    return version


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--formula", type=pathlib.Path, default=DEFAULT_FORMULA)
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

        version = validate(args.formula, release, checksums_data)
    except ValidationError as exc:
        print(f"Homebrew Formula check failed: {exc}", file=sys.stderr)
        return 1

    release_source = "latest stable GitHub release" if is_live else "release fixture"
    print(
        f"Homebrew Formula codeaf {version} matches the {release_source}, "
        f"its official checksums, and all {len(ASSETS)} platform assets."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())