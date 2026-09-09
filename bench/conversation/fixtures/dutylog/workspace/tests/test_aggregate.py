"""The window and the week: both are boundaries, and both are inclusive rules."""
from datetime import date, datetime
import unittest

from dutylog import aggregate, validation


def entry(entry_id, start, site="Depot", minutes=60):
    return validation.Entry(2, entry_id, datetime.fromisoformat(start), "ana", site, minutes)


class AggregateTest(unittest.TestCase):
    def test_the_window_includes_its_last_day(self):
        entries = [entry("E-1", "2026-12-21T08:00"), entry("E-2", "2026-12-27T08:00")]
        totals = aggregate.weekly_totals(entries, date(2026, 12, 21), date(2026, 12, 27))
        self.assertEqual(sum(sum(s.values()) for s in totals.values()), 120)

    def test_an_entry_after_the_window_is_left_out(self):
        entries = [entry("E-1", "2026-12-28T08:00")]
        self.assertEqual(aggregate.weekly_totals(entries, date(2026, 12, 21), date(2026, 12, 27)), {})

    def test_the_last_days_of_december_belong_to_the_iso_year_of_their_week(self):
        # Monday 2025-12-29 through Sunday 2026-01-04 are one ISO week: 2026-W01.
        entries = [entry("E-1", "2025-12-29T08:00"), entry("E-2", "2026-01-02T08:00")]
        totals = aggregate.weekly_totals(entries, date(2025, 12, 29), date(2026, 1, 4))
        self.assertEqual(totals, {"2026-W01": {"Depot": 120}})

    def test_sites_are_totalled_separately(self):
        entries = [entry("E-1", "2027-01-05T08:00", site="Annex", minutes=30),
                   entry("E-2", "2027-01-06T08:00", site="Depot", minutes=45)]
        totals = aggregate.weekly_totals(entries, date(2027, 1, 4), date(2027, 1, 10))
        self.assertEqual(totals, {"2027-W01": {"Annex": 30, "Depot": 45}})


if __name__ == "__main__":
    unittest.main()
