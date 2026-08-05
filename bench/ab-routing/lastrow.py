#!/usr/bin/env python3
"""Print the last row of a results jsonl as one progress line.

This is a file rather than an inline `python3 -c` or a heredoc because both of
those have now failed in run-arm.sh for different reasons. The single-quoted
`-c` form cannot carry the double quotes an f-string needs. Replacing it with
`tail -1 "$JSONL" | python3 - <<'PY'` fixed that and broke something worse: the
heredoc *is* stdin, so the piped row never arrived and every cell printed a
JSONDecodeError over a result that had actually landed correctly.

A path argument has neither problem.
"""
import json
import sys


def main():
    with open(sys.argv[1]) as f:
        row = json.loads(f.readlines()[-1])
    line = (f"score {row['score']:.3f}  success={str(row['success']):5s}  "
            f"${row['cost_usd']:.4f}  {row['wall_seconds']}s  "
            f"{row['turns']} turns  {row['leaves_done']}/{row['leaves_total']} leaves  "
            f"stops={row['stop_reasons']}")
    routing = row.get("routing") or {}
    if routing:
        picks = sorted(routing.get("by_model", {}).items(), key=lambda kv: -kv[1])
        short = ",".join(f"{slug.split('/')[-1][:18]}:{n}" for slug, n in picks)
        line += f"  models[{short}]"
        if routing.get("escalated_calls"):
            line += f"  escalated={routing['escalated_calls']}"
    print(line)


if __name__ == "__main__":
    main()
