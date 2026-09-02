package resident

import (
	"strings"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
)

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
	return ranOutWords(result.Stop) + meterAside(result.Meter) + recordedTail(recorded)
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

// A RELEASE REASON THAT HANDS WORK ON ENDS WITH THE SAME CLAUSE, AND IT IS
// SPELLED HERE ONCE.
//
// Two different endings put a claim back on the queue with a record behind it —
// a leaf that ran out of its room, and a claim taken back from a worker that
// stopped answering — and both sentences end by saying how much survived and
// that the next claim starts from it. The headless stream then cuts that clause
// back off, because the line it writes says the count in its own words and a
// line that said it twice would read as two different numbers (see
// [ReleaseWhy], and cmd/aforge's narrateOne on store.EventNodeReleased). A join
// and a cut that each carried their own copy of the wording would drift apart on
// the first edit, so there is one copy and the cut is derived from it.
const (
	// recordedSeam separates why the claim went back from what survived.
	recordedSeam = " — "
	// recordedSuffix is the rest of that clause, after the count.
	recordedSuffix = " of its work is recorded, and the next attempt carries on from there"
)

// recordedTail is the clause every release reason that hands work on ends with.
func recordedTail(recorded int) string {
	return recordedSeam + pluralTurns(recorded) + recordedSuffix
}

// ReleaseWhy is a release reason with [recordedTail] taken off: what stopped
// the claim, and nothing about how much of its work survived.
//
// It exists so the headless ↻ line can name the bound that fired WITHOUT
// repeating the turn count it has already said in its own words. A reason this
// package did not compose — or one with no such clause — comes back whole,
// which is the honest answer: the caller asked for the why, and the whole
// sentence is the why.
func ReleaseWhy(reason string) string {
	end := strings.Index(reason, recordedSuffix)
	if end < 0 {
		return strings.TrimSpace(reason)
	}
	seam := strings.LastIndex(reason[:end], recordedSeam)
	if seam < 0 {
		return strings.TrimSpace(reason)
	}
	return strings.TrimSpace(reason[:seam])
}
