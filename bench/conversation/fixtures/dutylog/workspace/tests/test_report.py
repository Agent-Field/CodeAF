"""The report is a function of the totals, and of nothing else."""
import unittest

from dutylog import report


class ReportTest(unittest.TestCase):
    def test_hours_round_halves_up(self):
        self.assertEqual(str(report.hours(15)), "0.3")
        self.assertEqual(str(report.hours(120)), "2.0")

    def test_the_total_line_is_computed_from_the_minutes(self):
        # 15 + 15 minutes is 0.5 hours, not the 0.6 that two rounded rows sum to.
        rendered = report.render({"2027-W01": {"Annex": 15, "Depot": 15}})
        self.assertTrue(rendered.endswith("0.5\n"), rendered)

    def test_a_bigger_total_comes_first(self):
        rendered = report.render({"2027-W01": {"Annex": 30, "Depot": 90}})
        self.assertLess(rendered.index("Depot"), rendered.index("Annex"))

    def test_equal_totals_do_not_depend_on_insertion_order(self):
        one = report.render({"2027-W01": {"Annex": 60, "Depot": 60}})
        other = report.render({"2027-W01": {"Depot": 60, "Annex": 60}})
        self.assertEqual(one, other)


if __name__ == "__main__":
    unittest.main()
