#!/usr/bin/env bash
# J14 · Repeat a workflow — EXPLORATORY
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J14 Repeat a workflow — craft learned from the first run compiles into the second'

# This one is reported, not graded. Craft is forged by the distiller only when
# the model judges a job SHAPE reusable, and the prompt that asks it says
# outright that "most jobs have none" (cmd/aforge/chat.go, the distiller's
# craft clause). Two toy jobs are exactly the case it is told to decline. The
# thresholds on the other side — recognition — are constants:
#   CraftDecisiveScore    = 1.5 × craft.MatchFloor   (use a known-good craft)
#   CraftOverwhelmingScore = 4  × craft.MatchFloor   (use an unproven draft)
# in internal/resident/craftmind.go. So: run the two jobs, look, say what was
# actually there.
observed 'craft forging is a model judgment with an explicit "most jobs have none" instruction; two toy jobs are reported, not graded'

since="$(mark)"
craft_dir="$UX_STATE/craft"

before_commits="$(git -C "$craft_dir" rev-list --count HEAD 2>/dev/null || echo 0)"
record 'craft repo commits before' "$before_commits"

say "make a file listing 3 prime numbers"
wait_journal "select count(*) from nodes where created_seq > $since and origin='user' and status='done'" 240
first_done="$(mark)"
sleep 20
snap first-job

say "make a file listing 3 square numbers"
wait_journal "select count(*) from nodes where created_seq > $first_done and origin='user' and status='done'" 240
sleep 30
snap second-job

after_commits="$(git -C "$craft_dir" rev-list --count HEAD 2>/dev/null || echo 0)"
record 'craft repo commits after' "$after_commits"
record 'craft repo log' "$(git -C "$craft_dir" log --oneline -10 2>/dev/null | tr '\n' ' ' || echo '<no commits>')"
record 'craft files' "$(ls "$craft_dir" 2>/dev/null | tr '\n' ' ' || echo '<empty>')"
record 'nodes carrying a craft' "$(journal "select count(*) from nodes where craft != ''")"
record 'craft named on nodes' "$(journal "select group_concat(distinct craft) from nodes where craft != ''")"

first_cost="$(journal "select round(coalesce(sum(cost),0),5) from usage where seq > $since and seq <= $first_done")"
second_cost="$(journal "select round(coalesce(sum(cost),0),5) from usage where seq > $first_done")"
first_nodes="$(journal "select count(*) from nodes where created_seq > $since and created_seq <= $first_done")"
second_nodes="$(journal "select count(*) from nodes where created_seq > $first_done")"
record 'first job' "$first_nodes nodes, \$$first_cost"
record 'second job' "$second_nodes nodes, \$$second_cost"

if [ "$after_commits" -gt "$before_commits" ]; then
  record 'OBSERVED' "craft WAS forged: $((after_commits - before_commits)) new commit(s) in the craft repo"
else
  record 'OBSERVED' 'no craft was forged from two toy jobs — consistent with the distiller being told most jobs have none'
fi

dump_turn "$since"
finish
