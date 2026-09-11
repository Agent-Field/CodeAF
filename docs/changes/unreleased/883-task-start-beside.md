---
kind: changed
title: a task's worker starts as soon as its folder exists, and memory and sizing are read beside it
pr: 883
surface: [engine, chat]
invalidates:
  - "A task node handed a drawing by the mark reader put it to the division reading BEFORE its worker's first request (`divideFromSketch`, called between building the worker and `runTaskChild`), and the card read `sizing the work` for as long as that reading took — 219 s on the measured node, whose worker waited the whole time. The drawing is now weighed beside the node's first worker (`sizingBeside`, `task_divide_sketch.go`), which starts at once on its unchanged brief. `divideFromSketch` is deleted."
  - "`sizing the work` (`TaskPhaseSizing`) was drawn for every division reading. It is now drawn only where the asker waits on the reading — a worker inside its own `divide_work` call (`divisionAsker.waits`). The reading beside a working worker moves no phase."
  - "The division reading's answer decided whether a worker started at all: parts meant no worker, `work no worker can do` meant the node landed `your call` without one. Now parts are admitted under the running worker and their receipt is put on its queue (under `Agent.handover`, so the runner's tail either folds them or the reading drops); a refusal or a reader nobody could reach does nothing; `work no worker can do` cancels the running worker and lands `your call` with the reader's sentence and whatever the worker wrote (`landNeedsPerson`); an answer that arrives after the worker said its last word or handed work out itself is dropped and journalled as `dropped`."
  - "`newTaskAgentOn` called the memory router (`memoryBlock`) as an argument to the worker's constructor, so every worker a node built — the first, a replacement after a provider fault, each repair round and merge resolver — waited one reflex call before existing. The constructor now asks no model (`TestATaskWorkersConstructorAsksNoModel`); one reading per node run starts before the working copy is carved (`nodeMemory`, `memory.go`) and its block rides the first request it arrives before, which may be the second or later. It still fails open."
  - "`divideOnce` was the one division body. It is split into `weighDivision` (every gate and the reading) and `admitDivision` (claim, freeze, admit), with one journal line written by whoever ends the division (`recordDivision`); `divide_work` and the reading beside the worker both go through it."
---

The worker's first request no longer waits on any model call that can run beside
it. The design is `docs/design/task-start/DESIGN.md` §S2; the one abstraction is
`besideWork` (`internal/session/task_beside.go`), a reading bound to the node's
context and joined on every road out, pinned by `TestEveryReadingBesideTheWorkHasAJoin`.
