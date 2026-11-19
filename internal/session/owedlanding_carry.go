package session

import "strings"

// ── an owed landing's bound carries the work on ─────────────────────────────
//
// THE MEASURED FAILURE (connected-canvas qloop run23, vector-db t1, scored 3.8).
// The manager's turn handed the charts and the research to tasks and ended:
// "When both land I'll write the brief itself into the Vector DB brief Doc."
// That was true. Both tasks were running, so #2838's promise check rightly let
// it through. Both landed together seven minutes later, and their report woke
// the conversation as an OWED landing ([landingOwesAnswer]). That turn is a
// settle wake on the cheap tier with a six-call ceiling ([settleCallCeiling]),
// and it is built to answer from the report and stop. This turn instead had to
// write the brief it had promised. It read the two sheets in six calls and hit
// the ceiling. [Agent.markSettleBound] found no undecided task to hand back,
// because both were done, so the turn sealed with no reply, no notice and an
// empty brief. The chat stayed on "Working · 4 actions · 3 failed" until the
// person's next message, and the brief was written only in that next turn.
//
// A BOUND THAT ENDS A TURN MUST NEVER END IT SILENTLY. That is the settle
// bound's own rule, and for a decision it is kept by handing the decision back
// with the reason on the card. An owed landing holds no decision. What it holds
// is the person's unanswered request, so it is kept by carrying the work on.
// The cheap reply turn ends, and one ordinary turn starts in its place. That
// turn has the conversation's own model and prompt, no settle ceiling and no
// window, and it reads every step the cheap turn already took. A reply that
// answers within the bound costs exactly what it did before, and a turn the
// person stopped is never carried on (the bound is read after the stop check).

// owedLandingCarryOnNote is the dim line said while the work carries on. It is
// a carry-on line ([IsCarryOnNotice]), so a surface draws it only while the
// turn is working and folds it away afterwards.
const owedLandingCarryOnNote = "the report needs more than a short reply · carrying on"

// owedLandingCarryOnOpening and [owedLandingCarryOnClosing] frame the note the
// carried-on turn reads. The bound's count sits between them.
const owedLandingCarryOnOpening = "[carry on] The short reply to the report above stopped at its limit of "

const owedLandingCarryOnClosing = " before it answered, and nothing it did has reached the person yet. " +
	"Finish what the person asked now, in this turn, including any work your earlier reply said you would do once this work landed. " +
	"Then reply to the person with what is done and what is not."

// owedLandingCarryOnLead is the whole continuation for one tripped bound.
func owedLandingCarryOnLead(count string) string {
	return owedLandingCarryOnOpening + count + owedLandingCarryOnClosing
}

// isOwedLandingCarryOnLead recognises the continuation, so every reader that
// hides the harness's own notes hides this one too ([isContinuationContext]).
func isOwedLandingCarryOnLead(text string) bool {
	return strings.HasPrefix(text, owedLandingCarryOnOpening) && strings.HasSuffix(text, owedLandingCarryOnClosing)
}

// carryOwedLandingOn queues the continuation of an owed landing whose reply
// turn reached its bound. The caller then ends that turn as COMPLETED. The
// turn's end drains this note into the transcript, and because the note is an
// owed wake, that drain starts one ordinary turn ([Agent.wakeAnsweringLocked]).
// The note carries no settle mark, so the new turn has no bound of its own.
//
// It is queued rather than recorded for the reason the end-of-turn drain
// exists: a wake reads the last thing recorded. Recording it here and then
// returning would leave a line in the transcript and no turn to answer it.
func (a *Agent) carryOwedLandingOn(hub *eventHub, count string) {
	hub.send(Event{Kind: EventNotice, Text: owedLandingCarryOnNote})
	a.journalSentBack("owed-landing-carried-on")
	note := wakeNote(owedLandingCarryOnLead(count))
	// IT IS STILL THE PERSON'S ANSWER, so the spend gate that holds an unasked
	// turn does not hold this one ([Agent.enqueueNote]), and it is drained as
	// its own message rather than batched with ambient news.
	note.answer = true
	note.batch = false
	a.enqueueNote(note)
}
