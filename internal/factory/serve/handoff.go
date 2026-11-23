package serve

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// EVERY STOP THE MAIN AGENT MAY DECIDE GOES TO THE MAIN AGENT FIRST, AS A PASS.
//
// The rules are internal/factory's handoff.go: the store hands a new stop to
// the job's main agent in the write that makes it, and the job reads
// "main_agent" with a deadline. This file gives the turn and ends the
// handoff. It runs in the watchdog's pass ([Handler.watchJobs]) and shares
// its turns, so a job has at most one main-agent turn in flight whatever it
// is for:
//
//   - a handoff that has had no turn gets ONE, at once, through
//     [Config.Stopped], off the pass. There is no confirm window: that is for
//     a stall, which may be a laptop waking, and a stop is a fact;
//   - when the turn ends and the job still reads the same stop, the handoff
//     is over: the job reads "you: <action>" and its log says the main agent
//     could not settle it, with its reason;
//   - a turn that fails ends it the same way at once, and a factory with no
//     main agent to ask ([Config.Stopped] nil, or [ErrNotAsked]) ends it with
//     no turn and no line;
//   - a handoff past its deadline, or one whose turn was lost to a restart,
//     is ended by the pass. It never goes through the stall's back-off. A
//     turn given late (the service was down when the stop came) still has
//     [factory.HandoffTurnFor] to end in ([factory.HandoffDue]).
//
// THE PASS DECIDES NOTHING ABOUT THE JOB. Whatever moves it is the main
// agent's own press of the manager_decide door (manager_decide.go), under the
// rules of [factory.ManagerDecides], or the person's press of their card,
// which stays open the whole time.

// ErrNotAsked is what a host's [Config.Stopped] answers when it has nobody to
// ask and nothing went wrong: its runner is off, so it spends nothing by
// itself. The stop goes to the person with no line on the job's log.
var ErrNotAsked = errors.New("the main agent was not asked")

// watchHandoff looks at a job whose main agent holds its stop: it gives the
// one turn, or ends a handoff nobody can settle any more. It answers whether
// it wrote the job.
func (h *Handler) watchHandoff(it factory.Item, now time.Time) bool {
	hd := *it.Handoff
	busy := h.stalls.busy(it.ID)
	due := factory.HandoffDue(it)
	late := !due.IsZero() && !now.Before(due)
	switch {
	case hd.Turns == 0 && h.config.Stopped == nil:
		return h.endHandoff(it.ID, hd, factory.HandoffNotAsked, false, now)
	case hd.Turns == 0 && !busy:
		return h.handTurn(it, hd, now)
	case late:
		// THE TURN OUTLIVED THE DEADLINE. The stop is the person's now; what
		// the main agent decides after this is still taken, by the same rules.
		return h.endHandoff(it.ID, hd, factory.HandoffLate, true, now)
	case hd.Turns > 0 && !busy:
		// A TURN WITH NO RESULT AND NOBODY RUNNING IT was lost: the service
		// restarted in the middle of it.
		return h.endHandoff(it.ID, hd, factory.HandoffLate, true, now)
	}
	return false
}

// sameHandoff says cur still carries the handoff hd was read from, not yet
// handed back.
func sameHandoff(cur *factory.Item, hd factory.Handoff) bool {
	c := cur.Handoff
	return c != nil && !c.Over && c.Wait == hd.Wait && c.At.Equal(hd.At)
}

// endHandoff hands the stop back to the person with why, and says so on the
// job's log when said. A job that moved, or whose stop someone already handed
// back, is left as it is.
func (h *Handler) endHandoff(id int, hd factory.Handoff, why string, said bool, now time.Time) bool {
	err := h.config.Store.Update(id, func(cur *factory.Item) error {
		if !sameHandoff(cur, hd) {
			return errWatchMoved
		}
		next := cur.Handoff.Ended(why)
		cur.Handoff = &next
		if said {
			watchLog(cur, now, "said", factory.HandoffLine(why))
		}
		return nil
	})
	return err == nil
}

// handTurn gives the job's main agent its one turn on the stop, off the pass.
// The turn is counted before it starts, so a service that restarts in the
// middle of it never gives a second.
func (h *Handler) handTurn(it factory.Item, hd factory.Handoff, now time.Time) bool {
	under, held := factory.HandedOver(it)
	if !held {
		return false
	}
	ctx, ok := h.beginStallTurn(it.ID)
	if !ok {
		return false
	}
	started := false
	defer func() {
		if !started {
			h.endStallTurn(it.ID)
		}
	}()
	err := h.config.Store.Annotate(it.ID, func(cur *factory.Item) error {
		if !sameHandoff(cur, hd) || cur.Handoff.Turns != hd.Turns {
			return errWatchMoved
		}
		next := cur.Handoff.Turned(now)
		cur.Handoff = &next
		return nil
	})
	if err != nil {
		return false
	}
	cur, err := h.config.Store.Get(it.ID)
	if err != nil || cur.Handoff == nil {
		return true
	}
	// THE TURN ENDS WHEN THE HANDOFF IS DUE BACK WITH THE PERSON, whatever the
	// host allows it.
	wait := factory.HandoffTurnFor
	if due := factory.HandoffDue(cur); !due.IsZero() {
		wait = due.Sub(now)
	}
	started = true
	guard.Go("factory/stop-turn", func() {
		defer h.endStallTurn(cur.ID)
		h.runStopTurn(ctx, cur, *cur.Handoff, under, max(wait, time.Second))
	})
	return true
}

// runStopTurn is the turn itself and the write after it. A job that moved
// while the main agent was on it (its own decision, or its person's press)
// has no handoff left to end, and nothing is written.
func (h *Handler) runStopTurn(ctx context.Context, it factory.Item, hd factory.Handoff, under factory.Wait, wait time.Duration) {
	tctx, cancel := context.WithTimeout(ctx, wait)
	said, err := h.config.Stopped(tctx, it, under)
	ran := tctx.Err() != nil
	cancel()
	if ctx.Err() != nil {
		// The service is closing. The handoff keeps its turn, and the next
		// service to start hands the stop to the person.
		return
	}
	why, line := firstWords(oneLine(said)), true
	switch {
	case errors.Is(err, ErrNotAsked):
		why, line = factory.HandoffNotAsked, false
	case err != nil && (ran || errors.Is(err, context.DeadlineExceeded)):
		why = factory.HandoffLate
	case err != nil:
		why = factory.HandoffFailed
	}
	h.endHandoff(it.ID, hd, why, line, h.clock())
	h.scheduleJobTimer()
}

// Wake has the service look at its jobs now. A host calls it when a write it
// saw land hands a stop to a main agent ([factory.HandedOver]): no request
// brought that write, so without it the turn would wait for the next request
// or the job timer.
func (h *Handler) Wake() {
	h.jobsMu.Lock()
	if h.jobsClosed {
		h.jobsMu.Unlock()
		return
	}
	if h.jobsTimer != nil {
		h.jobsTimer.Stop()
	}
	h.jobsTimer = time.AfterFunc(time.Millisecond, h.runJobTimer)
	h.jobsMu.Unlock()
}
