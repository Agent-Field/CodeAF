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
import glob, json, os, re, sys, time

# A LIVE STATE IS NOT THE SAME AS A LIVE TASK. wave 1h's batch cells proved it:
# part 2 made its last request at 09:23:46, reported "paused - it resumes", and
# kept the state `running` for the next fifty minutes. The settle rule refused to
# settle while anything was live, so the cell sat until the hard wall and 3100 of
# its 3610 seconds were dead. The rule was right to refuse — that refusal is what
# stopped us killing live workers — but "live" has to mean "still working", and
# the evidence for that is a request, not a label.
#
# So a task in a live state whose own journal has not seen a request for this
# long is STRANDED: counted out of the live set, named in the settle reason, and
# the row marked STRANDED rather than passed off as a clean finish. Fifteen
# minutes is comfortably longer than the slowest thing a worker legitimately
# waits on here (a full test suite or a cold build ran 6-8 minutes on this
# corpus) and far shorter than the wall it replaces.
STRANDED_SECONDS = int(os.environ.get("ONEROAD_STRANDED_SECONDS", "900"))
JOURNAL = re.compile(r"^(\d{8}-\d{6})_(\d+)(.*)\.jsonl$")

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


def journal_age(session, node_id):
    """Seconds since this node's own journal last grew, or -1 if it has none.

    Per NODE, not per session: the whole point is to tell a part that is still
    working from one that stopped in a live state while its siblings carried on,
    and a session-wide age cannot separate those two.
    """
    newest = 0.0
    for path in glob.glob(os.path.join(session, "tasks", "*.jsonl")):
        m = JOURNAL.match(os.path.basename(path))
        if not m or int(m.group(2)) != int(node_id):
            continue
        try:
            newest = max(newest, os.path.getmtime(path))
        except OSError:
            pass
    return int(time.time() - newest) if newest else -1


def main():
    session = sys.argv[1]
    nodes = read(session)
    live, landed, stranded, states = [], [], [], {}
    ages = {}
    for node in nodes:
        state = (node.get("state") or "").strip().lower()
        states[state] = states.get(state, 0) + 1
        node_id = node.get("id")
        if state in LANDED:
            landed.append(node_id)
            continue
        age = journal_age(session, node_id)
        ages[node_id] = age
        # A node with no journal at all has never started; it is live (waiting
        # for a lane), not stranded — there is nothing yet that could go stale.
        if age >= 0 and age >= STRANDED_SECONDS:
            stranded.append(node_id)
        else:
            live.append(node_id)

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
                      "stranded": len(stranded),
                      "live_ids": live, "stranded_ids": stranded,
                      "states": states, "journal_age_s": ages,
                      "stranded_after_s": STRANDED_SECONDS,
                      "newest_journal_age_s": age}))


if __name__ == "__main__":
    main()
