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
        # ONE BINARY PER ARM, and their bytes differ. A campaign refuses a plan
        # whose arms share a file, because two arms that are one binary report a
        # dead heat — so a fixture that handed all three the same bytes would be
        # testing a plan nobody is allowed to run.
        self.binary = self.root / "binary"
        self.binary.write_bytes(b"frozen executable")
        self.peers = {}
        for peer in ("pi", "omp"):
            path = self.root / peer
            path.write_bytes(b"frozen executable for " + peer.encode())
            self.peers[peer] = str(path)
        self.args = argparse.Namespace(manifest=str(self.root / "plan.json"),
            arms="aforge,pi,omp", scenarios="data-tally,revision-midwork", repeats=3,
            cap=30, seed=12, id="test", aforge=str(self.binary), bin=[],
            model=campaign.MODEL, effort="low", max_cost=0, condition="")

    def plan(self):
        with patch.object(campaign.shutil, "which", side_effect=lambda name: self.peers.get(name)):
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

    def test_the_default_battery_is_the_six_calibration_slices(self):
        # An added scenario must not join the battery by existing: every earlier
        # campaign planned the six, and a seventh appearing on its own would
        # change what "the default" measured without anybody choosing it.
        self.assertEqual(campaign.CALIBRATION_SCENARIOS,
                         {"data-tally", "research-brief", "writing-memo", "code-fix",
                          "followup-while-working", "revision-midwork"})
        parser = campaign.build_parser()
        planned = parser.parse_args(["plan", "manifest.json", "--id", "x"]).scenarios.split(",")
        self.assertCountEqual(planned, campaign.CALIBRATION_SCENARIOS)
        for scenario in campaign.EXTRA_SCENARIOS:
            self.assertNotIn(scenario, planned)

    def test_an_extra_scenario_can_be_planned_when_it_is_asked_for(self):
        self.args.scenarios = "multi-defect-pipeline"
        manifest = self.plan()
        self.assertEqual({row["scenario"] for row in manifest["schedule"]},
                         {"multi-defect-pipeline"})
        self.assertEqual(len(manifest["schedule"]), 9)

    # ── two builds of one harness ──────────────────────────────────────────

    def test_a_labelled_arm_without_a_binary_is_refused(self):
        # A label with nothing bound to it would fall back to whatever binary
        # the rig defaults to, which is the other arm's — and the grid would
        # then measure one build twice and report that the branch changed
        # nothing.
        self.args.arms = "aforge@dev,aforge@simplify"
        self.args.bin = [f"aforge@dev={self.binary}"]
        with self.assertRaises(ValueError) as caught:
            self.plan()
        self.assertIn("aforge@simplify", str(caught.exception))

    def test_two_arms_sharing_one_binary_are_refused(self):
        # The easiest mistake on a two-build grid: one worktree not rebuilt, or
        # an install that landed on the path the other arm reads.
        self.args.arms = "aforge@dev,aforge@simplify"
        self.args.bin = [f"aforge@dev={self.binary}", f"aforge@simplify={self.binary}"]
        with self.assertRaises(ValueError) as caught:
            self.plan()
        self.assertIn("sha256", str(caught.exception))

    def test_labelled_arms_are_planned_and_blocked_like_any_other(self):
        other = self.root / "simplify-binary"
        other.write_bytes(b"a different build entirely")
        self.args.arms = "aforge@dev,aforge@simplify"
        self.args.bin = [f"aforge@dev={self.binary}", f"aforge@simplify={other}"]
        self.args.scenarios = "repo-hover-print,repo-wording-print"
        manifest = self.plan()
        self.assertEqual(manifest["expected_arms"], ["aforge@dev", "aforge@simplify"])
        self.assertNotEqual(manifest["binaries"]["aforge@dev"]["sha256"],
                            manifest["binaries"]["aforge@simplify"]["sha256"])
        # Every block still holds exactly one attempt per arm per scenario, so
        # neither build is ever measured against the other's block.
        for block in ("1", "2", "3"):
            for scenario in ("repo-hover-print", "repo-wording-print"):
                self.assertCountEqual(
                    [r["arm"] for r in manifest["schedule"]
                     if r["block_id"] == block and r["scenario"] == scenario],
                    ["aforge@dev", "aforge@simplify"])

    def test_the_condition_carries_the_model_and_the_effort(self):
        # pareto.py refuses to compare blocks whose condition_id differs, so the
        # condition has to name everything that would make two halves of a grid
        # two experiments.
        self.args.model = "moonshotai/kimi-k3"
        manifest = self.plan()
        self.assertIn("moonshotai/kimi-k3", manifest["condition_id"])
        self.assertIn("effort=low", manifest["condition_id"])
        self.assertEqual(manifest["model_pin"], "moonshotai/kimi-k3")

    def test_a_scenario_this_rig_does_not_define_is_refused(self):
        self.args.scenarios = "data-tally,invented-slice"
        with self.assertRaisesRegex(ValueError, "supported comparable scenarios"):
            self.plan()

    def test_manifest_never_overwrites(self):
        self.plan()
        with self.assertRaises(FileExistsError):
            self.plan()

    def test_imported_package_change_is_detected_with_unchanged_entry(self):
        (self.root / 'package.json').write_text('{"name":"fixture"}')
        entry = self.root / 'cli.js'
        entry.write_text('import "./agent.js"')
        implementation = self.root / 'agent.js'
        implementation.write_text('const prompt = "original"')
        self.args.aforge = str(entry)
        self.plan()
        implementation.write_text('const prompt = "changed"')
        with patch.object(campaign.subprocess, 'run') as run:
            with self.assertRaisesRegex(ValueError, 'package changed'):
                campaign.execute(argparse.Namespace(manifest=self.args.manifest, out=str(self.root/'out')))
            run.assert_not_called()

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
