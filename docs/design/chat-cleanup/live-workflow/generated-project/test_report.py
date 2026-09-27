import json
import os
import subprocess
import sys
import tempfile
import unittest
from decimal import Decimal
from pathlib import Path

import report

HERE = Path(__file__).resolve().parent


class Base(unittest.TestCase):
    def setUp(self):
        self._temp = []

    def tearDown(self):
        for path in self._temp:
            os.unlink(path)

    def make_csv(self, text):
        handle = tempfile.NamedTemporaryFile(
            "w", suffix=".csv", delete=False, encoding="utf-8"
        )
        handle.write(text)
        handle.close()
        self._temp.append(handle.name)
        return handle.name

    def run_cli(self, *argv):
        return subprocess.run(
            [sys.executable, "report.py", *argv],
            cwd=HERE,
            capture_output=True,
            text=True,
        )


class ReportTests(Base):
    def test_category_totals_sorted_by_name(self):
        path = self.make_csv(
            "date,category,amount\n"
            "2026-09-01,zebra,1.00\n"
            "2026-09-02,apple,2.00\n"
            "2026-09-03,apple,3.00\n"
        )
        ranked, grand, skipped = report.report(path)
        self.assertEqual(
            ranked, [("apple", Decimal("5.00")), ("zebra", Decimal("1.00"))]
        )
        self.assertEqual(grand, Decimal("6.00"))
        self.assertEqual(skipped, [])

    def test_decimal_addition_is_exact(self):
        # 0.10 + 0.20 is 0.30000000000000004 in binary floating point.
        path = self.make_csv(
            "date,category,amount\n2026-09-01,misc,0.10\n2026-09-02,misc,0.20\n"
        )
        ranked, grand, _ = report.report(path)
        self.assertEqual(ranked, [("misc", Decimal("0.30"))])
        self.assertEqual(grand, Decimal("0.30"))

    def test_negative_amount_is_a_refund(self):
        path = self.make_csv(
            "date,category,amount\n"
            "2026-09-01,food,12.50\n"
            "2026-09-02,food,-2.50\n"
            "2026-09-03,travel,25.00\n"
        )
        ranked, grand, skipped = report.report(path)
        self.assertEqual(ranked, [("food", Decimal("10.00")), ("travel", Decimal("25.00"))])
        self.assertEqual(grand, Decimal("35.00"))
        self.assertEqual(skipped, [])

    def test_prints_sorted_lines_and_grand_total(self):
        path = self.make_csv(
            "date,category,amount\n"
            "2026-09-01,travel,25.00\n"
            "2026-09-02,food,12.50\n"
            "2026-09-03,food,7.25\n"
            "2026-09-04,books,15.00\n"
        )
        ranked, grand, _ = report.report(path)
        self.assertEqual(
            report.format_text(ranked, grand),
            "books: 15.00\nfood: 19.75\ntravel: 25.00\ngrand total: 59.75",
        )

    def test_missing_column_is_rejected(self):
        path = self.make_csv("date,category\n2026-09-01,food\n")
        with self.assertRaises(ValueError):
            report.load_expenses(path)

    def test_missing_category_is_rejected(self):
        path = self.make_csv("date,category,amount\n2026-09-01,,1.00\n")
        with self.assertRaises(ValueError):
            report.load_expenses(path)

    def test_category_filter_is_case_insensitive(self):
        path = self.make_csv(
            "date,category,amount\n"
            "2026-09-01,food,12.50\n"
            "2026-09-02,Food,7.25\n"
            "2026-09-03,books,15.00\n"
        )
        ranked, grand, _ = report.report(path, "FOOD")
        self.assertEqual(
            ranked, [("Food", Decimal("7.25")), ("food", Decimal("12.50"))]
        )
        self.assertEqual(grand, Decimal("19.75"))

    def test_category_filter_with_no_match_is_empty(self):
        path = self.make_csv("date,category,amount\n2026-09-01,food,12.50\n")
        ranked, grand, _ = report.report(path, "nope")
        self.assertEqual(ranked, [])
        self.assertEqual(grand, Decimal("0.00"))

    def test_json_output_uses_decimal_strings(self):
        path = self.make_csv(
            "date,category,amount\n"
            "2026-09-01,travel,25.00\n"
            "2026-09-02,food,12.50\n"
            "2026-09-03,food,7.25\n"
            "2026-09-04,books,15.00\n"
        )
        ranked, grand, _ = report.report(path)
        payload = json.loads(report.format_json(ranked, grand))
        self.assertEqual(
            payload,
            {
                "categories": {"books": "15.00", "food": "19.75", "travel": "25.00"},
                "total": "59.75",
            },
        )

    def test_json_output_honours_filter(self):
        path = self.make_csv(
            "date,category,amount\n"
            "2026-09-01,food,12.50\n"
            "2026-09-02,Food,7.25\n"
            "2026-09-03,books,15.00\n"
        )
        ranked, grand, _ = report.report(path, "food")
        payload = json.loads(report.format_json(ranked, grand))
        self.assertEqual(payload["total"], "19.75")
        self.assertEqual(set(payload["categories"]), {"food", "Food"})


class BadAmountTests(Base):
    def test_invalid_amounts_are_skipped_and_named_by_line(self):
        path = self.make_csv(
            "date,category,amount\n"       # line 1 header
            "2026-09-01,food,12.50\n"      # line 2 valid
            "2026-09-02,food,abc\n"        # line 3 bad
            "2026-09-03,books,15.00\n"     # line 4 valid
            "2026-09-04,travel,NaN\n"      # line 5 bad
            "2026-09-05,travel,Infinity\n" # line 6 bad
            "2026-09-06,travel,-\n"        # line 7 bad
        )
        warnings = []
        ranked, grand, skipped = report.report(
            path, warn=lambda line, raw: warnings.append((line, raw))
        )
        # Only the valid rows are summed ...
        self.assertEqual(
            ranked, [("books", Decimal("15.00")), ("food", Decimal("12.50"))]
        )
        self.assertEqual(grand, Decimal("27.50"))
        # ... and each bad row is reported once, with its line number.
        self.assertEqual(
            warnings,
            [(3, "abc"), (5, "NaN"), (6, "Infinity"), (7, "-")],
        )
        self.assertEqual(skipped, warnings)

    def test_nan_and_infinity_are_invalid(self):
        for token in ("NaN", "nan", "Infinity", "inf", "-Infinity", "-inf"):
            self.assertIsNone(report.parse_amount(token), token)

    def test_finite_amounts_parse_including_negative(self):
        self.assertEqual(report.parse_amount("0.10"), Decimal("0.10"))
        self.assertEqual(report.parse_amount("-2.50"), Decimal("-2.50"))
        self.assertEqual(report.parse_amount("1000"), Decimal("1000"))

    def test_no_skips_reports_empty_skipped(self):
        path = self.make_csv("date,category,amount\n2026-09-01,food,1.00\n")
        _, _, skipped = report.report(path)
        self.assertEqual(skipped, [])

    def test_large_finite_amount_is_summed_not_crashed(self):
        # 29 significant digits: exceeds the default 28-digit decimal context,
        # which used to make quantize raise InvalidOperation.
        path = self.make_csv(
            "date,category,amount\n"
            "2026-09-01,food,99999999999999999999999999999\n"
            "2026-09-02,food,1.00\n"
        )
        ranked, grand, skipped = report.report(path)
        self.assertEqual(skipped, [])
        self.assertEqual(grand, Decimal("100000000000000000000000000000.00"))
        self.assertEqual(report.format_text(ranked, grand).splitlines()[-1],
                         "grand total: 100000000000000000000000000000.00")

    def test_scientific_large_amount_is_summed_not_crashed(self):
        path = self.make_csv("date,category,amount\n2026-09-01,food,1E+100\n")
        ranked, grand, skipped = report.report(path)
        self.assertEqual(skipped, [])
        self.assertEqual(grand, Decimal("1E+100"))

    def mixed_month_csv(self):
        return self.make_csv(
            "date,category,amount\n"
            "2026-09-10,travel,120.00\n"
            "2026-09-11,food,18.40\n"
            "2026-09-12,food,-3.40\n"
            "2026-09-13,books,25.00\n"
            "2026-10-02,travel,80.00\n"
            "2026-10-05,food,42.50\n"
            "2026-10-09,books,10.00\n"
        )

    def test_month_filter_separates_months(self):
        path = self.mixed_month_csv()
        ranked, grand, _ = report.report(path, month="2026-09")
        self.assertEqual(
            ranked,
            [
                ("books", Decimal("25.00")),
                ("food", Decimal("15.00")),
                ("travel", Decimal("120.00")),
            ],
        )
        self.assertEqual(grand, Decimal("160.00"))
        ranked, grand, _ = report.report(path, month="2026-10")
        self.assertEqual(
            ranked,
            [
                ("books", Decimal("10.00")),
                ("food", Decimal("42.50")),
                ("travel", Decimal("80.00")),
            ],
        )
        self.assertEqual(grand, Decimal("132.50"))

    def test_month_and_category_filters_combine(self):
        ranked, grand, _ = report.report(self.mixed_month_csv(), "food", "2026-10")
        self.assertEqual(ranked, [("food", Decimal("42.50"))])
        self.assertEqual(grand, Decimal("42.50"))

    def test_unreadable_or_impossible_date_is_reported_not_dropped(self):
        # A malformed date and a well-formed but impossible date must both be
        # reported (line + value) rather than vanishing under --month.
        path = self.make_csv(
            "date,category,amount\n"
            "2026-09-01,food,1.00\n"
            "not-a-date,food,2.00\n"
            "2026-02-30,food,4.00\n"
        )
        warnings = []
        ranked, grand, skipped = report.report(
            path,
            month="2026-09",
            warn_date=lambda line, raw: warnings.append((line, raw)),
        )
        self.assertEqual(ranked, [("food", Decimal("1.00"))])
        self.assertEqual(grand, Decimal("1.00"))
        self.assertEqual(warnings, [(3, "not-a-date"), (4, "2026-02-30")])
        self.assertEqual(skipped, [(3, "not-a-date"), (4, "2026-02-30")])

    def test_date_is_ignored_without_month_filter(self):
        path = self.make_csv("date,category,amount\nnot-a-date,food,3.00\n")
        ranked, grand, skipped = report.report(path)
        self.assertEqual(ranked, [("food", Decimal("3.00"))])
        self.assertEqual(grand, Decimal("3.00"))
        self.assertEqual(skipped, [])


class CliTests(Base):
    def fixture_csv(self):
        return self.make_csv(
            "date,category,amount\n"
            "2026-09-01,travel,25.00\n"
            "2026-09-02,food,12.50\n"
            "2026-09-03,food,7.25\n"
            "2026-09-04,books,15.00\n"
        )

    def test_runs_on_csv_file(self):
        result = self.run_cli(self.fixture_csv())
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, "")
        self.assertEqual(
            result.stdout.strip(),
            "books: 15.00\nfood: 19.75\ntravel: 25.00\ngrand total: 59.75",
        )

    def test_category_filter_cli(self):
        result = self.run_cli(self.fixture_csv(), "--category", "FOOD")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "food: 19.75\ngrand total: 19.75")

    def test_json_cli_options(self):
        result = self.run_cli(self.fixture_csv(), "--json")
        self.assertEqual(result.returncode, 0, result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(payload["total"], "59.75")
        self.assertEqual(payload["categories"]["food"], "19.75")

    def test_csv_cli_options(self):
        result = self.run_cli(self.fixture_csv(), "--csv")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stdout.strip(),
            "category,amount\n"
            "books,15.00\n"
            "food,19.75\n"
            "travel,25.00\n"
            "TOTAL,59.75",
        )

    def test_json_and_csv_are_mutually_exclusive(self):
        result = self.run_cli(self.fixture_csv(), "--json", "--csv")
        self.assertEqual(result.returncode, 2)
        self.assertIn("not allowed with", result.stderr)

    def test_month_filter_cli(self):
        result = self.run_cli(self.fixture_csv(), "--month", "2026-09", "--csv")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stdout.strip(),
            "category,amount\n"
            "books,15.00\n"
            "food,19.75\n"
            "travel,25.00\n"
            "TOTAL,59.75",
        )

    def test_malformed_month_argument_is_rejected(self):
        result = self.run_cli(self.fixture_csv(), "--month", "2026-13")
        self.assertEqual(result.returncode, 2)
        self.assertIn("YYYY-MM", result.stderr)

    def test_bad_date_under_month_warns_on_stderr_and_exits_2(self):
        path = self.make_csv(
            "date,category,amount\n"
            "2026-09-01,food,1.00\n"
            "2026-02-30,food,4.00\n"
        )
        result = self.run_cli(path, "--month", "2026-09", "--csv")
        self.assertEqual(result.returncode, 2)
        self.assertIn(":3:", result.stderr)
        self.assertEqual(
            result.stdout.strip(), "category,amount\nfood,1.00\nTOTAL,1.00"
        )

    def test_strict_withholds_report_when_a_row_is_skipped(self):
        path = self.make_csv(
            "date,category,amount\n"
            "2026-10-05,food,42.50\n"
            "2026-10-12,food,inf\n"
        )
        result = self.run_cli(path, "--strict", "--csv")
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, "")
        self.assertIn("skipping invalid amount 'inf'", result.stderr)

    def test_strict_withholds_report_when_a_date_is_unreadable(self):
        path = self.make_csv(
            "date,category,amount\n"
            "2026-10-05,food,42.50\n"
            "not-a-date,food,1.00\n"
        )
        result = self.run_cli(path, "--strict", "--month", "2026-10", "--csv")
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, "")
        self.assertIn("unreadable date", result.stderr)

    def test_strict_emits_report_for_clean_data(self):
        result = self.run_cli(self.fixture_csv(), "--strict", "--csv")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, "")
        self.assertEqual(
            result.stdout.strip(),
            "category,amount\n"
            "books,15.00\n"
            "food,19.75\n"
            "travel,25.00\n"
            "TOTAL,59.75",
        )

    def test_non_strict_still_emits_partial_report(self):
        path = self.make_csv(
            "date,category,amount\n"
            "2026-10-05,food,42.50\n"
            "2026-10-12,food,inf\n"
        )
        result = self.run_cli(path, "--csv")
        self.assertEqual(result.returncode, 2)
        self.assertEqual(
            result.stdout.strip(), "category,amount\nfood,42.50\nTOTAL,42.50"
        )

    def test_skipped_rows_warn_and_exit_2(self):
        path = self.make_csv(
            "date,category,amount\n"
            "2026-09-01,food,12.50\n"
            "2026-09-02,books,abc\n"
            "2026-09-03,travel,Infinity\n"
        )
        result = self.run_cli(path, "--json")
        self.assertEqual(result.returncode, 2, result.stderr)
        # Concise warnings on stderr, one per skipped row, with its line number.
        self.assertIn("skipping invalid amount 'abc'", result.stderr)
        self.assertIn("skipping invalid amount 'Infinity'", result.stderr)
        self.assertIn(":3:", result.stderr)
        self.assertIn(":4:", result.stderr)
        # stdout stays pure JSON covering the valid rows only.
        payload = json.loads(result.stdout)
        self.assertEqual(payload, {"categories": {"food": "12.50"}, "total": "12.50"})

    def test_clean_file_exits_0(self):
        path = self.make_csv("date,category,amount\n2026-09-01,food,1.00\n")
        result = self.run_cli(path)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, "")

    def test_large_amount_cli_exits_0(self):
        path = self.make_csv(
            "date,category,amount\n2026-09-01,food,99999999999999999999999999999\n"
        )
        result = self.run_cli(path, "--json")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, "")
        self.assertEqual(
            json.loads(result.stdout)["total"],
            "99999999999999999999999999999.00",
        )


if __name__ == "__main__":
    unittest.main()
