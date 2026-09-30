package relaybill

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// Schedule is when an open home screen asks the directory for its list: the
// surface's own pace, not a copy of it.
type Schedule interface {
	// Ready says whether the wait since the last answer is over.
	Ready(now time.Time) bool
	// Landed files an answer: whether it differed from the one before, and
	// whether the change socket was up when it came.
	Landed(now time.Time, changed, watched bool)
	// Hurry makes the next ask due now, as a person arriving on the screen does.
	Hurry()
}

// followable is a chat list source that can follow the directory's change feed.
type followable interface{ Follow() dirwatch.Follower }

// Home is one open home screen: it reads the chat list through src whenever
// its schedule says so, looking at the clock every Tick, which is the screen's
// own beat, and follows the directory's change socket the way the screen does:
// a frame announcing a version past the last reading asks at once.
type Home struct {
	Src      chatlist.Source
	Schedule Schedule
	Tick     time.Duration
	last     []chatlist.Row
	version  uint64
	feed     dirwatch.Follower
}

// Read is a person arriving on the screen: the socket is joined, the list is
// asked once at once, and the screen is left again.
func (h *Home) Read(ctx context.Context, now time.Time) error {
	h.join()
	defer h.leave()
	h.Schedule.Hurry()
	return h.ask(ctx, now)
}

// Run keeps the screen open until ctx ends. An attended window is assumed: the
// bill being measured is the heavy user's, who is looking at it.
func (h *Home) Run(ctx context.Context) {
	h.join()
	defer h.leave()
	h.Schedule.Hurry()
	_ = h.ask(ctx, time.Now())
	tick := time.NewTicker(h.Tick)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			if h.Schedule.Ready(now) {
				_ = h.ask(ctx, now)
			}
		case <-h.changes():
			if h.feedAhead() {
				_ = h.ask(ctx, time.Now())
			}
		}
	}
}

// join follows the feed if the source has one.
func (h *Home) join() {
	if src, ok := h.Src.(followable); ok {
		h.feed = src.Follow()
	}
}

// leave lets the feed go.
func (h *Home) leave() {
	if h.feed != nil {
		h.feed.Close()
		h.feed = nil
	}
}

// changes is the feed's signal, or a channel that never fires without one.
func (h *Home) changes() <-chan struct{} {
	if h.feed == nil {
		return nil
	}
	return h.feed.Changes()
}

// feedAhead says whether the feed has announced a version past the reading.
func (h *Home) feedAhead() bool { return h.feed != nil && h.feed.State().Version > h.version }

// socketUp says whether the change socket is up.
func (h *Home) socketUp() bool { return h.feed != nil && h.feed.State().Up }

// ask reads the list and tells the schedule whether it changed. A failed read
// counts as no change, as the screen counts it.
func (h *Home) ask(ctx context.Context, now time.Time) error {
	rows, version, err := chatlist.RowsAt(ctx, h.Src)
	h.Schedule.Landed(now, err == nil && !chatlist.Same(h.last, rows), h.socketUp())
	if err == nil {
		h.last, h.version = rows, version
	}
	return err
}
