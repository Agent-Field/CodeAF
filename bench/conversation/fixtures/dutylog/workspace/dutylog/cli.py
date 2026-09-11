"""The pipeline: read, check, total, render.

    python3 -m dutylog.cli --input LOG.csv --from 2026-12-21 --to 2027-01-03

Exit status is the whole point of a pipeline in a cron job, so it is explicit:
0 a report, 1 rejected records (named on stderr), 3 a file that could not be
parsed at all. Nothing goes to stdout unless the report does.
"""
import argparse
from datetime import date
import sys

from . import aggregate
from . import parsing
from . import report
from . import validation


def build_parser():
    parser = argparse.ArgumentParser(prog="dutylog", description=__doc__.splitlines()[0])
    parser.add_argument("--input", required=True, help="duty log CSV")
    parser.add_argument("--from", dest="window_start", required=True,
                        type=date.fromisoformat, help="first day of the window (inclusive)")
    parser.add_argument("--to", dest="window_end", required=True,
                        type=date.fromisoformat, help="last day of the window (inclusive)")
    return parser


def main(argv=None):
    parser = build_parser()
    args = parser.parse_args(argv)
    if args.window_start > args.window_end:
        parser.error("--from is after --to")
    try:
        raw_entries = parsing.read_entries(args.input)
    except (OSError, parsing.ParseError) as problem:
        print("cannot read %s: %s" % (args.input, problem), file=sys.stderr)
        return 3
    checked = validation.validate(raw_entries)
    if checked.errors:
        for message in checked.errors:
            print(message, file=sys.stderr)
        return 1
    totals = aggregate.weekly_totals(checked.entries, args.window_start, args.window_end)
    sys.stdout.write(report.render(totals))
    return 0


if __name__ == "__main__":
    sys.exit(main())
