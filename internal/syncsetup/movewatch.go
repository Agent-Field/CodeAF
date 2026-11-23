package syncsetup

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/handoff"
)

// MoveIdleWithin is how long a move may wait on the relay with nothing coming
// through before it stops. It is the take's twin of PublishIdleWithin: a move
// is bounded by progress, not by size, so a large chat copies for as long as
// its bytes keep arriving, and a stuck connection still ends it. Local work
// (sealing edits kept here, restoring the folder) never counts against it.
// Tests shorten it.
var MoveIdleWithin = 2 * time.Minute

// ErrMoveStalled ends a move that waited MoveIdleWithin on the relay with
// nothing coming through. Nothing was acquired by then, or the acquire is
// handed back, so the chat stays on the computer it was on.
var ErrMoveStalled = errors.New("nothing came through for 2 minutes")

// moveReportEvery is how often a move reports how far it has got.
var moveReportEvery = 250 * time.Millisecond

// Move steps, in the order a move goes through them.
const (
	MovePreparing = "preparing" // reading the record, keeping local edits
	MoveCopying   = "copying"   // bytes are arriving
	MoveOpening   = "opening"   // the chat is ours; it is being put in place
	MoveArriving  = "arriving"  // chat is open; workspace files are arriving in the background
	MoveFailed    = "failed"    // background workspace fetch failed after retries
	MoveDone      = "done"      // move and background workspace files are complete
)

// MoveProgress is how far one move has got.
type MoveProgress struct {
	Step string
	// Done is the bytes copied to this computer so far.
	Done int64
	// Total is the bytes the chat's record says a copy takes, 0 when it says
	// nothing or this computer already holds most of it.
	Total int64
	// Err is the plain-words reason a background workspace fetch stopped, "" otherwise.
	Err string
}

// moveWatch follows one move: it reports progress, and ends the move once it
// has waited MoveIdleWithin on the relay with nothing coming through.
type moveWatch struct {
	report func(MoveProgress)
	idle   time.Duration
	ctx    context.Context
	cancel context.CancelCauseFunc

	done     atomic.Int64
	total    atomic.Int64
	inflight atomic.Int64
	mu       sync.Mutex
	step     string
	err      string
	arrival  *cell.WorkspaceArrival
	last     time.Time // last request start, end or byte
	wake     chan struct{}
	stop     chan struct{}
	stopped  sync.Once
	finished chan struct{}
}

var _ blobstore.TrafficWatch = (*moveWatch)(nil)

// watchMove returns ctx carrying the watch, and a stop that ends it. With
// neither a report nor a limit it returns ctx unchanged.
func watchMove(ctx context.Context, report func(MoveProgress), idle time.Duration) (context.Context, *moveWatch, func()) {
	if report == nil && idle <= 0 {
		return ctx, nil, func() {}
	}
	ctx, cancel := context.WithCancelCause(ctx)
	w := &moveWatch{report: report, idle: idle, ctx: ctx, cancel: cancel, step: MovePreparing, last: time.Now(), wake: make(chan struct{}, 1), stop: make(chan struct{}), finished: make(chan struct{})}
	go w.run()
	return blobstore.WithTraffic(ctx, w), w, func() {
		w.stopped.Do(func() { close(w.stop) })
		<-w.finished
		cancel(nil)
	}
}

func (w *moveWatch) run() {
	defer close(w.finished)
	tick := time.NewTicker(min(moveReportEvery, max(w.idle/4, time.Millisecond)))
	defer tick.Stop()
	var sent MoveProgress
	reported := false
	for {
		now := w.now()
		if w.report != nil && (!reported || now != sent) {
			w.report(now)
			sent, reported = now, true
		}
		if w.stalled() {
			w.cancel(ErrMoveStalled)
			return
		}
		select {
		case <-w.stop:
			now := w.now()
			if w.report != nil && (!reported || now != sent) {
				w.report(now)
			}
			return
		case <-w.wake:
		case <-tick.C:
		}
	}
}

func (w *moveWatch) notify() {
	if w == nil || w.wake == nil {
		return
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *moveWatch) now() MoveProgress {
	w.mu.Lock()
	step, errText := w.step, w.err
	w.mu.Unlock()
	done, total := w.done.Load(), w.total.Load()
	if done > total {
		// The record undercounts (objects another chat had already shared
		// arrive outside its count): say what has arrived, not a wrong share.
		total = 0
	}
	return MoveProgress{Step: step, Done: done, Total: total, Err: errText}
}

// stalled says a request has waited the whole limit with nothing coming
// through. Between requests the move is doing local work, which the limit
// does not cover, so the clock starts again with each request.
func (w *moveWatch) stalled() bool {
	if w.idle <= 0 || w.inflight.Load() == 0 {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return time.Since(w.last) >= w.idle
}

func (w *moveWatch) touch() {
	w.mu.Lock()
	w.last = time.Now()
	arr := w.arrival
	w.mu.Unlock()
	if arr != nil {
		arr.Touch()
	}
}

// Begin implements blobstore.TrafficWatch.
func (w *moveWatch) Begin() {
	w.inflight.Add(1)
	w.touch()
}

// End implements blobstore.TrafficWatch.
func (w *moveWatch) End() {
	w.inflight.Add(-1)
	w.touch()
}

// Moved implements blobstore.TrafficWatch. Only bytes arriving count as
// copied; an upload of edits kept here still shows the move is alive.
func (w *moveWatch) Moved(_, down int64) {
	if down > 0 {
		w.done.Add(down)
		w.setStep(MoveCopying, false)
		w.notify()
	}
	w.touch()
}

// phase hears the take's own measurements: once the lease is acquired the
// chat is ours and the rest is putting it in place.
func (w *moveWatch) phase(p handoff.Phase) {
	if w == nil {
		return
	}
	if p.Name == "acquire" && !p.Ended.IsZero() && w.ctx.Err() == nil {
		// A move cancelled during the acquire hands the lease back: it is
		// not opening anything.
		w.setStep(MoveOpening, true)
		w.notify()
	}
	w.touch()
}

// setStep moves the step on; a later step is never undone by bytes of an
// earlier one unless force (a head that moved fetches again after acquire).
func (w *moveWatch) setStep(step string, force bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if force || w.step == MovePreparing {
		w.step = step
	}
}

// expect sets the bytes the copy is expected to take.
func (w *moveWatch) expect(total int64) {
	if w != nil && total > 0 {
		w.total.Store(total)
		w.notify()
	}
}

func (w *moveWatch) setArrival(a *cell.WorkspaceArrival) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.arrival = a
	w.mu.Unlock()
}

func (w *moveWatch) setArriving() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.step = MoveArriving
	w.err = ""
	w.last = time.Now()
	w.mu.Unlock()
	if w.report != nil {
		w.report(w.now())
	}
}

func (w *moveWatch) setFailed(reason string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.step = MoveFailed
	w.err = reason
	w.last = time.Now()
	w.mu.Unlock()
	if w.report != nil {
		w.report(w.now())
	}
}

func (w *moveWatch) setDone() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.step = MoveDone
	w.err = ""
	w.last = time.Now()
	w.mu.Unlock()
	if w.report != nil {
		w.report(w.now())
	}
}
