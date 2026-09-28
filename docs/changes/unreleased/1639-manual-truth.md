---
kind: fixed
title: the manual's standing, pool, task-card, team-start and first-run pages say what the code does
pr: 1639
surface: [chat, docs]
invalidates:
  - "The manual said a standing card's `2` changes when it wakes and `esc` says no. The card has no `2`: `1` sets it up, `3` (once) is offered on checks and watches only, `0` declines, `esc` leaves it for later, and `o Change…` (`o Change where…` on a rule) corrects it."
  - "The manual said codeaf picks its crew against the public Model Pool and that the crew reads `own.json`. Crew routing uses its own prior and this install's outcomes and reads only the model-name aliases in the index copy built into the binary; only `codeaf pool` reads `own.json`."
  - "The manual said `codeaf pool verify` prints a metric count (`3 metrics`). It prints the metric names: `signature good: version <n>, generated <date>, metrics <names>`."
  - "The manual drew the task proposal's clock as `start it in 9s` on its answers row with the foot `enter take it · esc later · c change`. The clock is on the top edge beside `codeaf asks`, defaults to 15s, and the foot is `esc later · o other · ? clarify`; `c` is not a key on that card."
  - "The manual said every task is bounded at 3 levels and 20 pieces per parent. Those bounds belong to the node belt (`CODEAF_TASK_BELT=node`); the default bash belt splits with `plandb split` under 256 tasks per batch, 1024 per run and four wakes per composite."
  - "The manual said a manager's `team_start` always asks first on a card. It follows the conversation's approval posture: the default `◇ YOLO` starts the member with no card, and only `◇ asks` raises one."
  - "The manual said the first run on an empty profile always opens two setup screens under a `setup · 2 of 2` header. With a provider key already in the profile or the environment, plain codeaf opens on home with no setup, and the header reads `setting up · 1 of 2` under the wordmark."
  - "The manual said a program's `[<name>]` badge is on the landed card. Only the proposal card and the task's rows wear it; the landed card's head does not."
---

Three of the issue's rows are code defects rather than page errors and are left for their own
issues: plain codeaf's engine road never draws `· N standing orders here — /standing`, the
`remember` and `stand` tools both claim a stated preference, and the standing card never shows
the grant.
