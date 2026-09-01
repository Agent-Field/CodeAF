---
kind: fixed
title: a divided parent reads its two facts as one, and stops buying a turn on an empty request
pr: 200
surface: [engine]
invalidates:
  - "A divided parent whose last two pieces landed together spent one model turn on an empty request — measured as two byte-identical requests. It no longer can: the two facts a parked runner reads are taken under the lock the delivery writes them under, so no reader sees half a delivery."
  - "`deliverTaskNote`'s order — the queue, then the fact, then the wake — was described as what made both readings safe. It never was on its own: the fact and the wake live under two different locks, so ANY pair of separate reads could land inside a delivery, whichever order the reader took them in. The order still holds and is now backed by [Agent.handOverTaskNews], which makes those last two writes one step."
  - "`Agent.taskNewsOwed` and `Agent.childrenOutstanding` were the way to ask both questions. A park decision that reads both now goes through `Agent.taskNewsStanding`, and `internal/session/tasknews_law_test.go` fails a function that asks both without saying in words how it reads them — `runTaskChild`'s no-progress switch is the one entry that still reads them separately, and says why that costs nothing."
  - "`TaskNode.markNoted` was one function that set the mark and wrote the checkpoint. The mark is `TaskNode.noteHandedOver` now and the checkpoint is written by its caller, because the delivery road sets the mark inside a seam a parked parent may be waiting on and a file write does not belong there. `markNoted` behaves exactly as it did for its other callers."
  - "A forked hand's report counted the hand home and then posted its news as two steps (`handIsHome`). They are one step now, through the same handover a divided part's report uses — a hand counted home before its news was posted is the same half-delivery, and it cost the same wasted turn."
---

Found by the #176 lane while chasing the load flakes (#187) and named there rather
than changed, because two other lanes owned the file at the time. The read site
and the write site are two thousand lines apart and each was correct on its own
page, which is why review never caught it — so the pairing is now held by a test
that makes a new reader of the pair say in words how it reads them.
