---
kind: fixed
title: the reading that tells a model what other windows did no longer writes into a closed conversation
pr: 997
surface: [engine]
invalidates:
  - "`Agent.refreshElsewhere` ran on a bare `go` of its own, started by the turn loop, taking no context and answering to nobody. Nothing joined it and `Agent.Close` never waited for it, so it went on stamping `told.json` in the conversation's folder after that conversation had closed. It goes through `readBeside` now (sidecar.go, the ONE door for a reading beside the work — it exists because four such readings each had their own idea of a cancellation, and this was a fifth with none): it is handed the reading's own context and obeys it before the walk and again before the assignment, and it is counted by the beside-watch a fixture can wait on. `Agent.tellsElsewhere` is asked at the call site too, so a task node's turn and a conversation with no folder start no reading at all."
  - "The delta stamp was written with `os.Stat` + `json.Marshal` + `os.WriteFile` on the reading's own goroutine, and — in the first draft of this fix — under `a.mu`. It is OWED under the lock and performed behind it now (`Agent.toldStamp`, placemeta.go's `stampWriter`), which is what #876 did to the meta stamp for the same reason: `a.mu` is the lock a steer, a cut, a named interruption and every drain take before a request leaves. `Agent.SettleWrites` settles it, so a test that reads `told.json` back settles first — that door's own stated contract."
  - "The stamp advanced BEFORE the block was assembled, ahead of the index walk. It is owed in the same locked step as the assignment now, and a session that has closed owes nothing: the owe is registered while the session is still open, which puts it ahead of `closed` and therefore inside the settle `Close` already performs. What the stamp can honestly claim is that the block was ASSIGNED — delivery is at the drain's `landVolatileLocked`, and `elsewhereTold` is in memory alone, so a process that stops between the assignment and the next request still loses those rows. That window used to be the whole of the walk plus every session that closed during one."
  - "`NoteTold` wrote whatever stamp it was handed. It only moves forward now (read, compare, write): two readings can be in flight over one conversation and they are written behind the path, so the order they land in is not the order they were taken in — an older stamp landing last would re-read landings the model was already told about, which `deltaRemember` then de-duplicates away into news nobody is ever told."
  - "`internal/session`'s load-dependent reds (#959) name a different test each full run, and at least one of them was never an assertion at all: `TestAPickTakenByTheClockReadsAsProvisionalAndRunsWithNobodyParked` failed 13 times in 5000 runs under load as `TempDir RemoveAll cleanup: directory not empty`, and the file left in the directory every time was this stamp. A red of that shape names whichever test happened to own the directory, not the test that caused it. #959 is NOT closed by this: a fixture counting the steering queue while a woken turn drains it, and a data race between a package-level variable a test writes and a previous test's `beatHeldPhase` goroutine, are two more faces of the same family and are still open."
---

A goroutine nobody owns is a goroutine that outlives the session, and what it
wrote was a claim about what the model had been told. The three halves of the fix
say the same thing from three ends: the reading belongs to the door that knows
what a cancellation means, the write belongs behind the path rather than under
the lock every request goes through, and neither may happen for a conversation
that has closed.
