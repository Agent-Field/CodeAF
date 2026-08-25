#!/usr/bin/env python3
"""collect.py — every cell's meta.json as one CSV, in README.md's columns."""
import csv, glob, json, os, subprocess, sys

ROOT = os.path.dirname(os.path.abspath(__file__))
FIELDS = ["task", "harness", "seed", "outcome", "wall_s", "wall_s_active",
          "cost_usd", "cost_source",
          "tests_before", "tests_after", "changed_files",
          "road", "armed", "parts", "peak_workers", "refused",
          "exit", "model", "base_commit", "loadavg_before", "loadavg_after"]


def active_wall(cell, row):
    """The wall clock with the settle window taken back off it.

    An aforge cell is called finished when its store has been silent for
    SILENCE_SECONDS, so the wall the cell timed ALWAYS carries that window on the
    end — a cell that worked for 66 seconds records 246. Quoting that beside a
    peer arm's wall, which has no such window, would hand every peer a free three
    minutes. So the real end of work is derived rather than assumed: the newest
    substantive file in the session store (presence.json excluded, it is a
    heartbeat) is the last moment anything happened, and the difference between
    that and the store's oldest file is the work.

    The peer arms have no settle window and no store, so their wall IS active and
    is passed through unchanged. A DNF has no end of work to find and reports
    nothing rather than a number.
    """
    if not row.get("harness", "").startswith("aforge"):
        return row.get("wall_s", "")
    if row.get("outcome") != "OK":
        return ""
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
        return ""
    return int(round(newest - oldest))


def main():
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.join(ROOT, "results", "results.csv")
    rows = []
    for path in sorted(glob.glob(os.path.join(ROOT, "results", "*", "meta.json"))):
        with open(path) as fh:
            row = json.load(fh)
        row["_cell"] = os.path.dirname(path)
        rows.append(row)
    for row in rows:
        row["wall_s_active"] = active_wall(row.pop("_cell"), row)
    rows.sort(key=lambda r: (str(r.get("task")), str(r.get("harness"))))
    with open(out, "w", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=FIELDS, extrasaction="ignore")
        writer.writeheader()
        for row in rows:
            writer.writerow(row)
    print(f"{len(rows)} cells -> {out}")


if __name__ == "__main__":
    main()
