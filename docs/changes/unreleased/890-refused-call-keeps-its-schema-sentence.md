---
kind: fixed
title: a refused call keeps the schema's sentence for the model, and the person reads their own words
pr: 890
surface: [chat, engine, docs]
invalidates:
  - "A tool call refused on its arguments drew its `Invalid arguments: …` sentence to the person and opened itself under the row. It is mail for the model — the field name, the quoted command, one repair away from a working call — and it stays in the tool's result, in the transcript, and behind `ctrl+o` on that row. The refused call's row reads `the call was refused`, and the task's settling card reads `not started · the call was refused`."
  - "`A FAILURE OPENS ITSELF` was taken by every `toolFailed` event, so a schema refusal opened a card about a call that never ran. An argument refusal — one the engine knows never reached the tool's work — no longer takes that road; a command that genuinely ran and failed still opens with its own output."
  - "`internal/manual/chat/how-tasks-run.md` quoted `Invalid arguments: checks must each be ONE command with no shell composition` as a sentence the screen shows. It says where that sentence goes now, and that the person reads `the call was refused` instead."
---

One sentence, two audiences. The belt's refusal text is written for the model so
it can repair the call in one round trip — which is why the door refuses rather
than dropping silently — and the person was reading a repair instruction
addressed to somebody else about a field they have never heard of, on a card
saying nothing started.
