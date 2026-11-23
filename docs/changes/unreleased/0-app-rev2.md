---
kind: changed
title: codeaf app answers agents.create at once and names every message once
pr: 0
surface: [engine]
invalidates:
  - "agents.create waited for the whole conversation to be assembled, which took seconds on a real profile. It now mints the folder, answers with the agent in about a millisecond, and assembles behind the answer; the engine's process half is warmed after hello."
  - "An answer was drawn twice: once from turn.delta and once from entry.added, and the words before a tool call came back again after it. Deltas and answer entries now carry msg, the person's entry echoes client_id, and entry ids are counted from the transcript so agents.entries names what entry.added named."
  - "A stopped turn ended ok, and its partial answer read back from agents.entries as a notice — the same paragraph a second time. A stop now ends turn.done {ok:false, error_kind:stopped} with one receipt, and the partial answer reads back as that answer."
  - "models.list read only the default service's remembered list and could answer nothing. It now answers what the terminal's model picker offers, through the picker's own composer, with the default model marked."
  - "task.changed said only running and settled tasks. A proposal is now a queued task, and tasks carry parent and group, so a nested task tree arrives whole."
---

Tool rows carry `title`, the terminal's own line for the call, and `detail`;
the engine's dim notices are `quiet`; `agents.set_effort` sets low, medium or
high. `docs/design/app-v3/tools/app-trace.py` records real protocol traces
against a real model.
