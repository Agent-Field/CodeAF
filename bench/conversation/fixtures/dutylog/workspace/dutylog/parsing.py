"""Read a duty log CSV as text, keeping the line each record came from.

Nothing here decides whether a record is *sensible* — that is validation's job.
This module's only contract is the file format in SPEC.md: RFC 4180 quoting
minus newlines inside quoted fields, which SPEC.md puts out of scope, plus an
optional byte order mark, blank lines that are not records, and a line number
that survives both of those so an error message can point at the right line.
"""
from dataclasses import dataclass

HEADER = ["entry_id", "start", "worker", "site", "minutes", "status"]


class ParseError(Exception):
    """The file is not a duty log this program can read at all."""


@dataclass(frozen=True)
class RawEntry:
    """One record, still as text. line_no is 1-based and counts every line."""

    line_no: int
    entry_id: str
    start: str
    worker: str
    site: str
    minutes: str
    status: str


def read_entries(path):
    """Read a duty log from disk."""
    with open(path, "r", encoding="utf-8", newline="") as handle:
        return parse_entries(handle.read())


def parse_entries(text):
    """Parse duty log text into RawEntry records, in file order."""
    entries = []
    header_seen = False
    for line_no, line in enumerate(text.splitlines(), start=1):
        if not line.strip():
            continue
        fields = [field.strip() for field in line.split(",")]
        if not header_seen:
            if fields != HEADER:
                raise ParseError("line %d: expected the header %s"
                                 % (line_no, ",".join(HEADER)))
            header_seen = True
            continue
        if len(fields) != len(HEADER):
            raise ParseError("line %d: expected %d fields, found %d"
                             % (line_no, len(HEADER), len(fields)))
        entries.append(RawEntry(line_no, *fields))
    if not header_seen:
        raise ParseError("the file has no header row")
    return entries
