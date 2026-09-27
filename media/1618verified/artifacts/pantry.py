#!/usr/bin/env python3
"""Read-only pantry stock report.

Usage: python3 pantry.py CSV [--threshold N] [--shopping]

Reads a CSV with header ``item,quantity`` and prints each item whose
quantity is STRICTLY BELOW the threshold (default 3), one per line as
``<item>: <quantity>``, sorted alphabetically by item. With ``--shopping``
the line becomes ``<item>: buy N`` where N = threshold - quantity.

A negative quantity is malformed input and is always rejected. This
program is READ-ONLY: it never writes, rewrites or deletes any file.
Inventory edits stay manual.
"""

import argparse
import csv
import sys


def parse_rows(path):
    """Yield (line_number, item, quantity) or raise ValueError on malformed input."""
    with open(path, newline="") as fh:
        reader = csv.reader(fh)
        try:
            header = next(reader)
        except StopIteration:
            raise ValueError("empty file: missing header row 'item,quantity'")
        if header != ["item", "quantity"]:
            raise ValueError(
                "line 1: bad header %r, expected 'item,quantity'" % (",".join(header),)
            )
        for line_number, row in enumerate(reader, start=2):
            if len(row) != 2:
                raise ValueError(
                    "line %d: expected 2 fields 'item,quantity', got %d"
                    % (line_number, len(row))
                )
            item, raw_quantity = row
            if item.strip() == "":
                raise ValueError("line %d: blank item" % line_number)
            try:
                quantity = int(raw_quantity)
            except ValueError:
                raise ValueError(
                    "line %d: quantity %r is not a valid integer"
                    % (line_number, raw_quantity)
                )
            if quantity < 0:
                raise ValueError(
                    "line %d: quantity %r is negative; stock must be >= 0"
                    % (line_number, raw_quantity)
                )
            yield line_number, item, quantity


def build_report(path, threshold, shopping=False):
    """Return the list of formatted report lines, or raise ValueError."""
    lines = []
    for _line_number, item, quantity in parse_rows(path):
        if quantity < threshold:
            lines.append((item, quantity))
    lines.sort(key=lambda pair: pair[0])
    if shopping:
        return ["%s: buy %d" % (item, threshold - quantity) for item, quantity in lines]
    return ["%s: %d" % (item, quantity) for item, quantity in lines]


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Report pantry items below a quantity threshold (read-only)."
    )
    parser.add_argument("csv", help="path to the pantry CSV file")
    parser.add_argument(
        "--threshold",
        type=int,
        default=3,
        help="report items with quantity strictly below this (default: 3)",
    )
    parser.add_argument(
        "--shopping",
        action="store_true",
        help="print 'item: buy N' (N = threshold - quantity) instead of stock levels",
    )
    args = parser.parse_args(argv)

    try:
        report = build_report(args.csv, args.threshold, shopping=args.shopping)
    except FileNotFoundError:
        print("error: file not found: %s" % args.csv, file=sys.stderr)
        return 2
    except OSError as exc:
        print("error: cannot read %s: %s" % (args.csv, exc), file=sys.stderr)
        return 2
    except ValueError as exc:
        print("error: malformed input: %s" % exc, file=sys.stderr)
        return 2

    for line in report:
        print(line)
    return 0


if __name__ == "__main__":
    sys.exit(main())
