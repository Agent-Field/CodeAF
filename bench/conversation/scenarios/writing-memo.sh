#!/usr/bin/env bash
# writing-memo — the writing workload, through the print door.
#
# Writing is the workload where a benchmark most easily fools itself: any fluent
# paragraph looks like success. So the cell asks for a deliverable with
# constraints that are countable — a file, a length ceiling, three facts that
# must appear, one word that must not, and an audience that rules out jargon.
# Every check is a fact about the file on disk. Whether the prose is any good is
# not claimed here, and the README says so.

SCENARIO_WORKLOAD="writing"
SCENARIO_DOOR="print"
SCENARIO_ARMS="aforge omp pi opencode"
SCENARIO_CAP_S="${SCENARIO_CAP_S:-420}"
SCENARIO_GUARDS="a constrained deliverable on disk, not a fluent reply"

# shellcheck source=../fixtures/corpus.sh
source "$CONV_ROOT/fixtures/corpus.sh"

scenario_fixture() { fixture_corpus "$1"; }

scenario_prompt() {
  cat <<'TXT'
Using the notes in notes/, write a short handover memo for whoever is on call
next weekend. Save it as memo.md in this directory.

Requirements, all of them:
- at most 150 words
- name the team that currently owns kestrel, kestrel's port, and the rollback
  command
- plain language for someone who has not worked on this service: no jargon, and
  do not use the word "synergy"
- end with a single line beginning "Escalation:" that says where escalation goes
TXT
}

scenario_check() {
  local work="$1" reply="$2"
  local memo="$work/memo.md"
  check_file "memo.md was written" "$memo"
  [ -s "$memo" ] || return 0

  local words; words="$(wc -w < "$memo" | tr -d ' ')"
  check_lt "memo is within the 150-word ceiling" 151 "$words"
  check_grep_all "memo carries the three required facts" "$memo" \
    "BRACKISH" "8431" "kestrelctl rollback"
  check_not_grep "memo avoids the banned word" "synergy" "$memo"
  check_grep "memo ends with an escalation line" "^ *Escalation:" "$memo"
  # Escalation goes to the duty lead, which is in ownership.md and is the one
  # fact a memo written from the incident log alone would miss.
  check_grep "escalation names the duty lead" "duty lead" "$memo"
  record "memo_words" "$words"
  record "reply_chars" "$(wc -c < "$reply" | tr -d ' ')"
}
