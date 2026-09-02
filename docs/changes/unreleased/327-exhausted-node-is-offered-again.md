---
kind: fixed
title: a node that ran out with work recorded is offered again, and the sentence names the bound
pr: 327
surface: [engine, resident, docs]
invalidates:
  - "`resident.ExecResult` no longer has a `Stopped` field, and `RanOut()` is `!Continued && Stop.OutOfRoom()` rather than `Stopped && !Continued && Stop.OutOfRoom()`. #291 added the flag so a result nobody filled in could not read as a leaf that stopped for no reason, but `StopReason.OutOfRoom` already reads the empty string as false — so the flag could only ever subtract, and a result carrying an unambiguous out-of-room ending with the flag unset had its node settled DONE over a worker that was still working. The ending word decides alone."
  - "`cmd/aforge`'s `leafSpend` no longer sets `Stopped`; it carries `Stop` and `Meter` from the outcome as before."
  - "The release reason a requeued node carries used to be composed from the meter alone — `it was still working when it ran out (cost: 178086 of 176834 tokens of billed work)` — which names the instrument and not the ending. It now names the bound that fired: `it was still working when it ran out of its token budget (cost: ...)`."
  - "`resident.outOfRoomFailure` used to read `it ran out of room on every attempt and was never able to finish (...)` for both ways of arriving there, and took only the result. It takes the recorded turn count as well and says which: `...on every attempt it was given, and was never able to finish`, or `..., with none of its work recorded, so there was nothing for another attempt to carry on from`."
  - "The word for what a leaf ran out of is `exec.RanOutSubject`, spelled once for both ends of the exec-to-resident seam. `cmd/aforge`'s `exhaustionWords` no longer keeps its own table, and the budget ending now reads `its token budget` where it read `its tokens`."
---

The evidence was an uncommitted test found in a working tree after the #271 wave and
preserved with the issue. It hands the scheduler exactly what a landed leaf produces — no
error, the bound that fired, the meter that named it — and on `dev` the node was settled
done and its release said nothing about the budget.

Two independent defects, one law. A flag with one writer and one reader, set in the same
breath as the ending it guarded, was standing between a worker's real work and the queue:
it could not add anything the ending word did not already say, and it could throw work
away. And the sentence a person reads named the reading rather than the ending, so somebody
watching a run had to know that "cost" is the instrument and "budget" is what ran out.

A leaf that ran out with turns banked goes back on the queue and the next claim carries on
from them; one that recorded nothing is still not resumable and still fails, with the bound
named. Bounded by `MaxOverrunRounds` exactly as before.
