"""Check raw records against the rules in SPEC.md.

Every rejected record produces one message and no exception: a duty log with a
bad row is an ordinary Tuesday, and the operator wants all of the bad rows at
once rather than the first one.
"""
from dataclasses import dataclass
from datetime import datetime
import re

STATUSES = ("logged", "void")
MIN_MINUTES = 1
MAX_MINUTES = 1440
TIMESTAMP = "%Y-%m-%dT%H:%M"
WHOLE_NUMBER = re.compile(r"^[+-]?[0-9]+$")


@dataclass(frozen=True)
class Entry:
    """One accepted, logged record."""

    line_no: int
    entry_id: str
    start: datetime
    worker: str
    site: str
    minutes: int


@dataclass(frozen=True)
class Validated:
    """Accepted logged entries, and one message per rejected record."""

    entries: list
    errors: list


def _timestamp(text):
    return datetime.strptime(text, TIMESTAMP)


def _minutes(text):
    return int(text) if WHOLE_NUMBER.match(text) else None


def _problem(raw, seen):
    """The first rule this record breaks, or None."""
    if not raw.entry_id:
        return "missing entry_id"
    try:
        _timestamp(raw.start)
    except ValueError:
        return "bad start timestamp"
    minutes = _minutes(raw.minutes)
    if minutes is None:
        return "minutes is not a whole number"
    if minutes < MIN_MINUTES or minutes > MAX_MINUTES:
        return "minutes out of range"
    if raw.status not in STATUSES:
        return "unknown status"
    if raw.entry_id in seen:
        return "duplicate entry_id"
    return None


def validate(raw_entries):
    """Split records into accepted entries and error messages."""
    entries = []
    errors = []
    seen = set()
    for raw in raw_entries:
        problem = _problem(raw, seen)
        if problem is not None:
            errors.append("line %d: %s" % (raw.line_no, problem))
            continue
        seen.add(raw.entry_id)
        if raw.status == "void":
            continue
        entries.append(Entry(raw.line_no, raw.entry_id, _timestamp(raw.start),
                             raw.worker, raw.site, int(raw.minutes)))
    return Validated(entries, errors)
