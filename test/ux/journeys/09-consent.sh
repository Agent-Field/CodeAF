#!/usr/bin/env bash
# J9 · Approve spend (cheap variant)
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J9 Approve spend — price shown before purchase, consent asked, refusal honored'

expect_nodes 1

# The real journey needs a plan estimated over $3, which is exactly the money a
# test suite may not spend. AFORGE_PLAN_CONSENT is the same gate with a movable
# line, so the cheap variant drops the line to a cent and asks the identical
# question about a job costing cents. Same code path, 1/300th of the money.
note 'variant: AFORGE_PLAN_CONSENT lowered to $0.0002 — below the estimate of a cents-scale job, so the same gate fires'

ux_kill "$UX_SESSION"
sleep 2
# A cent is still above what these jobs are estimated to cost, so the gate
# never fires at $0.01 — the line has to go below the estimate, not below the
# real $3 rail.
ux_launch "$UX_SESSION" "AFORGE_PLAN_CONSENT=0.0002"

since="$(mark)"
say "research the three most common causes of bridge collapses and write me a short report file"

assert_journal "select count(*) from agent_questions where seq > $since and category='plan-consent'" \
  'the consent question was asked before the work was bought' 150
snap asked

qseq="$(journal "select seq from agent_questions where seq > $since and category='plan-consent' order by seq limit 1")"
record 'consent question' "$(journal "select replace(substr(text,1,300),char(10),' / ') from agent_questions where seq=$qseq")"

if pane | grep -Eqi '\$[0-9]'; then
  _check yes 'the price is shown before the purchase' 'a dollar figure on screen' "$(pane | grep -Eo '\$[0-9][0-9.]*' | head -3 | tr '\n' ' ')"
else
  _check no 'the price is shown before the purchase' 'a dollar figure on screen' 'no dollar figure in the ask'
fi

held_before="$(journal "select count(*) from nodes where created_seq > $since and status in ('running','done')")"
record 'nodes running or done while consent is outstanding' "$held_before"

# Refuse it.
answer_number 2
sleep 12
snap refused

assert_journal_is "select status from agent_questions where seq=$qseq" 'answered' 'the refusal was recorded'
record 'resolution' "$(journal "select resolution from agent_questions where seq=$qseq")"

assert_journal "select count(*) from nodes where created_seq > $since and held=1" \
  'refusal is honored — the plan is held, not run' 30
assert_journal_is "select count(*) from nodes where created_seq > $since and status='done'" 0 \
  'nothing was executed after the refusal'
record 'hold reason' "$(journal "select group_concat(distinct error) from nodes where created_seq > $since")"

# Put the suite back on the ordinary rail for the journeys that follow.
ux_kill "$UX_SESSION"; sleep 2; ux_launch "$UX_SESSION"

dump_turn "$since"
finish
