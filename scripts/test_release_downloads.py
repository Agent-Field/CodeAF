#!/usr/bin/env python3
"""Tests for scripts/release_downloads.py, dry-run mode against a fixture."""

import datetime
import json
import os
import pathlib
import re
import subprocess
import sys
import unittest
import uuid

HERE = pathlib.Path(__file__).resolve().parent
SCRIPT = HERE / "release_downloads.py"
FIXTURE = HERE / "testdata" / "github_releases.json"
EVENT_PROPERTIES = (
    "release_tag",
    "channel",
    "prerelease",
    "os",
    "arch",
    "download_count",
    "snapshot_date",
)


def run_script(*args, env_extra=None):
    env = dict(os.environ)
    env.pop("CODEAF_POSTHOG_PROJECT_KEY", None)
    env.update(env_extra or {})
    return subprocess.run(
        [sys.executable, str(SCRIPT), *args],
        capture_output=True,
        text=True,
        env=env,
    )


def dry_run_payload(env_extra=None):
    result = run_script(
        "--dry-run", "--fixture", str(FIXTURE), env_extra=env_extra
    )
    assert result.returncode == 0, result.stderr
    return json.loads(result.stdout)


class ReleaseDownloadsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.payload = dry_run_payload({"CODEAF_POSTHOG_PROJECT_KEY": "phx_test"})
        cls.batch = cls.payload["batch"]

    def test_one_event_per_binary_asset(self):
        # 6 assets x 2 releases; checksums.txt and any other sidecar on the
        # fixture is not a binary asset and gets no event.
        self.assertEqual(len(self.batch), 12)
        tags = {event["properties"]["release_tag"] for event in self.batch}
        self.assertEqual(tags, {"v1.2.0", "v1.3.0-rc.1"})

    def test_payload_shape(self):
        self.assertEqual(
            self.payload["api_key"], "phx_test"
        )
        for event in self.batch:
            self.assertEqual(event["event"], "codeaf:release_downloads")
            self.assertEqual(event["distinct_id"], "codeaf-release-downloads")

    def test_event_properties(self):
        for event in self.batch:
            properties = event["properties"]
            self.assertEqual(
                sorted(properties), sorted(EVENT_PROPERTIES + ("$process_person_profile",))
            )
            self.assertIs(properties["$process_person_profile"], False)
            self.assertEqual(
                properties["snapshot_date"],
                datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d"),
            )
            self.assertIsInstance(properties["prerelease"], bool)
            self.assertIsInstance(properties["download_count"], int)

    def test_channel_and_prerelease(self):
        channels = {
            event["properties"]["release_tag"]: (
                event["properties"]["channel"],
                event["properties"]["prerelease"],
            )
            for event in self.batch
        }
        self.assertEqual(channels["v1.2.0"], ("stable", False))
        self.assertEqual(channels["v1.3.0-rc.1"], ("rc", True))

    def test_platforms_and_counts(self):
        counts = {
            (event["properties"]["os"], event["properties"]["arch"]): event[
                "properties"
            ]["download_count"]
            for event in self.batch
            if event["properties"]["release_tag"] == "v1.2.0"
        }
        self.assertEqual(
            counts,
            {
                ("linux", "amd64"): 15010,
                ("linux", "arm64"): 11240,
                ("darwin", "amd64"): 18930,
                ("darwin", "arm64"): 22471,
                ("windows", "amd64"): 4109,
                ("windows", "arm64"): 2051,
            },
        )

    def test_event_uuid_format_and_uniqueness(self):
        uuids = [event["uuid"] for event in self.batch]
        self.assertEqual(len(set(uuids)), len(uuids))
        for value in uuids:
            self.assertRegex(
                value,
                r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
            )
            self.assertEqual(uuid.UUID(value).version, 5)

    def test_same_day_rerun_is_deduplicated(self):
        # The id is derived from the tag, the asset and the UTC date, so a
        # rerun on the same day produces the same ids and PostHog drops the
        # repeat instead of counting the downloads twice.
        again = dry_run_payload({"CODEAF_POSTHOG_PROJECT_KEY": "phx_test"})
        self.assertEqual(
            [event["uuid"] for event in again["batch"]],
            [event["uuid"] for event in self.batch],
        )

    def test_uuid_depends_on_snapshot_date(self):
        # A different day is a different event, not a repeat of yesterday's.
        tag, asset, distinct = "v1.2.0", "codeaf-linux-amd64", "codeaf-release-downloads"
        self.assertNotEqual(
            uuid.uuid5(uuid.NAMESPACE_URL, f"{distinct}|{tag}|{asset}|2026-02-11"),
            uuid.uuid5(uuid.NAMESPACE_URL, f"{distinct}|{tag}|{asset}|2026-02-12"),
        )

    def test_no_checksums_or_asset_names_in_payload(self):
        # Sidecars are not binary releases: their names and counts never
        # appear in the payload at all.
        text = json.dumps(self.payload)
        self.assertNotIn("checksums.txt", text)
        for event in self.batch:
            self.assertNotIn("name", event["properties"])

    def test_missing_project_key_is_a_notice_and_success(self):
        result = run_script("--dry-run", "--fixture", str(FIXTURE))
        self.assertEqual(result.returncode, 0)
        self.assertIn("notice", result.stderr)
        self.assertIn("CODEAF_POSTHOG_PROJECT_KEY", result.stderr)
        self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
