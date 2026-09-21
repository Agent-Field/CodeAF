#!/usr/bin/env bash
# wait.sh - idle CPU of a chat surface that WAITS ON WORK IN FLIGHT, by K.
# Usage: wait.sh K:MODE [K:MODE ...]   e.g. wait.sh 5:nodelta 5:stream 200:nodelta
# One row per point: K, mode, threads, idle %core (two 30s windows), RSS before
# and after the windows, stub POST counts (requests in flight during sampling),
# first-delta count (0 means no output arrived), pane state word.
set -u
REPO="/home/santosh/src/c286tree"
BIN="$REPO/bin/codeaf"
TAR="${TAR:-/home/santosh/src/profiles/fresh-codeaf.tar}"
STUB="$REPO/bench/idle-surface/stub-tunable.py"
BASE="${BASE:-/home/santosh/src/c286tree/bench/idle-surface/runs}"
PORT="${PORT:-8099}"
WIN="${WIN:-30}"
CLK_TCK=$(getconf CLK_TCK)
RESULTS="$BASE/wait-$(date +%Y%m%d-%H%M%S).tsv"
mkdir -p "$BASE"

printf 'K\tmode\tthreads\tidle_pct_w1\tidle_pct_w2\trss_mb_w1\trss_mb_w2\tposts_before\tposts_after\tfirst_deltas\tstate\n' > "$RESULTS"
echo "results: $RESULTS"
for pt in "$@"; do
  K="${pt%%:*}"; MODE="${pt##*:}"
  RUN="$BASE/w-$K-$MODE"
  rm -rf "$RUN"; mkdir -p "$RUN/prof" "$RUN/out"
  tar -xf "$TAR" -C "$RUN/prof"
  unshare -rn --mount bash "$REPO/bench/idle-surface/ns-wait.sh" \
    "$RUN" "$BIN" "$STUB" "$PORT" "$K" "$MODE" "$WIN" "$RESULTS" "$CLK_TCK" \
    > "$RUN/out/outer.log" 2>&1 || { printf 'K=%s %s FAILED:\n' "$K" "$MODE"; tail -20 "$RUN/out/outer.log"; }
done
echo '== table =='
column -t -s "$(printf '\t')" "$RESULTS"
