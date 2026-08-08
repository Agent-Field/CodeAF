#!/usr/bin/env bash
# J3 · Watch progress
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J3 Watch progress — "how'"'"'s it going" is a read of the live graph, never a refusal'

since="$(mark)"

say "did you finish the haiku?"
assert_screen 'aforge' 'the head replied at all' 90
sleep 6
snap replied

reply="$(journal "select group_concat(lower(body), ' ') from messages where seq > $since and role='agent'")"
record 'reply' "$(printf '%s' "$reply" | head -c 400)"

# The read must reflect the graph. The haiku node is done, so the honest answer
# says so — and must never be a refusal to look.
if printf '%s' "$reply" | grep -Eq 'done|finish|complete|wrote|written|created|yes'; then
  _check yes 'the status read reflects the finished haiku' 'a reply naming the work as done' "$(printf '%s' "$reply" | head -c 160)"
else
  _check no 'the status read reflects the finished haiku' 'a reply naming the work as done' "$(printf '%s' "$reply" | head -c 160)"
fi

if printf '%s' "$reply" | grep -Eq "i can'?t|cannot|unable to|don'?t have access|no access|i do not have"; then
  _check no 'status is a read, not a refusal' 'no refusal language' "$(printf '%s' "$reply" | head -c 160)"
else
  _check yes 'status is a read, not a refusal' 'no refusal language' 'none found'
fi

assert_journal "select count(*) from messages where seq > $since and role='agent'" \
  'the question got exactly one reply in the thread'

dump_turn "$since"
finish
