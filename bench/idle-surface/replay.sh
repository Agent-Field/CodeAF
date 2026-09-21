#!/usr/bin/env bash
# replay.sh - idle CPU and resident memory of a waiting chat surface as a
# function of K replayed turns. One row per K.
#
# Per point: the pinned profile tar is restored FRESH to a scratch dir; inside
# `unshare -rn --mount` (loopback-only net, own tmpfs /tmp) the stub serves the
# canned stream and `bin/codeaf chat --yolo --no-host --max-cost 5` is driven
# through K turns in a real terminal (tmux, private socket). After the K-th
# turn completes and the surface shows idle with nothing in flight, two
# consecutive 30s windows are sampled read-only from /proc: utime+stime over all
# threads (clock ticks) and VmRSS for the surface and any engine-daemon child.
#
# Hard rules honoured here: no busy loops, no load generators, nothing runs
# except the chat process, the stub and tmux. Kills are by pid of processes
# this run started, after reading the proc cmdline of each; never pkill/killall.
#
# Usage: replay.sh K [K ...]        e.g. replay.sh 5 50 200
# Env:   BASE (scratch root, default /tmp/codeaf-idle-runs), WIN (window secs).
set -u
REPO="/home/santosh/src/c286tree"
BIN="$REPO/bin/codeaf"
TAR="${TAR:-/home/santosh/src/profiles/fresh-codeaf.tar}"
STUB="${STUB:-/home/santosh/src/stub.py}"
BASE="${BASE:-/home/santosh/src/c286tree/bench/idle-surface/runs}"
PORT="${PORT:-8099}"
WIN="${WIN:-30}"
CLK_TCK=$(getconf CLK_TCK)

RESULTS="$BASE/results-$(date +%Y%m%d-%H%M%S).tsv"
mkdir -p "$BASE"

one_point() {
  local K="$1"
  local RUN; RUN="$BASE/k$K"
  rm -rf "$RUN"; mkdir -p "$RUN/prof" "$RUN/out"
  tar -xf "$TAR" -C "$RUN/prof"   # FRESH restore for every point

  unshare -rn --mount bash "$REPO/bench/idle-surface/ns.sh" \
    "$RUN" "$BIN" "$STUB" "$PORT" "$K" "$WIN" "$RESULTS" "$CLK_TCK" \
    > "$RUN/out/outer.log" 2>&1
  local rc=$?
  if [ $rc -ne 0 ]; then
    printf 'K=%s FAILED rc=%s - tail of %s/out/outer.log:\n' "$K" "$rc" "$RUN"
    tail -20 "$RUN/out/outer.log"
    return 1
  fi
}

printf 'K\tthreads\tidle_pct_core_w1\tidle_pct_core_w2\trss_surface_mb\trss_daemon_mb\tturns_done\ttranscript_kb\twaiting_state\n' > "$RESULTS"
echo "results: $RESULTS"
for K in "$@"; do one_point "$K" || true; done
echo '== table =='
column -t -s "$(printf '\t')" "$RESULTS"
