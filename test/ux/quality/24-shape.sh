#!/usr/bin/env bash
# Q5 · Is the decomposition any good?
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'Q5 Graph shape — across everything the suite commissioned: how many tasks did it form, and was that the right number?'

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

bloated="$(journal "
  select count(*) from nodes n
  where n.origin='user' and n.parent_id='root'
    and (select count(*) from nodes c where c.parent_id = n.id) > 6")"
record 'jobs that fanned out past 6 children' "$bloated"
[ "${bloated:-0}" = "0" ] \
  && _check yes 'no toy job exploded into a bureaucracy' 'no user job with more than 6 children' "$bloated" \
  || _check no 'no toy job exploded into a bureaucracy' 'no user job with more than 6 children' "$bloated jobs did"

orphans="$(journal "select count(*) from nodes where origin='user' and status in ('pending','claimed') and created_seq < (select max(seq) - 200 from events)")"
record 'jobs still stuck pending long after they were asked for' "$orphans"
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
