#!/usr/bin/env bash
# Q4 · Does what it learned change what it does?
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'Q4 Learning applied — the lesson taught in J7 ("run what you build before saying it is done") must show up as behaviour, not as a stored string'

lesson="$(journal "select group_concat(substr(body,1,120), ' | ') from facts where status='active' and (lower(body) like '%run%' or lower(body) like '%verif%')")"
record 'the lesson on the shelf' "${lesson:-<the notebook has no such lesson>}"
[ -n "$lesson" ] \
  && _check yes 'the lesson is still in the notebook when the next job starts' 'an active fact about running/verifying' "$(printf '%s' "$lesson" | head -c 160)" \
  || _check no 'the lesson is still in the notebook when the next job starts' 'an active fact about running/verifying' 'nothing'

expect_nodes 1
since="$(mark)"
# Deliberately NOT asking it to run anything. If the lesson landed, it runs the
# script anyway and shows the output. If it only stored a sentence, it will
# hand back a file and call it done.
say "write a python script called fizz.py that prints fizzbuzz for 1 to 15"

assert_journal "select count(*) from nodes where created_seq > $since and origin='user' and status='done'" \
  'the job finished' 360
sleep 12
snap delivered

claim="$(journal "select group_concat(body,char(10)) from messages where seq > $since and role in ('agent','system')")"
printf '%s\n' "$claim" > "$UX_DIR/claim.txt"
record 'what it said' "$(printf '%s' "$claim" | tr '\n' ' ' | head -c 800)"

# Evidence of having actually run it: real fizzbuzz output in the deliverable.
if printf '%s' "$claim" | grep -qi 'fizzbuzz' && printf '%s' "$claim" | grep -qi 'buzz' && printf '%s' "$claim" | grep -q '\b11\b'; then
  _check yes 'it ran what it built and showed the output — the lesson changed behaviour' \
    'real program output in the deliverable' 'fizzbuzz output present'
  record 'LEARNING' 'APPLIED — the taught lesson visibly changed how the next job was reported'
else
  _check no 'it ran what it built and showed the output — the lesson changed behaviour' \
    'real program output in the deliverable' "$(printf '%s' "$claim" | tr '\n' ' ' | head -c 200)"
  record 'LEARNING' 'NOT APPLIED — the lesson is stored but the next job did not honour it'
fi

# And the file itself has to be right.
script="$(find "$UX_STATE/workspace" -name 'fizz*.py' 2>/dev/null | head -1)"
record 'script' "${script:-<none>}"
if [ -n "$script" ]; then
  out="$(python3 "$script" 2>&1 | tr '\n' ' ')"
  record 'suite ran it' "$(printf '%s' "$out" | head -c 200)"
  # Case is not the contract — the sequence is. 3 and 15 must be fizz-ish,
  # 5 must be buzz-ish, 15 must be the combined word.
  if printf '%s' "$out" | grep -qi 'fizzbuzz' && printf '%s' "$out" | grep -qi 'fizz' \
     && printf '%s' "$out" | grep -qi 'buzz' && printf '%s' "$out" | grep -q '11'; then
    _check yes 'the script is correct when the suite runs it' 'fizzbuzz through 15' "$(printf '%s' "$out" | head -c 120)"
  else
    _check no 'the script is correct when the suite runs it' 'fizzbuzz through 15' "$(printf '%s' "$out" | head -c 200)"
  fi
else
  _check no 'the script exists on disk' 'a fizz*.py in the workspace' 'not found'
fi

judge_this 'write a python script called fizz.py that prints fizzbuzz for 1 to 15' "$(deliverable_since "$since")"

dump_turn "$since"
finish
