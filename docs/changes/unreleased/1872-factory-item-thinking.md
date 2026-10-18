---
kind: fixed
title: Job thinking changes follow step bounds and report each step accurately
pr: 1872
surface: [chat, engine, docs]
invalidates:
  - "Was true: the item card changed thinking in a private loop that could overwrite fixed or rewound steps and bypass landed or proof-started edit refusals. Is true now: eligible conversation steps go through the shared edit as you; fixed and begun steps keep their settings, and a refused change saves neither its budget nor its note."
  - "Was true: the first running step's thinking stood for the whole job, so a fixed first step could hide a real change or contradict the confirmation and opening note. Is true now: proposal, saved confirmation and manager brief use per-step facts, including switched-off steps in changes and mixed thinking in the running steps. Legacy event fields remain readable."
  - "Was true: budget- or note-only confirmations could repeat the first step's thinking even though thinking was not changed. Is true now: they say nothing about thinking, while a requested thinking change names the saved values and steps left as they were."
---

A thinking edit records the person and the card's reason through the shared
step-edit history, just as another person edit does. Off steps stay off; their
new thinking is ready if they are later switched on. A conversation step after
proof refuses the whole card. The manual quotes the argument refusal the chat
actually returns and describes default resets and unchanged thinking.
