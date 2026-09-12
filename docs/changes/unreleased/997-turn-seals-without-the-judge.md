---
kind: changed
title: the turn seals when the answer is done, and the route judge is spent when it lands
pr: 997
surface: [chat, engine]
invalidates:
  - "The end of a turn waited for BOTH end-of-turn readers. It waits only for the one that can re-open the turn; the work-or-words judge no longer holds the seal, and a yes that lands after the turn ended starts its task then."
  - "The status line said `whether that should be work` at the end of a turn. That line is deleted, because the wait it named is gone."
  - "`judgeRace.takeAtTheEnd` existed and blocked. It is `judgeRace.spendWhenItLands`, which hands the ruling over when it arrives and never waits."
  - "Every reading in `internal/session` ran on the turn's own context. The post-turn judge runs on the session's lifetime (`afterTurn`, sidecar.go), because its effect — starting a task — lands outside the turn anyway."
  - "`journalPace` carried `sendMs`, `firstWordMs` and `stepGapMs`. It carries `sealMs` — the model's last word to the seal — and `reaskMs`, how long the request a recall threw away was on the wire."
  - "`loop_speed_test.go` asserted two figures. It asserts three: the seal must not be held by any reading whose answer could be spent afterwards."
---

The post-turn judge stood between the model's last word and the seal, blocking on
a cheap screen plus, on a yes, a mastermind confirm — with the answer already
fully written on the person's screen. Parked behind it were the seal, the usage
row, the next Submit and the follow-up drain.

The turn's job was never to know whether the ruling had arrived; it was to say
when one may be spent. It still says exactly that and walks away.

`reaskMs` is an instrument and not a fix. The pace ledger holds three rows and
one of them has a recall beside it, so the share of turns that pay for a
cut-and-re-ask cannot be derived yet. What the one row does show is that the
≤50 ms law can be satisfied by a request that is then thrown away: `sendMs` read
1 on a turn where the person waited 7,389 ms for their first word.
