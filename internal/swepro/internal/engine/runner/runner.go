package runner

// The 4-state runner, a transliteration of src/effect/runner.ts:32-220.
//
//	Idle | Running{run} | Shell{shell} | ShellThenRun{shell, run(pending)}
//
// SynchronizedRef.modify serialises the transition but NOT the awaiting, so
// every method here takes the mutex, decides, mutates, starts whatever
// goroutine the transition calls for, releases, and only then blocks on a
// channel. The effect half of each `modify` tuple runs after the ref is
// released, which is why onIdle and complete() are deferred into an `after`
// closure and onBusy (yielded INSIDE modifyEffect at runner.ts:153) is not.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// Work is one unit of runner work. `Effect<A, E>` with the environment already
// provided; the context is the interruption channel.
type Work[A any] func(ctx context.Context) (A, error)

// ErrCancelled is `class Cancelled extends Schema.TaggedErrorClass("RunnerCancelled")`
// (runner.ts:11). String(new Cancelled()) is "RunnerCancelled" and its
// `message` is empty, which is what Cause.pretty and the fixtures record.
var ErrCancelled error = cancelledError{}

type cancelledError struct{}

func (cancelledError) Error() string        { return "RunnerCancelled" }
func (cancelledError) ErrorName() string    { return "RunnerCancelled" }
func (cancelledError) ErrorMessage() string { return "" }

// ── Latch (effect Latch, closed at birth) ─────────────────────────────────

// Latch is Effect's Latch restricted to the one shape runner.ts uses: created
// closed, awaited by stopShell, opened once by the shell body when it reaches
// a safe point (prompt.ts:1874).
type Latch struct {
	ch   chan struct{}
	once sync.Once
}

func NewLatch() *Latch { return &Latch{ch: make(chan struct{})} }

// Open releases everyone waiting. Idempotent, like Deferred.succeed.
func (l *Latch) Open() { l.once.Do(func() { close(l.ch) }) }

// Await is `latch.await` as a channel.
func (l *Latch) Await() <-chan struct{} { return l.ch }

// ── deferred ──────────────────────────────────────────────────────────────

// deferred is Deferred<A, E | Cancelled>: settled at most once, then readable
// by any number of waiters.
type deferred[A any] struct {
	done chan struct{}
	once sync.Once
	val  A
	err  error
}

func newDeferred[A any]() *deferred[A] { return &deferred[A]{done: make(chan struct{})} }

// settle is Deferred.done / Deferred.fail. A second call is a no-op, matching
// Effect's "already completed" return of false.
func (d *deferred[A]) settle(v A, err error) {
	d.once.Do(func() {
		d.val, d.err = v, err
		close(d.done)
	})
}

// await blocks until settled. A caller whose own context dies first sees an
// interruption of the AWAITING fiber, which in Effect is an interrupt cause on
// the caller, not a Cancelled on the deferred.
func (d *deferred[A]) await(ctx context.Context) (A, error) {
	select {
	case <-d.done:
		return d.val, d.err
	case <-ctx.Done():
		var zero A
		return zero, Interrupt()
	}
}

// ── handles ───────────────────────────────────────────────────────────────

type runHandle[A any] struct {
	id     float64
	cancel context.CancelCauseFunc
	d      *deferred[A]
}

type pendingHandle[A any] struct {
	id   float64
	d    *deferred[A]
	work Work[A]
}

type shellHandle[A any] struct {
	id     float64
	cancel context.CancelCauseFunc
	// cancelled is the `cancelled: Deferred<void>` at runner.ts:21 — read by
	// the shell await path to tell "it stopped because we asked" from "it
	// stopped on its own".
	cancelled atomic.Bool
	ready     *Latch
	d         *deferred[A]
}

// ── state ─────────────────────────────────────────────────────────────────

type state uint8

const (
	stIdle state = iota
	stRunning
	stShell
	stShellThenRun
)

func (s state) String() string {
	switch s {
	case stRunning:
		return "Running"
	case stShell:
		return "Shell"
	case stShellThenRun:
		return "ShellThenRun"
	default:
		return "Idle"
	}
}

// Options mirrors the `opts` bag of runner.ts:38-45. A nil hook is TS's
// `undefined`, not a no-op default with different behaviour: OnInterrupt in
// particular changes what a cancelled turn returns.
type Options[A any] struct {
	// OnIdle is `opts.onIdle`. Runs whenever the runner reaches Idle — and
	// note it runs OUTSIDE the mutex, see the package comment.
	OnIdle func()
	// OnBusy is `opts.onBusy`. Only startShell ever runs it.
	OnBusy func()
	// OnInterrupt is `opts.onInterrupt`. When set, a cancelled turn RESOLVES
	// with its result (codeaf passes lastAssistant(sessionID), prompt.ts:1869)
	// instead of failing.
	OnInterrupt func() (A, error)
	// Busy is `opts.busy`, a function that must not return. run-state.ts:63-65
	// throws Session.BusyError from it.
	Busy func()
}

// Runner is Runner<A, E>.
type Runner[A any] struct {
	mu      sync.Mutex
	st      state
	changed chan struct{}
	ids     float64 // runner.ts:51 `let ids = 0`
	run     *runHandle[A]
	pending *pendingHandle[A]
	shell   *shellHandle[A]

	// scopeCtx is the Scope runs are forked into (Effect.forkIn(scope),
	// runner.ts:88). Shells fork off the CALLER's context instead
	// (Effect.forkChild, runner.ts:156).
	scopeCtx context.Context
	opts     Options[A]
}

// New is `Runner.make(scope, opts)`.
func New[A any](scopeCtx context.Context, opts Options[A]) *Runner[A] {
	if scopeCtx == nil {
		scopeCtx = context.Background()
	}
	return &Runner[A]{scopeCtx: scopeCtx, opts: opts, changed: make(chan struct{})}
}

// State is the `state` getter, as the TS tag string.
func (r *Runner[A]) State() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.st.String()
}

// Busy is the `busy` getter: `state._tag !== "Idle"`.
func (r *Runner[A]) Busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.st != stIdle
}

// setStateLocked updates the state and wakes transition observers. Runner
// operations do not wait on changed; it exists so concurrency tests and future
// orchestration can rendezvous on an actual transition without polling.
func (r *Runner[A]) setStateLocked(next state) {
	r.st = next
	close(r.changed)
	r.changed = make(chan struct{})
}

func (r *Runner[A]) waitForState(ctx context.Context, want state) error {
	for {
		r.mu.Lock()
		if r.st == want {
			r.mu.Unlock()
			return nil
		}
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

// next is runner.ts:54-57.
func (r *Runner[A]) next() float64 {
	r.ids += 1
	return r.ids
}

// complete is runner.ts:59-62: an interrupt-only exit is reported as
// Cancelled, everything else passes through untouched.
func (r *Runner[A]) complete(d *deferred[A], v A, err error) {
	if err != nil && IsInterruptOnly(err) {
		var zero A
		d.settle(zero, ErrCancelled)
		return
	}
	d.settle(v, err)
}

// awaitDone is runner.ts:64-65. On Cancelled it runs onInterrupt if one was
// supplied and otherwise DIES — Effect.die, so a defect, not a failure.
func (r *Runner[A]) awaitDone(ctx context.Context, d *deferred[A]) (A, error) {
	v, err := d.await(ctx)
	if err != nil && errors.Is(err, ErrCancelled) {
		if r.opts.OnInterrupt != nil {
			return r.opts.OnInterrupt()
		}
		var zero A
		return zero, Die(ErrCancelled)
	}
	return v, err
}

// idleIfCurrent is runner.ts:67-68: run onIdle only if the state is ALREADY
// Idle. A compare-and-run guard against a concurrent transition.
func (r *Runner[A]) idleIfCurrent() {
	r.mu.Lock()
	isIdle := r.st == stIdle
	r.mu.Unlock()
	if isIdle && r.opts.OnIdle != nil {
		r.opts.OnIdle()
	}
}

// finishRun is runner.ts:70-81, the run goroutine's onExit.
func (r *Runner[A]) finishRun(id float64, d *deferred[A], v A, err error) {
	r.mu.Lock()
	match := r.st == stRunning && r.run != nil && r.run.id == id
	if match {
		r.setStateLocked(stIdle)
		r.run = nil
	}
	r.mu.Unlock()

	if match && r.opts.OnIdle != nil {
		r.opts.OnIdle()
	}
	r.complete(d, v, err)
}

// startRun is runner.ts:83-91. Must be called with the mutex held; the
// goroutine it forks takes the mutex itself, in finishRun.
func (r *Runner[A]) startRun(work Work[A], d *deferred[A]) *runHandle[A] {
	id := r.next()
	ctx, cancel := context.WithCancelCause(r.scopeCtx)
	h := &runHandle[A]{id: id, cancel: cancel, d: d}
	go func() {
		v, err := runWork(ctx, work)
		cancel(nil)
		// onIdle is user-provided code inside finishRun. A panic there is an
		// Effect defect too; never let it escape the runner goroutine.
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					var zero A
					d.settle(zero, Die(rec))
				}
			}()
			r.finishRun(id, d, v, err)
		}()
	}()
	return h
}

// finishShell is runner.ts:93-106, the shell's `ensuring` finalizer and the
// promotion point: a pending run starts here, and only here.
func (r *Runner[A]) finishShell(id float64) {
	r.mu.Lock()
	var after func()
	switch {
	case r.st == stShell && r.shell != nil && r.shell.id == id:
		r.setStateLocked(stIdle)
		r.shell = nil
		after = r.opts.OnIdle
	case r.st == stShellThenRun && r.shell != nil && r.shell.id == id:
		p := r.pending
		r.run = r.startRun(p.work, p.d)
		r.pending = nil
		r.shell = nil
		r.setStateLocked(stRunning)
	}
	r.mu.Unlock()
	if after != nil {
		after()
	}
}

// stopShell is runner.ts:108-113. The ready await is a rendezvous with no
// deadline: the shell promises to open the latch once it is safe to kill.
func (r *Runner[A]) stopShell(sh *shellHandle[A]) {
	if sh.ready != nil {
		<-sh.ready.Await()
	}
	sh.cancelled.Store(true)
	sh.cancel(ErrInterrupted)
	<-sh.d.done
}

// EnsureRunning is runner.ts:115-138.
//
// ctx is the CALLER's context and governs only the wait: a caller that walks
// away does not stop the run, which was forked into the runner's scope.
func (r *Runner[A]) EnsureRunning(ctx context.Context, work Work[A]) (A, error) {
	r.mu.Lock()
	var d *deferred[A]
	switch r.st {
	case stRunning:
		// runner.ts:120-122 — `work` is dropped on the floor.
		d = r.run.d
	case stShellThenRun:
		d = r.pending.d
	case stShell:
		d = newDeferred[A]()
		r.pending = &pendingHandle[A]{id: r.next(), d: d, work: work}
		r.setStateLocked(stShellThenRun)
	default: // Idle
		d = newDeferred[A]()
		r.run = r.startRun(work, d)
		r.setStateLocked(stRunning)
	}
	r.mu.Unlock()
	return r.awaitDone(ctx, d)
}

// StartShell is runner.ts:140-174.
//
// PANICS when the runner is busy — see the package comment. `ready` may be nil.
func (r *Runner[A]) StartShell(ctx context.Context, work Work[A], ready *Latch) (A, error) {
	r.mu.Lock()
	if r.st != stIdle {
		r.mu.Unlock()
		// runner.ts:146-149. opts.busy() never returns in codeaf, so the
		// second throw is dead code that is kept anyway.
		if r.opts.Busy != nil {
			r.opts.Busy()
		}
		panic(errors.New("Runner is busy"))
	}
	if r.opts.OnBusy != nil {
		r.opts.OnBusy() // runner.ts:153 — inside the ref, unlike onIdle.
	}
	id := r.next()
	shellCtx, cancel := context.WithCancelCause(ctx)
	sh := &shellHandle[A]{id: id, cancel: cancel, ready: ready, d: newDeferred[A]()}
	r.shell = sh
	r.setStateLocked(stShell)
	go func() {
		v, err := runWork(shellCtx, work)
		cancel(nil)
		// Effect.ensuring runs before the fiber's exit is observable, so the
		// promotion/idle transition is always done by the time the awaiting
		// caller wakes up. A panic in that finalizer becomes the shell's
		// defect, rather than escaping the goroutine.
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					var zero A
					sh.d.settle(zero, Die(rec))
				}
			}()
			r.finishShell(id)
			sh.d.settle(v, err)
		}()
	}()
	r.mu.Unlock()

	v, err := sh.d.await(ctx)
	if err == nil {
		return v, nil
	}
	// runner.ts:162-164. The second disjunct: we asked it to stop, it did stop
	// partly by interruption, and it did not panic.
	if IsInterruptOnly(err) || (sh.cancelled.Load() && HasInterrupts(err) && !HasDefects(err)) {
		if r.opts.OnInterrupt != nil {
			return r.opts.OnInterrupt()
		}
		var zero A
		return zero, Die(ErrCancelled)
	}
	var zero A
	return zero, err
}

// Cancel is runner.ts:176-207. The state goes Idle synchronously; the stopping
// happens after the ref is released, which is why a concurrent State() during
// a cancel reports Idle while the shell is still winding down.
func (r *Runner[A]) Cancel() {
	r.mu.Lock()
	st := r.st
	run, sh, pend := r.run, r.shell, r.pending
	if st != stIdle {
		r.setStateLocked(stIdle)
		r.run, r.shell, r.pending = nil, nil, nil
	}
	r.mu.Unlock()

	switch st {
	case stIdle:
		return
	case stRunning:
		run.cancel(ErrInterrupted)
		<-run.d.done // Effect.exit — the outcome is swallowed
		r.idleIfCurrent()
	case stShell:
		r.stopShell(sh)
		r.idleIfCurrent()
	case stShellThenRun:
		r.stopShell(sh)
		var zero A
		pend.d.settle(zero, ErrCancelled)
		r.idleIfCurrent()
	}
}
