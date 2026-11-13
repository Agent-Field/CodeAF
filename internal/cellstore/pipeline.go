package cellstore

import (
	"context"
	"errors"
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
	// gone says the chat's folder is no longer on this computer, read when
	// the failure is asked for rather than when the capture ended: a factory
	// job closed locally removes its checkout while the chat's capture may be
	// reading it, so the capture fails part-way through the removal and the
	// folder is only gone a moment later ([ErrWorkspaceGone]).
	gone func() bool
}

var captures = struct {
	sync.Mutex
	cells map[string]*captureState
	held  map[string]*Recorder
}{cells: map[string]*captureState{}, held: map[string]*Recorder{}}

// Await captures finished steps before a move, upload or displacement reads
// their head, then makes it durable. An open step keeps its calls in the WAL
// until the caller has written their result lines and settled it.
func Await(ctx context.Context, c cell.Cell) error {
	if err := Captured(ctx, c); err != nil {
		return err
	}
	return durable(ctx, c.Root)
}

// Captured forces held calls into a capture only between steps, without forcing
// a group commit. An open step may still owe its result lines, so boundaries
// wait only for an existing capture and never for a running tool.
func Captured(ctx context.Context, c cell.Cell) error {
	captures.Lock()
	r := captures.held[c.Root]
	captures.Unlock()
	if r != nil {
		r.scheduleAtPause(ctx)
	}
	return WaitCapture(ctx, c)
}

// Unsaved counts pending calls in the recorder held for this literal cell root.
// A closing recorder detaches; its unsaved calls remain in the WAL for recovery.
func Unsaved(c cell.Cell) int {
	captures.Lock()
	r := captures.held[c.Root]
	captures.Unlock()
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}

// scheduleAtPause checks the step and queues its capture under one lock, so a
// new tool cannot enter between the decision and the capture barrier.
func (r *Recorder) scheduleAtPause(ctx context.Context) bool {
	r.asyncMu.Lock()
	defer r.asyncMu.Unlock()
	if r.stepOpen {
		return false
	}
	r.scheduleLocked(ctx)
	return true
}

// WaitCapture joins only an existing capture. An ordinary command's ownership
// check reads no head and must not turn held calls into a full save (C28/E1).
func WaitCapture(ctx context.Context, c cell.Cell) error {
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
	// A FAILED CAPTURE OF A FOLDER THAT IS GONE IS NOT A CHAT THAT COULD NOT BE
	// SAVED: there is nothing on disk left to keep, the transcript is the
	// session file's, and the calls stay pending in the WAL. Reporting it made
	// the ninth live WorkAF round's idle quit exit 1 with "codeaf could not
	// save this chat" for a job chat whose checkout the job had taken away.
	if err != nil && state.gone != nil && state.gone() {
		return nil
	}
	return err
}

// hold makes open steps and unscheduled completions visible to boundaries that
// know the cell rather than its seat. Open steps stay in the WAL until Settle
// closes them, even when no capture has started yet.
func (r *Recorder) hold() {
	captures.Lock()
	defer captures.Unlock()
	captures.held[r.cell.Root] = r
}

func (r *Recorder) releaseHeld() {
	captures.Lock()
	defer captures.Unlock()
	if captures.held[r.cell.Root] == r {
		delete(captures.held, r.cell.Root)
	}
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

// enter keeps capture admission and the step flag under the same lock. Once a
// tool enters, a boundary leaves its batch in the WAL until Settle says that
// the result lines are written; an already queued capture still comes first.
func (r *Recorder) enter(ctx context.Context) error {
	if !r.opts.Async {
		return r.wait(ctx)
	}
	for {
		if err := r.wait(ctx); err != nil {
			return err
		}
		r.asyncMu.Lock()
		if r.running == nil {
			r.stepOpen = true
			r.hold()
			r.asyncMu.Unlock()
			return nil
		}
		r.asyncMu.Unlock()
	}
}

// Warm prepares the store before tools run without sealing an empty turn.
// A tool arriving before the screen finishes waits, so it cannot change files
// during that screen. Close and movement wait on the same worker.
func (r *Recorder) Warm(ctx context.Context) {
	r.asyncMu.Lock()
	r.warming = true
	r.asyncMu.Unlock()
	r.schedule(ctx)
}

// schedule coalesces only requests made while the same capture is running.
// The completion is already durable before this starts. A new tool waits for
// the worker, so its writes cannot be folded into an earlier call's snapshot.
func (r *Recorder) schedule(ctx context.Context) {
	r.asyncMu.Lock()
	defer r.asyncMu.Unlock()
	r.scheduleLocked(ctx)
}

// scheduleLocked installs the capture barrier before releasing admission.
// Callers hold asyncMu, including boundaries that checked the step flag.
func (r *Recorder) scheduleLocked(ctx context.Context) {
	r.requested = true
	if r.running != nil {
		return
	}
	state := &captureState{done: make(chan struct{}), gone: r.folderGone}
	r.running = state.done
	registerCapture(r.cell.Root, state)
	guard.Go("save chat", func() { r.runQueued(ctx, state) })
}

// folderGone reports whether this recorder's chat folder is no longer there:
// [Options.FolderGone] when the seat said, else a store that knows its folder
// ([Engine]); any other store says no.
func (r *Recorder) folderGone() bool {
	if r.opts.FolderGone != nil {
		return r.opts.FolderGone()
	}
	f, ok := r.store.(interface{ workspaceThere(cell.Cell) error })
	return ok && errors.Is(f.workspaceThere(r.cell), ErrWorkspaceGone)
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
	ctx = context.WithValue(ctx, daemonOwnerKey{}, r.daemonGeneration)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asyncMu.Lock()
	warm := r.warming
	r.warming = false
	r.asyncMu.Unlock()
	if prepare, ok := r.store.(interface {
		Prepare(context.Context, cell.Cell) error
	}); warm && ok {
		run, cancel := context.WithTimeout(context.WithoutCancel(ctx), SealWithin)
		err := prepare.Prepare(run, r.cell)
		cancel()
		if err != nil {
			r.report(err)
			return
		}
		if len(r.pending) == 0 && len(r.models) == 0 {
			r.releaseHeld()
			return
		}
		warm = false
	}
	if !warm && len(r.pending) == 0 && len(r.models) == 0 && !r.transcriptGrew() {
		r.releaseHeld()
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
// propagates a saving error to the caller that is closing the chat. The WAL
// keeps unsaved calls after the closing recorder leaves the held registry.
func (r *Recorder) Drain(ctx context.Context) error {
	defer r.releaseHeld()
	r.Settle(ctx)
	if err := r.wait(ctx); err != nil {
		r.report(err)
		return err
	}
	if err := durable(ctx, r.cell.Root); err != nil {
		r.report(err)
		return err
	}
	r.errorMu.Lock()
	defer r.errorMu.Unlock()
	return r.lastError
}
