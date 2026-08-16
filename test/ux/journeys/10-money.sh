#!/usr/bin/env bash
# J10 · Ask about money
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J10 Ask about money — per-job and windowed spend readable in the thread; and always-on where the surface puts it (v1 header · v2 meta strip)'

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
# v1 puts it in the header row. v2 has no header at all (13.4 J10, and the
# verdict's open product decision (c)); the doc's home for the always-on
# figure is the composer's meta strip, which carries `<role> <model> · $X.XX ·
# <ctx>` one row above the footer, filled from the poll.go:118-193 spend seam
# and honest with `$—` until something has actually been measured (5.14
# critical-vs-incidental, G8's `—` honesty law). So v2 reads the strip, not
# row 1 — the same fact, at the address the design gives it.
if is_v2; then
  strip="$(pane | tail -4 | grep -E '\$' | head -1)"
  record 'composer meta strip' "$(printf '%s' "$strip" | sed 's/^ *//')"
  if printf '%s' "$strip" | grep -Eq '\$[0-9]'; then
    _check yes 'the composer meta strip carries spend without being asked' \
      'a dollar figure on the meta strip' "$(printf '%s' "$strip" | grep -Eo '\$[0-9.]+[KM]?' | head -1)"
  else
    _check no 'the composer meta strip carries spend without being asked' \
      'a dollar figure on the meta strip' "${strip:-no strip row with a dollar figure}"
  fi
else
  if pane | head -1 | grep -Eq '\$[0-9]|tok'; then
    _check yes 'the header carries spend without being asked' 'tokens/dollars in the header row' "$(pane | head -1 | grep -Eo '[0-9.]+k? tok · \$[0-9.]+' | head -1)"
  else
    _check no 'the header carries spend without being asked' 'tokens/dollars in the header row' "$(pane | head -1)"
  fi
fi

dump_turn "$since"
finish
