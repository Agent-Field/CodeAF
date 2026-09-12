package remote

import "strconv"

// ratchetComplaint is what a ledger that may only shrink says when it has not.
//
// IT WAS internal/ci's SENTENCE, SAID ONCE FOR THE SECOND LEDGER. That package's
// known-red ratchet — burned to zero and deleted in #1012 — and this package's
// [surfaceDoorLedger] were the same shape: a list of things that are wrong, a
// number beside it, and a rule that the number only comes down. A rule spelled
// twice is two rules that will one day disagree about which direction is the red
// one. The second branch is the half that is easy to forget and is the reason
// both exist: A RATCHET LEFT SLACK WOULD LET THE NEXT CHANGE PUT AN ENTRY BACK
// WITH THE GATE GREEN THROUGHOUT.
//
// It lives in a test file because both callers are laws, and a law's vocabulary
// is not something the shipped binary should carry (the size budget, SIZE-BUDGET).
func ratchetComplaint(ledger string, entries, ratchet int) string {
	switch {
	case entries > ratchet:
		return ledger + " holds " + strconv.Itoa(entries) + " entries and the ratchet is at " +
			strconv.Itoa(ratchet) + ". There is no road to a new entry: the ledger only shrinks. " +
			"Land the door across the wire and delete its line instead."
	case entries < ratchet:
		return ledger + " is down to " + strconv.Itoa(entries) + " entries and the ratchet still says " +
			strconv.Itoa(ratchet) + " — lower it to " + strconv.Itoa(entries) + " in this change. A ratchet " +
			"left slack would let the next change put an entry back with the gate green throughout."
	}
	return ""
}
