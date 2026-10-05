#!/usr/bin/env python3
"""Curate a run into the evidence folder, in DeepSource's row shape.

    export.py <results-dir> [<results-dir> ...]        -> evidence/<run>/
    export.py --calibration <replay-results-dir> ...   -> evidence/calibration/

A run directory holds about 30 KB a cell that is evidence and, until cell.sh
started parking it, megabytes a cell that is not. This copies the evidence
into the tree, in the field names DeepSource's judged-results JSONL uses, so a
reader can diff the two files side by side:

    cve_id, variant, cve_explanation, original_explanations,
    detected_issues[{file, explanation, position{begin{line}}}],
    TP, FP, TN, FN, judge_reasoning

plus the fields theirs does not carry and ours should: the judge model, whether
a call was made, the tool's self-reported spend, seconds, exit code and any
tool error. Beside it go summary.json, the run's meta.json, the prompts the
tool and the judge were given (the brief of the first cell that has one, and
judge.py's own text), and the per-cell raw findings.json under raw/.

RESULTS.md is written by hand from these files; this script never touches it."""
import json
import os
import shutil
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
EVIDENCE = os.path.join(HERE, "evidence")


def row(cell, cve, variant):
    truth = json.load(open(os.path.join(cell, "truth.json")))
    findings = json.load(open(os.path.join(cell, "findings.json")))["findings"]
    meta = json.load(open(os.path.join(cell, "meta.json")))
    jpath = os.path.join(cell, "judge.json")
    judge = json.load(open(jpath)) if os.path.exists(jpath) else {}
    cost = {}
    if os.path.exists(os.path.join(cell, "cost.json")):
        cost = json.load(open(os.path.join(cell, "cost.json")))
    return {
        "cve_id": cve,
        "variant": variant,
        "cve_explanation": truth.get("osv_details", ""),
        "original_explanations": [w.get("explanation", "") for w in truth.get("weaknesses", [])],
        "weakness_locations": ["%s:%s" % (w["file"], w.get("line")) for w in truth.get("weaknesses", [])],
        "cwes": truth.get("cwes", []),
        "detected_issues": [
            {"file": f.get("file"), "explanation": " — ".join(x for x in (f.get("title"), f.get("description")) if x),
             "position": {"begin": {"line": f.get("line"), "column": 1}, "end": {"line": f.get("line"), "column": 1}},
             "cwe": f.get("cwe"), "severity": f.get("severity")}
            for f in findings
        ],
        "TP": judge.get("TP", 0), "FP": judge.get("FP", 0), "TN": judge.get("TN", 0), "FN": judge.get("FN", 0),
        "judged": bool(judge),
        "judge_reasoning": judge.get("reasoning", ""),
        "judge_model": judge.get("judge_model"),
        "judge_called": judge.get("called", False),
        "judge_finding_index": judge.get("finding_index"),
        "tool_cost_usd": cost.get("cost_usd"),
        "tool_seconds": meta.get("tool_seconds"),
        "tool_exit_code": meta.get("exit_code"),
        "tool_error": meta.get("tool_error"),
    }


def export_run(results):
    results = results.rstrip("/")
    name = os.path.basename(results)
    out = os.path.join(EVIDENCE, name)
    os.makedirs(os.path.join(out, "raw"), exist_ok=True)
    rows = [l.rstrip("\n").split("\t") for l in open(os.path.join(results, "rows.tsv")) if l.strip()]
    brief_written = False
    with open(os.path.join(out, "judged.jsonl"), "w") as fh:
        for cve, variant in rows:
            cell = os.path.join(results, cve, variant)
            if not os.path.exists(os.path.join(cell, "truth.json")):
                continue
            fh.write(json.dumps(row(cell, cve, variant), ensure_ascii=False) + "\n")
            shutil.copy(os.path.join(cell, "findings.json"), os.path.join(out, "raw", "%s_%s.json" % (cve, variant)))
            brief = os.path.join(cell, "brief.md")
            if os.path.exists(brief) and not brief_written:
                shutil.copy(brief, os.path.join(out, "brief.md"))
                brief_written = True
    for f in ("summary.json", "meta.json", "rows.tsv"):
        if os.path.exists(os.path.join(results, f)):
            shutil.copy(os.path.join(results, f), os.path.join(out, f))
    # The judge's text, verbatim, beside the verdicts it produced.
    sys.path.insert(0, HERE)
    import judge  # noqa: E402
    open(os.path.join(out, "judge-prompt.md"), "w").write(judge.SYSTEM + "\n")
    print("exported %s -> %s (%d rows)" % (name, os.path.relpath(out, HERE), len(rows)))


def export_calibration(results):
    results = results.rstrip("/")
    name = os.path.basename(results)
    out = os.path.join(EVIDENCE, "calibration")
    os.makedirs(out, exist_ok=True)
    src = os.path.join(results, "calibration.json")
    if not os.path.exists(src):
        print("no calibration.json in %s — run calibrate.py first" % results, file=sys.stderr)
        return
    shutil.copy(src, os.path.join(out, name + ".json"))
    print("exported calibration %s" % name)


def main():
    args = sys.argv[1:]
    if not args:
        print(__doc__)
        sys.exit(2)
    if args[0] == "--calibration":
        for r in args[1:]:
            export_calibration(r)
        return
    for r in args:
        export_run(r)


if __name__ == "__main__":
    main()
