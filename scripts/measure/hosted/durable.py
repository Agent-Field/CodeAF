#!/usr/bin/env python3
"""Join a cell's sealed turns with the directory's head timeline.

  durable.py <cellread.json> <watch.jsonl> <cell id>

The watch file is s1probe's JSONL: one line per poll of the directory listing.
A turn becomes durable at the `durable_at` (directory clock, ms) the directory
gave the FIRST head that is this turn or a later one. Turns that never became
durable are counted, not dropped, because "nothing lost" is what item 7 checks.
seal_to_durable_ms = durable_at - sealed_at_ms. Both clocks are A's own here
(the relay container shares the host kernel clock); the watch lines also carry
the directory's `now` next to local ms so any skew is visible in the raw file.
"""
import json
import sys


def head_changes(watch, cell):
    """(head, durable_at) each time the head moves, in poll order."""
    seen, out = None, []
    for line in open(watch):
        try:
            rec = json.loads(line)
        except ValueError:   # the last line can be cut where the watcher was stopped
            continue
        row = next((c for c in rec.get("cells") or [] if c["id"] == cell), None)
        if row and row["head"] != seen:
            seen = row["head"]
            out.append((row["head"], row["durable_at"]))
    return out


def join(turns, changes):
    index = {t["id"]: t["idx"] for t in turns}
    done, out = -1, []
    for head, at in changes:
        upto = index.get(head)
        if upto is None:
            continue
        out += [(t, at) for t in turns if done < t["idx"] <= upto]
        done = max(done, upto)
    return out, [t for t in turns if t["idx"] > done]


if __name__ == "__main__":
    turns = json.load(open(sys.argv[1]))
    joined, lost = join(turns, head_changes(sys.argv[2], sys.argv[3]))
    samples = [at - t["sealed_ms"] for t, at in joined]
    print(json.dumps({"samples_ms": samples, "turns": len(turns),
                      "durable": len(joined), "not_durable": [t["id"] for t in lost]}))
