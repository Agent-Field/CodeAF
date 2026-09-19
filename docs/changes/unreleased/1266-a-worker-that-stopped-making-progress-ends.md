---
kind: fixed
title: a worker that has stopped making progress is told once, and then ends, well short of the step cap
pr: 1266
surface: [engine, chat]
invalidates:
  - "A run worker that alternated one action and one text reply could never be ended by the no-action rule, because a reply carrying a tool call resets that run to one; nothing but the step cap (200 finished calls), the wall or an outside limit ended it. A real pair of workers spent 820 model calls and $4.30 in half an hour in exactly that shape."
  - "Now the same command coming back with the same answer, with nothing moving under the worker's own task between the looks, is first ANSWERED and then ended. After three identical steps the run tells the worker once, in its own voice and inside the turn that is still running, what it saw and what the worker can do: try something else, wait for an outside thing in one longer action (the note states the longest one action may run, read from the belt's own ceiling), or park with `plandb wait` when the plan names what it waits on. Three more identical steps after that end the task with the reason `the same command came back with the same answer 6 times in a row: the work was not moving`. The note records no step and draws no row."
  - "A different command resets everything, the note included, so a later run of identical steps is told again. A worker whose repeated look answers differently each time, or whose own task or a row under it moved between the looks (a person's note on the task counts), is never ended by it. What its siblings do is not its progress. A worker the store says is blocked on another task is not failed either: the loop parks it the way `plandb wait` would, and it wakes when its wait is over."
  - "The only endings a run worker's loop had were a store finish, a store park, the no-action run, the step cap, the wall and an errored turn. There is now one more."
---

The property is read from what the run already records about a step: the command
as spelled, the head of the answer that came back, and the store's own record of
movement under the worker's task. It reads no word of any command or any error,
so it holds whatever the tool and whatever the cause. A worker waiting on
something outside the plan is the most common honest loop there is, which is why
the run speaks before it ends anything.
