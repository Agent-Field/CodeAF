package serve

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// THE STALL WATCHDOG, AS A PASS.
//
// Every settle that sweeps the floor runs [Handler.watchJobs], and the job
// timer wakes it for each wait's deadline and each stall's next look
// ([jobTimerDue], [factory.WatchAt]), so a job that stops moving is seen
// without a request arriving. The rules are internal/factory's watchdog.go;
// this file keeps the record and gives the turns:
//
//   - a wait [factory.StallOf] reports goes on the job as its Stall, and is
//     left alone for [factory.WatchConfirmFor], because a laptop that just
//     woke reads late on every clock;
//   - a stall still standing then is handed to the job's manager, one turn
//     through [Config.Stalled], off the pass, and again after each back-off;
//   - a turn that fails, or a stall still there at the next look, sets GaveUp,
//     and the job reads "you: decide" with the stall's words. So does a stall
//     on a factory with no manager, or on a job whose budget is spent;
//   - a stall whose cause is gone ([factory.StallStands]) is taken off.
//
// THE WATCHDOG WRITES THE JOB'S STALL AND ITS LOG, AND NOTHING ELSE, but for
// one write that changes nothing: a job whose stored wait is missing or wrong
// is written again so the store sets it. It never pushes, comments, merges or
// approves; whatever moves the job is done by the manager through its own
// doors, or by the person.

// watchTurnFor bounds one manager turn on a stall, and is how long a turn the
// service lost (a restart in the middle of it) is waited for before the stall
// is looked at again.
const watchTurnFor = 10 * time.Minute

// errWatchMoved ends a watchdog write whose job changed since it was listed,
// before anything is written.
var errWatchMoved = errors.New("the job moved before the watchdog wrote")

// stallWatch is the watchdog's state on one handler: the manager turns in
// flight, at most one for a job, and the context that ends them when the
// service closes.
type stallWatch struct {
	mu     sync.Mutex
	turns  map[int]bool
	ctx    context.Context
	cancel context.CancelFunc
	// facts is the last pass's facts, for the job timer ([Handler.lastFacts]).
	facts *watchFacts
}

// busy says a manager turn on the job's stall is in flight.
func (w *stallWatch) busy(id int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.turns[id]
}

// stop ends every turn in flight. Close calls it before it waits for them.
func (w *stallWatch) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
	}
}

// beginStallTurn takes the job's one turn, and answers the context it runs
// under. It refuses while a turn on the job is in flight and once the service
// is closing, so no turn starts that Close would not wait for.
func (h *Handler) beginStallTurn(id int) (context.Context, bool) {
	h.jobsMu.Lock()
	if h.jobsClosed {
		h.jobsMu.Unlock()
		return nil, false
	}
	h.jobsBusy.Add(1)
	h.jobsMu.Unlock()
	w := &h.stalls
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.turns[id] {
		h.jobsBusy.Done()
		return nil, false
	}
	if w.ctx == nil {
		w.ctx, w.cancel = context.WithCancel(context.Background())
	}
	if w.turns == nil {
		w.turns = map[int]bool{}
	}
	w.turns[id] = true
	return w.ctx, true
}

func (h *Handler) endStallTurn(id int) {
	h.stalls.mu.Lock()
	delete(h.stalls.turns, id)
	h.stalls.mu.Unlock()
	h.jobsBusy.Done()
}

// watchJobs is the watchdog's pass over one listing of the floor. Like every
// scan of a settle it only picks candidates from the listing, and each write
// checks the job again under its lock. It answers whether it may have written
// a job, so the caller knows its listing is old.
func (h *Handler) watchJobs(items []factory.Item, facts *watchFacts, now time.Time) (wrote bool) {
	for _, it := range items {
		if !factory.Watched(it) {
			continue
		}
		if it.Stall != nil {
			if h.watchStall(it, facts.of(it), now) {
				wrote = true
			}
			continue
		}
		// A stop the main agent holds is given its one turn, or handed back
		// (handoff.go). It is never a stall.
		if _, held := factory.HandedOver(it); held && it.Wait != nil && it.Wait.Who == factory.WaitMainAgent {
			if h.watchHandoff(it, now) {
				wrote = true
			}
			continue
		}
		stall := factory.StallOf(it, facts.of(it), now)
		if stall != nil && stall.Kind == factory.StallWait {
			// A WAIT STORED WRONG IS PUT RIGHT BY WRITING THE JOB: the store sets
			// the wait on every write. A job saved before waits were stored has
			// none until its first write, and this is that write. Only a fault
			// that survives it is a stall.
			wrote = true
			cur, ok := h.watchRewrite(it.ID)
			if !ok || !factory.Watched(cur) || cur.Stall != nil {
				continue
			}
			it = cur
			stall = factory.StallOf(it, facts.of(it), now)
		}
		if stall == nil {
			continue
		}
		wrote = true
		h.watchSight(it, *stall, facts.of(it), now)
	}
	return wrote
}

// watchFacts is what the service knows about its jobs beside their documents
// at one look: which a lease holds, when GitHub was last read well, what the
// computers here have free. What costs a read is read once, when a job first
// needs it, and safely from more than one goroutine: the job timer reads the
// last pass's facts while the next pass makes its own.
type watchFacts struct {
	h *Handler
	// leases says this service keeps leases at all; one that keeps none
	// cannot tell whether a step is held.
	leases  bool
	held    []lease.Lease
	working map[int]bool

	readOnce sync.Once
	read     time.Time
	capOnce  sync.Once
	capacity []machineCapacity
	// auto and busy keep each job's answer for the length of the look: a pass
	// asks for a job's facts more than once, and each answer reads the store.
	mu   sync.Mutex
	auto map[int]bool
	busy map[int]bool
}

// once answers fn for job id, asked at most once in this look.
func (f *watchFacts) once(kept *map[int]bool, id int, fn func() bool) bool {
	f.mu.Lock()
	got, ok := (*kept)[id]
	f.mu.Unlock()
	if ok {
		return got
	}
	got = fn()
	f.mu.Lock()
	if *kept == nil {
		*kept = map[int]bool{}
	}
	(*kept)[id] = got
	f.mu.Unlock()
	return got
}

func (h *Handler) watchFacts(held []lease.Lease) *watchFacts {
	f := &watchFacts{h: h, held: held, leases: h.workers != nil && h.workers.desk != nil, working: make(map[int]bool, len(held))}
	// A hold lent to the manager for a reply runs no step.
	for _, row := range held {
		if !row.ThreadOnly {
			f.working[row.Job] = true
		}
	}
	return f
}

// of answers the facts for one job. The ones that cost a read are answered
// only for a job they bear on.
func (f *watchFacts) of(it factory.Item) factory.WatchFacts {
	out := factory.WatchFacts{Held: !f.leases || f.working[it.ID]}
	w := it.Wait
	if (w != nil && w.Who == factory.WaitGitHub) || (it.Stall != nil && it.Stall.Kind == factory.StallGitHub) {
		f.readOnce.Do(func() {
			if meta, err := f.h.config.Store.SourceMeta(factorygithub.Name); err == nil {
				f.read = meta.Polled
			}
		})
		out.GitHubRead = f.read
	}
	if w != nil && w.Who == factory.WaitYou && factory.Automatic(it) {
		out.AutoPresses = f.once(&f.auto, it.ID, func() bool { return f.h.autoPresses(it) })
	}
	// A job in the queue that nothing holds may be waiting its turn.
	if it.State == factory.StateQueued && !out.Held {
		out.Busy = f.once(&f.busy, it.ID, func() bool {
			f.capOnce.Do(func() { f.capacity = f.h.executionCapacity(f.held) })
			return queuedBehindWork(it, f.capacity)
		})
	}
	return out
}

// queuedBehindWork says the job waits in the queue for a slot: its person's
// computers are here and all of their slots are in use. It is the rule the
// job page says "Waiting for capacity" by ([presentQueuedCapacity]), asked of
// the job as a queued one whatever it is queued for (a step, a publish).
func queuedBehindWork(it factory.Item, machines []machineCapacity) bool {
	job := api.FromItem(it)
	job.State = api.StateQueued
	presentQueuedCapacity(&job, machines)
	return job.StatePresentation.Kind == "waiting_for_capacity"
}

// autoPresses says Auto would give the approval the job waits on now, by the
// rule the settle pass presses by ([Handler.prepareAutomaticApprovals]).
func (h *Handler) autoPresses(it factory.Item) bool {
	if !h.automaticApprovalAllowed(it) {
		return false
	}
	a := factory.JobApproval(it)
	return a != nil && a.State == "waiting"
}

// watchRewrite writes the job with nothing changed, so the store sets its
// wait, offers and clocks again, and answers the job as saved.
func (h *Handler) watchRewrite(id int) (factory.Item, bool) {
	if err := h.config.Store.Annotate(id, func(*factory.Item) error { return nil }); err != nil {
		return factory.Item{}, false
	}
	cur, err := h.config.Store.Get(id)
	return cur, err == nil
}

// watchSight records a stall seen for the first time. Nothing is said yet:
// the manager is asked once it has stood for the confirm window.
func (h *Handler) watchSight(it factory.Item, stall factory.Stall, facts factory.WatchFacts, now time.Time) {
	stall.At, stall.NextAt = now, now.Add(factory.WatchConfirmFor)
	err := h.config.Store.Annotate(it.ID, func(cur *factory.Item) error {
		if cur.Stall != nil || !factory.Watched(*cur) || factory.StallWaitKey(*cur) != stall.Wait {
			return errWatchMoved
		}
		cp := stall
		cur.Stall = &cp
		return nil
	})
	if err != nil || now.Before(stall.NextAt) {
		return
	}
	if cur, err := h.config.Store.Get(it.ID); err == nil && cur.Stall != nil {
		h.watchStall(cur, facts, now)
	}
}

// watchStall looks at a job with a stall on record: it takes the stall off
// when its cause is gone, and when a look is due it gives the manager a turn
// or hands the stall to the person. It answers whether it had anything to do.
func (h *Handler) watchStall(it factory.Item, facts factory.WatchFacts, now time.Time) bool {
	st := *it.Stall
	if !factory.StallStands(it, facts) {
		h.watchClear(it, st, now)
		return true
	}
	if st.NextAt.IsZero() || now.Before(st.NextAt) || h.stalls.busy(it.ID) {
		return false
	}
	switch {
	case !st.TurnsLeft():
		h.watchGiveUp(it, st, "", now)
	case h.config.Stalled == nil:
		h.watchGiveUp(it, st, factory.StallNoManager, now)
	case factory.StallBudgetSpent(it):
		// A TURN IS CHARGED TO THE JOB, so a job with nothing left gets none.
		h.watchGiveUp(it, st, factory.StallNoBudget, now)
	default:
		h.watchTurn(it, st, now)
	}
	return true
}

// sameStall says cur still carries the stall st was read from, at the same
// count of turns.
func sameStall(cur *factory.Item, st factory.Stall) bool {
	return cur.Stall != nil && cur.Stall.At.Equal(st.At) && cur.Stall.Turns == st.Turns
}

// writeStall is a watchdog write. One that changes what the job waits on
// (a stall handed to the person, or one taken back from them) is a change to
// the job; the rest is the watchdog's own bookkeeping and leaves Changed as
// it was, as a read the factory made by itself does.
func (h *Handler) writeStall(id int, moves bool, change func(*factory.Item) error) error {
	if moves {
		return h.config.Store.Update(id, change)
	}
	return h.config.Store.Annotate(id, change)
}

// watchLog adds one line to the job's log, when it has a run to log on.
func watchLog(it *factory.Item, now time.Time, tone, text string) {
	if it.Stream == nil {
		return
	}
	it.Stream.Log = append(it.Stream.Log, factory.LogLine{At: now, Tone: tone, Text: text})
}

// The lines the watchdog writes on a job's log.
const (
	watchLogAsked    = "Stalled: %s Asked the manager to look."
	watchLogAgain    = "Still stalled: %s Asked the manager again (turn %d of %d)."
	watchLogGaveUp   = "Stalled: %s It now waits on its person to decide."
	watchLogSaid     = "Manager on the stall: %s"
	watchLogSilent   = "The manager's turn on the stall ended without a word."
	watchLogFailed   = "The manager's turn on the stall failed: %s"
	watchLogCleared  = "No longer stalled: %s"
	watchLogMovedOn  = "The job moved on from its stall."
	watchLogDecision = " It now waits on its person to decide."
)

// watchGone says why a stall of each kind no longer stands.
var watchGone = map[string]string{
	factory.StallUnheld:  "a computer is running it again.",
	factory.StallGitHub:  "GitHub is being read again.",
	factory.StallAuto:    "Auto no longer gives this approval by itself.",
	factory.StallNoOffer: "it offers a way forward again.",
	factory.StallWait:    "its wait is stored again.",
}

// watchClear takes a stall off a job whose cause is gone. A stall nobody was
// told of goes without a word.
func (h *Handler) watchClear(it factory.Item, st factory.Stall, now time.Time) {
	_ = h.writeStall(it.ID, st.GaveUp, func(cur *factory.Item) error {
		if !sameStall(cur, st) {
			return errWatchMoved
		}
		cur.Stall = nil
		if st.Turns > 0 || st.GaveUp {
			watchLog(cur, now, "ok", fmt.Sprintf(watchLogCleared, watchGone[st.Kind]))
		}
		return nil
	})
}

// watchGiveUp hands a stall to the person with no turn (why says which of the
// reasons), or closes one whose turns are used up. Either way the watchdog
// does not look at it again.
func (h *Handler) watchGiveUp(it factory.Item, st factory.Stall, why string, now time.Time) {
	_ = h.writeStall(it.ID, !st.GaveUp, func(cur *factory.Item) error {
		if !sameStall(cur, st) {
			return errWatchMoved
		}
		next := cur.Stall.GivenUp(firstWords(why, factory.StallUnmoved))
		next.NextAt = time.Time{}
		cur.Stall = &next
		if !st.GaveUp {
			watchLog(cur, now, "fail", fmt.Sprintf(watchLogGaveUp, next.Words))
		}
		return nil
	})
}

func firstWords(words ...string) string {
	for _, w := range words {
		if strings.TrimSpace(w) != "" {
			return w
		}
	}
	return ""
}

// watchTurn gives the job's manager one turn on the stall, off the pass. A
// stall that already had a turn and is still here is handed to the person in
// the same write, and the manager is asked again all the same: its later turns
// may still move the job, and the stall goes when they do.
func (h *Handler) watchTurn(it factory.Item, st factory.Stall, now time.Time) {
	ctx, ok := h.beginStallTurn(it.ID)
	if !ok {
		return
	}
	started := false
	defer func() {
		if !started {
			h.endStallTurn(it.ID)
		}
	}()
	handing := st.Turns > 0 && !st.GaveUp
	err := h.writeStall(it.ID, handing, func(cur *factory.Item) error {
		if !sameStall(cur, st) {
			return errWatchMoved
		}
		next := *cur.Stall
		if handing {
			next = next.GivenUp(factory.StallUnmoved)
		}
		// THE NEXT LOOK IS PUT PAST THE TURN, so the timer does not come round
		// for this job while the manager is still on it, and a turn lost to a
		// restart is given again.
		next.NextAt = now.Add(watchTurnFor)
		cur.Stall = &next
		switch {
		case st.Turns == 0:
			watchLog(cur, now, "ask", fmt.Sprintf(watchLogAsked, next.Words))
		default:
			words := next.Words
			if handing {
				words += watchLogDecision
			}
			watchLog(cur, now, "ask", fmt.Sprintf(watchLogAgain, words, st.Turns+1, len(factory.StallBackoff)+1))
		}
		return nil
	})
	if err != nil {
		return
	}
	cur, err := h.config.Store.Get(it.ID)
	if err != nil || cur.Stall == nil {
		return
	}
	started = true
	guard.Go("factory/stall-turn", func() {
		defer h.endStallTurn(cur.ID)
		h.runStallTurn(ctx, cur, *cur.Stall)
	})
}

// runStallTurn is the turn itself and the write after it: the turn counted,
// the next look set by the back-off, and what the manager said on the job's
// log. A job that moved while the manager was on it has no stall left to
// count on, and only the log line is written.
func (h *Handler) runStallTurn(ctx context.Context, it factory.Item, st factory.Stall) {
	tctx, cancel := context.WithTimeout(ctx, watchTurnFor)
	said, err := h.config.Stalled(tctx, it, st)
	cancel()
	if ctx.Err() != nil {
		// The service is closing. The stall keeps its count, and the next
		// service to start gives the turn again.
		return
	}
	now := h.clock()
	tone, line := "said", watchLogSilent
	switch {
	case err != nil:
		tone, line = "fail", fmt.Sprintf(watchLogFailed, oneLine(err.Error()))
	case strings.TrimSpace(said) != "":
		line = fmt.Sprintf(watchLogSaid, oneLine(said))
	}
	_ = h.writeStall(it.ID, err != nil && !st.GaveUp, func(cur *factory.Item) error {
		if cur.Stall == nil || !cur.Stall.At.Equal(st.At) {
			if cur.Stream == nil {
				return errWatchMoved
			}
			watchLog(cur, now, tone, line)
			watchLog(cur, now, "ok", watchLogMovedOn)
			return nil
		}
		next := cur.Stall.Turned(now, err != nil)
		cur.Stall = &next
		watchLog(cur, now, tone, line)
		if err != nil && !st.GaveUp {
			watchLog(cur, now, "fail", fmt.Sprintf(watchLogGaveUp, next.Words))
		}
		return nil
	})
	h.scheduleJobTimer()
}

// oneLine is s on one line, cut to what a log line carries.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const most = 400
	if r := []rune(s); len(r) > most {
		s = string(r[:most]) + "…"
	}
	return s
}

// watchDue is the soonest moment the watchdog has a job among items to look
// at on these facts ([factory.WatchAt]), zero for none. It may be past: the
// floor is then not remembered as idle.
func watchDue(items []factory.Item, facts func(factory.Item) factory.WatchFacts, now time.Time) time.Time {
	var next time.Time
	for _, it := range items {
		if at := factory.WatchAt(it, facts(it), now); !at.IsZero() && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	return next
}

// keepFacts remembers the facts of the last pass for the job timer, which is
// set from doors that hold no leases to read.
func (h *Handler) keepFacts(facts *watchFacts) {
	h.stalls.mu.Lock()
	h.stalls.facts = facts
	h.stalls.mu.Unlock()
}

// lastFacts is the facts of the last pass, or none yet: a service that has
// not looked reads every job as unheld, and its first pass sets the timer
// again on what it finds.
func (h *Handler) lastFacts() *watchFacts {
	h.stalls.mu.Lock()
	facts := h.stalls.facts
	h.stalls.mu.Unlock()
	if facts == nil {
		facts = h.watchFacts(nil)
	}
	return facts
}
