// STUB — delete this whole file at merge; the rewind/engine branch owns these.
//
// WHAT THIS IS. The surface's rewind mode (internal/tui3/rewind.go) is a picker
// over a list of places the conversation can be cut, and the engine is the thing
// that knows where those places are and how to cut. That engine is being built on
// a branch of its own. This file is the CONTRACT it will land with, implemented
// naively but honestly, so the surface can be written, driven and tested against
// something real rather than against a mock of a shape nobody has agreed on.
//
// NAIVE MEANS: a turn point is a user message, a step point is an assistant
// message, and the cut is the truncation [Agent.Rewind] already performs at a
// chosen index rather than at the last one. Everything the real engine will have
// to decide — whether a cut inside a tool batch is legal, what a compaction
// boundary does to the walk, how a step point is named — is not decided here.
//
// HONEST MEANS: it takes the same lock, refuses on the same two conditions with
// the same sentinel, and journals through the same [sessionFile.appendRewind] the
// existing Rewind does (rewind.go). A test that passes against this file is
// testing a real truncation of a real transcript, not a stub that returned nil.
package session

import (
	"errors"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// RewindPoint is one place the conversation can be cut, as the surface needs to
// see it.
//
//	Index  names the point to [Agent.RewindAt]. It is this package's own
//	       coordinate and the surface never does arithmetic on it.
//	Turn   says the point is a USER MESSAGE — the start of a turn, which is what
//	       ↑/↓ walk. Everything else is a step inside one.
//	Said   is what the person said at a turn point, so the surface can put the
//	       message back in the box a cut takes it out of. Empty at a step point.
//	Entry  is where the drop would begin in [Agent.Transcript], so a surface that
//	       drew that list can find the row a cut line belongs above.
type RewindPoint struct {
	Index int
	Turn  bool
	Said  string
	Entry int
}

// RewindPoints is every place this conversation can be cut, OLDEST FIRST.
//
// A session nobody has spoken to has none, and the surface says so rather than
// opening a picker over an empty list.
func (a *Agent) RewindPoints() []RewindPoint {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rewindPointsLocked()
}

// rewindPointsLocked is the walk itself. It runs the SAME shaping rule
// [shapeEntries] does — the system message contributes no display entry, and a
// message's tool calls each contribute one after it — because Entry is an index
// into that list and a second counting rule would make it a lie.
//
// A compaction note is skipped for [Agent.lastTurnStartLocked]'s reason: it is
// context this package handed the model, not something anybody said, and a cut
// there would drop a resumed conversation while keeping the summary that replaced
// its beginning.
func (a *Agent) rewindPointsLocked() []RewindPoint {
	points := make([]RewindPoint, 0, len(a.messages))
	entry := 0
	for index, message := range a.messages {
		if message.Role == "system" {
			continue
		}
		text := messageContentText(message)
		switch message.Role {
		case "user":
			if !isCompactionNote(text) {
				points = append(points, RewindPoint{
					Index: index, Turn: true, Said: text, Entry: entry,
				})
			}
		case "assistant":
			points = append(points, RewindPoint{Index: index, Entry: entry})
		}
		entry += 1 + len(message.ToolCalls)
	}
	return points
}

// RewindAt cuts the conversation at one point and reports what it removed,
// oldest first, in the display shape [Agent.Transcript] uses.
//
// It is [Agent.Rewind] with the cut chosen rather than assumed: the same
// refusal while a turn or a compaction pass is holding an index into the
// messages ([ErrTurnInFlight]), the same journal marker so the file replays to
// what the session now believes, and the same clearing of the dropped tail so a
// hundred kilobytes of tool results are not kept alive by the backing array.
//
// An index that is not one of [Agent.RewindPoints]' own is [ErrNothingToRewind]
// rather than a truncation at whatever happened to be there: the surface picks
// from the list this package handed it, and a cut at a number nobody offered is a
// bug worth reporting rather than obeying.
func (a *Agent) RewindAt(index int) ([]DisplayEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, errors.New("session: agent is closed")
	}
	if a.running || a.compacting {
		return nil, ErrTurnInFlight
	}
	known := false
	for _, point := range a.rewindPointsLocked() {
		if point.Index == index {
			known = true
			break
		}
	}
	if !known || index <= 0 || index >= len(a.messages) {
		return nil, ErrNothingToRewind
	}

	dropped := a.messages[index:]
	removed := displayEntries(dropped)
	if a.file != nil {
		a.file.appendRewind(len(dropped))
	}
	for at := index; at < len(a.messages); at++ {
		a.messages[at] = ai.Message{}
	}
	a.messages = a.messages[:index]
	return removed, nil
}
