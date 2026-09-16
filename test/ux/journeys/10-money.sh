#!/usr/bin/env bash
# J10 · Ask about money
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J10 Ask about money — per-job and windowed spend readable in the thread, and always-on on the foot'

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

# The always-on half of the same fact — the money you never had to ask for.
#
# v3 carries the bill on the frame's foot row, beside the cache and context
# readings ("$0.27 · ⟲ saved $0.0038 · 58% cached … 66.8k/1.3M · 5% …"), and on
# the status deck's first row beside the session name. The foot is the row the
# frame always draws, so this reads the foot — the same fact, at the address
# the frame gives it.
foot="$(pane | tail -3 | grep -E '\$' | head -1)"
record 'foot row' "$(printf '%s' "$foot" | sed 's/^ *//')"
if printf '%s' "$foot" | grep -Eq '\$[0-9]'; then
  _check yes 'the foot carries spend without being asked' \
    'a dollar figure on the foot row' "$(printf '%s' "$foot" | grep -Eo '\$[0-9.]+[KM]?' | head -1)"
else
  _check no 'the foot carries spend without being asked' \
    'a dollar figure on the foot row' "${foot:-no foot row with a dollar figure}"
fi

dump_turn "$since"
finish
