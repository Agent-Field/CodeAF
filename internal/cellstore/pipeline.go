package cellstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// SealWithin bounds capture and every boundary that must keep it. A timed-out
// capture remains in the WAL and is reported, never acknowledged as sealed.
const SealWithin = executor.DrainWithin

type captureState struct {
	done chan struct{}
	err  error
}

var captures = struct {
	sync.Mutex
	cells map[string]*captureState
}{cells: map[string]*captureState{}}

// Await waits for this process's pending capture before a move, upload or
// displacement can read its head. It does not start work or contact a relay.
func Await(ctx context.Context, c cell.Cell) error {
	captures.Lock()
	state := captures.cells[c.Root]
	captures.Unlock()
	if state == nil {
		return nil
	}
	if err := awaitCapture(ctx, state.done); err != nil {
		return err
	}
	captures.Lock()
	err := state.err
	captures.Unlock()
	return err
}

func awaitCapture(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, SealWithin)
	defer cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("still saving this chat on this computer: %w", ctx.Err())
	}
}

func (r *Recorder) wait(ctx context.Context) error {
	r.asyncMu.Lock()
	done := r.running
	r.asyncMu.Unlock()
	return awaitCapture(ctx, done)
}

// schedule coalesces only requests made while the same capture is running.
// The completion is already durable before this starts. A new tool waits for
// the worker, so its writes cannot be folded into an earlier call's snapshot.
func (r *Recorder) schedule(ctx context.Context) {
	r.asyncMu.Lock()
	defer r.asyncMu.Unlock()
	r.requested = true
	if r.running != nil {
		return
	}
	state := &captureState{done: make(chan struct{})}
	r.running = state.done
	registerCapture(r.cell.Root, state)
	guard.Go("save chat", func() { r.runQueued(ctx, state) })
}

func registerCapture(root string, state *captureState) {
	captures.Lock()
	defer captures.Unlock()
	captures.cells[root] = state
}

func (r *Recorder) runQueued(ctx context.Context, state *captureState) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.report(guard.Note("save chat", recovered))
			r.finishCapture(state, true)
		}
	}()
	for {
		r.takeRequest()
		r.capture(ctx)
		if r.finishCapture(state, false) {
			return
		}
	}
}

func (r *Recorder) takeRequest() {
	r.asyncMu.Lock()
	defer r.asyncMu.Unlock()
	r.requested = false
}

func (r *Recorder) capture(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) == 0 && len(r.models) == 0 && !r.transcriptGrew() {
		return
	}
	run, cancel := context.WithTimeout(context.WithoutCancel(ctx), SealWithin)
	defer cancel()
	r.seal(run)
}

func (r *Recorder) finishCapture(state *captureState, fault bool) bool {
	r.asyncMu.Lock()
	defer r.asyncMu.Unlock()
	if r.requested && !fault {
		return false
	}
	r.errorMu.Lock()
	state.err = r.lastError
	r.errorMu.Unlock()
	completeCapture(r.cell.Root, state)
	r.running = nil
	return true
}

func completeCapture(root string, state *captureState) {
	captures.Lock()
	defer captures.Unlock()
	if captures.cells[root] == state && state.err == nil {
		delete(captures.cells, root)
	}
	close(state.done)
}

// Drain is the final, bounded boundary: it includes a transcript tail and
// propagates a saving error to the caller that is closing the chat.
func (r *Recorder) Drain(ctx context.Context) error {
	r.Settle(ctx)
	if err := r.wait(ctx); err != nil {
		r.report(err)
		return err
	}
	r.errorMu.Lock()
	defer r.errorMu.Unlock()
	return r.lastError
}
