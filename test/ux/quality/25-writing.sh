#!/usr/bin/env bash
# Q6 · A writing task with no code and no numbers in it
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'Q6 Writing quality — a professional email, where the only measure is whether a person would send it'

# Coding and research are the two tasks that grade themselves. Most work does
# not: it is a paragraph somebody has to be willing to put their name on. This
# is the journey that catches a product which is good at tests and bad at
# English.

expect_nodes 1
since="$(mark)"
started="$(date +%s)"

say "draft a short professional email declining an invitation to speak at a conference next month because of a scheduling conflict. show me the email."

assert_journal "select count(*) from messages where seq > $since and role in ('agent','system') and length(body) > 150" \
  'a draft came back with something in it' 300
sleep 10
snap drafted

elapsed=$(( $(date +%s) - started ))
record 'wall time' "${elapsed}s"

draft="$(journal "select group_concat(body, char(10)) from messages where seq > $since and role in ('agent','system')")"
printf '%s\n' "$draft" > "$UX_DIR/email.txt"
record 'the draft' "$(printf '%s' "$draft" | tr '\n' ' ' | head -c 800)"

# The literal shape of an email, checked without a model: the parts a person
# would notice missing at a glance.
for part in 'subject' 'dear\|hello\|hi ' 'regards\|sincerely\|best'; do
  if printf '%s' "$draft" | grep -Eqi "$part"; then
    _check yes "the draft has the '$part' part of an email" "an email-shaped section" 'present'
  else
    _check no "the draft has the '$part' part of an email" "an email-shaped section" 'missing'
  fi
done

if printf '%s' "$draft" | grep -Eqi 'conflict|unable to|regret|decline|cannot attend|won.t be able'; then
  _check yes 'it actually declines, and gives the reason it was told to give' \
    'a decline citing the scheduling conflict' "$(printf '%s' "$draft" | grep -Eoi 'scheduling conflict|conflict|regret[a-z]*' | head -1)"
else
  _check no 'it actually declines, and gives the reason it was told to give' \
    'a decline citing the scheduling conflict' "$(printf '%s' "$draft" | head -c 200)"
fi

if printf '%s' "$draft" | grep -Eq '\[|\{\{|XXX|TODO'; then
  record 'placeholders' "the draft contains fill-in slots: $(printf '%s' "$draft" | grep -Eo '\[[^]]{1,30}\]' | head -3 | tr '\n' ' ')"
else
  record 'placeholders' 'none — the draft is ready to send as written'
fi

judge_this 'draft a short professional email declining an invitation to speak at a conference next month because of a scheduling conflict; show me the email' "$draft"

dump_turn "$since"
finish
