---
kind: added
title: codeaf app closes, archives, reopens, renames and finds agents
pr: 0
surface: [engine]
---

Was true: the app could only close one agent for the length of the process,
had no archive, no rename and no search, and listed every agent at once.

Is true now: `agents.move_to_closed`, `agents.archive`, `agents.reopen`,
`agents.rename`, `agents.idle` and `search.all`; `agents.list` takes a state
and a query and lists open agents by default, and an agent carries
`closed_at`, `archived_at`, `last_active_at` and `spent_usd`. Archive is the
conversation's own archive mark; a rename is the person's literal name, which
no namer replaces (`session.Agent.RenameByPerson`).
