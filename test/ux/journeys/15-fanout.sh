#!/usr/bin/env bash
# J15-fanout · Parallelism lives in the graph, not in terminal tabs
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'Fan-out — "say five things and five jobs run": one message asking for three independent things must become three jobs, not one queue'

# docs/JOURNEY.md: "Parallelism lives in the graph, not in terminal tabs: say
# five things and five jobs run; the board shows them side by side." Three is
# enough to tell splitting from serialising, and cheap enough to say twice.
expect_nodes 3

since="$(mark)"
started="$(date +%s)"

say "three separate things please: write a file with the first 5 prime numbers, write a file with the capitals of France Japan and Peru, and write a file with a two-line poem about rain"

assert_journal "select count(*) from commands where seq > $since and kind='splice' and status='applied'" \
  'the ask was journaled as work' 90

# The fan-out: three independent jobs, not one node doing three things.
wait_journal "select case when count(*) >= 3 then 1 else 0 end from nodes where created_seq > $since and origin='user'" 240
jobs="$(journal "select count(*) from nodes where created_seq > $since and origin='user'")"
record 'jobs formed from one sentence' "$jobs"
record 'job titles' "$(journal "select group_concat(title, ' · ') from nodes where created_seq > $since and origin='user'")"

if [ "${jobs:-0}" -ge 3 ]; then
  _check yes 'one sentence naming three things became three jobs' 'three or more user jobs' "$jobs"
else
  _check no 'one sentence naming three things became three jobs' 'three or more user jobs' \
    "$jobs — the fan-out did not split the ask"
fi

wait_journal "select case when count(*) = 0 then 1 else 0 end from nodes where created_seq > $since and origin='user' and status not in ('done','failed','cancelled')" "$UX_JOB_TIMEOUT"
wall=$(( $(date +%s) - started ))
sleep 10
snap delivered

assert_journal_is "select count(*) from nodes where created_seq > $since and origin='user' and status != 'done'" 0 \
  'every one of the jobs finished'

# Concurrency factor: the work the graph did, divided by the time the person
# waited. At 1.0 the jobs were a queue with extra steps; above it, the graph is
# the parallelism the product promises instead of terminal tabs.
busy="$(journal "
  select cast(coalesce(sum(
    (julianday(finished_at) - julianday(started_at)) * 86400.0), 0) as int)
  from nodes where created_seq > $since and origin='user'
    and started_at is not null and finished_at is not null")"
record 'wall time the person waited' "${wall}s"
record 'summed job time the graph did' "${busy}s"
factor="$(python3 -c "
wall = max(1.0, float('${wall:-1}'))
print('%.2f' % (float('${busy:-0}') / wall))")"
record 'CONCURRENCY FACTOR (summed job seconds / wall seconds)' "$factor"
printf '%s\n' "$factor" > "$UX_DIR/concurrency"

if python3 -c "import sys; sys.exit(0 if float('$factor') >= 1.3 else 1)"; then
  _check yes 'the jobs actually ran side by side' 'concurrency factor at or above 1.3' "$factor"
else
  _check no 'the jobs actually ran side by side' 'concurrency factor at or above 1.3' \
    "$factor — the graph held three jobs but ran them one after another"
fi

# Each ask must be answered on its own terms, not merged into one blob.
answers="$(journal "select group_concat(lower(body), char(10)) from messages where seq > $since and role in ('agent','system')")"
for want in 'prime' 'lima\|peru\|tokyo\|paris' 'rain'; do
  if printf '%s' "$answers" | grep -Eqi "$want"; then
    _check yes "the thread answers the '$want' part of the ask" "a deliverable mentioning $want" 'found'
  else
    _check no "the thread answers the '$want' part of the ask" "a deliverable mentioning $want" 'absent'
  fi
done

judge_this 'three separate things: a file with the first 5 primes, a file with the capitals of France Japan and Peru, and a file with a two-line poem about rain' \
  "$(journal "select group_concat(body, char(10)) from messages where seq > $since and node_id != ''")"

dump_turn "$since"
finish
