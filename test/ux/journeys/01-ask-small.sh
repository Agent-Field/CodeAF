#!/usr/bin/env bash
# J1 · Ask a small thing
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J1 Ask a small thing — "what'"'"'s 2^32?" → direct answer in thread, no job ceremony'

expect_nodes 0
since="$(mark)"

say "what is 2 to the power 10?"
assert_screen '1024' 'the thread answers with the number' 90
snap answered

# No job ceremony: a small question must not become a work order.
assert_journal_is "select count(*) from commands where seq > $since and kind='splice'" 0 \
  'no splice command — a question is not a commission'
assert_journal_is "select count(*) from nodes where created_seq > $since and origin='user'" 0 \
  'no user-origin task node was created'
assert_journal_is "select count(*) from messages where seq > $since and role='agent'" 1 \
  'exactly one agent message for the turn'
assert_journal "select count(*) from messages where seq > $since and role='agent' and body like '%1024%'" \
  'the durable agent message holds the answer'

dump_turn "$since"
finish
