package remote

// ── THE ROAD IS CLASSED, NOT FREE-FOR-ALL ───────────────────────────────────
//
// callClass is WHAT A METHOD OWES THE ONES AROUND IT, and it is THE ONE
// PREDICATE that decides where the method runs: [server.serve] asks it and
// nothing else in this package asks that question anywhere else. A list
// sprinkled through the switch in [server.invoke] would be a second answer
// waiting to disagree with this one, and one method in the wrong place there is
// a keystroke queued behind a getter — which is what this file was written for.
//
// ORDERED is anything that opens a stream or changes the conversation's shape.
// What it owes is an ORDER, twice over: a stream's first event may not overtake
// the result that names it ([server.release] is one slot for that reason), and
// a surface that sets a model and then submits means those two things in that
// order (server.go's file header).
//
// A GETTER is a read. The surface asks half of them from its update loop, so
// they may not queue behind anything (a listing that blocked a keystroke was
// the measured defect) and they may not wait longer than [callDeadline] (a
// terminal that has stopped repainting).
//
// AN ACT is a person's small write — answering a card, taking the keyboard,
// interrupting, saying they have started typing. It owes nothing to anything
// for the getter's reason said the other way round: a key is not queued behind
// a listing. It waits the same [callDeadline] a getter does, and the note below
// the classes says why the longer window it was given first had to come back
// out.
//
// AND LETTING EITHER OF THEM OVERTAKE AN ORDERED CALL COSTS NOTHING, which is
// the whole licence for this split and is worth stating plainly: EVERY CALL ON
// THIS WIRE IS A SYNCHRONOUS ROUND TRIP ([Client.callAnswered] waits for the
// result frame). A caller that makes two calls therefore has the first one's
// answer in hand before the second frame is written, so two calls whose order
// matters were already ordered by the caller and cannot be reordered here; two
// made from different goroutines were never ordered by anything, before this
// change or after it.
//
// A METHOD THIS FUNCTION HAS NOT HEARD OF IS ORDERED. A door that lands next
// year keeps its place in the queue until someone decides it is a getter or a
// small act, which is the same allow-list shape [watcherMay] uses.
//
// ── AND ORDERED NEVER MEANT "ON THE READER" ─────────────────────────────────
//
// That was the confusion this file carried until 2026-09-11, and it is why a
// keystroke could sit behind a send. Running the ordered bodies on the
// goroutine that reads the socket gave the order for free and took the READING
// away with it: [MethodSubmit] alone rebinds the client, re-reads the person's
// standing orders, reassembles the system prompt, journals the message and
// starts the naming errand before it returns, and for all of that the engine
// was not reading its own pipe. A person who pressed Enter and then Esc was
// queued behind their own send. [MethodCompact] was the extreme case — it does
// the summarizer's work itself — and it was described here as the price of
// being ordered, which it never was.
//
// SO THE READER READS, AND NOTHING ELSE. Every class runs its body off it; what
// a class chooses is WHICH goroutine ([callClass.road]). Ordered calls go to
// the connection's one [orderedLane] — one goroutine, arrival order, which is
// exactly the guarantee the reader was giving and is now given by the thing
// that actually needs it. Getters and small acts owe each other nothing, so
// each takes a goroutine of its own.
//
// AND THE ORDERED LANE IS THE ONLY OWNER OF [server.pending]. Every method that
// names a stream is ordered, so the one slot is written and read by one
// goroutine and needs no lock. A method that opened a stream from any other
// class would be racing it, which is what [TestEveryStreamOpenerIsOrdered]
// refuses.

type callClass int

const (
	classOrdered callClass = iota
	classGetter
	classAct
)

// road is WHERE a class of call runs, and this is the one place a class is
// turned into a goroutine. [server.serve] switches on it and nothing else
// decides it, so a method's road is a property of its class rather than a
// branch at the site that happens to read the socket.
//
// NEITHER ROAD IS THE READER. That is the law of this file said as a type: the
// goroutine that reads the socket has no third option to be given.
type road int

const (
	// inOrder is the connection's one ordered lane: off the reader, and in the
	// order the frames arrived (orderedlane.go).
	inOrder road = iota
	// onItsOwn is a goroutine per call. A getter and a small act owe nothing to
	// each other, so neither owes a queue.
	onItsOwn
)

func (c callClass) road() road {
	if c == classOrdered {
		return inOrder
	}
	return onItsOwn
}

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
		MethodTranscript, MethodEarlier, MethodRewindPoints, MethodPlanSpend,
		MethodPlanTasks, MethodPlanTaskPage, MethodPlanTaskWork, MethodPlanRunSummary, MethodRefreshRunSummary,
		MethodReasoningFor, MethodEffort, MethodResolvedEffort, MethodResolvedApproval,
		MethodAttachedSkills, MethodSkillShelf,
		MethodSessionsRecent, MethodHeldQuestions,
		MethodStandingItems, MethodStandingWatch,
		MethodPlacesWorld, MethodPlacesTask, MethodPlacesLedger, MethodPlacesSearch,
		MethodMemorySnapshot, MethodMemoryChanged, MethodMemoryList, MethodMemoryProvenance,
		MethodTaskRoom, MethodTaskPending, MethodTaskEffort,
		MethodListDir, MethodStatPaths, MethodFetchFile,
		// The wall's two model asks are reads of the naming role, asked off the
		// update loop and bounded by the wall; queued behind a turn they would
		// wait out the wall's patience and answer nobody.
		MethodTeamsName, MethodTeamsPropose:
		return classGetter
	case MethodQuestionResolve, MethodQuestionHold,
		MethodConsent, MethodConsentRemember,
		MethodStandingResolve, MethodHarness, MethodConnect, MethodConnectKey,
		MethodNoteConnected,
		MethodTake, MethodAnswerLaneOffer, MethodInterrupt,
		MethodPlanNote, MethodPlanPause, MethodPlanResume, MethodPlanCancel, MethodPlanAmend, MethodPlanPriority,
		MethodTyping:
		return classAct
	default:
		return classOrdered
	}
}

// opensAStream reports whether calls of this class may name a stream, which is
// the same question as "may they write [server.pending]". Exactly one class
// may, one goroutine runs that class, and [server.dispatch] starts the pump for
// exactly that one — which is the whole of why the slot needs no lock.
func (c callClass) opensAStream() bool { return c == classOrdered }
