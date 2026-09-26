#!/usr/bin/env bash
# measure-cli.sh — measure ONE agent CLI: startup, first interactive frame, idle cost.
#
#   measure-cli.sh <name> <home> <command...>
#
# From the root of a real repository:
#
#   WORKDIR=$PWD docs/benchmarks/performance/measure-cli.sh mine /tmp/bench-home/mine bin/mine chat
#
# <name> only labels the output, the tmux session and the run; nothing about the
# CLI is inferred from it. <home> is the profile the CLI runs under — an
# authenticated one, because an unauthenticated profile measures a login screen
# (METHODOLOGY.md says why). <command...> is the interactive invocation, launched
# in a tmux pane whose working directory is WORKDIR.
#
# Output is key=value lines, grouped by phase=:
#
#   phase=meta      what was run, where, on what — the run's reproducibility.
#                   load1/load5/load15 say what the machine was doing at launch.
#   phase=startup   best-of-N wall clock for `--version`: hyperfine when it is on
#                   PATH, a `date +%s%3N` loop when it is not. tool= says which.
#   phase=frame     milliseconds from launch until the pane first paints a
#                   non-blank character, with any folder-trust gate answered
#                   first, so the frame reported is a session and not a dialog.
#   phase=idle      the whole process tree over the idle window: RSS, PSS, peak
#                   RSS, threads, open fds, CPU from utime+stime deltas, and
#                   voluntary context switches per second.
#   phase=proc      one line per launched-tree process retained alive through the
#                   end of the window, so a detached helper remains in the sum.
#   phase=summary   every headline figure on one line.
#   phase=done      ok=1 with teardown_survivors=0, or ok=0 with reason= set.
#
# A value that contains a space is single-quoted; every other value is bare.
#
# Teardown runs from a trap on EXIT, INT, HUP and TERM and is idempotent: it
# signals the pane's process group and every pid the walk ever saw, then sweeps
# for a helper that detached from the pane, then kills the private tmux server and
# reports what survived. A helper whose command line names this workspace is
# reaped specifically; HOME matching is retained only for isolated profiles.
#
# This script measures. It draws no conclusion, ranks nothing and names no CLI:
# whoever runs it supplies the commands and owns the comparison.
#
# Dependencies: bash 4+, coreutils (date, sort, awk, grep, tr, sed), tmux, and a
# /proc with smaps_rollup (Linux 4.14+). hyperfine is used when present and is not
# required. perf is never used — perf_event_paranoid blocks it on most machines —
# so idle CPU comes from /proc. Optional knobs, all with defaults:
#
#   WORKDIR           working directory the CLI is launched in (default $PWD).
#                     Must be a real repository, not an empty directory.
#   IDLE_SECONDS      length of the idle window (default 30).
#   SAMPLE_MS         idle sampling interval (default 500).
#   SETTLE_SECONDS    pause between the first frame and the idle window, so
#                     startup work is not billed as idle (default 4).
#   STARTUP_RUNS      N of best-of-N startup (default 7).
#   STARTUP_WARMUP    warmup runs discarded before the measured ones (default 2).
#   VERSION_CMD       invocation whose --version prints this CLI's version. The
#                     default is the command's program with --version appended;
#                     set it for a CLI launched through a runtime, where argv[0]
#                     is the runtime and not the CLI.
#   PANE_COLS, PANE_ROWS  pane geometry (default 160x48). A wider pane paints
#                     more, so every CLI in one comparison needs the same value.
#   FRAME_TIMEOUT_MS  stop waiting for a first frame after this (default 60000).
#                     -1 is reported and the run continues.
#   FRAME_POLL_MS     pane polling interval (default 20). first_paint_ms is
#                     quantised to roughly this, and each poll is a tmux client.
#   TRUST             auto (default) | never | always — whether to answer a
#                     folder-trust gate. answer_trust_gate says how narrow auto is.
#   SWEEP             auto (default) | never | always: legacy fallback for
#                     detached processes in an isolated HOME. always is refused
#                     when <home> is the invoking user's HOME.
#   SOCKET            tmux socket name. Always a private one: the run must not
#                     touch the tmux server a person is working in.
#   BENCH_ENV         space-separated names of variables to pass through from the
#                     operator's shell into the run — the credentials a CLI
#                     authenticates with. Everything else is dropped; see
#                     env_prefix for why.
#   PANE_TERM         TERM the pane gets (default xterm-256color). Every CLI in
#                     one comparison needs the same value.
#   BENCH_OUT         directory to also write the captured frame and the
#                     per-process lines into. Unset, nothing is written but stdout.
set -uo pipefail

# ── knobs ────────────────────────────────────────────────────────────────────
WORKDIR="${WORKDIR:-$PWD}"
IDLE_SECONDS="${IDLE_SECONDS:-30}"
SAMPLE_MS="${SAMPLE_MS:-500}"
SETTLE_SECONDS="${SETTLE_SECONDS:-4}"
STARTUP_RUNS="${STARTUP_RUNS:-7}"
STARTUP_WARMUP="${STARTUP_WARMUP:-2}"
PANE_COLS="${PANE_COLS:-160}"
PANE_ROWS="${PANE_ROWS:-48}"
FRAME_TIMEOUT_MS="${FRAME_TIMEOUT_MS:-60000}"
FRAME_POLL_MS="${FRAME_POLL_MS:-20}"
TRUST="${TRUST:-auto}"
SWEEP="${SWEEP:-auto}"
BENCH_OUT="${BENCH_OUT:-}"
BENCH_ENV="${BENCH_ENV:-}"
PANE_TERM="${PANE_TERM:-xterm-256color}"
VERSION_CMD="${VERSION_CMD:-}"
SOCKET="${SOCKET:-}"
CLK_TCK="$(getconf CLK_TCK 2>/dev/null || echo 100)"
[ -n "$CLK_TCK" ] || CLK_TCK=100
PHASE=meta
EXIT_STATUS=0

usage() {
  printf 'usage: %s <name> <home> <command...>\n' "${0##*/}" >&2
  # shellcheck disable=SC2016  # $PWD is shown to a person, not expanded here
  printf 'example: WORKDIR=$PWD %s mine /tmp/bench-home/mine bin/mine chat\n' "${0##*/}" >&2
}

case "${1:-}" in
  (-h | --help)
    usage
    exit 0
    ;;
esac

if [ "$#" -lt 3 ]; then
  usage
  printf 'name=%s phase=done ok=0 reason=usage\n' "${1:-?}"
  exit 2
fi

NAME="$1"
HOMEDIR="$2"
shift 2
CMD=("$@")
[ -n "$VERSION_CMD" ] || VERSION_CMD="${CMD[0]} --version"
# shellcheck disable=SC2206  # an override like "bun cli.js" is meant to split
VERSION_ARGS=($VERSION_CMD)

# tmux session and socket names accept a narrower character set than a CLI name
# might use; the label printed in the output is the one we were given.
SAFE_NAME="$(printf '%s' "$NAME" | tr -c 'A-Za-z0-9_.-' '_')"
SESSION="bench-${SAFE_NAME}-$$"
[ -n "$SOCKET" ] || SOCKET="benchcli-${SAFE_NAME}-$$"
TMUX=(tmux -L "$SOCKET")
INVOKER_HOME="${HOME:-}"
CLI_EXE="$(readlink -f -- "${CMD[0]}" 2>/dev/null || command -v -- "${CMD[0]}" 2>/dev/null || printf '%s' "${CMD[0]}")"
# tmux resolves a target differently by what the command wants. A pane command
# (capture-pane, send-keys) and a window option need the trailing colon: the bare
# exact-match form `=name` is rejected with "can't find pane", and a run that
# cannot read the pane reports a first frame that never arrived for every CLI it
# measures. A session command (list-panes, kill-session) takes the bare form.
PANE_T="=${SESSION}:"
SESS_T="=${SESSION}"

if ! mkdir -p "$HOMEDIR" 2>/dev/null; then
  printf 'name=%s phase=done ok=0 reason=cannot-create-home\n' "$NAME"
  exit 1
fi
[ -z "$BENCH_OUT" ] || mkdir -p "$BENCH_OUT" 2>/dev/null

# ── small helpers ────────────────────────────────────────────────────────────

now_ms() { date +%s%3N; }

# loadavg reads the machine's one-, five- and fifteen-minute load averages with a
# builtin read, no fork. A millisecond figure taken under contention is worthless
# and nothing else in the run says what the box was doing, so every timing phase
# is bracketed by a reading: meta, the startup window, the idle window.
loadavg() {
  read -r LOAD1 LOAD5 LOAD15 _ < /proc/loadavg 2>/dev/null || { LOAD1=-1; LOAD5=-1; LOAD15=-1; }
}

# kv prints one key=value pair on the current phase's line format.
kv() { printf 'name=%s phase=%s %s\n' "$NAME" "$PHASE" "$*"; }

# kvq prints one pair, single-quoting a value that contains a space so the line
# stays parseable.
kvq() {
  case "$2" in
    (*[[:space:]]*) kv "$1=$(shq "$2")" ;;
    (*) kv "$1=$2" ;;
  esac
}

# shq single-quotes one argument for POSIX sh, which is what tmux and hyperfine
# hand a launch string to.
shq() {
  local s=${1//\'/\'\\\'\'}
  printf "'%s'" "$s"
}

# env_prefix builds the environment every phase launches the CLI in, so the
# startup probe, the interactive pane and the teardown sweep all see one profile.
#
# IT STARTS FROM AN EMPTY ENVIRONMENT. The shell of whoever runs this script
# carries variables that redirect a CLI's profile — a <TOOL>_HOME, a profile
# directory, a cached credential — and inheriting them would mean the run measured
# a profile other than the <home> it names, with nothing in the output to say so.
# So the run gets HOME, the XDG directories under it, a locale, a terminal type and
# PATH, plus exactly the variables BENCH_ENV names. BENCH_ENV is how a credential
# the CLI authenticates with reaches the run: name it and it is passed through.
# Locale and identity come along because a CLI that cannot resolve them may draw
# differently or refuse to start, and that is not what is being measured.
PASS_ENV=(LANG LANGUAGE LC_ALL LC_CTYPE USER LOGNAME)
env_prefix() {
  local s name v
  local -a extra=()
  read -r -a extra <<< "$BENCH_ENV"
  s="env -i"
  s="$s HOME=$(shq "$HOMEDIR")"
  s="$s XDG_CONFIG_HOME=$(shq "$HOMEDIR/.config")"
  s="$s XDG_DATA_HOME=$(shq "$HOMEDIR/.local/share")"
  s="$s XDG_STATE_HOME=$(shq "$HOMEDIR/.local/state")"
  s="$s XDG_CACHE_HOME=$(shq "$HOMEDIR/.cache")"
  s="$s TMPDIR=$(shq "${TMPDIR:-/tmp}")"
  s="$s TERM=$(shq "$PANE_TERM")"
  s="$s COLORTERM=truecolor"
  s="$s PATH=$(shq "$PATH")"
  for name in "${PASS_ENV[@]}" "${extra[@]}"; do
    [ -n "$name" ] || continue
    v="${!name:-}"
    [ -n "$v" ] || continue
    s="$s $name=$(shq "$v")"
  done
  printf '%s' "$s"
}

# launch_line is the interactive invocation as one shell line.
launch_line() {
  local s arg
  s="$(env_prefix)"
  for arg in "${CMD[@]}"; do s="$s $(shq "$arg")"; done
  printf '%s' "$s"
}

# version_line is the --version invocation as one shell line.
version_line() {
  local s arg
  s="$(env_prefix)"
  for arg in "${VERSION_ARGS[@]}"; do s="$s $(shq "$arg")"; done
  printf '%s' "$s"
}

# ms_of converts hyperfine's seconds to milliseconds.
ms_of() { awk -v s="${1:-0}" 'BEGIN { printf "%.1f", s * 1000 }'; }

# median_of prints the median of the numbers on stdin.
median_of() {
  sort -n | awk '{ a[NR] = $1 } END {
    if (NR == 0) { print 0 }
    else if (NR % 2) { print a[(NR + 1) / 2] }
    else { printf "%.1f\n", (a[NR / 2] + a[NR / 2 + 1]) / 2 }
  }'
}

# sec_of renders a millisecond knob as a fractional second for sleep, once, so the
# sampling loop is not paying an awk per tick.
sec_of() { awk -v ms="$1" 'BEGIN { printf "%.3f", ms / 1000 }'; }

fail() {
  PHASE="done"
  kv "ok=0 reason=$1"
  exit 1
}

# ── the process tree ─────────────────────────────────────────────────────────

# pane_pid prints the pid of the process running in the session's pane.
pane_pid() { "${TMUX[@]}" list-panes -t "$SESS_T" -F '#{pane_pid}' 2>/dev/null | head -1; }

# tree_pids prints every pid in the pane's tree, the pane process included, by
# following /proc/<pid>/task/*/children. A pid that vanishes mid-walk is skipped
# rather than fatal — the walk reports what is in the tree now.
tree_pids() {
  local root="$1"
  [ -n "$root" ] || return 0
  local -a frontier=() seen=() next=() kids=()
  local p t child
  frontier=("$root")
  seen=("$root")
  while [ "${#frontier[@]}" -gt 0 ]; do
    next=()
    for p in "${frontier[@]}"; do
      for t in /proc/"$p"/task/*; do
        [ -r "$t/children" ] || continue
        kids=()
        # proc children files have no trailing newline. read returns nonzero at EOF
        # after filling kids, so the populated array, not read status, is truth.
        read -r -a kids < "$t/children" 2>/dev/null || [ "${#kids[@]}" -gt 0 ] || continue
        for child in "${kids[@]}"; do
          case "$child" in ('' | *[!0-9]*) continue ;; esac
          [ -d "/proc/$child" ] || continue
          case " ${seen[*]} " in (*" $child "*) continue ;; esac
          seen+=("$child")
          next+=("$child")
        done
      done
    done
    if [ "${#next[@]}" -gt 0 ]; then
      frontier=("${next[@]}")
    else
      frontier=()
    fi
  done
  [ "${#seen[@]}" -gt 0 ] && printf '%s\n' "${seen[@]}"
  return 0
}

# ── /proc readers ────────────────────────────────────────────────────────────
# These are bash builtins reading a file, not subprocesses. The idle loop runs
# them for every pid twice a second, and a fork per field per pid would put the
# harness's own CPU inside the figure it is measuring.

# read_stat sets P_UTIME, P_STIME, P_START and P_PGRP from /proc/<pid>/stat, or
# returns 1 if the process is gone or the line is not what it should be.
#
# Fields are counted only AFTER stripping through the last ") ", because a
# process's comm may contain spaces or parentheses — summing whitespace-separated
# fields from the start of such a line reads the wrong columns and silently
# reports another process's CPU. ${raw##*) } strips the longest prefix ending in
# ") ", which is the last one; utime, stime, pgrp and starttime are then fields
# 14, 15, 5 and 22 of the whole line, i.e. indexes 11, 12, 2 and 19 of the rest.
P_UTIME=0
P_STIME=0
P_START=0
P_PGRP=0
read_stat() {
  local raw rest
  read -r raw < "/proc/$1/stat" 2>/dev/null || return 1
  [ -n "$raw" ] || return 1
  rest=${raw##*) }
  local -a f=()
  read -r -a f <<< "$rest"
  [ "${#f[@]}" -ge 20 ] || return 1
  P_UTIME=${f[11]}
  P_STIME=${f[12]}
  P_PGRP=${f[2]}
  P_START=${f[19]}
  case "$P_UTIME$P_STIME$P_PGRP$P_START" in (*[!0-9]*) return 1 ;; esac
  return 0
}

# read_status sets P_RSS and P_HWM in kB, P_THREADS and P_VOLCTX as counts, from
# /proc/<pid>/status, or returns 1 if the process is gone.
P_RSS=0
P_HWM=0
P_THREADS=0
P_VOLCTX=0
read_status() {
  local line key val
  P_RSS=0
  P_HWM=0
  P_THREADS=0
  P_VOLCTX=0
  [ -r "/proc/$1/status" ] || return 1
  while read -r line; do
    key=${line%%:*}
    case "$key" in
      (VmRSS | VmHWM | Threads | voluntary_ctxt_switches) ;;
      (*) continue ;;
    esac
    val=${line#*:}
    # the value is the first word after any run of spaces, then the unit is dropped
    read -r val _ <<< "$val"
    case "$val" in ('' | *[!0-9]*) val=0 ;; esac
    case "$key" in
      (VmRSS) P_RSS=$val ;;
      (VmHWM) P_HWM=$val ;;
      (Threads) P_THREADS=$val ;;
      (voluntary_ctxt_switches) P_VOLCTX=$val ;;
    esac
  done < "/proc/$1/status"
  return 0
}

# read_pss sets P_PSS in kB from /proc/<pid>/smaps_rollup.
#
# PSS is read from the rollup rather than smaps because the rollup is one small
# file the kernel summarises, where smaps lists every mapping. Proportional set
# size charges a shared page to each process that maps it in proportion to how
# many do, so a two-process design is not billed twice for one binary's shared
# text the way summing RSS bills it. It is read once per window and not per tick:
# the kernel walks every mapping to produce it, and sampling it twice a second
# would put that cost inside the idle figure it reports.
P_PSS=0
read_pss() {
  local line val
  P_PSS=0
  [ -r "/proc/$1/smaps_rollup" ] || return 0
  while read -r line; do
    case "$line" in
      (Pss:*)
        val=${line#*:}
        read -r val _ <<< "$val"
        case "$val" in ('' | *[!0-9]*) val=0 ;; esac
        P_PSS=$(( P_PSS + val ))
        ;;
    esac
  done < "/proc/$1/smaps_rollup"
  return 0
}

# count_fds sets P_FDS to the number of descriptors the process holds. nullglob
# makes an unreadable or vanished /proc/<pid>/fd count 0 rather than one literal
# unmatched pattern.
P_FDS=0
count_fds() {
  local -a f=()
  P_FDS=0
  [ -d "/proc/$1/fd" ] || return 0
  shopt -s nullglob
  f=(/proc/"$1"/fd/*)
  shopt -u nullglob
  P_FDS=${#f[@]}
  return 0
}

# ── process ownership and teardown ───────────────────────────────────────────

# own_pids applies one rule to every CLI: a process is its own when /proc reports
# the same executable as the command being measured. This includes a detached
# helper implemented by the same executable and excludes wrappers and shells.
own_pids() {
  local p exe
  for p in /proc/[0-9]*; do
    p=${p#/proc/}
    [ "$p" = "$$" ] && continue
    exe="$(readlink -f -- "/proc/$p/exe" 2>/dev/null)" || continue
    [ "$exe" = "$CLI_EXE" ] && printf '%s\n' "$p"
  done
}

# workspace_helper_pids identifies only this run's detached codeaf helper. The
# workspace argument is the ownership boundary, never HOME.
workspace_helper_pids() {
  local p cl
  for p in $(own_pids); do
    [ -r "/proc/$p/cmdline" ] || continue
    cl="$(tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null)"
    case " $cl " in
      (*" engine --daemon --workspace $WORKDIR "*) printf '%s\n' "$p" ;;
    esac
  done
}

# home_sweep_pids reports exactly what the legacy HOME sweep would select. It is
# retained for truthful survivor accounting, not as the helper ownership rule.
home_sweep_pids() {
  local p
  for p in /proc/[0-9]*; do
    p=${p#/proc/}
    [ "$p" = "$$" ] && continue
    [ -r "/proc/$p/environ" ] || continue
    LC_ALL=C grep -qzxF "HOME=$HOMEDIR" "/proc/$p/environ" 2>/dev/null || continue
    read_stat "$p" || continue
    [ "$P_START" -ge "$START_JIF" ] || continue
    printf '%s\n' "$p"
  done
}

SESSION_LIVE=0
TEARDOWN_DONE=0
START_JIF=0

# sweep_home signals every process of ours that carries this run's HOME in its
# environment AND started after this script did. The start-time test is what makes
# the sweep safe to leave on: a helper that called setsid and left the pane tree is
# still caught, while a process the person started themselves during the run — a
# shell, an editor in the same profile — is not, because it predates us.
# SWEEP=auto skips the sweep when the run's HOME is the invoking user's own, where
# matching on HOME would be least specific.
# shellcheck disable=SC2317  # reached from the EXIT trap, not by a direct call
sweep_home() {
  local sig="$1" p
  case "$SWEEP" in
    (never) return 0 ;;
    (always) ;;
    (*) [ "$HOMEDIR" != "$INVOKER_HOME" ] || return 0 ;;
  esac
  for p in $(home_sweep_pids); do
    kill "-$sig" "$p" 2>/dev/null || true
  done
  return 0
}

# shellcheck disable=SC2317  # reached from the EXIT trap, not by a direct call
teardown() {
  [ "$TEARDOWN_DONE" = 1 ] && return 0
  TEARDOWN_DONE=1
  local pids helpers sweepable p pgid left count
  pids="$(tree_pids "$(pane_pid)")"
  helpers="$(workspace_helper_pids)"
  pgid=""
  for p in $pids; do
    read_stat "$p" || continue
    pgid="$P_PGRP"
    break
  done
  # signal first, then take the session away: kill-session hangs up the pane
  # process, and a CLI that traps SIGHUP to clean up its own children should get
  # the chance before its terminal disappears
  if [ -n "$pgid" ]; then
    kill -TERM -- "-$pgid" 2>/dev/null || true
  fi
  if [ -n "$pids" ]; then
    # shellcheck disable=SC2086  # a pid list, one word each
    kill -TERM $pids 2>/dev/null || true
  fi
  for p in $helpers; do
    [ -r "/proc/$p/cmdline" ] || continue
    case " $(tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null) " in
      (*" engine --daemon --workspace $WORKDIR "*) kill -TERM "$p" 2>/dev/null || true ;;
    esac
  done
  sweep_home TERM
  sleep 0.4
  # A helper can detach while the pane is handling TERM, so discover again
  # before the final signal rather than relying only on the first snapshot.
  helpers="$helpers $(workspace_helper_pids)"
  if [ -n "$pgid" ]; then
    kill -KILL -- "-$pgid" 2>/dev/null || true
  fi
  if [ -n "$pids" ]; then
    # shellcheck disable=SC2086  # a pid list, one word each
    kill -KILL $pids 2>/dev/null || true
  fi
  for p in $helpers; do
    [ -r "/proc/$p/cmdline" ] || continue
    case " $(tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null) " in
      (*" engine --daemon --workspace $WORKDIR "*) kill -KILL "$p" 2>/dev/null || true ;;
    esac
  done
  sweep_home KILL
  if [ "$SESSION_LIVE" = 1 ]; then
    "${TMUX[@]}" kill-session -t "$SESS_T" 2>/dev/null || true
  fi
  "${TMUX[@]}" kill-server 2>/dev/null || true
  sleep 0.2
  # Count pane descendants and workspace helpers as leftovers owned by this run.
  owned_left=""
  for p in $pids $helpers; do
    [ -d "/proc/$p" ] || continue
    case " $owned_left " in (*" $p "*) ;; (*) owned_left="$owned_left $p" ;; esac
  done
  owned_count="$(printf '%s' "$owned_left" | wc -w)"
  # This skipped HOME sweep advisory is machine-wide, not attributable to this measurement.
  sweepable="$(home_sweep_pids)"
  advisory_count="$(printf '%s' "$sweepable" | wc -w)"
  PHASE="done"
  kv "teardown_owned_survivors=${owned_count:-0}"
  if [ -n "${owned_left// /}" ]; then
    kv "teardown_owned_survivor_pids=$(printf '%s' "$owned_left" | sed 's/^ //')"
  fi
  kv "advisory_skipped_home_sweep_matches=${advisory_count:-0}"
  return 0
}
trap teardown EXIT
trap 'exit 130' INT
trap 'exit 129' HUP
trap 'exit 143' TERM

# boot-relative jiffies at script start, for sweep_home's "started after us" test
read -r UPTIME_S _ < /proc/uptime 2>/dev/null || UPTIME_S=0
START_JIF=$(( ${UPTIME_S%%.*} * CLK_TCK ))
START_MS="$(now_ms)"
SAMPLE_SLEEP="$(sec_of "$SAMPLE_MS")"
POLL_SLEEP="$(sec_of "$FRAME_POLL_MS")"

# ── run context ──────────────────────────────────────────────────────────────
if [ "$SWEEP" = always ] && [ "$HOMEDIR" = "$INVOKER_HOME" ]; then
  # Disable the trap's sweep before refusing, otherwise refusal itself would
  # perform the unsafe operation it exists to prevent.
  SWEEP=never
  fail sweep-always-refused-because-home-is-the-invoking-users
fi
WORKDIR_IS_REPO=no
if [ -d "$WORKDIR/.git" ] || git -C "$WORKDIR" rev-parse --git-dir >/dev/null 2>&1; then
  WORKDIR_IS_REPO=yes
fi
kvq cmd "$(printf '%s ' "${CMD[@]}" | sed 's/ $//')"
kvq version_cmd "$VERSION_CMD"
kvq home "$HOMEDIR"
kvq workdir "$WORKDIR"
kv workdir_is_git_repo=$WORKDIR_IS_REPO
kv "pane=${PANE_COLS}x${PANE_ROWS}"
kv "idle_seconds=$IDLE_SECONDS"
kv "sample_ms=$SAMPLE_MS"
kv "settle_seconds=$SETTLE_SECONDS"
kv "startup_runs=$STARTUP_RUNS"
kv "startup_warmup=$STARTUP_WARMUP"
kv "clk_tck=$CLK_TCK"
kvq kernel "$(uname -sr)"
kv cpus="$(nproc 2>/dev/null || echo 0)"
loadavg
kv "load1=$LOAD1" "load5=$LOAD5" "load15=$LOAD15"
kvq tmux_version "$("${TMUX[@]}" -V 2>/dev/null | head -1)"
kv perf_event_paranoid="$(cat /proc/sys/kernel/perf_event_paranoid 2>/dev/null || echo unknown)"
kv "trust_mode=$TRUST"
kv "sweep_mode=$SWEEP"
kvq "tmux_socket" "$SOCKET"
kvq "pane_term" "$PANE_TERM"
kvq "bench_env" "${BENCH_ENV:-none}"
kv "env_isolated=yes"
PRIOR_OWN_PIDS="$(own_pids)"
PRIOR_OWN_COUNT="$(printf '%s\n' "$PRIOR_OWN_PIDS" | awk 'NF { n++ } END { print n+0 }')"
kv "prior_own_processes=$PRIOR_OWN_COUNT"
[ -z "$PRIOR_OWN_PIDS" ] || kvq prior_own_pids "$(printf '%s' "$PRIOR_OWN_PIDS" | tr '\n' ' ')"

# ── phase 1: startup ─────────────────────────────────────────────────────────
# Best-of-N wall clock for `--version`, run in WORKDIR under the run's HOME so it
# reads the same profile the interactive launch does. The minimum is the headline
# because it is the sample least contaminated by the machine doing something
# else; the median, mean and spread are printed beside it so a reader can see how
# much the machine was interfering and judge the minimum accordingly.
PHASE=startup
STARTUP_TOOL=none
STARTUP_MIN=-1
STARTUP_MEDIAN=-1
STARTUP_MEAN=-1
STARTUP_STDDEV=-1
STARTUP_EXIT=-1
STARTUP_LINE="$(version_line)"
loadavg
kv "startup_load1_before=$LOAD1"

if command -v hyperfine >/dev/null 2>&1; then
  STARTUP_TOOL=hyperfine
  tmp_json="$(mktemp)"
  rm -f "$tmp_json" # hyperfine refuses to overwrite a file that exists
  (
    cd "$WORKDIR" || exit 1
    hyperfine --runs "$STARTUP_RUNS" --warmup "$STARTUP_WARMUP" \
      --export-json "$tmp_json" -- "$STARTUP_LINE" >/dev/null 2>&1
  )
  STARTUP_EXIT=$?
  if [ -s "$tmp_json" ]; then
    STARTUP_MIN="$(ms_of "$(grep -o '"min":[0-9.eE+-]*' "$tmp_json" | head -1 | cut -d: -f2)")"
    STARTUP_MEDIAN="$(ms_of "$(grep -o '"median":[0-9.eE+-]*' "$tmp_json" | head -1 | cut -d: -f2)")"
    STARTUP_MEAN="$(ms_of "$(grep -o '"mean":[0-9.eE+-]*' "$tmp_json" | head -1 | cut -d: -f2)")"
    STARTUP_STDDEV="$(ms_of "$(grep -o '"stddev":[0-9.eE+-]*' "$tmp_json" | head -1 | cut -d: -f2)")"
  fi
  rm -f "$tmp_json"
else
  STARTUP_TOOL=shell-loop
  # Two `date` subprocesses per sample are inside every measurement equally, so
  # they raise the level of all of them and not the ordering between them. They
  # are about a millisecond each on this class of machine, which is why the loop
  # is the fallback and hyperfine the first choice.
  tmp_samples="$(mktemp)"
  i=0
  while [ "$i" -lt $(( STARTUP_RUNS + STARTUP_WARMUP )) ]; do
    t0="$(now_ms)"
    (
      cd "$WORKDIR" || exit 1
      eval "$STARTUP_LINE" >/dev/null 2>&1
    )
    rc=$?
    t1="$(now_ms)"
    i=$(( i + 1 ))
    [ "$i" -le "$STARTUP_WARMUP" ] && continue
    [ "$STARTUP_EXIT" = -1 ] && STARTUP_EXIT=$rc
    printf '%s\n' $(( t1 - t0 )) >> "$tmp_samples"
  done
  if [ -s "$tmp_samples" ]; then
    STARTUP_MIN="$(sort -n "$tmp_samples" | head -1)"
    STARTUP_MEDIAN="$(median_of < "$tmp_samples")"
    STARTUP_MEAN="$(awk '{ s += $1; n++ } END { if (n) printf "%.1f", s / n; else print 0 }' "$tmp_samples")"
    STARTUP_STDDEV="$(awk -v m="$STARTUP_MEAN" '{ d = $1 - m; s += d * d; n++ } END { if (n > 1) printf "%.1f", sqrt(s / (n - 1)); else print 0 }' "$tmp_samples")"
  fi
  rm -f "$tmp_samples"
fi

loadavg
kv "startup_load1_after=$LOAD1"
kv "tool=$STARTUP_TOOL"
kv "runs=$STARTUP_RUNS"
kv "warmup=$STARTUP_WARMUP"
kv "min_ms=$STARTUP_MIN"
kv "median_ms=$STARTUP_MEDIAN"
kv "mean_ms=$STARTUP_MEAN"
kv "stddev_ms=$STARTUP_STDDEV"
kv "exit=$STARTUP_EXIT"

# ── phase 2: first interactive frame ─────────────────────────────────────────
PHASE=frame
"${TMUX[@]}" kill-server 2>/dev/null || true
LAUNCH_MS="$(now_ms)"
if ! "${TMUX[@]}" new-session -d -s "$SESSION" -x "$PANE_COLS" -y "$PANE_ROWS" \
  -c "$WORKDIR" "$(launch_line)" 2>/dev/null; then
  fail tmux-new-session
fi
SESSION_LIVE=1
# pin the geometry: a detached session can otherwise be resized to the server's
# default, and pane size changes how much a CLI paints before its first frame
"${TMUX[@]}" set-option -t "$PANE_T" -w window-size manual >/dev/null 2>&1 || true
"${TMUX[@]}" resize-window -t "$PANE_T" -x "$PANE_COLS" -y "$PANE_ROWS" >/dev/null 2>&1 || true

capture_pane() { "${TMUX[@]}" capture-pane -p -t "$PANE_T" 2>/dev/null; }

# DISCOVERED_PIDS is the launched instance tree. It is seeded only from the pane
# process and grows while descendants remain attached. A discovered pid stays in
# the set while alive even if it later reparents, which counts helpers launched
# during frame, settle or idle without adopting pre-existing machine processes.
declare -A DISCOVERED=()
declare -a DISCOVERED_PIDS=() LIVE_PIDS=() ALL_PIDS=()
EMPTY_TICKS=0
refresh_tree() {
  local -a t=() alive=()
  local p
  mapfile -t t < <(tree_pids "$(pane_pid)")
  if [ "${#t[@]}" -eq 0 ]; then
    EMPTY_TICKS=$(( EMPTY_TICKS + 1 ))
  fi
  for p in "${t[@]}"; do
    [ -n "${DISCOVERED[$p]:-}" ] && continue
    DISCOVERED[$p]=1
    DISCOVERED_PIDS+=("$p")
  done
  for p in "${DISCOVERED_PIDS[@]}"; do
    [ -d "/proc/$p" ] && alive+=("$p")
  done
  LIVE_PIDS=("${alive[@]}")
  ALL_PIDS=("${DISCOVERED_PIDS[@]}")
  return 0
}

# sampled_sleep keeps discovery active during pauses outside the measured idle
# window. The final refresh covers a duration shorter than one sampling tick.
sampled_sleep() {
  local seconds="$1" deadline
  deadline=$(( $(now_ms) + seconds * 1000 ))
  while [ "$(now_ms)" -lt "$deadline" ]; do
    refresh_tree
    sleep "$SAMPLE_SLEEP"
  done
  refresh_tree
}

refresh_tree

# wait_paint blocks until the pane holds a non-blank character, or the pane's
# process exits, or WAIT_TIMEOUT_MS passes. It reads WAIT_EPOCH as the moment the
# clock started and sets WAIT_MS and WAIT_PAINTED. A pane that never paints
# reports -1 and not the timeout, so a run that measured nothing cannot be read
# as a CLI that took sixty seconds to draw.
PANE_TEXT=""
WAIT_EPOCH="$LAUNCH_MS"
WAIT_MS=-1
WAIT_PAINTED=no
WAIT_TIMEOUT_MS="$FRAME_TIMEOUT_MS"
wait_paint() {
  local deadline=$(( WAIT_EPOCH + WAIT_TIMEOUT_MS ))
  WAIT_PAINTED=no
  WAIT_MS=-1
  while :; do
    refresh_tree
    PANE_TEXT="$(capture_pane)"
    if [ -n "${PANE_TEXT//[[:space:]]/}" ]; then
      WAIT_PAINTED=yes
      WAIT_MS=$(( $(now_ms) - WAIT_EPOCH ))
      return 0
    fi
    if [ "$("${TMUX[@]}" list-panes -t "$SESS_T" -F '#{pane_dead}' 2>/dev/null | head -1)" = 1 ]; then
      return 0
    fi
    [ "$(now_ms)" -ge "$deadline" ] && return 0
    sleep "$POLL_SLEEP"
  done
}

# line_is_option is true for a line in a choice list: a cursor marker or a number.
line_is_option() {
  printf '%s' "$1" | grep -Eq '^[[:space:]]*(❯|›|>|●|■|\*|[0-9]+[.)])'
}

# line_is_selected is true for the line the cursor sits on.
line_is_selected() {
  printf '%s' "$1" | grep -Eq '^[[:space:]]*(❯|›|●|■)'
}

# line_is_refusal is true for an option that declines: the one a folder-trust gate
# commonly opens with preselected.
line_is_refusal() {
  printf '%s' "$1" |
    grep -Eiq '(^|[^[:alpha:]])(no|not|don.?t|deny|exit|cancel|quit|reject|refuse)([^[:alpha:]]|$)'
}

# line_is_accept is true for an option that agrees, and only when the same line
# carries no refusal: a gate offering "Yes, and don't ask again" beside "No, exit"
# makes a bare keyword match pick the wrong one.
line_is_accept() {
  printf '%s' "$1" |
    grep -Eiq '(^|[^[:alpha:]])(yes|y\b|trust|proceed|continue|allow|accept|always|enter)([^[:alpha:]]|$)' &&
    ! line_is_refusal "$1"
}

# answer_trust_gate answers a folder-trust gate, and only a folder-trust gate.
#
# The naive test — does the splash contain the word "trust" — fires on a CLI that
# merely mentions trusting anything, and the keystroke meant for a gate then lands
# in a session that never had one, corrupting whatever it was measuring. So the
# gate has to show BOTH halves of a question that blocks the session:
#
#   1. it asks about trusting THIS LOCATION: "trust" and folder/directory/
#      workspace/project/repository/path close together on one line; and
#   2. it is a preselected choice list that offers a way to refuse: a cursor
#      marker or numbered options, and an option reading as a refusal.
#
# A theme picker, a sign-in list, a provider chooser and a welcome box all have
# half 2 and none of half 1, and are left alone. Answering then moves the cursor
# to the accepting option by counting option lines rather than pressing Down once
# and hoping: the refusing option is commonly the preselected one, and Enter on it
# exits the CLI before anything is measured.
TRUST_GATE=absent
TRUST_SELECTED=""
TRUST_ACCEPT=""
answer_trust_gate() {
  local text="$1" line
  local -a opts=()
  local sel=0 acc=0 pos=0 delta key n i after
  local has_list=0 has_refusal=0

  if [ "$TRUST" = never ]; then
    TRUST_GATE=skipped
    return 1
  fi
  if [ "$TRUST" != always ]; then
    # half 1: the question is about trusting this location, not about trust in
    # general — a splash that merely says the word must not be answered
    printf '%s' "$text" | grep -Eiq \
      'trust[^.?!|]{0,48}(folder|director|workspace|project|repositor|path|files?)|(folder|director|workspace|project|repositor|path)[^.?!|]{0,48}trust' ||
      return 1
    # half 2: a choice list is on screen and one of its options declines
    while IFS= read -r line; do
      line_is_option "$line" || continue
      has_list=1
      line_is_refusal "$line" && has_refusal=1
    done <<< "$text"
    [ "$has_list" = 1 ] || return 1
    [ "$has_refusal" = 1 ] || return 1
  fi
  TRUST_GATE=detected

  while IFS= read -r line; do
    case "$line" in (*[![:space:]]*) ;; (*) continue ;; esac
    line_is_option "$line" || continue
    pos=$(( pos + 1 ))
    opts+=("$line")
    if [ "$sel" = 0 ] && line_is_selected "$line"; then sel=$pos; fi
    if [ "$acc" = 0 ] && line_is_accept "$line"; then acc=$pos; fi
  done <<< "$text"

  # a list with no visible cursor: the first option is the one Enter would take
  [ "$sel" -gt 0 ] || sel=1
  TRUST_SELECTED="$(printf '%s' "${opts[$(( sel - 1 ))]:-}" | tr -s '[:space:]' ' ' | cut -c1-64)"
  if [ "$acc" = 0 ] || [ "${#opts[@]}" -lt 1 ]; then
    # nothing reads as an acceptance. Guessing here exits the CLI, so leave the
    # gate standing and say so in the output instead of reporting a frame that
    # measured a dialog nobody knows about.
    TRUST_GATE=detected-no-accept-option
    return 1
  fi
  TRUST_ACCEPT="$(printf '%s' "${opts[$(( acc - 1 ))]:-}" | tr -s '[:space:]' ' ' | cut -c1-64)"

  delta=$(( acc - sel ))
  key=Down
  n=$delta
  if [ "$delta" -lt 0 ]; then
    key=Up
    n=$(( -delta ))
  fi
  i=0
  while [ "$i" -lt "$n" ]; do
    "${TMUX[@]}" send-keys -t "$PANE_T" "$key" 2>/dev/null || true
    sleep 0.15
    i=$(( i + 1 ))
  done
  sleep 0.3
  "${TMUX[@]}" send-keys -t "$PANE_T" Enter 2>/dev/null || true
  sleep 2
  after="$(capture_pane)"
  if [ "$after" = "$text" ]; then
    TRUST_GATE=detected-unchanged
    return 1
  fi
  TRUST_GATE=cleared
  PANE_TEXT="$after"
  return 0
}

wait_paint
FIRST_PAINT_MS="$WAIT_MS"
FIRST_PAINTED="$WAIT_PAINTED"
GATE_TEXT="$PANE_TEXT"

# The frame that counts is the session's, so a gate is answered first and the pane
# waited on again. With no gate, frame_ms is first_paint_ms and the two are
# directly comparable between CLIs. With one, frame_ms also contains the harness's
# own keystrokes and settle time, which is why both figures and trust_gate= are
# printed: a gate makes the pair non-comparable to a CLI that had none, and the
# output says so rather than leaving a reader to guess.
FRAME_MS="$FIRST_PAINT_MS"
TRUST_GATE_MS=0
if [ "$FIRST_PAINTED" = yes ]; then
  if answer_trust_gate "$GATE_TEXT"; then
    GATE_NOW="$(now_ms)"
    wait_paint
    FRAME_MS="$WAIT_MS"
    TRUST_GATE_MS=$(( GATE_NOW - LAUNCH_MS ))
  fi
else
  TRUST_GATE=no-paint
fi
[ -z "$BENCH_OUT" ] || printf '%s\n' "$(capture_pane)" > "$BENCH_OUT/frame-${SAFE_NAME}.txt" 2>/dev/null

kv "first_paint_ms=$FIRST_PAINT_MS"
kv "frame_ms=$FRAME_MS"
kv "painted=$FIRST_PAINTED"
kv "trust_gate=$TRUST_GATE"
kv "gate_handling_ms=$TRUST_GATE_MS"
[ -z "$TRUST_SELECTED" ] || kvq gate_selected_option "$TRUST_SELECTED"
[ -z "$TRUST_ACCEPT" ] || kvq gate_accept_option "$TRUST_ACCEPT"
kv procs_at_frame="$(tree_pids "$(pane_pid)" | wc -l)"

# ── phase 3: idle cost over the window ───────────────────────────────────────
PHASE=idle
loadavg
kv "idle_load1_before=$LOAD1"
sampled_sleep "$SETTLE_SECONDS"
[ -n "$(pane_pid)" ] || fail no-pane-pid

declare -A JIF_BASE=() JIF_LAST=() CTX_BASE=() CTX_LAST=()
declare -a SAMPLE_RSS=()
RSS_TOTAL=0
PSS_TOTAL=0
HWM_TOTAL=0
THREAD_TOTAL=0
FD_TOTAL=0
ALIVE_NOW=0
TICK_RSS=0

# refresh_tree was started at launch and continues to retain the launched tree.
# Detached descendants remain in LIVE_PIDS while alive; unrelated prior processes
# can never enter because discovery has no machine-wide or workspace-wide seed.

# sample_tree takes one reading per live pid: the cumulative counters, kept as the
# last good value per pid, and this tick's summed RSS. Both counters are monotonic
# for the life of a process, so a reading is accepted only when it does not go
# backwards, and a pid that dies mid-window keeps the value it last reported. That
# is what stops a delta taken across a changing pid set from coming out negative
# or from losing the work a short-lived child already did.
sample_tree() {
  local p v
  TICK_RSS=0
  for p in "${LIVE_PIDS[@]}"; do
    [ -d "/proc/$p" ] || continue
    if read_stat "$p"; then
      v=$(( P_UTIME + P_STIME ))
      if [ -z "${JIF_BASE[$p]:-}" ]; then
        JIF_BASE[$p]=$v
        JIF_LAST[$p]=$v
      elif [ "$v" -ge "${JIF_LAST[$p]}" ]; then
        JIF_LAST[$p]=$v
      fi
    fi
    read_status "$p" || continue
    TICK_RSS=$(( TICK_RSS + P_RSS ))
    v=$P_VOLCTX
    if [ -z "${CTX_BASE[$p]:-}" ]; then
      CTX_BASE[$p]=$v
      CTX_LAST[$p]=$v
    elif [ "$v" -ge "${CTX_LAST[$p]}" ]; then
      CTX_LAST[$p]=$v
    fi
  done
  return 0
}

# sum_live adds the instantaneous figures over the pids alive at the end of the
# window. A pid that is gone contributes nothing: carrying the last memory a dead
# process held would overstate what the CLI costs while it sits idle.
sum_live() {
  local p
  RSS_TOTAL=0
  PSS_TOTAL=0
  HWM_TOTAL=0
  THREAD_TOTAL=0
  FD_TOTAL=0
  ALIVE_NOW=0
  for p in "${LIVE_PIDS[@]}"; do
    [ -d "/proc/$p" ] || continue
    read_status "$p" || continue
    RSS_TOTAL=$(( RSS_TOTAL + P_RSS ))
    HWM_TOTAL=$(( HWM_TOTAL + P_HWM ))
    THREAD_TOTAL=$(( THREAD_TOTAL + P_THREADS ))
    count_fds "$p"
    FD_TOTAL=$(( FD_TOTAL + P_FDS ))
    read_pss "$p"
    PSS_TOTAL=$(( PSS_TOTAL + P_PSS ))
    ALIVE_NOW=$(( ALIVE_NOW + 1 ))
  done
  return 0
}

refresh_tree
sample_tree
WINDOW_T0="$(now_ms)"

TICKS=$(( IDLE_SECONDS * 1000 / SAMPLE_MS ))
[ "$TICKS" -ge 1 ] || TICKS=1
i=0
while [ "$i" -lt "$TICKS" ]; do
  sleep "$SAMPLE_SLEEP"
  refresh_tree
  sample_tree
  SAMPLE_RSS+=("$TICK_RSS")
  i=$(( i + 1 ))
done

WINDOW_T1="$(now_ms)"
WINDOW_MS=$(( WINDOW_T1 - WINDOW_T0 ))
loadavg
kv "idle_load1_after=$LOAD1"

refresh_tree
sum_live

JIF_DELTA=0
CTX_DELTA=0
for p in ${ALL_PIDS+"${ALL_PIDS[@]}"}; do
  [ -n "${JIF_BASE[$p]:-}" ] || continue
  JIF_DELTA=$(( JIF_DELTA + JIF_LAST[$p] - JIF_BASE[$p] ))
  [ -n "${CTX_BASE[$p]:-}" ] || continue
  CTX_DELTA=$(( CTX_DELTA + CTX_LAST[$p] - CTX_BASE[$p] ))
done
# a counter can still come out backwards if a pid was reused inside the window;
# the honest figure then is zero, not a negative cost
[ "$JIF_DELTA" -lt 0 ] && JIF_DELTA=0
[ "$CTX_DELTA" -lt 0 ] && CTX_DELTA=0

CPU_PCT="$(awk -v d="$JIF_DELTA" -v hz="$CLK_TCK" -v ms="$WINDOW_MS" \
  'BEGIN { if (ms > 0) printf "%.2f", (d / hz) / (ms / 1000) * 100; else printf "0.00" }')"
CPU_SECONDS="$(awk -v d="$JIF_DELTA" -v hz="$CLK_TCK" 'BEGIN { printf "%.3f", d / hz }')"
CTX_PER_S="$(awk -v d="$CTX_DELTA" -v ms="$WINDOW_MS" \
  'BEGIN { if (ms > 0) printf "%.2f", d / (ms / 1000); else printf "0.00" }')"
RSS_MEDIAN="$(printf '%s\n' "${SAMPLE_RSS[@]}" | median_of)"
RSS_MAX="$(printf '%s\n' "${SAMPLE_RSS[@]}" | sort -n | tail -1)"

kv "window_ms=$WINDOW_MS"
kv "samples=${#SAMPLE_RSS[@]}"
kv "empty_tree_ticks=$EMPTY_TICKS"
kv "procs_alive=$ALIVE_NOW"
kv "procs_seen=${#ALL_PIDS[@]}"
kv "rss_kb=$RSS_TOTAL"
kv "rss_kb_median=${RSS_MEDIAN:-0}"
kv "rss_kb_max=${RSS_MAX:-0}"
kv "pss_kb=$PSS_TOTAL"
kv "peak_rss_kb=$HWM_TOTAL"
kv "threads=$THREAD_TOTAL"
kv "fds=$FD_TOTAL"
kv "cpu_pct_of_one_core=$CPU_PCT"
kv "cpu_seconds=$CPU_SECONDS"
kv "voluntary_ctxsw_per_s=$CTX_PER_S"

[ "$ALIVE_NOW" -gt 0 ] || fail no-process

# ── per-process lines ────────────────────────────────────────────────────────
PHASE=proc
PROC_OUT=""
for p in "${LIVE_PIDS[@]}"; do
  [ -d "/proc/$p" ] || continue
  comm="?"
  read -r comm < "/proc/$p/comm" 2>/dev/null || comm="?"
  read_status "$p" || continue
  read_pss "$p"
  count_fds "$p"
  cl="$(tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null)"
  line="name=$NAME phase=proc pid=$p comm=$comm rss_kb=$P_RSS pss_kb=$P_PSS peak_rss_kb=$P_HWM threads=$P_THREADS fds=$P_FDS cmd=${cl:0:120}"
  printf '%s\n' "$line"
  PROC_OUT="$PROC_OUT$line
"
done
if [ -n "$BENCH_OUT" ] && [ -n "$PROC_OUT" ]; then
  printf '%s' "$PROC_OUT" > "$BENCH_OUT/procs-${SAFE_NAME}.txt" 2>/dev/null
fi

# ── summary ──────────────────────────────────────────────────────────────────
PHASE=summary
printf 'name=%s phase=summary startup_tool=%s startup_ms=%s first_paint_ms=%s frame_ms=%s trust_gate=%s procs=%s rss_kb=%s rss_kb_median=%s rss_kb_max=%s pss_kb=%s peak_rss_kb=%s threads=%s fds=%s cpu_pct_of_one_core=%s voluntary_ctxsw_per_s=%s window_ms=%s\n' \
  "$NAME" "$STARTUP_TOOL" "$STARTUP_MIN" "$FIRST_PAINT_MS" "$FRAME_MS" "$TRUST_GATE" \
  "$ALIVE_NOW" "$RSS_TOTAL" "${RSS_MEDIAN:-0}" "${RSS_MAX:-0}" "$PSS_TOTAL" "$HWM_TOTAL" \
  "$THREAD_TOTAL" "$FD_TOTAL" "$CPU_PCT" "$CTX_PER_S" "$WINDOW_MS"

PHASE="done"
kv ok=1
kv "elapsed_ms=$(( $(now_ms) - START_MS ))"
exit "$EXIT_STATUS"
