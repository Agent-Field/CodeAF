#!/usr/bin/env bash
# Q8 · An everyday errand, shaped the way a person actually asks
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'Q8 Errand quality — an ordinary, slightly vague household ask: does it just do it, or does it interview you first?'

# Nobody says "produce a structured artifact enumerating". They say "make me a
# packing list". The failure this catches is a product that answers a small
# errand with a clarifying question, a plan, and a preamble.

expect_nodes 1
since="$(mark)"
started="$(date +%s)"
touch "$UX_DIR/.stamp"

say "make me a packing list for a 3-day work trip and save it as a file"

assert_journal "select count(*) from nodes where created_seq > $since and origin='user' and status='done'" \
  'the errand was actually run' 300
sleep 10
snap done
elapsed=$(( $(date +%s) - started ))
record 'wall time' "${elapsed}s"

answer="$(journal "select group_concat(body, char(10)) from messages where seq > $since and role in ('agent','system')")"
printf '%s\n' "$answer" > "$UX_DIR/answer.txt"
record 'the answer' "$(printf '%s' "$answer" | tr '\n' ' ' | head -c 700)"

# It must have just done it: no question asked back for something this small.
asked="$(journal "select count(*) from agent_questions where seq > $since")"
record 'questions asked back' "$asked"
[ "${asked:-0}" = "0" ] \
  && _check yes 'a small errand is done, not interviewed' 'no clarifying question for a 3-day packing list' '0 questions' \
  || _check no 'a small errand is done, not interviewed' 'no clarifying question for a 3-day packing list' "$asked asked"

# The list itself has to be a list of things a person packs.
hits=0
for item in charger toothbrush shirt sock laptop 'passport|id' 'trousers|pants' shoes; do
  printf '%s' "$answer" | grep -qiE "$item" && hits=$((hits + 1))
done
record 'ordinary packing items named' "$hits of 8"
[ "$hits" -ge 4 ] \
  && _check yes 'the list is a real packing list' 'at least 4 of the obvious items' "$hits" \
  || _check no 'the list is a real packing list' 'at least 4 of the obvious items' "$hits"

file="$(find "$UX_STATE/workspace" -type f -newer "$UX_DIR/.stamp" 2>/dev/null | head -1)"
record 'file written' "${file:-<none>}"
[ -n "$file" ] && [ -s "$file" ] \
  && _check yes 'it saved the file it was asked to save' 'a non-empty file in the workspace' "$(wc -c < "$file" | tr -d ' ') bytes" \
  || _check no 'it saved the file it was asked to save' 'a non-empty file in the workspace' "${file:-nothing written}"

judge_this 'make me a packing list for a 3-day work trip and save it as a file' "$answer"

dump_turn "$since"
finish
