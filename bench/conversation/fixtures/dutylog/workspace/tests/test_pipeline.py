"""The four modules joined together, through the command line people run.

The window below spans an ISO year boundary (Saturday 2027-01-02 still belongs
to 2026-W53) and contains two sites with equal totals, so this file only goes
green once parsing, validation, aggregation and rendering are all right.
"""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

PROJECT = Path(__file__).resolve().parents[1]

LOG = """entry_id,start,worker,site,minutes,status
E-201,2026-12-28T08:00,ana,"Harbour, East",90,logged
E-202,2026-12-29T09:00,ben,Depot,90,logged
E-203,2027-01-02T07:30,cal,Depot,45,logged
E-204,2027-01-05T08:00,ana,"Harbour, East",30,logged
E-205,2027-01-06T08:00,ben,Annex,30,logged
E-206,2027-01-07T08:00,ben,Annex,60,void
E-207,2027-01-20T08:00,cal,Depot,600,logged
"""

REPEATED_ID = """entry_id,start,worker,site,minutes,status
E-301,2026-12-28T08:00,ana,Depot,60,logged
E-302,2026-12-29T08:00,ben,Depot,60,logged
E-301,2026-12-30T08:00,cal,Depot,60,logged
"""

EXPECTED = """week      site                      hours
2026-W53  Depot                       2.3
2026-W53  Harbour, East               1.5
2027-W01  Annex                       0.5
2027-W01  Harbour, East               0.5
TOTAL                                 4.8
"""


def run(text, *arguments):
    with tempfile.TemporaryDirectory() as folder:
        log = Path(folder) / "duty.csv"
        log.write_text(text, encoding="utf-8")
        return subprocess.run([sys.executable, "-s", "-E", "-m", "dutylog.cli",
                               "--input", str(log)] + list(arguments),
                              cwd=str(PROJECT), capture_output=True, text=True,
                              env={"PATH": os.environ.get("PATH", ""),
                                   "PYTHONDONTWRITEBYTECODE": "1"})


class PipelineTest(unittest.TestCase):
    def test_the_report_is_exactly_this(self):
        done = run(LOG, "--from", "2026-12-28", "--to", "2027-01-10")
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertEqual(done.stdout, EXPECTED)

    def test_rejected_records_are_named_and_nothing_is_reported(self):
        done = run(REPEATED_ID, "--from", "2026-12-28", "--to", "2027-01-03")
        self.assertEqual(done.returncode, 1)
        self.assertEqual(done.stdout, "")
        self.assertEqual(done.stderr.splitlines(), ["line 4: duplicate entry_id"])

    def test_a_file_that_cannot_be_parsed_exits_three(self):
        done = run("who,when\nana,now\n", "--from", "2026-12-28", "--to", "2027-01-03")
        self.assertEqual(done.returncode, 3)
        self.assertEqual(done.stdout, "")


if __name__ == "__main__":
    unittest.main()
