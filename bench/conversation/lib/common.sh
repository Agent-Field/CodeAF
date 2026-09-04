#!/usr/bin/env bash
# common.sh — the portable floor every other module in this suite stands on.
#
# Two things live here and nothing else: the handful of shell facts that differ
# between a Mac and a Linux box, and the small helpers that turn a value into a
# line of evidence. Everything harness-specific is in adapters.sh; everything
# about which models may be called is in allowlist.sh.
#
# Portability is not a nicety here. The rig this suite grew out of
# (bench/canary/lib/chat.sh) reads a file's modification time with `stat -c %Y`,
# which is GNU-only: on macOS that call fails, the fallback puts "now" in the
# variable, and the quiet-for-N-seconds witness therefore never fires. A witness
# that silently never fires is worse than no witness, so every OS-dependent call
# in this suite goes through a function below that is written for both.

# conv_root is this suite's directory; CONV_REPO_ROOT is the checkout it sits in.
CONV_LIB="${CONV_LIB:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
CONV_ROOT="${CONV_ROOT:-$(cd "$CONV_LIB/.." && pwd)}"
CONV_REPO_ROOT="${CONV_REPO_ROOT:-$(cd "$CONV_ROOT/../.." && pwd)}"

# TIMEOUT_BIN caps a print-door invocation. The interactive door does not use it:
# a TUI killed by an outside signal leaves no reply on the screen to read, so
# that door carries its own clock and ends the session the way a person would.
TIMEOUT_BIN="$(command -v timeout || command -v gtimeout || true)"

# now_s and file_mtime are the two clocks the doors read.
now_s() { date +%s; }

# file_mtime prints a file's modification time in epoch seconds, or nothing when
# the file does not exist. BSD stat and GNU stat spell this differently and
# neither accepts the other's flag, so both are tried before giving up.
file_mtime() {
  local path="$1"
  [ -e "$path" ] || return 1
  stat -f %m "$path" 2>/dev/null || stat -c %Y "$path" 2>/dev/null
}

# quiet_for prints how many seconds ago a file last changed, or the sentinel
# -1 when the file is not there yet. Callers must treat -1 as "no evidence
# either way" rather than as a large quiet period: a call log that has not been
# created yet is not a call log that has gone quiet.
quiet_for() {
  local path="$1" mtime
  mtime="$(file_mtime "$path" 2>/dev/null)" || { echo -1; return; }
  [ -n "$mtime" ] || { echo -1; return; }
  echo $(( $(now_s) - mtime ))
}

conv_log() { printf '%s\n' "$*"; }
conv_warn() { printf '%s\n' "$*" >&2; }

# quote_argv prints an argv a person could paste back into a shell. A scenario
# prompt is a paragraph and would drown the line, so long arguments are shown as
# a head and a length — the same treatment bench/e2e/run.sh gives them.
quote_argv() {
  local part
  for part in "$@"; do
    case "$part" in
      *[!A-Za-z0-9@%+=:,./_-]*)
        if [ "${#part}" -gt 60 ]; then
          printf " '%s… (%d chars)'" "$(printf '%s' "$part" | head -1 | cut -c1-56)" "${#part}"
        else
          printf " '%s'" "$part"
        fi
        ;;
      *) printf ' %s' "$part" ;;
    esac
  done
  printf '\n'
}

# csv_field makes a value safe for a CSV cell: no commas, no newlines.
csv_field() {
  printf '%s' "$1" | tr '\n' ' ' | tr ',' ';' | sed 's/  */ /g; s/^ *//; s/ *$//'
}

# json_str quotes a value for embedding in JSON. Scenario prompts and screen
# lines carry quotes, backslashes and control characters, and a receipt that
# does not parse is not a receipt.
json_str() {
  CONV_JSON_IN="${1-}" python3 -c 'import json, os; print(json.dumps(os.environ["CONV_JSON_IN"]))'
}

# CONV_CARRIED lists the only variables a harness process inherits. Everything
# else is dropped, credentials included: a peer that never sees an Anthropic or
# OpenAI key cannot spend one, which enforces the open-model policy at the only
# point where enforcement is certain. PATH, HOME, TERM, LANG and TMPDIR are
# carried because a CLI that cannot find its own runtime, home or terminal fails
# for reasons that have nothing to do with what is being measured.
CONV_CARRIED="PATH HOME TERM LANG LC_ALL TMPDIR OPENROUTER_API_KEY"

# CONV_PASS_ENV names extra variables to carry, for the cases where a harness
# needs one this suite does not know about (and for the fakes in test/, which
# are configured through the environment). It is a list of NAMES, so what is
# carried is always visible in the cell's config record.
CONV_PASS_ENV="${CONV_PASS_ENV:-}"

# child_env prints `env -i NAME=VALUE …` for the carried variables plus the
# per-arm ones passed as arguments. Callers use it for both doors.
child_env() {
  local name value
  printf 'env -i'
  for name in $CONV_CARRIED $CONV_PASS_ENV; do
    eval "value=\${$name:-}"
    [ -n "$value" ] || continue
    printf ' %s=%s' "$name" "$(printf '%q' "$value")"
  done
  for value in "$@"; do printf ' %s' "$(printf '%q' "$value")"; done
}

# child_env_array fills CHILD_ENV with the same thing in argv form.
CHILD_ENV=()
child_env_array() {
  local name value
  CHILD_ENV=(env -i)
  for name in $CONV_CARRIED $CONV_PASS_ENV; do
    eval "value=\${$name:-}"
    [ -n "$value" ] || continue
    CHILD_ENV+=("$name=$value")
  done
  for value in "$@"; do CHILD_ENV+=("$value"); done
}

# record_config writes the cell's configuration as evidence. It is a list of
# things worth knowing — binary, version, pins, isolation, door — and not a dump
# of the environment: a redaction regex over everything a shell happens to hold
# is one unfamiliar variable name away from publishing a secret.
record_config() {
  local out="$1"; shift
  local line
  : > "$out"
  for line in "$@"; do printf '%s\n' "$line" >> "$out"; done
  # Credential names only, never values, so a reader can tell which key the run
  # could have used without the file carrying one.
  printf 'credentials_present:%s\n' \
    "$(env | awk -F= '$1 ~ /(KEY|TOKEN|SECRET)$/ { printf " %s", $1 }')" >> "$out"
}

# require_tools says what is missing before a run starts rather than letting a
# cell discover it as exit 127, which writes a row indistinguishable from a
# harness that ran and produced nothing.
require_tools() {
  local missing="" tool
  for tool in "$@"; do
    command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
  done
  if [ -n "$missing" ]; then
    conv_warn "missing required tool(s):$missing"
    return 1
  fi
  return 0
}
