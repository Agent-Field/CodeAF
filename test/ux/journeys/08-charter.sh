#!/usr/bin/env bash
# J8 · Stand up a rule
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J8 Stand up a rule — charter drafted and priced, ratified with one answer, and stood down just as sayably'

expect_nodes 0

since="$(mark)"

say "remind me every day at 9am to stretch"

assert_screen 'fires:' 'the drafted charter shows its cadence' 120
assert_screen 'costs:' 'the draft carries a price line — per firing and per day' 30
snap drafted
record 'price line' "$(pane | grep -i 'costs:' | head -1)"

assert_journal "select count(*) from charters where created_seq > $since" \
  'a charter row was written' 60
charter="$(journal "select id from charters where created_seq > $since order by created_seq limit 1")"
record 'charter' "$charter"
assert_journal_is "select status from charters where id='$charter'" 'proposed' \
  'the charter starts proposed, not active — nothing stands up unasked'

# Answer only once the question is actually open in the journal. A digit typed
# at a draft that has not yet been asked lands in the composer instead, and the
# journey would then be measuring the suite's timing rather than the product.
assert_journal "select count(*) from agent_questions where seq > $since and status in ('pending','asked')" \
  'the ratification question is open' 120
qseq="$(journal "select seq from agent_questions where seq > $since and status in ('pending','asked') order by seq limit 1")"
answer_number 1
wait_journal "select count(*) from agent_questions where seq=$qseq and status='answered'" 60
sleep 8
snap ratified
record 'ratification question' "$(journal "select status || ' → ' || resolution from agent_questions where seq=$qseq")"

assert_journal_is "select status from charters where id='$charter'" 'active' \
  'ratifying with one answer made the charter active'
assert_journal "select count(*) from commands where seq > $since and kind='charter_ratify' and status='applied'" \
  'the ratification is journaled as an applied command'
record 'next due' "$(journal "select next_due from charters where id='$charter'")"

dismiss_standing_watch

# Stand-down is the other half of the journey, and it is said, not navigated.
down_at="$(mark)"
say "stand down the stretch reminder"
sleep 25
snap stood-down

status="$(journal "select status from charters where id='$charter'")"
record 'status after stand-down' "$status"
record 'commands after stand-down' "$(journal "select group_concat(kind || '/' || status, ', ') from commands where seq > $down_at")"
record 'reply to stand-down' "$(journal "select group_concat(substr(body,1,200),' | ') from messages where seq > $down_at and role in ('agent','system')")"

if [ "$status" = "retired" ] || [ "$status" = "paused" ]; then
  _check yes 'saying "stand down" retired the charter in the journal' \
    "charters.status in (retired,paused)" "$status"
else
  _check no 'saying "stand down" retired the charter in the journal' \
    "charters.status in (retired,paused)" "$status — the thread may claim it, but the journal does not"
fi

dump_turn "$since"
finish
