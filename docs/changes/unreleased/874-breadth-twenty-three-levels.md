---
kind: changed
title: A task may hand out twenty pieces and tasks nest three deep, and the fan-out page says the goal is the whole job's wall time
pr: 874
surface: [engine, chat]
invalidates:
  - "One task could hand out at most five pieces (`taskFanLimit = 5`), and a sixth was refused. The cap is twenty (`taskFanLimit = 20`), and the refusal reads `no: you have already handed out 20 pieces of this work, which is as many as one task may.`"
  - "Tasks nested two deep (`taskDepthLimit = 2`): the conversation's task could hand pieces out, and a piece could not. They nest three deep: a piece carries `propose_task`, `quick_task` and `tasks` and may split its own share, and a piece of a piece has none of them."
  - "The fan-out page told a worker to split when a step had `TWO OR THREE PARTS`, and it said unconditionally that a piece you hand out cannot hand out more. It now says split when a step has parts that do not need each other, however many, and says whether the worker's pieces may split in turn from that worker's depth."
  - "The five and the two were described as chosen bounds on decomposition. Neither decides how wide work goes. How much runs at once is `task.parallel` and the admission governor, and the fan cap is only a runaway stop."
  - "A node calling `tasks` with no arguments was shown at most ten of its own pieces, with no line saying more existed. It is shown every piece it handed out."
  - "Every part of a division was armed to divide again by its parent's count of items, because it read the parent's brief composed around it and the inherited request. It was inert only because parts had no verb. A part is now armed only by its own scope, and a `/task` judge's yes arms only the root task it read."
---

The owner asked for breadth to be large where work parallelises, and for the prompt to
say that the goal is the shortest wall time for the whole job. The fan-out page carries
that as one sentence. It replaced a paragraph that restated arithmetic the belt facts
already state, so the conversation's fixed prefix grew by 24 bytes and the task worker's
page by 23.

The admission governor still weighs the machine once per frontier pass, so twenty
pieces that become ready together start on one reading. That is written down in
`docs/design/task-start/DESIGN.md` as the governor's next change.
