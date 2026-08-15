package session

import (
	"errors"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ErrTurnInFlight refuses a rewind while the session is working. Rewinding
// under a running turn would cut the transcript the turn is mid-way through
// writing: the loop holds message indices across a provider call (a compaction
// pass holds one for the whole summary), and a truncation underneath it turns
// an append-only invariant into a slice out of range.
//
// It is a refusal rather than a wait because a rewind is a person saying "not
// that" about work they can see. Making them wait for the work they are
// cancelling to finish first would be the wrong shape; interrupting first and
// rewinding after is the right one, and it is one keystroke.
var ErrTurnInFlight = errors.New("session: a turn is in flight; interrupt it first")

// ErrNothingToRewind says there is no turn to drop: a session that has not been
// spoken to yet, or one whose entire transcript is a compaction summary. It is a
// sentinel so the surface can say so in the person's words instead of reporting
// a success that removed nothing.
var ErrNothingToRewind = errors.New("session: nothing to rewind")

// Rewind drops the last thing the person said and everything that followed it —
// the whole turn it started: the assistant's replies, its tool calls, and the
// results those calls returned. It reports what it removed, oldest first, in the
// same display shape [Agent.Transcript] uses, so a surface can un-draw exactly
// the rows it drew.
//
// It is an edit of the CONVERSATION, not of the workspace. Files the dropped
// turn wrote stay written and commands it ran stay run: this makes the model
// stop having been told something, which is what a person means when they take
// back a badly-phrased instruction and type a better one. Nothing pretends the
// work did not happen.
//
// Both the live transcript and the session file are rewound. The file is
// append-only, so the cut is journaled as a marker rather than by rewriting
// history: a {"type":"rewind","dropped":N} line, which a replay applies by
// dropping the N messages it had accumulated when it reached the line. The count
// travels in the marker so that a session which rewinds and then keeps working
// resumes as itself — everything after the marker is ordinary conversation and
// is replayed as such.
func (a *Agent) Rewind() ([]DisplayEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, errors.New("session: agent is closed")
	}
	// compacting is checked beside running for the same reason it is refused
	// at all: the surface's /compact runs a pass with no turn around it, and
	// that pass is holding an index into a.messages across its summary call.
	if a.running || a.compacting {
		return nil, ErrTurnInFlight
	}

	cut, ok := a.lastTurnStartLocked()
	if !ok {
		return nil, ErrNothingToRewind
	}

	dropped := a.messages[cut:]
	removed := displayEntries(dropped)
	if a.file != nil {
		a.file.appendRewind(len(dropped))
	}
	// The tail is cleared before the slice is shortened so the messages the
	// session no longer holds are not kept alive by its backing array — a
	// dropped turn can be several hundred KB of tool results.
	for index := cut; index < len(a.messages); index++ {
		a.messages[index] = ai.Message{}
	}
	a.messages = a.messages[:cut]
	return removed, nil
}

// lastTurnStartLocked is the index of the message that started the last turn:
// the final user-role message, which is the last thing said to the model.
//
// One user-role message is skipped, and it is this package's own: a compaction
// note is context handed TO the model rather than something anybody said, and
// cutting there would drop a whole resumed conversation while leaving the
// summary that replaced its beginning. It is recognized by the marker
// compactionNote writes, not by guessing at its wording.
//
// A background job's completion line (jobs.go) rides the user role too and is
// NOT skipped: it is indistinguishable from typed text without inventing a
// second marker, so a rewind taken immediately after one drops the note and a
// second rewind drops the turn. That is the honest reading of "drop the last
// thing that was said", and it is visible — the surface shows what it removed.
//
// Index 0 is the system message and is never a cut point.
func (a *Agent) lastTurnStartLocked() (int, bool) {
	for index := len(a.messages) - 1; index > 0; index-- {
		message := a.messages[index]
		if message.Role != "user" {
			continue
		}
		if isCompactionNote(messageContentText(message)) {
			continue
		}
		return index, true
	}
	return 0, false
}

// isCompactionNote reports whether a user-role message is the summary this
// package injected rather than something that was said. It matches the marker
// compactionNote writes (loop.go), which is a string this package controls.
func isCompactionNote(text string) bool {
	return strings.HasPrefix(text, "[context compacted]")
}
