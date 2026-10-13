#!/usr/bin/env python3
"""Exercise the drive's real outcome predicates and evidence after home removal."""

import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


HERE = Path(__file__).resolve().parent
HELPER = HERE / "hosted-evidence.py"
DRIVE = HERE / "hosted-drive.sh"
spec = importlib.util.spec_from_file_location("hosted_evidence", HELPER)
evidence = importlib.util.module_from_spec(spec)
spec.loader.exec_module(evidence)


class HostedEvidenceTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home = self.root / "home"
        self.home.mkdir()
        self.journal = self.home / "v3/projects/project/tasks.jsonl"
        self.journal.parent.mkdir(parents=True)

    def rows(self, *rows):
        self.journal.write_text("".join(json.dumps(row) + "\n" for row in rows))

    def run_helper(self, *args):
        return subprocess.run([sys.executable, str(HELPER), *args], capture_output=True, text=True)

    def test_real_file_presence_predicate_refuses_failed_or_pending_handoffs(self):
        work = self.root / "work"
        work.mkdir()
        for name in ["README.md", "muldiv.go"]:
            (work / name).touch()
        body = re.search(r"^first_run_home\(\) \{.*$", DRIVE.read_text(), re.M)[0]
        shell = 'terminal_tasks() { python3 "$EVIDENCE_HELPER" terminal --home "$HOME_DIR" --ids "$@"; }\n' + body + "\nfirst_run_home"
        env = dict(os.environ, WORK=str(work), HOME_DIR=str(self.home), EVIDENCE_HELPER=str(HELPER))
        for status in ["failed", "running", "queued", "your call"]:
            self.rows({"id": "1", "status": status}, {"id": "2", "status": "done"})
            self.assertNotEqual(subprocess.run(["bash", "-c", shell], env=env).returncode, 0, status)
        self.rows({"id": "1", "status": "done"}, {"id": "2", "status": "done"})
        self.assertEqual(subprocess.run(["bash", "-c", shell], env=env).returncode, 0)

    def test_latest_outcome_wins_and_missing_or_ambiguous_tasks_fail_closed(self):
        self.rows({"id": "1", "sessionId": "a", "status": "done"},
                  {"id": "1", "sessionId": "a", "status": "failed", "ending": "refused"})
        self.assertNotEqual(self.run_helper("terminal", "--home", str(self.home), "--ids", "1").returncode, 0)
        self.assertFalse(evidence.successful(self.home, ["2"]))
        self.rows({"id": "1", "sessionId": "a", "status": "done"},
                  {"id": "1", "sessionId": "b", "status": "done"})
        self.assertFalse(evidence.successful(self.home, ["1"]))
        self.journal.write_text('{"id": "1", "status": "done"}\n{bad json}\n')
        self.assertNotEqual(self.run_helper("terminal", "--home", str(self.home), "--ids", "1").returncode, 0)
        self.journal.write_text('{"id": "1", "status": "done"}\n{"id":"1","status":"fail')
        self.assertNotEqual(self.run_helper("terminal", "--home", str(self.home), "--ids", "1").returncode, 0)

    def test_failed_reason_actions_and_run_basis_survive_home_removal(self):
        row = {"id": "1", "status": "failed", "ending": "refused", "outcome": "task reached its spending limit"}
        self.rows(row)
        task = self.journal.parent / "tasks/1"
        task.mkdir(parents=True)
        (task / "trajectory.jsonl").write_text('{"tool":"bash","exit_code":1,"observation":"real failed command"}\n')
        database = self.journal.parent / "plandb.db"
        connection = sqlite3.connect(database)
        connection.execute("CREATE TABLE tasks (id TEXT, status TEXT, err TEXT, verdict_basis TEXT)")
        connection.execute("INSERT INTO tasks VALUES (?, ?, ?, ?)", ("1", "failed", "real failure", '{"kind":"run","runs":[]}'))
        connection.commit()
        connection.close()
        output = self.root / "kept"
        result = self.run_helper("snapshot", "--home", str(self.home), "--output", str(output))
        self.assertEqual(result.returncode, 0, result.stderr)
        shutil.rmtree(self.home)
        self.assertEqual(json.loads((output / "summary.json").read_text())["outcomes"], [row])
        self.assertIn("real failed command", (output / "records" / task.relative_to(self.home) / "trajectory.jsonl").read_text())
        stored = json.loads((output / "sqlite" / Path(str(database.relative_to(self.home)) + ".json")).read_text())
        self.assertEqual(stored[0]["err"], "real failure")
        self.assertEqual(json.loads(stored[0]["verdict_basis"])["kind"], "run")

    def test_bound_mode_does_not_open_configs_or_follow_worker_links(self):
        config = self.home / "config.json"
        config.write_text("MUST NEVER BE READ")
        external = self.root / "external-profile"
        external.mkdir()
        (external / "config.json").write_text("MUST NEVER BE READ")
        (self.home / "profile-link").symlink_to(external, target_is_directory=True)
        (self.home / "credential.json").symlink_to(external / "config.json")
        self.rows({"id": "1", "status": "done"})
        read_text = Path.read_text

        def guarded(path, *args, **kwargs):
            if path.name == "config.json" or path.is_symlink():
                self.fail("bound credential file/link was opened")
            return read_text(path, *args, **kwargs)

        output = self.root / "kept"
        with mock.patch.object(Path, "read_text", guarded):
            evidence.snapshot(self.home, output, evidence.secrets())
        self.assertFalse(list(output.rglob("config.json")))
        self.assertFalse(list(output.rglob("credential.json")))

    def test_redaction_preserves_diagnostic_json_without_known_key(self):
        key = "synthetic_known_secret_123"
        self.rows({"id": "1", "status": "failed", "outcome": "Bearer " + key})
        output = self.root / "kept"
        evidence.snapshot(self.home, output, {key})
        for path in output.rglob("*"):
            if path.is_file():
                self.assertNotIn(key, path.read_text())
        self.assertIn("struck out", json.loads((output / "summary.json").read_text())["outcomes"][0]["outcome"])

    def test_success_cleanup_keeps_full_outcome_before_removing_owned_home(self):
        home = Path(tempfile.mkdtemp(prefix="codeaf-drive-home.", dir="/tmp"))
        work = Path(tempfile.mkdtemp(prefix="codeaf-drive-work.", dir="/tmp"))
        self.addCleanup(shutil.rmtree, home, True)
        self.addCleanup(shutil.rmtree, work, True)
        journal = home / "tasks.jsonl"
        journal.write_text('{"id":"1","status":"failed","ending":"refused","outcome":"actual reason"}\n')
        output = self.root / "kept"
        output.mkdir()
        source = DRIVE.read_text()
        functions = "\n".join(re.search(r"^" + name + r"\(\) \{\n.*?^\}", source, re.M | re.S)[0]
                              for name in ["strike_keys", "capture_evidence", "cleanup"])
        shell = '''set -u
EVIDENCE_ARGS=(--home "$HOME_DIR")
passed=yes
final_kept=no
say() { printf '%s\\n' "$*"; }
t() { :; }
end_engine() { :; }
screen() { printf 'final screen\\n'; }
''' + functions + "\ncleanup\n"
        env = dict(os.environ, HOME_DIR=str(home), WORK=str(work), OUT=str(output), EVIDENCE_HELPER=str(HELPER))
        result = subprocess.run(["bash", "-c", shell], env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(home.exists())
        self.assertFalse(work.exists())
        row = json.loads((output / "final/summary.json").read_text())["outcomes"][0]
        self.assertEqual((row["status"], row["ending"], row["outcome"]), ("failed", "refused", "actual reason"))

    def test_bound_profile_copy_block_never_calls_copy(self):
        source = DRIVE.read_text()
        block = re.search(r'^profile=.*?^fi$', source, re.M | re.S)[0]
        profile = self.root / "profile"
        profile.mkdir()
        (profile / "config.json").write_text("synthetic credential file")
        env = dict(os.environ, CODEAF_PROFILE_DIR=str(profile), CODEAF_HOME=str(profile), HOME_DIR=str(self.home))
        result = subprocess.run(["bash", "-c", 'cp() { echo "unexpected credential copy" >&2; return 99; }\n' + block],
                                env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("unexpected credential copy", result.stderr)
        self.assertFalse((self.home / "config.json").exists())

    def test_launch_pins_call_metadata_to_owned_home_without_bodies(self):
        work = self.root / "work"
        work.mkdir()
        fake = self.root / "fixture-cli"
        fake.write_text('''#!/usr/bin/env python3
import json, os
from pathlib import Path
path = Path(os.environ["CODEAF_CALL_LOG"])
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(json.dumps({"model":"fixture-model", "status":400, "error":"actual endpoint refusal", "bodies":os.environ["CODEAF_CALL_LOG_BODIES"]})+"\\n")
''')
        fake.chmod(0o700)
        launch = re.search(r'^t new-session.*?^\s*".*?sleep 3600"$', DRIVE.read_text(), re.M | re.S)[0]
        # Execute the production environment binding with a deterministic
        # process fixture, without tmux or the drive's post-exit pane delay.
        shell = '''t() {
  for command in "$@"; do :; done
  command=${command%; sleep 3600}
  bash -c "$command"
}
''' + launch
        env = dict(os.environ, HOME_DIR=str(self.home), WORK=str(work), BIN=str(fake), CODEAF_PROFILE_DIR=str(self.root / "external"))
        result = subprocess.run(["bash", "-c", shell], env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        output = self.root / "kept"
        evidence.snapshot(self.home, output, set())
        shutil.rmtree(self.home)
        record = json.loads((output / "records/logs/calls.jsonl").read_text())
        self.assertEqual((record["status"], record["error"], record["bodies"]), (400, "actual endpoint refusal", "off"))

    def test_original_guards_and_all_fourteen_assertions_still_exist(self):
        source = DRIVE.read_text()
        self.assertIn("wait_for 60 5 first_run_home", source)
        self.assertIn("wait_for 60 5 second_run_home", source)
        self.assertEqual(len(re.findall(r'^check "', source, re.M)), 16)
        # This test covers the newly added outcome checks without accepting a
        # weaker UI-only result; the live drive still owns real-model proof.
        self.assertIn('check "both first hand-offs ended successfully" terminal_tasks 1 2', source)
        self.assertIn('check "the later hand-off ended successfully" terminal_tasks 3', source)


if __name__ == "__main__":
    unittest.main()
