package session

// ── ONE READING BESIDE THE WORK ─────────────────────────────────────────────
//
// A SIDECAR is a reading this harness makes on its own behalf while the person's
// work carries on: which remembered lines this message needs, what is left of a
// long answer, whether what somebody typed was really a job for a task. None of
// them is anything anybody asked for, and loop.go's law is that none of them may
// ever stand in front of the work — they run BESIDE it, and the only power they
// have over it is to INTERRUPT it.
//
// THERE IS ONE MECHANISM FOR THAT SHAPE AND THIS IS IT. There were four, and
// that is the whole reason this file exists: route_judge.go's race, the memory
// pass awaited as a line of the turn, the mark's reader awaited as another, and
// the post-turn cascade awaited as a third — three of them ad-hoc goroutines or
// straight-line calls with their own idea of what a cancelled reading means, and
// one of them (the race) the shape all four should have had. A reading that is
// its own goroutine with its own channel is a reading whose cancellation, whose
// one-answer rule and whose "has it landed yet" test are written again by
// whoever adds the next one, and the next one is where they get written wrong.
//
// THE LAW IS THREE VERBS, and every caller uses all three:
//
//	START  where the question first becomes askable — as early as possible,
//	       because everything the work does between here and the answer is
//	       latency the reading hides behind.
//	TAKE   where the answer can still be SPENT, and never anywhere else. It is
//	       non-blocking by construction: a reading still in flight costs the
//	       caller one closed-channel test and is asked again at its next chance.
//	END    where nobody can spend it any more. It does not wait for the
//	       goroutine — a caller that waited here for a provider to notice a
//	       cancelled context would have moved the wait to the other end of the
//	       turn — and the reading is bounded by its own window either way.
//
// [sidecar.settle] is the fourth verb and it is the exception rather than the
// rule: it WAITS. It exists for the one moment a reading has nothing to run
// beside — the end of a turn, after the model has stopped writing — and every
// use of it is a place where the law above has been thought about and does not
// apply. There is exactly one other reading in this package that must precede
// the work rather than ride beside it, and it is named in loop.go's header and
// in guardian.go: the safety gate, which decides whether a tool RUNS and so has
// nothing to be applied to afterwards.
//
// AND A READING MAY NOT BLOCK. `act` runs on the sidecar's own goroutine the
// instant the answer lands, and what it is for is the interruption: cutting the
// generation in flight (steer.go's [Agent.cutGeneration]) so that the boundary
// where the answer can be spent arrives at once rather than whenever the step
// happens to end. Anything slower than that belongs in the caller's `take`.

import (
	"context"
	"sync/atomic"
)

// sidecar is one reading running beside the work. T is whatever the reading
// answers with; a caller that wants no answer at all uses struct{}.
//
// THE ANSWER IS WRITTEN ONCE AND READ AFTER. `settled` closing is the whole of
// the synchronisation: the goroutine writes `answer` before it closes the
// channel, every reader reads it afterwards, and there is no lock because there
// is nothing left to contend for. `taken` is touched only by the caller's own
// goroutine, which is the one place the ONE-ANSWER rule is decided.
type sidecar[T any] struct {
	stop    context.CancelFunc
	settled chan struct{}
	answer  T
	taken   bool
	// asked says a reading was started at all. It outlives `taken`, so a caller
	// writing down what ran beside its work can still say so after spending it.
	// It is an atomic because the decomposition row is written by the turn while
	// a late reading may still be finishing.
	asked atomic.Bool
}

// readBeside starts one reading beside the work.
//
// ctx is the WORK'S OWN context, so a reading cannot outlive the thing it was
// read for; the reading gets a child of it that [sidecar.end] closes.
//
// ask is the reading. It is given the sidecar's context and must respect it.
//
// act is OPTIONAL and is the reading's one power over the work: it is called on
// this goroutine the instant the answer lands, before anybody can take it, and
// it is where an interruption is raised. It must be as cheap as a stream
// observer. Nil is the ordinary case — most readings simply wait to be taken.
func readBeside[T any](ctx context.Context, ask func(context.Context) T, act func(T)) *sidecar[T] {
	if ask == nil {
		return nil
	}
	readCtx, stop := context.WithCancel(ctx)
	side := &sidecar[T]{stop: stop, settled: make(chan struct{})}
	side.asked.Store(true)
	go func() {
		answer := ask(readCtx)
		side.answer = answer
		close(side.settled)
		if act != nil {
			act(answer)
		}
	}()
	return side
}

// take answers the reading IF IT HAS ALREADY LANDED, and never waits.
//
// A SETTLED SIDECAR ANSWERS ONCE. Whatever the caller does with the answer, the
// reading has said its piece — including when the answer was "nothing", which
// cannot become something later. A nil sidecar is a reading that was never
// started, which is the ordinary case for every gated turn and is what lets a
// caller hold this in one line with no branch around it.
func (s *sidecar[T]) take() (T, bool) {
	var none T
	if s == nil || s.taken {
		return none, false
	}
	select {
	case <-s.settled:
		s.taken = true
		s.stop()
		return s.answer, true
	default:
		return none, false
	}
}

// settle WAITS for the reading and then answers it, once.
//
// IT IS THE EXCEPTION AND NOT THE RULE — see this file's header. Use it only
// where there is genuinely nothing left to run beside, and where the reading is
// bounded by a window of its own; the work's context bounds it too, so a stopped
// turn does not wait here.
func (s *sidecar[T]) settle() (T, bool) {
	var none T
	if s == nil || s.taken {
		return none, false
	}
	<-s.settled
	s.taken = true
	s.stop()
	return s.answer, true
}

// pending reports that a reading is in flight or landed and not yet spent. It is
// what a caller asks before starting a second one: two readers shown the same
// question a step apart answer it twice and the turn pays for both.
func (s *sidecar[T]) pending() bool { return s != nil && !s.taken }

// everAsked reports whether this reading was started at all, for a caller
// writing down what ran beside its work.
func (s *sidecar[T]) everAsked() bool { return s != nil && s.asked.Load() }

// end lets the reading go, whether or not it has answered. It does not wait —
// see the header — and it is safe to call on a sidecar that has already been
// taken, which is what lets it be deferred at the top of a turn.
func (s *sidecar[T]) end() {
	if s == nil || s.stop == nil {
		return
	}
	s.stop()
}
