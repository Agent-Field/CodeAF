// Package offpath holds the two shapes of work that must happen but must not
// happen HERE — on the path a person is waiting on.
//
// THE PATH IS THE THING BEING PROTECTED. Between a person pressing Enter and
// the first word arriving, and again between a batch of tools finishing and the
// next request leaving, this program does a surprising amount of work that
// nobody asked for at that instant: it runs `git status` to find out whether the
// tree moved, it writes a counter file to record that something was looked up,
// it stamps a lookup file so a picker can sort. Each of those is milliseconds on
// a small machine and seconds on a big one, each was written where it was needed
// rather than where it was cheap, and together they are the difference between a
// surface that feels immediate and one that does not.
//
// There are exactly two answers, and this package is both of them so that
// nobody writes a third:
//
//   - [Reading] — a fact somebody will want SOON, gathered beside the work
//     instead of in front of it, and taken when it is wanted with a bound on how
//     long the taker will wait.
//   - [Write] — a write a read path OWES but must not perform, coalesced and
//     performed behind the path, with one door ([Write.Settle]) for whoever has
//     to know it landed.
//
// NEITHER IS A QUEUE AND NEITHER IS A WORKER POOL. Both are one goroutine per
// live thing, started when there is something to do and gone when there is not,
// because the work they carry is one command or one small file — and a pool
// would be a second scheduler for a program that already has one.
package offpath

import (
	"sync"
	"time"
)

// ── a fact gathered beside the work ─────────────────────────────────────────

// Reading is a fact being gathered somewhere else.
//
// THE LAW IT KEEPS: the gathering starts when [Take] is called and the caller
// pays nothing for it until [Reading.Settle], which waits only as long as the
// caller says it may. A gathering that is not finished by then is NOT cancelled
// and NOT thrown away — it stays on the same Reading and the next Settle takes
// it for free. So a reading that is fast is exact, and a reading that is slow
// arrives late rather than making somebody wait; what it must never do is put
// its own duration in front of a person.
//
// IT HOLDS ONE VALUE AND IS TAKEN ONCE. A settled Reading is spent: the caller
// has the fact and the Reading has nothing left to give. Take another.
type Reading[T any] struct {
	done chan T
}

// Take starts gathering. The function runs on a goroutine of its own and must
// not touch anything the path it was started from is holding a lock on — it is
// running BESIDE that path, which is the whole point.
func Take[T any](gather func() T) *Reading[T] {
	// The channel is buffered so the gatherer never blocks on a reading nobody
	// came back for: a Reading that is dropped must not leak the goroutine that
	// was filling it.
	done := make(chan T, 1)
	go func() { done <- gather() }()
	return &Reading[T]{done: done}
}

// Settle takes the fact if it is here, waiting at most `within` for it.
//
// The second return says whether the fact arrived. A false is not a failure: it
// is "not yet", and the same Reading answers true on a later Settle.
func (r *Reading[T]) Settle(within time.Duration) (T, bool) {
	var zero T
	if r == nil || r.done == nil {
		return zero, false
	}
	if within <= 0 {
		select {
		case value := <-r.done:
			return value, true
		default:
			return zero, false
		}
	}
	timer := time.NewTimer(within)
	defer timer.Stop()
	select {
	case value := <-r.done:
		return value, true
	case <-timer.C:
		return zero, false
	}
}

// Wait takes the fact however long it takes. It is for a caller that genuinely
// cannot go on without it — and a caller on a person's path is never that
// caller.
func (r *Reading[T]) Wait() T {
	var zero T
	if r == nil || r.done == nil {
		return zero
	}
	return <-r.done
}

// ── a write a read path owes ────────────────────────────────────────────────

// Write is a write that is owed and is performed behind the path that owed it.
//
// THE LAW IT KEEPS: [Write.Owe] never blocks and never writes. It records that
// the thing behind it has changed and makes sure exactly one goroutine is
// performing the write; a hundred Owes while one write is running collapse into
// ONE further write, because what is being written is the current state and not
// a log of changes.
//
// AND IT IS NOT A TIMER. The write starts immediately, so the window in which a
// process could die holding an owed write is the length of one write rather
// than the length of somebody's chosen delay. [Write.Settle] closes even that,
// for the exit doors and for every test that reads the file back.
type Write struct {
	perform func()

	mu      sync.Mutex
	owed    bool
	running bool
	idle    chan struct{}
}

// Deferred names a write and what performs it. The function is called with
// nothing held by this type, so it is free to take whatever lock the thing it
// writes is guarded by.
func Deferred(perform func()) *Write {
	return &Write{perform: perform}
}

// Owe records that the write is due. It returns at once, always.
func (w *Write) Owe() {
	if w == nil || w.perform == nil {
		return
	}
	w.mu.Lock()
	w.owed = true
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	if w.idle == nil {
		w.idle = make(chan struct{})
	}
	w.mu.Unlock()
	go w.drain()
}

// drain performs the write until nothing is owed, then lets anyone settling go.
func (w *Write) drain() {
	for {
		w.mu.Lock()
		if !w.owed {
			w.running = false
			idle := w.idle
			w.idle = nil
			w.mu.Unlock()
			if idle != nil {
				close(idle)
			}
			return
		}
		w.owed = false
		w.mu.Unlock()
		w.perform()
	}
}

// Settle waits until nothing is owed and nothing is being written.
//
// IT IS THE EXIT DOOR AND THE TEST'S DOOR, and it is the reason a deferred write
// is not a lost write: whoever closes a session calls it, and whoever asserts on
// the file calls it. Nothing on a person's path does.
func (w *Write) Settle() {
	if w == nil {
		return
	}
	for {
		w.mu.Lock()
		// A WRITE IS RUNNING EXACTLY WHEN THERE IS A CHANNEL TO WAIT ON. Both
		// are set together under this lock in [Write.Owe] and cleared together
		// in drain, so there is no window in which a settler has to guess.
		idle := w.idle
		w.mu.Unlock()
		if idle == nil {
			return
		}
		<-idle
	}
}
