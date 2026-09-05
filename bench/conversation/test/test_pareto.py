#!/usr/bin/env python3
"""Counterexample tests for bench/conversation/lib/pareto.py.

Every test here is a way the old report lied, or a way a naive report would
lie: a wrong answer with tidy infrastructure scored as 90% quality, a missing
wall read as zero seconds, failures dropped from denominators, scenarios
averaged together, unbalanced or duplicated blocks quietly compared, and a
legacy file talked about frontiers it had no pairing to support. Each test
builds the smallest results.jsonl that contains the trap and asserts the
report refuses it, by name, in its stdout.
"""
import json
import os
import subprocess
import tempfile
import unittest

PARETO = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "lib", "pareto.py")


def run_pareto(cells):
    """Run the real CLI over a throwaway results.jsonl and return its stdout.

    The tests exercise the report the way its one caller (summary.sh) does,
    because the contract being defended is what the printed report says."""
    with tempfile.NamedTemporaryFile("w", suffix=".jsonl", delete=False) as handle:
        for cell in cells:
            handle.write(json.dumps(cell) + "\n")
        path = handle.name
    try:
        done = subprocess.run([sys_python(), PARETO, path], capture_output=True, text=True)
    finally:
        os.unlink(path)
    return done.stdout


def sys_python():
    return "python3"


def base(**overrides):
    """One honest, boring, comparable attempt with everything the rig records."""
    cell = {
        "scenario": "code-fix", "workload": "coding", "door": "print",
        "arm": "pi", "arm_version": "1.0", "model_pin": "m1",
        "effort_requested": "high", "effort_sent": "high",
        "wall_s": 30, "exit": 0, "cost_usd": 0.10, "cost_source": "receipt",
        "tokens_in": 100, "tokens_out": 50, "turns": 1,
        "verdict": "pass", "comparable": "yes", "reason": "",
        "checks": [{"name": "answer", "outcome": "pass"}],
    }
    cell.update(overrides)
    return cell


class BinarySuccessTests(unittest.TestCase):
    def test_wrong_answer_with_many_passed_infrastructure_checks_is_a_failure(self):
        # Nine checks passed and the tenth — the one about the actual answer —
        # failed, so the verdict is fail. A fraction-of-assertions score would
        # call this cell 90% good; the report must call it a failure.
        checks = [{"name": "infra-%d" % i, "outcome": "pass"} for i in range(9)]
        checks.append({"name": "the answer is right", "outcome": "fail"})
        out = run_pareto([base(checks=checks, verdict="fail")])
        self.assertIn("0/1", out)
        self.assertNotIn("1/1", out)

    def test_verdict_pass_alone_is_not_success_when_a_check_failed(self):
        out = run_pareto([base(checks=[
            {"name": "a", "outcome": "pass"}, {"name": "b", "outcome": "fail"}])])
        self.assertIn("0/1", out)

    def test_cell_with_no_assertions_is_excluded_not_success(self):
        out = run_pareto([base(checks=[], reason="")])
        self.assertIn("the cell made no assertions", out)
        self.assertNotIn("1/1", out)


class MissingDataTests(unittest.TestCase):
    def test_missing_cost_on_a_failure_is_shown_not_hidden(self):
        # The arm failed and the harness never billed it. The attempt must stay
        # in the denominator and the report must say the ledger is partial
        # rather than computing a mean from the one cost it happens to have.
        out = run_pareto([
            base(verdict="fail", cost_usd=None,
                 checks=[{"name": "answer", "outcome": "fail"}]),
            base(cost_usd=0.20),
        ])
        self.assertIn("1/2", out)
        self.assertIn("cost withheld", out)
        self.assertIn("no total, mean, or per-success cost is computed", out)

    def test_missing_wall_is_never_zero(self):
        # One attempt at 60s and one attempt with wall_s null. If the missing
        # wall were read as 0, the mean would be 30; it must be 60, and the
        # missing-time count must be visible rather than guessed.
        out = run_pareto([base(wall_s=60), base(wall_s=None, scenario="code-fix")])
        self.assertIn("60.0", out)
        self.assertNotIn("30.0", out)
        self.assertIn("miss_s", out)

    def test_cost_per_success_uses_all_attempts(self):
        # Three attempts at $0.10 with one success: cost per success is $0.30,
        # not $0.10 — the failures are part of what the success cost.
        out = run_pareto([
            base(),
            base(verdict="fail", checks=[{"name": "answer", "outcome": "fail"}]),
            base(verdict="fail", checks=[{"name": "answer", "outcome": "fail"}]),
        ])
        self.assertIn("0.3000", out)


class StratumTests(unittest.TestCase):
    def test_mixed_scenarios_are_never_averaged(self):
        # Two scenarios, one arm succeeding everywhere at 100%. A report that
        # rolled scenarios into a workload would print one row; this must print
        # one row per scenario, each with its own denominator.
        out = run_pareto([
            base(scenario="code-fix", workload="coding"),
            base(scenario="writing-memo", workload="writing"),
        ])
        self.assertIn("scenario=code-fix", out)
        self.assertIn("scenario=writing-memo", out)
        self.assertEqual(out.count("1/1"), 2)

    def test_mixed_doors_do_not_share_a_denominator(self):
        out = run_pareto([base(door="print"), base(door="interactive")])
        self.assertIn("door=print", out)
        self.assertIn("door=interactive", out)

    def test_mixed_models_and_efforts_are_separate_strata(self):
        out = run_pareto([
            base(model_pin="m1", effort_requested="high", effort_sent="high"),
            base(model_pin="m2", effort_requested="high", effort_sent="high"),
            base(model_pin="m1", effort_requested="low", effort_sent="low"),
        ])
        self.assertEqual(out.count("1/1"), 3)
        self.assertIn("model_pin=m1", out)
        self.assertIn("model_pin=m2", out)
        self.assertIn("effort_requested=low", out)


class PairedBlockTests(unittest.TestCase):
    @staticmethod
    def paired(n_blocks, arms=("pi", "cc"), experiment_id="e1", mutate=None):
        cells = []
        for block in range(n_blocks):
            for arm in arms:
                cell = base(arm=arm, arm_version="%s-1.0" % arm,
                            experiment_id=experiment_id, block_id="b%02d" % block)
                if mutate:
                    mutate(block, arm, cell)
                cells.append(cell)
        return cells

    def test_duplicate_in_a_block_rejects_the_block(self):
        cells = self.paired(6)
        # A seventh block carries pi twice, which is not a pairing and must not
        # be read as one.
        cells.append(base(arm="pi", arm_version="pi-1.0",
                          experiment_id="e1", block_id="b07"))
        cells.append(base(arm="pi", arm_version="pi-1.0",
                          experiment_id="e1", block_id="b07"))
        out = run_pareto(cells)
        self.assertIn("duplicate", out)
        self.assertIn("b07", out)
        self.assertIn("6 paired", out)

    def test_unbalanced_block_is_rejected_and_named(self):
        cells = self.paired(6)
        # Block 03 lost its cc attempt; the comparison must not quietly
        # compare 6 pi cells against 5 cc cells as if nothing were missing.
        cells.append(base(arm="pi", arm_version="pi-1.0",
                          experiment_id="e1", block_id="b06"))
        out = run_pareto(cells)
        self.assertIn("unbalanced", out)
        self.assertIn("no attempt for cc", out)
        self.assertIn("6 paired", out)

    def test_inconsistent_arm_version_refuses_the_comparison(self):
        cells = self.paired(6, mutate=lambda block, arm, cell:
                            cell.update({"arm_version": "%s-2.0" % arm}) if block == 3 else None)
        out = run_pareto(cells)
        self.assertIn("arm_version is inconsistent", out)
        self.assertIn("nothing is compared", out)

    def test_missing_condition_metadata_rejects_the_cell(self):
        cells = self.paired(6, mutate=lambda block, arm, cell:
                            cell.update({"door": None}) if block == 4 else None)
        out = run_pareto(cells)
        self.assertIn("condition metadata is missing", out)

    def test_fewer_than_five_blocks_gets_no_confidence_interval(self):
        out = run_pareto(self.paired(4))
        self.assertIn("fewer than 5 paired blocks", out)
        self.assertNotIn("95% CI", out)
        # And the observed set is explicitly exploratory, never a frontier.
        self.assertIn("exploratory only", out)

    def test_five_blocks_get_a_deterministic_interval(self):
        cells = self.paired(6, mutate=lambda block, arm, cell:
                            cell.update({"cost_usd": 0.10 if arm == "pi" else 0.20,
                                         "wall_s": 30 if arm == "pi" else 60}))
        first = run_pareto(cells)
        second = run_pareto(cells)
        self.assertIn("95% CI", first)
        # The seed is fixed: two reads of the same file agree, so a quoted
        # interval is reproducible.
        self.assertEqual(first, second)
        self.assertNotIn("cost withheld", first)

    def test_all_success_small_n_cannot_prove_equality(self):
        # Both arms succeeded in every block. That is consistent with equality
        # and equally consistent with one arm being better where the scenarios
        # happened to be easy, so the report must say the samples cannot
        # establish equivalence rather than calling the arms equivalent.
        out = run_pareto(self.paired(6))
        self.assertIn("cannot establish equivalence", out)

    def test_point_means_do_not_declare_superiority(self):
        # cc is better on every mean, but with all-success samples the success
        # difference is zero and the report must not crown a winner.
        cells = self.paired(6, mutate=lambda block, arm, cell:
                            cell.update({"cost_usd": 0.10 if arm == "pi" else 0.05,
                                         "wall_s": 60 if arm == "pi" else 30}))
        out = run_pareto(cells)
        self.assertNotIn("superior", out.lower().replace("superiority", ""))
        self.assertIn("not a frontier, not a ranking", out)


class LegacyTests(unittest.TestCase):
    def test_legacy_file_is_descriptive_only(self):
        out = run_pareto([
            base(arm="pi", cost_usd=0.05, wall_s=10),
            base(arm="cc", cost_usd=0.50, wall_s=90,
                 checks=[{"name": "answer", "outcome": "fail"}], verdict="fail"),
        ])
        # The rows are described; no frontier or dominance claim appears.
        self.assertIn("DESCRIPTIVE ONLY", out)
        lowered = out.lower()
        self.assertNotIn("not dominated:", lowered)
        self.assertNotIn("on the frontier", lowered)
        self.assertIn("scenario=code-fix", out)

    def test_legacy_failures_stay_in_the_denominator(self):
        out = run_pareto([
            base(arm="cc"),
            base(arm="cc", verdict="fail",
                 checks=[{"name": "answer", "outcome": "fail"}]),
        ])
        self.assertIn("1/2", out)

    def test_half_a_pairing_key_is_excluded(self):
        out = run_pareto([base(experiment_id="e1", block_id=None)])
        self.assertIn("must both be present to pair", out)


class ExclusionTests(unittest.TestCase):
    def test_unsupported_and_skipped_are_not_attempts(self):
        out = run_pareto([
            base(),
            base(verdict="unsupported", comparable="no", reason="scenario not defined",
                 checks=[], wall_s=None, cost_usd=None),
            base(verdict="skipped", comparable="no", reason="not installed",
                 checks=[], wall_s=None, cost_usd=None),
        ])
        self.assertIn("1/1", out)
        self.assertIn("unsupported: scenario not defined", out)
        self.assertIn("skipped: not installed", out)
        self.assertIn("measurement", out)


if __name__ == "__main__":
    unittest.main()
