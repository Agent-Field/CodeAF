---
kind: fixed
title: a cut stream's bill is asked for from the provider's own receipt, and a price nobody has is said
pr: 631
surface: [engine, chat]
invalidates:
  - "A remote background duty released its latch before recording its fault and queuing its notice. It now finishes publication before releasing the latch, so the next trip cannot pass a fault still being announced."
  - "A spend window containing only missing-price markers took the no-spending teaching path. The page now treats those markers as a real reading and displays their count even when no priced call exists."
  - "Review found that missing prices lived only in a process counter and never reached /cost. They now persist as attributed unbilled markers without invented money; /cost reads this conversation and its task descendants, and /spend reads its selected time window after restart."
  - "The first receipt permanently started four workers for every transient client. Workers now retire when their queue drains, with admission and retirement serialized so a later receipt restarts them safely."
  - "A streamed call that ended without a usage block — cut by its wall, cut by silence, torn, interrupted, or a hedge's losing arm — reached neither the conversation's meter nor `~/.aforge/v3/usage.jsonl`, and no surface said so. The provider's generation id is asked for that generation's own receipt in the background now, and the receipt's exact cost and token counts are written down late; the ledger still invents nothing, but the gap is no longer silent."
  - "`internal/provider/billing.go` had one door, `WithBilling`, and a response with no usage block was simply dropped there. There is a second door beside it, `WithReconcile`, told only what became of a call the wire never priced — the two are mutually exclusive, so one call can never be banked through both."
  - "A row in the usage ledger was always the provider's usage block off the wire. A row may now carry `reconciled: true`, which says its figures came from the provider's generation receipt after the stream ended; every row without the field means what it always did."
  - "`session.UsageDrops` was the only shortfall a spend surface could report — records the disk would not take. `session.UnbilledCalls` sits beside it for calls the provider charged for and nothing could price, said on Settings→Spending and the `/spend` place as `2 calls the provider charged for and could not be priced`. Both draw nothing at zero."
  - "`hedge_waste_usd` on a rescued call was always `hedgeRace.estimate`'s proportion of the frontier's price. A losing arm whose receipt can be had now writes its own row carrying the receipt's figure, so the waste column is a measurement wherever one exists and an estimate only where it does not."
  - "The first draft incorrectly called /cost an alias of /spend. /cost remains the conversation and its task family, while /spend opens the machine ledger; both now expose missing-price markers in their own scope."
---

The audit that closed the books on the 2026-08-31 dogfood run found the ledger
correct to the cent and named one class it does not record: money on a stream
that was cut. `billing.go` refused to guess at a figure, which is right, on the
promise that "the gap is visible as a call the ledger did not see" — and the gap
was visible nowhere. Three cut streams in one run carried 76k, 79k and 53k
prompt tokens that the provider processed, charged for, and this machine never
heard about, in the flattering direction.

The fact that was missing is that the stream has already named its generation id
by the time it is cut, and the router serves an exact receipt for that id. So
the id is what the money is asked for, on a bounded background schedule that no
reply ever waits on. A receipt that cannot be had writes an unbilled marker without a guessed
number. The gap is counted and said now, which
is the same sentence the missing-writes reading already makes about the other
shortfall (290-one-ledger-one-number).

A call that produced neither a generation id nor a token is not counted at all:
a refusal delivered inside a 200 before the first frame is an upstream breaking,
not money anybody spent.
