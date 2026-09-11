---
kind: added
title: A quick task does the small job where you are — no copy of the folder, no branch, no check
pr: 811
surface: [chat, engine, docs]
invalidates:
  - "Every task got its own copy of the folder, was checked, and landed. A quick task gets none of the three: it runs in the caller's own workspace, its last message is its result, and its row goes `done` with no branch and nothing to merge."
  - "`propose_task` was the only way the model handed work off. `quick_task` is beside it now, on the chat's belt and on every task worker above the depth floor, and it starts the work at once with no card, no countdown and no consent."
  - "A running task's row showed a state word. A quick task's row shows what it is doing instead: `quick · 2/4 · <the item it is on>`, or `quick` alone when it has no items."
  - "`fork` was on the chat's belt for parallel hands inside one reply. It is off the belt; work of that shape is a quick task now."
  - "The tool a worker uses to tick its checklist did not exist. `items` is on a quick worker's belt and nowhere else: `{done: n}` ticks an item, `{add: [...]}` appends."
  - "Two pieces of work that wanted the same file both wrote it and the merge was yours. Two quick tasks claiming one path run one after the other, and the second's start line says ` · waits for task 5 (both claim <path>)`."
  - "A task working in your own folder took the folder off you: the chat could read it and not write it. A quick task does not — the folder stays yours while it runs, and its only claim is the files it named plus the ones it has already written."
  - "A quick worker that had ticked every item could read on for as long as its rounds allowed. The tick that finishes the list now replies ` · every item is ticked: your next message is your answer, whole — what you found, in full, not that you are done — and it is the last thing you say`."
  - "The manual said work handed off always leaves on a branch you can inspect. A quick task leaves nothing to inspect: stopped or out of rounds, what it wrote is in your folder as it left it, possibly half made."
---

The big task is unchanged. What is new is a second, smaller notion beside it with
no barrier to enter or to leave: one tool call, a line and an ordered list of
items, run where the caller works. The rule for which road a piece of work takes
is written once, in `quick_task`'s own description, and `propose_task` now points
at it — if you will read the result yourself and carry on, and it needs no check
and no branch of its own, it is quick.
