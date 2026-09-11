---
kind: fixed
title: the machine reading is divided by the lanes the whole process is running, not one conversation's
pr: 947
surface: [engine]
invalidates:
  - "The admission governor used to divide its reading by ONE GRAPH'S count of running nodes, while the memory in that reading is the whole process tree's; a second conversation's build therefore became this conversation's per-node footprint, and that figure only rises. The count now covers every lane the reading does, kept in one `session.TaskLanes` the process hands to every conversation it opens (`Config.TaskLanes`, set by `cmd/aforge`'s `v3OpenSession`). A graph handed none is alone in its process and keeps an account of its own."
  - "`TaskGraph.running` used to be incremented and decremented at five sites (the frontier's start, `handBackSlotLocked`, `park`, `unpark`, and `standingWideWork`'s `graph.running = 1`). There is now ONE door, `takeLaneLocked`/`giveLaneLocked`, it moves the process's account in the same breath, and `TestEveryLaneMovesThroughTheOneDoor` fails the build on a sixth site."
  - "`docs/design/task-start/DESIGN.md` used to state the per-conversation attribution gap as a boundary owed to #907. It is closed, and the section now describes the account."
---

Two conversations share one process, and `/proc` cannot say which of them started
which compiler. Divided by one graph's own lanes, a neighbour's build read here as
that build over this conversation's node count — and a measured footprint only
rises, so one such reading narrowed every later fan in this conversation for the
rest of the session. The same gap ran the other way on the reservation, each graph
reading the other's visible memory as covering part of its own. Both halves now
read one count over the same population the reading covers.
