package remote

// ── THE ROAD IS CLASSED, NOT FREE-FOR-ALL ───────────────────────────────────
//
// callClass is which side of the road a method travels on, and it is THE ONE
// PREDICATE that decides it: the engine's reader asks it whether [server.invoke]
// may leave the reader goroutine, and nothing else in this package asks that
// question anywhere else. A list sprinkled through the switch in
// [server.invoke] would be a second answer waiting to disagree with this one,
// and one method in the wrong place there is a keystroke queued behind a getter
// — which is what this file was written for.
//
// ORDERED stays on the reader, in the order the surface sent it: anything that
// opens a stream or changes the conversation's shape. A stream's first event
// cannot overtake the result that names it ([server.release] is one slot for
// that reason), and a surface that sets a model and then submits means those
// two things in that order (server.go's file header). Compact is the one that
// pays for this — it does the work itself and holds the reader for as long as
// the summarizer takes.
//
// A GETTER is a read. The surface asks half of them from its update loop, so
// they cannot hold the reader (a listing that blocked a keystroke was the
// measured defect) and they cannot wait longer than [callDeadline] (a terminal
// that has stopped repainting).
//
// AN ACT is a person's small write — answering a card, taking the keyboard,
// interrupting. It runs off the reader for the getter's reason, said the other
// way round: a key is not queued behind a listing. It waits the same
// [callDeadline] a getter does, and the note below the classes says why the
// longer window it was given first had to come back out.
//
// AND LETTING EITHER OF THEM OVERTAKE AN ORDERED CALL COSTS NOTHING, which is
// the whole licence for this split and is worth stating plainly: EVERY CALL ON
// THIS WIRE IS A SYNCHRONOUS ROUND TRIP ([Client.callAnswered] waits for the
// result frame). A caller that makes two calls therefore has the first one's
// answer in hand before the second frame is written, so two calls whose order
// matters were already ordered by the caller and cannot be reordered here; two
// made from different goroutines were never ordered by anything, before this
// change or after it. What the reader still owes is the order WITHIN one call —
// the result frame before the first event of the stream it names — and that is
// exactly what stays on it.
//
// A METHOD THIS FUNCTION HAS NOT HEARD OF IS ORDERED. A door that lands next
// year stays on the reader until someone decides it is a getter or a small
// act, which is the same allow-list shape [watcherMay] uses.

type callClass int

const (
	classOrdered callClass = iota
	classGetter
	classAct
)

// ── AND AN ACT DOES NOT GET A LONGER WINDOW THAN A GETTER ───────────────────
//
// It is the obvious second half of this fix and it was written, measured and
// TAKEN BACK OUT, so here is the measurement rather than the temptation.
//
// The argument for it is real: a getter that gives up costs one stale number
// and is asked again on the next frame, while an act that gives up costs a
// DECISION — the engine takes the answer and this window is told it did not.
// So an act was given three times [callDeadline], thirty seconds.
//
// WHAT THAT BUYS IS A FROZEN TERMINAL, because an act is asked from the update
// loop exactly as a getter is: internal/tui3's [app.answerQuestion] calls its
// door straight from Update rather than from a command, so the window is also
// the longest that terminal can sit without repainting after a key is pressed.
// Measured on the Spark, eighteen copies of the ordinary road's own e2e run six
// at a time: with thirty seconds, two copies took 53 seconds where every other
// copy took 22, and both lost the receipt — the surface was not drawing. Twelve
// copies of the same commit's parent produced no copy over 33 seconds.
//
// So the window is [callDeadline] for everything, and what stops an act needing
// more is the road above rather than a bigger number: it is no longer queued
// behind anything. A deadline it does reach is now said honestly instead of as a
// dead connection ([Client.late]), and the receipt stamped before the door is
// what makes the engine's news close as yours (internal/tui3's
// [app.markQuestionSent]) — which is the pair that makes a short window safe.

// lateCallTail is what a deadline says on a connection that is still here,
// after [Client.where]. THE CONNECTION IS NOT GONE. The engine may be working
// on this call right now; what ran out is this end's patience. Naming the
// machine the same way [Client.gone] does keeps the two sentences one voice;
// an empty where is "the engine", which is the sentence the surface already
// quotes when a door never answered.
const lateCallTail = " did not answer in time"

func classify(method string) callClass {
	switch method {
	case MethodPing,
		MethodModel, MethodTitle, MethodUsage, MethodContextTokens,
		MethodTranscript, MethodEarlier, MethodRewindPoints,
		MethodReasoningFor, MethodEffort, MethodResolvedEffort,
		MethodSessionsRecent, MethodHeldQuestions,
		MethodStandingItems, MethodStandingWatch,
		MethodPlacesWorld, MethodPlacesTask, MethodPlacesLedger, MethodPlacesSearch,
		MethodMemorySnapshot, MethodMemoryChanged, MethodMemoryList, MethodMemoryProvenance,
		MethodTaskRoom, MethodTaskPending, MethodTaskJudge, MethodTaskEffort,
		MethodListDir, MethodStatPaths, MethodFetchFile:
		return classGetter
	case MethodQuestionResolve,
		MethodConsent, MethodConsentRemember,
		MethodStandingResolve, MethodHarness, MethodConnect, MethodConnectKey,
		MethodNoteConnected,
		MethodTake, MethodAnswerLaneOffer, MethodInterrupt:
		return classAct
	default:
		return classOrdered
	}
}

// staysOnReader is [classify] as the engine's reader asks it: stream-opening
// and shape-changing calls stay here; getters and small acts run off it.
func staysOnReader(method string) bool { return classify(method) == classOrdered }
