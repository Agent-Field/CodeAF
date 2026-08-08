#!/usr/bin/env bash
# Q7 · A data task with exactly one right answer
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'Q7 Data quality — five numbers, a ranking, and a median: arithmetic has a right answer and this checks it'

# 12, 7, 41, 3, 28 sorted is 3, 7, 12, 28, 41 — the median is 12. There is
# nothing to argue about, which is the point: a product that is fluent and
# wrong fails here and nowhere else.

expect_nodes 1
since="$(mark)"
started="$(date +%s)"

say "here are five numbers: 12, 7, 41, 3, 28 — make a file ranking them from largest to smallest and tell me the median"

assert_journal "select count(*) from nodes where created_seq > $since and origin='user' and status='done'" \
  'the job finished' 300
sleep 10
snap answered
record 'wall time' "$(( $(date +%s) - started ))s"

answer="$(journal "select group_concat(body, char(10)) from messages where seq > $since and role in ('agent','system')")"
printf '%s\n' "$answer" > "$UX_DIR/answer.txt"
record 'the answer' "$(printf '%s' "$answer" | tr '\n' ' ' | head -c 600)"

# The median is 12 and nothing else.
if printf '%s' "$answer" | grep -Eqi 'median[^0-9]{0,40}12|12[^0-9]{0,20}(is the )?median'; then
  _check yes 'the median is right (12)' 'the number 12 named as the median' 'found'
else
  _check no 'the median is right (12)' 'the number 12 named as the median' \
    "$(printf '%s' "$answer" | grep -Eoi 'median[^.]{0,40}' | head -1)"
fi

# The ranking, largest first, in the file it was asked to write.
ranked="$(find "$UX_STATE/workspace" -type f -newermt "@$((started - 30))" 2>/dev/null | head -5)"
record 'files written' "$(printf '%s' "$ranked" | tr '\n' ' ')"
found_order=0
while IFS= read -r file; do
  [ -z "$file" ] && continue
  order="$(grep -Eo '\b(41|28|12|7|3)\b' "$file" 2>/dev/null | tr '\n' ' ')"
  record "order in $(basename "$file")" "$order"
  case "$order" in "41 28 12 7 3 "*) found_order=1 ;; esac
done <<< "$ranked"

if [ "$found_order" = "1" ]; then
  _check yes 'the file ranks them largest to smallest' '41 28 12 7 3 in that order' 'correct'
else
  # The thread may carry the ranking even when the file is shaped differently.
  if printf '%s' "$answer" | grep -Eq '41.{0,12}28.{0,12}12.{0,12}7.{0,12}3'; then
    _check yes 'the ranking is right, largest first' '41 28 12 7 3 in that order' 'correct, in the thread'
  else
    _check no 'the ranking is right, largest first' '41 28 12 7 3 in that order' \
      "$(printf '%s' "$answer" | grep -Eo '41[^a-z]{0,40}' | head -1)"
  fi
fi

judge_this 'here are five numbers: 12, 7, 41, 3, 28 — make a file ranking them from largest to smallest and tell me the median' "$answer"

dump_turn "$since"
finish
