#!/usr/bin/env python3
"""Expense report CLI: category totals sorted by name plus a grand total.

Reads a CSV with a header row of ``date,category,amount`` and prints one line
per category (sorted by name) and a grand total. Amounts are handled with
``decimal.Decimal`` so cents stay exact; a negative amount is a refund. By
default the report is plain text; ``--json`` emits JSON and ``--csv`` emits a
``category,amount`` CSV whose last row is ``TOTAL``. ``--month YYYY-MM``
restricts the report to one calendar month, so a mixed-month (e.g. quarterly)
export can be reported a month at a time. ``--strict`` is for automation: if
any row would be skipped, the warnings are still written to stderr but no
report is written to stdout and the process exits 2, so a partial report can
never be mistaken for a complete one.

Rows whose amount is not a finite decimal (including ``NaN`` and ``Infinity``)
are skipped with a one-line warning on stderr. The report still covers the
valid rows, and the process exits with status 2 when any row was skipped. Under
``--month``, a row whose date cannot be read is skipped and warned about the
same way, so a bad date is never silently dropped by the filter.
"""

import argparse
import csv
import datetime
import io
import json
import re
import sys
from collections import defaultdict
from decimal import Decimal, InvalidOperation, localcontext

TWO_PLACES = Decimal("0.01")
MONTH_RE = re.compile(r"[0-9]{4}-(0[1-9]|1[0-2])\Z")


def parse_month(value):
    """Argparse type for ``--month``: accept exactly ``YYYY-MM``."""
    if not MONTH_RE.match(value):
        raise argparse.ArgumentTypeError(
            "expected a month as YYYY-MM (e.g. 2026-09), got %r" % (value,)
        )
    return value


def _month_of(date):
    """The ``YYYY-MM`` a row's ``date`` falls in, or ``None`` if unreadable."""
    try:
        return datetime.date.fromisoformat(date).strftime("%Y-%m")
    except ValueError:
        return None


def parse_amount(raw):
    """Return the Decimal *raw* denotes, or None when it is not a finite number."""
    try:
        amount = Decimal(raw)
    except (InvalidOperation, ValueError):
        return None
    if not amount.is_finite():
        return None
    return amount


def load_expenses(path):
    """Read *path* and return ``(rows, skipped)``.

    ``rows`` is a list of ``(line_no, date, category, amount)`` for every valid
    row; ``skipped`` is a list of ``(line_no, raw_amount)`` for rows whose
    amount could not be read. A missing header or category is still a hard
    error.
    """
    rows = []
    skipped = []
    with open(path, newline="", encoding="utf-8") as handle:
        reader = csv.DictReader(handle)
        expected = {"date", "category", "amount"}
        if reader.fieldnames is None or not expected.issubset(reader.fieldnames):
            raise ValueError(
                "CSV must have columns: date, category, amount "
                "(got: %s)" % (reader.fieldnames,)
            )
        for line_no, row in enumerate(reader, start=2):
            date = (row.get("date") or "").strip()
            category = (row.get("category") or "").strip()
            raw_amount = (row.get("amount") or "").strip()
            if not category:
                raise ValueError("missing category on line %d" % line_no)
            amount = parse_amount(raw_amount)
            if amount is None:
                skipped.append((line_no, raw_amount))
            else:
                rows.append((line_no, date, category, amount))
    return rows, skipped


def _working_precision(rows):
    """Significant digits needed so every amount and running total stays exact.

    The default decimal context holds 28 digits; a larger finite amount would
    otherwise make ``quantize`` raise ``InvalidOperation``.
    """
    return max(
        (amount.adjusted() + 1 for _line_no, _date, _name, amount in rows), default=1
    )


def summarize(rows, category=None, month=None):
    """Return ``(ranked, grand, date_skips)`` for *rows*.

    When *category* is given, only categories matching it case-insensitively
    are included. When *month* (``YYYY-MM``) is given, only rows whose date
    falls in that month are included, and a row with an unreadable date is
    returned in *date_skips* as ``(line_no, raw_date)`` rather than silently
    dropped.
    """
    wanted = category.strip().lower() if category is not None else None
    date_skips = []
    with localcontext() as ctx:
        ctx.prec = max(ctx.prec, _working_precision(rows) + 10)
        totals = defaultdict(lambda: Decimal("0.00"))
        for line_no, date, name, amount in rows:
            if month is not None:
                row_month = _month_of(date)
                if row_month is None:
                    date_skips.append((line_no, date))
                    continue
                if row_month != month:
                    continue
            if wanted is not None and name.lower() != wanted:
                continue
            totals[name] += amount
        ranked = [(cat, totals[cat].quantize(TWO_PLACES)) for cat in sorted(totals)]
        grand = sum((total for _, total in ranked), Decimal("0.00")).quantize(TWO_PLACES)
    return ranked, grand, date_skips


def report(path, category=None, month=None, warn=None, warn_date=None):
    """Return ``(ranked, grand, skipped)`` for the CSV at *path*.

    *month* (``YYYY-MM``) restricts the totals to one calendar month. *warn*,
    when given, is called as ``warn(line_no, raw_amount)`` once for each row
    whose amount could not be read. *warn_date*, when given, is called as
    ``warn_date(line_no, raw_date)`` once for each row whose date could not be
    read under *month*, so a malformed date is reported rather than silently
    filtered out. *skipped* holds both kinds of ``(line_no, raw_value)``.
    """
    rows, skipped = load_expenses(path)
    if warn is not None:
        for line_no, raw_amount in skipped:
            warn(line_no, raw_amount)
    ranked, grand, date_skips = summarize(rows, category, month)
    if warn_date is not None:
        for line_no, raw_date in date_skips:
            warn_date(line_no, raw_date)
    return ranked, grand, skipped + date_skips


def format_text(ranked, grand):
    """The human-readable report."""
    lines = ["%s: %s" % (name, total) for name, total in ranked]
    lines.append("grand total: %s" % grand)
    return "\n".join(lines)


def format_json(ranked, grand):
    """The machine-readable report: totals as exact decimal strings."""
    payload = {
        "categories": {name: str(total) for name, total in ranked},
        "total": str(grand),
    }
    return json.dumps(payload, indent=2)


def format_csv(ranked, grand):
    """The spreadsheet-import report: ``category,amount`` rows and a ``TOTAL`` row."""
    out = io.StringIO()
    writer = csv.writer(out, lineterminator="\n")
    writer.writerow(["category", "amount"])
    for name, total in ranked:
        writer.writerow([name, str(total)])
    writer.writerow(["TOTAL", str(grand)])
    return out.getvalue().rstrip("\n")


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Print expense totals by category and a grand total."
    )
    parser.add_argument(
        "csv",
        nargs="?",
        default="expenses.csv",
        help="path to the expenses CSV (default: expenses.csv)",
    )
    parser.add_argument(
        "--category",
        help="only include this category (case-insensitive)",
    )
    parser.add_argument(
        "--month",
        type=parse_month,
        metavar="YYYY-MM",
        help="only include rows dated in this calendar month",
    )
    output = parser.add_mutually_exclusive_group()
    output.add_argument(
        "--json",
        action="store_true",
        dest="as_json",
        help="emit machine-readable JSON instead of text",
    )
    output.add_argument(
        "--csv",
        action="store_true",
        dest="as_csv",
        help="emit a category,amount CSV (with a TOTAL row) instead of text",
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="if any row would be skipped, write no report to stdout and exit 2",
    )
    args = parser.parse_args(argv)

    def warn(line_no, raw_amount):
        print(
            "%s:%d: skipping invalid amount %r" % (args.csv, line_no, raw_amount),
            file=sys.stderr,
        )

    def warn_date(line_no, raw_date):
        print(
            "%s:%d: skipping row with unreadable date %r (needed for --month)"
            % (args.csv, line_no, raw_date),
            file=sys.stderr,
        )

    try:
        ranked, grand, skipped = report(args.csv, args.category, args.month, warn, warn_date)
    except (OSError, ValueError) as exc:
        parser.exit(1, "error: %s\n" % exc)

    if args.strict and skipped:
        # Warnings have already gone to stderr; withhold the report so a
        # partial result cannot be imported as if it were complete.
        return 2

    if args.as_json:
        print(format_json(ranked, grand))
    elif args.as_csv:
        print(format_csv(ranked, grand))
    else:
        print(format_text(ranked, grand))
    return 2 if skipped else 0


if __name__ == "__main__":
    sys.exit(main())
