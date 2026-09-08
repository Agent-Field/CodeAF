---
kind: fixed
title: the rig forgets the quirks memo between tests, so a learned repair is not the next test's first move
pr: 459
surface: [engine]
invalidates:
  - "`internal/provider` was believed repeat-safe because CI is green. CI runs `-count=1`. `TestARepairedRefusalLeavesTheRefusedShapeAndThenTheAnswer` passed only as the first run of its model in a process, and failed at `-count=2` on a clean `dev`. It passes at `-count=3` now, and the whole package passes at `-count=2`."
  - "The quirks memo (`quirks.go`) was the learner `resetSharedLearners` recorded as a deliberate absence, on the grounds that it wants a reset seam in non-test code. That seam exists: `(*quirksStore).resetForTests()`. The helper's list is four learners now, and the only absence left is `offers`."
  - "A test that wanted the quirks memo clean had to name its models to `quirksAt` and have them deleted one at a time. There is now one call that empties every one of the store's seven maps, waits for the writes in flight, and puts `path` and `loaded` back to what they are before `LoadQuirks`."
  - "`loggingTo` in `calllog_test.go` only pointed the call log at a temporary file. It is the rig for the tests in that file and now also calls `resetSharedLearners` on the way out, because a run filtered down to one test there never builds a lane rig."
---

The test stands up a stub that refuses its first call and answers its second,
and asserts the call log holds two finished rows: the 400 that taught the
adapter, and the repair that landed. Run 1 does that. Run 2 in the same process
already knows the model will not have its reasoning turned off — the memo told
it — so its FIRST request is the repaired one, the stub's first-call branch
refuses it anyway, and there is no second call left to repair. One row, and the
assertion that wanted the refused shape and then the answer sees only the
refusal.

The law is the one #432 landed: A PACKAGE-LEVEL LEARNER IS RESET BY THE RIG
BETWEEN TESTS, so a test's result never depends on which test ran before it.
The quirks store is the learner that cannot be reset by reassignment — a fresh
value would leave the old one's scheduled save still holding a descriptor into a
profile directory its test is about to remove — so it grows the reset itself.
`resetForTests` forgets first and waits second, which is the opposite of the
obvious order and the only sound one. A save schedules itself with the mutex
already released, so a reset that waited first would return from `Wait` and then
race an `Add` against the WaitGroup it had just waited on; waiting under the lock
deadlocks against the snapshot. Emptying the maps and the path under the lock
disarms every writer instead — a save already scheduled reads the path under the
lock, finds it empty, and writes nothing — and the wait afterwards is only there
to see those writers off the profile directory before the test removes it.

It leaves the file on disk alone, and clearing the path is what makes that safe:
the path is set by `load` and nowhere else, so a store with no path can neither
save nor be re-read. The only way the file is learned back is a test pointing
`LoadQuirks` at that directory on purpose — which is `quirks_test.go` asserting
that a memo on disk is believed at startup, and deleting the file would break
exactly that.

It is unexported and its name says who it is for. The memo's whole value is that
a fact learned by being told no survives the call, the process and the file, so
a production caller that forgot it would buy back the rejected call the memo was
written to spend once. Nothing on a production path may call it.
