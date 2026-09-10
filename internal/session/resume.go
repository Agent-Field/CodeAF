package session

// resume.go is A QUESTION THAT WAS NEVER ANSWERED BEING ASKED AGAIN BY THE
// WINDOW THAT NOW HAS IT.
//
// ── WHAT WAS BELIEVED ───────────────────────────────────────────────────────
//
// takeover.go's second law says a request to move a conversation is answered at
// once, mid-reply or not, and gives the reason: the holder interrupts and closes
// the way /new does, "and the window that asked resumes it from the checkpoint
// with the partial reply already in the journal". That is true of a turn that
// had SAID something. It is false of a turn that had only been THINKING —
// reasoning never enters the partial, so there is nothing to keep — and the
// measured case (#760) was exactly that one: 108 seconds of streamed reasoning,
// a takeover, and a conversation arriving in the new window as a question with
// nothing under it.
//
// #760 made that ending visible: the journal gets a failed call naming the door,
// and the person gets one sentence. But the sentence ended `— ask again to pick
// it up`, which leaves the repair to the person. They typed the question once;
// the machine took the answer away; asking them to type it a second time is the
// machine's work handed back.
//
// ── WHAT IS TRUE NOW ────────────────────────────────────────────────────────
//
// The window that takes the conversation over asks the question again itself,
// through the ordinary turn door, and the reply streams where the person is
// sitting.
//
// ── THE SHAPE IS THE WHOLE OF THE DECISION ──────────────────────────────────
//
// Nothing is passed between the two windows to say a resume is owed — they are
// two processes and the only thing they share is the file. THE JOURNAL SAYS IT,
// and it says it as a shape rather than as a flag: the last message is the
// person's own words, and the only thing after it is this machine stopping the
// turn that was answering them (sessionfile.go's [readJournal] reads it,
// [replayedSession.stopped] carries it). That shape can be produced by nothing
// except a turn that ended with NOTHING SAID:
//
//   - an assistant message, a tool call or a result after the question means an
//     answer of some size arrived, and the tail is not the question any more;
//   - a provider's refusal is a thing that happened on the way to an answer and
//     leaves the shape alone, because the turn was still asking;
//   - a person's own stop writes no row at all ([Agent.endStoppedTurn]), so a
//     turn somebody stopped themselves can never look like this.
//
// ── AND ONLY ONE DOOR RESUMES ───────────────────────────────────────────────
//
// [resumesUnanswered] is that list and it has one name on it. A takeover is the
// one door where somebody is DEMONSTRABLY SITTING IN FRONT OF THE CONVERSATION
// a moment later — they asked for it, they waited for it, and the window they
// asked from is the window this runs in. Every other door leads somewhere else:
// a conversation left is a person who went to another one, a session closed is a
// terminal that is gone, a retired host is nobody watching at all. Re-asking a
// question in a window nobody is looking at would spend the person's money on an
// answer nobody would read, so those keep the sentence #760 gave them and wait
// to be asked again.

import (
	"context"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ResumedWord is what a person reads when the reply they were waiting for
// starts again in the window they moved the conversation to.
//
// IT IS [cutShortNotice]'s REGISTER AND FOR ITS REASON. Something took the reply
// away, something is already being done about it, and there is no decision to
// make — so the line says what happened and what is happening, in that order,
// and stops. It is one sentence and not two: the window they came from already
// said [TakeoverWord] on its way out.
const ResumedWord = "the reply stopped when this conversation moved — asking again"

// resumesUnanswered reports whether a turn that ended at this door with nothing
// said is one this build asks again by itself.
//
// It is a function with a switch rather than a set so that a door added to
// stopcause.go tomorrow has to be thought about here rather than defaulting into
// spending somebody's money.
func resumesUnanswered(door StopDoor) bool {
	switch door {
	case StopByTakeover:
		return true
	default:
		return false
	}
}

// ResumeStoppedTurn asks the person's last question again, once, when this
// conversation was opened onto the exact shape resume.go describes, and reports
// whether it did. It answers nil and false in every other case, which is nearly
// every conversation ever opened.
//
// THE FACT IS SPENT WHEN IT IS READ, whether or not a turn starts. A window that
// takes a conversation may attach to it more than once — the switcher brings it
// forward, the keeper hands it back — and a question asked twice is two answers
// and two bills for one thing the person typed. It is cleared here, under the
// lock, before anything else can decide anything.
//
// THE MESSAGE IS THE ONE ALREADY IN THE TRANSCRIPT. Nothing is appended: the
// person's words are in a.messages because the journal put them there, and in
// the file because the stopped turn wrote them. A second copy would be a
// conversation in which somebody said the same thing twice
// ([userMessage.resumed] is that mark).
//
// AND IT IS THE ORDINARY TURN DOOR. [Agent.startTurnLocked] is what a typed
// message reaches, so steering, presence, the title, the checkpoint and the
// retry ladder all behave here exactly as they behave everywhere else — and the
// stopped attempt's reasoning and half-written reply are simply gone, because
// nothing kept them.
func (a *Agent) ResumeStoppedTurn(ctx context.Context) (<-chan Event, bool) {
	a.mu.Lock()
	door := a.stoppedTurn
	a.stoppedTurn = ""
	if !resumesUnanswered(door) || a.closed || a.running || a.workStopped {
		a.mu.Unlock()
		return nil, false
	}
	// The transcript has to still end where the journal said it did. Everything
	// between the reading and this call — a scrub, a repair, a rewind — is
	// allowed to have moved the tail, and a resume that asked about the wrong
	// message would be worse than no resume at all.
	last, ok := a.lastPersonMessageLocked()
	if !ok {
		a.mu.Unlock()
		return nil, false
	}
	if err := a.resumeWorkLocked(); err != nil {
		a.mu.Unlock()
		return nil, false
	}
	// THE SPEND RAIL IS ASKED THE SAME QUESTION A TYPED MESSAGE ASKS IT
	// (rail.go). A turn nobody may pay for is not started here either, and the
	// person keeps the sentence #760 gave them — their question is in the file
	// and asking again is theirs to do once the rail moves.
	if err := a.railBlockLocked(); err != nil {
		a.mu.Unlock()
		return nil, false
	}
	events := a.startTurnLocked(ctx, userMessage{message: last, resumed: true}, nil)
	a.mu.Unlock()
	return events, true
}

// lastPersonMessageLocked is the transcript's final message when it is one the
// person typed, and false otherwise.
func (a *Agent) lastPersonMessageLocked() (ai.Message, bool) {
	if len(a.messages) == 0 {
		return ai.Message{}, false
	}
	last := a.messages[len(a.messages)-1]
	if last.Role != "user" || strings.TrimSpace(messageContentText(last)) == "" {
		return ai.Message{}, false
	}
	return last, true
}
