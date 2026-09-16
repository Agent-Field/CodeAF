#!/usr/bin/env bash
# Q3 · Does asking for it quicker actually make it quicker?
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'Q3 Urgency — "quick, do not overthink" against a matched control: does the shape of the work change?'

expect_nodes 2

run_one() {
  local label="$1" sentence="$2"
  local from t0 t1
  from="$(mark)"
  t0="$(date +%s)"
  say "$sentence"
  wait_journal "select count(*) from messages where seq > $from and role in ('agent','system') and length(body) > 60" 300
  wait_journal "select count(*) from nodes where created_seq > $from and origin='user' and status in ('done','failed')" 60 >/dev/null
  t1="$(date +%s)"
  sleep 10
  QN="$(journal "select count(*) from nodes where created_seq > $from")"
  QC="$(journal "select round(coalesce(sum(cost),0),5) from usage where seq > $from")"
  QCALLS="$(journal "select count(*) from usage where seq > $from")"
  QT=$((t1 - t0))
  record "$label" "$QN nodes · $QCALLS model calls · \$$QC · ${QT}s"
  printf '%s %s %s %s' "$QN" "$QCALLS" "$QC" "$QT"
}

since="$(mark)"

control="$(run_one 'control  (no urgency words)' 'write a short explanation of what a binary search tree is into a file')"
snap control
urgent="$(run_one 'urgent   ("quick, one pass")' 'quick, one pass, do not overthink this: write a short explanation of what a hash table is into a file')"
snap urgent

read -r cn ccalls ccost ctime <<< "$control"
read -r un ucalls ucost utime <<< "$urgent"

record 'nodes    control vs urgent' "$cn vs $un"
record 'calls    control vs urgent' "$ccalls vs $ucalls"
record 'cost     control vs urgent' "\$$ccost vs \$$ucost"
record 'seconds  control vs urgent' "${ctime}s vs ${utime}s"

# The honest bar: urgency must not make the work BIGGER. Whether it makes it
# genuinely smaller on one paired sample is reported, not asserted — one pair
# is an anecdote, and the suite refuses to dress an anecdote as a measurement.
if [ "${un:-99}" -le "${cn:-0}" ]; then
  _check yes 'urgency words did not inflate the work' "urgent nodes ≤ control nodes" "$un ≤ $cn"
else
  _check no 'urgency words did not inflate the work' "urgent nodes ≤ control nodes" "$un > $cn"
fi

cheaper="$(python3 -c "print('yes' if float('$ucost') <= float('$ccost') else 'no')")"
faster="$([ "${utime:-99999}" -le "${ctime:-0}" ] && echo yes || echo no)"
record 'OBSERVED — cheaper when asked to hurry?' "$cheaper"
record 'OBSERVED — faster when asked to hurry?' "$faster"

urgency_cmd="$(journal "select group_concat(kind || '/' || status, ', ') from commands where seq > $since")"
record 'commands across both jobs' "${urgency_cmd:-<none>}"

dump_turn "$since"
finish
