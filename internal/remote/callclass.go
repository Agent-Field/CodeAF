package remote

import "time"

// ── THE ROAD IS CLASSED, NOT FREE-FOR-ALL ───────────────────────────────────
//
// callClass is which side of the road a method travels on, and it is THE ONE
// PREDICATE both halves of the connection read. The engine's reader uses it to
// decide whether invoke may leave this goroutine; the surface uses it to decide
// how long to wait and what a timeout is allowed to claim. A list sprinkled
// through the switch would drift the two halves apart, which is how a
// keystroke ended up queued behind a getter.
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
// interrupting. It runs off the reader for the getter's reason (a key is not
// queued behind a listing) and it waits [actDeadline], which is longer for the
// reason stated where that constant is.
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

// actDeadline is how long a person's small act waits for its result. It is
// derived from [callDeadline] so the two windows cannot drift.
//
// IT IS LONGER THAN A GETTER'S BECAUSE GIVING UP EARLY COSTS DIFFERENT THINGS.
// A getter that gives up costs one stale number and is asked again on the very
// next frame the surface draws. An act that gives up costs a DECISION: the
// engine takes the answer, this window is told it did not, and the two accounts
// of one keystroke then have to be reconciled after the fact — which is the
// defect this file was written for. Compact is still an ordered call and still
// holds the engine's reader for as long as the summarizer takes (server.go's
// file header), so an act genuinely can be slow with nothing wrong.
//
// AND IT IS ALSO THE PRICE OF A STILL TERMINAL, WHICH IS WHY IT IS NOT LONGER.
// The question block asks its door straight from the surface's update loop
// (internal/tui3's [app.answerQuestion] is not a command), so this window is
// the longest that terminal can sit without repainting after a key is pressed.
// Thirty seconds is the most that trade is worth, and the road above is what
// makes reaching it rare rather than one run in six.
const actDeadline = 3 * callDeadline

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

// deadlineFor is how long this call class waits. Shaping already has its own
// bound ([taskCallDeadline]) and those doors pass it explicitly.
func deadlineFor(method string) time.Duration {
	if classify(method) == classAct {
		return actDeadline
	}
	return callDeadline
}
