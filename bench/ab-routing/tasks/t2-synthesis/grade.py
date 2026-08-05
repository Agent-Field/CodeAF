#!/usr/bin/env python3
"""Grade a T2 (corpus synthesis) submission. Deterministic; no LLM judge.

Eight independent checks, each worth one point:

  1  findings.json exists, parses, and has exactly the seven required keys
  2  services_on_deprecated_queue   (set equality)
  3  total_incident_minutes         (exact)
  4  most_impacted_service          (exact)
  5  monthly_cost_usd_cents         (exact)
  6  unowned_services               (set equality)
  7  superseded_docs + contradictions (both set equality)
  8  REPORT.md coverage and citations (mechanical)

Check 1 is a gate on checks 2-7 only: a missing or unparseable findings.json
cannot be scored field by field, but REPORT.md can still be, and a run that
wrote a good report and no JSON is a different failure from one that wrote
nothing. Keeping them separable is what makes the failure mode legible.

Usage: python3 grade.py <workspace-dir> [--json]
"""
import argparse
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import truth  # noqa: E402

REQUIRED_KEYS = {
    "services_on_deprecated_queue", "total_incident_minutes",
    "most_impacted_service", "monthly_cost_usd_cents", "unowned_services",
    "superseded_docs", "contradictions",
}

# Findings that REPORT.md has to actually mention. Each is (label, predicate);
# the predicate looks for the value in the prose, tolerant of separators a
# writer would reasonably use, because the check is "did you say it", not "did
# you format it my way".
MIN_WORDS = 400


def find_file(root, name):
    """Locate a deliverable, preferring the workspace root.

    The brief says root, and a file written three directories down is a real
    instruction-following miss -- but scoring it as "produced nothing" would
    confuse that with a run that never wrote anything, which is a different and
    much worse failure. So it is found, and the location is recorded.
    """
    direct = os.path.join(root, name)
    if os.path.isfile(direct):
        return direct, "."
    for dirpath, dirnames, files in os.walk(root):
        dirnames[:] = [d for d in dirnames
                       if d not in {".git", "__pycache__", ".venv", "obs", "corpus"}]
        if name in files:
            return os.path.join(dirpath, name), os.path.relpath(dirpath, root)
    return None, None


def as_set(value):
    return frozenset(value) if isinstance(value, (list, tuple, set)) else None


def contradiction_set(value):
    """Normalise contradictions to a set of triples, tolerating stray keys."""
    if not isinstance(value, list):
        return None
    out = set()
    for item in value:
        if not isinstance(item, dict):
            return None
        try:
            out.add((str(item["claim_doc"]).strip().zfill(2),
                     str(item["authoritative_doc"]).strip().zfill(2),
                     str(item["field"]).strip()))
        except KeyError:
            return None
    return frozenset(out)


def number(value):
    if isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, float) and value.is_integer():
        return int(value)
    if isinstance(value, str):
        cleaned = value.replace(",", "").replace("$", "").strip()
        try:
            return int(cleaned)
        except ValueError:
            return None
    return None


def grade_report(path, key):
    """Mechanical checks on REPORT.md. No judgement, no model."""
    checks = {}
    if path is None:
        return {"present": False, "checks": {}, "ok": False, "words": 0}
    with open(path, encoding="utf-8", errors="replace") as f:
        text = f.read()
    words = len(re.findall(r"\S+", text))
    lower = text.lower()

    checks["length"] = words >= MIN_WORDS
    # every document the findings actually rest on has to be cited as "doc NN"
    cited = {m.group(1) for m in re.finditer(r"\bdoc(?:ument)?\s*#?\s*(\d{2})\b", lower)}
    needed = {"01", "02", "03", "04", "06", "08", "09"}
    checks["citations"] = needed.issubset(cited)
    checks["cited_docs"] = sorted(cited)

    def mentions_number(n):
        # 490 / 257820 / 257,820 / 2,578.20 are all the writer saying the number
        variants = {str(n), f"{n:,}"}
        if n >= 100:
            variants |= {f"{n // 100:,}.{n % 100:02d}", f"{n // 100}.{n % 100:02d}"}
        return any(v in text for v in variants)

    checks["total_minutes_stated"] = mentions_number(key["total_incident_minutes"])
    checks["cost_stated"] = mentions_number(key["monthly_cost_usd_cents"])
    checks["most_impacted_stated"] = key["most_impacted_service"] in lower
    checks["unowned_stated"] = all(s in lower for s in key["unowned_services"])
    checks["deprecated_count_stated"] = all(
        s in lower for s in key["services_on_deprecated_queue"])
    checks["contradictions_discussed"] = (
        lower.count("contradict") + lower.count("conflict") + lower.count("disagree")) >= 2
    checks["rules_cited"] = "rules.md" in lower or re.search(r"\brule\s*\d", lower) is not None

    boolean = {k: v for k, v in checks.items() if isinstance(v, bool)}
    return {"present": True, "words": words, "checks": checks,
            "passed": sum(boolean.values()), "total": len(boolean),
            "ok": all(boolean.values())}


def grade(workspace):
    key = truth.answer()
    result = {"task": "t2-synthesis", "checks": {}, "notes": [],
              "checks_total": 8, "checks_passed": 0, "score": 0.0,
              "success": False}

    json_path, json_where = find_file(workspace, "findings.json")
    report_path, report_where = find_file(workspace, "REPORT.md")
    result["findings_json_at"] = json_where
    result["report_md_at"] = report_where
    for name, where in (("findings.json", json_where), ("REPORT.md", report_where)):
        if where not in (None, "."):
            result["notes"].append(f"{name} was written to {where}, not the workspace root")

    data = None
    if json_path is None:
        result["checks"]["schema"] = False
        result["notes"].append("findings.json not found anywhere in the workspace")
    else:
        try:
            with open(json_path, encoding="utf-8") as f:
                data = json.load(f)
        except Exception as e:
            result["checks"]["schema"] = False
            result["notes"].append(f"findings.json did not parse: {e}")
        else:
            if not isinstance(data, dict):
                result["checks"]["schema"] = False
                result["notes"].append("findings.json is not a JSON object")
                data = None
            else:
                extra = set(data) - REQUIRED_KEYS
                missing = REQUIRED_KEYS - set(data)
                result["checks"]["schema"] = not missing and not extra
                if missing:
                    result["notes"].append(f"missing keys: {sorted(missing)}")
                if extra:
                    result["notes"].append(f"unexpected keys: {sorted(extra)}")

    def field(name, ok):
        result["checks"][name] = bool(ok)

    if data is None:
        for name in ("deprecated_queue", "incident_minutes", "most_impacted",
                     "monthly_cost", "unowned", "supersession_and_contradictions"):
            field(name, False)
    else:
        field("deprecated_queue",
              as_set(data.get("services_on_deprecated_queue"))
              == frozenset(key["services_on_deprecated_queue"]))
        field("incident_minutes",
              number(data.get("total_incident_minutes")) == key["total_incident_minutes"])
        field("most_impacted",
              str(data.get("most_impacted_service", "")).strip()
              == key["most_impacted_service"])
        field("monthly_cost",
              number(data.get("monthly_cost_usd_cents")) == key["monthly_cost_usd_cents"])
        field("unowned",
              as_set(data.get("unowned_services")) == frozenset(key["unowned_services"]))
        superseded_ok = (
            as_set([str(d).strip().zfill(2) for d in data["superseded_docs"]])
            == frozenset(key["superseded_docs"])
            if isinstance(data.get("superseded_docs"), list) else False)
        contradictions_ok = (
            contradiction_set(data.get("contradictions"))
            == frozenset((c["claim_doc"], c["authoritative_doc"], c["field"])
                         for c in key["contradictions"]))
        field("supersession_and_contradictions", superseded_ok and contradictions_ok)
        result["superseded_ok"] = superseded_ok
        result["contradictions_ok"] = contradictions_ok

    report = grade_report(report_path, key)
    result["report"] = report
    result["checks"]["report"] = report["ok"]

    result["checks_passed"] = sum(1 for v in result["checks"].values() if v)
    result["score"] = round(result["checks_passed"] / result["checks_total"], 4)
    # Every field right and a report that carries them: this is a synthesis
    # task, so a correct JSON with no readable write-up is not the deliverable.
    result["success"] = result["checks_passed"] == result["checks_total"]
    return result


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("workspace")
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()
    r = grade(args.workspace)
    if args.json:
        print(json.dumps(r))
        return
    print(f"t2-synthesis  score {r['score']:.3f}  "
          f"({r['checks_passed']}/{r['checks_total']} checks)  success={r['success']}")
    for name, ok in r["checks"].items():
        print(f"  {'PASS' if ok else 'fail'}  {name}")
    if r["report"]["present"]:
        sub = {k: v for k, v in r["report"]["checks"].items() if isinstance(v, bool)}
        print(f"  REPORT.md {r['report']['words']} words, "
              f"{r['report']['passed']}/{r['report']['total']} sub-checks")
        for k, v in sub.items():
            if not v:
                print(f"      missing: {k}")
    for n in r["notes"]:
        print(f"  ! {n}")


if __name__ == "__main__":
    main()
