#!/usr/bin/env bash
# J2 · Commission work
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J2 Commission work — "build me X" → work order journaled, tasks on the board, deliverable lands in the thread answer-first'

expect_nodes 1
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

# "I'm on it — I'll have a haiku about rivers written to a file" also contains
# the word "rivers" and is longer than forty characters. It is a promise, not a
# deliverable, and an answer-first check that a promise can satisfy is not a
# check. So this asks for the message the finished NODE produced.
assert_journal "select count(*) from messages where seq > $since and node_id='$node' and lower(body) like '%river%'" \
  'the finished job posted its own message into the thread' 90
assert_journal "select count(*) from messages where seq > $since and node_id='$node' and length(body) > 60 and (body like '%' || char(10) || '%')" \
  'that message carries the haiku itself — several lines of it — not only a path' 30

deliverable="$(journal "select body from messages where seq > $since and node_id='$node' order by seq desc limit 1")"
record 'the deliverable message' "$(printf '%s' "$deliverable" | tr '\n' ' / ' | head -c 400)"
if printf '%s' "$deliverable" | grep -Eqi "i'?m on it|i will|i'll have|when it'?s done"; then
  _check no 'the deliverable is the work, not another promise' \
    'the haiku, not a sentence about writing one' "$(printf '%s' "$deliverable" | head -c 120)"
else
  _check yes 'the deliverable is the work, not another promise' \
    'the haiku, not a sentence about writing one' 'no promise language in the delivered message'
fi

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

judge_this 'write a haiku about rivers into a file and show me the haiku' "$(journal "select group_concat(body, char(10)) from messages where seq > $since and node_id='$node'")"

dump_turn "$since"
finish
