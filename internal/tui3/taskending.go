package tui3

import "github.com/Agent-Field/aforge-v2/internal/session"

// ── why a task stopped where it did ─────────────────────────────────────────
//
// Every node that did not finish used to wear ONE sentence on the rail —
// "stopped — branch kept" — and its comment said that the commonest way to earn
// it was a person pressing stop. Then six such rows were read against their
// records: three were the connection to the model dropping, two were a worker
// giving up going in circles (one of them because another task held the files
// it kept trying to write), one was a check refusing the work, and not one was a
// person. The sentence was true of none of them, and "nothing went wrong" was
// the wrong thing to tell somebody about five.
//
// The engine now says why (session's TaskNotice.Ending), and this file is the
// three kinds of news it draws them as. A PERSON STOPPED IT: the ⊘ and the old
// sentence, because nothing is wrong. IT WAS HALTED — the wire, a threshold, a
// loop, another task's copy, a worker that would not write down what it was
// doing: nothing is known to be wrong and the work can go on from its branch, so
// it wears the ! that asks for a steer rather than the cross that reports a
// fault. IT WAS REFUSED: the run is incomplete, with the report saying what the
// check still needs. IT BROKE: the cross, and the report says what went wrong.

// The words under a row for each ending, in a person's vocabulary and never
// the engine's ("wire", "circling", "refused" are field values, not sentences).
const (
	endingWordWire     = "lost the connection"
	endingWordUpstream = "the model provider refused it"
	endingWordCircling = "went in circles"
	endingWordBlocked  = "blocked by another task"
	endingWordSteps    = "out of steps"
	endingWordNotes    = "would not write its notes down"
	endingWordRefused  = "not accepted"
	endingWordStale    = "its world did not match"
	endingWordError    = "ended with an error"
)

// endingWord is the two-or-three-word reason a failed node's row leads with,
// and "" for a node the engine gave no reason for — every failed row an older
// engine or checkpoint sends, which keeps its old sentence ([taskStoppedWord]).
func endingWord(ending session.TaskEnding) string {
	switch ending {
	case session.TaskEndingStopped:
		return taskStoppedWord
	case session.TaskEndingWire:
		return endingWordWire
	case session.TaskEndingUpstream:
		return endingWordUpstream
	case session.TaskEndingCircling:
		return endingWordCircling
	case session.TaskEndingBlocked:
		return endingWordBlocked
	case session.TaskEndingSteps:
		return endingWordSteps
	case session.TaskEndingNotes:
		return endingWordNotes
	case session.TaskEndingRefused:
		return endingWordRefused
	case session.TaskEndingStale:
		return endingWordStale
	case session.TaskEndingError:
		return endingWordError
	}
	return ""
}

// endingKept is the sentence a failed node whose branch was kept wears — the
// reason, then the half that says what to do about it. It is [taskStoppedKept]
// for a stop and for every node that gave no reason, so a rail talking to an
// older engine reads exactly as it did.
func endingKept(ending session.TaskEnding) string {
	word := endingWord(ending)
	if word == "" || word == taskStoppedWord {
		return taskStoppedKept
	}
	return word + " — " + taskBranchKept
}

// halted says this ending is the middle kind of news: the run did not finish,
// nothing was found wrong with the work, and a person can pick it up from its
// branch — the wire, a threshold, a loop, another task's copy, a rule the worker
// would not follow.
func halted(ending session.TaskEnding) bool {
	switch ending {
	case session.TaskEndingWire, session.TaskEndingUpstream, session.TaskEndingCircling,
		session.TaskEndingBlocked, session.TaskEndingSteps, session.TaskEndingNotes:
		return true
	}
	return false
}

// glyphHalted is the cell for a halted node. It is the ? of "needs your look"
// with the question taken out: the machine has stopped and the next move is a
// person's, but nothing is being asked — it is being pointed at. It is not
// [glyphBad], for the reason [glyphStopped] is not: a cross is a finding, and
// nobody found anything wrong with work the connection dropped out from under.
// The same cell in both glyph tiers, because ! is already a character a screen
// with no Unicode has.
const glyphHalted = "!"

// refused says the task reached a check and the check did not accept its claim.
// The engine still settles that node as failed so dependencies and delivery do
// not advance; the surface calls the person's state incomplete because the
// report names work still to do rather than a runtime fault.
func refused(ending session.TaskEnding) bool {
	return ending == session.TaskEndingRefused
}
