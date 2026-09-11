---
kind: fixed
title: A reading beside the work is waited on, not guessed at, and a held memory line is not lost
pr: 952
surface: [chat, engine]
invalidates:
  - "A HELD MEMORY LINE IS NO LONGER SAID INTO THE DARK. `Agent.refreshMemory` took the lines the post-turn pass had no stream for off the queue and said them onto the hub the reading had been HANDED when it started (`memoryNotice(hub, line)`). A recall rides beside the turn and is never joined, so on a busy machine it runs after that turn has ended — measured on this package's own suite beside another one, where every losing run logged `ctxErr=context canceled hubClosed=true` — and the line was taken, said to a closed stream, and never said again. Every memory line now goes through `Agent.sayMemory`, and that send is under `a.mu`: every road that ends a turn clears `a.hub` under that lock before it closes the hub, and a starting turn holds it until its stream is subscribed, so there is no instant at which a line is sent onto a stream nobody can read."
  - "`memoryNotice` IS GONE, AND SO ARE THE CAPTURED HUBS. `Agent.routedMemory` and `Agent.runMemoryCommand` take `say func(string)` where they took `hub *eventHub`, and nil says nothing — which is a task node's reading borrowing its parent's store, exactly as silent as it was. `Agent.refreshMemory(ctx, cue)` and `Agent.importMemoryFile()` no longer take a hub at all. `Agent.startRecallLocked` still receives the turn's hub from agent.go and no longer uses it; the argument goes when that line is next touched."
  - "A READING'S LANDING IS A FACT A TEST CAN WAIT ON. `readBeside` (sidecar.go) tells a `besideWatch` carried on the work's context when each reading under it STARTS and when it has LANDED, the second after the settle closes, so whoever the watch lets go of finds the answer there to take. `besideWatch.quiet` waits until none is in flight. It is `desk.settled`'s bargain for the other half of that file: THE RUNNING PRODUCT CARRIES NO WATCH AND NEVER WAITS ON ONE, and `TestTheRunningProductNeverWaitsOnAReadingsLanding` reads the tree with `go/ast` — so it is on the laws gate — and fails the build on either of the two ways the product would reach for one."
  - "THREE FIXTURES NO LONGER GUESS AT THE SCHEDULER. A scripted conversation answers in no time and never blocks, so a reading the turn has just started can sit unscheduled behind it for as many rounds as the machine is busy: in every failing run the mark's drawing was still pending at all eight remaining boundaries, the script ended in words, and `markAside.takeAtTheEnd` journalled a drawing nobody could spend. A fixture that opts in with `watchReadings(t, agent)` answers the conversation only once every reading beside its turn has landed (`answerWhenQuiet`, applied by `scriptedCompleter`, `routeCompleter` and `reflexScript` to the request that carries the belt), which is the order a real turn usually has: a real model spends seconds on a step, the readings beside it are small, and a drawing that splits cuts the step it lands in. `racingAgent`, `stewardCheckpointAgent` and the three route tests that held their first answer take it."
  - "`routeCompleter.holdUntilRaced`, `awaitRace` AND `stopHolding` ARE DELETED. Holding the conversation until the pre-turn calls had been ENTERED — in a loop that slept a millisecond at a time — said nothing about the verdict having been parsed and settled, which is what the boundary after the first word kept failing to find. Nothing sets a hold count now; the watch is the whole of it."
  - "A SCRIPTED ARM ORDERS ITSELF BY A SIGNAL, INCLUDING ITS FIRST TOKEN. `TestARescueTheAllowanceRefusedSaysSoOnTheRow` scripted lane A at sixty milliseconds against a ceiling of fifty, and a starved timer spent that ten-millisecond margin: the first token arrived before the controller acted and the row said nothing had been done at all (`action = \"\"`). `laneRig.answersAfterTheWord` gives the stub a clock of the scenario's own through `lanestub.Server.SetClock`, whose every wait ends no sooner than it was scripted to AND no sooner than the controller has spoken — `lanestub.Profile.StallUntil`'s stated rule, for the one wait `StallUntil` cannot hold. A switch releases it as well, so a regression that sends the refused rescue fails on the rescue rather than hanging."
  - "`docs/rules/ci.md` SAYS WHAT TO DO WITH A TEST THAT FAILS ONLY BESIDE ANOTHER SUITE — reproduce it under load, wait on the fact through the door that owns it, and never `t.Skip`, a retry or a wider bound. It is on that page because the known-red ledger's rules are, and because nothing may be added to that ledger."
---

Five tests were green alone and red beside another suite, a different one each
run, none of them on the ledger. One was the product's: a memory line a person
is owed could be taken off its queue by a reading that had outlived its turn and
said to a stream that had already closed. The other four were tests that
measured the scheduler and called it the road — a count of the calls a fixture
had answered, a script long enough on a quiet machine, ten milliseconds of wall
clock between a ceiling and a first token. What replaces all three guesses is a
wait on the fact itself.

Still true and not fixed here: a drawing that splits can miss the boundary its
own cut opened, because `act` runs before the settle by #871's ruling and a loop
that reaches `take` in that window finds the reading pending — it pays another
generation for the drawing, or drops it when that generation is the turn's last.
The same gap is there when the drawing lands between a boundary and the next
request, where `Agent.cutGeneration` finds nothing in flight to cut.
