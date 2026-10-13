---
kind: added
title: The app searches saved work and records engine tidy actions
pr: 1800
surface: [engine, docs]
invalidates:
  - "App search, history and Activity returned authenticated 501 stubs. A12 now merges agents, crews, solo jobs and available file results, filters saved history and durably records successful engine tidy actions."
  - "The agent list refused said: and cost: filters. It now accepts the shared history grammar, including quoted phrases and actual-spend comparisons without an AI call."
---

Search keeps four rows per group with prefix, word-prefix and substring ranks.
History keeps closed and archived work. Activity preserves successful lifecycle
and pin changes across restarts and emits typed events; app tab undo remains
app state. Other resource tidy verbs are observed through their existing typed
read interfaces, so their lanes can land without editing this journal.
