"""Tests for the pantry stock-report CLI.

Run with: python3 -m unittest -v test_pantry.py
"""

import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "pantry.py")


def run_cli(csv_path, *extra):
    """Invoke the CLI as a subprocess and return (returncode, stdout, stderr)."""
    proc = subprocess.run(
        [sys.executable, SCRIPT, csv_path, *extra],
        capture_output=True,
        text=True,
    )
    return proc.returncode, proc.stdout, proc.stderr


class PantryCliTests(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.dir = self._tmp.name

    def write_csv(self, text, name="data.csv"):
        path = os.path.join(self.dir, name)
        with open(path, "w", newline="") as fh:
            fh.write(text)
        return path

    def test_strict_below_boundary(self):
        # threshold default 3: 2 in, 3 out, 4 out
        path = self.write_csv("item,quantity\nlow,2\nequal,3\nhigh,4\n")
        rc, out, err = run_cli(path)
        self.assertEqual(rc, 0, err)
        self.assertEqual(out, "low: 2\n")

    def test_alphabetical_ordering(self):
        path = self.write_csv("item,quantity\nzebra,1\napple,0\nmango,2\n")
        rc, out, err = run_cli(path)
        self.assertEqual(rc, 0, err)
        self.assertEqual(out, "apple: 0\nmango: 2\nzebra: 1\n")

    def test_default_threshold_is_three(self):
        path = self.write_csv("item,quantity\nbelow,2\nat,3\n")
        rc, out, err = run_cli(path)
        self.assertEqual(rc, 0, err)
        self.assertEqual(out, "below: 2\n")

    def test_custom_threshold(self):
        path = self.write_csv("item,quantity\na,2\nb,3\nc,5\n")
        rc, out, err = run_cli(path, "--threshold", "5")
        self.assertEqual(rc, 0, err)
        self.assertEqual(out, "a: 2\nb: 3\n")

    def test_malformed_quantity_nonzero_and_no_stdout(self):
        path = self.write_csv("item,quantity\nok,1\nbad,abc\n")
        rc, out, err = run_cli(path)
        self.assertEqual(rc, 2)
        self.assertEqual(out, "")
        self.assertIn("line 3", err)

    def test_malformed_row_three_fields(self):
        path = self.write_csv("item,quantity\nok,1\nextra,2,3\n")
        rc, out, err = run_cli(path)
        self.assertEqual(rc, 2)
        self.assertEqual(out, "")
        self.assertIn("line 3", err)

    def test_bad_header_is_error(self):
        path = self.write_csv("name,count\na,1\n")
        rc, out, err = run_cli(path)
        self.assertEqual(rc, 2)
        self.assertEqual(out, "")
        self.assertIn("header", err.lower())

    def test_missing_file_is_error(self):
        path = os.path.join(self.dir, "does_not_exist.csv")
        rc, out, err = run_cli(path)
        self.assertEqual(rc, 2)
        self.assertEqual(out, "")
        self.assertNotEqual(err.strip(), "")

    def test_blank_item_is_error(self):
        path = self.write_csv("item,quantity\nok,1\n,5\n")
        rc, out, err = run_cli(path)
        self.assertEqual(rc, 2)
        self.assertEqual(out, "")
        self.assertIn("line 3", err)

    def test_invalid_threshold_is_error(self):
        path = self.write_csv("item,quantity\na,1\n")
        rc, out, err = run_cli(path, "--threshold", "abc")
        self.assertEqual(rc, 2)
        self.assertEqual(out, "")
        self.assertNotEqual(err.strip(), "")

    def test_empty_result_exits_zero_and_empty_stdout(self):
        path = self.write_csv("item,quantity\na,9\nb,8\n")
        rc, out, err = run_cli(path)
        self.assertEqual(rc, 0, err)
        self.assertEqual(out, "")

    def test_input_csv_unchanged(self):
        text = "item,quantity\nrice,2\nbeans,0\ntea,5\n"
        path = self.write_csv(text)
        with open(path, "rb") as fh:
            before = fh.read()
        rc, _out, err = run_cli(path)
        self.assertEqual(rc, 0, err)
        with open(path, "rb") as fh:
            after = fh.read()
        self.assertEqual(before, after)

    def test_shopping_prints_buy_amounts(self):
        path = self.write_csv("item,quantity\nrice,2\nbeans,0\ntea,5\n")
        rc, out, err = run_cli(path, "--shopping")
        self.assertEqual(rc, 0, err)
        self.assertEqual(out, "beans: buy 3\nrice: buy 1\n")

    def test_shopping_custom_threshold(self):
        # threshold 5: a(2)->3, b(3)->2, c(5) excluded
        path = self.write_csv("item,quantity\na,2\nb,3\nc,5\n")
        rc, out, err = run_cli(path, "--shopping", "--threshold", "5")
        self.assertEqual(rc, 0, err)
        self.assertEqual(out, "a: buy 3\nb: buy 2\n")

    def test_shopping_alphabetical(self):
        path = self.write_csv("item,quantity\nzebra,1\napple,0\nmango,2\n")
        rc, out, err = run_cli(path, "--shopping")
        self.assertEqual(rc, 0, err)
        self.assertEqual(out, "apple: buy 3\nmango: buy 1\nzebra: buy 2\n")

    def test_default_output_unchanged_without_flag(self):
        path = self.write_csv("item,quantity\nrice,2\nbeans,0\ntea,5\n")
        rc, out, err = run_cli(path)
        self.assertEqual(rc, 0, err)
        self.assertEqual(out, "beans: 0\nrice: 2\n")

    def test_negative_quantity_rejected_default_mode(self):
        path = self.write_csv("item,quantity\nok,1\nrice,-1\n")
        rc, out, err = run_cli(path)
        self.assertEqual(rc, 2)
        self.assertEqual(out, "")
        self.assertIn("line 3", err)
        self.assertIn("-1", err)

    def test_negative_quantity_rejected_shopping_mode(self):
        path = self.write_csv("item,quantity\nok,1\nrice,-1\n")
        rc, out, err = run_cli(path, "--shopping")
        self.assertEqual(rc, 2)
        self.assertEqual(out, "")
        self.assertIn("line 3", err)
        self.assertIn("-1", err)

    def test_input_csv_unchanged_after_shopping(self):
        text = "item,quantity\nrice,2\nbeans,0\ntea,5\n"
        path = self.write_csv(text)
        with open(path, "rb") as fh:
            before = fh.read()
        rc, _out, err = run_cli(path, "--shopping")
        self.assertEqual(rc, 0, err)
        with open(path, "rb") as fh:
            after = fh.read()
        self.assertEqual(before, after)


if __name__ == "__main__":
    unittest.main()
