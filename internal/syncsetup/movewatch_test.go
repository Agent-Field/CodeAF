package syncsetup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/handoff"
)

// A request that waits the whole limit with nothing coming through ends the
// move with ErrMoveStalled.
func TestMoveWatchStopsARequestWithNothingComingThrough(t *testing.T) {
	ctx, w, stop := watchMove(context.Background(), nil, 40*time.Millisecond)
	defer stop()
	w.Begin()
	select {
	case <-ctx.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("a stalled request never ended the move")
	}
	if !errors.Is(context.Cause(ctx), ErrMoveStalled) {
		t.Fatalf("cause = %v, want ErrMoveStalled", context.Cause(ctx))
	}
}

// Local work between requests (sealing, restoring) never counts against the
// limit, and bytes arriving start the clock again.
func TestMoveWatchCountsOnlyWaitsOnTheRelay(t *testing.T) {
	w := &moveWatch{idle: time.Minute, step: MovePreparing}
	w.last = time.Now().Add(-time.Hour)
	if w.stalled() {
		t.Fatal("no request in flight, yet the move counted as stalled")
	}
	w.inflight.Add(1)
	if !w.stalled() {
		t.Fatal("a request waited an hour, yet the move was not stalled")
	}
	w.Moved(0, 10)
	if w.stalled() {
		t.Fatal("bytes arrived, yet the move still counted as stalled")
	}
}

// The report says the step and the bytes, and never shows more than all of a
// total the record undercounted.
func TestMoveWatchReportsStepAndBytes(t *testing.T) {
	var mu sync.Mutex
	var seen []MoveProgress
	_, w, stop := watchMove(context.Background(), func(p MoveProgress) {
		mu.Lock()
		seen = append(seen, p)
		mu.Unlock()
	}, 0)
	defer stop()
	last := func() MoveProgress {
		mu.Lock()
		defer mu.Unlock()
		if len(seen) == 0 {
			return MoveProgress{}
		}
		return seen[len(seen)-1]
	}
	waitFor(t, "the first report", func() bool { return last().Step == MovePreparing })
	w.expect(100)
	w.Begin()
	w.Moved(0, 40)
	waitFor(t, "a copying report", func() bool { return last() == MoveProgress{Step: MoveCopying, Done: 40, Total: 100} })
	w.Moved(0, 80)
	waitFor(t, "an undercounted total dropped", func() bool { return last() == MoveProgress{Step: MoveCopying, Done: 120} })
	w.End()
	w.phase(handoff.Phase{Name: "acquire", Started: time.Now(), Ended: time.Now()})
	waitFor(t, "an opening report", func() bool { return last().Step == MoveOpening })
}
