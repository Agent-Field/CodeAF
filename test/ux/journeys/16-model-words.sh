#!/usr/bin/env bash
# J16 · Choose models in words
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J16 Choose models in words — "boost this" routes the call and is recorded on usage'

work_model="$(python3 -c "import json;print(json.load(open('$UX_STATE/settings.json')).get('task_model',''))")"
boost_model="$(python3 -c "import json;print(json.load(open('$UX_STATE/settings.json')).get('boost_model',''))")"
record 'work model' "$work_model"
record 'boost model' "$boost_model"

expect_nodes 1
since="$(mark)"
say "use the boost model for this: write one sentence about mountains"

assert_screen 'mountain' 'the sentence came back' 150
sleep 8
snap answered

models="$(journal "select group_concat(distinct model) from usage where seq > $since")"
record 'models on usage rows for this turn' "${models:-<none>}"

boost_bare="${boost_model#\~}"
if [ -n "$models" ] && printf '%s' "$models" | grep -qF "$boost_bare"; then
  _check yes 'the words routed the call — usage names the boost model' \
    "a usage row on $boost_model" "$models"
  record 'outcome' 'ROUTED ON USAGE — the boost model appears on the turn'
elif [ -n "$models" ] && [ "$models" != "$work_model" ] && [ "$models" != "${work_model#\~}" ]; then
  _check yes 'the words routed the call — usage differs from the default work model' \
    "a usage model other than $work_model" "$models"
  record 'outcome' 'ROUTED ON USAGE — a different model than the default'
else
  cmd="$(journal "select group_concat(kind || ':' || substr(instruction,1,60), ' | ') from commands where seq > $since")"
  record 'commands this turn' "${cmd:-<none>}"
  if printf '%s' "$cmd" | grep -qi 'boost'; then
    _check yes 'the words are carried on the command even though usage kept the default model' \
      'a command carrying boost' "$cmd"
    record 'outcome' 'CARRIED ON COMMAND — the boost word is journaled but usage stayed on the work model'
  else
    _check no 'the model words reached either the usage row or the command' \
      "$boost_model on usage, or boost on a command" "usage=[${models:-none}] commands=[${cmd:-none}]"
    record 'outcome' 'NOT ROUTED — the words changed nothing durable'
  fi
fi

if pane | head -1 | grep -q '»'; then
  record 'header' "boost pin visible in the header glance: $(pane | head -1 | grep -o 'talk »[^·]*')"
fi

dump_turn "$since"
finish
