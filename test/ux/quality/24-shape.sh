#!/usr/bin/env bash
# Q5 · Is the decomposition any good?
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'Q5 Graph shape — across everything the suite commissioned: how many tasks did it form, and was that the right number?'

expect_nodes 0

# Nothing is said in this journey. It reads the whole run's graph and judges the
# one thing a task graph can get obviously wrong in both directions: a haiku
# that became eleven tasks, or a research report that became none.

record 'user jobs commissioned' "$(journal "select count(*) from nodes where origin='user' and parent_id='root'")"
record 'total nodes' "$(journal "select count(*) from nodes")"
record 'failed nodes' "$(journal "select count(*) from nodes where status='failed'")"
record 'cancelled nodes' "$(journal "select count(*) from nodes where status='cancelled'")"
record 'max depth below root' "$(journal "select coalesce(max(stage),0) from nodes")"

{
  echo
  echo '### every job the suite commissioned, and what it became'
  echo
  journal_table "
    select
      n.id as job,
      n.status,
      (select count(*) from nodes c where c.parent_id = n.id) as children,
      (select count(*) from usage u where u.node_id = n.id) as calls,
      round((select coalesce(sum(cost),0) from usage u where u.node_id = n.id), 5) as cost,
      substr(n.title,1,44) as title
    from nodes n
    where n.origin='user' and n.parent_id='root'
    order by n.created_order"
} >> "$UX_DIR/notes.md"

# The other half of proportionality, and the more interesting one: how often
# did a job branch AT ALL? A graph product whose every job is one leaf is not
# over-decomposing, but it is also not decomposing, and a reader of this report
# should be told which of those two things is happening.
branched="$(journal "
  select count(*) from nodes n
  where n.origin='user' and n.parent_id='root'
    and (select count(*) from nodes c where c.parent_id = n.id) > 0")"
total_jobs="$(journal "select count(*) from nodes where origin='user' and parent_id='root'")"
record 'jobs that decomposed into children at all' "$branched of $total_jobs"

bloated="$(journal "
  select count(*) from nodes n
  where n.origin='user' and n.parent_id='root'
    and (select count(*) from nodes c where c.parent_id = n.id) > 6")"
record 'jobs that fanned out past 6 children' "$bloated"
[ "${bloated:-0}" = "0" ] \
  && _check yes 'no toy job exploded into a bureaucracy' 'no user job with more than 6 children' "$bloated" \
  || _check no 'no toy job exploded into a bureaucracy' 'no user job with more than 6 children' "$bloated jobs did"

# Held work is not forgotten work: J9 refuses a plan on purpose and the node
# that stays pending behind that refusal is the refusal being honoured.
orphans="$(journal "select count(*) from nodes where origin='user' and status in ('pending','claimed') and held=0 and created_seq < (select max(seq) - 200 from events)")"
record 'jobs still stuck pending long after they were asked for' "$orphans"
record 'jobs deliberately held (a refused plan is not a forgotten one)' \
  "$(journal "select count(*) from nodes where origin='user' and held=1")"
[ "${orphans:-0}" = "0" ] \
  && _check yes 'nothing was commissioned and then quietly forgotten' 'no stale pending user job' "$orphans" \
  || _check no 'nothing was commissioned and then quietly forgotten' 'no stale pending user job' "$orphans stuck"

failed="$(journal "select count(*) from nodes where origin='user' and status='failed'")"
[ "${failed:-0}" = "0" ] \
  && _check yes 'no commissioned job failed outright' 'zero failed user nodes' "$failed" \
  || _check no 'no commissioned job failed outright' 'zero failed user nodes' "$(journal "select group_concat(id || ': ' || substr(error,1,60), ' | ') from nodes where origin='user' and status='failed'")"

# Every user turn must have ended in a reply. Silence wearing a message id is
# the cardinal sin in docs/JOURNEY.md, so it gets its own count.
users="$(journal "select count(*) from messages where role='user'")"
agents="$(journal "select count(*) from messages where role in ('agent','system')")"
record 'user turns / agent+system replies' "$users / $agents"
[ "${agents:-0}" -ge "${users:-1}" ] \
  && _check yes 'every user turn was answered — no silence wearing a message id' 'replies ≥ user turns' "$agents ≥ $users" \
  || _check no 'every user turn was answered — no silence wearing a message id' 'replies ≥ user turns' "$agents < $users"

# The thread is the only mouth, so a journaled agent message that draws as a
# bare stream cursor is silence wearing a message id — the exact sin the law
# names. One live cursor is a reply in flight; a screen full of them is not.
snap final-thread
if is_v2; then
  # v2 draws no stream cursor at all: a turn in flight is a titled block
  # ("aforge", poll.go:588) with an awaiting line beneath it, so counting bare
  # ▌ rows would be a check that can only pass and would say nothing. The law
  # is the same law, so v2 measures it where v2 can be caught: a journaled
  # agent message whose body is empty IS silence wearing a message id,
  # whatever the surface does with it.
  blanks="$(journal "select count(*) from messages where role in ('agent','system') and trim(body) = ''")"
  record 'journaled agent messages with an empty body' "${blanks:-0}"
  [ "${blanks:-0}" = "0" ] \
    && _check yes 'agent messages carry words, not an empty message id' 'no agent message with an empty body' "${blanks:-0}" \
    || _check no 'agent messages carry words, not an empty message id' 'no agent message with an empty body' "$blanks empty bodies — journaled replies that said nothing"
else
  cursors="$(pane | grep -c '^[[:space:]]*▌[[:space:]]*$')"
  record 'thread rows that are a bare stream cursor with no words' "$cursors"
  [ "${cursors:-0}" -le 1 ] \
    && _check yes 'agent messages render their words, not an empty stream cursor' 'at most one live cursor on screen' "$cursors" \
    || _check no 'agent messages render their words, not an empty stream cursor' 'at most one live cursor on screen' "$cursors bare cursors — journaled replies are drawing blank"
fi

{
  echo
  echo '### where the money went'
  echo
  journal_table "select model, count(*) as calls, sum(prompt_tokens) as prompt_tok, sum(completion_tokens) as out_tok, round(sum(cost),5) as cost from usage group by model order by cost desc"
  echo
  echo '### the notebook at the end of the run'
  echo
  journal_table "select kind, channel, status, substr(body,1,90) as body from facts order by seq"
  echo
  echo '### charters at the end of the run'
  echo
  journal_table "select id, status, autonomy, substr(invariant,1,60) as invariant from charters"
} >> "$UX_DIR/notes.md"

finish
