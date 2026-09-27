#!/usr/bin/env python3
"""Dependency-free pantry shopping-list CLI.

Reads a two-column CSV (header ``item,quantity``) and prints the items whose
quantity is strictly below a threshold (default 3), sorted alphabetically by
item name, together with how much to buy to reach the threshold
(``threshold - quantity``). Quantities may be fractional. Standard library only.
"""

import argparse
import csv
import json
import math
import sys

DEFAULT_THRESHOLD = 3


def parse_number(raw):
    """Parse ``raw`` as a number, or return None if it is not one.

    Whole values come back as ``int`` (so ``2`` and ``2.0`` both give ``2``),
    anything else as ``float``. This keeps printed/JSON output free of a
    trailing ``.0`` while still accepting fractional quantities.
    """
    try:
        return int(raw)
    except ValueError:
        pass
    try:
        value = float(raw)
    except ValueError:
        return None
    if not math.isfinite(value):
        return None
    return int(value) if value.is_integer() else value


def format_number(value):
    """Whole numbers print bare; fractional ones print as themselves."""
    return str(int(value)) if float(value).is_integer() else str(value)


def load_rows(path):
    """Return a list of {"item": str, "quantity": number} from the CSV at *path*.

    Raises SystemExit(1) with a message on stderr for a missing file, a CSV
    that does not start with the ``item,quantity`` header, a row that does not
    have exactly two columns, a blank item name, or a quantity that is not a
    finite non-negative number.
    """
    try:
        handle = open(path, newline="")
    except OSError as exc:
        print("pantry: cannot read %s: %s" % (path, exc.strerror), file=sys.stderr)
        raise SystemExit(1)

    with handle:
        reader = csv.reader(handle)
        try:
            header = next(reader)
        except StopIteration:
            print("pantry: %s is empty" % path, file=sys.stderr)
            raise SystemExit(1)
        if [field.strip() for field in header] != ["item", "quantity"]:
            print(
                "pantry: %s: expected header 'item,quantity'" % path,
                file=sys.stderr,
            )
            raise SystemExit(1)
        rows = []
        for lineno, row in enumerate(reader, start=2):
            if not row or all(not field.strip() for field in row):
                continue
            if len(row) != 2:
                print(
                    "pantry: %s:%d: expected 2 columns, got %d"
                    % (path, lineno, len(row)),
                    file=sys.stderr,
                )
                raise SystemExit(1)
            item = row[0].strip()
            if not item:
                print(
                    "pantry: %s:%d: item name is blank" % (path, lineno),
                    file=sys.stderr,
                )
                raise SystemExit(1)
            raw_quantity = row[1].strip()
            quantity = parse_number(raw_quantity)
            if quantity is None:
                print(
                    "pantry: %s:%d: item %r has non-numeric quantity %r"
                    % (path, lineno, item, raw_quantity),
                    file=sys.stderr,
                )
                raise SystemExit(1)
            if quantity < 0:
                print(
                    "pantry: %s:%d: item %r has negative quantity %r"
                    % (path, lineno, item, raw_quantity),
                    file=sys.stderr,
                )
                raise SystemExit(1)
            rows.append({"item": item, "quantity": quantity})
    return rows


def select(rows, threshold):
    """Rows strictly below *threshold*, each with its buy amount, sorted A-Z."""
    below = (row for row in rows if row["quantity"] < threshold)
    return sorted(
        (
            {
                "item": row["item"],
                "quantity": row["quantity"],
                "buy": parse_number(str(threshold - row["quantity"])),
            }
            for row in below
        ),
        key=lambda row: row["item"],
    )


def main(argv=None):
    parser = argparse.ArgumentParser(
        prog="pantry.py",
        description="Print pantry items below a quantity threshold and how much to buy.",
    )
    parser.add_argument(
        "csv",
        nargs="?",
        default="pantry.csv",
        help="CSV file with an item,quantity header (default: pantry.csv)",
    )
    parser.add_argument(
        "--threshold",
        type=float,
        default=DEFAULT_THRESHOLD,
        help="buy back up to this quantity; items strictly below are listed "
        "(default: 3; must be a finite non-negative number)",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        dest="as_json",
        help="print a JSON array of {item, quantity, buy} objects",
    )
    args = parser.parse_args(argv)

    threshold = parse_number(str(args.threshold))
    if threshold is None or threshold < 0:
        print(
            "pantry: --threshold must be a finite non-negative number, got %r"
            % args.threshold,
            file=sys.stderr,
        )
        raise SystemExit(1)
    rows = select(load_rows(args.csv), threshold)

    if args.as_json:
        print(json.dumps(rows))
    else:
        for row in rows:
            print("%s: %s" % (row["item"], format_number(row["buy"])))
    return 0


if __name__ == "__main__":
    sys.exit(main())
