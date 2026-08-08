#!/usr/bin/env bash
# J7 · Teach a lesson
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J7 Teach a lesson — a stated lesson lands in the notebook and future jobs see it'

since="$(mark)"

say "always run what you build once before telling me it's done — remember that"
assert_screen 'aforge' 'the head replied' 90
sleep 5
snap taught

# The notebook is the durable half. A stated lesson is channel=stated; the
# learning loop writes it asynchronously, so the window is generous.
assert_journal "select count(*) from facts where seq > $since" \
  'something landed in the notebook' 180

assert_journal "select count(*) from facts where seq > $since and channel='stated'" \
  'it was recorded as STATED, not inferred — the user said it out loud'
assert_journal "select count(*) from facts where seq > $since and kind in ('lesson','preference')" \
  'it was filed as a lesson or a preference'
assert_journal "select count(*) from facts where seq > $since and (lower(body) like '%run%' or lower(body) like '%verif%')" \
  'the fact body carries the substance of the lesson'

record 'facts written' "$(journal "select group_concat(kind || '/' || channel || ': ' || substr(body,1,140), char(10)) from facts where seq > $since")"

reply="$(journal "select group_concat(lower(body),' ') from messages where seq > $since and role in ('agent','system')")"
if printf '%s' "$reply" | grep -Eq "got it|noted|remember|i'?ll|will do|understood|reflected"; then
  _check yes 'the thread confirms the lesson was taken' 'a confirming reply' "$(printf '%s' "$reply" | head -c 160)"
else
  _check no 'the thread confirms the lesson was taken' 'a confirming reply' "${reply:-<silence>}"
fi

dump_turn "$since"
finish
