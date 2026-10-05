#!/usr/bin/env bash
# The oracle. On the vulnerable revision it reports exactly the weakness the
# dataset records; on the fixed revision it reports nothing. It is the only
# driver allowed to read the answer (BENCH_INFO), and it exists for one reason:
# a run of it must score F1 100 through the real judge, or the judge and the
# scorer cannot be trusted with a real tool's zero. Costs one judge call per
# vulnerable row and nothing else.
set -uo pipefail
[ -n "${BENCH_INFO:-}" ] || { echo "gold.sh: BENCH_INFO not set (cell.sh exports it for TOOL=gold only)" >&2; exit 2; }
if [ "$BENCH_VARIANT" = unfixed ]; then
  jq '{findings: [.weaknesses[] | {file: .file, line: .line, title: .explanation, description: ("The code at this location carries the weakness the benchmark records: " + .explanation + "."), cwe: null, severity: "high"}]}' "$BENCH_INFO" > "$BENCH_OUT/findings.json"
else
  printf '{"findings": []}\n' > "$BENCH_OUT/findings.json"
fi
