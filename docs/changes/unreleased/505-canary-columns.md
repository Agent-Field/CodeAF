---
kind: added
title: every canary chat row counts the check rounds that failed and the steward's carry-ons, beside done-to-wall
pr: 505
surface: [build, chat]
invalidates:
  - "The canary's chat-door wall (a chat that keeps working past its own task landing until the 900 s wall) was read on #407 as a regression introduced between 1de89c08 and 5e1f5540, and bisected as such. It is baseline behaviour at 35c1a79e: after the task lands, the post-task check (the `mark` rows of the cell transcript) fails every round, the handoff rung fails under `--one-model` with `roles: no model for role \"handoff\"`, and the steward decides `carry on` with no floor. Across 36 chat cells at 8 shas not one check round passed, including cells whose fix-PR tests were green on the tree. The two numbers a fix for #468 must move are now columns on every chat row: `marks` and `carry`."
  - "A canary COST flag of 2–7× against the 35c1a79e baseline was read as spend that appeared. It is #357 (22894273): the call log used to drop any end row whose `wait_s` or `cost_s` was `+Inf`, which under one model with no alternative lane is most waited calls, so every cost figure from a binary before 22894273 is understated by exactly those rows. Cost is comparable only among rows from 22894273 onward."
  - "The canary table printed the literal `None` in the `gate` column of chat rows. A missing measurement now renders as nothing in every column, as the emptiness law says."
---

The columns are read from the cell's own v3 transcript (`home/v3/projects/*/*/transcript.jsonl`),
whose `mark` and `principal` payloads are Python-repr strings rather than JSON, so they are
matched as text. A `do` cell, or a chat cell with no transcript, leaves both absent; a chat
cell that ran and never looped writes a real 0. Also carried: the picker admits `.pyi`
sources and `releasenotes/notes/*.yaml` fragments, and `lib/chat.sh` can be sourced from any
shell through `CANARY_LIB`.
