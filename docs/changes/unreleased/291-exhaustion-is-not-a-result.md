---
kind: fixed
title: a leaf that ran out of its tokens is never settled on a judge's say-so
pr: 291
surface: [engine, resident]
invalidates:
  - "A leaf that ran out of room used to be able to settle DONE: `cmd/aforge/chat.go`'s exhaustion arm asked `revision.JudgeRemainder`, and a checked `{\"done\": true}` overwrote the verdict to a verified success with no continuation spliced — a worker cut off mid-edit was ✓ two seconds after its own ⏳ line. Both answers now take the splice arm; the judge only decides what the continuation is aimed at."
  - "`resident.ExecResult` used to carry only a summary, money and turns, so the scheduler could not tell a leaf that ran out from one that finished and called `graph.Complete` on both. It now carries Stopped, Stop, Meter and Continued, and `runOne` releases a leaf that ran out back to pending instead of completing it."
  - "`revision.JudgeRemainder` used to take `(ctx, settings, client, graph, node, produced, workerModel)` and was shown only the brief and the worker's final text — its prompt said in as many words that \"exhaustion is not evidence of incompleteness\". It now takes a `revision.Evidence` as well, and is shown the exhaustion record, the run's own banked turns, the change and the project's own reading."
  - "`exec.Meter.Reached` used to be frozen at the moment the landing reserve was granted, so every ⏳ line and every stored `leaf_exhausted.reached` under-reported the leaf's spend by 12-14%. The two live bounds are re-read at land time."
  - "`cmd/aforge`'s `leafSpend` used to take four arguments; it now takes the outcome and whether the work was continued, so a result cannot be built without an ending on it."
---

The measured run: a leaf hit its grant mid-`sh` with a red build, got its four landing
turns, stopped — and the node was ✓ two seconds later with the leaf's own last sentence as
its summary. Its two siblings then briefed on truncated work and opened with "everything is
already in place". Nothing that runs after that ✓ can recover what was cut.

The law is #267's in the other organ: exhaustion is a measured fact, doneness is a claim,
and a measured fact is never overturned by an unmeasured claim. A leaf that ran out has no
account — its work is evidence for the next attempt, never a result. Where its remainder can
be spliced it is; where it cannot, the node goes back on the queue with its banked turns
rather than being ticked, bounded by the same three rounds every other growth is.
