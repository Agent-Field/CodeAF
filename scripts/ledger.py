#!/usr/bin/env python3
"""Assemble docs/design/polish/LEDGER.md from the per-surface audits beside it.

The audits are where a defect is written down in full: what it is, the line
that decides it, why it costs a developer something, the fix shape, the
severity and the frames it was seen on. The ledger is the one ORDERED list the
owner reads, so it is GENERATED from them and never edited by hand — an edit
here would be a second copy of a number that is already written down somewhere
else, and a second copy is a copy that will drift.

A row CLOSES when the audit that holds it names its number under `## fixed`,
which is the same thing as saying it closes on a captured before/after pair,
because that is what a lane has to put there. The spellings below are the ones
the lanes actually write — a table row, a bolded number, a `Row N` heading —
and a spelling nobody uses is not worth matching.
"""

import datetime
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
D = ROOT / "docs/design/polish"

# One letter per surface, so a row can be named in a commit message or across a
# desk ("H3", "C12") without saying which file it came out of.
PREFIX = {
    "home": "H", "tasks": "T", "cli": "C", "help": "P",
    "chat": "K", "settings": "S", "commands": "M", "jobs": "J",
    # The second eye is not a surface; it is one read of the whole wave by a
    # reader who wrote none of it. Its rows are numbered here like any other.
    "eyes": "E",
    # And the words pass, which is not a surface either: one read of every
    # person-facing string the wave adds, against every other one.
    "words": "W",
}

ROW = re.compile(r"^(\d+)\. (.*)$")
# A `## fixed` SECTION IS NOT ALL CLOSURES. Every lane writes its own account of
# what it did NOT do under a sub-heading — `### skipped, and why`, `### Not fixed
# here`, `### Not done by this lane` — and those paragraphs name row numbers too.
# Reading them as closures counted twenty-eight rows as done that their own audit
# says are open, in a ledger whose whole purpose is to be believable. So the
# closures are the sub-blocks that are not disclaimers, and a heading has to earn
# its rows rather than merely stand under the right `##`.
NOT_A_CLOSURE = re.compile(r"\b(skip|not\s+(?:fixed|done)|left|remain|still\s+open|what\s+other\s+lanes)", re.I)
# AND A DISCLAIMER IS NOT ALWAYS A SUB-HEADING. A lane that fixed four rows and
# declined two writes all six as bold leads in one list, and says so in the lead
# itself — `**Row 12 — the status line wraps.** NOT FIXED, and here is why`. The
# heading gate above never sees that, so the row counted as done in a ledger that
# says on its own first page it closes on a before/after pair. This gate reads the
# LEAD LINE ONLY, deliberately: the vocabulary below is loose enough to catch a
# refusal and would fire on half the prose in the body of a genuine closure.
# AND IT IS NARROW ON PURPOSE. `could not` was in this list for one revision and
# read `the narrow bar keeps every word, and says how many it could not` — a lane
# DESCRIBING ITS FIX — as a refusal. A phrase that appears in ordinary prose about
# what a frame could not fit does not belong here; only the handful below, which
# a lane writes when and only when it is declining a row.
NOT_DONE_LEAD = re.compile(
    r"\b(not\s+(?:fixed|done|closed|ours|aforge|this\s+lane|for\s+this\s+lane)"
    r"|left\s+open|still\s+open|skipped|deferred|declined|handed\s+back"
    r"|no\s+fix|out\s+of\s+scope)\b",
    re.I,
)
SEV = re.compile(r"sev: (high|med|low)")
# How a lane says "this row is done", in the three shapes they write.
CLOSED = [
    re.compile(r"^\|\s*(\d+)\s*\|", re.M),               # a table row
    # "Row 7 — …", and "Rows 15 and 16", which is how a lane writes a fix that
    # closes two rows at once. The plural cost this ledger a fourth undercount:
    # `Row\s` cannot match `Rows 15`, so a lane that had done the work was told
    # to reword its prose to suit a regex. The generator reads what lanes write.
    re.compile(r"[Rr]ows?\s+(\d+(?:\s*(?:,|and)\s*\d+)*)\b"),
    re.compile(r"\*\*([\d,\s]+)\*\*"),                    # "**1, 2, 11, 23**"
    re.compile(r"^\*\*(\d+)\s*[—-]", re.M),               # "**1 — a person's own message…"
]


# A ROW CAN BE CLOSED BY AN AUDIT THAT DID NOT FIND IT. A lane sent at one
# surface routinely closes a row another surface's audit wrote down — the narrow
# tier's lane closed four of home's — and it records that by the row's LEDGER ID
# (`H4`, `N8`), which is the only name the two audits share. So closures are
# gathered from every audit's fixed section, not only from the one the row lives
# in, and a bare number still means a row of the file it was written in.
BY_ID = re.compile(r"\b([HTCPKSMJN])(\d+)\b")


def closures_only(fixed: str) -> str:
    """The part of a `## fixed` section that actually claims work was done.

    Two gates, because lanes disclaim at two scales: a whole sub-heading of rows
    they did not get to, and a single bold lead inside a list of rows they did.
    A block runs from one heading or bold lead to the next, and it is kept only
    if BOTH the heading it sits under and its own lead claim work was done.
    """
    kept, section, lead = [], True, True
    for line in fixed.splitlines():
        # A `##` OPENS A NEW SECTION AND CLEARS BOTH GATES. A lane that comes back
        # for a second pass writes `## fixed — the second pass` under the first
        # pass's closing `### Not done by this lane`, and a gate that only ever
        # reset on `###` swallowed the entire second pass — four rows of real work
        # read as nothing at all. A ledger errs in both directions or it is not a
        # ledger, so this is checked the same way the generous direction is.
        if line.startswith("## "):
            section, lead = True, True
        elif line.startswith("###"):
            section = not NOT_A_CLOSURE.search(line)
            lead = True
        elif line.startswith("**"):
            lead = not NOT_DONE_LEAD.search(line)
        if section and lead:
            kept.append(line)
    return "\n".join(kept)


def closed_rows(text: str) -> set[str]:
    found: set[str] = set()
    for pattern in CLOSED:
        for hit in pattern.findall(text):
            found.update(re.findall(r"\d+", hit))
    return found


def closed_ids(text: str) -> set[str]:
    return {p + n for p, n in BY_ID.findall(text)}


SELFTEST = [
    # (a `## fixed` section, the row numbers it truly claims)
    ("\n**Row 1 — the bar keeps every word.** Done at every width.\n", {"1"}),
    # The plural, and a list, which is how a lane closes two rows in one entry.
    ("\n**Rows 15 and 16 — one function, one fix.** Landed.\n", {"15", "16"}),
    ("\n**Row 12 — the line wraps.** NOT FIXED, and here is why it was not.\n", set()),
    ("\n### Not done by this lane\n**Row 4 — the tail was cut.** Somebody else's.\n", set()),
    # A second pass under a first pass's closing disclaimer: the whole reason the
    # `##` reset exists. Four rows of real work were read as nothing without it.
    ("\n### Not done by this lane\n**Row 4 — theirs.**\n"
     "\n## fixed — the second pass\n**Row 7 — the price is drawn once.** Done.\n", {"7"}),
    # A closure whose own prose says what the frame could not hold. Not a refusal.
    ("\n**Row 1 — the bar says how many words it could not keep.** Landed.\n", {"1"}),
]


def selftest() -> int:
    bad = 0
    for text, want in SELFTEST:
        got = closed_rows(closures_only(text))
        if got != want:
            bad += 1
            print(f"closures_only misread {text!r}\n  wanted {sorted(want)}, got {sorted(got)}")
    print("ledger.py self-check: " + ("ok" if not bad else f"{bad} FAILED"))
    return 1 if bad else 0


def main() -> int:
    out = [
        "# The polish ledger",
        "",
        "Generated by `scripts/ledger.py` from the per-surface audits beside it —",
        "do not edit this file. Each row is `<id> <severity> <state> — what it is`,",
        "and the audit named in the heading carries the line it lives on, why it",
        "costs a developer something, the fix shape and the frames it was seen on.",
        "",
        "A row closes on a captured before/after pair and on nothing else.",
        "",
        f"_{datetime.datetime.now(datetime.UTC):%Y-%m-%d %H:%MZ}_",
        "",
    ]
    tally = {"open": 0, "CLOSED": 0}
    # Every audit's fixed section, read once, so a cross-surface closure counts.
    elsewhere: set[str] = set()
    for path in D.glob("audit-*.md"):
        _, _, fixed = path.read_text().partition("\n## fixed")
        if fixed:
            elsewhere |= closed_ids(closures_only(fixed))
    for path in sorted(D.glob("audit-*.md")):
        surface = path.stem[len("audit-"):]
        p = PREFIX.get(surface, surface[:1].upper())
        body = path.read_text()
        head, _, fixed = body.partition("\n## fixed")
        done = closed_rows(closures_only(fixed)) if fixed else set()

        out += [f"## {surface} — [{path.name}]({path.name})", ""]
        for line in head.splitlines():
            m = ROW.match(line)
            if not m:
                continue
            n, rest = m.group(1), m.group(2)
            what = rest.split(" — ", 1)[0]
            sev = SEV.search(line)
            state = "CLOSED" if n in done or f"{p}{n}" in elsewhere else "open"
            tally[state] += 1
            out.append(f"- `{p}{n}`".ljust(9) + f" {sev.group(1) if sev else '?':<5}{state:<7}{what}")
        out.append("")

    (D / "LEDGER.md").write_text("\n".join(out) + "\n")
    print(f"{D.relative_to(ROOT)}/LEDGER.md — {tally['open']} open, {tally['CLOSED']} closed")
    return 0


if __name__ == "__main__":
    sys.exit(selftest() if "--selftest" in sys.argv else main())
