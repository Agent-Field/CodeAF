---
kind: added
title: A task an agent does carries a brief, and its run starts with its inputs and the hand-off note
pr: 2748
surface: [factory, chat, docs]
invalidates:
  - "ws_task_create's request was \"what is to be done, in a few lines\" and the tool had no field for acceptance, inputs or references; now an agent's task is a brief (goal, deliverable, 2-6 acceptance lines, inputs as references to files, task Results, decisions or links, constraints), refused without the first three, and every task it waits on is an input."
  - "A server task run was given only the task's title and request, and found what it built on by searching (4-7 calls per dependant); now its first message carries the brief, the decisions in force and every input's words up to 48 KB."
  - "Done needed only a check line; now an agent's done carries a hand-off note, which goes into every dependant's opening context."
---
