---
kind: fixed
title: four flaky tests now measure the claim rather than the machine they ran on or the hour
pr: 199
surface: [engine, build]
invalidates:
  - "`internal/lane`'s scenario 2 (`TestS2TheDefaultGoesSlowAndTheRouterMoves`) scored the design's ship gate on the whole answer's wall clock. It scores it on the FIRST TOKEN, which is what `ideation/provider-routing.md` C3 gates a talk scenario on and the only part of an answer this hundredfold clock can measure — the generation phase is twenty-four timer sleeps, and `e2eLane.stub`'s own note already called a rate read off this socket a measurement of the scheduler. On a quiet box about two thirds of every measured healthy answer was the test machine multiplied by a hundred, sitting in both halves of a RATIO where it does not cancel."
  - "`e2eRouter` recorded only `answers`, the time to a finished answer. It records `firsts` beside it, measured from the moment the request went out and NOT from the winning stream's own start — a hedge starts its second stream late, and the alternate's own first token would credit the design with a wait it did not save."
  - "`TestATaskWhoseShapingFailedIsNamedAnyway` said in a comment that the shaping calls are exhausted before admission \"so the order is fixed\". It never was: admission starts the naming errand AND the node, whose finish wakes the conversation into a turn of its own, and nothing orders those two. A test in `internal/session` that scripts a shared completer by call index is scripting one caller in a package that rarely has only one; `isShapeCall`/`isNameCall` are how a call says which it is."
  - "`beatSheet` in `internal/session` offered `called`, closed on the FIRST refresh, and `TestOpeningASessionStartsTheLaneBeatAndClosingItStopsIt` waited on it and then asserted on two models. `lanes.Beat` walks its models one at a time and checks the context before each, so `Close` was racing the second refresh. The sheet now carries `refreshed`, one name per call, and `waitForRefreshes(t, n)` waits for as many as the assertion is about."
  - "`internal/store`'s three `PracticedToday` tests handed the read `time.Now()`, so what they asserted depended on the hour the suite ran at — a round that started twenty minutes ago starts YESTERDAY at ten past midnight, and the read correctly clamps it to today. They run at three chosen instants now, `23:59:59`, `00:00:01` and `14:30:00` local, with the clamped expectation."
  - "`cmd/aforge-demo-home` reached a day's bucket by adding an offset to `now` itself, so a demo home built within an hour of midnight put today's standing firing on yesterday and today's turns on tomorrow — the spend page's cost-per-firing clause drew nothing on a fixture whose whole purpose is to have something on every place. `demoMoment(now, daysAgo, offset)` chooses the day first, applies the offset inside it, and clamps to the day at both ends and to `now`. A fixture line is never stamped in the future now either, which it could be at any hour of the day."
---

Four tests, four causes, and not one of them was a wait that needed lengthening.
Three failed only under load and one only when a run crossed local midnight, and
in every case what the test measured was partly the machine or the clock rather
than the claim: a ratio of two wall-clock percentiles taken through a loopback
socket on a hundredfold clock, a call index standing in for a call's identity,
a wait for one signal in front of an assertion about two, and "today" read off
the wall on both sides of a seam that already took its clock as an argument.

The four are `internal/lane TestS2TheDefaultGoesSlowAndTheRouterMoves`,
`internal/session TestATaskWhoseShapingFailedIsNamedAnyway` and
`TestOpeningASessionStartsTheLaneBeatAndClosingItStopsIt`, `internal/store`'s
`PracticedToday` family, and `cmd/aforge-demo-home TestTheDemoHomeFillsEveryPlace`.
None was in `.github/known-red.txt` and none is added to it: they are fixed
rather than described, which is what the ledger's own rule asks for.
