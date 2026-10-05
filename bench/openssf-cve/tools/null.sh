#!/usr/bin/env bash
# The floor: a tool that reports nothing. Recall 0, precision undefined,
# accuracy equal to the share of fixed rows (83 of 165 on the DeepSource set,
# which is 50.30). A run of it costs no model calls at all and is the number a
# real tool has to be read against — "stays quiet" is worth half the accuracy
# column by itself.
set -uo pipefail
printf '{"findings": []}\n' > "$BENCH_OUT/findings.json"
