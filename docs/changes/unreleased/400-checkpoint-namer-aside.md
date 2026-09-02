---
kind: fixed
title: the checkpoint fixtures answer the namer by shape, so the early name spends no scripted step
pr: 400
surface: [engine]
invalidates:
  - "Three checkpoint tests failing about one run in five looked like a load flake or a #333 regression to revert. Neither: #333 is correct, and the fixture was scripting one positional queue that the namer's concurrent call stole a step from."
  - "`scriptedCompleter` answered every request by its place in the queue. It now consults an optional `aside` first, and a request the aside recognises is answered with no step spent and recorded in `asides` rather than `seen`."
---

The tests were `TestAContinuationSayingNothingIsLeftDropsTheCeilingHandover`,
`TestTheCeilingIsDroppedWhenTheReaderAgreesNothingRemains` and
`TestADowryOfMachineMarkupIsRefusedAndNeverBecomesTheName`, and none of them is
about naming. #333 asks for a task's name the moment a road decides to start
work, ahead of the node, on a goroutine of its own — which is what keeps the
told-after line from carrying the person's raw sentence and being renamed under
their eyes a moment later. The fixture had not caught up: landing on the
final-answer slot the namer's call left the turn to run off the end of the
script and end on `(unscripted)`, and landing on a grinding step it was answered
with that round's tool call and the task was announced as `Working through the`.

A scripted completer answers a concurrent errand by the shape of its request,
never by its place in the queue — `taskname_test.go` met the same race first and
already routes by `isNameCall`. `checkpointAgent` and `writeSeamAgent` now
install an aside that answers the namer with an empty name, which leaves the
title as the person's own words. Test files only; no step count moved.
