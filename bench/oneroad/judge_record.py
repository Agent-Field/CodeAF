#!/usr/bin/env python3
"""Record one cell's three judgements as one row, or refuse.

The judge is a Sonnet subagent answering JUDGE.md, three times, independently.
This script is what turns those three answers into a number somebody can quote
next month: it pins the prompt they were given, takes the median rather than
the mean, and REFUSES the row when the three judges did not agree closely
enough for a median to mean anything.

Usage:
    judge_record.py --cell <dir> --judgements a.json b.json c.json [--csv judgements.csv]

Each judgement file is the JSON object JUDGE.md asks for. The cell directory is
where the run's own facts live (meta.json: task, harness, wall_s, cost_usd,
road, parts, peak_workers).
"""
import argparse, csv, hashlib, json, os, statistics, sys

DIMS = ["requirement_coverage", "correctness", "scope_discipline",
        "completeness", "report_honesty"]
# A spread this wide means the three judges read different work. The median of
# {0,2,4} is 2, which looks like a considered middle and is not one.
UNRELIABLE_SPREAD = 2
PROMPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "JUDGE.md")


def prompt_sha() -> str:
    with open(PROMPT, "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()[:16]


def load(path: str) -> dict:
    with open(path) as fh:
        raw = fh.read().strip()
    # A judge that fenced its answer despite being told not to is a formatting
    # slip and not a failed judgement; anything else is refused.
    if raw.startswith("```"):
        raw = raw.split("\n", 1)[1].rsplit("```", 1)[0]
    obj = json.loads(raw)
    missing = [d for d in DIMS if not isinstance(obj.get(d), int)]
    if missing:
        raise ValueError(f"{path}: missing or non-integer {missing}")
    for d in DIMS:
        if not 0 <= obj[d] <= 4:
            raise ValueError(f"{path}: {d}={obj[d]} outside 0-4")
    return obj


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--cell", required=True)
    ap.add_argument("--judgements", nargs="+", required=True)
    ap.add_argument("--csv", default="judgements.csv")
    ap.add_argument("--judge-model", default="claude-sonnet-5")
    args = ap.parse_args()

    if len(args.judgements) < 3:
        print("three judgements or none: one judge's opinion is not a measurement",
              file=sys.stderr)
        return 2

    judged = [load(p) for p in args.judgements]
    with open(os.path.join(args.cell, "meta.json")) as fh:
        meta = json.load(fh)

    row = {
        "task": meta["task"], "harness": meta["harness"], "seed": meta.get("seed", 1),
        "wall_s": meta.get("wall_s", ""), "cost_usd": meta.get("cost_usd", ""),
        "road": meta.get("road", ""), "armed": meta.get("armed", ""),
        "parts": meta.get("parts", 0), "peak_workers": meta.get("peak_workers", ""),
        "tests_before": meta.get("tests_before", ""), "tests_after": meta.get("tests_after", ""),
        "judge_model": args.judge_model,
        "judge_prompt": os.path.basename(PROMPT), "judge_sha": prompt_sha(),
        "n_judges": len(judged),
    }

    unreliable = []
    total = 0
    for dim in DIMS:
        scores = [j[dim] for j in judged]
        median = int(statistics.median(scores))
        spread = max(scores) - min(scores)
        row[dim] = median
        row[dim + "_spread"] = spread
        if spread >= UNRELIABLE_SPREAD:
            unreliable.append(dim)
        total += median

    # The hard gate. Any judge seeing a weakened test is enough: it is a claim
    # about a fact in the diff, not an opinion, and two judges missing it is
    # two judges who did not look rather than a vote against.
    weakened = any(j.get("weakened_tests") for j in judged)
    row["weakened_tests"] = weakened
    row["total"] = 0 if weakened else total
    row["unreliable"] = ",".join(unreliable)
    row["one_line"] = judged[0].get("one_line", "")

    exists = os.path.exists(args.csv)
    with open(args.csv, "a", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=list(row), quoting=csv.QUOTE_MINIMAL)
        if not exists:
            writer.writeheader()
        writer.writerow(row)

    if weakened:
        print(f"{row['task']}/{row['harness']}: FAILED — a test was weakened")
    elif unreliable:
        print(f"{row['task']}/{row['harness']}: total {total}/20 · "
              f"unreliable on {', '.join(unreliable)} — quote those as a range")
    else:
        print(f"{row['task']}/{row['harness']}: total {total}/20")
    return 0


if __name__ == "__main__":
    sys.exit(main())
