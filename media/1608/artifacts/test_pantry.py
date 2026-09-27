"""Tests for pantry.py. Standard library unittest only.

Chosen bad-input behaviour (stated here as required): a missing file, a wrong
header, a row with the wrong number of columns, or a non-numeric quantity make
pantry.py print a message to stderr and exit with status 1. Nothing is printed
to stdout in those cases.

Output contract under test: items strictly below the threshold (default 3),
sorted A-Z; each row carries ``buy`` = threshold - quantity; plain output is
``<item>: <buy>`` per line; ``--json`` is a list of ``{"item", "quantity", "buy"}``.
Whole numbers print without a decimal point; fractional quantities are allowed.
"""

import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

REPO_ROOT = os.path.dirname(os.path.abspath(__file__))

import pantry  # noqa: E402  (repo root on sys.path when run from there)


FIXTURE = "item,quantity\nrice,2\nbeans,0\ntea,5\n"


def write_csv(text):
    handle = tempfile.NamedTemporaryFile(
        "w", suffix=".csv", delete=False, newline=""
    )
    handle.write(text)
    handle.close()
    return handle.name


class RunCli(unittest.TestCase):
    """Exercise main() the way the command line does."""

    def setUp(self):
        self._dir = tempfile.TemporaryDirectory()
        self.addCleanup(self._dir.cleanup)
        self.csv = os.path.join(self._dir.name, "pantry.csv")
        with open(self.csv, "w", newline="") as handle:
            handle.write(FIXTURE)

    def run_main(self, argv):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            try:
                code = pantry.main(argv)
            except SystemExit as exc:
                code = exc.code
        return code, out.getvalue(), err.getvalue()


class TestFixtureBehaviour(RunCli):
    def test_default_threshold_excludes_equal_and_above(self):
        code, out, err = self.run_main([self.csv])
        self.assertEqual(code, 0)
        self.assertEqual(out, "beans: 3\nrice: 1\n")
        self.assertEqual(err, "")

    def test_quantity_equal_to_threshold_is_excluded(self):
        path = write_csv("item,quantity\neggs,3\nmilk,2\n")
        self.addCleanup(os.unlink, path)
        code, out, _ = self.run_main([path])
        self.assertEqual(code, 0)
        self.assertEqual(out, "milk: 1\n")  # eggs (3) is NOT strictly below 3

    def test_custom_threshold_changes_the_set_and_buy(self):
        code, out, _ = self.run_main([self.csv, "--threshold", "5"])
        self.assertEqual(code, 0)
        self.assertEqual(out, "beans: 5\nrice: 3\n")  # tea(5) still excluded

        code, out, _ = self.run_main([self.csv, "--threshold", "6"])
        self.assertEqual(code, 0)
        self.assertEqual(out, "beans: 6\nrice: 4\ntea: 1\n")

    def test_alphabetical_ordering(self):
        path = write_csv("item,quantity\nzucchini,0\napple,1\nmango,2\n")
        self.addCleanup(os.unlink, path)
        code, out, _ = self.run_main([path])
        self.assertEqual(code, 0)
        self.assertEqual(out, "apple: 2\nmango: 1\nzucchini: 3\n")


class TestBuyAmount(RunCli):
    def test_buy_is_threshold_minus_quantity(self):
        path = write_csv("item,quantity\nflour,0.5\nsugar,2\n")
        self.addCleanup(os.unlink, path)
        code, out, _ = self.run_main([path])
        self.assertEqual(code, 0)
        self.assertEqual(out, "flour: 2.5\nsugar: 1\n")


class TestFractionalQuantities(RunCli):
    def test_fractional_quantity_below_threshold(self):
        path = write_csv("item,quantity\nmilk,1.5\n")
        self.addCleanup(os.unlink, path)
        code, out, _ = self.run_main([path])
        self.assertEqual(code, 0)
        self.assertEqual(out, "milk: 1.5\n")

    def test_fractional_threshold(self):
        path = write_csv("item,quantity\neggs,2\n")
        self.addCleanup(os.unlink, path)
        code, out, _ = self.run_main([path, "--threshold", "2.5"])
        self.assertEqual(code, 0)
        self.assertEqual(out, "eggs: 0.5\n")

    def test_whole_valued_decimal_prints_without_point(self):
        path = write_csv("item,quantity\nrice,2.0\n")
        self.addCleanup(os.unlink, path)
        code, out, _ = self.run_main([path])
        self.assertEqual(code, 0)
        self.assertEqual(out, "rice: 1\n")


class TestJsonOutput(RunCli):
    def test_exact_json_shape_and_order(self):
        code, out, err = self.run_main([self.csv, "--json"])
        self.assertEqual(code, 0)
        self.assertEqual(
            out,
            '[{"item": "beans", "quantity": 0, "buy": 3}, '
            '{"item": "rice", "quantity": 2, "buy": 1}]\n',
        )
        self.assertEqual(err, "")

    def test_json_round_trip_types_and_values(self):
        _, out, _ = self.run_main([self.csv, "--json"])
        rows = json.loads(out)
        self.assertEqual(
            rows,
            [{"item": "beans", "quantity": 0, "buy": 3},
             {"item": "rice", "quantity": 2, "buy": 1}],
        )
        for row in rows:
            self.assertIsInstance(row["item"], str)
            self.assertIsInstance(row["quantity"], int)
            self.assertIsInstance(row["buy"], int)

    def test_json_fractional_is_float(self):
        path = write_csv("item,quantity\nmilk,1.5\n")
        self.addCleanup(os.unlink, path)
        _, out, _ = self.run_main([path, "--json"])
        rows = json.loads(out)
        self.assertEqual(rows, [{"item": "milk", "quantity": 1.5, "buy": 1.5}])


class TestBadInput(RunCli):
    def test_missing_file_exits_nonzero_with_stderr_message(self):
        missing = os.path.join(self._dir.name, "nope.csv")
        code, out, err = self.run_main([missing])
        self.assertNotEqual(code, 0)
        self.assertEqual(out, "")
        self.assertIn("pantry:", err)

    def test_malformed_quantity_row_exits_nonzero(self):
        path = write_csv("item,quantity\nrice,2\nbeans,lots\n")
        self.addCleanup(os.unlink, path)
        code, out, err = self.run_main([path])
        self.assertNotEqual(code, 0)
        self.assertEqual(out, "")
        self.assertIn("beans", err)

    def test_wrong_column_count_row_exits_nonzero(self):
        path = write_csv("item,quantity\nrice,2,3\n")
        self.addCleanup(os.unlink, path)
        code, out, err = self.run_main([path])
        self.assertNotEqual(code, 0)
        self.assertEqual(out, "")
        self.assertIn("2 columns", err)


class TestSubprocessOnRealFixture(unittest.TestCase):
    """End-to-end: the real pantry.csv through the real interpreter."""

    def run_cli(self, *args):
        return subprocess.run(
            [sys.executable, "pantry.py", *args],
            cwd=REPO_ROOT,
            capture_output=True,
            text=True,
        )

    def test_plain(self):
        result = self.run_cli("pantry.csv")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "beans: 3\nrice: 1\n")

    def test_json(self):
        result = self.run_cli("pantry.csv", "--json")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(
            result.stdout,
            '[{"item": "beans", "quantity": 0, "buy": 3}, '
            '{"item": "rice", "quantity": 2, "buy": 1}]\n',
        )


if __name__ == "__main__":
    unittest.main()
