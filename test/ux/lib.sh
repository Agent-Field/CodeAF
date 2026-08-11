#!/usr/bin/env bash
# lib.sh — the vocabulary a journey script is written in.
#
# A journey is a person at a terminal. It says things, waits for the screen to
# say something back, and then asks the journal whether anything durable
# actually happened. Those three verbs — say, wait_screen, journal — are the
# whole library; everything else is bookkeeping so the report can print WHAT
# was expected beside WHAT was there.
#
# Sourced by every journeys/*.sh under an environment run.sh exports:
#   UX_BIN UX_HOME UX_STATE UX_DB UX_SESSION UX_EVIDENCE UX_CAP UX_JOURNEY
#   UX_SURFACE UX_CHAT_ARGS

set -uo pipefail

: "${UX_REPLY_TIMEOUT:=120}"   # a conversational reply
: "${UX_JOB_TIMEOUT:=300}"     # a commissioned job
: "${UX_POLL:=2}"

# ---------------------------------------------------------------- the surface
#
# UX_SURFACE is `v1` (the old chat) or `v2` (`aforge chat --v2`). chat-rebuild
# 11.1 keeps both alive until the parity checklist passes, so every journey is
# written ONCE and run TWICE. What a journey says, and everything it asks the
# journal, is identical on both. A journey branches only where 13.4 records
# that v2 legitimately draws the same fact somewhere else — and every such
# branch cites the section it follows. The v1 side of a branch is never
# loosened to make the v2 side agree: a v2 gap is a red journey and a finding,
# not a re-worded assertion.
: "${UX_SURFACE:=v1}"
: "${UX_CHAT_ARGS:=}"

is_v2() { [ "$UX_SURFACE" = "v2" ]; }

# surface_note records, in the evidence, which anatomy an assertion followed —
# so a reader of the v2 report can tell "asserted something else" from
# "asserted the same thing and it was missing".
surface_note() { note "[$UX_SURFACE] $*"; }

UX_FAILURES=0
UX_CHECKS=0
UX_FORCED=""
UX_DIR="$UX_EVIDENCE/$UX_JOURNEY"
mkdir -p "$UX_DIR"
: > "$UX_DIR/notes.md"

# ---------------------------------------------------------------- narration

note() { printf '%s\n' "$*" >> "$UX_DIR/notes.md"; printf '    %s\n' "$*" >&2; }
head_line() { printf '\n## %s\n\n' "$*" >> "$UX_DIR/notes.md"; printf '  %s\n' "$*" >&2; }

# journey_is echoes the journey's line from docs/JOURNEY.md so a reader of the
# transcript never has to go and look up what number 4 was.
journey_is() {
  printf '# %s — %s\n\n' "$UX_JOURNEY" "$*" >> "$UX_DIR/notes.md"
  printf '_surface: %s (`aforge chat %s`)_\n\n' "$UX_SURFACE" "$UX_CHAT_ARGS" >> "$UX_DIR/notes.md"
  printf '\n\033[1m▶ %s [%s] — %s\033[0m\n' "$UX_JOURNEY" "$UX_SURFACE" "$*" >&2
}

record() { printf -- '- **%s**: %s\n' "$1" "$2" >> "$UX_DIR/notes.md"; printf -- '    · %s: %s\n' "$1" "$2" >&2; }

# ---------------------------------------------------------------- the mouth

# say types a sentence and presses Enter, with enough pacing that the composer
# has drawn the draft before the newline lands. Never start a sentence with
# "/" (the command palette eats Enter) or "?" (it opens the help overlay).

# clear_composer empties the draft. A digit typed at a question that has since
# been answered stays in the composer and silently prefixes the next sentence
# ("2stand down the stretch reminder"), which would make the suite lie about
# what the user said.
clear_composer() { tmux send-keys -t "${1:-$UX_SESSION}" C-u; sleep 0.3; }

say() {
  local text="$1" sess="${2:-$UX_SESSION}"
  case "$text" in
    /*) note "REFUSED to say a line starting with / — the palette would eat it"; return 1 ;;
    \?*) note "REFUSED to say a line starting with ? — it opens help"; return 1 ;;
  esac
  clear_composer "$sess"
  tmux send-keys -t "$sess" -l -- "$text"
  sleep 0.6
  tmux send-keys -t "$sess" Enter
  note "said: $text"
}

# press sends a named key (Enter, Up, C-c …).
press() { tmux send-keys -t "${2:-$UX_SESSION}" "$1"; note "pressed: $1"; }

# type_raw sends characters with no Enter. Answering an option question is a
# bare digit that submits itself, so it must NOT be followed by a newline.
type_raw() { tmux send-keys -t "${2:-$UX_SESSION}" -l -- "$1"; note "typed: $1"; }

answer_number() { type_raw "$1" "${2:-$UX_SESSION}"; sleep 3; }

# ---------------------------------------------------------------- the eyes

pane() { tmux capture-pane -t "${1:-$UX_SESSION}" -p 2>/dev/null; }

snap() {
  local name="$1" sess="${2:-$UX_SESSION}"
  pane "$sess" > "$UX_DIR/$name.snap.txt"
  note "snap: $name.snap.txt"
}

# wait_screen polls the pane until the regex matches. Match on SHORT fragments:
# every line is wrapped and hard-clamped to the pane width, so a long literal
# can straddle two rows and never appear whole.
wait_screen() {
  local re="$1" limit="${2:-$UX_REPLY_TIMEOUT}" sess="${3:-$UX_SESSION}" waited=0
  while [ "$waited" -lt "$limit" ]; do
    if pane "$sess" | grep -Eqi -- "$re"; then return 0; fi
    sleep "$UX_POLL"; waited=$((waited + UX_POLL))
  done
  return 1
}

# ---------------------------------------------------------------- the journal

journal() {
  sqlite3 -readonly "$UX_DB" "$1" 2>/dev/null
}

journal_table() { sqlite3 -readonly -header -column "$UX_DB" "$1" 2>/dev/null; }

# mark is the journal watermark before a turn. Every assertion afterwards is
# scoped to "since the mark", so journey 5 can never pass on journey 2's rows.
mark() { journal "select coalesce(max(seq),0) from events"; }

spend() { journal "select coalesce(round(sum(cost),6),0) from usage"; }

# wait_journal polls a SQL count until it is greater than zero.
wait_journal() {
  local sql="$1" limit="${2:-$UX_JOB_TIMEOUT}" waited=0 value
  while [ "$waited" -lt "$limit" ]; do
    value="$(journal "$sql")"
    if [ -n "$value" ] && [ "$value" != "0" ]; then return 0; fi
    sleep "$UX_POLL"; waited=$((waited + UX_POLL))
  done
  return 1
}

# ---------------------------------------------------------------- assertions

_check() {
  local ok="$1" what="$2" expected="$3" actual="$4"
  UX_CHECKS=$((UX_CHECKS + 1))
  if [ "$ok" = "yes" ]; then
    printf -- '- ✅ %s\n      expected: %s\n      found:    %s\n' "$what" "$expected" "$actual" >> "$UX_DIR/notes.md"
    printf '    \033[32m✅ %s\033[0m\n' "$what" >&2
  else
    UX_FAILURES=$((UX_FAILURES + 1))
    printf -- '- ❌ %s\n      expected: %s\n      found:    %s\n' "$what" "$expected" "$actual" >> "$UX_DIR/notes.md"
    printf '    \033[31m❌ %s\033[0m\n       expected: %s\n       found:    %s\n' "$what" "$expected" "$actual" >&2
    snap "fail-$UX_CHECKS"
  fi
}

# assert_screen waits for a pattern and reports the last screen when it never
# arrives, because "the screen never said it" is only useful beside the screen.
assert_screen() {
  local re="$1" what="$2" limit="${3:-$UX_REPLY_TIMEOUT}"
  if wait_screen "$re" "$limit"; then
    _check yes "$what" "screen matching /$re/" "$(pane | grep -Eio -- "$re" | head -1)"
  else
    _check no "$what" "screen matching /$re/ within ${limit}s" "timed out; last screen in fail snap"
  fi
}

refute_screen() {
  local re="$1" what="$2"
  if pane | grep -Eqi -- "$re"; then
    _check no "$what" "no screen line matching /$re/" "$(pane | grep -Eio -- "$re" | head -1)"
  else
    _check yes "$what" "no screen line matching /$re/" "none"
  fi
}

# assert_journal passes when the query returns something that is neither empty
# nor zero. The query IS the expectation, so it prints verbatim on failure.
assert_journal() {
  local sql="$1" what="$2" limit="${3:-0}" value
  if [ "$limit" -gt 0 ]; then wait_journal "$sql" "$limit" >/dev/null; fi
  value="$(journal "$sql")"
  if [ -n "$value" ] && [ "$value" != "0" ]; then
    _check yes "$what" "$sql → non-zero" "$value"
  else
    _check no "$what" "$sql → non-zero" "${value:-<empty>}"
  fi
}

assert_journal_is() {
  local sql="$1" want="$2" what="$3" value
  value="$(journal "$sql")"
  if [ "$value" = "$want" ]; then
    _check yes "$what" "$sql = $want" "$value"
  else
    _check no "$what" "$sql = $want" "${value:-<empty>}"
  fi
}

assert_cmd() {
  local what="$1"; shift
  if "$@" >/dev/null 2>&1; then
    _check yes "$what" "$* succeeds" "exit 0"
  else
    _check no "$what" "$* succeeds" "exit $?"
  fi
}

# ---------------------------------------------------------------- gauges
#
# Completion is the cheap half. A journey that passes every assertion can still
# have spliced eleven tasks to write a haiku, spent a dollar doing it, taken
# four minutes, filled the thread with chatter, and produced something bad.
# These four leave a trace the runner turns into the report card.

# expect_nodes declares the proportionate size of this ask. The runner flags a
# journey that formed materially more work than the ask deserved.
expect_nodes() { printf '%s\n' "$1" > "$UX_DIR/expect-nodes"; }

# judge_this hands one deliverable to a cheap model against a fixed rubric.
# The suite can prove a haiku was journaled; only a reader can say it is a
# haiku about rivers that mentions a heron.
judge_this() {
  printf '%s\n' "$1" > "$UX_DIR/judge-ask"
  printf '%s\n' "$2" > "$UX_DIR/judge-deliverable"
  note "queued for the quality judge (${#2} bytes of deliverable)"
}

# deliverable_since is the usual thing to judge: everything the agent said
# after the ask, which is where an answer-first product puts the content.
deliverable_since() {
  journal "select group_concat(body, char(10)) from messages where seq > $1 and role in ('agent','system')"
}

# ---------------------------------------------------------------- verdicts

# observed marks a journey whose mechanism has a threshold we cannot force
# cheaply. It is not a pass and not a failure: it is evidence, reported.
observed() { UX_FORCED="OBSERVED"; note "OBSERVED: $*"; printf '%s\n' "$*" > "$UX_DIR/verdict-note"; }
skipped()  { UX_FORCED="SKIPPED";  note "SKIPPED: $*";  printf '%s\n' "$*" > "$UX_DIR/verdict-note"; exit 0; }

# ---------------------------------------------------------------- windows

# ux_launch opens another window onto the same brain, from the same UX_ENV the
# runner built, so a relaunch is a relaunch and not a different program.
ux_launch() {
  local sess="$1"; shift
  local extra="${*:-}"
  tmux kill-session -t "$sess" 2>/dev/null
  # $UX_CHAT_ARGS carries the surface (empty for v1, --v2 for v2), so a window
  # a journey opens for itself is the same surface the runner opened.
  tmux new-session -d -s "$sess" -x "$UX_WIDTH" -y "$UX_HEIGHT" \
    "env $UX_ENV $extra '$UX_BIN' chat $UX_CHAT_ARGS; echo AFORGE-EXITED; sleep 900"
  sleep 8
  note "launched window $sess ($UX_SURFACE)"
}

ux_kill() { tmux kill-session -t "$1" 2>/dev/null; note "killed window $1"; }

# ---------------------------------------------------------------- hygiene

# dismiss_standing_watch answers the "should I keep watching when you're not
# here" offer with NO. Saying yes installs a launchd agent on the real machine,
# and a test suite does not get to leave a daemon behind.
dismiss_standing_watch() {
  local open
  local waited=0
  while [ "$waited" -lt 20 ]; do
    open="$(journal "select count(*) from agent_questions where status in ('pending','asked') and lower(text) like '%keep watching%'")"
    [ "${open:-0}" != "0" ] && break
    sleep 2; waited=$((waited + 2))
  done
  if [ "${open:-0}" != "0" ]; then
    note "standing-watch offer is open — declining (a suite must not install a daemon)"
    answer_number 2
    sleep 5
  else
    note "no standing-watch offer open; nothing to decline"
  fi
  clear_composer
}

# settle waits for the resident to go quiet: no journal growth for a few polls.
settle() {
  local limit="${1:-30}" last="" now waited=0 stable=0
  while [ "$waited" -lt "$limit" ]; do
    now="$(mark)"
    if [ "$now" = "$last" ]; then
      stable=$((stable + 1))
      [ "$stable" -ge 3 ] && return 0
    else
      stable=0
    fi
    last="$now"; sleep 2; waited=$((waited + 2))
  done
  return 0
}

dump_turn() {
  local since="$1"
  {
    echo; echo '### messages since the mark'
    journal_table "select seq,role,node_id,command_seq,substr(replace(body,char(10),' / '),1,220) as body from messages where seq > $since order by seq"
    echo; echo '### commands since the mark'
    journal_table "select seq,kind,status,target,substr(instruction,1,70) as instruction,substr(result,1,70) as result from commands where seq > $since order by seq"
    echo; echo '### nodes since the mark'
    journal_table "select id,parent_id,status,origin,grp,substr(title,1,50) as title from nodes where created_seq > $since order by created_seq"
    echo; echo '### usage since the mark'
    journal_table "select model,count(*) as calls,round(sum(cost),5) as cost from usage where seq > $since group by model"
  } >> "$UX_DIR/notes.md"
}

# finish is the last line of every journey.
finish() {
  local verdict
  if [ -n "$UX_FORCED" ]; then
    verdict="$UX_FORCED"
  elif [ "$UX_CHECKS" -eq 0 ]; then
    verdict="OBSERVED"
  elif [ "$UX_FAILURES" -eq 0 ]; then
    verdict="PASS"
  else
    verdict="FAIL"
  fi
  printf '%s\n' "$verdict" > "$UX_DIR/verdict"
  printf '%s/%s checks\n' "$((UX_CHECKS - UX_FAILURES))" "$UX_CHECKS" > "$UX_DIR/checks"
  printf '\n**verdict: %s** (%d/%d checks)\n' "$verdict" "$((UX_CHECKS - UX_FAILURES))" "$UX_CHECKS" >> "$UX_DIR/notes.md"
  printf '  \033[1m%s\033[0m — %d/%d checks\n' "$verdict" "$((UX_CHECKS - UX_FAILURES))" "$UX_CHECKS" >&2
  [ "$verdict" = "FAIL" ] && exit 1
  exit 0
}
