#!/usr/bin/env python3
"""Judge a dutylog repair from outside it: input in, output and status out.

WHAT IT IS. The `multi-defect-pipeline` scenario hands a harness a small Python
project with four defects, one each in parsing, validation, aggregation and
rendering. This program decides whether they were repaired, and it decides it
the way an operator would: by running `python3 -m dutylog.cli` on files the
workspace never contained and reading stdout, stderr and the exit status.

WHY IT IS ONLY THAT. An earlier version imported the candidate's modules into
this process and called them. That put the code under judgement and the expected
answers in one interpreter, and a root review demonstrated the consequence: a
`dutylog/__init__.py` that replaced this module's case bodies with no-ops
produced `cases: 34, failed: 0, passed: true` with this file's checksum intact.
The parent must not import the candidate. Everything below is a subprocess.

WHAT THAT COSTS. The four modules are no longer judged through their own
functions; each is judged through the command line, on inputs chosen so that
exactly one defect can change the answer. That is less coverage of a module's
internal contract and it is honest about what was actually observed. The
workspace's own unittest suite still describes the modules directly; it is
developer guidance, not the mark.

BOUNDS. Every child runs in its own session, and the group is ended by the pgid
captured when it was spawned — TERM, a grace, then KILL, whether or not the
leader is still alive, and on the ordinary path as well as the timeout one. A
descendant that ignores TERM and holds the inherited pipes open is why reading
its output is bounded too. The whole judging run has a budget; when it is spent
the remaining cases fail as unjudged rather than hanging. Because those children
are in their own sessions, an outer watchdog's signal to this process would not
reach them — so this process ends their groups on its way out, however it goes.

    judge_dutylog.py --project DIR --out RESULT.json

It exits 0 when it produced a verdict, whatever that verdict says.
"""
import argparse
import atexit
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time

CASES = []
GROUPS = ("parsing", "validation", "aggregate", "report", "integration")
BOM = "﻿"
# A correct run of this fixture takes well under a second per invocation. These
# are deliberately tight: they are the difference between a failed cell and a
# benchmark that stops.
CLI_TIMEOUT_S = float(os.environ.get("DUTYLOG_CLI_TIMEOUT_S", "20"))
BUDGET_S = float(os.environ.get("DUTYLOG_BUDGET_S", "120"))

HEADER_LINE = "entry_id,start,worker,site,minutes,status\n"
ROW = "%-9s %-25s %5s"


def report(rows, total):
    """The expected report text, spelled out from SPEC.md's own format."""
    lines = [ROW % ("week", "site", "hours")]
    lines.extend(ROW % row for row in rows)
    lines.append(ROW % ("TOTAL", "", total))
    return "\n".join(lines) + "\n"


EMPTY_REPORT = report([], "0.0")


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


class Run:
    """What one bounded invocation of the candidate's CLI produced."""

    def __init__(self, code, out, err, timed_out=False):
        self.code, self.out, self.err, self.timed_out = code, out, err, timed_out

    def check(self, code, out, err=None, what="the run"):
        if self.timed_out:
            raise Wrong("%s: no answer within %gs — the process group was killed"
                        % (what, CLI_TIMEOUT_S))
        same(self.code, code, what + ": exit status (stderr: %s)" % self.err.strip()[:200])
        same(self.out, out, what + ": stdout")
        if err is not None:
            same(self.err.splitlines(), err, what + ": stderr")


# Every group this process has started and not yet finished with. The identity
# is the pgid captured at spawn — `start_new_session=True` makes the child a
# session and group leader, so its pgid IS its pid, and that number stays usable
# after the leader has exited. Asking os.getpgid() for it later does not: a
# leader that has already gone raises, while its descendants carry on.
ACTIVE_GROUPS = set()
# Long enough for an ordinary program to finish on TERM, short enough that a
# stubborn one does not become the cell's runtime.
GROUP_GRACE_S = 0.5
PIPE_GRACE_S = 2.0


def end_group(pgid, grace=GROUP_GRACE_S):
    """Kill a whole process group, whether or not its leader is still there.

    Best effort and bounded: TERM, a fixed grace, then KILL unconditionally. It
    never waits on the leader, because the leader is not the point — a
    descendant that ignored TERM and inherited the pipes is.
    """
    if grace > 0:
        try:
            os.killpg(pgid, signal.SIGTERM)
        except (ProcessLookupError, PermissionError):
            pass
        else:
            time.sleep(grace)
    try:
        os.killpg(pgid, signal.SIGKILL)
    except (ProcessLookupError, PermissionError):
        pass
    ACTIVE_GROUPS.discard(pgid)


def end_all_groups():
    """Leave nothing behind, on the way out of this process for any reason."""
    for pgid in sorted(ACTIVE_GROUPS):
        end_group(pgid, grace=0)


def collect(child, timeout):
    """Read what a child said, bounded: a held-open pipe is not a reason to wait."""
    try:
        return child.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        for stream in (child.stdout, child.stderr):
            try:
                if stream is not None:
                    stream.close()
            except OSError:
                pass
        try:
            child.wait(timeout=1)
        except subprocess.TimeoutExpired:
            pass
        return "", ""


class Judging:
    """A project on disk, run from outside, under a deadline."""

    def __init__(self, directory):
        self.directory = str(Path(directory).resolve())
        self.deadline = time.monotonic() + BUDGET_S

    def out_of_budget(self):
        return time.monotonic() >= self.deadline

    def cli(self, text, *arguments, encoding="utf-8"):
        """`python3 -m dutylog.cli` on a file, in a group this ends either way.

        The group is ended on the ordinary path too. A CLI that exits 0 having
        left something of its own running has left it running.
        """
        with tempfile.TemporaryDirectory() as folder:
            log = Path(folder) / "duty.csv"
            log.write_bytes(text.encode(encoding))
            argv = [sys.executable, "-s", "-E", "-m", "dutylog.cli", "--input", str(log)]
            argv.extend(arguments)
            child = subprocess.Popen(
                argv, cwd=self.directory, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                text=True, start_new_session=True,
                env={"PATH": os.environ.get("PATH", ""), "PYTHONDONTWRITEBYTECODE": "1"})
            pgid = child.pid  # the leader of its own new session: pgid == pid
            ACTIVE_GROUPS.add(pgid)
            allowance = max(0.5, min(CLI_TIMEOUT_S, self.deadline - time.monotonic()))
            try:
                try:
                    out, err = child.communicate(timeout=allowance)
                    return Run(child.returncode, out, err)
                except subprocess.TimeoutExpired:
                    end_group(pgid)
                    out, err = collect(child, PIPE_GRACE_S)
                    return Run(None, out or "", err or "", timed_out=True)
            finally:
                end_group(pgid, grace=0)


# ── parsing: a file only this module can spoil ───────────────────────────────
#
# Quoting and the byte order mark, on records whose dates, identifiers and
# totals cannot trouble any of the other three modules.

QUOTED_LOG = (
    BOM + HEADER_LINE +
    "H-1,2027-03-01T08:00,pat,\"Ward \"\"B\"\", North\",90,logged\r\n"
    "\n"
    "  H-2  ,  2027-03-02T09:15  ,  ada  ,  Ferry  ,  45  ,  logged  \n"
    "\n"
)
SAFE_WEEK = ("--from", "2027-03-01", "--to", "2027-03-07")


@case("parsing", "a quoted comma, a doubled quote and a byte order mark all read")
def _(judging):
    judging.cli(QUOTED_LOG, *SAFE_WEEK).check(
        0, report([("2027-W09", 'Ward "B", North', "1.5"), ("2027-W09", "Ferry", "0.8")], "2.3"),
        [], what="a quoted log")


@case("parsing", "the same file without the byte order mark reads the same")
def _(judging):
    with_mark = judging.cli(QUOTED_LOG, *SAFE_WEEK)
    without = judging.cli(QUOTED_LOG.lstrip(BOM), *SAFE_WEEK)
    truthy(not without.timed_out, "the run finished")
    same((without.code, without.out), (with_mark.code, with_mark.out),
         "the file with and without a byte order mark")


@case("parsing", "surrounding spaces are not part of a value")
def _(judging):
    padded = ("  " + HEADER_LINE.rstrip("\n") + "  \n"
              "  H-1 , 2027-03-01T08:00 , pat , Ferry , 90 , logged \n")
    judging.cli(padded, *SAFE_WEEK).check(
        0, report([("2027-W09", "Ferry", "1.5")], "1.5"), [], what="a padded log")


@case("parsing", "a header that is not the contract is refused, and nothing is reported")
def _(judging):
    done = judging.cli("worker,when,minutes\npat,now,60\n", *SAFE_WEEK)
    done.check(3, "", what="a wrong header")
    truthy(done.err.strip() != "", "the refusal is explained on stderr")


@case("parsing", "a record with the wrong number of fields is refused by line")
def _(judging):
    done = judging.cli(HEADER_LINE + "H-1,2027-03-01T08:00,pat,Ferry\n", *SAFE_WEEK)
    done.check(3, "", what="a short record")
    truthy("line 2" in done.err, "the refusal names line 2: " + done.err.strip()[:200])


# ── validation: rejection paths, which never reach the other three ───────────

def logged(line_id, day, site="Ferry", minutes="60", status="logged", hour="08:00"):
    return "%s,2027-03-%02dT%s,pat,%s,%s,%s\n" % (line_id, day, hour, site, minutes, status)


@case("validation", "an identifier repeated far later in the file is rejected")
def _(judging):
    text = HEADER_LINE + logged("K-1", 1) + logged("K-2", 2) + logged("K-3", 3) + logged("K-1", 4)
    judging.cli(text, *SAFE_WEEK).check(1, "", ["line 5: duplicate entry_id"],
                                        what="a distant repeat")


@case("validation", "an identifier repeated on the next line is rejected too")
def _(judging):
    text = HEADER_LINE + logged("K-1", 1) + logged("K-1", 2)
    judging.cli(text, *SAFE_WEEK).check(1, "", ["line 3: duplicate entry_id"],
                                        what="an adjacent repeat")


@case("validation", "a void record still owns its identifier")
def _(judging):
    text = HEADER_LINE + logged("K-9", 1, status="void") + logged("K-9", 2)
    judging.cli(text, *SAFE_WEEK).check(1, "", ["line 3: duplicate entry_id"],
                                        what="a void record's identifier")


@case("validation", "an identifier from a rejected record is not remembered")
def _(judging):
    text = HEADER_LINE + logged("K-5", 1, minutes="0") + logged("K-5", 2, minutes="90")
    judging.cli(text, *SAFE_WEEK).check(1, "", ["line 2: minutes out of range"],
                                        what="an identifier on a rejected record")


@case("validation", "minutes are inside the range at 1 and at 1440 and nowhere outside")
def _(judging):
    inside = HEADER_LINE + logged("K-1", 1, minutes="1") + logged("K-2", 2, site="Annex",
                                                                 minutes="1440")
    judging.cli(inside, *SAFE_WEEK).check(
        0, report([("2027-W09", "Annex", "24.0"), ("2027-W09", "Ferry", "0.0")], "24.0"),
        [], what="the ends of the range")
    outside = (HEADER_LINE + logged("K-3", 1, minutes="0") + logged("K-4", 2, minutes="1441")
               + logged("K-5", 3, minutes="-30"))
    judging.cli(outside, *SAFE_WEEK).check(
        1, "", ["line 2: minutes out of range", "line 3: minutes out of range",
                "line 4: minutes out of range"], what="minutes outside the range")


@case("validation", "minutes that are not a whole number say so")
def _(judging):
    text = (HEADER_LINE + logged("K-1", 1, minutes="60.0") + logged("K-2", 2, minutes="")
            + logged("K-3", 3, minutes="an hour"))
    judging.cli(text, *SAFE_WEEK).check(
        1, "", ["line 2: minutes is not a whole number",
                "line 3: minutes is not a whole number",
                "line 4: minutes is not a whole number"], what="unusable minutes")


@case("validation", "the other rules, each with its own message and in file order")
def _(judging):
    text = (HEADER_LINE + logged("", 1) + "K-2,2027-03-02,pat,Ferry,60,logged\n"
            + logged("K-3", 3, status="cancelled"))
    judging.cli(text, *SAFE_WEEK).check(
        1, "", ["line 2: missing entry_id", "line 3: bad start timestamp",
                "line 4: unknown status"], what="one message per broken rule")


@case("validation", "the first rule a record breaks is the one reported")
def _(judging):
    text = HEADER_LINE + logged("K-1", 1) + "K-1,never,pat,Ferry,60,logged\n"
    judging.cli(text, *SAFE_WEEK).check(1, "", ["line 3: bad start timestamp"],
                                        what="a record that breaks two rules")


@case("validation", "an unpadded month, day or hour is still a timestamp")
def _(judging):
    # SPEC.md says %Y-%m-%dT%H:%M, which accepts 2027-3-1T8:00. This is written
    # down as accepted rather than left for somebody to discover.
    text = HEADER_LINE + "K-1,2027-3-1T8:00,pat,Ferry,90,logged\n"
    judging.cli(text, *SAFE_WEEK).check(
        0, report([("2027-W09", "Ferry", "1.5")], "1.5"), [], what="an unpadded timestamp")


# ── aggregate: windows and ISO weeks, on files nothing else can spoil ────────

@case("aggregate", "a December date belongs to the ISO year of its week")
def _(judging):
    text = (HEADER_LINE + "A-1,2024-12-30T08:00,pat,Ferry,90,logged\n"
            + "A-2,2025-01-02T08:00,pat,Ferry,30,logged\n")
    judging.cli(text, "--from", "2024-12-30", "--to", "2025-01-05").check(
        0, report([("2025-W01", "Ferry", "2.0")], "2.0"), [], what="one ISO week")


@case("aggregate", "a January date can belong to the previous ISO year")
def _(judging):
    text = HEADER_LINE + "A-1,2027-01-01T08:00,pat,Ferry,15,logged\n"
    judging.cli(text, "--from", "2026-12-28", "--to", "2027-01-03").check(
        0, report([("2026-W53", "Ferry", "0.3")], "0.3"), [], what="the week of 2027-01-01")


@case("aggregate", "two different ISO weeks are never merged")
def _(judging):
    text = (HEADER_LINE + "A-1,2025-01-02T08:00,pat,Ferry,30,logged\n"
            + "A-2,2025-12-29T08:00,pat,Ferry,45,logged\n")
    judging.cli(text, "--from", "2025-01-01", "--to", "2025-12-31").check(
        0, report([("2025-W01", "Ferry", "0.5"), ("2026-W01", "Ferry", "0.8")], "1.3"),
        [], what="two weeks")


@case("aggregate", "the window includes both of its own days and excludes the rest")
def _(judging):
    text = (HEADER_LINE + "A-1,2027-02-28T23:59,pat,Ferry,600,logged\n"
            + "A-2,2027-03-01T00:00,pat,Ferry,30,logged\n"
            + "A-3,2027-03-07T23:59,pat,Ferry,60,logged\n"
            + "A-4,2027-03-08T00:00,pat,Ferry,600,logged\n")
    judging.cli(text, *SAFE_WEEK).check(
        0, report([("2027-W09", "Ferry", "1.5")], "1.5"), [], what="both ends of the window")


@case("aggregate", "a window with nothing in it still reports")
def _(judging):
    text = HEADER_LINE + "A-1,2027-03-01T08:00,pat,Ferry,60,logged\n"
    judging.cli(text, "--from", "2027-06-01", "--to", "2027-06-07").check(
        0, EMPTY_REPORT, [], what="an empty window")


# ── report: ordering and rounding, on totals nothing else can spoil ──────────

@case("report", "equal totals are ordered by site, not by arrival")
def _(judging):
    text = (HEADER_LINE + logged("R-1", 1, site="Ferry") + logged("R-2", 2, site="Annex")
            + logged("R-3", 3, site="Byre"))
    judging.cli(text, *SAFE_WEEK).check(
        0, report([("2027-W09", "Annex", "1.0"), ("2027-W09", "Byre", "1.0"),
                   ("2027-W09", "Ferry", "1.0")], "3.0"), [], what="three tied sites")


@case("report", "ties are broken in code point order, not case-insensitively")
def _(judging):
    text = HEADER_LINE + logged("R-1", 1, site="annex", minutes="30") + \
        logged("R-2", 2, site="Byre", minutes="30")
    judging.cli(text, *SAFE_WEEK).check(
        0, report([("2027-W09", "Byre", "0.5"), ("2027-W09", "annex", "0.5")], "1.0"),
        [], what="a tie across cases")


@case("report", "the same records in another order are the same bytes")
def _(judging):
    first = (HEADER_LINE + logged("R-1", 1, site="Ferry") + logged("R-2", 2, site="Annex"))
    second = (HEADER_LINE + logged("R-2", 2, site="Annex") + logged("R-1", 1, site="Ferry"))
    one, other = judging.cli(first, *SAFE_WEEK), judging.cli(second, *SAFE_WEEK)
    truthy(not (one.timed_out or other.timed_out), "both runs finished")
    same(other.out, one.out, "the report from reordered records")


@case("report", "a bigger total comes first, and halves round up")
def _(judging):
    text = (HEADER_LINE + logged("R-1", 1, site="Annex", minutes="15")
            + logged("R-2", 2, site="Ferry", minutes="45"))
    # 15 and 45 minutes are 0.3 and 0.8; the total is one hour, not the 1.1 that
    # two rounded rows sum to.
    judging.cli(text, *SAFE_WEEK).check(
        0, report([("2027-W09", "Ferry", "0.8"), ("2027-W09", "Annex", "0.3")], "1.0"),
        [], what="ordering and rounding")


@case("report", "weeks come out in ISO order across a year boundary")
def _(judging):
    text = (HEADER_LINE + "R-1,2026-01-02T08:00,pat,Ferry,60,logged\n"
            + "R-2,2025-12-22T08:00,pat,Annex,30,logged\n")
    judging.cli(text, "--from", "2025-12-22", "--to", "2026-01-04").check(
        0, report([("2025-W52", "Annex", "0.5"), ("2026-W01", "Ferry", "1.0")], "1.5"),
        [], what="weeks ascending")


# ── integration: everything at once, which needs all four ────────────────────

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

# Row order carries no meaning, so the same records in another order are the
# same report.
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

# Not the window the workspace's own suite uses: an answer hard-coded for that
# one has nothing to say here.
PIPELINE_WINDOW = ("--from", "2026-12-27", "--to", "2027-01-08")

PIPELINE_REPORT = report([
    ("2026-W53", "Ferry", "1.5"),
    ("2026-W53", 'Ward "B", North', "1.5"),
    ("2027-W01", "Annex", "0.5"),
    ("2027-W01", 'Ward "B", North', "0.5"),
], "4.0")

REJECTED_LOG = (
    HEADER_LINE +
    "R-1,2027-03-01T08:00,pat,Annex,60,logged\n"
    "R-2,2027-03-02T08:00,ada,Ferry,1441,logged\n"
    "R-3,2027-03-03T08:00,pat,Annex,60,logged\n"
    "R-1,2027-03-04T08:00,ada,Ferry,60,logged\n"
)


@case("integration", "the pipeline reports exactly this")
def _(judging):
    judging.cli(PIPELINE_LOG, *PIPELINE_WINDOW).check(
        0, PIPELINE_REPORT, [], what="the whole pipeline")


@case("integration", "the same records in another order give the same bytes")
def _(judging):
    judging.cli(PIPELINE_LOG_SHUFFLED, *PIPELINE_WINDOW).check(
        0, PIPELINE_REPORT, [], what="the pipeline on reordered records")


@case("integration", "rejected records are named on stderr and stop the report")
def _(judging):
    judging.cli(REJECTED_LOG, "--from", "2027-03-01", "--to", "2027-03-07").check(
        1, "", ["line 3: minutes out of range", "line 5: duplicate entry_id"],
        what="a log with two bad records")


@case("integration", "a window that runs backwards is a usage error")
def _(judging):
    judging.cli(PIPELINE_LOG, "--from", "2027-01-08", "--to", "2026-12-27").check(
        2, "", what="a backwards window")


@case("integration", "an empty window still reports its header and total")
def _(judging):
    judging.cli(PIPELINE_LOG, "--from", "2027-06-01", "--to", "2027-06-07").check(
        0, EMPTY_REPORT, [], what="a window with no records in it")


def judge(directory):
    verdict = {"schema": 2, "project": str(Path(directory).resolve()),
               "method": "black-box: the candidate runs only as a child process",
               "groups": {group: {"cases": 0, "failed": []} for group in GROUPS}}
    judging = Judging(directory)
    for group, name, function in CASES:
        verdict["groups"][group]["cases"] += 1
        if judging.out_of_budget():
            verdict["groups"][group]["failed"].append(
                {"case": name, "detail": "the judging budget of %gs ran out before this case"
                                         % BUDGET_S})
            continue
        try:
            function(judging)
        except KeyboardInterrupt:
            raise
        except Wrong as problem:
            verdict["groups"][group]["failed"].append({"case": name, "detail": str(problem)[:600]})
        except Exception as problem:  # a rig fault must not read as a pass
            verdict["groups"][group]["failed"].append(
                {"case": name, "detail": "%s: %s" % (type(problem).__name__, problem)})
    for group in GROUPS:
        state = verdict["groups"][group]
        state["passed"] = state["cases"] > 0 and not state["failed"]
    verdict["cases"] = sum(state["cases"] for state in verdict["groups"].values())
    verdict["failed"] = sum(len(state["failed"]) for state in verdict["groups"].values())
    verdict["passed"] = all(state["passed"] for state in verdict["groups"].values())
    return verdict


def _stop(signum, frame):
    """A watchdog's TERM must not orphan a candidate's process group."""
    end_all_groups()
    os._exit(128 + signum)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--project", required=True, help="the repaired project to judge")
    parser.add_argument("--out", required=True, help="where to write the JSON verdict")
    args = parser.parse_args(argv)
    # The outer timeout(1) signals THIS process; the candidate's children are in
    # their own sessions and would not hear it. So they are cleaned up here, on
    # the signalled path and on the ordinary one.
    atexit.register(end_all_groups)
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, _stop)
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
