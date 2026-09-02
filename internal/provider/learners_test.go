package provider

import lanes "github.com/Agent-Field/aforge-v2/internal/lane"

// resetSharedLearners puts every package-level learner back to a fresh one.
//
// A PACKAGE-LEVEL LEARNER IS RESET BY THE RIG BETWEEN TESTS, so a test's result
// never depends on which test ran before it. `sharedVelocity` is what bit:
// `TestARefusedRescueWalksToTheNextLaneRatherThanRelaxingTheRequest` passed only
// as the first run of its model in a process, because the ledger every client
// folds into still held what the previous run taught it about the refusing lane,
// and the second run walked a ladder the first had already reshaped (#432).
//
// SO THIS FUNCTION MUST LIST EVERY SUCH LEARNER AND NOT ONLY THE ONES THAT HAVE
// BITTEN. The list is a sweep of this package's own `var`s — a process-wide value
// that accumulates what it saw belongs here — and a learner added without a line
// here is the same bug again under a different name. Three today:
//
//	sharedVelocity  velocity.go   what each lane was measured doing
//	sharedPins      affinity.go   which endpoint holds a prompt lineage's cache
//	sharedLimiter   limiter.go    the concurrency a key was seen to tolerate
//
// TWO DELIBERATE ABSENCES, so the next reader does not think they were missed.
// `quirks` (quirks.go) is a learner and is NOT reset here: it is not a plain
// reassignment — it carries a `loaded` flag over a file on disk and a `writes`
// WaitGroup whose save races a removed profile directory, so giving it a fresh
// value would re-trigger a load and orphan a save in flight. It needs a real
// reset seam in non-test code, which is its own change. And `offers` (offer.go)
// is questions in flight rather than anything learned, so it has nothing to
// forget.
//
// WHO CALLS IT. Two places, and both for the same reason: they leave their
// client on the shared learners and then assert on what those learners were
// taught. The hedge rig (`newLaneRig`, hedge_test.go) is one, which covers every
// scenario in hedge_test.go and ladder_test.go. `TestADataPolicyRefusalDrops`
// `TheCeilingAndTeachesTheLedger` is the other, and it is not on the rig — it
// teaches `noCeiling`, a memo inside the velocity ledger that is deliberately
// process-lifetime and never expires.
//
// Twenty-nine test files in this package build a client with `NewClient` and
// every one of them folds into these same three learners. The rest are safe
// because they either assert on request counts, bodies and logged rows — which
// no learner reshapes — or take the isolation the ledgers' own comments describe
// and overwrite the client's field with a fresh one, the way
// `streamguard_test.go`'s `streamAgainstCtx` does with `client.velocity` and
// `affinity_test.go` does with `client.pins`. That per-client override is there
// because this same fault bit the guard tests once already. A rig that starts
// reading a learner back takes this call in its own cleanup rather than a third
// private override.
//
// THE FOURTH LEARNER IS NOT THIS PACKAGE'S OWN, and it is reset here on the
// same law. #368 landed `refusedLanes` in `internal/lane` (sheet.go) — the
// negative half of the serving set — behind the reset door
// `lanes.ForgetRefusals()`. It is process-wide state that accumulates what it
// saw, so a scenario that refuses a lane would otherwise hand that refusal to
// every test running after it. This function is the ONLY place in this package
// that names it: a rig keeping its own copy of the call would be a second list
// to hold in step with this one, which is the fault this helper exists to end.
func resetSharedLearners() {
	sharedVelocity = newVelocityLedger()
	sharedPins = newEndpointPins()
	sharedLimiter = newAdaptiveLimiter()
	lanes.ForgetRefusals()
}
