---
kind: changed
title: chat answers number their steps, say where a job stands, and end on one next action
pr: 1209
surface: [chat]
invalidates:
  - "The answer section's bans carried examples: no opener (\"Sure\", \"Great question\", \"You're right\") and no closing offer (\"Want me to…\", \"Say the word and I'll…\"). The rewording kept both bans and cut the examples to \"Want me to…\" alone, to stay inside the prefix budget. A test that used `Say the word and I'll` as a sentence only the chat's page carries went on passing without testing anything; it now uses `no closing offer` and checks its sentinels are still on the page."
  - "The rewording also dropped \"never hand back half-solved work\" from the definition of done, which no line of this entry said. That was not intended and #1393 put it back, as \"never a compiling scaffold, a narrowed test or half-solved work\", paid for by rewording the same section."
---

The answer section of the system prompt (internal/session/prompts/system.md)
now shapes replies the way the i-have-adhd skill does, adapted in our own
words and credited here: the skill is MIT licensed, Copyright 2026 Ayoub
Ghriss. No text of the skill was copied.

What moved: a multi-step job must say where it stands each turn (like
"step 3 of 5") and its cost in minutes or hours; a sequence is numbered with
as few steps as the job allows; an error is stated matter-of-factly, cause
then fix; and when one concrete thing is the person's to do next, the answer
ends on that action instead of a permission question. A closing offer is
still banned, and a next action is not one: it names the single thing to do,
it does not ask "Want me to?".

The rewrite is paid for by rewording, not growth, and both prefix budgets in
internal/session/prefixbudget_test.go stay green: the fixed prefix goes 55,266
to 55,253 bytes (budget 55,280) and the lean prefix 47,055 to 47,042 (budget
47,055), so each one shrinks. Completed work stays folded into the "worked"
line; that rule was deliberately left alone.
