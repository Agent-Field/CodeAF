---
kind: fixed
title: a timeout is not a person's no, and a look does not ask in default mode
pr: 622
surface: [chat, engine, docs]
invalidates:
  - "A consent wait that ended — timeout, interrupt, the turn dying — was handed to the model as `denied by the person: <rule>`, including `denied by the person: default` on propose_task. It now says `not approved: the question timed out` or `not approved: ended before an answer`."
  - "Default (prompt) mode asked about every look: `read`, `ls`, `grep`, `find`, a `tasks` read, and `git status`. Those run without a card; writes, steers, and compound shell lines still ask."
---

R2: misattributed deny on a wait that nobody answered, and approval prompts on read-only calls.
