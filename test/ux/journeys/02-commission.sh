#!/usr/bin/env bash
# J2 · Commission work
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J2 Commission work — "build me X" → work order journaled, tasks on the board, deliverable lands in the thread answer-first'

since="$(mark)"

say "write a haiku about rivers into a file and show me the haiku"

# The deliverable is answer-first: the words, not a path to go and read.
assert_journal "select count(*) from commands where seq > $since and kind='splice' and status='applied'" \
  'a splice command was journaled and applied' 60
assert_journal "select count(*) from nodes where created_seq > $since and origin='user' and status='done'" \
  'the commissioned node reached done' "$UX_JOB_TIMEOUT"

node="$(journal "select id from nodes where created_seq > $since and origin='user' order by created_seq limit 1")"
record 'task node' "${node:-<none>}"
record 'nodes formed' "$(journal "select count(*) from nodes where created_seq > $since and origin='user'")"

assert_journal "select count(*) from messages where seq > $since and role in ('agent','system') and lower(body) like '%river%' and length(body) > 40" \
  'the final message carries the haiku itself, not only a path' 60
assert_screen 'river' 'the thread shows the haiku on screen' 30
snap delivered

# Nothing else happened: no question left dangling, no failed node.
assert_journal_is "select count(*) from agent_questions where seq > $since and status in ('pending','asked')" 0 \
  'no orphaned question was left open'
assert_journal_is "select count(*) from nodes where created_seq > $since and status='failed'" 0 \
  'no node failed'

# Keep the haiku's file path for the journeys that correct it later.
journal "select group_concat(body, char(10)) from messages where seq > $since and role in ('agent','system')" \
  > "$UX_DIR/deliverable.txt"

dump_turn "$since"
finish
