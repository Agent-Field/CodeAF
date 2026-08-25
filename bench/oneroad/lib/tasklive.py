#!/usr/bin/env python3
"""tasklive.py — is any task of this session still running?

WHY THIS EXISTS. The settle rule was "the store has been quiet for 180 seconds",
and that is correct for the WORDS road, where the only writer is the
conversation. It is WRONG for the task road, and it destroyed six wave-1f cells:
a worker that has just typed `pytest` writes nothing at all for minutes, the
store goes quiet, the cell is called settled, the tmux session is reaped and the
task is killed in the middle of its test run. The rows came back with 0 files
landed over an 18-minute wall — work that had been done and was then thrown away.

So silence is no longer sufficient. A cell is settled only when the conversation
is idle AND every task it started has LANDED.

WHAT COUNTS AS LANDED IS AN ALLOW-LIST, AND THAT DIRECTION IS THE WHOLE POINT.
Anything not recognised as a terminal state is treated as LIVE, so a state this
build has not met yet — a rename, a new intermediate rung — makes a cell wait
rather than makes it kill a worker. Waiting is bounded by the hard wall; killing
is not bounded by anything and loses the work.

Sources, both of them the session's own:
  tasks.json        the checkpoint: every node and its last recorded state. It is
                    written at admission and again when a node lands, so a task
                    that is running reads `running` for as long as it runs.
  tasks/<id>.jsonl  the node journals, one `call` line per request. Their mtime
                    is the last moment a worker did anything, which is how a
                    still-thinking task is told from an abandoned one.

Usage: tasklive.py <session-dir>   → JSON {live, landed, states, newest_journal_age_s}
"""
import glob, json, os, sys, time

# A node is finished in one of these. Everything else — running, queued,
# claimed, pending, mending, waiting, and anything added later — is live.
LANDED = {"done", "failed", "unverified", "cancelled", "canceled",
          "incomplete", "needs-your-look", "needs_your_look", "dropped"}


def read(session):
    try:
        doc = json.load(open(os.path.join(session, "tasks.json")))
    except (OSError, ValueError):
        return []
    return doc.get("nodes") or []


def main():
    session = sys.argv[1]
    nodes = read(session)
    live, landed, states = [], [], {}
    for node in nodes:
        state = (node.get("state") or "").strip().lower()
        states[state] = states.get(state, 0) + 1
        (landed if state in LANDED else live).append(node.get("id"))

    # How long since any worker wrote anything. Reported rather than acted on:
    # the caller's own quiet window is the timer, and this is the evidence for
    # what that window was measuring.
    newest = 0.0
    for path in glob.glob(os.path.join(session, "tasks", "*.jsonl")):
        try:
            newest = max(newest, os.path.getmtime(path))
        except OSError:
            pass
    age = int(time.time() - newest) if newest else -1

    print(json.dumps({"live": len(live), "landed": len(landed),
                      "live_ids": live, "states": states,
                      "newest_journal_age_s": age}))


if __name__ == "__main__":
    main()
