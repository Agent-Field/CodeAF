package session

import (
	"errors"
	"fmt"
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
//
// It is now the special case of [Agent.RewindAt]: the last Turn point in
// [Agent.RewindPoints], which is the newest place in the conversation the person
// said something. It keeps its own door because it is the rewind that needs no
// picker — one keystroke for "not that, let me say it again" — but it no longer
// keeps its own cutting path. THERE IS ONE CUT IN THIS PACKAGE, and every door
// onto it only chooses where to put it.
func (a *Agent) Rewind() ([]DisplayEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.rewindReadyLocked(); err != nil {
		return nil, err
	}
	points := a.rewindPointsLocked()
	for index := len(points) - 1; index >= 0; index-- {
		if points[index].Turn {
			return a.cutLocked(points[index].Index), nil
		}
	}
	return nil, ErrNothingToRewind
}

// RewindPoint is one place the conversation can be cut. Index is the message
// index the cut drops from — messages[Index:] go. Turn marks a cut landing on
// something the person said, the anchors a rewind usually returns to; a false
// Turn is a step boundary inside a turn (after a tool result or an assistant
// reply). Said carries the person's words at a Turn cut, for the surface to
// hand back to the draft. Entry is the index into [Agent.Transcript]'s display
// entries at which the drop would begin, so a surface can place the cut line
// on the exact row it drew.
type RewindPoint struct {
	Index int
	Turn  bool
	Said  string
	Entry int
}

// RewindPoints is every place this conversation can be cut, oldest first — the
// list a picker draws and the only indices [Agent.RewindAt] will accept.
//
// A TURN POINT IS A USER-ROLE MESSAGE, which is where a rewind usually wants to
// land: it takes back what was said and everything the saying caused. Index 0 is
// the system message and is never a cut, and a compaction note never appears at
// all — it is context this package handed to the model rather than something
// anybody said, and cutting to it would drop a whole resumed conversation while
// leaving the summary that replaced its beginning. Both exclusions are the ones
// [Agent.lastTurnStartLocked] already makes; the note is recognized by the
// marker [foldMarker] writes, not by guessing at its wording.
//
// A STEP POINT IS ANY OTHER MESSAGE BOUNDARY: after an assistant reply, after a
// tool result. These are the cuts inside a turn, for a person who wants to keep
// the instruction and drop only the last thing the model did with it.
//
// THE LAW A BOUNDARY MUST OBEY: A CUT MAY NOT SEPARATE A TOOL CALL FROM ITS
// ANSWER. A retained assistant message whose calls were answered in the dropped
// tail is a transcript no provider will take — the call is still there, asking
// for a result that no longer exists — so those boundaries are left OUT of the
// list rather than repaired at cut time. The alternative, stripping the dangling
// calls from the retained message, was rejected on two counts. It would make the
// live transcript say the model never made a call that it really did make, which
// is a lie about the past that this package refuses everywhere else — a rewind
// is an edit of the CONVERSATION and never a claim that the work did not happen.
// And it could not survive a resume: the journal is append-only and its rewind
// marker is a COUNT of messages dropped from the tail (sessionfile.go), so there
// is no way to journal a mutation of a message that is being KEPT. A cut only
// ever drops a tail, in memory and in the file alike, and the points list is
// exactly the set of tails that can be dropped.
//
// Note that the law is about what the cut CREATES, not about what it inherits. A
// call that was never answered anywhere — an interrupt that landed between a
// batch and its results — is dangling before the cut and after it, and no choice
// of cut point mends or worsens it. Only a call whose answer sits on the far
// side of the cut disqualifies a boundary.
func (a *Agent) RewindPoints() []RewindPoint {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rewindPointsLocked()
}

// RewindAt is the generalized cut: it drops messages[index:], journals the drop,
// and reports what it removed in the same display shape [Agent.Transcript] uses.
// Everything Rewind says about a rewind is true of it — the workspace is not
// touched, the file is not rewritten, a turn in flight is refused rather than
// waited for.
//
// The index must be one [Agent.RewindPoints] offered. It is not rounded to the
// nearest one: a cut is destructive, the caller is a surface that just drew the
// list, and a picker that is one row off should hear about it rather than have
// this package quietly remove a different turn. The error names the nearest
// valid point so the caller can say what it should have asked for.
func (a *Agent) RewindAt(index int) ([]DisplayEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.rewindReadyLocked(); err != nil {
		return nil, err
	}
	points := a.rewindPointsLocked()
	if len(points) == 0 {
		return nil, ErrNothingToRewind
	}
	for _, point := range points {
		if point.Index == index {
			return a.cutLocked(index), nil
		}
	}
	return nil, fmt.Errorf("session: %d is not a rewind point; the nearest is %d", index, nearestRewindPoint(points, index))
}

// rewindReadyLocked is the one set of refusals every cut answers to, held in one
// place so a new door cannot be opened that forgets one of them.
func (a *Agent) rewindReadyLocked() error {
	if a.closed {
		return errors.New("session: agent is closed")
	}
	// compacting is checked beside running for the same reason it is refused
	// at all: the surface's /compact runs a pass with no turn around it, and
	// that pass is holding an index into a.messages across its summary call.
	if a.running || a.compacting {
		return ErrTurnInFlight
	}
	return nil
}

// cutLocked is the cut itself, and the only place in this package that shortens
// a.messages on a person's say-so. The caller has already checked the refusals
// and established that index is a point; this does the three things a cut is:
// journal it, shape what is leaving, and let go of it.
func (a *Agent) cutLocked(index int) []DisplayEntry {
	dropped := a.messages[index:]
	removed := displayEntries(dropped)
	if a.file != nil {
		a.file.appendRewind(len(dropped))
	}
	// The tail is cleared before the slice is shortened so the messages the
	// session no longer holds are not kept alive by its backing array — a
	// dropped turn can be several hundred KB of tool results.
	for at := index; at < len(a.messages); at++ {
		a.messages[at] = ai.Message{}
	}
	a.messages = a.messages[:index]
	return removed
}

// rewindPointsLocked walks the transcript once and answers three questions per
// message at the same time: whether the boundary in front of it is legal, what
// kind of point it is, and how many display rows sit behind it.
//
// Entry is counted by SHAPING each message and asking how many rows it made,
// rather than by a second rule about which messages draw and how many rows a
// batch of calls costs. A second rule is a rule that can drift from the first,
// and the whole worth of Entry is that it lands on the row the surface really
// drew: it is [Agent.Transcript]'s own shaping or it is a guess.
func (a *Agent) rewindPointsLocked() []RewindPoint {
	answers := toolAnswerPositions(a.messages)
	points := make([]RewindPoint, 0, len(a.messages))
	// answered is the furthest position holding an answer to a call made in the
	// messages already walked past. A boundary is legal exactly when that
	// position is behind it — every call already made is already answered on the
	// retained side. Because it only ever moves forward, one walk decides every
	// boundary.
	answered := -1
	entry := 0
	for index, message := range a.messages {
		if index > 0 && answered < index {
			said := ""
			skipped := false
			turn := false
			if message.Role == "user" {
				said = messageContentText(message)
				if isCompactionNote(said) {
					skipped = true
				} else {
					turn = true
				}
			}
			if !skipped {
				point := RewindPoint{Index: index, Turn: turn, Entry: entry}
				if turn {
					point.Said = said
				}
				points = append(points, point)
			}
		}
		for _, call := range message.ToolCalls {
			if at, ok := answers[call.ID]; ok && at > answered {
				answered = at
			}
		}
		entry += len(shapeEntries(a.messages[index:index+1], nil))
	}
	return points
}

// toolAnswerPositions indexes where each call's result landed. It is the
// positional twin of toolResults (agent.go), which indexes the same messages by
// their TEXT: a cut cares where the answer sits, not what it said. A result with
// no id is skipped for the same reason there — it is a message no call can
// claim.
func toolAnswerPositions(messages []ai.Message) map[string]int {
	var answers map[string]int
	for index, message := range messages {
		if message.Role != "tool" || message.ToolCallID == "" {
			continue
		}
		if answers == nil {
			answers = make(map[string]int, 8)
		}
		answers[message.ToolCallID] = index
	}
	return answers
}

// nearestRewindPoint is for the error message alone — it names what the caller
// probably meant, and nothing acts on it. A tie goes to the LATER point, which
// is the one that drops less: if this package is going to put a number in front
// of a person, the smaller cut is the kinder guess.
func nearestRewindPoint(points []RewindPoint, index int) int {
	nearest := points[0].Index
	for _, point := range points {
		if abs(point.Index-index) <= abs(nearest-index) {
			nearest = point.Index
		}
	}
	return nearest
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// lastTurnStartLocked is the index of the message that started the last turn:
// the final user-role message, which is the last thing said to the model.
//
// The cut no longer reads it — [Agent.Rewind] takes the last Turn point out of
// [Agent.RewindPoints], which applies the same two exclusions and the boundary
// law besides. It stays for [Agent.Why], which wants the same anchor for a
// different purpose: to quote the instruction the current turn is working on.
//
// One user-role message is skipped, and it is this package's own: a compaction
// note is context handed TO the model rather than something anybody said, and
// cutting there would drop a whole resumed conversation while leaving the
// summary that replaced its beginning. It is recognized by the marker
// [foldMarker] writes, not by guessing at its wording.
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

// isCompactionNote reports whether a user-role message is one THIS PACKAGE
// injected rather than something that was said.
//
// Two markers answer yes, and both are strings this package controls rather than
// wordings anybody guessed at: the fold marker the current pass writes
// ([foldMarker]), and the "[context compacted]" note an older aforge's summary
// arrived under, which a resumed session can still be holding (sessionfile.go's
// [legacyCompactionNote]).
func isCompactionNote(text string) bool {
	return strings.HasPrefix(text, foldMarkerPrefix) ||
		strings.HasPrefix(text, "[context compacted]")
}
