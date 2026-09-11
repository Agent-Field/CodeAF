---
kind: fixed
title: An answer the door never acknowledged is still your answer, not another window's
pr: 812
surface: [chat]
invalidates:
  - "The two subtests lane H's parity bench recorded as diet-only regressions are not: `TestTUIE2E/space_in_the_task_room_pages_the_card` fails on `dev` at 6aa6a946e too (1 run in 4 measured on the Spark, same shape — the checker did not answer, the node landed `your call`), and `TestQuestionsE2E/TheOrdinaryRoadCarriesAQuestionAndItsAnswer` is a pre-existing surface race, not a byte the diet cut."
  - "`decided … · another window ·` on a question you answered yourself was never proof that another window answered it. Until now the block read `the question is still open here` as `somebody else decided it`, which is wrong for the one window whose answer crossed the wire and whose reply did not come back."
---

Measured to the millisecond: six copies of the ordinary road's own e2e at once,
and the one that failed spent exactly ten seconds — `internal/remote`'s
`callDeadline` — before its answer came back as `the connection to the engine is
gone`, while the engine had applied it and the model had already said `They
picked "delete it"`. The five that passed round-tripped in one millisecond.

`internal/tui3`'s question block wrote its receipt only after
`ResolveQuestion` came back. The engine runs in its own process even on this
machine, so that call applies the answer, emits `EventQuestionAnswered`, and
writes its reply afterwards: a reply that is lost leaves the question open here
with the engine already settled, and the lane's news then reads exactly like
somebody else's answer. The block now remembers what it sent BEFORE it asks the
door, so an answer coming back with the same keys closes as yours — and one that
came back with different keys still wears the other window's name, because FIRST
ANSWER WINS has not moved.
