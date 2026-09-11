---
kind: fixed
title: a decayed belief draws no tail, and the greeting's spend test stops turning on the hour
pr: 862
surface: [chat]
invalidates:
  - "The lane picker decided a belief had aged past saying anything about its worst case by catching the OVERFLOW: a lane nobody had heard from since yesterday had a p99 of +Inf, and `laneTail`'s IsInf arm refused it. #852's `lane.MaxSpread` clamp — a floor under the arithmetic, not a judgement — made that p99 finite, so the same belief arrived at the ceiling (σ ≈ 2.4, p99 ≈ 212 s against an 800 ms median) and the row drew `tail 212.7s`. Vagueness is now read off the BELIEF (`laneTailOf`: a spread at `lane.MaxSpread` draws nothing at all — not a figure, and not `no tail`, which is a claim about the worst case too). `laneTail`'s own arithmetic gate is unchanged."
  - "`TestTheGreetingsFirstFrameCarriesTheSpend` was believed to be a regression #857 or #849 introduced. It is neither: it wrote its ledger line an hour before a real `time.Now()`, and `today` is a calendar day in the person's own zone (`session.SpendToday`) — so it was green for twenty-three hours a day and red for the hour after midnight, at #842 where it landed and at every commit since. The surface's arithmetic was right on both sides of midnight and does not move; `homeLab` grew a pinned clock (`homeLab.pin`, midday of the day it is asked for) and the test writes its fixture against that."
  - "A `homeLab` test whose subject is a calendar day — a `today`, a fortnight, a `yesterday` — must call `homeLab.pin` before `homeLab.app` rather than passing `time.Now()` around. A lab that pins nothing still reads the real wall clock, which is right for every test whose subject is not a date."
---

Both were on `dev` with nothing in `.github/known-red.txt` naming them, and
neither goes on it: that ledger only shrinks. Proved by `internal/tui3` in full
on the Spark with `-count=1`, and the two named tests run across a sweep of
zones that put the wall clock at 23:52, 00:52 and 04:52.
