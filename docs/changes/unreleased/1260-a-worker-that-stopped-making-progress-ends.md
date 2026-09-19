---
kind: fixed
title: a worker that has stopped making progress ends by an observable property, not only at the step cap
pr: 1260
surface: [engine, chat]
invalidates:
  - "A run worker that alternated one action and one text reply could never be ended by the no-action rule, because a reply carrying a tool call resets that run to one; nothing but the step cap (200 finished calls), the wall or an outside limit ended it. A real pair of workers spent 820 model calls and $4.30 in half an hour in exactly that shape. Now the same command coming back with the same answer four finished steps in a row, with nothing the plan store records moving between the steps, ends the task with the reason `the same command came back with the same answer 4 times in a row: the work was not moving` on its record's ending line."
  - "The only endings a run worker's loop had were a store finish, a store park, the no-action run, the step cap, the wall and an errored turn. There is now one more: the same-action ending. A worker whose repeated look answers differently each time, or whose plan store moved between the looks, is never ended by it — the step cap still bounds those. A worker the store says is blocked on another task (an open child or an open dependency) is not failed by it either: the loop parks it the way `plandb wait` would, and it wakes when its wait is over."
---

The property is read from what the run already records about a step — the
command as spelled, the head of the answer that came back, and the store's own
record of movement — and from no word of any command or any error, so it holds
whatever the tool and whatever the cause. Four is the bound, the same figure the
no-action ending uses.
