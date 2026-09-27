#!/usr/bin/env python3
"""Build SHOPPING.md from pantry.csv using only the standard library.

Reads a two-column CSV (item,quantity) and writes a Markdown shopping list
naming every item whose quantity is below TARGET, together with how many units
to buy to reach TARGET.

Invalid input (a missing file, a malformed row, or a negative quantity) is
rejected with a clear error on stderr and a non-zero exit status. In that case
an existing SHOPPING.md is left untouched, so the last good report survives.
"""

import csv
import sys
from pathlib import Path

TARGET = 6
PANTRY_PATH = Path("pantry.csv")
OUTPUT_PATH = Path("SHOPPING.md")
EXPECTED_COLUMNS = ["item", "quantity"]


class PantryError(ValueError):
    """Raised when pantry.csv cannot be turned into a shopping list."""


def read_pantry(path=PANTRY_PATH):
    """Return a list of (item, quantity) pairs read from *path*.

    Raises PantryError for a negative or non-integer quantity, a blank item
    name, or a CSV whose header is not exactly ``item,quantity``.
    """
    items = []
    with open(path, newline="", encoding="utf-8") as handle:
        reader = csv.DictReader(handle)
        if reader.fieldnames != EXPECTED_COLUMNS:
            raise PantryError(
                "expected a CSV header of 'item,quantity', got {!r}".format(
                    reader.fieldnames
                )
            )
        for lineno, row in enumerate(reader, start=2):
            name = (row.get("item") or "").strip()
            raw = (row.get("quantity") or "").strip()
            if not name:
                raise PantryError("line {}: missing item name".format(lineno))
            try:
                quantity = int(raw)
            except ValueError:
                raise PantryError(
                    "line {}: quantity for '{}' is not an integer: {!r}".format(
                        lineno, name, raw
                    )
                )
            if quantity < 0:
                raise PantryError(
                    "line {}: quantity for '{}' is negative: {}".format(
                        lineno, name, quantity
                    )
                )
            items.append((name, quantity))
    return items


def build_report(items, target=TARGET):
    """Render the Markdown report for *items* against *target*."""
    below = [(name, qty) for name, qty in items if qty < target]
    # Closest to the target first, then alphabetical for a stable order.
    below.sort(key=lambda pair: (-pair[1], pair[0]))
    lines = ["# Shopping list", "", "Target: {}. Buy this many to reach it.".format(target), ""]
    if not below:
        lines.append("Nothing to buy.")
    else:
        for name, qty in below:
            lines.append("{}: buy {}".format(name, target - qty))
    return "\n".join(lines) + "\n"


def generate(pantry_path=PANTRY_PATH, output_path=OUTPUT_PATH, target=TARGET):
    """Read *pantry_path* and write the report to *output_path*.

    The report is computed in full before the file is written, so a rejected
    input never clobbers a previously written SHOPPING.md.
    """
    items = read_pantry(pantry_path)
    report = build_report(items, target)
    output_path.write_text(report, encoding="utf-8")
    return report


def main(argv=None):
    try:
        generate()
    except (OSError, PantryError) as exc:
        print("error: {}".format(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
