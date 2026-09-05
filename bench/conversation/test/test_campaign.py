"""Counterexamples for scheduling and evidence preservation; no live inference."""
import argparse
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("campaign", Path(__file__).parents[1] / "campaign.py")
campaign = importlib.util.module_from_spec(spec)
spec.loader.exec_module(campaign)


class CampaignTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.binary = self.root / "binary"
        self.binary.write_bytes(b"frozen executable")
        self.args = argparse.Namespace(manifest=str(self.root / "plan.json"),
            arms="aforge,pi,omp", scenarios="data-tally,revision-midwork", repeats=3,
            cap=30, seed=12, id="test", aforge=str(self.binary))

    def plan(self):
        with patch.object(campaign.shutil, "which", return_value=str(self.binary)):
            campaign.plan(self.args)
        return json.loads(Path(self.args.manifest).read_text())

    def test_balanced_reproducible_blocks(self):
        first = self.plan()
        self.args.manifest = str(self.root / "other.json")
        second = self.plan()
        self.assertEqual(first, second)
        self.assertEqual(len(first["schedule"]), 18)
        for block in ("1", "2", "3"):
            for scenario in ("data-tally", "revision-midwork"):
                self.assertCountEqual([r["arm"] for r in first["schedule"]
                    if r["block_id"] == block and r["scenario"] == scenario], ["aforge", "pi", "omp"])

    def test_manifest_never_overwrites(self):
        self.plan()
        with self.assertRaises(FileExistsError):
            self.plan()

    def test_changed_binary_refused_before_execution(self):
        self.plan()
        self.binary.write_bytes(b"different executable")
        with patch.object(campaign.subprocess, "run") as run:
            with self.assertRaisesRegex(ValueError, "binary changed"):
                campaign.execute(argparse.Namespace(manifest=self.args.manifest, out=str(self.root / "out")))
            run.assert_not_called()

    def test_partial_attempt_not_retried(self):
        manifest = self.plan()
        row = manifest["schedule"][0]
        out = self.root / "out"
        (out / ("%03d-%s-%s" % (row["ordinal"], row["scenario"], row["arm"]))).mkdir(parents=True)
        with patch.object(campaign.subprocess, "run") as run:
            with self.assertRaisesRegex(ValueError, "partial cell"):
                campaign.execute(argparse.Namespace(manifest=self.args.manifest, out=str(out)))
            run.assert_not_called()

    def test_infrastructure_failure_is_preserved_and_stops(self):
        self.plan()
        out = self.root / "out"
        with patch.object(campaign.subprocess, "run", return_value=argparse.Namespace(returncode=7)) as run:
            with self.assertRaisesRegex(ValueError, "infrastructure failure"):
                campaign.execute(argparse.Namespace(manifest=self.args.manifest, out=str(out)))
            self.assertEqual(run.call_count, 1)
        rows = [json.loads(x) for x in (out / "results.jsonl").read_text().splitlines()]
        self.assertEqual(rows[0]["verdict"], "skipped")
        self.assertIsNone(rows[0]["cost_usd"])
        self.assertIsNone(rows[0]["wall_s"])


if __name__ == "__main__":
    unittest.main()
