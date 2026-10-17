"""Tests for shared Homebrew release and checksum handling."""

import hashlib
import io
import json
import pathlib
import urllib.error
import unittest
from unittest import mock

from scripts import homebrew_release
from scripts.homebrew_test_support import CHECKSUMS, release_metadata


class FakeResponse:
    def __init__(
        self,
        body: bytes,
        *,
        url: str,
        status: int = 200,
        content_length: str | None = None,
    ):
        self.body = body
        self.url = url
        self.status = status
        self.headers = {}
        if content_length is not None:
            self.headers["Content-Length"] = content_length

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return None

    def getcode(self):
        return self.status

    def geturl(self):
        return self.url

    def read(self, _limit: int) -> bytes:
        return self.body


class HomebrewReleaseTests(unittest.TestCase):
    def test_checksum_parser_accepts_blank_lines_crlf_and_binary_marker(self):
        line = CHECKSUMS.splitlines()[0]
        digest, name = line.split()
        parsed = homebrew_release.parse_checksum_manifest(
            f"\ufeff\r\n{digest}\t*{name}\r\n\r\n".encode()
        )
        self.assertEqual(parsed, {name: digest})

    def test_checksum_parser_rejects_empty_and_blank_manifests(self):
        for data in (b"", b"\r\n \t\r\n"):
            with self.subTest(data=data):
                with self.assertRaisesRegex(
                    homebrew_release.ReleaseValidationError, "empty|no checksum"
                ):
                    homebrew_release.parse_checksum_manifest(data)

    def test_checksum_parser_rejects_html(self):
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError, "HTML response"
        ):
            homebrew_release.parse_checksum_manifest(
                b"  <!DOCTYPE html><html><body>not found</body></html>"
            )

    def test_checksum_parser_rejects_truncated_digest(self):
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError, "expected 64"
        ):
            homebrew_release.parse_checksum_manifest(
                b"abc123  codeaf-darwin-amd64\n"
            )

    def test_checksum_parser_rejects_non_hex_digest(self):
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError, "non-hexadecimal"
        ):
            homebrew_release.parse_checksum_manifest(
                (("z" * 64) + "  codeaf-darwin-amd64\n").encode()
            )

    def test_checksum_parser_rejects_malformed_line(self):
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError, "malformed"
        ):
            homebrew_release.parse_checksum_manifest(
                (("a" * 64) + "  codeaf-darwin-amd64 extra\n").encode()
            )

    def test_checksum_parser_rejects_duplicate_asset(self):
        entry = CHECKSUMS.splitlines()[0]
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError, "more than once"
        ):
            homebrew_release.parse_checksum_manifest(
                f"{entry}\n{entry}\n".encode()
            )

    def test_checksum_parser_rejects_non_utf8(self):
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError, "valid UTF-8"
        ):
            homebrew_release.parse_checksum_manifest(b"\xff")

    def test_checksum_parser_rejects_oversized_response(self):
        data = b"a" * (homebrew_release.MAX_CHECKSUM_BYTES + 1)
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError, "byte limit"
        ):
            homebrew_release.parse_checksum_manifest(data)

    def test_release_manifest_requires_exact_asset_set_and_matching_digests(self):
        release = release_metadata()
        version, assets, checksums = homebrew_release.validate_release_manifest(
            release, CHECKSUMS.encode()
        )
        self.assertEqual(version, "0.8.0")
        self.assertIn("codeaf-darwin-arm64", assets)
        self.assertEqual(len(checksums), 4)

        release["assets"][1]["digest"] = "sha256:" + "0" * 64
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError,
            "does not match GitHub's asset digest",
        ):
            homebrew_release.validate_release_manifest(release, CHECKSUMS.encode())

    def test_release_manifest_rejects_truncated_download(self):
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError,
            "altered or truncated",
        ):
            homebrew_release.validate_release_manifest(
                release_metadata(), CHECKSUMS.encode()[:-1]
            )

    def test_release_manifest_rejects_missing_checksum_entry(self):
        incomplete_manifest = "\n".join(CHECKSUMS.splitlines()[1:]) + "\n"
        release = release_metadata(incomplete_manifest)
        release_asset_names = {asset["name"] for asset in release["assets"]}
        self.assertIn("codeaf-darwin-amd64", release_asset_names)
        checksum_asset = next(
            asset for asset in release["assets"] if asset["name"] == "checksums.txt"
        )
        self.assertEqual(
            checksum_asset["digest"],
            "sha256:" + hashlib.sha256(
                incomplete_manifest.encode("utf-8")
            ).hexdigest(),
        )
        self.assertEqual(
            checksum_asset["size"], len(incomplete_manifest.encode("utf-8"))
        )
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError,
            "missing: codeaf-darwin-amd64",
        ):
            homebrew_release.validate_release_manifest(
                release,
                incomplete_manifest.encode(),
            )

    def test_release_manifest_rejects_unexpected_checksum_entry(self):
        extra_entry = "0" * 64 + "  codeaf-not-a-release-asset"
        manifest = CHECKSUMS.rstrip() + "\n" + extra_entry + "\n"
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError,
            "unexpected: codeaf-not-a-release-asset",
        ):
            homebrew_release.validate_release_manifest(
                release_metadata(manifest),
                manifest.encode(),
            )

    def test_load_release_inputs_requires_both_fixture_paths(self):
        with self.assertRaisesRegex(
            homebrew_release.ReleaseValidationError, "must be supplied together"
        ):
            homebrew_release.load_release_inputs(pathlib.Path("release.json"), None)

    def test_live_loader_fetches_metadata_then_official_manifest(self):
        release = release_metadata()
        metadata = json.dumps(release).encode()
        responses = [
            FakeResponse(metadata, url=homebrew_release.RELEASE_API),
            FakeResponse(
                CHECKSUMS.encode(),
                url=(
                    "https://release-assets.githubusercontent.com/"
                    "codeaf-checksums.txt"
                ),
            ),
        ]
        with mock.patch(
            "scripts.homebrew_release.urllib.request.urlopen",
            side_effect=responses,
        ) as urlopen:
            loaded, checksums, is_live = homebrew_release.load_release_inputs()
        self.assertEqual(loaded, release)
        self.assertEqual(checksums, CHECKSUMS.encode())
        self.assertTrue(is_live)
        self.assertEqual(urlopen.call_count, 2)
        self.assertEqual(
            urlopen.call_args_list[0].args[0].full_url,
            homebrew_release.RELEASE_API,
        )

    def test_live_loader_rejects_redirect_to_untrusted_host(self):
        with mock.patch(
            "scripts.homebrew_release.urllib.request.urlopen",
            return_value=FakeResponse(
                b"{}",
                url="https://attacker.example/release.json",
            ),
        ):
            with self.assertRaisesRegex(
                homebrew_release.ReleaseValidationError, "outside the trusted HTTPS"
            ):
                homebrew_release.load_release_inputs()

    def test_live_loader_reports_http_errors(self):
        error = urllib.error.HTTPError(
            homebrew_release.RELEASE_API, 503, "unavailable", {}, io.BytesIO()
        )
        with mock.patch(
            "scripts.homebrew_release.urllib.request.urlopen", side_effect=error
        ):
            with self.assertRaisesRegex(
                homebrew_release.ReleaseValidationError, "HTTP 503"
            ):
                homebrew_release.load_release_inputs()

    def test_live_loader_rejects_empty_response(self):
        with mock.patch(
            "scripts.homebrew_release.urllib.request.urlopen",
            return_value=FakeResponse(b"", url=homebrew_release.RELEASE_API),
        ):
            with self.assertRaisesRegex(
                homebrew_release.ReleaseValidationError, "response is empty"
            ):
                homebrew_release.load_release_inputs()

    def test_live_loader_reports_timeouts(self):
        with mock.patch(
            "scripts.homebrew_release.urllib.request.urlopen",
            side_effect=TimeoutError("request timed out"),
        ):
            with self.assertRaisesRegex(
                homebrew_release.ReleaseValidationError, "timed out"
            ):
                homebrew_release.load_release_inputs()

    def test_live_loader_rejects_truncated_http_response(self):
        with mock.patch(
            "scripts.homebrew_release.urllib.request.urlopen",
            return_value=FakeResponse(
                b"{}",
                url=homebrew_release.RELEASE_API,
                content_length="20",
            ),
        ):
            with self.assertRaisesRegex(
                homebrew_release.ReleaseValidationError, "response is truncated"
            ):
                homebrew_release.load_release_inputs()

    def test_live_loader_rejects_invalid_json(self):
        with mock.patch(
            "scripts.homebrew_release.urllib.request.urlopen",
            return_value=FakeResponse(
                b"<html>not JSON</html>",
                url=homebrew_release.RELEASE_API,
            ),
        ):
            with self.assertRaisesRegex(
                homebrew_release.ReleaseValidationError, "not valid JSON"
            ):
                homebrew_release.load_release_inputs()


if __name__ == "__main__":
    unittest.main()
