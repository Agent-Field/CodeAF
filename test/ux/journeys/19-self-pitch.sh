#!/usr/bin/env bash
# J19 · Ask what it can do
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J19 Ask what it can do — the product carries its own pitch: journeys, in its own voice, from live capabilities'

expect_nodes 0
since="$(mark)"
say "what can you do?"
assert_journal "select count(*) from messages where seq > $since and role='agent'" 'the head replied' 150
sleep 8
snap answered

reply="$(journal "select group_concat(lower(body),' ') from messages where seq > $since and role in ('agent','system')")"
record 'reply' "$(printf '%s' "$reply" | head -c 900)"

length="$(printf '%s' "$reply" | wc -c | tr -d ' ')"
record 'reply length (bytes)' "$length"
[ "${length:-0}" -gt 150 ] \
  && _check yes 'the pitch has substance' 'more than a sentence' "$length bytes" \
  || _check no 'the pitch has substance' 'more than a sentence' "$length bytes"

# It must describe the journeys, not itself in the abstract.
hits=0
for word in task work file build remember learn charter remind cost watch run graph question; do
  printf '%s' "$reply" | grep -q "$word" && hits=$((hits + 1))
done
record 'capability words named' "$hits of 13"
[ "$hits" -ge 4 ] \
  && _check yes 'the answer names actual capabilities, not vibes' 'at least 4 capability words' "$hits" \
  || _check no 'the answer names actual capabilities, not vibes' 'at least 4 capability words' "$hits"

if printf '%s' "$reply" | grep -Eq "i'?m an ai|language model|as an ai|i can'?t do|i have no"; then
  _check no 'it pitches the product, not a disclaimer' 'no assistant boilerplate' "$(printf '%s' "$reply" | grep -Eo "i'?m an ai|language model|as an ai" | head -1)"
else
  _check yes 'it pitches the product, not a disclaimer' 'no assistant boilerplate' 'none'
fi

dump_turn "$since"
finish
