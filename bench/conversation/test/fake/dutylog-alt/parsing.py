"""An alternative correct parsing module: same contract, different code.

The judge must accept this. It shares no structure with the reference repair —
a generator, a different splitter call, different error wording — so a judge
that recognised the reference's text rather than its behaviour would fail here.
"""
import csv
from dataclasses import dataclass

HEADER = ["entry_id", "start", "worker", "site", "minutes", "status"]


class ParseError(Exception):
    pass


@dataclass(frozen=True)
class RawEntry:
    line_no: int
    entry_id: str
    start: str
    worker: str
    site: str
    minutes: str
    status: str


def _records(text):
    reader = csv.reader(iter(text.splitlines()))
    for row in reader:
        trimmed = [value.strip() for value in row]
        if not any(trimmed):
            continue
        yield reader.line_num, trimmed


def parse_entries(text):
    stream = _records(text.removeprefix("\N{ZERO WIDTH NO-BREAK SPACE}"))
    try:
        line_no, header = next(stream)
    except StopIteration:
        raise ParseError("nothing that looks like a duty log") from None
    if header != HEADER:
        raise ParseError("line %d: that is not the duty log header" % line_no)
    out = []
    for line_no, values in stream:
        if len(values) != 6:
            raise ParseError("line %d: %d fields where 6 were wanted" % (line_no, len(values)))
        out.append(RawEntry(line_no, *values))
    return out


def read_entries(path):
    with open(path, "r", encoding="utf-8", newline="") as handle:
        return parse_entries(handle.read())
