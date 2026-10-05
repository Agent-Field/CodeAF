#!/usr/bin/env bash
# sec-af, through the AgentField control plane: `af call sec-af.audit` on the
# workspace path, which the node accepts as repo_url when it is a directory.
# sec-af audits a whole checkout and has no notion of a pull request, so run it
# with MODE=repo; in diff mode it would simply see the same files on `review`.
#
# Only findings whose verdict is in SECAF_VERDICTS (default confirmed,likely)
# are reported, because that is what sec-af itself puts in front of a person;
# `inconclusive` and `not_exploitable` are its own way of staying quiet.
#
# NOT YET EXERCISED ON THIS LAPTOP: the `af` CLI and a running sec-af node are
# needed, and neither was installed when the rig was written (2026-10-05). The
# mapping below follows exampl/dvga-benchmark-result.json in the sec-af
# repository; the first real run is the test.
set -uo pipefail
AF="${AF_BIN:-af}"
command -v "$AF" >/dev/null 2>&1 || { echo "sec-af.sh: af CLI not found (set AF_BIN)" >&2; exit 2; }
VERDICTS="${SECAF_VERDICTS:-confirmed,likely}"

INPUT="$(jq -cn --arg p "$BENCH_WORKSPACE" '{repo_url: $p}')"
"$AF" call sec-af.audit --in "$INPUT" > "$BENCH_OUT/sec-af.out" 2> "$BENCH_OUT/sec-af.log"
CODE=$?

# af streams progress before the result; the result is the last JSON object
# on stdout that carries a findings list.
python3 - "$BENCH_OUT/sec-af.out" "$BENCH_OUT/result.json" <<'PY'
import json, sys
raw = open(sys.argv[1], errors="replace").read()
result = None
depth = 0; start = None
for i, ch in enumerate(raw):
    if ch == "{":
        if depth == 0: start = i
        depth += 1
    elif ch == "}" and depth:
        depth -= 1
        if depth == 0 and start is not None:
            try:
                cand = json.loads(raw[start:i + 1])
                if isinstance(cand, dict) and isinstance(cand.get("findings"), list):
                    result = cand
            except Exception:
                pass
if result is not None:
    json.dump(result, open(sys.argv[2], "w"), indent=2)
PY

if [ -s "$BENCH_OUT/result.json" ]; then
  jq --arg v "$VERDICTS" '
    ($v | split(",")) as $keep
    | {findings: [ .findings[] | select(.verdict as $x | $keep | index($x))
        | {file: .location.file_path, line: .location.start_line, title: .title,
           description: (.rationale // .description // ""), cwe: .cwe_id, severity: .severity} ]}' \
    "$BENCH_OUT/result.json" > "$BENCH_OUT/findings.json"
  jq '{cost_usd: .cost_usd, source: "self-reported", duration_seconds: .duration_seconds}' "$BENCH_OUT/result.json" > "$BENCH_OUT/cost.json"
fi
exit "$CODE"
