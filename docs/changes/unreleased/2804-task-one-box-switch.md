---
kind: changed
title: a task's one box switches between Comment and Tell Afi; the box under Working is gone
pr: 2804
surface: [chat, app]
invalidates:
  - "Was true: the manual said the task's owner and whoever started the run get a separate box under Working that says \"Tell Afi…\", and that a done Afi task is revised by typing in that box. Is true now (OWNER-DECISIONS §14): a task's view has one box; on a task Afi or a chat does that you may steer, its left is a two-way switch, \"Comment · everyone in <workstream>\" or \"Tell Afi\" (\"Revise with Afi\" on a done task). It opens on Comment unless the task waits on Afi's next step (you may tell it, not done, nothing working, no open question). Steering and revising work exactly as before through that side."
---

The task view had two boxes stacked for a task Afi does, against build spec v2's one box per screen. The owner kept the capability and the one box: the who-hears-it figure became the switch. The desk change is codeaf-desk's T1 one-box PR; this entry moves the manual with it.
