#!/usr/bin/env bash
# The product, through its headless door: `codeaf do`, one brief, no person
# watching, pinned to one model for every seat. The brief asks for a security
# review of the pull request (diff mode) or an audit of the repository (repo
# mode) and for ONE file at the workspace root, .bench-findings.json, in the
# rig's finding shape. That file is the report; if the run never writes it, the
# envelope's answer field is searched for the same JSON before the cell is
# recorded as a tool error.
#
# WHEN `/security-review` LANDS, THIS IS THE LINE TO CHANGE: the brief below
# becomes that command's invocation and the schema stays. Nothing else in the
# rig knows how the findings were produced.
#
# Needs: CODEAF_BIN (run.sh sets it), OPENROUTER_API_KEY (codeaf reads the
# shell's variable before any profile, so the key is never written to disk),
# BENCH_* from cell.sh. Writes do.json, run.log, brief.md and cost.json beside
# the findings; --keep leaves the run's own store under home/, the isolated
# state root. (The run engine refuses --db, which only the older engine took.)
set -uo pipefail
BIN="${CODEAF_BIN:?set CODEAF_BIN}"
HOME_DIR="$BENCH_OUT/home"; mkdir -p "$HOME_DIR"; chmod 700 "$HOME_DIR"

# An isolated profile, so the cell can never read or write ~/.codeaf, with every
# model seat pinned to the one model: the measurement is one model's behaviour,
# and an unpinned seat silently escalates. The rows are the DeepSWE rig's.
python3 - "$HOME_DIR/config.json" "$BENCH_MODEL" <<'PY'
import datetime, json, sys
path, model = sys.argv[1], sys.argv[2]
roles = ["auditor", "careful", "compaction", "consolidate", "designer", "division",
         "guardian", "handoff", "imagegen", "intake", "markreader", "planner",
         "reflex", "router", "routerconfirm", "shaper", "speech", "taskname",
         "title", "video", "vision", "worker"]
out = {
    "model.talk": model,
    "tools.approvalMode": "allow",
    "daily_budget_usd": 20,
    "setup_seen_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "models.roles": "\n".join("%s:%s" % (r, model) for r in roles),
    "models.fallbacks": model,
    "task.model": model,
    "vision_model": model,
}
for tier in ("low", "high", "mastermind", "reflex", "worker"):
    out["models.tiers." + tier] = model
json.dump(out, open(path, "w"), indent=2)
PY

SCHEMA='{"findings": [{"file": "path/relative/to/the/repository/root", "line": 123, "title": "short title", "description": "what the vulnerability is, how attacker-controlled input reaches the dangerous sink, and the impact", "cwe": "CWE-79", "severity": "high"}]}'

if [ "$BENCH_MODE" = diff ]; then
  FILES="$(sed 's/^/  - /' "$BENCH_FILES")"
  cat > "$BENCH_OUT/brief.md" <<EOF
Security review of a pull request.

This repository has two branches. \`main\` is the base. \`review\` is the pull request under review, and it is checked out. Run \`git diff main review --stat\` and \`git diff main review\` to see exactly what the pull request adds. It adds these files in full:
$FILES

Review the code this pull request adds for security vulnerabilities an attacker could actually exploit: injection of any kind, command execution, path traversal, prototype pollution, unsafe deserialization or eval, cross-site scripting, regular-expression denial of service, server-side request forgery, insecure network or cryptographic settings, authentication and authorization flaws. Read the surrounding code as much as you need to; trace attacker-controlled input to the dangerous sink before reporting. Report only findings you are confident are real. Do not report style, hygiene, or theoretical concerns. If the added code is safe, report no findings.

When you are done, write ONE file, \`.bench-findings.json\`, at the repository root, and change nothing else in the repository. Its exact shape:
$SCHEMA
An empty list is a valid and complete answer: {"findings": []}
EOF
else
  cat > "$BENCH_OUT/brief.md" <<EOF
Security audit of this repository.

Audit the code in this repository for security vulnerabilities an attacker could actually exploit: injection of any kind, command execution, path traversal, prototype pollution, unsafe deserialization or eval, cross-site scripting, regular-expression denial of service, server-side request forgery, insecure network or cryptographic settings, authentication and authorization flaws. Trace attacker-controlled input to the dangerous sink before reporting. Report only findings you are confident are real. Do not report style, hygiene, or theoretical concerns.

When you are done, write ONE file, \`.bench-findings.json\`, at the repository root, and change nothing else in the repository. Its exact shape:
$SCHEMA
An empty list is a valid and complete answer: {"findings": []}
EOF
fi

cd "$BENCH_WORKSPACE" || exit 2
CODEAF_HOME="$HOME_DIR" HOME="$HOME_DIR" \
"$BIN" do --dir "$BENCH_WORKSPACE" \
  --model "$BENCH_MODEL" --plan-model "$BENCH_MODEL" --check-model "$BENCH_MODEL" \
  --json --yes-spend --timeout "${BENCH_TIMEOUT}s" --keep \
  < "$BENCH_OUT/brief.md" > "$BENCH_OUT/do.json" 2> "$BENCH_OUT/run.log"
CODE=$?

python3 - "$BENCH_WORKSPACE/.bench-findings.json" "$BENCH_OUT/do.json" "$BENCH_OUT/findings.json" "$BENCH_OUT/cost.json" "$CODE" <<'PY'
import json, os, re, sys
written, envelope, out, costpath, code = sys.argv[1:6]
found = None
if os.path.exists(written):
    try:
        found = json.load(open(written))
    except Exception:
        found = None
env = {}
try:
    env = json.load(open(envelope))
except Exception:
    pass
if found is None:
    # The run answered in prose instead of writing the file: take the last
    # JSON object in the answer that has a findings list.
    answer = env.get("answer") or ""
    for m in re.finditer(r"\{[\s\S]*?\"findings\"[\s\S]*\}", answer):
        try:
            cand = json.loads(m.group(0))
            if isinstance(cand.get("findings"), list):
                found = cand
        except Exception:
            continue
if found is not None:
    json.dump(found, open(out, "w"), indent=2)
spend = env.get("spend_usd", env.get("spend"))
json.dump({"cost_usd": spend, "source": "self-reported" if spend is not None else "unknown",
           "stop": env.get("stop"), "ok": env.get("ok"), "exit_code": int(code)},
          open(costpath, "w"), indent=2)
PY
exit "$CODE"
