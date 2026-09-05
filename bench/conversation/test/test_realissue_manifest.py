#!/usr/bin/env python3
"""The real-issue manifest refuses the four ways a candidate can lie.

Offline: no git, no network, no model. Each test mutates one field of a manifest
that is otherwise valid, so a refusal names the field it is about.

    python3 -m unittest discover -s bench/conversation/test -p 'test_realissue*.py'
"""
import copy
import hashlib
import json
from pathlib import Path
import sys
import unittest

RIG = Path(__file__).resolve().parents[1] / "fixtures/realissue"
sys.path.insert(0, str(RIG))
import manifest as ri  # noqa: E402

CANDIDATES = sorted((RIG / "candidates").glob("*.json"))

PROMPT = "The tally is wrong on the second page.\n\nSeen twice on a fresh home.\n"
EVIDENCE = {"command": "go test ./pkg/", "observed_at": "2026-09-05", "exit_code": 0,
            "build_ok": True, "output_sha256": "c" * 64, "verification": "manual-recorded"}
GOOD = {
    "schema": 2, "id": "example", "task_kind": "bug-existing-api", "readiness": "candidate",
    "provenance": {"repo": "example/repo", "issue": 7, "fix_pr": 9, "language": "go",
                   "base_commit": "a" * 40, "fix_commit": "b" * 40},
    "workspace": {"export": "git-archive", "carries_git_history": False},
    "prompt": {"verbatim": False, "text": PROMPT,
               "sha256": hashlib.sha256(PROMPT.encode()).hexdigest(),
               "derivation": {"source_url": "https://example/7", "source_sha256": "d" * 64,
                              "rule": "headings", "curated_at": "2026-09-05",
                              "kept_sections": ["What happened"],
                              "dropped_sections": ["The fix", "Acceptance"],
                              "dropped_paragraphs": {}}},
    "acceptance": {"held_out_paths": ["pkg/tally_test.go"], "fix_paths": ["pkg/tally.go"],
                   "guarded_paths": ["pkg/fixtures/rates.csv"], "command": "go test ./pkg/"},
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
    if "prompt__text" in changes:      # the prompt is hashed, so keep the pair honest
        m["prompt"]["sha256"] = hashlib.sha256(m["prompt"]["text"].encode()).hexdigest()
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
        self.refuses(mutate(prompt__text="broken:\ndiff --git a/pkg/rates.go b/pkg/rates.go\n"),
                     "contains a diff")

    def test_an_attachment_leaks_the_same_way_as_the_prompt(self):
        self.refuses(mutate(prompt__attachments={"note.md": "the answer is in #9"}),
                     "fixing pull request")

    def test_the_report_section_that_carried_the_patch(self):
        """#528's `## The fix` held a literal Go patch; the first version shipped it."""
        leaked = PROMPT + "\n## The fix\n\nGrow the missing arm on the switch.\n"
        self.refuses(mutate(prompt__text=leaked), "keeps the report's")

    def test_the_acceptance_section_that_named_the_held_out_tests(self):
        leaked = PROMPT + "\n## Acceptance\n\n- the column names jobs\n"
        self.refuses(mutate(prompt__text=leaked), "keeps the report's")

    def test_a_fenced_block_of_the_implementation_language(self):
        leaked = PROMPT + "\n```go\ncase session.EventJobUpdate:\n\ta.touch()\n```\n"
        self.refuses(mutate(prompt__text=leaked), "fenced go block")

    def test_the_name_of_a_test_function(self):
        leaked = PROMPT + "\nTestAReopenedConversationStillShowsItsJobs covers it.\n"
        self.refuses(mutate(prompt__text=leaked), "names the test function")

    def test_the_path_the_repair_must_edit(self):
        self.refuses(mutate(prompt__text=PROMPT + "\nthe switch in pkg/tally.go is missing an arm\n"),
                     "which the repair must edit")

    def test_a_verbatim_body_is_not_a_task(self):
        self.refuses(mutate(prompt__verbatim=True), "the issue body carries its own fix")

    def test_a_prompt_edited_after_curation(self):
        m = copy.deepcopy(GOOD)
        m["prompt"]["text"] += "\nand one more hint: grow the switch arm.\n"
        self.refuses(m, "does not match prompt.text")

    def test_a_derivation_that_dropped_nothing(self):
        self.refuses(mutate(prompt__derivation=dict(GOOD["prompt"]["derivation"],
                                                    dropped_sections=[])),
                     "verbatim body under another name")

    def test_a_derivation_with_no_source(self):
        derivation = {k: v for k, v in GOOD["prompt"]["derivation"].items() if k != "source_url"}
        self.refuses(mutate(prompt__derivation=derivation), "derivation.source_url is missing")

    def test_a_held_out_path_cannot_also_be_guarded(self):
        self.refuses(mutate(acceptance__guarded_paths=["pkg/tally_test.go"]),
                     "cannot also be guarded")

    def test_guarding_the_file_the_repair_must_edit(self):
        """Protecting the source the agent has to change rejects every real fix."""
        self.refuses(mutate(acceptance__guarded_paths=["pkg/tally.go"]),
                     "guard the evaluation instruments, never a known implementation path")

    def test_a_manifest_that_names_no_implementation_path(self):
        self.refuses(mutate(acceptance__fix_paths=[]), "fix_paths is empty")

    def test_the_held_out_test_cannot_also_be_the_repair(self):
        self.refuses(mutate(acceptance__fix_paths=["pkg/tally_test.go"]),
                     "both held out and a path the repair must write")

    def test_acceptance_with_nothing_held_out(self):
        self.refuses(mutate(acceptance__held_out_paths=[]), "nothing to grade with")

    # --- doors and models --------------------------------------------------
    def test_an_arm_no_door_names(self):
        self.refuses(mutate(doors={"print": ["aforge", "pi"], "interactive": ["aforge", "pi"]}),
                     "door print is missing omp")

    def test_doors_that_cover_the_arms_only_between_them(self):
        """aforge through print and pi through interactive is two experiments, not one row."""
        m = mutate(arms=["aforge", "pi"],
                   doors={"print": ["aforge"], "interactive": ["pi"]})
        self.refuses(m, "a compared door carries every arm")

    def test_a_door_that_repeats_an_arm(self):
        m = mutate(arms=["aforge", "pi"],
                   doors={"print": ["aforge", "pi", "pi"], "interactive": ["aforge", "pi"]})
        self.refuses(m, "door print repeats an arm")

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
        self.refuses(mutate(model_pin=ri), "missing model_pin")

    # --- readiness is a claim about evidence -------------------------------
    def test_calibrated_with_no_evidence(self):
        self.refuses(mutate(readiness="grader-calibrated"), "claims fails_at_base")

    def test_calibrated_with_only_half_the_pair(self):
        m = mutate(readiness="grader-calibrated",
                   calibration={"fails_at_base": dict(EVIDENCE, exit_code=1)})
        self.refuses(m, "claims passes_on_fix")

    def test_evidence_whose_polarity_points_the_wrong_way(self):
        """A pair that does not point both ways grades nothing."""
        m = mutate(readiness="grader-calibrated",
                   calibration={"fails_at_base": dict(EVIDENCE, exit_code=0),
                                "passes_on_fix": EVIDENCE})
        self.refuses(m, "that is not a failure at base")

    def test_a_pass_recorded_with_a_non_zero_exit(self):
        m = mutate(readiness="grader-calibrated",
                   calibration={"fails_at_base": dict(EVIDENCE, exit_code=1),
                                "passes_on_fix": dict(EVIDENCE, exit_code=2)})
        self.refuses(m, "that is not a pass")

    def test_evidence_with_no_exit_code(self):
        broken = {k: v for k, v in EVIDENCE.items() if k != "exit_code"}
        self.refuses(mutate(calibration={"passes_on_fix": broken}), "needs the exit code")

    def test_evidence_with_no_output_hash(self):
        broken = {k: v for k, v in EVIDENCE.items() if k != "output_sha256"}
        self.refuses(mutate(calibration={"passes_on_fix": broken}), "needs output_sha256")

    def test_evidence_that_does_not_say_a_person_recorded_it(self):
        broken = dict(EVIDENCE, verification="machine-verified")
        self.refuses(mutate(calibration={"passes_on_fix": broken}), "manual-recorded")

    def test_a_bug_task_whose_test_never_built_at_base(self):
        """For this task_kind the held-out test drives an API that exists at base."""
        m = mutate(readiness="grader-calibrated",
                   calibration={"fails_at_base": dict(EVIDENCE, exit_code=1, build_ok=False),
                                "passes_on_fix": EVIDENCE})
        self.refuses(m, "not a bug-existing-api task")

    def test_a_feature_task_kind_is_out_of_scope_rather_than_smuggled_in(self):
        self.refuses(mutate(task_kind="feature-new-api"), "outside this version's scope")

    def test_grading_a_repair_is_not_campaign_readiness(self):
        m = mutate(readiness="grader-calibrated", campaign_ready=True,
                   calibration={"fails_at_base": dict(EVIDENCE, exit_code=1),
                                "passes_on_fix": EVIDENCE})
        self.refuses(m, "campaign readiness is not decided by a grader pair")

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

    # --- malformed shapes are refusals, not tracebacks ---------------------
    def test_a_path_list_that_is_not_a_list(self):
        self.refuses(mutate(acceptance__held_out_paths="pkg/tally_test.go"), "must be a list")

    def test_a_path_that_is_not_a_string(self):
        self.refuses(mutate(acceptance__held_out_paths=[{"path": "x"}]), "holds a non-path")

    def test_an_absolute_held_out_path(self):
        self.refuses(mutate(acceptance__held_out_paths=["/etc/passwd"]), "workspace-relative")

    def test_a_held_out_path_that_climbs_out_of_the_workspace(self):
        self.refuses(mutate(acceptance__held_out_paths=["../../judge/answers.json"]),
                     "escapes the workspace")

    def test_a_path_repeated(self):
        self.refuses(mutate(acceptance__fix_paths=["pkg/tally.go", "pkg/tally.go"]),
                     "repeats a path")

    def test_provenance_that_is_not_an_object(self):
        self.refuses(mutate(provenance=["example/repo"]), "must be an object")

    def test_an_issue_number_that_is_a_string(self):
        self.refuses(mutate(provenance__issue="7"), "must be a number")

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

    def test_every_candidate_prompt_is_derived_and_hashed(self):
        for path in CANDIDATES:
            manifest = json.loads(path.read_text())
            with self.subTest(path.name):
                prompt = manifest["prompt"]
                self.assertIs(prompt["verbatim"], False)
                self.assertEqual(hashlib.sha256(prompt["text"].encode()).hexdigest(),
                                 prompt["sha256"])
                self.assertTrue(prompt["derivation"]["dropped_sections"])

    def test_the_leak_that_shipped_would_be_refused_today(self):
        """The first version of this fixture shipped issue #528's body verbatim.

        Its `## The fix` section carries the patch and `## Acceptance` names both
        held-out tests. Neither the manifest's lint nor its author caught it; this
        replays that exact text against the candidate as it stands now.
        """
        shipped = json.loads((RIG / "candidates/528-reopened-conversation-jobs.json").read_text())
        leaked = shipped["prompt"]["text"] + (
            "\n## The fix\n\n`app.taskEvent` grows the one arm the standing lane is "
            "missing:\n\n```go\ncase session.EventJobUpdate:\n\tif ev.Job != nil && "
            "a.jobUpdate(*ev.Job) {\n\t\ta.touch()\n\t}\n```\n\n## Acceptance\n\n"
            "- **Unit:** `TestAReopenedConversationStillShowsItsJobs` in `internal/tui3`\n")
        shipped["prompt"]["text"] = leaked
        shipped["prompt"]["sha256"] = hashlib.sha256(leaked.encode()).hexdigest()
        with self.assertRaises(ri.Refusal):
            ri.validate(shipped, "528-as-first-shipped")

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
