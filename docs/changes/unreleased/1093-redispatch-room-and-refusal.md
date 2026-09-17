---
kind: changed
title: A re-dispatch after running out is granted more room, and one that banks nothing new is refused
pr: 1093
surface: [resident, docs]
invalidates:
  - "A leaf that ran out of its token budget was re-dispatched with the identical budget and the identical brief, up to the round cap, and bought the same truncated ending each time. The dispatch's grant is attempt-aware now: `regrantAfterRunningOut` in `cmd/codeaf/subharness.go` grows the token grant by three halves per re-dispatch, never above `overrunGrantCeiling` (four flat leaf grants; a fan-in that measured more keeps every token it measured), and the wall the leaf is given follows the larger grant."
  - "A leaf that ran out was requeued whenever anything at all was banked, for as many rounds as the cap allowed. A re-dispatch that banks nothing the attempt before it had not banked is failed now, with the refusal named in the node's error; the count it is measured against is the one the hand-on release journaled, read back through `store.ReleasedTurnsFor`."
  - "The `--json` envelope had no field for in-place re-dispatches. `redispatches` counts them across the run's nodes off the journal (`store.NodeRedispatches`), and is absent rather than zero when there were none."
---

The two halves are one mechanism: the grant moves with the attempt, and the
record says whether moving it moved anything. A re-dispatch is worth buying only
when the room grows and the work follows; either half alone is the old loop with
a bigger bill.
