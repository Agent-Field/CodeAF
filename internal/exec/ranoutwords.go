package exec

// RanOutSubject names WHAT A LEAF RAN OUT OF, in the words a person would use
// rather than in the loop's own one-word ending.
//
// It lives beside [StopReason.OutOfRoom] and for the same reason: the answer
// travels. The record and the headless stream say it at the moment the leaf
// stops (cmd/aforge's exhaustionWords), and the scheduler says it again on the
// other side of the seam when it hands the node back to the queue
// (resident.outOfRoomClaimReason) — and for as long as each of them kept its own
// table, one sentence could name the bound that fired while the other named only
// the meter that measured it, about the same leaf, three lines apart.
//
// An ending nobody recorded, and any ending that is not a leaf running out, gets
// the neutral phrase: a reason a reader cannot name is worse than a general one,
// and this is only ever composed for a leaf that was still working.
func RanOutSubject(stop StopReason) string {
	switch stop {
	case StopBudget:
		// "budget" and not "tokens": the tokens are the meter's unit and appear
		// in its figures already, while the budget is the bound that fired, and
		// a sentence that names only the unit leaves a reader counting.
		return "its token budget"
	case StopTurnCap:
		return "its turns"
	case StopDeadline:
		return "its time"
	case StopOverrun:
		return "far more than this kind of work usually takes"
	}
	return "the room it was given"
}
