package exec

// The straggler, and why nothing used to happen to one.
//
// Measured, on a real run: one leaf consumed 150,000 prompt tokens — thirty-six
// times what each of its structurally identical siblings spent — took 31% of the
// whole run's bill on its own, and set the wall time of the barrier every
// sibling was already waiting at. Nothing in the system reacted to any of that.
// Not because nobody was watching the number: the loop counts its own spend on
// every turn and has done for as long as it has had a budget. It reacted to
// nothing because the only question it ever asked of that number was "is the
// grant spent yet", and the grant is a per-task ceiling sized for the worst
// honest leaf. A leaf at thirty-six times the median is comfortably inside it,
// and stays inside it for as long as it takes to spend the whole thing.
//
// The missing comparison is against the leaf's own kind. What one leaf of this
// worker costs is measured, journaled, and already read by four other decisions
// (internal/profile); it had simply never been shown to the one loop that could
// act on it while there was still something to act on.
//
// Three things this deliberately is not:
//
//   - It is not a cap. Nothing here stops a leaf. The threshold produces
//     evidence and asks a question; the answer comes from the same judge that
//     already decides what happens to a leaf that failed, and that judge is
//     entitled to say "carry on", which is what it says by default and what it
//     says whenever it cannot be reached.
//   - It is not a new number. The threshold is derived from the worker's own
//     measured spread (profile.Straggler), so a worker whose leaves genuinely
//     vary by an order of magnitude gets a threshold an order of magnitude out,
//     and a worker nobody has measured yet gets none at all and behaves exactly
//     as it does today.
//   - It is not a new decision. "Finish it, hand it back, or divide it" is the
//     judgement the system already makes about a leaf that ran out — the retry
//     judge picks who takes the next attempt, and the remainder judge decides
//     whether anything is left and plans it. All that changes is when it is
//     asked: while the siblings are still waiting, instead of after the money
//     is gone.

// OverrunWatch is one worker's measured threshold, installed on a task by the
// party that can both derive it and judge what to do when it is crossed.
//
// It is one field on Task rather than several because the four numbers and the
// judge are one decision that either exists or does not: a threshold with nobody
// to ask is a cap, and a judge with no threshold has nothing to be asked about.
// Nil — every path that has no measurement, which is every path that existed
// before this — means the loop never looks.
type OverrunWatch struct {
	// Threshold is the spend past which this leaf has stopped resembling
	// anything this worker has been measured doing, in raw tokens — prompt plus
	// completion, undiscounted, which is the unit the profile journals and
	// therefore the only unit a threshold derived from it can be in.
	Threshold int
	// Anchor is the median cost of a measured leaf of this worker: what one
	// piece of work of this kind normally costs here.
	Anchor int
	// Multiple is how many anchors the threshold sits at. It is derived from the
	// worker's measured spread rather than chosen, and it is carried so the
	// journalled evidence can say where the number came from instead of asking a
	// reader to take it on faith.
	Multiple float64
	// Samples is how many measured leaves the anchor and the multiple came from.
	Samples int
	// Judge answers the one question. Nil is OverrunContinue, which makes an
	// installed watch with no judge exactly as inert as no watch at all.
	Judge func(OverrunEvidence) OverrunVerdict
}

// OverrunEvidence is what can be said about the leaf at the moment it passes the
// point where its kind of work has ever been observed to finish.
//
// Every field is a measurement or a count. There is no verdict here and no
// suggestion of one: this is the evidence a judgement is made on, and the loop
// that assembles it is not the party that decides.
type OverrunEvidence struct {
	// Spent is what this leaf has read and written so far, in the same raw
	// tokens the threshold is in.
	Spent int
	// Turns is how many rounds it has taken to spend that.
	Turns int
	// Anchor, Threshold, Multiple and Samples are the watch's own numbers,
	// echoed back so that whatever journals this event records the comparison
	// rather than one side of it.
	Anchor    int
	Threshold int
	Multiple  float64
	Samples   int
	// Partial is what the leaf has said most recently — the work in hand, so a
	// judge deciding whether to let it finish is deciding about something rather
	// than about a token count.
	Partial string
}

// OverrunVerdict is the answer, and there are only two because there are only
// two things this loop can do about it.
type OverrunVerdict string

const (
	// OverrunContinue leaves the leaf exactly as it was. It is the zero value,
	// which means it is also what a nil hook, an unreachable judge, an
	// unparseable answer and a judge that says "the same worker should finish
	// this" all produce. Everything about this mechanism fails toward the
	// behaviour that existed before it did.
	OverrunContinue OverrunVerdict = ""

	// OverrunHandBack lands the leaf and gives the work back to whatever routed
	// it, which is the same exit a leaf takes when its budget runs out: a
	// landing reserve to leave the workspace consistent, the partial carried
	// out whole, and an ending that the existing escalation and continuation
	// paths already know how to read. It is a hand-back and never a kill — the
	// work is not abandoned, it is returned to the party that can send it
	// somewhere else or divide it.
	OverrunHandBack OverrunVerdict = "hand-back"
)

// watching reports whether this leaf has a threshold to cross at all, and is the
// whole of the cost paid by a task with no measurement behind it: one nil
// comparison per turn.
func (t Task) watching() bool {
	return t.Overrun != nil && t.Overrun.Threshold > 0
}

// overrun puts the question, and answers it itself when nobody is listening. The
// nil checks live here rather than at the call site for the reason Task.progress
// gives: a caller that has to remember them will forget one, in the path that
// only runs when something has already gone wrong.
func (t Task) overrun(spent, turns int, partial string) (OverrunEvidence, OverrunVerdict) {
	if !t.watching() {
		return OverrunEvidence{}, OverrunContinue
	}
	evidence := OverrunEvidence{
		Spent:     spent,
		Turns:     turns,
		Anchor:    t.Overrun.Anchor,
		Threshold: t.Overrun.Threshold,
		Multiple:  t.Overrun.Multiple,
		Samples:   t.Overrun.Samples,
		Partial:   partial,
	}
	if t.Overrun.Judge == nil {
		return evidence, OverrunContinue
	}
	return evidence, t.Overrun.Judge(evidence)
}
