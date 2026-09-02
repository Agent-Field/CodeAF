---
kind: fixed
title: a brief that fails to write is retried, then composed, and the plan it belongs to is kept
pr: 422
surface: [chat, engine]
invalidates:
  - "plan.Build used to join brief-writing faults into its returned error, and cmd/aforge/chat.go read any non-nil error as no plan at all. It no longer does either: brief faults do not travel out on Build's return, and both of chat.go's fall-backs to one worker now fire on a nil graph. A drawn plan is run as drawn."
  - "A leaf whose brief call failed used to reach the executor with no instruction, to be composed for at dispatch by internal/exec's fallbackBrief. The call is now retried once and composed for during the build, so the node leaves the planner already briefed. The composition itself moved to plan.ComposedBrief and fallbackBrief calls it — a node's title, summary and sources, and it no longer restates the goal."
  - "store.NodeBrief carries a fault field. It is empty on the ordinary node; on a node no model wrote a brief for it says why, which is the only way to tell a composed brief from a written one after the fact."
---

One node's brief call came back with a reply that had stopped being language, and
a sound six-node plan was thrown away for it — the whole goal ran as a single
oversized worker while five of its instructions sat already written. A brief is
one leaf's instruction and never the graph's right to exist, so the failing call
is asked once more on the same bytes, the way the ground and fan-out passes
already ask, and a node the model will not write for keeps an instruction
composed from what the plan already knows about it.
