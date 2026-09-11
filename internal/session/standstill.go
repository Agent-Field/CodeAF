package session

import "strings"

// ── THE STANDSTILL FLOOR ────────────────────────────────────────────────────

// standstillFloor is the floor under carrying on, and it is the ONE mechanism
// every principal that can say carry on decides a standstill with — [Person]
// and [Steward] both, on the same rule and in the same words
// ([standstillReason]).
//
// A STANDSTILL IS A STOP AND NOT A CARRY-ON. The evidence that a run is getting
// somewhere is that what is left CHANGES. A reading that says exactly what the
// reading before it said has learned nothing from the turn just spent, and
// carrying on with it buys one more answer to an observation that has already
// been answered — which on an unattended road is the same money against the
// same wall (#468) and on an attended one is an argument the person reads two
// turns of and never typed a word into (#888).
//
// IT REMEMBERS ONE READING AND NOT A SET OF THEM, because the question it
// answers is narrow: does this reading say anything the last one did not? A
// third reading that differs is news however many echoes came before it, and
// what bounds a reader that never repeats itself is the road's own ceiling —
// the budget on one road, [checkpointCarryOnCap] on the other.
type standstillFloor struct {
	// seen and brief are the last reading this floor was shown: what the work
	// and the checks left over, and the line a reader wrote on top of it. BOTH
	// ARE COMPARED, because either one standing still is the same event, and a
	// change in either is progress enough to go again.
	seen  string
	brief string
	// held says a reading has been taken at all, so the FIRST one is never
	// mistaken for an echo of the empty pair this starts life as.
	held bool
}

// moved reports whether this reading says anything the last one did not, and
// remembers it either way. It is the whole of the decision: a caller that gets
// false is about to say what it already said.
func (f *standstillFloor) moved(seen, brief string) bool {
	if f.held && f.seen == seen && f.brief == brief {
		return false
	}
	f.seen, f.brief, f.held = seen, brief, true
	return true
}

// forget drops what the floor remembers, so the next reading is a first one.
//
// It is called wherever the STRETCH the floor is about has ended: an ask that
// finished, work that is still moving under it, and — on the attended road —
// the person saying something new, which starts a stretch of its own.
func (f *standstillFloor) forget() { *f = standstillFloor{} }

// standstillReason is what a standstill says, in the register every stop on
// this road wears: an observation, and then what is being done about it.
//
// IT NAMES WHAT IS STILL LEFT and it says the run stopped RATHER THAN REPEAT
// ITSELF, which are the two things somebody coming back to a stopped session
// needs — the second one because a run that went quiet on its own is otherwise
// indistinguishable from one that crashed.
//
// ONE SENTENCE FOR BOTH ROADS. An attended session and an unattended one stop
// at a standstill for identical reasons, and a second spelling of it would be
// the same fact read two ways by the same person.
func standstillReason(left []string) string {
	return "nothing moved since the last look and what is left is the same — " +
		strings.Join(left, "; ") + " · saying it again would not change it"
}
