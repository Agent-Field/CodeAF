package relaybill

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// Schedule is when an open home screen asks the directory for its list: the
// surface's own pace, not a copy of it.
type Schedule interface {
	// Ready says whether the wait since the last answer is over.
	Ready(now time.Time) bool
	// Landed files an answer, and whether it differed from the one before.
	Landed(now time.Time, changed bool)
	// Hurry makes the next ask due now, as a person arriving on the screen does.
	Hurry()
}

// Home is one open home screen: it reads the chat list through src whenever
// its schedule says so, looking at the clock every Tick, which is the screen's
// own beat.
type Home struct {
	Src      chatlist.Source
	Schedule Schedule
	Tick     time.Duration
	last     []chatlist.Row
}

// Read asks once, as a person who just arrived does, and files the answer.
func (h *Home) Read(ctx context.Context, now time.Time) error {
	h.Schedule.Hurry()
	return h.ask(ctx, now)
}

// Run keeps the screen open until ctx ends. An attended window is assumed: the
// bill being measured is the heavy user's, who is looking at it.
func (h *Home) Run(ctx context.Context) {
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
		}
	}
}

// ask reads the list and tells the schedule whether it changed. A failed read
// counts as no change, as the screen counts it.
func (h *Home) ask(ctx context.Context, now time.Time) error {
	rows, err := h.Src.Rows(ctx)
	h.Schedule.Landed(now, err == nil && !chatlist.Same(h.last, rows))
	if err == nil {
		h.last = rows
	}
	return err
}
