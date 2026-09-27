"""Unittest suite for shopping_list.py."""

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import shopping_list
from shopping_list import PantryError, build_report, generate, read_pantry

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / "shopping_list.py"


def write_pantry(directory, text, name="pantry.csv"):
    path = Path(directory) / name
    path.write_text(text, encoding="utf-8")
    return path


class ReadPantryTests(unittest.TestCase):
    def test_reads_items_and_quantities(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_pantry(tmp, "item,quantity\nrice,2\nbeans,0\ntea,5\n")
            self.assertEqual(
                read_pantry(path), [("rice", 2), ("beans", 0), ("tea", 5)]
            )

    def test_rejects_negative_quantity_with_clear_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_pantry(tmp, "item,quantity\nrice,-1\n")
            with self.assertRaises(PantryError) as ctx:
                read_pantry(path)
            message = str(ctx.exception)
            self.assertIn("negative", message)
            self.assertIn("rice", message)

    def test_rejects_non_integer_quantity(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_pantry(tmp, "item,quantity\nrice,many\n")
            with self.assertRaises(PantryError):
                read_pantry(path)

    def test_rejects_wrong_header(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = write_pantry(tmp, "name,count\nrice,2\n")
            with self.assertRaises(PantryError):
                read_pantry(path)


class BuildReportTests(unittest.TestCase):
    def test_reports_the_corrected_target_of_six(self):
        items = [("rice", 2), ("beans", 0), ("tea", 5)]
        report = build_report(items, target=6)
        self.assertIn("tea: buy 1", report)
        self.assertIn("rice: buy 4", report)
        self.assertIn("beans: buy 6", report)

    def test_ordered_closest_to_target_first(self):
        items = [("rice", 2), ("beans", 0), ("tea", 5)]
        lines = [
            line
            for line in build_report(items, target=6).splitlines()
            if ": buy " in line
        ]
        self.assertEqual(lines, ["tea: buy 1", "rice: buy 4", "beans: buy 6"])

    def test_item_at_or_above_target_is_omitted(self):
        report = build_report([("tea", 5), ("salt", 6), ("honey", 9)], target=6)
        self.assertNotIn("salt", report)
        self.assertNotIn("honey", report)
        self.assertIn("tea: buy 1", report)

    def test_empty_list_says_nothing_to_buy(self):
        self.assertIn("Nothing to buy.", build_report([], target=6))


class GenerateTests(unittest.TestCase):
    def test_writes_shopping_md(self):
        with tempfile.TemporaryDirectory() as tmp:
            write_pantry(tmp, "item,quantity\nrice,2\nbeans,0\ntea,5\n")
            out = Path(tmp) / "SHOPPING.md"
            generate(Path(tmp) / "pantry.csv", out)
            text = out.read_text(encoding="utf-8")
            self.assertIn("tea: buy 1", text)
            self.assertIn("rice: buy 4", text)
            self.assertIn("beans: buy 6", text)

    def test_invalid_input_preserves_last_good_report(self):
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "SHOPPING.md"
            write_pantry(tmp, "item,quantity\nrice,2\n")
            generate(Path(tmp) / "pantry.csv", out)
            good = out.read_text(encoding="utf-8")
            write_pantry(tmp, "item,quantity\nrice,-3\n")
            with self.assertRaises(PantryError):
                generate(Path(tmp) / "pantry.csv", out)
            self.assertEqual(out.read_text(encoding="utf-8"), good)


class CommandLineTests(unittest.TestCase):
    def run_cli(self, cwd):
        return subprocess.run(
            [sys.executable, str(SCRIPT)],
            cwd=cwd,
            capture_output=True,
            text=True,
        )

    def test_cli_writes_report_and_exits_zero(self):
        with tempfile.TemporaryDirectory() as tmp:
            write_pantry(tmp, "item,quantity\nrice,2\nbeans,0\ntea,5\n")
            result = self.run_cli(tmp)
            self.assertEqual(result.returncode, 0, result.stderr)
            text = (Path(tmp) / "SHOPPING.md").read_text(encoding="utf-8")
            self.assertIn("beans: buy 6", text)

    def test_cli_reports_negative_quantity_and_keeps_last_report(self):
        with tempfile.TemporaryDirectory() as tmp:
            write_pantry(tmp, "item,quantity\nrice,2\n")
            self.assertEqual(self.run_cli(tmp).returncode, 0)
            good = (Path(tmp) / "SHOPPING.md").read_text(encoding="utf-8")
            write_pantry(tmp, "item,quantity\nbeans,-6\n")
            result = self.run_cli(tmp)
            self.assertEqual(result.returncode, 1)
            self.assertIn("negative", result.stderr)
            self.assertIn("beans", result.stderr)
            self.assertEqual(
                (Path(tmp) / "SHOPPING.md").read_text(encoding="utf-8"), good
            )

    def test_cli_missing_pantry_reports_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = self.run_cli(tmp)
            self.assertEqual(result.returncode, 1)
            self.assertIn("error:", result.stderr)


if __name__ == "__main__":
    unittest.main()
