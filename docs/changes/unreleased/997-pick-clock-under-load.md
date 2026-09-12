---
kind: fixed
title: the reading that tells a model what other windows did no longer writes into a closed conversation
pr: 997
surface: [engine]
invalidates:
  - "`Agent.refreshElsewhere` ran on a bare `go` of its own, started by the turn loop. Nothing joined it and `Agent.Close` never waited for it, so it went on stamping `told.json` in the conversation's folder after that conversation had closed. It goes through `readBeside` now (sidecar.go, the ONE door for a reading beside the work — it exists because four such readings each had their own idea of a cancellation, and this was a fifth with none), so it is cancelled with the turn and counted by the beside-watch a fixture can wait on."
  - "The delta stamp advanced BEFORE the block was assembled, on the reasoning that the block was going into the next request either way. It is written in the same locked step as the assignment now, and a session that has closed writes neither. What the old order bought was a stamp no panic between the two lines could skip; what it cost was a stamp claiming `this session's model was told` about a block no model ever read — a day of other windows' landings that conversation would never mention again. The repeat the new order can cost is one bounded first delivery, which `deltaRemember` already de-duplicates."
  - "`internal/session`'s load-dependent reds (#959) name a different test each full run, and at least one of them was never an assertion at all: `TestAPickTakenByTheClockReadsAsProvisionalAndRunsWithNobodyParked` failed 13 times in 5000 runs under load as `TempDir RemoveAll cleanup: directory not empty`, and the file left in the directory every time was this stamp. A red of that shape names whichever test happened to own the directory, not the test that caused it. #959 is NOT closed by this: a fixture counting the steering queue while a woken turn drains it, and a data race between a package-level variable a test writes and a previous test's `beatHeldPhase` goroutine, are two more faces of the same family and are still open."
---

A goroutine nobody owns is a goroutine that outlives the session, and what it
wrote was a claim about what the model had been told. Both halves of the fix say
the same thing from two ends: the reading belongs to the door that knows what a
cancellation means, and the stamp belongs where what it claims becomes true.
