#!/usr/bin/env python3
"""The blind judge: does any finding a tool reported on a revision name the
CVE that revision is in the benchmark for?

    judge.py <results-dir> [--model M] [--jobs N] [--rejudge]

For every cell under the results directory that has findings.json and
truth.json, write judge.json. A cell with no findings is decided without a
call — "No issues were detected by the tool." — exactly as DeepSource's rows
record it. Everything else goes to the model named by --model through
OpenRouter with the SAME three criteria DeepSource's judge applied: a hit
describes the same security impact as the CVE, the same attack pattern, and
the exact vulnerability instance. The judge is told neither which tool wrote
the findings nor which variant it is looking at; the variant is applied after
the verdict, by this script:

    unfixed + hit -> TP      unfixed + no hit -> FN
    fixed   + hit -> FP      fixed   + no hit -> TN

So "precision" on this benchmark measures one thing only: whether a tool stops
reporting the CVE once it is fixed. A tool that reports twenty unrelated
things on every revision pays nothing here. README.md says so again.

The key comes from OPENROUTER_API_KEY (run.sh resolves it through lib.sh)."""
import argparse
import hashlib
import json
import os
import sys
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor

ENDPOINT = "https://openrouter.ai/api/v1/chat/completions"
NO_FINDINGS = "No issues were detected by the tool."

SYSTEM = """You are the blind judge for a software-security benchmark.

You will be shown a CVE — its public description, the weakness type, and the file and line where the benchmark records the weakness in the VULNERABLE revision — and then the findings a code-analysis tool reported on ONE revision of that repository. You are not told which tool produced the findings, and you are not told whether this revision is the vulnerable one or the one that fixed it. Do not guess either; judge only what is in front of you.

Decide whether ANY single finding is a hit. A finding is a hit only when all three hold:
1. It describes the same security impact as the CVE.
2. It involves the same attack pattern.
3. It identifies the exact vulnerability instance the CVE describes — the same code path, not merely the same file, the same function family, or the same vulnerability category.

The recorded file and line are a pointer to that instance, not a test. Line numbers move between revisions and a tool may report the source, the sink, or another step of the same path; a finding on the same code path in that file at another line is the same instance. A finding on a different code path, or on a different weakness in the same file, is not.

These are NOT hits:
- A finding that says a fix is inadequate, bypassable, incomplete, or could be hardened. That describes the quality of a fix, not the original vulnerability.
- A different weakness in the same file, or the same kind of weakness somewhere else in the code.
- A generic statement of the vulnerability class with no location that matches.
- A weakness the CVE's description does not cover, even if real.

Reply with a single JSON object and nothing else:
{"hit": true or false, "finding_index": <0-based index of the hit, or null>, "reasoning": "<under 100 words>"}"""


def user_message(truth, findings):
    weak = "\n".join(
        "- %s:%s — %s" % (w["file"], w.get("line") if w.get("line") is not None else "?", w.get("explanation") or "")
        for w in truth.get("weaknesses", [])
    ) or "- (no location recorded)"
    prose = truth.get("osv_details") or "(no public description available)"
    shown = [{"index": i, "file": f.get("file"), "line": f.get("line"),
              "explanation": (" — ".join(x for x in (f.get("title"), f.get("description")) if x)).strip()}
             for i, f in enumerate(findings)]
    return (
        "CVE: %s\nCWE(s): %s\n\nPublic description:\n%s\n\n"
        "Weakness location(s) recorded by the benchmark, in the vulnerable revision (line numbers may differ in other revisions):\n%s\n\n"
        "Findings the tool reported on the revision under judgment (JSON):\n%s\n\n"
        "Is any one of these findings a hit for this CVE, by the three criteria?"
        % (truth["cve"], ", ".join(truth.get("cwes", [])) or "unknown", prose, weak,
           json.dumps(shown, indent=1, ensure_ascii=False))
    )


def call(model, system, user, key):
    body = json.dumps({
        "model": model,
        "temperature": 0,
        "messages": [{"role": "system", "content": system}, {"role": "user", "content": user}],
        "usage": {"include": True},
    }).encode()
    req = urllib.request.Request(ENDPOINT, data=body, headers={
        "Authorization": "Bearer " + key,
        "Content-Type": "application/json",
        "HTTP-Referer": "https://github.com/Agent-Field/codeaf",
        "X-Title": "codeaf openssf-cve rig judge",
    })
    delay = 3
    for attempt in range(6):
        try:
            with urllib.request.urlopen(req, timeout=180) as r:
                return json.load(r)
        except urllib.error.HTTPError as e:
            text = e.read().decode(errors="replace")[:300]
            if e.code in (429, 500, 502, 503, 504) and attempt < 5:
                time.sleep(delay); delay *= 2; continue
            raise RuntimeError("HTTP %d: %s" % (e.code, text))
        except (urllib.error.URLError, TimeoutError, OSError) as e:
            if attempt < 5:
                time.sleep(delay); delay *= 2; continue
            raise
    raise RuntimeError("unreachable")


def parse_verdict(text):
    text = text.strip()
    if text.startswith("```"):
        text = text.strip("`")
        if text.startswith("json"):
            text = text[4:]
    start, end = text.find("{"), text.rfind("}")
    if start < 0 or end < 0:
        raise ValueError("no JSON object in reply: %r" % text[:200])
    v = json.loads(text[start:end + 1])
    if not isinstance(v.get("hit"), bool):
        raise ValueError("hit is not a boolean: %r" % v)
    return v


def classify(variant, hit):
    if variant == "unfixed":
        return {"TP": int(hit), "FN": int(not hit), "FP": 0, "TN": 0}
    return {"FP": int(hit), "TN": int(not hit), "TP": 0, "FN": 0}


def judge_cell(cell, model, key, rejudge):
    out = os.path.join(cell, "judge.json")
    if os.path.exists(out) and not rejudge:
        return "cached"
    truth = json.load(open(os.path.join(cell, "truth.json")))
    findings = json.load(open(os.path.join(cell, "findings.json")))["findings"]
    rec = {"cve": truth["cve"], "variant": truth["variant"], "finding_count": len(findings),
           "judge_model": model if findings else None}
    if not findings:
        rec.update({"hit": False, "finding_index": None, "reasoning": NO_FINDINGS, "called": False})
    else:
        user = user_message(truth, findings)
        rec["prompt_sha256"] = hashlib.sha256((SYSTEM + "\n" + user).encode()).hexdigest()[:16]
        try:
            resp = call(model, SYSTEM, user, key)
            text = resp["choices"][0]["message"]["content"]
            v = parse_verdict(text)
            rec.update({"hit": v["hit"], "finding_index": v.get("finding_index"),
                        "reasoning": str(v.get("reasoning", "")).strip(), "called": True,
                        "usage": resp.get("usage"), "raw": text if len(text) < 4000 else text[:4000]})
        except Exception as e:
            # An unjudged cell stays unjudged: no verdict is written, score.py
            # reports it as such, and a re-run of judge.py picks it up.
            json.dump({"cve": truth["cve"], "variant": truth["variant"], "error": str(e)},
                      open(os.path.join(cell, "judge.error.json"), "w"), indent=2)
            return "error: %s" % e
    rec.update(classify(truth["variant"], rec["hit"]))
    json.dump(rec, open(out, "w"), indent=2)
    if os.path.exists(os.path.join(cell, "judge.error.json")):
        os.remove(os.path.join(cell, "judge.error.json"))
    return "TP" if rec["TP"] else "FP" if rec["FP"] else "TN" if rec["TN"] else "FN"


def cells_under(root):
    for cve in sorted(os.listdir(root)):
        d = os.path.join(root, cve)
        if not os.path.isdir(d) or cve == "rig":
            continue
        for variant in ("unfixed", "fixed"):
            cell = os.path.join(d, variant)
            if os.path.exists(os.path.join(cell, "findings.json")) and os.path.exists(os.path.join(cell, "truth.json")):
                yield cell


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("results")
    ap.add_argument("--model", default=os.environ.get("JUDGE_MODEL", "anthropic/claude-opus-4.5"))
    ap.add_argument("--jobs", type=int, default=2)
    ap.add_argument("--rejudge", action="store_true")
    ap.add_argument("--offline", action="store_true", help="decide only the cells that need no model call")
    a = ap.parse_args()
    key = os.environ.get("OPENROUTER_API_KEY", "")
    cells = list(cells_under(a.results))
    pending = [c for c in cells if a.rejudge or not os.path.exists(os.path.join(c, "judge.json"))]
    if a.offline:
        pending = [c for c in pending if not json.load(open(os.path.join(c, "findings.json")))["findings"]]
    needs_call = [c for c in pending if json.load(open(os.path.join(c, "findings.json")))["findings"]]
    if needs_call and not key:
        print("judge.py: %d cells need a model call and OPENROUTER_API_KEY is empty" % len(needs_call), file=sys.stderr)
        sys.exit(2)
    print("judge.py: %d cells, %d to judge, %d need a call to %s" % (len(cells), len(pending), len(needs_call), a.model), file=sys.stderr)
    errors = 0
    with ThreadPoolExecutor(max_workers=max(1, a.jobs)) as ex:
        for cell, res in zip(pending, ex.map(lambda c: judge_cell(c, a.model, key, a.rejudge), pending)):
            rel = os.path.relpath(cell, a.results)
            print("  %-32s %s" % (rel, res), file=sys.stderr)
            if res.startswith("error"):
                errors += 1
    sys.exit(1 if errors else 0)


if __name__ == "__main__":
    main()
