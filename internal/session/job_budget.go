package session

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// jobBudgetLiveEvery is how often a manager with work in flight looks at its
// job's budget and the person's Stop; jobBudgetIdleEvery is how often one with
// nothing in flight does ([Agent.watchJobBudget]). The idle look keeps the
// presence file's maintenance beat, since it keeps nothing but the manager's
// stopped state honest and the beat is the session's own idea of "soon".
const (
	jobBudgetLiveEvery = 100 * time.Millisecond
	jobBudgetIdleEvery = presenceHeartbeat
)

// JobBudgetUnreadable refuses new paid calls without cancelling existing work.
const JobBudgetUnreadable = "job budget could not be read; new work held"

// jobBudgetOf is shared by the whole owned family. Children charge their own
// provider receipts immediately; folding their usage home must not charge again.
func (a *Agent) jobBudgetOf() JobChat {
	if a.config.JobBudget != nil {
		return a.config.JobBudget
	}
	return a.jobChatOf()
}

func (a *Agent) jobBudgetBlock() error {
	if budget := a.jobBudgetOf(); budget != nil {
		if why := budget.Refusal(); why != "" {
			return spendRailReached{said: why}
		}
	}
	return nil
}

// Idle managers can still own detached commands or delegated work. Keep watching
// until the agent closes, not just until its foreground turn ends. No model call
// is involved. StopWork closes admission before cancelling the complete family.
//
// A MANAGER THAT OWNS NOTHING IN FLIGHT LOOKS ONCE A BEAT, NOT TEN TIMES A SECOND.
// Each look is a stat of the job's document, and a person's-Stop look can read
// the whole floor; ten a second for every open manager kept the fifth live
// run's idle engine at 21% of a core with 82 jobs. With no turn, no running
// command and no unfinished task there is nothing a Stop or a spent budget
// could cut short, and the next model call is checked at its own boundary
// ([Agent.admitJobCall]), so the idle look only keeps the manager's stopped
// state honest. PERF.md's "Idle factory manager checks" states the cost.
//
// AND AN IDLE WATCH SLEEPS THE WHOLE BEAT, rather than waking ten times a
// second to decide not to look. The sixth live run's 24 idle job and step
// chats each woke their watch every 100 ms and looked once a second, which
// alone kept the Go scheduler and the kernel's poller busy and cost two stats
// a second per chat for an engine doing nothing. The watch moves to
// the live cadence the moment work starts: a turn opening, a command
// starting and every other change the presence file hears of wake it
// ([Agent.wakeWorkWatch]), and anything that starts unannounced is seen at the
// next idle look.
func (a *Agent) watchJobBudget() {
	if a.jobBudgetOf() == nil {
		return
	}
	go func() {
		// The first look comes at once, as it always has: a manager opened
		// onto a job that was stopped or spent while it was closed learns so
		// before anything else, and the cadence decides only the looks after.
		timer := time.NewTimer(jobBudgetLiveEvery)
		defer timer.Stop()
		var idleLook time.Time
		for {
			select {
			case <-timer.C:
			case <-a.workWake:
			}
			a.mu.Lock()
			closed, stopped, epoch := a.closed, a.workStopped, a.workEpoch
			a.mu.Unlock()
			if closed {
				return
			}
			live := !stopped && a.ownsLiveWork()
			if !stopped && !live {
				if now := time.Now(); now.Sub(idleLook) < jobBudgetIdleEvery {
					stopped = true
				} else {
					idleLook = now
				}
			}
			if !stopped {
				stopOwned := a.factoryOwnedStop()
				if err := a.jobBudgetBlock(); err != nil && err.Error() != JobBudgetUnreadable {
					_ = a.stopWorkEpoch(epoch, err.Error())
				} else if a.watchedFactoryRefusal() == factory.PersonStopReason {
					stopOwned()
				}
			}
			next := jobBudgetIdleEvery
			if live {
				next = jobBudgetLiveEvery
			}
			timer.Reset(next)
		}
	}()
}

// wakeWorkWatch tells the budget watch that work may have started. It never
// blocks and takes no lock, because its callers may hold a.mu.
func (a *Agent) wakeWorkWatch() {
	select {
	case a.workWake <- struct{}{}:
	default:
	}
}

// ownsLiveWork says whether a has anything in flight that a person's Stop or a
// spent budget would end: a turn, a running background command, or a task that
// has not finished.
func (a *Agent) ownsLiveWork() bool {
	a.mu.Lock()
	running, jobs, graph := a.running, a.jobs, a.tasks
	a.mu.Unlock()
	if running {
		return true
	}
	if jobs != nil {
		for _, j := range jobs.all() {
			if j.running() {
				return true
			}
		}
	}
	if graph != nil {
		graph.mu.Lock()
		defer graph.mu.Unlock()
		for _, id := range graph.order {
			if node := graph.nodes[id]; node != nil && (node.state == TaskQueued || node.state == TaskRunning) {
				return true
			}
		}
	}
	return false
}

// Check again at each model boundary, including retries and auxiliary calls.
func (a *Agent) admitJobCall(ctx context.Context) error {
	if err := factoryCallRefusal(ctx); err != nil {
		return err
	}
	if a.jobBudgetOf() == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.jobBudgetBlock()
}

// A job Stop cancels this manager's owned producers without closing the
// conversation or discarding the person's queued answers.
// Capture producers before sampling authority, so a late answer cannot cancel
// work admitted by a subsequent Start.
func (a *Agent) factoryOwnedStop() func() {
	a.mu.Lock()
	jobs, graph := a.jobs, a.tasks
	cancel, automatic := a.cancel, a.running && a.factoryAutomaticTurn
	a.mu.Unlock()
	tasks := map[uint64]<-chan struct{}{}
	if graph != nil {
		graph.mu.Lock()
		for _, id := range graph.order {
			if node := graph.nodes[id]; node != nil {
				tasks[id] = node.done
			}
		}
		graph.mu.Unlock()
	}
	var owned []*job
	if jobs != nil {
		owned = jobs.all()
	}
	return func() {
		if automatic && cancel != nil {
			cancel(stopFor(StopByWorkStopped))
		}
		for id, done := range tasks {
			_, _ = graph.stopAttempt(id, "", done, TaskEndingStopped)
		}
		var stopped sync.WaitGroup
		for _, job := range owned {
			if job.running() {
				stopped.Add(1)
				go func(id int) {
					defer stopped.Done()
					_, _ = jobs.killFor(context.Background(), id)
				}(job.id)
			}
		}
		stopped.Wait()
	}
}
