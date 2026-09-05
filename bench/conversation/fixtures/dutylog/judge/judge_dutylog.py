#!/usr/bin/env python3
"""Judge a dutylog repair by behaviour, on inputs the workspace never held.

WHAT IT IS. The `multi-defect-pipeline` scenario hands a harness a small Python
project with four independent defects, one in each of parsing, validation,
aggregate and report. This program is the judge, and it lives outside the
workspace the harness can see: it imports the repaired project, exercises it on
data that was never in that directory, runs the command-line pipeline as a
subprocess, and writes one JSON verdict per group.

WHAT IT DELIBERATELY DOES NOT DO. It never reads the project's source text. A
judge that grep'd for `csv.reader` or for a `(-minutes, site)` sort key would
fail a healthy rewrite and pass a hard-coded lookup table, which is the reverse
of what a benchmark is for. Every case here is input in, behaviour out.

Cases per group are boundary-shaped where the module has a boundary: the ISO
year that a December date can belong to, a window that includes both its ends,
minutes at exactly 1 and exactly 1440, halves that round up, and ties whose
order must not depend on the order records arrived in. The integration group
runs the CLI on files that exercise all four at once — a repair of three
modules out of four cannot pass it.

    judge_dutylog.py --project DIR --out RESULT.json

It exits 0 when it produced a verdict, whatever that verdict says, and non-zero
only when it could not judge at all. The caller reads the JSON.
"""
import argparse
from datetime import date, datetime
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import traceback

BOM = "\ufeff"
CASES = []
GROUPS = ("parsing", "validation", "aggregate", "report", "integration")
CLI_TIMEOUT_S = 120

# Expected text is spelled out, never read back off the project under judgement:
# a judge that builds its expectation out of the code it is judging agrees with
# whatever that code does.
EMPTY_REPORT = ("week      site                      hours\n"
                "TOTAL                                 0.0\n")


def case(group, name):
    def keep(function):
        CASES.append((group, name, function))
        return function
    return keep


class Wrong(Exception):
    """A behaviour that does not match the contract."""


def same(got, want, what):
    if got != want:
        raise Wrong("%s: expected %r, got %r" % (what, want, got))


def truthy(got, what):
    if not got:
        raise Wrong(what)


class Project:
    """The repaired project, imported from a copy of the workspace."""

    def __init__(self, directory):
        self.directory = str(Path(directory).resolve())
        sys.path.insert(0, self.directory)
        importlib.invalidate_caches()
        for name in ("dutylog", "dutylog.parsing", "dutylog.validation",
                     "dutylog.aggregate", "dutylog.report", "dutylog.cli"):
            sys.modules.pop(name, None)
        self.parsing = importlib.import_module("dutylog.parsing")
        self.validation = importlib.import_module("dutylog.validation")
        self.aggregate = importlib.import_module("dutylog.aggregate")
        self.report = importlib.import_module("dutylog.report")

    # ── helpers the cases share ────────────────────────────────────────────
    def entries(self, text):
        """Parsed and validated entries, or a Wrong if either stage refused."""
        checked = self.validation.validate(self.parsing.parse_entries(text))
        if checked.errors:
            raise Wrong("this log should have no rejected records: %r" % (checked.errors,))
        return checked.entries

    def raw(self, entry_id, start="2027-03-01T08:00", worker="pat",
            site="Annex", minutes="60", status="logged", line_no=2):
        return self.parsing.RawEntry(line_no, entry_id, start, worker, site, minutes, status)

    def entry(self, entry_id, start, site="Annex", minutes=60, line_no=2):
        return self.validation.Entry(line_no, entry_id, datetime.fromisoformat(start),
                                     "pat", site, minutes)

    def run_cli(self, text, *arguments, encoding="utf-8"):
        """The pipeline as an operator runs it: a file in, an exit status out."""
        with tempfile.TemporaryDirectory() as folder:
            log = Path(folder) / "duty.csv"
            log.write_bytes(text.encode(encoding))
            return subprocess.run(
                [sys.executable, "-s", "-E", "-m", "dutylog.cli", "--input", str(log)]
                + list(arguments),
                cwd=self.directory, capture_output=True, text=True, timeout=CLI_TIMEOUT_S,
                env={"PATH": os.environ.get("PATH", ""), "PYTHONDONTWRITEBYTECODE": "1"})


# ── parsing: the file format, and nothing about whether a record makes sense ──

HEADER_LINE = "entry_id,start,worker,site,minutes,status\n"

QUOTED_LOG = (
    BOM + HEADER_LINE +
    "H-1,2027-03-01T08:00,pat,\"Ward \"\"B\"\", North\",60,logged\r\n"
    "\n"
    "  H-2  ,  2027-03-02T09:15  ,  ada  ,  Ferry  ,  30  ,  logged  \n"
    "\n"
)


@case("parsing", "a quoted field keeps its comma and its doubled quotes")
def _(project):
    entries = project.parsing.parse_entries(QUOTED_LOG)
    same([e.entry_id for e in entries], ["H-1", "H-2"], "the records read")
    same(entries[0].site, 'Ward "B", North', "the quoted site")


@case("parsing", "a byte order mark is not part of the first field")
def _(project):
    with_mark = project.parsing.parse_entries(QUOTED_LOG)
    without = project.parsing.parse_entries(QUOTED_LOG.lstrip(BOM))
    same([e.entry_id for e in with_mark], [e.entry_id for e in without],
         "the same file with and without a byte order mark")


@case("parsing", "line numbers count blank lines that separate records")
def _(project):
    same([e.line_no for e in project.parsing.parse_entries(QUOTED_LOG)], [2, 4],
         "the line each record came from")


@case("parsing", "surrounding spaces are not part of a value")
def _(project):
    entries = project.parsing.parse_entries(QUOTED_LOG)
    same((entries[1].entry_id, entries[1].site, entries[1].minutes), ("H-2", "Ferry", "30"),
         "the trimmed fields")


@case("parsing", "a header that is not the contract is refused")
def _(project):
    for text in ("worker,when,minutes\npat,now,60\n",
                 "entry_id,start,worker,site,minutes\nH-1,x,pat,Annex,60\n"):
        try:
            project.parsing.parse_entries(text)
        except project.parsing.ParseError:
            continue
        raise Wrong("a wrong header was accepted: %r" % text)


@case("parsing", "a record with the wrong number of fields is refused by line")
def _(project):
    try:
        project.parsing.parse_entries(HEADER_LINE + "H-1,2027-03-01T08:00,pat,Annex,60\n")
    except project.parsing.ParseError as refusal:
        truthy("line 2" in str(refusal), "the refusal names line 2: %s" % refusal)
        return
    raise Wrong("a five-field record was accepted")


@case("parsing", "a file with no header at all is refused")
def _(project):
    for text in ("", "\n\n"):
        try:
            project.parsing.parse_entries(text)
        except project.parsing.ParseError:
            continue
        raise Wrong("a file with no header was accepted: %r" % text)


# ── validation: the record rules ─────────────────────────────────────────────

@case("validation", "an identifier repeated far later in the file is rejected")
def _(project):
    records = [project.raw("K-1", line_no=2), project.raw("K-2", line_no=3),
               project.raw("K-3", line_no=4), project.raw("K-1", line_no=5)]
    checked = project.validation.validate(records)
    same(checked.errors, ["line 5: duplicate entry_id"], "the rejected record")
    same([e.entry_id for e in checked.entries], ["K-1", "K-2", "K-3"], "the accepted records")


@case("validation", "a void record still owns its identifier")
def _(project):
    records = [project.raw("K-9", status="void", line_no=2), project.raw("K-9", line_no=3)]
    checked = project.validation.validate(records)
    same(checked.errors, ["line 3: duplicate entry_id"], "the rejected record")
    same(checked.entries, [], "a void record is not an entry")


@case("validation", "an identifier repeated on the next line is still rejected")
def _(project):
    checked = project.validation.validate([project.raw("K-1", line_no=2),
                                           project.raw("K-1", line_no=3)])
    same(checked.errors, ["line 3: duplicate entry_id"], "the rejected record")


@case("validation", "an identifier from a rejected record is not remembered")
def _(project):
    records = [project.raw("K-5", minutes="0", line_no=2), project.raw("K-5", line_no=3)]
    checked = project.validation.validate(records)
    same(checked.errors, ["line 2: minutes out of range"], "only the first record is rejected")
    same([e.entry_id for e in checked.entries], ["K-5"], "the second record is accepted")


@case("validation", "minutes are accepted at 1 and at 1440 and nowhere outside")
def _(project):
    ok = project.validation.validate([project.raw("K-1", minutes="1", line_no=2),
                                      project.raw("K-2", minutes="1440", line_no=3)])
    same(ok.errors, [], "the ends of the range are inside it")
    same([e.minutes for e in ok.entries], [1, 1440], "the accepted minutes")
    bad = project.validation.validate([project.raw("K-3", minutes="0", line_no=4),
                                       project.raw("K-4", minutes="1441", line_no=5),
                                       project.raw("K-5", minutes="-30", line_no=6)])
    same(bad.errors, ["line 4: minutes out of range", "line 5: minutes out of range",
                      "line 6: minutes out of range"], "the records outside the range")


@case("validation", "minutes that are not a whole number are rejected as such")
def _(project):
    checked = project.validation.validate([project.raw("K-1", minutes="60.0", line_no=2),
                                           project.raw("K-2", minutes="", line_no=3),
                                           project.raw("K-3", minutes="an hour", line_no=4)])
    same(checked.errors, ["line 2: minutes is not a whole number",
                          "line 3: minutes is not a whole number",
                          "line 4: minutes is not a whole number"], "the unusable minutes")


@case("validation", "the other rules, each with its own message")
def _(project):
    checked = project.validation.validate([
        project.raw("", line_no=2),
        project.raw("K-2", start="2027-03-02", line_no=3),
        project.raw("K-3", status="cancelled", line_no=4)])
    same(checked.errors, ["line 2: missing entry_id", "line 3: bad start timestamp",
                          "line 4: unknown status"], "one message per broken rule")


@case("validation", "the first rule a record breaks is the one reported")
def _(project):
    records = [project.raw("K-1", line_no=2), project.raw("K-1", start="never", line_no=3)]
    checked = project.validation.validate(records)
    same(checked.errors, ["line 3: bad start timestamp"], "the timestamp outranks the duplicate")


# ── aggregate: the window and the ISO week ───────────────────────────────────

@case("aggregate", "a December date belongs to the ISO year of its week")
def _(project):
    entries = [project.entry("A-1", "2024-12-30T08:00"), project.entry("A-2", "2025-01-02T08:00")]
    same(project.aggregate.weekly_totals(entries, date(2024, 12, 30), date(2025, 1, 5)),
         {"2025-W01": {"Annex": 120}}, "one ISO week, whatever the calendar year says")


@case("aggregate", "two different ISO weeks are never merged")
def _(project):
    entries = [project.entry("A-1", "2025-01-02T08:00", minutes=30),
               project.entry("A-2", "2025-12-29T08:00", minutes=45)]
    same(project.aggregate.weekly_totals(entries, date(2025, 1, 1), date(2025, 12, 31)),
         {"2025-W01": {"Annex": 30}, "2026-W01": {"Annex": 45}}, "two weeks, two buckets")


@case("aggregate", "a January date can belong to the previous ISO year")
def _(project):
    entries = [project.entry("A-1", "2027-01-01T08:00", minutes=15)]
    same(project.aggregate.weekly_totals(entries, date(2026, 12, 28), date(2027, 1, 3)),
         {"2026-W53": {"Annex": 15}}, "the week of 2027-01-01")


@case("aggregate", "the window includes both of its own days")
def _(project):
    entries = [project.entry("A-1", "2027-03-01T00:00", minutes=10),
               project.entry("A-2", "2027-03-07T23:59", minutes=20)]
    same(project.aggregate.weekly_totals(entries, date(2027, 3, 1), date(2027, 3, 7)),
         {"2027-W09": {"Annex": 30}}, "both ends of the window")


@case("aggregate", "a day outside the window contributes nothing")
def _(project):
    entries = [project.entry("A-1", "2027-02-28T23:59"), project.entry("A-2", "2027-03-08T00:00")]
    same(project.aggregate.weekly_totals(entries, date(2027, 3, 1), date(2027, 3, 7)), {},
         "a window that contains no entry")


@case("aggregate", "minutes accumulate per site and per week")
def _(project):
    entries = [project.entry("A-1", "2027-03-01T08:00", site="Annex", minutes=30),
               project.entry("A-2", "2027-03-02T08:00", site="Annex", minutes=45),
               project.entry("A-3", "2027-03-03T08:00", site="Ferry", minutes=15),
               project.entry("A-4", "2027-03-09T08:00", site="Annex", minutes=60)]
    same(project.aggregate.weekly_totals(entries, date(2027, 3, 1), date(2027, 3, 14)),
         {"2027-W09": {"Annex": 75, "Ferry": 15}, "2027-W10": {"Annex": 60}},
         "the totals per week and site")


# ── report: a rendering that depends on the totals and nothing else ──────────

@case("report", "equal totals are ordered by site, not by arrival")
def _(project):
    one = project.report.render({"2027-W09": {"Ferry": 60, "Annex": 60, "Byre": 60}})
    other = project.report.render({"2027-W09": {"Byre": 60, "Ferry": 60, "Annex": 60}})
    same(one, other, "the same totals built in a different order")
    truthy(one.index("Annex") < one.index("Byre") < one.index("Ferry"),
           "tied sites are in name order:\n" + one)


@case("report", "ties are broken in code point order, not case-insensitively")
def _(project):
    rendered = project.report.render({"2027-W09": {"annex": 30, "Byre": 30}})
    truthy(rendered.index("Byre") < rendered.index("annex"),
           "'Byre' sorts before 'annex':\n" + rendered)


@case("report", "a bigger total comes before a smaller one")
def _(project):
    rendered = project.report.render({"2027-W09": {"Annex": 30, "Ferry": 90, "Byre": 60}})
    truthy(rendered.index("Ferry") < rendered.index("Byre") < rendered.index("Annex"),
           "minutes descending:\n" + rendered)


@case("report", "halves round up, and hours keep one decimal")
def _(project):
    same([str(project.report.hours(minutes)) for minutes in (0, 15, 45, 90, 1440)],
         ["0.0", "0.3", "0.8", "1.5", "24.0"], "hours from minutes")


@case("report", "the total is the total of the minutes, not of the rounded hours")
def _(project):
    rendered = project.report.render({"2027-W09": {"Annex": 15, "Byre": 15}})
    truthy(rendered.rstrip("\n").endswith("0.5"),
           "30 minutes is 0.5 hours, not the 0.6 that two rounded rows sum to:\n" + rendered)


@case("report", "weeks come out in ISO order across a year boundary")
def _(project):
    rendered = project.report.render({"2026-W01": {"Annex": 60}, "2025-W52": {"Byre": 60}})
    truthy(rendered.index("2025-W52") < rendered.index("2026-W01"),
           "weeks ascending:\n" + rendered)


@case("report", "an empty set of totals still renders a report")
def _(project):
    same(project.report.render({}), EMPTY_REPORT, "the report for no totals")


# ── integration: the four modules through the command line ───────────────────

PIPELINE_LOG = (
    BOM + HEADER_LINE +
    "P-1,2026-12-28T08:00,pat,\"Ward \"\"B\"\", North\",90,logged\n"
    "P-2,2026-12-29T09:00,ada,Ferry,45,logged\n"
    "\n"
    "P-3,2027-01-02T07:30,pat,Ferry,45,logged\n"
    "P-4,2027-01-05T08:00,ada,\"Ward \"\"B\"\", North\",30,logged\n"
    "P-5,2027-01-06T08:00,pat,Annex,30,logged\n"
    "P-6,2027-01-07T08:00,ada,Annex,120,void\n"
    "P-7,2027-02-01T08:00,pat,Ferry,600,logged\n"
)

# Row order carries no meaning, so a report built from the same records in a
# different order must be the same bytes.
PIPELINE_LOG_SHUFFLED = (
    BOM + HEADER_LINE +
    "P-5,2027-01-06T08:00,pat,Annex,30,logged\n"
    "P-7,2027-02-01T08:00,pat,Ferry,600,logged\n"
    "P-2,2026-12-29T09:00,ada,Ferry,45,logged\n"
    "P-4,2027-01-05T08:00,ada,\"Ward \"\"B\"\", North\",30,logged\n"
    "\n"
    "P-1,2026-12-28T08:00,pat,\"Ward \"\"B\"\", North\",90,logged\n"
    "P-6,2027-01-07T08:00,ada,Annex,120,void\n"
    "P-3,2027-01-02T07:30,pat,Ferry,45,logged\n"
)

# The window is not the one the workspace's own suite uses: a repair that
# hard-codes an answer for the visible window has nothing to say here.
PIPELINE_WINDOW = ("--from", "2026-12-27", "--to", "2027-01-08")

PIPELINE_REPORT = """week      site                      hours
2026-W53  Ferry                       1.5
2026-W53  Ward "B", North             1.5
2027-W01  Annex                       0.5
2027-W01  Ward "B", North             0.5
TOTAL                                 4.0
"""

REJECTED_LOG = (
    HEADER_LINE +
    "R-1,2027-03-01T08:00,pat,Annex,60,logged\n"
    "R-2,2027-03-02T08:00,ada,Ferry,1441,logged\n"
    "R-3,2027-03-03T08:00,pat,Annex,60,logged\n"
    "R-1,2027-03-04T08:00,ada,Ferry,60,logged\n"
)


@case("integration", "the pipeline reports exactly this")
def _(project):
    done = project.run_cli(PIPELINE_LOG, *PIPELINE_WINDOW)
    same(done.returncode, 0, "the exit status (stderr: %s)" % done.stderr.strip())
    same(done.stdout, PIPELINE_REPORT, "the report")
    same(done.stderr, "", "nothing on stderr")


@case("integration", "the same records in another order give the same bytes")
def _(project):
    first = project.run_cli(PIPELINE_LOG, *PIPELINE_WINDOW)
    second = project.run_cli(PIPELINE_LOG_SHUFFLED, *PIPELINE_WINDOW)
    same(second.returncode, 0, "the exit status (stderr: %s)" % second.stderr.strip())
    same(second.stdout, first.stdout, "the report from reordered records")


@case("integration", "rejected records are named on stderr and stop the report")
def _(project):
    done = project.run_cli(REJECTED_LOG, "--from", "2027-03-01", "--to", "2027-03-07")
    same(done.returncode, 1, "the exit status")
    same(done.stdout, "", "stdout stays empty")
    same(done.stderr.splitlines(),
         ["line 3: minutes out of range", "line 5: duplicate entry_id"], "the messages")


@case("integration", "a file it cannot parse exits 3 and reports nothing")
def _(project):
    done = project.run_cli("worker,when\npat,now\n", "--from", "2027-03-01", "--to", "2027-03-07")
    same(done.returncode, 3, "the exit status")
    same(done.stdout, "", "stdout stays empty")
    truthy(done.stderr.strip() != "", "the refusal is explained on stderr")


@case("integration", "a window that runs backwards is a usage error")
def _(project):
    done = project.run_cli(PIPELINE_LOG, "--from", "2027-01-10", "--to", "2026-12-28")
    same(done.returncode, 2, "the exit status")
    same(done.stdout, "", "stdout stays empty")


@case("integration", "an empty window still reports its header and total")
def _(project):
    done = project.run_cli(PIPELINE_LOG, "--from", "2027-06-01", "--to", "2027-06-07")
    same(done.returncode, 0, "the exit status (stderr: %s)" % done.stderr.strip())
    same(done.stdout, EMPTY_REPORT, "the empty report")


def judge(directory):
    verdict = {"schema": 1, "project": str(Path(directory).resolve()),
               "groups": {group: {"cases": 0, "failed": []} for group in GROUPS}}
    try:
        project = Project(directory)
    except BaseException as problem:  # a project that will not import fails everything
        detail = "".join(traceback.format_exception_only(type(problem), problem)).strip()
        for group in GROUPS:
            verdict["groups"][group]["failed"].append(
                {"case": "the project imports", "detail": detail})
            verdict["groups"][group]["cases"] = 1
        project = None
    if project is not None:
        for group, name, function in CASES:
            verdict["groups"][group]["cases"] += 1
            try:
                function(project)
            except KeyboardInterrupt:
                raise
            except BaseException as problem:
                detail = str(problem) or "".join(
                    traceback.format_exception_only(type(problem), problem)).strip()
                verdict["groups"][group]["failed"].append(
                    {"case": name, "detail": detail[:600]})
    for group in GROUPS:
        state = verdict["groups"][group]
        state["passed"] = state["cases"] > 0 and not state["failed"]
    verdict["cases"] = sum(state["cases"] for state in verdict["groups"].values())
    verdict["failed"] = sum(len(state["failed"]) for state in verdict["groups"].values())
    verdict["passed"] = all(state["passed"] for state in verdict["groups"].values())
    return verdict


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--project", required=True, help="the repaired project to judge")
    parser.add_argument("--out", required=True, help="where to write the JSON verdict")
    args = parser.parse_args(argv)
    if not Path(args.project, "dutylog").is_dir():
        print("no dutylog package under %s" % args.project, file=sys.stderr)
    verdict = judge(args.project)
    Path(args.out).write_text(json.dumps(verdict, indent=1, sort_keys=True) + "\n")
    for group in GROUPS:
        state = verdict["groups"][group]
        print("%-12s %s  %d/%d" % (group, "pass" if state["passed"] else "FAIL",
                                   state["cases"] - len(state["failed"]), state["cases"]))
        for failure in state["failed"]:
            print("    · %s: %s" % (failure["case"], failure["detail"].replace("\n", " ")[:200]))
    print("verdict: %s (%d of %d cases failed)"
          % ("pass" if verdict["passed"] else "FAIL", verdict["failed"], verdict["cases"]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
