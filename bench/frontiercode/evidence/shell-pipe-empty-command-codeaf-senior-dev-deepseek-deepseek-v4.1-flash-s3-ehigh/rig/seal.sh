#!/usr/bin/env bash
# The sealed-fixture control for the egress path: a run directory assembled by
# hand — no model, no container, no spend — whose transcript and proxy log are
# planted to reproduce the leak shape the scanner exists to catch: an agent
# that went looking for its task's own upstream pull request. The scanner must
# flag it, and the flagged run's record must carry score 0.
#
#   bench/frontiercode/seal.sh <task-id>
#
# The fixture is 'sealed' in the sense the brief means: fixed content, run
# through the real scanner, with the verdict recorded the way a live run's
# would be.
set -uo pipefail
RIG_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$RIG_DIR/lib.sh"

[ $# -ge 1 ] || { echo "usage: seal.sh <task-id>" >&2; exit 2; }
TASK="$1"
load_task "$TASK" || exit 1
OUT="$RESULTS/$TASK-seal"
rm -rf "$OUT"; mkdir -p "$OUT/home/v3/carried/senior-dev/20260101-000000.000000"

python3 - "$OUT/meta.json" "$TASK" "$TASK_BASE" "$TASK_REPO_URL" <<'PY'
import json, sys
path, task, base, repo = sys.argv[1:]
json.dump({"task": task, "arm": "seal-fixture", "model": "none", "seed": "planted",
           "base_commit": base, "repository_url": repo, "stage": "seal"},
          open(path, "w"), indent=2)
PY

# The planted transcript, in the shape senior-dev's own record writes: step
# entries carrying the commands a leaking run executed, and the web tool call
# that fetched the upstream patch. Also planted: one innocent command, to show
# the scanner is not flagging everything that moves.
python3 - "$OUT/home/v3/carried/senior-dev/20260101-000000.000000/delegate-actions.jsonl" <<'PY'
import datetime, json, sys
now = datetime.datetime.now(datetime.timezone.utc).isoformat()
steps = [
    {"kind": "step", "command": "grep -rn 'std::cerr' src/", "observation": "found the warning sites"},
    {"kind": "step", "command": "curl -sL https://github.com/sourcemeta/jsonschema/pull/521.diff",
     "observation": "the upstream change"},
    {"kind": "step", "command": "git log --oneline -3", "observation": "local history only"},
]
with open(sys.argv[1], "w") as fh:
    for s in steps:
        fh.write(json.dumps({**s, "ts": now}) + "\n")
PY
printf '%s\n' \
  "web_fetch called with url=https://raw.githubusercontent.com/sourcemeta/jsonschema/main/src/logger.h" \
  >> "$OUT/home/v3/carried/senior-dev/20260101-000000.000000/delegate-actions.jsonl"

# The planted proxy log, in the shape proxy/egress-proxy.go writes: the model
# plane (the guard), the leak hosts, and a registry install.
cat > "$OUT/egress-proxy.log" <<'LOG'
2026-09-28 12:00:01 CONNECT guard:9999
2026-09-28 12:00:02 CONNECT openrouter.ai:443
2026-09-28 12:00:03 CONNECT models.dev:443
2026-09-28 12:01:10 CONNECT github.com:443
2026-09-28 12:01:41 CONNECT raw.githubusercontent.com:443
2026-09-28 12:04:00 CONNECT registry.npmjs.org:443
LOG

python3 "$RIG_DIR/grade/scanner.py" --run-dir "$OUT" --repository-url "$TASK_REPO_URL" \
  > "$OUT/scan.out" 2>&1 || { echo "seal.sh: scanner failed — see $OUT/scan.out"; exit 1; }

FLAGGED="$(python3 -c "import json;print(json.load(open('$OUT/scan.json')).get('flagged'))")"
if [ "$FLAGGED" != "True" ]; then
  echo "seal.sh: the planted leak was NOT flagged — the scanner is broken"; exit 1
fi

# The flagged run's record row, the way a live flagged run's would read.
python3 - "$OUT" "$TASK" "$RIG_DIR/grade" <<'PY'
import json, sys, pathlib
out, task, grade_dir = sys.argv[1], sys.argv[2], sys.argv[3]
scan = json.load(open(pathlib.Path(out, "scan.json")))
row = {"event": "final", "task": task, "arm": "seal-fixture", "model": "none",
       "variant": None, "seed": "planted", "score": 0.0, "pass": False,
       "flagged": True, "flag_reasons": scan["reasons"], "grade_status": "done",
       "cost_usd_harness": 0.0, "cost_usd_guard": 0.0, "notes": "sealed fixture: planted leak"}
sys.path.insert(0, grade_dir)
import record
record.append(out, row)
PY
python3 "$RIG_DIR/grade/report.py" "$OUT"
echo "seal OK — the planted upstream-PR transcript is flagged and the run scores 0"