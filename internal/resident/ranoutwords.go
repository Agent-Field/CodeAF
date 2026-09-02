package resident

import executor "github.com/Agent-Field/aforge-v2/internal/exec"

// The two sentences a node gets when the leaf holding it ran out. They are in
// one file because they are one account of one event told to two different
// endings, and a reader who meets them a day apart must not have to work out
// whether they are describing the same thing.
//
// EACH ONE NAMES THREE FACTS AND NOTHING ELSE: what stopped the leaf, how far it
// had got by the meter that measured it, and what survives for whoever comes
// next. The first of those was missing for as long as these sentences were
// composed from the meter alone — the meter is named "cost" and the bound that
// fired is the budget, so a node handed back for running out of its tokens said
// only `cost: 178086 of 176834`, which names the reading and not the ending. A
// person reading a run should not have to know that "cost" is the instrument.

// ranOutWords opens both sentences: the leaf was still working, and this is what
// it ran out of. The word for the bound is [executor.RanOutSubject], which is
// the same word the record and the headless stream use at the moment the leaf
// stops, so the two ends of the seam cannot name the ending differently.
func ranOutWords(stop executor.StopReason) string {
	return "it was still working when it ran out of " + executor.RanOutSubject(stop)
}

// meterAside is the bound's own figures, in brackets. A leaf can run out with no
// bound having written any figures down — a structural ending, or a meter that
// has no allowance worth printing — and then the sentence simply ends without
// them rather than printing a zero of a zero.
func meterAside(meter executor.Meter) string {
	if words := meter.Words(); words != "" {
		return " (" + words + ")"
	}
	return ""
}

// outOfRoomClaimReason is what a person reads when a leaf that ran out goes back
// on the queue: what ran out, how far it got, and that its work is kept.
func outOfRoomClaimReason(result ExecResult, recorded int) string {
	return ranOutWords(result.Stop) + meterAside(result.Meter) + " — " +
		pluralTurns(recorded) + " of its work is recorded, and the next attempt carries on from there"
}

// outOfRoomFailure is the same fact when there is no next attempt to hand it to,
// and there are exactly two ways to arrive here — so it says which. A leaf that
// recorded nothing leaves the next claim the same cold start it just paid for; a
// leaf that ran out on every round it was given has said all it is going to.
// Neither settles on a summary written by a worker that was cut off mid-sentence.
func outOfRoomFailure(result ExecResult, recorded int) string {
	tail := " on every attempt it was given, and was never able to finish"
	if recorded == 0 {
		tail = ", with none of its work recorded, so there was nothing for another attempt to carry on from"
	}
	return ranOutWords(result.Stop) + tail + meterAside(result.Meter)
}
