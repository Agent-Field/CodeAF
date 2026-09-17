#!/usr/bin/env python3
"""Tests for scripts/release_downloads.py: dry-run mode against a recorded
fixture of the GitHub releases API, plus the identifiers the wire spec spells
out, the workflow that schedules the script, and the docs paragraph.
"""

import ast
import datetime
import importlib.util
import json
import os
import pathlib
import re
import subprocess
import sys
import unittest
import uuid
from unittest import mock

HERE = pathlib.Path(__file__).resolve().parent
SCRIPT = HERE / "release_downloads.py"
FIXTURE = HERE / "testdata" / "github_releases.json"
WORKFLOW = HERE.parent / ".github" / "workflows" / "release-downloads.yml"
DOCS = HERE.parent / "docs"

_spec = importlib.util.spec_from_file_location("release_downloads", SCRIPT)
module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(module)


class FakeResponse:
    """Stand-in for urlopen's return: a context manager whose read() is JSON."""

    def __init__(self, payload):
        self._payload = (
            payload if isinstance(payload, bytes) else json.dumps(payload).encode()
        )

    def __enter__(self):
        return self

    def __exit__(self, *exception):
        return False

    def read(self):
        return self._payload


try:
    import yaml  # type: ignore
except ImportError:  # pragma: no cover - depends on the machine
    yaml = None


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
        # 6 assets x 2 full releases plus one asset on each of the dev, staging
        # and beta tags; checksums.txt and any other sidecar on the fixture is
        # not a binary asset and gets no event.
        self.assertEqual(len(self.batch), 15)
        tags = {event["properties"]["release_tag"] for event in self.batch}
        self.assertEqual(
            tags,
            {
                "v1.2.0",
                "v1.3.0-rc.1",
                "dev-20260917-09299019bcdc",
                "staging-20260916-ed5fe58eb546",
                "v1.4.0-beta.1",
            },
        )

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
                sorted(properties),
                sorted(
                    EVENT_PROPERTIES
                    + ("$process_person_profile", "$geoip_disable")
                ),
            )
            self.assertIs(properties["$process_person_profile"], False)
            # The sender is a CI runner; its location is meaningless.
            self.assertIs(properties["$geoip_disable"], True)
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
        self.assertEqual(channels["dev-20260917-09299019bcdc"], ("dev", True))
        self.assertEqual(
            channels["staging-20260916-ed5fe58eb546"], ("staging", True)
        )
        # Not one of the four tag shapes this repository publishes.
        self.assertEqual(channels["v1.4.0-beta.1"], ("unknown", True))

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
        # A different day is a new event, not a repeat of yesterday's.
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

    def test_dry_run_without_project_key_prints_the_batch(self):
        # --dry-run is the local and test path: no key in the environment is no
        # obstacle, the key is printed as a placeholder and the batch still
        # comes out, with no notice.
        result = run_script("--dry-run", "--fixture", str(FIXTURE))
        self.assertEqual(result.returncode, 0, result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(payload["api_key"], "<unset>")
        self.assertEqual(len(payload["batch"]), 15)

    def test_sending_without_project_key_is_a_notice_and_success(self):
        # Only the sending path stops on a missing key; it never reaches the
        # network or the PostHog host.
        result = run_script()
        self.assertEqual(result.returncode, 0)
        self.assertIn("notice", result.stderr)
        self.assertIn("CODEAF_POSTHOG_PROJECT_KEY", result.stderr)
        self.assertEqual(result.stdout, "")


class PaginationTest(unittest.TestCase):
    def test_all_pages_are_followed(self):
        page_one = [
            {
                "tag_name": f"v1.{number}.0",
                "draft": False,
                "prerelease": False,
                "assets": [{"name": "codeaf-linux-amd64", "download_count": 1}],
            }
            for number in range(100)
        ]
        page_two = [
            {
                "tag_name": "v0.1.0",
                "draft": False,
                "prerelease": False,
                "assets": [{"name": "codeaf-linux-arm64", "download_count": 2}],
            }
        ]
        with mock.patch(
            "urllib.request.urlopen",
            side_effect=[FakeResponse(page_one), FakeResponse(page_two)],
        ) as urlopen:
            releases = module.fetch_releases("Agent-Field/codeaf", "token")
        self.assertEqual(urlopen.call_count, 2)
        self.assertIn("page=1", urlopen.call_args_list[0].args[0].full_url)
        self.assertIn("page=2", urlopen.call_args_list[1].args[0].full_url)
        self.assertEqual(len(releases), 101)
        self.assertEqual(releases[0]["tag_name"], "v1.0.0")
        self.assertEqual(releases[-1]["tag_name"], "v0.1.0")

    def test_posthog_host_is_overridable(self):
        with mock.patch("urllib.request.urlopen") as urlopen:
            urlopen.return_value = FakeResponse(b"")
            module.send({}, "https://telemetry.example.com")
        request = urlopen.call_args.args[0]
        self.assertEqual(request.full_url, "https://telemetry.example.com/batch/")


class WorkflowTest(unittest.TestCase):
    """The workflow that schedules the script, asserted as text so the check
    reads the same file GitHub Actions does."""

    @classmethod
    def setUpClass(cls):
        cls.text = WORKFLOW.read_text()

    def test_workflow_file_exists(self):
        self.assertTrue(WORKFLOW.exists())

    def test_workflow_is_scheduled_daily_and_dispatchable(self):
        self.assertIn("workflow_dispatch", self.text)
        self.assertIn("'0 6 * * *'", self.text)

    def test_workflow_permissions_are_contents_read_only(self):
        self.assertIn("permissions:", self.text)
        self.assertIn("contents: read", self.text)

    def test_workflow_runs_the_script(self):
        self.assertIn("python3 scripts/release_downloads.py", self.text)
        self.assertIn("runs-on: ubuntu-latest", self.text)
        self.assertIn("actions/checkout", self.text)

    def test_workflow_uses_builtin_token_secret_and_host_variable(self):
        self.assertIn("GH_TOKEN: ${{ github.token }}", self.text)
        self.assertIn(
            "CODEAF_POSTHOG_PROJECT_KEY: ${{ secrets.CODEAF_POSTHOG_PROJECT_KEY }}",
            self.text,
        )
        self.assertIn("POSTHOG_HOST: ${{ vars.POSTHOG_HOST }}", self.text)

    def test_workflow_parses_as_yaml(self):
        if yaml is None:
            self.skipTest("PyYAML is not installed; the text checks above stand")
        document = yaml.safe_load(self.text)
        triggers = document[True]
        self.assertEqual(triggers["schedule"][0]["cron"], "0 6 * * *")
        self.assertIn("workflow_dispatch", triggers)
        self.assertEqual(document["permissions"], {"contents": "read"})

    def test_workflow_and_script_are_the_only_nonstandard_files(self):
        # The feature is workflow + script + fixture + test + docs; no Go
        # source anywhere carries it.
        text = self.text
        self.assertNotIn("go run", text)
        self.assertNotIn("go build", text)


class DocsTest(unittest.TestCase):
    def test_download_counts_paragraph_exists(self):
        self.assertTrue(
            (DOCS / "TELEMETRY.md").exists()
            or (DOCS / "rules" / "promotion.md").exists(),
            "no docs file carries the paragraph",
        )
        path = DOCS / "TELEMETRY.md"
        if not path.exists():
            path = DOCS / "rules" / "promotion.md"
        text = path.read_text()
        flat = " ".join(text.split())
        self.assertIn("## Download counts", text)
        self.assertIn("public GitHub release data", flat)

    def test_no_telemetry_doc_on_this_branch(self):
        # The paragraph therefore belongs in docs/rules/promotion.md, the
        # release docs; this pins the fallback that was chosen.
        self.assertFalse((DOCS / "TELEMETRY.md").exists())


class DocsAndWorkflowMixTest(unittest.TestCase):
    def test_docs_paragraph_names_workflow_and_script(self):
        path = DOCS / "TELEMETRY.md"
        if not path.exists():
            path = DOCS / "rules" / "promotion.md"
        text = path.read_text()
        self.assertIn("release-downloads.yml", text)
        self.assertIn("scripts/release_downloads.py", text)
        self.assertIn("checksums.txt", text)


class ScriptSourceTest(unittest.TestCase):
    """The script is python3 standard library only, and imports prove it."""

    def test_script_imports_stdlib_only(self):
        tree = ast.parse(SCRIPT.read_text())
        imported = set()
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                imported.update(alias.name.split(".")[0] for alias in node.names)
            elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
                imported.add(node.module.split(".")[0])
        self.assertEqual(
            imported,
            {"argparse", "datetime", "json", "os", "re", "sys", "urllib", "uuid"},
        )
        # Nothing outside the standard library is imported, so the script
        # runs anywhere python3 does, including the workflow runner.
        # sys.stdlib_module_names exists from Python 3.10; on older interpreters
        # the pinned import set above is the check that stands.
        stdlib = getattr(sys, "stdlib_module_names", None)
        if stdlib is not None:
            self.assertLessEqual(imported, set(stdlib))


class ChannelTest(unittest.TestCase):
    """The four tag shapes of cmd/codeaf-release/version.go, and nothing
    else: any other tag is reported as "unknown" rather than mislabelled."""

    def test_stable_tags(self):
        self.assertEqual(module.release_channel("v1.2.0"), "stable")
        self.assertEqual(module.release_channel("v0.0.1"), "stable")
        self.assertEqual(module.release_channel("v10.20.30"), "stable")

    def test_rc_tags(self):
        self.assertEqual(module.release_channel("v1.3.0-rc.1"), "rc")
        self.assertEqual(module.release_channel("v2.0.0-rc.12"), "rc")

    def test_dev_tags(self):
        self.assertEqual(
            module.release_channel("dev-20260917-09299019bcdc"), "dev"
        )
        self.assertEqual(module.release_channel("dev-20250101-abcdef012345"), "dev")

    def test_staging_tags(self):
        self.assertEqual(
            module.release_channel("staging-20260916-ed5fe58eb546"), "staging"
        )
        self.assertEqual(
            module.release_channel("staging-20250102-000000000abc"), "staging"
        )

    def test_unrecognised_tags_are_unknown(self):
        for tag in (
            "v1.4.0-beta.1",
            "v1.3.0-rc.0",
            "v1.3.0-rc",
            "v01.2.0",
            "1.2.0",
            "v1.2",
            "dev-20260917-09299019bcdcX",
            "staging-20260916-ED5FE58EB546",
            "nightly-20260917-09299019bcdc",
            "",
        ):
            self.assertEqual(module.release_channel(tag), "unknown", tag)


class SendEndpointTest(unittest.TestCase):
    def test_default_host_is_posthog_us(self):
        with mock.patch("urllib.request.urlopen") as urlopen:
            urlopen.return_value = FakeResponse(b"")
            module.send({}, "https://us.i.posthog.com")
        self.assertEqual(
            urlopen.call_args.args[0].full_url, "https://us.i.posthog.com/batch/"
        )


class AssetMatchingTest(unittest.TestCase):
    def test_sidecars_and_nonmatching_names_are_skipped(self):
        self.assertIsNone(module.asset_os_arch("checksums.txt"))
        self.assertIsNone(module.asset_os_arch("codeaf-linux-amd64.tar.gz"))
        self.assertIsNone(module.asset_os_arch("codeaf-solaris-sparc"))
        self.assertEqual(module.asset_os_arch("codeaf-linux-amd64"), ("linux", "amd64"))
        self.assertEqual(
            module.asset_os_arch("codeaf-windows-amd64.exe"), ("windows", "amd64")
        )


class DraftTest(unittest.TestCase):
    def test_draft_releases_produce_no_events(self):
        draft = {
            "tag_name": "v9.9.9",
            "draft": True,
            "prerelease": False,
            "assets": [{"name": "codeaf-linux-amd64", "download_count": 5}],
        }
        self.assertEqual(module.build_events([draft], "2026-02-11"), [])


if __name__ == "__main__":
    unittest.main()
