#!/usr/bin/env python3
"""Home maintenance helper.

Reads a maintenance CSV (columns: title,due,status) and either prints the
unfinished chores (status other than "done") whose due date is on or before
a date you give with --due, or marks one chore done by exact title with
--done.

Standard library only.
"""

import argparse
import csv
import json
import os
import sys
import tempfile
from datetime import date
from pathlib import Path

DONE_STATUSES = {"done", "complete", "completed", "closed"}
DEFAULT_CSV = Path(__file__).with_name("maintenance.csv")


def parse_date(value):
    """Parse an ISO date (YYYY-MM-DD) or raise ValueError."""
    return date.fromisoformat(value.strip())


def load_chores(path):
    """Yield dicts {title, due, status, line} from the CSV at *path*."""
    with open(path, newline="", encoding="utf-8") as handle:
        reader = csv.DictReader(handle)
        required = {"title", "due", "status"}
        missing = required - {(field or "").strip().lower() for field in reader.fieldnames or []}
        if missing:
            raise ValueError(
                f"{path}: missing column(s): {', '.join(sorted(missing))}"
            )
        for lineno, row in enumerate(reader, start=2):
            title = (row.get("title") or "").strip()
            due_raw = (row.get("due") or "").strip()
            status = (row.get("status") or "").strip()
            if not title and not due_raw:
                continue  # blank line
            try:
                due = parse_date(due_raw)
            except ValueError:
                raise ValueError(f"{path}:{lineno}: bad due date {due_raw!r}")
            yield {"title": title, "due": due, "status": status, "line": lineno}


def load_rows(path):
    """Return (header, rows) from the CSV at *path*.

    *rows* are plain lists so the file can be rewritten without dropping
    columns this tool does not know about.
    """
    with open(path, newline="", encoding="utf-8") as handle:
        reader = csv.reader(handle)
        try:
            header = next(reader)
        except StopIteration:
            raise ValueError(f"{path}: empty CSV, expected a header row")
        return header, [row for row in reader]


def _column_index(header, name, path):
    wanted = name.strip().lower()
    for index, field in enumerate(header):
        if (field or "").strip().lower() == wanted:
            return index
    raise ValueError(f"{path}: missing column(s): {name}")


def mark_done(path, title):
    """Mark the chore whose title exactly matches *title* as done.

    Returns the data line number (the header is line 1) of the changed
    row. Raises ValueError, without touching the file, when the title is
    empty, missing, or matches more than one row.
    """
    title = (title or "").strip()
    if not title:
        raise ValueError("a non-empty title is required for --done")
    header, rows = load_rows(path)
    title_index = _column_index(header, "title", path)
    status_index = _column_index(header, "status", path)

    matches = [
        lineno
        for lineno, row in enumerate(rows, start=2)
        if len(row) > title_index and row[title_index].strip() == title
    ]
    if not matches:
        raise ValueError(f"no chore titled {title!r} in {path}")
    if len(matches) > 1:
        lines = ", ".join(str(lineno) for lineno in matches)
        raise ValueError(
            f"ambiguous title {title!r} in {path}: "
            f"{len(matches)} matching rows (lines {lines})"
        )

    lineno = matches[0]
    row = rows[lineno - 2]
    while len(row) <= status_index:
        row.append("")
    row[status_index] = "done"
    write_rows_atomic(path, header, rows)
    return lineno


def write_rows_atomic(path, header, rows):
    """Rewrite the CSV through a temp file in its directory, then rename.

    The rename is atomic, so a crash or a concurrent reader never sees a
    half-written file, and an error leaves the original untouched.
    """
    directory = os.path.dirname(os.path.abspath(path)) or "."
    handle_fd, temp_path = tempfile.mkstemp(
        prefix=".maintenance-", suffix=".csv", dir=directory
    )
    try:
        with os.fdopen(handle_fd, "w", newline="", encoding="utf-8") as handle:
            writer = csv.writer(handle, lineterminator="\n")
            writer.writerow(header)
            for row in rows:
                writer.writerow(row)
        os.replace(temp_path, path)
    except BaseException:
        try:
            os.unlink(temp_path)
        except OSError:
            pass
        raise
    return path


def unfinished_due_by(chores, cutoff):
    """Unfinished chores due on or before *cutoff*, soonest first."""
    selected = [
        chore
        for chore in chores
        if chore["status"].lower() not in DONE_STATUSES and chore["due"] <= cutoff
    ]
    selected.sort(key=lambda chore: (chore["due"], chore["title"].lower()))
    return selected


def chores_as_json(chores, cutoff):
    """Build the JSON payload: the cutoff, a count, and the chores."""
    return {
        "due_by": cutoff.isoformat(),
        "count": len(chores),
        "chores": [
            {
                "title": chore["title"],
                "due": chore["due"].isoformat(),
                "status": chore["status"],
            }
            for chore in chores
        ],
    }


def build_parser():
    parser = argparse.ArgumentParser(
        prog="maintenance",
        description="Show unfinished home maintenance chores due by a date, "
        "or mark one done by its exact title.",
    )
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument(
        "--due",
        metavar="YYYY-MM-DD",
        help="show unfinished chores due on or before this date",
    )
    mode.add_argument(
        "--done",
        metavar="TITLE",
        help="mark the chore whose title exactly matches TITLE as done",
    )
    parser.add_argument(
        "--csv",
        default=str(DEFAULT_CSV),
        help=f"path to the maintenance CSV (default: {DEFAULT_CSV.name})",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="print results as JSON for scripting instead of text",
    )
    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)
    if args.done is not None:
        try:
            line = mark_done(args.csv, args.done)
        except (OSError, ValueError) as exc:
            print(f"error: {exc}", file=sys.stderr)
            return 2
        if args.json:
            print(
                json.dumps(
                    {"marked": args.done.strip(), "line": line, "csv": args.csv},
                    indent=2,
                )
            )
        else:
            print(f"Marked {args.done.strip()!r} (line {line}) done in {args.csv}.")
        return 0
    try:
        cutoff = parse_date(args.due)
    except ValueError:
        print(f"error: --due must be a date like 2026-09-25 (got {args.due!r})", file=sys.stderr)
        return 2
    try:
        chores = list(load_chores(args.csv))
    except (OSError, ValueError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    due = unfinished_due_by(chores, cutoff)
    if args.json:
        print(json.dumps(chores_as_json(due, cutoff), indent=2))
        return 0
    if not due:
        print(f"No unfinished chores due by {cutoff.isoformat()}.")
        return 0

    print(f"{len(due)} unfinished chore(s) due by {cutoff.isoformat()}:")
    width = max(len(chore["title"]) for chore in due)
    for chore in due:
        print(f"  {chore['due'].isoformat()}  {chore['title']:<{width}}  [{chore['status']}]")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
