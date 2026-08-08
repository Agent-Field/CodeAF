#!/usr/bin/env bash
# J4 · Steer running work
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J4 Steer running work — words reach running workers; words arriving after landing are called out, not eaten'

since="$(mark)"

say "write a 6-verse poem about mountains, one verse at a time, into a file"
sleep 12   # let the splice land and a worker start
snap running

steer_at="$(mark)"
say "make it about the sea instead"

assert_journal "select count(*) from commands where seq > $steer_at and kind in ('redirect','steer')" \
  'a redirect command was journaled' 90

status="$(journal "select status from commands where seq > $steer_at and kind in ('redirect','steer') order by seq limit 1")"
result="$(journal "select result from commands where seq > $steer_at and kind in ('redirect','steer') order by seq limit 1")"
record 'redirect status' "${status:-<none>}"
record 'redirect result' "${result:-<none>}"

sleep 20

# Only the head's own reply to the steer is evidence. The deliverable that
# lands afterwards is not: a poem is free to contain the word "sea" without
# anyone having steered anything, and grading on that would let the journey
# pass on a coincidence.
reply="$(journal "select lower(body) from messages where seq > $steer_at and role='agent' order by seq limit 1")"
record 'the head'"'"'s reply to the steer' "${reply:-<nothing>}"
record 'everything said afterwards' "$(journal "select group_concat(substr(body,1,120),' | ') from messages where seq > $steer_at and role in ('agent','system')" | head -c 600)"

if printf '%s' "$reply" | grep -Eq "couldn'?t queue|could not queue|try again"; then
  _check no 'the redirect produced a reply saying what happened to the words' \
    'either "passed to the workers" or an honest too-late line' \
    "the generic command-failure line: \"$reply\""
  record 'outcome' 'REFUSED — the head returned its generic "I couldn'"'"'t queue that change — try again" instead of saying the work had already landed'
elif printf '%s' "$reply" | grep -Eq 'passed|reached|worker|redirect|steer|now working|carrying it|took it'; then
  _check yes 'the redirect landed and one reply says what was done with the words' \
    'a reply naming where the words went' "$reply"
  record 'outcome' 'LANDED — the words reached running work'
elif printf '%s' "$reply" | grep -Eq 'already|too late|finished|had landed|completed before|done before|by the time'; then
  _check yes 'the redirect arrived after landing and was called out, not eaten' \
    'an honest too-late line' "$reply"
  record 'outcome' 'TOO LATE — called out honestly (the mechanism works either way)'
else
  _check no 'the redirect produced a reply saying what happened to the words' \
    'either "passed to the workers" or an honest too-late line' "${reply:-<no reply at all>}"
  record 'outcome' 'SILENT — the cardinal UX sin'
fi

assert_journal "select count(*) from messages where seq > $steer_at and role in ('agent','system')" \
  'the steer was not eaten in silence'

settle 60
snap after
dump_turn "$since"
finish
