#!/usr/bin/env python3
"""finalize.py — rebuild a cell's record from what is on disk.

TWO JOBS, and both come from the same fact: everything a row needs is durable
evidence in the cell directory, so the row can always be rebuilt without re-
running anything.

  1. RESCUE. A cell whose driver died after the harness finished still has its
     store, its suite logs and its diff. This writes the meta.json and the judge
     bundle that the driver would have written, rather than throwing away a run
     that has already been paid for.
  2. BACKFILL. When a road column is added or corrected — `escalated` was added
     after wave 1 had landed — every earlier cell can be re-read against the new
     reader instead of being left with a blank column or, worse, re-run and
     quietly compared against rows measured on a different day.

It never re-runs a harness and never touches the work tree. wall_s is taken from
the driver's own log, because the clock is the one fact the directory cannot
recompute.

Usage: finalize.py <cell-dir> [<cell-dir> ...]
"""
import glob, json, os, re, subprocess, sys

ROOT = os.path.dirname(os.path.abspath(__file__))


def suite_counts(path):
    try:
        text = open(path, errors="replace").read()
    except OSError:
        return "0/0", None, None
    line = ""
    for candidate in re.findall(r"^.*\d+ (?:passed|failed).*$", text, re.M):
        line = candidate
    passed = re.search(r"(\d+) passed", line)
    failed = re.search(r"(\d+) failed", line)
    p = int(passed.group(1)) if passed else 0
    f = int(failed.group(1)) if failed else 0
    return f"{p}/{f}", p, f


def wall_from_log(cell):
    """The wall the driver timed, recovered from its own log lines."""
    try:
        text = open(os.path.join(cell, "cell.log"), errors="replace").read()
    except OSError:
        return None
    sent = re.search(r"\[(\d\d):(\d\d):(\d\d)\].*sent \(", text)
    end = None
    for match in re.finditer(r"\[(\d\d):(\d\d):(\d\d)\].*(settled at|DNF at) (\d+)s", text):
        end = match
    if sent and end:
        return int(end.group(5))
    return None


def wall_from_store(cell):
    """The wall a driver never got to write, taken from the store's own clock.

    A cell whose driver was killed (or whose settle never fired) still has the
    only two moments that matter on disk: the first and last thing the session
    wrote. It is the same span collect.py calls wall_s_active, and it is the
    honest number for a rescued row — smaller than a settled cell's wall_s
    because it carries no silence window, and labelled as such by the caller.
    """
    sessions = os.path.join(cell, "profile", "v3", "projects")
    newest = oldest = None
    for base, _, names in os.walk(sessions):
        for name in names:
            if name == "presence.json":
                continue
            try:
                stamp = os.path.getmtime(os.path.join(base, name))
            except OSError:
                continue
            newest = stamp if newest is None else max(newest, stamp)
            oldest = stamp if oldest is None else min(oldest, stamp)
    if newest is None or oldest is None:
        return None
    return int(round(newest - oldest))


def main():
    for cell in sys.argv[1:]:
        cell = cell.rstrip("/")
        name = os.path.basename(cell)
        existing = {}
        meta_path = os.path.join(cell, "meta.json")
        if os.path.exists(meta_path):
            existing = json.load(open(meta_path))

        arm = existing.get("harness") or name.rsplit("-", 2)[0]
        task = existing.get("task") or name.rsplit("-", 2)[1]

        # The store copy is the benchmark's own; fall back to the live profile
        # for a cell whose driver died before it made one.
        session = ""
        for base in ("store", "profile"):
            found = glob.glob(os.path.join(cell, base, "v3", "projects", "*", "*"))
            found = [d for d in found if os.path.isdir(d)]
            if found:
                session = found[0]
                break

        columns = {}
        if arm.startswith("aforge") and session:
            out = subprocess.run(
                [sys.executable, os.path.join(ROOT, "lib", "road.py"), session,
                 "--timeline", os.path.join(cell, "timeline.json")],
                capture_output=True, text=True)
            if out.stdout.strip():
                columns = json.loads(out.stdout)

        before, pb, fb = suite_counts(os.path.join(cell, "pytest-before.log"))
        after, pa, fa = suite_counts(os.path.join(cell, "pytest-after.log"))
        wall = wall_from_log(cell)

        meta = dict(existing)
        meta.update({
            "task": task, "harness": arm, "seed": existing.get("seed", "s1"),
            "tests_before": before, "tests_after": after,
            "passed_before": pb, "failed_before": fb,
            "passed_after": pa, "failed_after": fa,
        })
        if wall is None:
            wall = wall_from_store(cell)
            if wall is not None:
                meta["wall_source"] = "store-span (driver wrote no settle line)"
        if wall is not None and not existing.get("wall_s"):
            meta["wall_s"] = wall
        if arm.startswith("aforge"):
            for key in ("road", "armed", "parts", "peak_workers", "refused",
                        "escalated", "route", "chat_tool_calls", "pre_turn_line",
                        "checkpoint_marks", "calls", "cost_usd_calls", "endpoint_mix",
                        "mark_reader_calls", "mark_reader_model",
                        "marks", "ceiling_decision", "division", "division_why", "division_admitted",
                        "division_refused", "forks", "cost_by_role", "cost_usd_usage",
                        "cost_usd", "cost_source"):
                if key in columns:
                    meta[key] = columns[key]
            meta.setdefault("escalation_seen_on_pane", "no")
        else:
            meta.setdefault("road", "n/a"); meta.setdefault("armed", "n/a")
            meta.setdefault("peak_workers", "n/a"); meta.setdefault("refused", "n/a")
            meta.setdefault("escalated", "n/a")
            meta.setdefault("escalation_seen_on_pane", "n/a")
        meta.setdefault("outcome", "OK")
        meta.setdefault("parts", 0)

        json.dump(meta, open(meta_path, "w"), indent=2)
        subprocess.run([sys.executable, os.path.join(ROOT, "lib", "judge_bundle.py"), cell],
                       capture_output=True)
        print(f"{name}: road={meta.get('road')} escalated={meta.get('escalated')} "
              f"parts={meta.get('parts')} peak={meta.get('peak_workers')} wall={meta.get('wall_s')}")


if __name__ == "__main__":
    main()
