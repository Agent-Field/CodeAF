#!/usr/bin/env bash
# J10 · Ask about money
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J10 Ask about money — per-job and windowed spend readable in the thread; costs in the header'

expect_nodes 0

since="$(mark)"
actual="$(spend)"
record 'journal spend so far' "\$$actual"

say "what have you spent so far today?"
assert_journal "select count(*) from messages where seq > $since and role='agent'" 'the head replied' 120
sleep 6
snap answered

reply="$(journal "select group_concat(body,' ') from messages where seq > $since and role in ('agent','system')")"
record 'reply' "$(printf '%s' "$reply" | head -c 400)"

if printf '%s' "$reply" | grep -Eq '\$[0-9]|[0-9]+ ?¢|cent'; then
  _check yes 'the reply carries a real money figure' 'a dollar or cent amount' "$(printf '%s' "$reply" | grep -Eo '\$[0-9.]+|[0-9]+ ?¢' | head -3 | tr '\n' ' ')"
else
  _check no 'the reply carries a real money figure' 'a dollar or cent amount' "$(printf '%s' "$reply" | head -c 200)"
fi

# The header is the always-on half of the same fact.
if pane | head -1 | grep -Eq '\$[0-9]|tok'; then
  _check yes 'the header carries spend without being asked' 'tokens/dollars in the header row' "$(pane | head -1 | grep -Eo '[0-9.]+k? tok · \$[0-9.]+' | head -1)"
else
  _check no 'the header carries spend without being asked' 'tokens/dollars in the header row' "$(pane | head -1)"
fi

dump_turn "$since"
finish
