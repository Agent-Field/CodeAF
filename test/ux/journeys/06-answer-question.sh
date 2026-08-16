#!/usr/bin/env bash
# J6 · Answer its questions
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J6 Answer its questions — type the number; the answer appears as the user'"'"'s message and a receipt lands in the thread'

expect_nodes 0

since="$(mark)"

# The cheapest reliable question generator is a standing rule: its proposal
# always asks to be ratified.
say "remind me every morning at 8am to drink water"

assert_journal "select count(*) from agent_questions where seq > $since and status in ('asked','pending')" \
  'the agent asked a question' 120
if is_v2; then
  # 13.4 J6 + question.go:55-90: v2 dresses a question as a marker row
  # ("? waiting on you") followed by one indented row per option, glyphed with
  # the option's own number. There is no "▸" before the digit — the chevron is
  # v1's anatomy, and 5.17's ▸ means "collapsed" in v2. The doc's promise is
  # that the options are numbered, in producer order, and this asserts exactly
  # that.
  assert_screen '^ +1 [a-z0-9]' 'numbered options render in the thread' 30
  record 'question block' "$(pane | grep -n 'waiting on you' | head -1 | sed 's/^ *//')"
else
  assert_screen '▸ 1 ' 'numbered options render in the thread' 30
fi
snap asked

qseq="$(journal "select seq from agent_questions where seq > $since order by seq limit 1")"
record 'question seq' "$qseq"
record 'question' "$(journal "select replace(substr(text,1,200),char(10),' / ') from agent_questions where seq=$qseq")"

# A bare digit with an empty composer submits itself — no Enter (v1). On v2
# answer_number adds the Enter the missing binding still needs; see lib.sh.
answer_number 1
sleep 8
snap answered

assert_journal_is "select status from agent_questions where seq=$qseq" 'answered' \
  'the question is recorded answered'
assert_journal "select answer_message_seq from agent_questions where seq=$qseq" \
  'the question carries the answering message seq'

ans="$(journal "select answer_message_seq from agent_questions where seq=$qseq")"
assert_journal_is "select role from messages where seq=$ans" 'user' \
  "the answer is journaled as the user's own message"
record 'resolution' "$(journal "select resolution from agent_questions where seq=$qseq")"
record "user's message" "$(journal "select body from messages where seq=$ans")"

assert_journal "select count(*) from messages where seq > $ans and role in ('agent','system')" \
  'a receipt landed in the thread after the answer' 60
assert_screen 'standing it up|stood up|ratified|watching' 'the receipt is visible on screen' 30

dismiss_standing_watch
dump_turn "$since"
finish
