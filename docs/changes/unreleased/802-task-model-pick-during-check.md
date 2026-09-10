---
kind: fixed
title: A model picked while a task is being checked is the next run's, not a rewrite of what ran
pr: 802
surface: [chat, engine]
invalidates:
  - "`RetargetTask` decided what a pick could move from the node's STATE alone. A node is `TaskRunning` across three lives — its own worker, the check reading what that worker left, and every repair round — so a pick made during the check rewrote the frozen spec of work that had already finished. It now reads the node's phase, the same way the steering door already did."
  - "A running node's row named whatever model was last picked for it. It names the model the work actually ran on; a pick the check cannot be shown is held under `Next run setup` instead."
---

A card that names a model which never ran a token of the work is a card nobody can
reconcile against a bill.

`RetargetTask` is the sanctioned exception to the model freeze: the person, standing
in one node's room, choosing for that node and nothing else. It branched on
`node.state`, and `TaskRunning` is not one moment — a node is running across its own
worker, the gate reading what that worker left, and every repair round. The worker
stops reading the instant `runTaskChild` returns, which on a checked node is minutes
before anything settles (`task_child_run.go` withdraws the speaker on every road
out). A pick made in that window wrote the frozen `spec.model`, so the row, the
checkpoint and the landed card all named the new id while every call had been billed
to the old one — and nothing would ever run on the new one, because the only worker
that could have was already finished.

It was measured on a real run: a node admitted on this install's worker tier with all
nineteen of its calls billed there, and a card claiming the low tier — the model that
had only ever run the hidden `router`, `taskname` and `caption` roles.

The steering door already knew this. `deliverTaskDirection` reads `TaskNode.lifeNow()`
and holds a correction the check cannot be shown rather than pretending it was
delivered, under the heading *THE CHECK HAS NO READER*. The model door now asks the
same question off the same word, so both doors mean one thing by "is there a turn left
to take this" instead of two.

A pick that cannot reach a turn lands where a settled node's pick already lands —
`nextModel` — so no surface learns new vocabulary: the room draws `next model <id>`
and the sidebar heads itself `Next run setup`, both of which already existed. What
moved is only that the row keeps the truth.

Still open, and deliberately not in this change: the sidebar's model name stays
pressable during the check, so the affordance lights up for a pick that will be saved
rather than applied. Making it absent needs the surface to be told whether anybody is
reading the node, which is the notice's business and not this door's.
