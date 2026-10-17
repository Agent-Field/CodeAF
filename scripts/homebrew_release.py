#!/usr/bin/env python3
"""Shared release and checksum verification for the Homebrew packages."""

from __future__ import annotations

import hashlib
import json
import pathlib
import re
import socket
import urllib.error
import urllib.parse
import urllib.request

RELEASE_API = "https://api.github.com/repos/Agent-Field/codeaf/releases/latest"
REPOSITORY = "Agent-Field/codeaf"
REQUEST_TIMEOUT_SECONDS = 20
MAX_RELEASE_BYTES = 2 * 1024 * 1024
MAX_CHECKSUM_BYTES = 64 * 1024
VERSION_PATTERN = re.compile(r"^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
SHA256_PATTERN = re.compile(r"^[0-9a-fA-F]{64}$")
ASSET_SHA256_PATTERN = re.compile(r"^sha256:([0-9a-fA-F]{64})$")
CHECKSUM_LINE_PATTERN = re.compile(
    r"^([0-9a-fA-F]+)[ \t]+(\*?)([A-Za-z0-9][A-Za-z0-9._-]*)$"
)


class ReleaseValidationError(ValueError):
    """A release, manifest, or download could not be verified."""


def _read_url(
    url: str,
    *,
    purpose: str,
    max_bytes: int,
    allowed_hosts: tuple[str, ...],
) -> bytes:
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/vnd.github+json, text/plain",
            "User-Agent": "codeaf-homebrew-validation",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=REQUEST_TIMEOUT_SECONDS) as response:
            status = response.getcode()
            if status != 200:
                raise ReleaseValidationError(
                    f"{purpose} download returned HTTP {status}: {url}"
                )

            final_url = urllib.parse.urlparse(response.geturl())
            final_host = (final_url.hostname or "").lower()
            if final_url.scheme != "https" or not any(
                final_host == host
                or (
                    host == "githubusercontent.com"
                    and final_host.endswith(".githubusercontent.com")
                )
                for host in allowed_hosts
            ):
                raise ReleaseValidationError(
                    f"{purpose} download redirected outside the trusted HTTPS hosts"
                )

            data = response.read(max_bytes + 1)
            if len(data) > max_bytes:
                raise ReleaseValidationError(
                    f"{purpose} response exceeds the {max_bytes}-byte limit"
                )
            if not data:
                raise ReleaseValidationError(f"{purpose} response is empty")

            content_length = response.headers.get("Content-Length")
            if content_length is not None:
                try:
                    expected_length = int(content_length)
                except ValueError:
                    raise ReleaseValidationError(
                        f"{purpose} response has an invalid Content-Length"
                    ) from None
                if expected_length != len(data):
                    raise ReleaseValidationError(
                        f"{purpose} response is truncated "
                        f"(received {len(data)} of {expected_length} bytes)"
                    )
            return data
    except urllib.error.HTTPError as exc:
        raise ReleaseValidationError(
            f"HTTP {exc.code} while downloading {purpose} from {url}"
        ) from exc
    except (TimeoutError, socket.timeout) as exc:
        raise ReleaseValidationError(
            f"timed out downloading {purpose} from {url}"
        ) from exc
    except urllib.error.URLError as exc:
        if isinstance(exc.reason, (TimeoutError, socket.timeout)):
            message = "timed out"
        else:
            message = f"network error ({exc.reason})"
        raise ReleaseValidationError(f"{message} downloading {purpose} from {url}") from exc
    except OSError as exc:
        raise ReleaseValidationError(
            f"could not read {purpose} from {url}: {exc}"
        ) from exc


def parse_checksum_manifest(data: bytes) -> dict[str, str]:
    if not data:
        raise ReleaseValidationError("checksums.txt is empty")
    if len(data) > MAX_CHECKSUM_BYTES:
        raise ReleaseValidationError(
            f"checksums.txt exceeds the {MAX_CHECKSUM_BYTES}-byte limit"
        )
    try:
        text = data.decode("utf-8-sig")
    except UnicodeDecodeError as exc:
        raise ReleaseValidationError("checksums.txt is not valid UTF-8") from exc

    if text.lstrip().lower().startswith(("<!doctype html", "<html")):
        raise ReleaseValidationError(
            "expected checksums.txt but received an HTML response"
        )

    entries: dict[str, str] = {}
    for line_number, line in enumerate(text.splitlines(), start=1):
        stripped = line.strip()
        if not stripped:
            continue

        match = CHECKSUM_LINE_PATTERN.fullmatch(stripped)
        if match is None:
            fields = stripped.split()
            if fields and len(fields[0]) != 64:
                raise ReleaseValidationError(
                    f"checksums.txt line {line_number} has a SHA-256 value "
                    f"of {len(fields[0])} characters, expected 64"
                )
            if fields and not SHA256_PATTERN.fullmatch(fields[0]):
                raise ReleaseValidationError(
                    f"checksums.txt line {line_number} contains a non-hexadecimal SHA-256"
                )
            raise ReleaseValidationError(
                f"checksums.txt line {line_number} is malformed"
            )

        digest, _binary_marker, name = match.groups()
        if len(digest) != 64:
            raise ReleaseValidationError(
                f"checksums.txt line {line_number} has a SHA-256 value "
                f"of {len(digest)} characters, expected 64"
            )
        if name in entries:
            raise ReleaseValidationError(
                f"checksums.txt lists {name} more than once"
            )
        entries[name] = digest.lower()

    if not entries:
        raise ReleaseValidationError("checksums.txt contains no checksum entries")
    return entries


def _read_release_json(path: pathlib.Path) -> dict:
    try:
        raw = path.read_bytes()
    except OSError as exc:
        raise ReleaseValidationError(
            f"cannot read release metadata {path}: {exc}"
        ) from exc
    if len(raw) > MAX_RELEASE_BYTES:
        raise ReleaseValidationError(
            f"release metadata exceeds the {MAX_RELEASE_BYTES}-byte limit"
        )
    try:
        release = json.loads(raw.decode("utf-8-sig"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ReleaseValidationError(
            f"release metadata {path} is not valid JSON: {exc}"
        ) from exc
    if not isinstance(release, dict):
        raise ReleaseValidationError(
            f"release metadata {path} must contain a JSON object"
        )
    return release


def load_release_inputs(
    release_json: pathlib.Path | None = None,
    checksums_file: pathlib.Path | None = None,
) -> tuple[dict, bytes, bool]:
    """Load live release data or paired offline fixtures; return whether it is live."""
    if (release_json is None) != (checksums_file is None):
        raise ReleaseValidationError(
            "--release-json and --checksums-file must be supplied together"
        )

    if release_json is not None and checksums_file is not None:
        release = _read_release_json(release_json)
        try:
            checksums_data = checksums_file.read_bytes()
        except OSError as exc:
            raise ReleaseValidationError(
                f"cannot read checksums file {checksums_file}: {exc}"
            ) from exc
        return release, checksums_data, False

    release_data = _read_url(
        RELEASE_API,
        purpose="GitHub latest-release metadata",
        max_bytes=MAX_RELEASE_BYTES,
        allowed_hosts=("api.github.com",),
    )
    try:
        release = json.loads(release_data.decode("utf-8-sig"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ReleaseValidationError(
            f"GitHub latest-release response is not valid JSON: {exc}"
        ) from exc
    if not isinstance(release, dict):
        raise ReleaseValidationError(
            "GitHub latest-release response must contain a JSON object"
        )

    tag = release.get("tag_name")
    if not isinstance(tag, str) or VERSION_PATTERN.fullmatch(tag) is None:
        raise ReleaseValidationError(
            f"GitHub latest-release response has an invalid stable tag: {tag!r}"
        )

    assets = release.get("assets")
    if not isinstance(assets, list):
        raise ReleaseValidationError(
            "GitHub latest-release response has no asset list"
        )
    checksum_assets = [
        asset
        for asset in assets
        if isinstance(asset, dict) and asset.get("name") == "checksums.txt"
    ]
    if len(checksum_assets) != 1:
        raise ReleaseValidationError(
            "GitHub latest release must contain exactly one checksums.txt asset"
        )
    checksum_url = checksum_assets[0].get("browser_download_url")
    expected_url = (
        f"https://github.com/{REPOSITORY}/releases/download/{tag}/checksums.txt"
    )
    if (
        not isinstance(checksum_url, str)
        or checksum_url.casefold() != expected_url.casefold()
    ):
        raise ReleaseValidationError(
            "checksums.txt URL is not from the expected official release"
        )

    checksums_data = _read_url(
        checksum_url,
        purpose="official release checksums.txt",
        max_bytes=MAX_CHECKSUM_BYTES,
        allowed_hosts=("github.com", "githubusercontent.com"),
    )
    return release, checksums_data, True


def validate_release_manifest(
    release: dict,
    checksums_data: bytes,
) -> tuple[str, dict[str, dict], dict[str, str]]:
    """Verify release metadata, the manifest asset, and every listed asset digest."""
    tag = release.get("tag_name")
    if not isinstance(tag, str) or VERSION_PATTERN.fullmatch(tag) is None:
        raise ReleaseValidationError(
            f"release tag is not a stable vX.Y.Z version: {tag!r}"
        )
    if release.get("draft") is not False or release.get("prerelease") is not False:
        raise ReleaseValidationError(
            "GitHub release metadata is not a published stable release"
        )

    raw_assets = release.get("assets")
    if not isinstance(raw_assets, list):
        raise ReleaseValidationError("GitHub release metadata has no asset list")

    assets: dict[str, dict] = {}
    for asset in raw_assets:
        if not isinstance(asset, dict) or not isinstance(asset.get("name"), str):
            raise ReleaseValidationError(
                "GitHub release contains an asset with no valid name"
            )
        name = asset["name"]
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", name):
            raise ReleaseValidationError(
                f"GitHub release contains an unsafe asset name: {name!r}"
            )
        if name in assets:
            raise ReleaseValidationError(
                f"GitHub release lists the asset {name} more than once"
            )

        expected_url = (
            f"https://github.com/{REPOSITORY}/releases/download/{tag}/{name}"
        )
        asset_url = asset.get("browser_download_url")
        if (
            not isinstance(asset_url, str)
            or asset_url.casefold() != expected_url.casefold()
        ):
            raise ReleaseValidationError(
                f"GitHub release asset URL is not official for {name}"
            )

        digest = asset.get("digest")
        if not isinstance(digest, str) or ASSET_SHA256_PATTERN.fullmatch(digest) is None:
            raise ReleaseValidationError(
                f"GitHub release has no valid SHA-256 asset digest for {name}"
            )
        size = asset.get("size")
        if type(size) is not int or size <= 0:
            raise ReleaseValidationError(
                f"GitHub release asset {name} has an invalid size"
            )
        assets[name] = asset

    if "checksums.txt" not in assets:
        raise ReleaseValidationError(
            "GitHub release is missing the checksums.txt asset"
        )

    manifest_asset_digest = ASSET_SHA256_PATTERN.fullmatch(
        assets["checksums.txt"]["digest"]
    ).group(1).lower()
    actual_manifest_digest = hashlib.sha256(checksums_data).hexdigest()
    if actual_manifest_digest != manifest_asset_digest:
        raise ReleaseValidationError(
            "downloaded checksums.txt does not match GitHub's asset digest "
            "(response is altered or truncated)"
        )

    expected_names = set(assets) - {"checksums.txt"}
    checksums = parse_checksum_manifest(checksums_data)
    missing = sorted(expected_names - checksums.keys())
    unexpected = sorted(checksums.keys() - expected_names)
    if missing or unexpected:
        details = []
        if missing:
            details.append("missing: " + ", ".join(missing))
        if unexpected:
            details.append("unexpected: " + ", ".join(unexpected))
        raise ReleaseValidationError(
            "checksums.txt asset list does not match the GitHub release ("
            + "; ".join(details)
            + ")"
        )

    for name, digest in checksums.items():
        asset_digest = ASSET_SHA256_PATTERN.fullmatch(assets[name]["digest"]).group(1)
        if digest != asset_digest.lower():
            raise ReleaseValidationError(
                f"checksums.txt SHA-256 for {name} does not match GitHub's asset digest"
            )

    version = tag[1:]
    return version, assets, checksums
