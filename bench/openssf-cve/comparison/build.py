#!/usr/bin/env python3
"""Build the comparison table from DeepSource's judged rows.

    build.py <work/deepsource> > comparison/deepsource-2026-04.json

Every figure in the table is recomputed from their per-row TP/FP/TN/FN with
score.py's own formulas, so the two sides of the printed table are added up
the same way. Their published page differs from this recomputation in one
row — Claude Code is 62.40 F1 on the page and 62.99 here — and the table
carries the recomputed value and says so, because a number we cannot
reproduce is not one we should print."""
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from score import metrics  # noqa: E402

PUBLISHED_F1 = {"deepsource": 84.51, "cursor-bugbot": 80.45, "devin": 78.08, "codex": 77.70,
                "greptile": 68.61, "claude-code": 62.40, "semgrep": 36.70, "coderabbit": 36.00}


def main():
    ds = sys.argv[1]
    rev = open(os.path.join(ds, "rev")).read().strip() if os.path.exists(os.path.join(ds, "rev")) else "unknown"
    tools = {}
    for name in sorted(os.listdir(os.path.join(ds, "judged"))):
        if not name.endswith(".jsonl"):
            continue
        tool = name[:-6]
        c = {"TP": 0, "FP": 0, "TN": 0, "FN": 0}
        for line in open(os.path.join(ds, "judged", name)):
            r = json.loads(line)
            for k in c:
                c[k] += int(r.get(k, 0))
        m = metrics(c["TP"], c["FP"], c["TN"], c["FN"])
        m["published_f1"] = PUBLISHED_F1.get(tool)
        tools[tool] = m
    json.dump({
        "source": "https://github.com/DeepSourceCorp/benchmarks",
        "revision": rev,
        "run": "DeepSource, April 2026; each tool reviewed a draft pull request adding the file(s) in full; judge Claude Opus 4.5, blind to tool",
        "rows": "165 = 82 unfixed + 83 fixed, over 85 CVEs; JavaScript and TypeScript only",
        "note": "metrics recomputed from their judged-results JSONL; published_f1 is the figure on deepsource.com/benchmarks where one was published",
        "tools": tools,
    }, sys.stdout, indent=2, sort_keys=True)
    print()


if __name__ == "__main__":
    main()
