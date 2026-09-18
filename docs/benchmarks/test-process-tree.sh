#!/usr/bin/env bash
set -eu
if [ "${1:-}" = --version ]; then
  printf 'fixture 1\n'
  exit 0
fi
if [ "${1:-}" = run ]; then
  printf 'ready helper=%s\n' "${LATE_HELPER:-0}"
  if [ "${LATE_HELPER:-0}" = 1 ]; then
    sleep 1
    sleep 30 &
    wait
  else
    exec sleep 30
  fi
  exit 0
fi
script_dir="$(cd "${BASH_SOURCE[0]%/*}" && pwd)"
self="$script_dir/${BASH_SOURCE[0]##*/}"
root="$(mktemp -d)"
cleanup() { rm -rf "$root"; }
trap cleanup EXIT
mkdir -p "$root/work/.git" "$root/home"
run_case() {
  label="$1"
  helper="$2"
  printf 'command: LATE_HELPER=%s IDLE_SECONDS=2 SETTLE_SECONDS=2 STARTUP_RUNS=1 STARTUP_WARMUP=0 WORKDIR=%s %s %s %s %s run\n' \
    "$helper" "$root/work" "$script_dir/measure-cli.sh" "$label" "$root/home" "$self"
  BENCH_ENV=LATE_HELPER LATE_HELPER="$helper" IDLE_SECONDS=2 SETTLE_SECONDS=2 STARTUP_RUNS=1 STARTUP_WARMUP=0 \
    WORKDIR="$root/work" "$script_dir/measure-cli.sh" "$label" "$root/home" "$self" run |
    awk '/phase=proc / || /phase=summary /'
}
run_case helper-one-second 1
run_case no-helper 0
