#!/usr/bin/env bash
# Q2 · Does it actually solve a research task?
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'Q2 Research quality — a finance question with checkable answers, graded against the real filed numbers'

# Apple's FY2024 (ended 28 Sep 2024) 10-K: total net sales $391.035B, of which
# Services $96.169B and Products $294.866B. Those are the ground truth this
# journey grades against — a number in the right place is worth more than a
# confident paragraph, and a wrong number is worth less than nothing.

expect_nodes 1
since="$(mark)"
started="$(date +%s)"

say "research Apple's total revenue in fiscal year 2024 and how much of it was Services. give me the two numbers and cite where they come from."

assert_journal "select count(*) from messages where seq > $since and role in ('agent','system')" 'it replied' 240
wait_journal "select count(*) from messages where seq > $since and role in ('agent','system') and length(body) > 120" 300
sleep 15
snap answered

elapsed=$(( $(date +%s) - started ))
record 'wall time' "${elapsed}s"
record 'cost' "\$$(journal "select round(coalesce(sum(cost),0),5) from usage where seq > $since")"
record 'nodes formed' "$(journal "select count(*) from nodes where created_seq > $since")"

answer="$(journal "select group_concat(body,char(10)) from messages where seq > $since and role in ('agent','system')")"
printf '%s\n' "$answer" > "$UX_DIR/answer.txt"
record 'answer' "$(printf '%s' "$answer" | tr '\n' ' ' | head -c 1200)"

grade="$(printf '%s' "$answer" | python3 -c "
import re, sys
text = sys.stdin.read()
numbers = [float(n.replace(',', '')) for n in re.findall(r'[0-9][0-9,]*\.?[0-9]*', text)]
def near(target, tolerance):
    return any(abs(n - target) <= tolerance for n in numbers)
total = near(391.0, 2.0) or near(391035.0, 2000.0) or near(391035000000.0, 2e9)
services = near(96.0, 1.5) or near(96169.0, 1500.0) or near(96169000000.0, 1.5e9)
print('total=%s services=%s' % (total, services))
")"
record 'number grade' "$grade"

printf '%s' "$grade" | grep -q 'total=True' \
  && _check yes 'total FY2024 revenue is right (\$391.0B)' 'a number within \$2B of 391.0' 'found' \
  || _check no  'total FY2024 revenue is right (\$391.0B)' 'a number within \$2B of 391.0' "$(printf '%s' "$answer" | grep -Eo '\$?[0-9][0-9,.]*( ?(billion|B|million))?' | head -8 | tr '\n' ' ')"

printf '%s' "$grade" | grep -q 'services=True' \
  && _check yes 'Services revenue is right (\$96.2B)' 'a number within \$1.5B of 96.2' 'found' \
  || _check no  'Services revenue is right (\$96.2B)' 'a number within \$1.5B of 96.2' "$(printf '%s' "$answer" | grep -Eo '\$?[0-9][0-9,.]*( ?(billion|B))?' | head -8 | tr '\n' ' ')"

if printf '%s' "$answer" | grep -Eqi 'http|sec\.gov|10-k|annual report|investor\.apple|press release'; then
  _check yes 'the claim carries evidence, not confidence' 'a named source' "$(printf '%s' "$answer" | grep -Eoi 'https?://[^ )]*|sec\.gov[^ )]*|10-K' | head -3 | tr '\n' ' ')"
else
  _check no 'the claim carries evidence, not confidence' 'a named source' 'no URL, filing, or report named'
fi

if printf '%s' "$answer" | grep -Eqi 'https?://'; then
  record 'looked it up' 'yes — a live URL appears in the answer'
else
  record 'looked it up' 'no live URL in the answer; the numbers may be from model memory rather than the web tool'
fi

judge_this "research Apple's total revenue in fiscal 2024 and how much was Services; give the two numbers and cite where they come from" "$(deliverable_since "$since")"

dump_turn "$since"
finish
