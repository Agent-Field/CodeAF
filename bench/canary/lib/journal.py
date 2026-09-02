#!/usr/bin/env python3
"""Read what one cell's home says about the calls it made.

The home is the only witness the rig trusts. A door's own words — a summary
line, a reply on a screen — say what the door believes; the journal under
`v3/` and the call log under `logs/` say what went over the wire, and every
figure the scoreboard carries comes from one of those two files.

Subcommands, each printing one value:

  seals    <home>   completed turns: journal `usage` rows that are not `aux`
  inflight <home>   calls that have started and not come back
  cost     <home>   dollars, summed over every call that came back
  ttft     <home>   milliseconds to the first token of the first turn call
  calls    <home>   how many calls came back
"""
import glob
import json
import os
import sys


def rows(path):
    """Yield the parsed lines of one JSON Lines file, skipping torn ones."""
    try:
        with open(path, errors="replace") as handle:
            for line in handle:
                try:
                    yield json.loads(line)
                except ValueError:
                    continue
    except OSError:
        return


def journal_rows(home):
    for path in glob.glob(os.path.join(home, "v3", "projects", "*", "*", "transcript.jsonl")):
        yield from rows(path)


def call_rows(home):
    yield from rows(os.path.join(home, "logs", "calls.jsonl"))


def usage_rows(home):
    return [row["usage"] for row in journal_rows(home) if row.get("type") == "usage" and row.get("usage")]


def seals(home):
    return sum(1 for used in usage_rows(home) if not used.get("aux"))


def inflight(home):
    started, ended = set(), set()
    for row in call_rows(home):
        (started if row.get("phase") == "start" else ended).add(row.get("id"))
    return len(started - ended)


def cost(home):
    # The call log is the one door every outbound call passes through, at both
    # surfaces; a headless run keeps no conversation journal to sum, and a
    # conversation's own seals sum to the same figure.
    total = sum(float(row.get("cost") or 0) for row in call_rows(home) if row.get("phase") != "start")
    return "%.6f" % total


def ttft(home):
    # The turn's own first call is the latency a person felt; a reflex or a
    # router confirmation that ran beside it is not. Fall back to the first
    # call with a figure at all, so a build that tags differently still
    # reports something measured rather than nothing.
    ended = [row for row in call_rows(home) if row.get("phase") != "start" and row.get("ttft_ms")]
    for row in ended:
        if row.get("tag") == "turn":
            return row["ttft_ms"]
    return ended[0]["ttft_ms"] if ended else ""


def calls(home):
    return sum(1 for row in call_rows(home) if row.get("phase") != "start")


def main(argv):
    if len(argv) != 3 or argv[1] not in ("seals", "inflight", "cost", "ttft", "calls"):
        sys.exit(__doc__)
    print(globals()[argv[1]](argv[2]))


if __name__ == "__main__":
    main(sys.argv)
