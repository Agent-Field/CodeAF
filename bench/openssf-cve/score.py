#!/usr/bin/env python3
"""Score a run and print it beside DeepSource's published rows.

    score.py <results-dir>

Reads every cell's meta.json and judge.json, counts TP/FP/TN/FN the way
DeepSource's judged rows count them, and writes summary.json. Precision,
recall, F1 and accuracy are theirs by construction:

    precision = TP / (TP + FP)      recall = TP / (TP + FN)
    F1 = harmonic mean              accuracy = (TP + TN) / judged rows

Three things are counted beside the score and printed with it, because each
one changes what the number means:

    unavailable   rows whose repository could not be fetched (not judged)
    tool_errors   cells where the driver crashed, timed out or wrote nothing
                  usable — they ARE judged, as an empty report, which is what
                  the tool delivered; a run with many of them is a broken run
    unjudged      cells with findings and no verdict yet (judge.py failed or
                  NO_JUDGE was set) — the score is partial until they are judged
"""
import json
import os
import sys


def metrics(tp, fp, tn, fn):
    n = tp + fp + tn + fn
    p = tp / (tp + fp) if tp + fp else 0.0
    r = tp / (tp + fn) if tp + fn else 0.0
    f1 = 2 * p * r / (p + r) if p + r else 0.0
    acc = (tp + tn) / n if n else 0.0
    return {"TP": tp, "FP": fp, "TN": tn, "FN": fn, "rows": n,
            "precision": round(p * 100, 2), "recall": round(r * 100, 2),
            "f1": round(f1 * 100, 2), "accuracy": round(acc * 100, 2)}


def main():
    root = sys.argv[1].rstrip("/")
    meta = json.load(open(os.path.join(root, "meta.json")))
    rows = [l.rstrip("\n").split("\t") for l in open(os.path.join(root, "rows.tsv")) if l.strip()]
    counts = {"TP": 0, "FP": 0, "TN": 0, "FN": 0}
    unavailable, errors, unjudged, judged = [], [], [], 0
    cost, tool_seconds, calls = 0.0, 0, 0
    by_cwe = {}
    for cve, variant in rows:
        cell = os.path.join(root, cve, variant)
        m = json.load(open(os.path.join(cell, "meta.json"))) if os.path.exists(os.path.join(cell, "meta.json")) else {}
        if m.get("stage") == "unavailable":
            unavailable.append("%s/%s" % (cve, variant)); continue
        if m.get("tool_error"):
            errors.append("%s/%s: %s" % (cve, variant, m["tool_error"]))
        tool_seconds += int(m.get("tool_seconds") or 0)
        cpath = os.path.join(cell, "cost.json")
        if os.path.exists(cpath):
            try:
                cost += float(json.load(open(cpath)).get("cost_usd") or 0)
            except Exception:
                pass
        jpath = os.path.join(cell, "judge.json")
        if not os.path.exists(jpath):
            unjudged.append("%s/%s" % (cve, variant)); continue
        j = json.load(open(jpath))
        judged += 1
        calls += int(bool(j.get("called")))
        for k in counts:
            counts[k] += int(j.get(k, 0))
        if variant == "unfixed":
            truth = json.load(open(os.path.join(cell, "truth.json")))
            for cwe in truth.get("cwes", []) or ["unknown"]:
                d = by_cwe.setdefault(cwe, {"found": 0, "of": 0})
                d["of"] += 1; d["found"] += int(j.get("TP", 0))

    ours = metrics(counts["TP"], counts["FP"], counts["TN"], counts["FN"])
    summary = {
        "tool": meta.get("tool"), "model": meta.get("model"), "mode": meta.get("mode"),
        "seed": meta.get("seed"), "set": meta.get("set"), "judge_model": meta.get("judge_model"),
        "rows_planned": len(rows), "rows_judged": judged, "unavailable": unavailable,
        "tool_errors": errors, "unjudged": unjudged, "judge_calls": calls,
        "tool_cost_usd": round(cost, 4), "tool_seconds": tool_seconds,
        "metrics": ours, "by_cwe_recall": by_cwe,
        "partial": bool(unjudged), "rig_rev": meta.get("rig_rev"), "dataset_rev": meta.get("dataset_rev"),
        "bin_sha256_16": meta.get("bin_sha256_16"),
    }
    json.dump(summary, open(os.path.join(root, "summary.json"), "w"), indent=2)

    here = os.path.dirname(os.path.abspath(__file__))
    cmp_path = os.path.join(here, "comparison", "deepsource-2026-04.json")
    comparison = json.load(open(cmp_path)) if os.path.exists(cmp_path) else {"tools": {}}

    label = "%s (%s, %s)" % (meta.get("tool"), meta.get("mode"), meta.get("model"))
    print()
    print("%-38s %7s %7s %7s %7s   %3s %3s %3s %3s  %s" % ("tool", "F1", "prec", "recall", "acc", "TP", "FP", "TN", "FN", "rows"))
    print("%-38s %7.2f %7.2f %7.2f %7.2f   %3d %3d %3d %3d  %d%s" % (
        label[:38], ours["f1"], ours["precision"], ours["recall"], ours["accuracy"],
        ours["TP"], ours["FP"], ours["TN"], ours["FN"], ours["rows"], " (partial)" if unjudged else ""))
    for name, t in sorted(comparison["tools"].items(), key=lambda kv: -kv[1]["f1"]):
        print("%-38s %7.2f %7.2f %7.2f %7.2f   %3d %3d %3d %3d  %d" % (
            ("  " + name + " [DeepSource 2026-04]")[:38], t["f1"], t["precision"], t["recall"], t["accuracy"],
            t["TP"], t["FP"], t["TN"], t["FN"], t["rows"]))
    print()
    print("judged %d of %d rows; unavailable %d; tool errors %d; unjudged %d; judge calls %d; tool cost $%.2f; tool time %ds"
          % (judged, len(rows), len(unavailable), len(errors), len(unjudged), calls, cost, tool_seconds))
    if errors:
        print("tool errors:")
        for e in errors[:20]:
            print("  " + e)
    if unjudged:
        print("UNJUDGED — the score above is partial. judge.py %s" % root)
    print("summary: %s" % os.path.join(root, "summary.json"))


if __name__ == "__main__":
    main()
