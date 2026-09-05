"""An alternative correct validation module: a rules table instead of a chain."""
from dataclasses import dataclass
from datetime import datetime

STATUSES = frozenset({"logged", "void"})


@dataclass(frozen=True)
class Entry:
    line_no: int
    entry_id: str
    start: datetime
    worker: str
    site: str
    minutes: int


@dataclass(frozen=True)
class Validated:
    entries: list
    errors: list


def _moment(text):
    try:
        return datetime.strptime(text, "%Y-%m-%dT%H:%M")
    except ValueError:
        return None


def _count(text):
    body = text[1:] if text[:1] in "+-" else text
    if not body.isdigit():
        return None
    return -int(body) if text[:1] == "-" else int(body)


def validate(raw_entries):
    accepted, complaints, claimed = [], [], []
    for record in raw_entries:
        minutes = _count(record.minutes)
        moment = _moment(record.start)
        if record.entry_id == "":
            reason = "missing entry_id"
        elif moment is None:
            reason = "bad start timestamp"
        elif minutes is None:
            reason = "minutes is not a whole number"
        elif not 1 <= minutes <= 1440:
            reason = "minutes out of range"
        elif record.status not in STATUSES:
            reason = "unknown status"
        elif record.entry_id in claimed:
            reason = "duplicate entry_id"
        else:
            reason = None
        if reason:
            complaints.append("line {}: {}".format(record.line_no, reason))
            continue
        claimed.append(record.entry_id)
        if record.status != "void":
            accepted.append(Entry(record.line_no, record.entry_id, moment,
                                  record.worker, record.site, minutes))
    return Validated(accepted, complaints)
