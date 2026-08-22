#!/usr/bin/env bash
# Four bench cells in parallel: one run.sh per issue, own RESULTS dir each.
set -u
ROOT="$1"; shift
mkdir -p "$ROOT"
for i in 20 21 22 23; do
  (
    AFORGE_MODE=select \
    AFORGE_BIN="${AFORGE_BIN:-$(pwd)/bin/aforge}" \
    HARNESSES=aforge \
    ISSUES="$i" \
    BASE_COMMIT=6c978ffa1c49ba600c85eb893958409e37dbedd2 \
    RESULTS="$ROOT/issue-$i" \
    bash "$(dirname "${BASH_SOURCE[0]}")/../run.sh" > "$ROOT/cell-$i.log" 2>&1
    echo "cell $i exit=$?" >> "$ROOT/DONE"
  ) &
done
wait
echo "ALL CELLS FINISHED" >> "$ROOT/DONE"
