---
kind: fixed
title: Job thinking changes follow step bounds and report each step accurately
pr: 1872
surface: [chat, engine, docs]
invalidates:
  - "Was true: the item card changed thinking in a private loop that could overwrite fixed or rewound steps and bypass landed or proof-started edit refusals. Is true now: eligible conversation steps go through the shared edit as you; fixed and begun steps keep their settings, and a refused change saves neither its budget nor its note."
  - "Was true: the first running step's thinking stood for the whole job, so a fixed first step could hide a real change or contradict the confirmation and opening note. Is true now: proposal, saved confirmation and manager brief use per-step facts, including switched-off steps in changes and mixed thinking in the running steps. Legacy event fields remain readable."
  - "Was true: the terminal chat drew no `factory_item` card, and a hosted call sat at `running factory_item` until esc. Is true now: the terminal chat draws it as a plain question block, with `1` yes applying once, `2` keep it changing nothing, typed words read as a correction and esc setting it aside; an interrupt or 15 minutes takes it down unanswered. Still true: the card omits fixed and begun steps, `factory_add` and `factory_recipe` cards are not drawn there and wait until an interrupt or 15 minutes, and a desktop-app yes sent with text is read as a correction."
  - "Was true: budget- or note-only confirmations could repeat the first step's thinking even though thinking was not changed. Is true now: they say nothing about thinking, while a requested thinking change names the saved values and steps left as they were."
  - "Was true: a step stored with capitals (`Tidy`) could not be reached by name in a step edit, and when two steps shared a name only the first was edited while the receipt spoke for both. Is true now: the shared step edit finds a step by its name without regard to case or surrounding spaces and keeps the stored spelling; a name that two steps answer to refuses the whole edit, and a step cannot be added whose name differs from an existing one only in case. Under a recipe's `ask`, a manager or plan change to such a job still gets its approve step, after a stored `Plan` too, and a job with two steps named plan refuses that change whole rather than landing it unratified. Stored names, phases and the runner are unchanged, and there is still no way to rename a step, so an older job with two same-named steps cannot be edited by name."
---

A thinking edit records the person and the card's reason through the shared
step-edit history, just as another person edit does. Off steps stay off; their
new thinking is ready if they are later switched on. A conversation step after
proof refuses the whole card. The manual quotes the argument refusal the chat
actually returns and describes default resets and unchanged thinking.
