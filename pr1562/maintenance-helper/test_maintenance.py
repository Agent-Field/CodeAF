"""Tests for the maintenance helper. Run: python -m unittest -v"""

import io
import json
import tempfile
import unittest
from contextlib import redirect_stdout, redirect_stderr
from datetime import date
from pathlib import Path

import maintenance


def write_csv(rows, directory):
    path = Path(directory) / "maintenance.csv"
    path.write_text("title,due,status\n" + "\n".join(rows) + "\n", encoding="utf-8")
    return str(path)


class FilteringTests(unittest.TestCase):
    def setUp(self):
        self.chores = [
            {"title": "Overdue open", "due": date(2026, 9, 20), "status": "open"},
            {"title": "Due today", "due": date(2026, 9, 25), "status": "open"},
            {"title": "Future", "due": date(2026, 10, 1), "status": "open"},
            {"title": "Done overdue", "due": date(2026, 9, 1), "status": "done"},
            {"title": "Done mixed case", "due": date(2026, 9, 1), "status": "Done"},
        ]

    def test_excludes_future(self):
        got = maintenance.unfinished_due_by(self.chores, date(2026, 9, 25))
        titles = [c["title"] for c in got]
        self.assertNotIn("Future", titles)

    def test_includes_on_cutoff(self):
        got = maintenance.unfinished_due_by(self.chores, date(2026, 9, 25))
        titles = [c["title"] for c in got]
        self.assertIn("Due today", titles)

    def test_excludes_done(self):
        got = maintenance.unfinished_due_by(self.chores, date(2026, 12, 31))
        titles = [c["title"] for c in got]
        self.assertNotIn("Done overdue", titles)
        self.assertNotIn("Done mixed case", titles)

    def test_sorted_soonest_first(self):
        got = maintenance.unfinished_due_by(self.chores, date(2026, 12, 31))
        dues = [c["due"] for c in got]
        self.assertEqual(dues, sorted(dues))

    def test_includes_in_progress(self):
        chores = [{"title": "Started", "due": date(2026, 9, 25), "status": "in-progress"}]
        got = maintenance.unfinished_due_by(chores, date(2026, 10, 1))
        self.assertEqual([c["title"] for c in got], ["Started"])


class LoadTests(unittest.TestCase):
    def test_loads_rows(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_csv(["A,2026-09-20,open", "B,2026-10-01,done"], tmp)
            chores = list(maintenance.load_chores(path))
        self.assertEqual(len(chores), 2)
        self.assertEqual(chores[0]["title"], "A")
        self.assertEqual(chores[0]["due"], date(2026, 9, 20))

    def test_missing_column_errors(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "bad.csv"
            path.write_text("title,status\nA,open\n", encoding="utf-8")
            with self.assertRaises(ValueError):
                list(maintenance.load_chores(str(path)))

    def test_bad_date_errors(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_csv(["A,not-a-date,open"], tmp)
            with self.assertRaises(ValueError):
                list(maintenance.load_chores(path))


class CliTests(unittest.TestCase):
    def run_cli(self, argv):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = maintenance.main(argv)
        return code, out.getvalue(), err.getvalue()

    def test_prints_matching_chore(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_csv(["Overdue,2026-09-20,open", "Future,2026-12-01,open"], tmp)
            code, out, _ = self.run_cli(["--due", "2026-09-25", "--csv", path])
        self.assertEqual(code, 0)
        self.assertIn("Overdue", out)
        self.assertNotIn("Future", out)

    def test_empty_result_message(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_csv(["Future,2026-12-01,open"], tmp)
            code, out, _ = self.run_cli(["--due", "2026-01-01", "--csv", path])
        self.assertEqual(code, 0)
        self.assertIn("No unfinished chores", out)

    def test_json_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_csv(
                [
                    "Overdue,2026-09-20,open",
                    "Started,2026-09-25,in-progress",
                    "Future,2026-12-01,open",
                ],
                tmp,
            )
            code, out, _ = self.run_cli(["--due", "2026-10-01", "--csv", path, "--json"])
        self.assertEqual(code, 0)
        data = json.loads(out)
        self.assertEqual(data["due_by"], "2026-10-01")
        self.assertEqual(data["count"], 2)
        self.assertEqual([c["title"] for c in data["chores"]], ["Overdue", "Started"])
        self.assertEqual(data["chores"][1]["status"], "in-progress")
        self.assertEqual(data["chores"][0]["due"], "2026-09-20")

    def test_bad_due_date_fails(self):
        code, _, err = self.run_cli(["--due", "25-09-2026"])
        self.assertEqual(code, 2)
        self.assertIn("--due", err)

    def test_missing_csv_fails(self):
        code, _, err = self.run_cli(["--due", "2026-09-25", "--csv", "/nope/x.csv"])
        self.assertEqual(code, 2)
        self.assertIn("error", err)


class MarkDoneTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = write_csv(
            [
                "Replace smoke alarm batteries,2026-09-20,open",
                "Clean dryer vent,2026-10-01,open",
                "Change HVAC filter,2026-09-01,done",
            ],
            self.tmp.name,
        )

    def read(self):
        return Path(self.path).read_text(encoding="utf-8")

    def test_marks_only_the_exact_matching_row(self):
        before = self.read().splitlines()
        line = maintenance.mark_done(self.path, "Clean dryer vent")
        after = self.read().splitlines()
        self.assertEqual(line, 3)
        self.assertEqual(after[0], before[0])
        self.assertEqual(after[1], before[1])
        self.assertEqual(after[2], "Clean dryer vent,2026-10-01,done")
        self.assertEqual(after[3], before[3])

    def test_missing_title_leaves_csv_byte_for_byte(self):
        before = self.read()
        with self.assertRaises(ValueError) as caught:
            maintenance.mark_done(self.path, "No such chore")
        self.assertIn("no chore titled", str(caught.exception))
        self.assertEqual(self.read(), before)
        self.assertEqual(list(Path(self.tmp.name).glob(".maintenance-*")), [])

    def test_ambiguous_title_leaves_csv_byte_for_byte(self):
        path = write_csv(
            ["Mow lawn,2026-09-20,open", "Mow lawn,2026-10-01,open"],
            self.tmp.name,
        )
        before = Path(path).read_text(encoding="utf-8")
        with self.assertRaises(ValueError) as caught:
            maintenance.mark_done(path, "Mow lawn")
        self.assertIn("ambiguous", str(caught.exception))
        self.assertEqual(Path(path).read_text(encoding="utf-8"), before)

    def test_title_match_is_case_sensitive(self):
        before = self.read()
        with self.assertRaises(ValueError):
            maintenance.mark_done(self.path, "clean dryer vent")
        self.assertEqual(self.read(), before)

    def test_surrounding_whitespace_is_trimmed(self):
        maintenance.mark_done(self.path, "  Clean dryer vent\t")
        self.assertIn("Clean dryer vent,2026-10-01,done", self.read())

    def test_extra_columns_are_preserved(self):
        path = Path(self.tmp.name) / "extra.csv"
        path.write_text(
            "title,due,status,notes\nClean dryer vent,2026-10-01,open,annual\n",
            encoding="utf-8",
        )
        maintenance.mark_done(str(path), "Clean dryer vent")
        self.assertEqual(
            path.read_text(encoding="utf-8"),
            "title,due,status,notes\nClean dryer vent,2026-10-01,done,annual\n",
        )


class DoneCliTests(unittest.TestCase):
    def run_cli(self, argv):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = maintenance.main(argv)
        return code, out.getvalue(), err.getvalue()

    def test_done_flag_updates_csv_and_reports(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_csv(["Clean dryer vent,2026-10-01,open"], tmp)
            code, out, _ = self.run_cli(["--done", "Clean dryer vent", "--csv", path])
            text = Path(path).read_text(encoding="utf-8")
        self.assertEqual(code, 0)
        self.assertIn("Clean dryer vent", out)
        self.assertEqual(text, "title,due,status\nClean dryer vent,2026-10-01,done\n")

    def test_done_missing_title_exits_2_without_writing(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_csv(["Clean dryer vent,2026-10-01,open"], tmp)
            before = Path(path).read_text(encoding="utf-8")
            code, _, err = self.run_cli(["--done", "No such chore", "--csv", path])
            after = Path(path).read_text(encoding="utf-8")
        self.assertEqual(code, 2)
        self.assertIn("error", err)
        self.assertEqual(after, before)

    def test_done_json_mode(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_csv(["Clean dryer vent,2026-10-01,open"], tmp)
            code, out, _ = self.run_cli(
                ["--done", "Clean dryer vent", "--csv", path, "--json"]
            )
        self.assertEqual(code, 0)
        data = json.loads(out)
        self.assertEqual(data["marked"], "Clean dryer vent")
        self.assertEqual(data["line"], 2)

    def test_due_and_done_together_are_rejected(self):
        with self.assertRaises(SystemExit):
            self.run_cli(["--due", "2026-10-01", "--done", "Clean dryer vent"])

    def test_neither_mode_is_rejected(self):
        with self.assertRaises(SystemExit):
            self.run_cli([])

    def test_done_then_due_view_excludes_the_chore(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_csv(["Clean dryer vent,2026-10-01,open"], tmp)
            self.run_cli(["--done", "Clean dryer vent", "--csv", path])
            code, out, _ = self.run_cli(["--due", "2026-10-01", "--csv", path])
        self.assertEqual(code, 0)
        self.assertIn("No unfinished chores", out)


if __name__ == "__main__":
    unittest.main()

