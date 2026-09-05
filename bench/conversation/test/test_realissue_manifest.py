#!/usr/bin/env python3
"""The real-issue manifest refuses the four ways a candidate can lie.

Offline: no git, no network, no model. Each test mutates one field of a manifest
that is otherwise valid, so a refusal names the field it is about.

    python3 -m unittest discover -s bench/conversation/test -p 'test_realissue*.py'
"""
import copy
import json
from pathlib import Path
import sys
import unittest

RIG = Path(__file__).resolve().parents[1] / "fixtures/realissue"
sys.path.insert(0, str(RIG))
import manifest as ri  # noqa: E402

CANDIDATES = sorted((RIG / "candidates").glob("*.json"))

GOOD = {
    "schema": 1, "id": "example", "readiness": "candidate",
    "provenance": {"repo": "example/repo", "issue": 7, "fix_pr": 9,
                   "base_commit": "a" * 40, "fix_commit": "b" * 40},
    "workspace": {"export": "git-archive", "carries_git_history": False},
    "prompt": {"text": "The tally is wrong on the second page."},
    "acceptance": {"held_out_paths": ["pkg/tally_test.go"], "guarded_paths": ["pkg/tally.go"],
                   "command": "go test ./pkg/"},
    "arms": ["aforge", "pi", "omp"],
    "doors": {"print": ["aforge", "pi", "omp"], "interactive": ["aforge", "pi", "omp"]},
    "model_pin": "deepseek/deepseek-v4-flash-0731",
}


def mutate(**changes):
    m = copy.deepcopy(GOOD)
    for path, value in changes.items():
        keys = path.split("__")
        node = m
        for key in keys[:-1]:
            node = node[key]
        if value is ri:          # sentinel: delete the field
            del node[keys[-1]]
        else:
            node[keys[-1]] = value
    return m


class TheManifestRefuses(unittest.TestCase):
    def refuses(self, manifest, fragment):
        with self.assertRaises(ri.Refusal) as caught:
            ri.validate(manifest)
        self.assertIn(fragment, str(caught.exception))

    def test_a_valid_manifest_passes(self):
        self.assertTrue(ri.validate(mutate()))

    # --- frozen base -------------------------------------------------------
    def test_a_missing_base_sha(self):
        self.refuses(mutate(provenance__base_commit=ri), "provenance.base_commit is missing")

    def test_a_short_sha_is_not_frozen(self):
        self.refuses(mutate(provenance__base_commit="a" * 12), "full 40-character sha")

    def test_a_branch_name_is_not_frozen(self):
        self.refuses(mutate(provenance__fix_commit="dev"), "full 40-character sha")

    def test_base_and_fix_cannot_be_the_same_commit(self):
        self.refuses(mutate(provenance__fix_commit="a" * 40), "the bug is not in the workspace")

    # --- no future history, no fix in what the model sees ------------------
    def test_a_workspace_that_carries_git_history(self):
        self.refuses(mutate(workspace__carries_git_history=True), "carries_git_history must be false")

    def test_an_unknown_export_mechanism(self):
        self.refuses(mutate(workspace__export="git-clone"), "git-archive or single-commit")

    def test_the_prompt_naming_the_fix_commit(self):
        self.refuses(mutate(prompt__text="fixed in " + "b" * 12), "names the fix_commit")

    def test_the_prompt_pointing_at_the_fixing_pull_request(self):
        self.refuses(mutate(prompt__text="see #9 for the repair"), "fixing pull request")

    def test_the_prompt_naming_the_held_out_test(self):
        self.refuses(mutate(prompt__text="add a case to pkg/tally_test.go"), "names the held-out test")

    def test_the_prompt_naming_only_the_held_out_basename(self):
        self.refuses(mutate(prompt__text="tally_test.go covers it"), "names the held-out test")

    def test_a_diff_of_the_fix_pasted_into_the_brief(self):
        self.refuses(mutate(prompt__text="broken:\ndiff --git a/pkg/tally.go b/pkg/tally.go\n"),
                     "contains a diff")

    def test_an_attachment_leaks_the_same_way_as_the_prompt(self):
        self.refuses(mutate(prompt__attachments={"note.md": "the answer is in #9"}),
                     "fixing pull request")

    def test_a_held_out_path_cannot_also_be_guarded(self):
        self.refuses(mutate(acceptance__guarded_paths=["pkg/tally_test.go"]),
                     "cannot also be guarded")

    def test_acceptance_with_nothing_held_out(self):
        self.refuses(mutate(acceptance__held_out_paths=[]), "nothing to grade with")

    # --- doors and models --------------------------------------------------
    def test_an_arm_no_door_names(self):
        self.refuses(mutate(doors={"print": ["aforge", "pi"], "interactive": ["aforge", "pi"]}),
                     "arms named by no door: omp")

    def test_a_door_naming_an_arm_that_is_not_in_the_run(self):
        self.refuses(mutate(doors={"print": ["aforge", "pi", "omp", "opencode"],
                                   "interactive": ["aforge", "pi", "omp"]}),
                     "which is not in arms")

    def test_an_interactive_door_this_suite_cannot_drive(self):
        m = mutate(arms=["aforge", "opencode"],
                   doors={"print": ["aforge", "opencode"], "interactive": ["aforge", "opencode"]})
        self.refuses(m, "drives no interactive door for opencode")

    def test_a_door_with_no_arm_at_all(self):
        self.refuses(mutate(doors={"print": ["aforge", "pi", "omp"], "interactive": []}),
                     "door interactive names no arm")

    def test_a_model_off_the_open_allowlist(self):
        self.refuses(mutate(model_pin="openai/gpt-5"), "not an allowlisted open model")

    def test_an_unpinned_model(self):
        self.refuses(mutate(model_pin=ri), "not an allowlisted open model")

    # --- readiness is a claim about evidence -------------------------------
    def test_calibrated_with_no_evidence(self):
        self.refuses(mutate(readiness="calibrated"), "calibrated claims fails_at_base")

    def test_calibrated_with_only_half_the_pair(self):
        m = mutate(readiness="calibrated", calibration={
            "fails_at_base": {"command": "go test ./pkg/", "observed_at": "2026-09-05",
                              "build_ok": True}})
        self.refuses(m, "calibrated claims passes_on_fix")

    def test_calibrated_on_a_test_that_never_built_at_base(self):
        evidence = {"command": "go test ./pkg/", "observed_at": "2026-09-05", "build_ok": True}
        broken = dict(evidence, build_ok=False)
        m = mutate(readiness="calibrated",
                   calibration={"fails_at_base": broken, "passes_on_fix": evidence})
        self.refuses(m, "grades the reference implementation's shape")

    def test_a_blocker_cannot_sit_under_a_ready_word(self):
        m = mutate(readiness="preflight-clean", blockers=["the test is not held out yet"])
        self.refuses(m, "readiness cannot be 'preflight-clean'")

    def test_a_rejection_has_to_say_why(self):
        self.refuses(mutate(readiness="rejected"), "must say why")

    def test_an_invented_readiness_word(self):
        self.refuses(mutate(readiness="ready"), "readiness must be one of")

    def test_calibration_evidence_nobody_can_repeat(self):
        m = mutate(calibration={"fails_at_base": {"observed_at": "2026-09-05"}})
        self.refuses(m, "nobody can repeat it")


class TheShippedCandidates(unittest.TestCase):
    """The files in candidates/ are held to the same rules, and claim no more than was seen."""

    def test_there_are_candidates_to_check(self):
        self.assertGreaterEqual(len(CANDIDATES), 3)

    def test_every_candidate_validates(self):
        for path in CANDIDATES:
            with self.subTest(path.name):
                self.assertTrue(ri.validate(json.loads(path.read_text()), path.name))

    def test_every_candidate_freezes_a_base_and_names_its_issue(self):
        for path in CANDIDATES:
            with self.subTest(path.name):
                provenance = json.loads(path.read_text())["provenance"]
                self.assertRegex(provenance["base_commit"], r"^[0-9a-f]{40}$")
                self.assertIsInstance(provenance["issue"], int)
                self.assertTrue(provenance["issue_url"].startswith("https://github.com/"))

    def test_a_candidate_calls_itself_calibrated_only_with_both_observations(self):
        for path in CANDIDATES:
            manifest = json.loads(path.read_text())
            if manifest["readiness"] == "calibrated":
                with self.subTest(path.name):
                    for key in ("fails_at_base", "passes_on_fix"):
                        self.assertTrue(manifest["calibration"][key]["build_ok"])
                        self.assertIn("observed_at", manifest["calibration"][key])

    def test_no_candidate_that_is_not_calibrated_pretends_otherwise(self):
        """An uncalibrated candidate carries its blockers or its rejection, never neither."""
        for path in CANDIDATES:
            manifest = json.loads(path.read_text())
            if manifest["readiness"] in ("candidate", "rejected"):
                with self.subTest(path.name):
                    self.assertTrue(manifest.get("blockers") or manifest.get("rejection"),
                                    "%s is not ready and says nothing about why" % path.name)


if __name__ == "__main__":
    unittest.main()
