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
// [sidecar.takeAtTheEnd] is the fourth verb and it is the exception rather than
// the rule: it WAITS. It exists for the one moment a reading has nothing to run
// beside — the end of a turn, after the model has stopped writing — and IT IS
// NAMED FOR THE ONLY CONDITION UNDER WHICH IT IS HONEST so that the law can
// find it: sidecar_law_test.go holds every call to it to the same ending-road
// property it holds an awaited reading to, and nothing else in this package is
// spelled that way. There is exactly one other reading in this package that must precede
// the work rather than ride beside it, and it is named in loop.go's header and
// in guardian.go: the safety gate, which decides whether a tool RUNS and so has
// nothing to be applied to afterwards.
//
// AND A READING MAY NOT BLOCK. `act` runs on the sidecar's own goroutine the
// instant the answer lands, and what it is for is the interruption: cutting the
// generation in flight (steer.go's [Agent.cutGeneration]) so that the boundary
// where the answer can be spent arrives at once rather than whenever the step
// happens to end. Anything slower than that belongs in the caller's `take`.
//
// AND THE ANSWER AND ITS INTERRUPTION ARE ONE FACT. An `act` that had been
// delivered while the answer was still unreadable was the whole of #956: the cut
// landed, the step unwound, the loop arrived at the boundary the cut had just
// bought — and `take`, which is non-blocking by construction, read the reading as
// still in flight and let that boundary pass. The turn then paid for a whole
// extra step, and where that step was its last, a drawing that said the work had
// independent parts in it was written down as a carry-on. So the answer is
// PUBLISHED FIRST and the interruption is delivered after it, and the two are
// ordered against the taker by one claim ([sidecar.spent]) rather than by which
// goroutine happens to be running.

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
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
	// spent GATES THE INTERRUPTION AND DOES NOT ORDER THE TAKE. A taker stores it
	// and takes; the interruption is delivered only if it claims it FIRST. So an
	// answer that has already reached a boundary raises no interruption, and a
	// taker never waits for one.
	//
	// An `act` exists to bring the boundary where the answer can be spent FORWARD.
	// An answer that has ALREADY REACHED a boundary has nothing left for one to
	// bring, and an interruption raised after that would land on whatever the turn
	// does next — which is the hazard the old act-before-settle order was reaching
	// for and paid for with #956's dropped drawing. Claiming it here costs one
	// atomic on each side and needs no lock, so `take` stays the closed-channel
	// test loop.go's law requires it to be.
	//
	// IT IS A GATE AND NOT A FENCE, and the difference is the width of one
	// instruction: between the settle closing and the claim being made, an
	// interruption can still win the claim while a taker at that same boundary
	// takes anyway. Nothing is lost when that happens — the answer is spent — but
	// a cut raised for an answer already spent is a cut nobody needs, so the door
	// that owes it drops it when the boundary it was owed for has arrived without
	// it (steer.go's [Agent.dropOwedCut]).
	spent atomic.Bool
}

// readBeside starts one reading beside the work.
//
// ctx is the WORK'S OWN context, so a reading cannot outlive the thing it was
// read for; the reading gets a child of it that [sidecar.end] closes.
//
// ask is the reading. It is given the sidecar's context and must respect it.
//
// act is OPTIONAL and is the reading's one power over the work: it is called on
// this goroutine the instant the answer lands and it is where an interruption is
// raised. It must be as cheap as a stream observer. Nil is the ordinary case —
// most readings simply wait to be taken.
//
// IT RUNS AFTER THE ANSWER IS TAKEABLE, and the order is load-bearing rather
// than incidental. What `act` does is bring the moment the answer can be spent
// FORWARD — cutting the request in flight so the next boundary arrives at once —
// and the taker that then arrives at that boundary is non-blocking by
// construction. So an answer published only once `act` had RETURNED was an
// answer the boundary its own cut opened could read as still in flight, which is
// #956: the cut was paid for and bought nothing. The settle therefore closes
// first, and [sidecar.spent] is what keeps the interruption from being delivered
// into a turn that has already spent the answer without it.
//
// AND A READING THAT HAS BEEN LET GO OF INTERRUPTS NOTHING. [sidecar.end] cancels
// this context, and a cancelled reading that answers anyway — because `ask` was
// already on its way back — must not reach into a turn that has stopped waiting
// for it: a cut raised there would land on whatever generation came NEXT.
func readBeside[T any](ctx context.Context, ask func(context.Context) T, act func(T)) *sidecar[T] {
	if ask == nil {
		return nil
	}
	readCtx, stop := context.WithCancel(ctx)
	side := &sidecar[T]{stop: stop, settled: make(chan struct{})}
	side.asked.Store(true)
	watch := besideWatchOn(ctx)
	watch.started()
	go func() {
		answer := ask(readCtx)
		side.answer = answer
		close(side.settled)
		if act != nil && readCtx.Err() == nil && side.spent.CompareAndSwap(false, true) {
			act(answer)
		}
		// AND THE WATCH HEARS IT LAST, once the answer can be taken AND the
		// interruption has been raised: whoever it lets go of finds the reading
		// settled rather than a moment from it.
		watch.landed()
	}()
	return side
}

// ── AND THE LIFETIME A READING GETS WHEN THE TURN IS NOT ONE ────────────────
//
// [readBeside] ties a reading to the WORK'S context, which is right for every
// reading that has work to run beside: a turn that ends has nothing left for its
// recall or its mark to be applied to, so cancelling them with it is the honest
// thing.
//
// THE END-OF-TURN READINGS ARE THE EXCEPTION AND THEY ARE WHY THIS EXISTS. The
// post-turn judge is asked whether the answer should have been WORK, and a yes
// starts a task — an effect that is about the turn and lands entirely outside it.
// Tied to the turn's context it could only be had by holding the turn open until
// it answered, which is the wait this whole file exists to remove. Tied to the
// SESSION it is what it always was: a reading nobody is waiting for, whose answer
// is spent when it arrives ([sidecar.spendWhenItLands]).
//
// THE BARGAIN IS THE [jobRegistry]'S, IN MINIATURE: the work is tracked, so a
// session that is closing can wait for it, and it is cancelled, so the wait is
// short. A session that has closed starts nothing, and that test is taken under
// this type's own lock so that "closed, therefore no new work" is one atomic
// fact rather than two.
//
// TWO OLDER SPELLINGS OF THIS SHAPE ARE STILL IN THE PACKAGE and are named here
// rather than left to be rediscovered: the namer's (session.go's `titleCtx`,
// `titleStop`, `titleJobs`) and the memory pass's (`memoryCtx`, `memoryStop`,
// `memoryJobs`). They are this type written out by hand, twice. They are not
// folded in here because their shutdowns genuinely differ — the namer is
// cancelled and then joined, the memory pass is waited for and THEN cancelled,
// because a pass two seconds from keeping something a person said is worth two
// seconds of a quit and a half-written name is not — and a fold that flattened
// that difference would be this type deciding something neither of them asked it
// to. Folding them wants that difference carried as a named property.
type afterTurn struct {
	mu      sync.Mutex
	ctx     context.Context
	stop    context.CancelFunc
	closed  bool
	running sync.WaitGroup
}

// newAfterTurn mints the lifetime. It is called once per session, at
// construction, because work bought by the FIRST turn has to have somewhere to
// be cancelled from.
func newAfterTurn() *afterTurn {
	ctx, stop := context.WithCancel(context.Background())
	return &afterTurn{ctx: ctx, stop: stop}
}

// begin registers one piece of work and answers the context it runs under. It
// answers false once the session is closing, which is the caller's whole
// instruction: do not start.
//
// THE CALLER OWNS THE `done` IT IS HANDED and must call it on every way out, or
// a closing session waits the whole grace for work that has already finished.
func (t *afterTurn) begin() (context.Context, func(), bool) {
	if t == nil {
		return nil, func() {}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, func() {}, false
	}
	t.running.Add(1)
	return t.ctx, t.running.Done, true
}

// settle ends the lifetime: nothing new starts, whatever is in flight is
// cancelled, and the caller waits a bounded moment for it to unwind.
//
// IT CANCELS BEFORE IT WAITS, which is the namer's order rather than the memory
// pass's, and for the namer's reason: nothing under here owes a store a write.
// What the judge owes is a decision, and a decision nobody is going to read is
// worth none of a person's quit.
func (t *afterTurn) settle(grace time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	already := t.closed
	t.closed = true
	t.mu.Unlock()
	if already {
		return
	}
	t.stop()
	settled := make(chan struct{})
	go func() {
		t.running.Wait()
		close(settled)
	}()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-settled:
	case <-timer.C:
	}
}

// ── AND HOW A LANDING IS SEEN WITHOUT RACING IT ─────────────────────────────
//
// A reading lands whenever the scheduler lets it, and nothing in this package
// may wait for one on the work's behalf; that is this file's law. A TEST that
// pins an order between a reading and the work still has to know when the
// reading has landed, and it cannot learn that from the calls it answered. An
// answered call is a reading that has still to parse, act and settle, and a
// reading the work started a moment ago may not have been scheduled at all: a
// scripted conversation answers in no time and never blocks, so the goroutine
// the turn has just started waits behind it for as many rounds as the machine
// is busy. Every fixture that guessed (a count of the calls entered, a script
// long enough on a quiet machine) was green alone and red beside another suite.
//
// SO THE DOOR SAYS. A watch carried on the work's context is told by
// [readBeside] itself when each reading under it starts and when it has landed,
// and [besideWatch.quiet] waits until none is in flight. It is [desk.settled]'s
// bargain for the other half of this file: THE RUNNING PRODUCT CARRIES NO WATCH
// AND NEVER WAITS ON ONE (sidecar_law_test.go holds it to that), and a context
// without a watch costs a reading one lookup.

// besideWatch counts the readings in flight under one context.
type besideWatch struct {
	mu     sync.Mutex
	flying int
	// calm is made on demand by [besideWatch.quiet] and closed when the last
	// reading in flight lands, so a watch nobody waits on allocates nothing.
	calm chan struct{}
}

type besideWatchKey struct{}

// withBesideWatch carries a watch on ctx. Every reading started under it is
// counted, which is the turn's own and anything the turn's context reaches.
func withBesideWatch(ctx context.Context, watch *besideWatch) context.Context {
	return context.WithValue(ctx, besideWatchKey{}, watch)
}

// besideWatchOn is the watch ctx carries, or nil, which every method answers.
func besideWatchOn(ctx context.Context) *besideWatch {
	watch, _ := ctx.Value(besideWatchKey{}).(*besideWatch)
	return watch
}

func (w *besideWatch) started() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.flying++
	w.mu.Unlock()
}

func (w *besideWatch) landed() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.flying--
	if w.flying == 0 && w.calm != nil {
		close(w.calm)
		w.calm = nil
	}
	w.mu.Unlock()
}

// quiet waits until no reading under this watch is in flight, or until the work
// those readings belong to is over.
//
// IT IS BOUNDED BY THE FACT AND NOT BY A CLOCK. [sidecar.end] cancels a
// reading's window but never joins it, so a reading whose own call ignores that
// cancellation is counted until the call returns — and an unbounded wait here
// would park the next caller forever and report as a package timeout naming
// whichever test happened to be running. The context these readings were
// started under is the fact that says the work is over, so it is what ends the
// wait when no landing does.
func (w *besideWatch) quiet(ctx context.Context) {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.flying == 0 {
		w.mu.Unlock()
		return
	}
	if w.calm == nil {
		w.calm = make(chan struct{})
	}
	wait := w.calm
	w.mu.Unlock()
	select {
	case <-wait:
	case <-ctx.Done():
	}
}

// take answers the reading IF IT HAS ALREADY LANDED, and never waits.
//
// A SETTLED SIDECAR ANSWERS ONCE. Whatever the caller does with the answer, the
// reading has said its piece — including when the answer was "nothing", which
// cannot become something later. A nil sidecar is a reading that was never
// started, which is the ordinary case for every gated turn and is what lets a
// caller hold this in one line with no branch around it.
//
// AND A LANDED READING IS TAKEABLE WHATEVER ITS OWN INTERRUPTION IS DOING, which
// is the whole of the boundary contract: this is the moment the `act` exists to
// buy, so it must never be the moment that reads the reading as still in flight.
func (s *sidecar[T]) take() (T, bool) {
	var none T
	if s == nil || s.taken {
		return none, false
	}
	select {
	case <-s.settled:
		s.taken = true
		s.claim()
		s.stop()
		return s.answer, true
	default:
		return none, false
	}
}

// claim records that this answer has reached a boundary, so that a reading whose
// `act` has not been delivered yet does not deliver it — see [sidecar.spent].
func (s *sidecar[T]) claim() { s.spent.Store(true) }

// settle WAITS for the reading and then answers it, once.
//
// IT IS THE EXCEPTION AND NOT THE RULE — see this file's header. Use it only
// where there is genuinely nothing left to run beside, and where the reading is
// bounded by a window of its own; the work's context bounds it too, so a stopped
// turn does not wait here.
//
// THE NAME IS THE LAW. It may be called where the turn is ENDING and nowhere
// else, and it is spelled that way because a comment saying so is a comment
// (sidecar_law_test.go is the law).
func (s *sidecar[T]) takeAtTheEnd() (T, bool) {
	var none T
	if s == nil || s.taken {
		return none, false
	}
	<-s.settled
	s.taken = true
	s.claim()
	s.stop()
	return s.answer, true
}

// spendWhenItLands hands the answer to `spend` AT THE MOMENT IT LANDS, and the
// caller does not wait for it.
//
// IT IS THE FOURTH VERB AND IT IS WHAT THE END OF A TURN ACTUALLY NEEDS.
// [sidecar.takeAtTheEnd] above is a WAIT, and the only thing that ever made it
// honest was that the caller had already been told the turn was over — which is
// true of the caller and NOT of the person, who is sitting in front of a
// finished answer while a judge decides something about it. A turn that waited
// there kept the seal, the transcript, the next Submit and the follow-up drain
// all parked behind a reading nobody asked for: measured at up to half a minute
// after the last word was written, under a phase line reading "whether that
// should be work".
//
// So the turn says WHEN the answer may be spent, and stops saying WHETHER it has
// arrived. `spend` runs on the reading's own goroutine the instant it lands, and
// on the CALLER'S goroutine when it has landed already — which is the ordinary
// fast case and costs exactly what taking it did.
//
// THE CALLER MUST HAVE DECIDED THE ANSWER IS SPENDABLE, exactly as [sidecar.take]
// requires: this is TAKE with the waiting removed, not a licence to act earlier.
// Everything [sidecar.take] does for the one-answer rule is done here — the
// sidecar is marked taken at once, so a second caller gets nothing, and the
// interruption is claimed so a cut nobody needs is not raised.
//
// AND THE READING MUST OUTLIVE THE TURN FOR THIS TO MEAN ANYTHING. A sidecar
// started on the turn's own context is cancelled the moment the turn returns, so
// arming this on one would be a promise to spend an answer that is about to be
// cancelled. A caller that uses this verb starts its reading on the session's own
// lifetime instead ([Agent.afterTurn]).
// It reports whether it TOOK the reading, which is the one-answer rule read from
// the caller's side: a sidecar that was already spent hands `spend` nothing, and
// a caller holding something on the reading's behalf — a place on a lifetime, a
// name being made — has to be told so it can let go of it.
func (s *sidecar[T]) spendWhenItLands(spend func(T)) bool {
	if s == nil || s.taken || spend == nil {
		return false
	}
	s.taken = true
	select {
	case <-s.settled:
		// It is already here, so this is [sidecar.take] with the select already
		// answered, and it runs on the caller's goroutine exactly as that did.
		s.claim()
		s.stop()
		spend(s.answer)
	default:
		// AND THE HANDOFF IS ITS OWN GOROUTINE because the whole point is that
		// this caller walks away. It is bounded by the reading's own windows and
		// by the lifetime the reading was started on, the same two bounds
		// [sidecar.takeAtTheEnd] was bounded by — the difference is only who is
		// standing in front of them.
		guard.Go("reading spent after the turn", func() {
			<-s.settled
			s.claim()
			s.stop()
			spend(s.answer)
		})
	}
	return true
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

// ── AND THE OTHER HALF: TELLING SOMEBODY WITHOUT WAITING FOR THEM ───────────
//
// [readBeside] keeps the WORK from waiting on a reading. A desk keeps the work
// from waiting on a LISTENER, which is the same law read from the other end and
// was a measured hole in it: every phase this package posts — and every phase
// the transport posts, which arrives by this same door (phasenews.go's
// [forwardPhase]) — was handed to the surface's reader ON THE CALLING
// GOROUTINE, and internal/tui3's reader asks Bubble Tea for a frame through an
// UNBUFFERED channel. So the last act of every model call, `phase.done()`, paid
// a whole Update-and-View cycle before the engine goroutine got its own call
// back. On every call, not only a cancelled one. `postPhaseNews` and
// `postLaneNews` both said in their own doc comments that they never block on a
// slow reader, and both of them did.
//
// THE LAW IS: THE PRODUCER LEAVES THE NEWS AND WALKS AWAY. internal/provider's
// phase.go already names the division — "the one live reader hands the news to
// a desk and asks for a frame" — and this is the desk. A listener is told on the
// desk's own goroutine, IN THE ORDER IT WAS TOLD, and at most one such goroutine
// is alive per desk at a time: it is started by the telling that finds the desk
// empty and it returns when the desk is empty again.
//
// NOTHING IS COALESCED AND NOTHING IS DROPPED. A phase is a state and a
// latest-wins slot would serve it, but lane news is a LEDGER and dropping one of
// those loses a request nobody can count again — so the queue is honest and the
// bound on it is the listener's own appetite. A listener that never returns
// holds one goroutine and a growing slice, which is a listener that is broken in
// a way this package cannot fix and must not hide.

// desk is somewhere to leave news for a listener who may be slow.
//
// The zero value is a working desk, which is what lets the two news doors each
// declare one as a package variable and never build it.
type desk struct {
	mu      sync.Mutex
	queue   []func()
	handing bool
	// idle is made on demand by [desk.settled] and closed when the queue runs
	// out, so a desk nobody is waiting on allocates nothing.
	idle chan struct{}
}

// tell leaves one telling on the desk and returns at once.
func (d *desk) tell(hand func()) {
	if d == nil || hand == nil {
		return
	}
	d.mu.Lock()
	d.queue = append(d.queue, hand)
	start := !d.handing
	d.handing = true
	d.mu.Unlock()
	if start {
		guard.Go("news desk", d.hand)
	}
}

// hand works through the desk until there is nothing on it. It is the whole of
// the desk's goroutine, and the empty desk is the only way out, so the flag it
// clears there is what makes the next [desk.tell] start a fresh one.
func (d *desk) hand() {
	for {
		d.mu.Lock()
		if len(d.queue) == 0 {
			d.handing = false
			if d.idle != nil {
				close(d.idle)
				d.idle = nil
			}
			d.mu.Unlock()
			return
		}
		next := d.queue[0]
		d.queue = d.queue[1:]
		d.mu.Unlock()
		next()
	}
}

// settled waits until everything the desk has been told has been handed on. A
// listener's log is the only thing that can see the difference between a desk
// and a straight call, so this is how a test reads one without racing it; the
// running product never waits here, which is the entire point of the desk.
func (d *desk) settled() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if !d.handing {
		d.mu.Unlock()
		return
	}
	if d.idle == nil {
		d.idle = make(chan struct{})
	}
	wait := d.idle
	d.mu.Unlock()
	<-wait
}
