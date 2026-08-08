#!/usr/bin/env bash
# J5 · Correct delivered work
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J5 Correct delivered work — "that'"'"'s wrong" spawns a revision beside the original, inheriting its workspace'

since="$(mark)"

say "that haiku is wrong — the haiku must mention a heron. fix it and show me the new haiku"

# Either shape counts as the mechanism: a surgery command against the original,
# or a correction node spliced beside it. The report says which one landed.
assert_journal "select count(*) from commands where seq > $since and kind in ('splice','revise','surgery','correct','redirect')" \
  'a correction command was journaled' 90

kind="$(journal "select group_concat(distinct kind) from commands where seq > $since")"
record 'command kinds' "${kind:-<none>}"

assert_journal "select count(*) from nodes where created_seq > $since" \
  'a revision node was created beside the original' 90

parent="$(journal "select parent_id || ' / retry_of=' || retry_of from nodes where created_seq > $since order by created_seq limit 1")"
record 'revision node lineage' "${parent:-<none>}"

assert_journal "select count(*) from nodes where created_seq > $since and status='done'" \
  'the revision reached done' "$UX_JOB_TIMEOUT"

assert_journal "select count(*) from messages where seq > $since and role in ('agent','system') and lower(body) like '%heron%'" \
  'the new deliverable mentions a heron' 90

# The word "heron" is on screen the moment the user types it, so the screen
# check has to exclude the user's own sentence or it grades itself.
snap corrected
if pane | grep -i heron | grep -qvi 'must mention'; then
  _check yes 'the corrected haiku is on screen, in the agent'"'"'s words not the user'"'"'s' \
    "a heron line that is not the user's own request" "$(pane | grep -i heron | grep -vi 'must mention' | head -1 | sed 's/^ *//')"
else
  _check no 'the corrected haiku is on screen, in the agent'"'"'s words not the user'"'"'s' \
    "a heron line that is not the user's own request" "only the user's own sentence mentions a heron"
fi

record 'the head'"'"'s reply' "$(journal "select group_concat(substr(body,1,200),' | ') from messages where seq > $since and role in ('agent','system')" | head -c 600)"

dump_turn "$since"
finish
