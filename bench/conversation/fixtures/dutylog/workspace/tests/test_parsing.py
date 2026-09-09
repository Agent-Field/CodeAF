"""The file format: quoting, the byte order mark, blank lines, line numbers."""
import unittest

from dutylog import parsing

LOG = (
    "\ufeff"
    "entry_id,start,worker,site,minutes,status\n"
    "E-101,2026-12-28T08:00,ana,\"Harbour, East\",120,logged\n"
    "\n"
    "E-102,2026-12-29T09:30,ben,Depot,45,logged\n"
)


class ParsingTest(unittest.TestCase):
    def test_reads_every_record(self):
        entries = parsing.parse_entries(LOG)
        self.assertEqual([e.entry_id for e in entries], ["E-101", "E-102"])

    def test_a_quoted_field_may_contain_a_comma(self):
        entries = parsing.parse_entries(LOG)
        self.assertEqual(entries[0].site, "Harbour, East")

    def test_a_byte_order_mark_is_not_part_of_the_header(self):
        # The same file without the mark must parse the same way.
        self.assertEqual([e.entry_id for e in parsing.parse_entries(LOG.lstrip("\ufeff"))],
                         [e.entry_id for e in parsing.parse_entries(LOG)])

    def test_line_numbers_survive_blank_lines(self):
        entries = parsing.parse_entries(LOG)
        self.assertEqual([e.line_no for e in entries], [2, 4])

    def test_a_file_with_the_wrong_header_is_refused(self):
        with self.assertRaises(parsing.ParseError):
            parsing.parse_entries("id,when,who\n1,2,3\n")


if __name__ == "__main__":
    unittest.main()
