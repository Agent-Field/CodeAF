package session

import (
	"errors"
	"strings"
)

// ErrFollowUpGone is what an edit of a queued message says when the message is
// no longer queued: its turn started (or it was taken back) between the
// surface's frame and this call. It is the race said honestly, the same false
// [Agent.UnqueueFollowUp] returns, with a sentence a surface can show.
var ErrFollowUpGone = errors.New("session: that message is no longer queued")

// followUpIndexLocked finds the queued follow-up that owns ch, or -1. The
// stream is the receipt a surface holds from the moment it queues, and matching
// on it rather than on a position is what keeps a queue the surface cannot see
// whole (a steering line that fell through, steer.go) from making an index lie.
func (a *Agent) followUpIndexLocked(ch <-chan Event) int {
	for i, item := range a.followups {
		if item.stream.out == ch {
			return i
		}
	}
	return -1
}

// EditFollowUp replaces the words of ONE queued follow-up, named by its stream.
//
// IT IS ONLY EVER TRUE WHILE THE MESSAGE IS STILL QUEUED. Both this and the
// drain ([Agent.nextFollowUpLocked]) run under a.mu, so a message is either
// edited before it is taken (and the turn it starts carries the new words) or
// taken first (and this answers [ErrFollowUpGone]); there is no window in
// which the turn starts on the old words while the edit reports success.
//
// The stream and the place in the queue are kept, so the surface's receipt
// stays good and the order the person typed does not change.
func (a *Agent) EditFollowUp(ch <-chan Event, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("session: empty message")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	i := a.followUpIndexLocked(ch)
	if a.closed || i < 0 {
		return ErrFollowUpGone
	}
	a.followups[i].message = userText(text)
	return nil
}

// MoveFollowUp puts the queued follow-up named by ch directly before the one
// named by before; a before that is nil, or is not queued, means the end. It
// reports whether ch was still queued.
//
// THE DESTINATION IS NAMED BY A NEIGHBOUR, NEVER BY AN INDEX, for the reason
// [Agent.UnqueueFollowUp] names by stream: a queue the surface cannot see whole
// would make a position lie. The drain still takes one at a time from the
// front, so the order set here is the order the turns run in.
func (a *Agent) MoveFollowUp(ch, before <-chan Event) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	from := a.followUpIndexLocked(ch)
	if a.closed || from < 0 {
		return false
	}
	item := a.followups[from]
	a.followups = append(a.followups[:from], a.followups[from+1:]...)
	to := len(a.followups)
	if before != nil {
		if at := a.followUpIndexLocked(before); at >= 0 {
			to = at
		}
	}
	a.followups = append(a.followups, followUp{})
	copy(a.followups[to+1:], a.followups[to:])
	a.followups[to] = item
	return true
}
