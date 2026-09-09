"""The record rules: ranges, statuses, and identifiers that must be unique."""
import unittest

from dutylog import parsing, validation


def raw(line_no, entry_id, start="2026-12-28T08:00", worker="ana",
        site="Depot", minutes="60", status="logged"):
    return parsing.RawEntry(line_no, entry_id, start, worker, site, minutes, status)


class ValidationTest(unittest.TestCase):
    def test_a_clean_log_produces_no_errors(self):
        checked = validation.validate([raw(2, "E-1"), raw(3, "E-2")])
        self.assertEqual(checked.errors, [])
        self.assertEqual([e.entry_id for e in checked.entries], ["E-1", "E-2"])

    def test_a_void_record_is_dropped_and_is_not_an_error(self):
        checked = validation.validate([raw(2, "E-1", status="void"), raw(3, "E-2")])
        self.assertEqual(checked.errors, [])
        self.assertEqual([e.entry_id for e in checked.entries], ["E-2"])

    def test_minutes_out_of_range(self):
        checked = validation.validate([raw(2, "E-1", minutes="0"), raw(3, "E-2", minutes="1441")])
        self.assertEqual(checked.errors,
                         ["line 2: minutes out of range", "line 3: minutes out of range"])

    def test_an_adjacent_repeat_of_an_identifier_is_an_error(self):
        checked = validation.validate([raw(2, "E-1"), raw(3, "E-1")])
        self.assertEqual(checked.errors, ["line 3: duplicate entry_id"])

    def test_an_identifier_repeated_later_in_the_file_is_an_error(self):
        checked = validation.validate([raw(2, "E-1"), raw(3, "E-2"), raw(4, "E-1")])
        self.assertEqual(checked.errors, ["line 4: duplicate entry_id"])
        self.assertEqual([e.entry_id for e in checked.entries], ["E-1", "E-2"])


if __name__ == "__main__":
    unittest.main()
