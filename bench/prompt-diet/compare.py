#!/usr/bin/env python3
"""compare.py — two labelled runs, side by side, with a ruling.

THE RULING IS THE POINT, and it is two claims that are decided separately
because they can disagree:

  PARITY      every outcome is equal or better. A subtest that passed on the
              baseline and fails on the candidate sinks the whole wave whatever
              it saved; a cell that failed on both is not a regression; a
              subtest that passed where it used to fail is better. `skip` beside
              `pass` is NOT parity — a suite that stopped running is a suite
              that stopped proving anything — and `incomplete` on either side is
              never called equal, because a run that was cut off measured
              nothing about that test at all.

  EFFICIENCY  fewer prompt tokens per turn for that same outcome. Per TURN and
              not per run: a candidate that answers in three rounds where the
              baseline took six has a smaller bill and a bigger per-turn figure,
              and it is the per-turn figure that says whether the prefix got
              lighter. The round count is printed beside it so that the other
              reading is never hidden. And a TURN is a request that carried the
              tool block — see `Run.carries_the_belt`, which exists because the
              first comparison this bench ran reported a 94% cut that was a
              change of call mix and not a diet at all.

WHAT IS RECORDED RATHER THAN RULED ON: wall clock and dollars. `bench/e2e`'s own
README sets the thresholds this follows and the reason — two runs of IDENTICAL
code came in 15s and 24s, a 60% swing, because wall clock carries the provider's
queue as well as the work. A ruling on wall would report a regression every
other run and then nobody would read the table. Dollars move with cached share,
which moves with how the provider felt about the prefix that minute.

Usage:
  bench/prompt-diet/compare.py <baseline-label> <candidate-label> [options]

  --out-root <dir>   where the runs live (default bench/prompt-diet/out)
  --markdown <path>  also write the table to a file
"""

import json
import os
import statistics
import sys

# The outcome vocabulary, worst to best. `incomplete` sits at the bottom because
# a test that never reported is less informative than one that reported badly.
#
# FAIL, TIMEOUT AND CRASH SHARE ONE RANK, and that is deliberate. It is tempting
# to score a cell that used to time out and now fails as an improvement — it
# finished, it cost less, it said something — and this bench met exactly that on
# its first real comparison: `followup-while-working` timed out at 423s on the
# baseline and failed its assertion at 112s on the candidate, for a fifth of the
# tokens. But neither of those is the cell passing, and ranking one above the
# other would let a genuine behaviour regression walk in wearing the word
# "better" because it got there faster. A move inside the not-a-pass band is
# SAME, and the token and wall columns beside it say what actually changed.
RANK = {"incomplete": 0, "unsupported": 1, "skip": 2,
        "crash": 4, "timeout": 4, "fail": 4, "pass": 5}


def load(path, default=None):
    try:
        with open(path, "r", errors="replace") as handle:
            return json.load(handle)
    except (OSError, ValueError):
        return default


def jsonl(path):
    out = []
    try:
        handle = open(path, "r", errors="replace")
    except OSError:
        return out
    with handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            try:
                out.append(json.loads(line))
            except ValueError:
                continue
    return out


def figure(value, spelling="{:,}"):
    """Unknown renders as nothing — the emptiness law, in a table."""
    if value is None:
        return "—"
    try:
        return spelling.format(value)
    except (TypeError, ValueError):
        return str(value)


def delta(before, after, spelling="{:+,}"):
    if not isinstance(before, (int, float)) or not isinstance(after, (int, float)):
        return "—"
    return spelling.format(after - before)


def percent(before, after):
    if not isinstance(before, (int, float)) or not isinstance(after, (int, float)):
        return "—"
    if before == 0:
        return "—"
    return "{:+.1f}%".format((after - before) * 100.0 / before)


def number(value):
    """A figure that may have travelled as a JSON string.

    `bench/conversation` emits every scalar of its row through one string
    writer, so `wall_s` arrives as "22.964" rather than as 22.964 — and a
    reader that filtered on isinstance(value, float) silently dropped every
    wall clock in the run and printed a dash where the sum belonged."""
    if isinstance(value, bool) or value is None:
        return None
    if isinstance(value, (int, float)):
        return value
    try:
        return float(str(value).strip())
    except (TypeError, ValueError):
        return None


def middle(values):
    values = [n for n in (number(value) for value in values) if n is not None]
    return round(statistics.median(values), 1) if values else None


def total(values):
    values = [n for n in (number(value) for value in values) if n is not None]
    return round(sum(values), 4) if values else None


class Run:
    """Everything one label measured, read once."""

    def __init__(self, root, label):
        self.label = label
        self.root = os.path.join(root, label)
        self.meta = load(os.path.join(self.root, "meta.json"), {}) or {}
        self.outcomes = load(os.path.join(self.root, "suites", "outcomes.json"), {}) or {}
        self.cells = jsonl(os.path.join(self.root, "cells", "conversation", "results.jsonl"))
        self.wire = jsonl(os.path.join(self.root, "wire.jsonl"))
        self.e2e = self._e2e()

    def _e2e(self):
        """bench/e2e's append-only CSV, keyed by cell. The columns are read by
        NAME out of the header rather than by position, because that file's own
        law is that columns are appended and a positional reader breaks on the
        next one that lands."""
        path = os.path.join(self.root, "cells", "e2e.csv")
        rows = {}
        try:
            handle = open(path, "r", errors="replace")
        except OSError:
            return rows
        with handle:
            lines = [line.rstrip("\n") for line in handle if line.strip()]
        if not lines:
            return rows
        header = lines[0].split(",")
        for line in lines[1:]:
            values = line.split(",")
            row = dict(zip(header, values))
            if row.get("cell"):
                rows[row["cell"]] = row
        return rows

    def exists(self):
        return os.path.isdir(self.root)

    def test_outcomes(self):
        """Every subtest of every suite, flattened to suite/test → word.

        LEAVES ONLY, where a suite has leaves. `go test -v` fails the parent
        whenever any child did, so keeping both would report one broken subtest
        as two regressions and make the ruling read worse than the truth. A
        suite that reported no subtests at all is kept as itself, because a
        parent that failed with no failing child is a real event — a fixture
        that died before any subtest ever started."""
        out = {}
        for suite, body in self.outcomes.items():
            tests = body.get("tests", {})
            leaves = {name: entry for name, entry in tests.items() if "/" in name}
            for test, entry in (leaves or tests).items():
                out[test if test.startswith(suite) else suite + "/" + test] = entry["outcome"]
        return out

    def cell_outcomes(self):
        out = {}
        for row in self.cells:
            if row.get("scenario"):
                out["conversation/" + row["scenario"]] = row.get("verdict") or "incomplete"
        for cell, row in self.e2e.items():
            word = (row.get("quality_pass") or "").strip().lower()
            out["e2e/" + cell] = {"yes": "pass", "true": "pass", "1": "pass",
                                  "no": "fail", "false": "fail", "0": "fail"}.get(word, word or "incomplete")
        return out

    def guard_rows(self):
        return [row for row in self.wire if row.get("source") == "guard"]

    @staticmethod
    def carries_the_belt(row):
        """Whether this request carried the tool block, and so the prefix.

        THIS IS THE WHOLE DIFFERENCE BETWEEN A REAL NUMBER AND A FLATTERING
        ONE. A conversation does not only send turns: a title call, a memory
        reflex, a judge and a router all go out on the same wire with a few
        hundred prompt tokens and no belt at all. The first comparison this
        bench ever ran took the median over every request and reported median
        prompt tokens falling 16,473 → 938, a 94% cut — which was not a diet at
        all but a change of MIX. The candidate made 19 turn calls where the
        baseline made 67, so its median landed among the small auxiliary calls
        that both runs make. Over the requests that actually carry the prefix
        the same two runs read 17,815 → 13,962, or 21.6%: a real result, and a
        quarter of the size of the one the average told.

        A request carrying at least one tool definition is a turn. Nothing else
        on the wire carries a belt, and the count is on the guard's own
        admission row, so no body has to be kept to ask the question."""
        return (row.get("tool_count") or 0) > 0

    def turn_rows(self):
        return [row for row in self.guard_rows() if self.carries_the_belt(row)]

    def aside_rows(self):
        return [row for row in self.guard_rows() if not self.carries_the_belt(row)]

    def calllog_rows(self):
        return [row for row in self.wire if row.get("source") == "calllog"]

    def by_cell(self):
        """Guard rows grouped by the cell that made them.

        The guard's evidence directory is named `<scenario>-<arm>`, and that
        tail is what the tables are keyed by. Rolling the arm off is safe here
        because this bench runs one arm — it is comparing two builds of aforge,
        not aforge against a peer — and keeping it would put `-aforge` on the
        end of every row of every table for no information at all."""
        groups = {}
        for row in self.guard_rows():
            name = os.path.basename(row.get("cell") or "")
            if name.endswith("-aforge"):
                name = name[: -len("-aforge")]
            groups.setdefault(name or "unnamed", []).append(row)
        return groups

    def cell_wall(self):
        return {row.get("scenario"): row.get("wall_s") for row in self.cells}


def outcome_table(before, after, kind):
    """One row per named thing, and the verdict word for the pair."""
    left = before.test_outcomes() if kind == "suite" else before.cell_outcomes()
    right = after.test_outcomes() if kind == "suite" else after.cell_outcomes()
    names = sorted(set(left) | set(right))
    rows, regressions, unproven = [], [], []
    for name in names:
        was, now = left.get(name, "incomplete"), right.get(name, "incomplete")
        if was == "incomplete" or now == "incomplete":
            verdict = "unproven"
            unproven.append(name)
        elif RANK.get(now, 0) > RANK.get(was, 0):
            verdict = "better"
        elif RANK.get(now, 0) == RANK.get(was, 0):
            verdict = "same"
        else:
            verdict = "WORSE"
            regressions.append(name)
        rows.append((name, was, now, verdict))
    return rows, regressions, unproven


def main():
    args = sys.argv[1:]
    out_root = os.path.join(os.path.dirname(os.path.abspath(__file__)), "out")
    markdown_path = None
    positional = []
    index = 0
    while index < len(args):
        token = args[index]
        if token in ("--out-root", "--markdown"):
            value = args[index + 1] if index + 1 < len(args) else None
            if value is None:
                sys.stderr.write("%s needs a value\n" % token)
                return 2
            if token == "--out-root":
                out_root = value
            else:
                markdown_path = value
            index += 2
            continue
        if token in ("-h", "--help"):
            sys.stdout.write(__doc__)
            return 0
        positional.append(token)
        index += 1

    if len(positional) != 2:
        sys.stdout.write(__doc__)
        return 2

    before, after = (Run(out_root, positional[0]), Run(out_root, positional[1]))
    for run in (before, after):
        if not run.exists():
            sys.stderr.write("no run at %s — run.sh has not been run for that label\n" % run.root)
            return 1

    out = []
    add = out.append

    add("# %s → %s" % (before.label, after.label))
    add("")
    add("| | %s | %s |" % (before.label, after.label))
    add("| --- | --- | --- |")
    for key in ("revision", "model", "rig_sha", "layers"):
        add("| %s | `%s` | `%s` |" % (key.replace("_", " "),
                                      before.meta.get(key, "—"), after.meta.get(key, "—")))
    add("")

    # ── the static prefix ───────────────────────────────────────────────────
    add("## The prefix, weighed (layer A)")
    add("")
    add("| piece | %s | %s | Δ | |" % (before.label, after.label))
    add("| --- | ---: | ---: | ---: | ---: |")
    for key, name in (("prefix_prompt", "page"), ("prefix_tools", "tool block"),
                      ("prefix_bytes", "**total**")):
        add("| %s | %s | %s | %s | %s |" % (
            name, figure(before.meta.get(key)), figure(after.meta.get(key)),
            delta(before.meta.get(key), after.meta.get(key)),
            percent(before.meta.get(key), after.meta.get(key))))
    add("")

    # ── outcomes ────────────────────────────────────────────────────────────
    regressions, unproven = [], []
    for kind, title in (("suite", "Suite outcomes (layer B)"),
                        ("cell", "Cell outcomes (layer C)")):
        rows, worse, unknown = outcome_table(before, after, kind)
        regressions.extend(worse)
        unproven.extend(unknown)
        add("## " + title)
        add("")
        if not rows:
            add("Neither run measured this layer.")
            add("")
            continue
        add("| | %s | %s | |" % (before.label, after.label))
        add("| --- | --- | --- | --- |")
        for name, was, now, verdict in rows:
            add("| `%s` | %s | %s | %s |" % (name, was, now, verdict))
        add("")

    # ── tokens per turn ─────────────────────────────────────────────────────
    #
    # A TURN HERE IS ONE REQUEST ON THE WIRE, which is what the guard counts and
    # what the prefix is paid for. It is not a person's message: one message can
    # be six rounds of tool calls, and the prefix rides every one of them.
    add("## Tokens per turn (layer D)")
    add("")
    add("Over the requests that CARRY THE BELT, and so carry the prefix. A title")
    add("call, a memory reflex, a judge and a router go out on the same wire with a")
    add("few hundred prompt tokens and no tool block at all; a median over all of")
    add("them measures the mix of call kinds rather than the size of the prompt, and")
    add("it moves when a build simply needs fewer rounds. Those are counted in their")
    add("own row underneath, where they cannot flatter anything.")
    add("")
    add("| | %s | %s | Δ | |" % (before.label, after.label))
    add("| --- | ---: | ---: | ---: | ---: |")
    pairs = [
        ("turn requests", lambda run: len(run.turn_rows()) or None, "{:,}"),
        ("median prompt tokens", lambda run: middle([row.get("prompt_tokens") for row in run.turn_rows()]), "{:,}"),
        ("median cached tokens", lambda run: middle([row.get("cached_tokens") for row in run.turn_rows()]), "{:,}"),
        ("median completion tokens", lambda run: middle([row.get("completion_tokens") for row in run.turn_rows()]), "{:,}"),
        ("median request bytes", lambda run: middle([row.get("request_bytes") for row in run.turn_rows()]), "{:,}"),
        ("median tools on the belt", lambda run: middle([row.get("tool_count") for row in run.turn_rows()]), "{:,}"),
        ("median tool block bytes", lambda run: middle([row.get("tool_block_bytes") for row in run.calllog_rows()]), "{:,}"),
        ("asides (no belt)", lambda run: len(run.aside_rows()) or None, "{:,}"),
        ("median aside prompt tokens", lambda run: middle([row.get("prompt_tokens") for row in run.aside_rows()]), "{:,}"),
    ]
    prompt_before = prompt_after = None
    for name, reader, spelling in pairs:
        left, right = reader(before), reader(after)
        if name == "median prompt tokens":
            prompt_before, prompt_after = left, right
        add("| %s | %s | %s | %s | %s |" % (
            name, figure(left, spelling), figure(right, spelling),
            delta(left, right), percent(left, right)))
    add("")

    # ── per cell ────────────────────────────────────────────────────────────
    #
    # The aggregate above can hide the shape that matters: one cell that got
    # much cheaper and one that got much dearer average out to no change at
    # all, and it is the pair, not the average, that says what the diet did.
    left_cells, right_cells = before.by_cell(), after.by_cell()
    if left_cells or right_cells:
        left_outcomes, right_outcomes = before.cell_outcomes(), after.cell_outcomes()
        left_wall, right_wall = before.cell_wall(), after.cell_wall()
        add("## Per cell")
        add("")
        add("| cell · run | outcome | turn calls | median prompt tok "
            "| median cached tok | median completion tok | $ | wall s |")
        add("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |")
        for name in sorted(set(left_cells) | set(right_cells)):
            key = "conversation/" + name
            was = left_outcomes.get(key, "—")
            now = right_outcomes.get(key, "—")
            for label, everything, outcome, wall in (
                (before.label, left_cells.get(name, []), was, left_wall.get(name)),
                (after.label, right_cells.get(name, []), now, right_wall.get(name)),
            ):
                # The tokens are the turn calls'; the spend is every call the
                # cell made, because an aside is still money.
                group = [row for row in everything if Run.carries_the_belt(row)]
                add("| `%s` · %s | %s | %d | %s | %s | %s | %s | %s |" % (
                    name, label, outcome, len(group),
                    figure(middle([row.get("prompt_tokens") for row in group])),
                    figure(middle([row.get("cached_tokens") for row in group])),
                    figure(middle([row.get("completion_tokens") for row in group])),
                    figure(total([row.get("cost_usd") for row in everything]), "{:.4f}"),
                    figure(wall, "{}")))
        add("")

    # ── recorded, not ruled on ──────────────────────────────────────────────
    add("## Recorded, not ruled on")
    add("")
    add("Wall clock and dollars move with the provider's weather as much as with the")
    add("change. `bench/e2e/README.md` measured a 60% wall swing between two runs of")
    add("identical code, so these are here to be looked at and not to decide anything.")
    add("")
    add("**Spend is a sum over the requests the provider actually priced.** OpenRouter")
    add("returns no usage block on some calls — a measured cell had 5 of 7 priced — and")
    add("the guard records that as unknown rather than as zero, so this figure is a")
    add("floor and not a bill. The token columns above are unaffected: they are medians")
    add("over the requests that reported, and a request that reported nothing is absent")
    add("from them rather than counted as free.")
    add("")
    add("| | %s | %s | Δ | |" % (before.label, after.label))
    add("| --- | ---: | ---: | ---: | ---: |")
    for name, reader, spelling in (
        ("total spend $", lambda run: total([row.get("cost_usd") for row in run.guard_rows()]), "{:.4f}"),
        ("median call ms", lambda run: middle([row.get("elapsed_ms") for row in run.guard_rows()]), "{:,}"),
        ("cell wall s (sum)", lambda run: total([row.get("wall_s") for row in run.cells]), "{:.0f}"),
    ):
        left, right = reader(before), reader(after)
        add("| %s | %s | %s | %s | %s |" % (
            name, figure(left, spelling), figure(right, spelling),
            delta(left, right, "{:+.4f}" if "$" in name else "{:+,.0f}"),
            percent(left, right)))
    add("")

    # ── the ruling ──────────────────────────────────────────────────────────
    add("## Ruling")
    add("")
    if regressions:
        add("**Parity: NO.** %d outcome(s) got worse:" % len(regressions))
        add("")
        for name in regressions:
            add("- `%s`" % name)
    elif unproven:
        add("**Parity: NOT PROVEN.** Nothing got worse, but %d thing(s) were never"
            % len(unproven))
        add("measured on one side or the other:")
        add("")
        for name in unproven:
            add("- `%s`" % name)
    else:
        add("**Parity: yes.** Every outcome is equal or better.")
    add("")

    if isinstance(prompt_before, (int, float)) and isinstance(prompt_after, (int, float)):
        if prompt_after < prompt_before:
            add("**Efficiency: yes.** Median prompt tokens per turn %s → %s (%s)." % (
                figure(prompt_before), figure(prompt_after),
                percent(prompt_before, prompt_after)))
        else:
            add("**Efficiency: no.** Median prompt tokens per turn %s → %s (%s)." % (
                figure(prompt_before), figure(prompt_after),
                percent(prompt_before, prompt_after)))
    else:
        add("**Efficiency: not measured.** Neither run has a wire ledger to read;")
        add("layer D needs layer C, and layer C needs a key.")
    add("")

    text = "\n".join(out) + "\n"
    sys.stdout.write(text)
    if markdown_path:
        with open(markdown_path, "w") as handle:
            handle.write(text)
    return 1 if regressions else 0


if __name__ == "__main__":
    sys.exit(main())
