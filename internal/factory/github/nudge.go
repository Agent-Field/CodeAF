package github

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// NudgeEvery is how often a waiting poll looks at the store's nudge mark
// ([store.Store.PollNudged]). A variable so a test can shorten it.
var NudgeEvery = time.Second

// nudged wakes a waiting poll in this process. One is buffered, so nudges
// that come while nobody waits coalesce into one read.
var nudged = make(chan struct{}, 1)

// Nudge asks the poll to read now rather than on its next tick: THE FIRST
// READ AFTER THE PICKER'S SAVE IS IMMEDIATE. It wakes a poll waiting in this
// process at once and touches the store's mark (`poll-now`), which wakes the
// poll in any other process on this machine within [NudgeEvery]; on the
// ordinary launch that is the engine, while the window is what saved. A poll
// mid-read reads again as soon as it finishes. st may be nil.
func Nudge(st *store.Store) {
	select {
	case nudged <- struct{}{}:
	default:
	}
	if st != nil {
		_ = st.NudgePoll()
	}
}

// Waker is one poll's way to wait: for a time, or until a nudge, whichever
// comes first. It remembers the store's mark as it last saw it, so a nudge is
// answered once.
type Waker struct {
	st   *store.Store
	seen time.Time
}

// NewWaker is a waker over st's nudge mark as it stands now; a nudge made
// before it is not one it answers.
func NewWaker(st *store.Store) *Waker {
	return &Waker{st: st, seen: st.PollNudged()}
}

// Wait waits d, or until a nudge, and answers false only when ctx ended
// first.
func (w *Waker) Wait(ctx context.Context, d time.Duration) bool {
	if w.moved() {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	look := time.NewTicker(max(NudgeEvery, time.Millisecond))
	defer look.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			return true
		case <-nudged:
			w.moved()
			return true
		case <-look.C:
			if w.moved() {
				return true
			}
		}
	}
}

// moved says whether the store's mark changed since last seen, and takes it
// as seen.
func (w *Waker) moved() bool {
	if w == nil || w.st == nil {
		return false
	}
	at := w.st.PollNudged()
	if at.Equal(w.seen) {
		return false
	}
	w.seen = at
	return true
}
