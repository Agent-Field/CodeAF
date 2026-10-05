#!/usr/bin/env bash
# Replay: not a tool, a recording of one. Hands our judge the findings
# DeepSource's judge was shown for one of their tools (their processed rows),
# so calibrate.py can measure judge against judge with nothing else varying.
#
#   REPLAY_TOOL=<claude-code|codex|devin|greptile|semgrep|deepsource|gitlab-duo|coderabbit> \
#     run.sh replay - <seed>
#
# Cursor Bugbot has judged rows but no processed rows in their repository, so
# it cannot be replayed. A row they have no record for is an empty report and
# is noted in replay.note so calibrate.py can leave it out.
set -uo pipefail
TOOL="${REPLAY_TOOL:-}"
[ -n "$TOOL" ] || { echo "replay.sh: set REPLAY_TOOL" >&2; exit 2; }
SRC="$WORK/deepsource/processed/$TOOL.jsonl"
[ -s "$SRC" ] || { echo "replay.sh: no processed rows at $SRC — run fetch.sh" >&2; exit 2; }
python3 - "$SRC" "$BENCH_CVE" "$BENCH_VARIANT" "$BENCH_OUT" <<'PY'
import json, os, sys
src, cve, variant, out = sys.argv[1:5]
rows = [json.loads(l) for l in open(src) if l.strip()]
rows = [r for r in rows if r.get("cve_id") == cve and r.get("variant") == variant]
findings = []
if rows:
    for issue in rows[0].get("detected_issues") or []:
        pos = issue.get("position") or {}
        line = ((pos.get("begin") or {}).get("line"))
        findings.append({"file": issue.get("file"), "line": line, "title": "",
                         "description": issue.get("explanation", ""), "cwe": None, "severity": None})
else:
    open(os.path.join(out, "replay.note"), "w").write("no processed row for %s/%s\n" % (cve, variant))
json.dump({"findings": findings}, open(os.path.join(out, "findings.json"), "w"), indent=2)
PY
