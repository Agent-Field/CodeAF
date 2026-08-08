#!/usr/bin/env bash
# J11 · Ask what it learned
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J11 Ask what it learned — a read over the notebook: lessons, beliefs, retractions'

since="$(mark)"
record 'notebook holds' "$(journal "select count(*) from facts where status='active'") active facts"

say "what have you learned about how I like work done?"
assert_screen 'aforge' 'the head replied' 120
sleep 8
snap answered

reply="$(journal "select group_concat(lower(body),' ') from messages where seq > $since and role in ('agent','system')")"
record 'reply' "$(printf '%s' "$reply" | head -c 600)"

# The proof it read the notebook rather than improvising is that J7's lesson —
# run what you build before calling it done — comes back in substance.
if printf '%s' "$reply" | grep -Eq 'run |verif|before .*done|test'; then
  _check yes "the answer draws on J7's lesson in substance" \
    'the run-it-before-done lesson, in its own words' "$(printf '%s' "$reply" | head -c 220)"
else
  _check no "the answer draws on J7's lesson in substance" \
    'the run-it-before-done lesson, in its own words' "$(printf '%s' "$reply" | head -c 220)"
fi

if printf '%s' "$reply" | grep -Eq "i haven'?t learned|nothing yet|no lessons|i don'?t have|cannot recall"; then
  _check no 'it does not refuse a question about itself' 'a substantive answer' "$(printf '%s' "$reply" | head -c 200)"
else
  _check yes 'it does not refuse a question about itself' 'a substantive answer' 'no refusal'
fi

length="$(journal "select coalesce(max(length(body)),0) from messages where seq > $since and role in ('agent','system')")"
record 'longest reply (bytes)' "$length"
[ "${length:-0}" -gt 60 ] \
  && _check yes 'the answer has substance' 'more than a shrug' "$length bytes" \
  || _check no 'the answer has substance' 'more than a shrug' "$length bytes"

dump_turn "$since"
finish
