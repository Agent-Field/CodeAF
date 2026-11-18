---
kind: added
title: Asks say whether they can be undone, and an Ask can be handed on, undone or its task taken back
pr: 2797
surface: [factory, engine]
invalidates:
  - "Was true: a workstream Ask had no way to say its answer could be undone, and every answer was final. Is true now: TaskAsk.reversible is set by whoever raises it (agents honestly, a person's Ask false), and a reversible answer can be undone from its task with undo_answer until the task is done; the Ask opens again and its decision is withdrawn."
  - "Was true: nothing changed an Ask's decider once raised. Is true now: ask_hand hands one open Ask to someone else in the workstream; the owner's own Ask becomes one of the task's Asks."
  - "Was true: taking a running task back from its agent needed Stop, then Done by You, in two writes. Is true now: take_back does both in one change, keeps what the agent made and drops the agent's own Asks."
---

The engine half of the Inbox in build spec v2 (screens 1 and 2) and owner
decisions 2026-11-17 §13: the "Quick, and can be undone" group and "Accept
Afi's answer on all" exist only for Asks marked reversible.
