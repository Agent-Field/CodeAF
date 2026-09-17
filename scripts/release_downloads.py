#!/usr/bin/env python3
"""Report release download counts to PostHog.

The numbers come from public GitHub release data: the GitHub API exposes a
running download count for every asset on a release, and this script reads
that count and sends one PostHog event per binary asset so the download
trend is visible next to the other codeaf telemetry. It runs daily from
.github/workflows/release_downloads.yml, and runs locally too:

    python3 scripts/release_downloads.py --dry-run

Nothing is inferred and nothing about the requester is known: an asset's
download count is public, the event carries the release tag, the asset's
platform, and that count. Only assets named codeaf-<os>-<arch>[.exe] are
reported; sidecars like checksums.txt are not releases of the binary.

An asset is reported once per day even if the run repeats: the event's id is
derived from the tag, the asset and the UTC date, so PostHog's event
deduplication makes a same-day rerun land on the first copy instead of
counting twice.
"""

import argparse
import datetime
import json
import os
import re
import sys
import urllib.request
import uuid

POSTHOG_EVENT = "codeaf:release_downloads"
DISTINCT_ID = "codeaf-release-downloads"
DEFAULT_POSTHOG_HOST = "https://us.i.posthog.com"
GITHUB_API = "https://api.github.com"
NAMESPACE = uuid.NAMESPACE_URL
# codeaf-<os>-<arch>[.exe]. os/arch follow the release.yml build matrix, so
# anything outside this set is a file this script has no opinion about.
ASSET_RE = re.compile(
    r"^codeaf-(?P<os>darwin|linux|windows)-"
    r"(?P<arch>amd64|arm64)(?P<exe>\.exe)?$"
)
# v1.3.0-rc.1 -> channel rc. Stable tags carry no suffix.
SUFFIX_RE = re.compile(r"^[^-]+-([A-Za-z]+)")
PROPS = (
    "release_tag",
    "channel",
    "prerelease",
    "os",
    "arch",
    "download_count",
    "snapshot_date",
)


def release_channel(tag: str) -> str:
    """The channel word of a tag: 'stable', or its prerelease suffix (rc)."""
    match = SUFFIX_RE.match(tag)
    if not match:
        return "stable"
    return match.group(1)


def asset_os_arch(name: str):
    """(os, arch) for an asset this script reports, else None."""
    match = ASSET_RE.match(name)
    if not match:
        return None
    return match.group("os"), match.group("arch")


def event_uuid(tag: str, asset: str, snapshot_date: str) -> str:
    """One id per release, asset and day, so a same-day rerun deduplicates."""
    return str(
        uuid.uuid5(
            NAMESPACE, f"{DISTINCT_ID}|{tag}|{asset}|{snapshot_date}"
        )
    )


def fetch_releases(repository: str, token: str):
    """Every release of the repository, following GitHub's pagination."""
    releases = []
    page = 1
    while True:
        request = urllib.request.Request(
            f"{GITHUB_API}/repos/{repository}/releases"
            f"?per_page=100&page={page}",
            headers={
                "Accept": "application/vnd.github+json",
                "Authorization": f"Bearer {token}",
                "X-GitHub-Api-Version": "2022-11-28",
                "User-Agent": "codeaf-release-downloads",
            },
        )
        with urllib.request.urlopen(request, timeout=30) as response:
            batch = json.load(response)
        if not isinstance(batch, list):
            raise ValueError(f"unexpected GitHub API response on page {page}")
        releases.extend(batch)
        if len(batch) < 100:
            return releases
        page += 1


def build_events(releases, snapshot_date: str):
    """One PostHog event per binary asset, with the properties it carries."""
    events = []
    for release in releases:
        if release.get("draft"):
            continue
        tag = release["tag_name"]
        for asset in release.get("assets") or []:
            name = asset.get("name") or ""
            platform = asset_os_arch(name)
            if platform is None:
                continue
            os_name, arch = platform
            events.append(
                {
                    "event": POSTHOG_EVENT,
                    "uuid": event_uuid(tag, name, snapshot_date),
                    "distinct_id": DISTINCT_ID,
                    "properties": {
                        "release_tag": tag,
                        "channel": release_channel(tag),
                        "prerelease": bool(release.get("prerelease")),
                        "os": os_name,
                        "arch": arch,
                        # The cumulative total the GitHub API reports.
                        "download_count": asset["download_count"],
                        "snapshot_date": snapshot_date,
                        "$process_person_profile": False,
                    },
                }
            )
    return events


def build_payload(events, project_key: str):
    return {"api_key": project_key, "batch": events}


def send(payload, posthog_host: str) -> None:
    body = json.dumps(payload).encode()
    request = urllib.request.Request(
        f"{posthog_host.rstrip('/')}/batch/",
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        response.read()


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="print the batch JSON instead of sending it",
    )
    parser.add_argument(
        "--fixture",
        metavar="PATH",
        help="read the releases API response from a file instead of the network",
    )
    parser.add_argument(
        "--repository",
        default=os.environ.get("GITHUB_REPOSITORY", "Agent-Field/codeaf"),
        help="owner/name of the repository whose releases are read",
    )
    args = parser.parse_args(argv)

    project_key = os.environ.get("CODEAF_POSTHOG_PROJECT_KEY", "")
    if not project_key:
        print(
            "notice: CODEAF_POSTHOG_PROJECT_KEY is not set; nothing sent",
            file=sys.stderr,
        )
        return 0

    if args.fixture:
        with open(args.fixture) as fixture:
            releases = json.load(fixture)
    else:
        token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN", "")
        if not token:
            print(
                "notice: GITHUB_TOKEN is not set; nothing sent",
                file=sys.stderr,
            )
            return 0
        releases = fetch_releases(args.repository, token)

    snapshot_date = datetime.datetime.now(datetime.timezone.utc).strftime(
        "%Y-%m-%d"
    )
    events = build_events(releases, snapshot_date)
    payload = build_payload(events, project_key)

    if args.dry_run:
        print(json.dumps(payload, indent=2, sort_keys=True))
        return 0

    send(payload, os.environ.get("POSTHOG_HOST") or DEFAULT_POSTHOG_HOST)
    print(f"sent {len(events)} events to PostHog")
    return 0


if __name__ == "__main__":
    sys.exit(main())
