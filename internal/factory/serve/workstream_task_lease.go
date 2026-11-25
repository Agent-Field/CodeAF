package serve

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// RUNS SURVIVE A RESTART, AND NOTHING SAYS RUNNING WITH NOTHING BEHIND IT
// (owner decisions 2026-11-17 §10; DESIGN §11.3; live2 WL, where a restart six
// seconds into a burst left six of eight tasks Running for over nine minutes).
//
// A task run is a goroutine (workstream_agent_task.go), and a restart ends
// every goroutine. So each run also holds a lease in the store
// (store.TaskRun): kept when the run begins, beating every taskLeaseBeat while
// it goes, dropped when it ends here. A server that closes leaves its leases
// behind on purpose: they are how the next start knows what was cut off.
//
// The sweep (sweepTaskRuns) settles every Afi task that says running and has
// no run in this process:
//
//   - CUT OFF BY A RESTART: everything it holds predates this process (its
//     lease's last beat, or the task's last change when no lease was ever
//     kept, as on a build before leases). Its thread says it was interrupted,
//     and it RESUMES ONCE BY ITSELF from its own conversation, which the run's
//     folder keeps. A run that was already resumed once is left Interrupted,
//     with Start.
//   - WENT QUIET WHILE THIS SERVER WAS UP: no beat for taskNoReportAfter. It
//     is "Didn't report back", with Start.
//
// And a person's Start on a task that says running with no run behind it
// starts it again (patchWorkstreamTask); it never answers 200 for nothing.

const (
	// taskLeaseBeat is how often a run says it is alive.
	taskLeaseBeat = 5 * time.Second
	// taskRestartSettle is how long a new process waits before it calls a
	// lease another process holds cut off: long enough for a live holder to
	// beat once after this process began.
	taskRestartSettle = taskLeaseBeat + 2*time.Second
	// taskNoReportAfter is how long a run may go without a beat while this
	// server is up before its task is "Didn't report back".
	taskNoReportAfter = 60 * time.Second
	// taskResumesMost is how many times a cut-off run resumes by itself.
	taskResumesMost = 1
)

// The words a person reads when a run is settled.
const (
	taskInterruptedLine = "Interrupted: the server restarted while Afi was working on it"
	taskResumedLine     = "Resumed after the restart"
	taskInterruptedWhy  = "The server restarted while Afi was working on it. Press Start to run it again."
	taskNoReportWhy     = "Afi stopped reporting back while working on it. Press Start to run it again."
)

// ownerLocked is this process's name on the leases it holds, made on first
// use. d.mu is held.
func (d *agentDesk) ownerLocked() string {
	if d.owner == "" {
		id, err := cell.NewID(time.Now().UTC())
		if err != nil {
			id = time.Now().UTC().Format(time.RFC3339Nano)
		}
		d.owner = "srv_" + id
	}
	return d.owner
}

// leaseSince is when this process began holding runs, for the sweep.
func (h *Handler) leaseSince() time.Time {
	d := &h.agentDesk
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.since.IsZero() {
		d.since = h.clock().UTC()
	}
	return d.since
}

// holdTaskLease keeps key's lease as owner, beats it until ctx ends, and
// answers the drop the run calls when it ends here. It runs inside the run's
// goroutine, after the write that started the run has ended: that store has
// one connection.
func (h *Handler) holdTaskLease(ctx context.Context, key taskRunKey, owner, starter, words string, started time.Time, resumes int, beat func() float64) func() {
	st := h.config.Store
	if st == nil {
		return func() {}
	}
	stamp := func() string { return h.clock().UTC().Format(time.RFC3339Nano) }
	_ = st.PutTaskRun(store.TaskRun{WS: key.ws, Task: key.task, Owner: owner, Starter: starter,
		Started: started.UTC().Format(time.RFC3339Nano), Beat: stamp(), Resumes: resumes, Words: words})
	done := make(chan struct{})
	guard.Go("factory workstream task lease", func() {
		tick := time.NewTicker(taskLeaseBeat)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-tick.C:
				_ = st.BeatTaskRun(key.ws, key.task, owner, stamp(), beat())
			}
		}
	})
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			_ = st.DropTaskRun(key.ws, key.task, owner)
		})
	}
}

// startTaskSweep runs the sweep now and then every taskLeaseBeat until the
// server closes. It is started only by a server that runs agents
// (Config.WorkstreamAgent): nothing else can resume a run.
func (h *Handler) startTaskSweep() {
	if h.config.Store == nil || h.config.WorkstreamAgent == nil {
		return
	}
	h.leaseSince()
	d := &h.agentDesk
	d.mu.Lock()
	if d.ctx == nil {
		d.ctx, d.stop = context.WithCancel(context.Background())
	}
	ctx := d.ctx
	done := make(chan struct{})
	d.sweepDone = done
	d.mu.Unlock()
	guard.Go("factory workstream task sweep", func() {
		defer close(done)
		// An answer kept while nothing could run Afi is carried on at start
		// when the server now has a key (set before a restart, say), as a
		// key set while it runs carries it on (org_keys.go).
		h.resumeParkedAnswers()
		tick := time.NewTicker(taskLeaseBeat)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				h.sweepTaskRuns()
			}
		}
	})
}

// sweepTaskRuns settles every Afi task that says running with no run in this
// process behind it (the file's comment says how).
func (h *Handler) sweepTaskRuns() {
	st := h.config.Store
	if st == nil {
		return
	}
	now, since := h.clock().UTC(), h.leaseSince()
	leases, err := st.TaskRuns()
	if err != nil {
		return
	}
	held := map[taskRunKey]store.TaskRun{}
	for _, l := range leases {
		held[taskRunKey{l.WS, l.Task}] = l
	}
	wss, err := st.Workstreams()
	if err != nil {
		return
	}
	d := &h.agentDesk
	d.mu.Lock()
	me := d.ownerLocked()
	d.mu.Unlock()
	for _, ws := range wss {
		tasks, err := st.WorkstreamTasks(ws.ID)
		if err != nil {
			continue
		}
		for _, t := range tasks {
			key := taskRunKey{ws.ID, t.ID}
			if t.State != factory.TaskRunning || t.DoneBy.Kind != factory.TaskByAfi || h.taskTurnRunning(ws.ID, t.ID) {
				continue
			}
			l, leased := held[key]
			last := parseStamp(t.UpdatedAt)
			if leased {
				last = parseStamp(l.Beat)
			}
			switch {
			case leased && l.Queued && l.Owner != me && last.Before(since) && now.Sub(since) >= taskRestartSettle:
				// It was waiting for its place, not working: it waits again,
				// with nothing to say on its thread, and a run an answer
				// started is still that run (D1's answered, kept on the lease).
				_ = h.startTaskRun(ws, t, l.Starter, l.Words, l.Resumes, l.Answered)
			case (!leased || l.Owner != me) && last.Before(since) && now.Sub(since) >= taskRestartSettle:
				h.settleCutRun(ws, t, l, leased)
			case now.Sub(last) >= taskNoReportAfter:
				h.settleQuietRun(ws, t)
			}
		}
		// A lease whose task no longer says running is a run that ended
		// without its own drop (a crash between the two): forget it.
		for _, t := range tasks {
			if l, ok := held[taskRunKey{ws.ID, t.ID}]; ok && t.State != factory.TaskRunning && !h.taskTurnRunning(ws.ID, t.ID) {
				_ = st.DropTaskRun(ws.ID, t.ID, l.Owner)
			}
		}
	}
}

// parseStamp reads an RFC 3339 stamp; one that cannot be read is the zero
// time, which is before everything.
func parseStamp(s string) time.Time {
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return at
}

// settleCutRun settles a run a restart cut off: Interrupted, then resumed
// once by itself as whoever started it; Interrupted, with Start, when it was
// already resumed or this server cannot run Afi.
func (h *Handler) settleCutRun(ws factory.OrgWorkstream, seen factory.WorkstreamTask, l store.TaskRun, leased bool) {
	starter := l.Starter
	if !leased || starter == "" {
		starter = seen.CreatedBy.ID
		if seen.Owner.Kind == factory.TaskOwnerPerson && seen.Owner.ID != "" {
			starter = seen.Owner.ID
		}
	}
	resumes := l.Resumes
	var settled factory.WorkstreamTask
	err := h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		t, ok := keptTask(tw, ws.ID, seen.ID)
		if !ok || t.State != factory.TaskRunning || t.Rev != seen.Rev {
			return errSettledElsewhere
		}
		stamp := h.clock().UTC().Format(time.RFC3339Nano)
		newID := h.newTaskID("te_")
		events := []factory.WorkstreamTaskEvent{{ID: newID(), Task: t.ID, At: stamp, By: afiBy, Kind: factory.TaskEventState, Text: taskInterruptedLine}}
		t.Rev++
		t.UpdatedAt = stamp
		if resumes < taskResumesMost && h.serverAgentServed() {
			events = append(events, factory.WorkstreamTaskEvent{ID: newID(), Task: t.ID, At: stamp, By: afiBy, Kind: factory.TaskEventState, Text: taskResumedLine})
			if err := keepTask(tw, t, seen.Rev); err != nil {
				return err
			}
			if err := tw.Append(ws.ID, events...); err != nil {
				return err
			}
			settled = t
			return h.startTaskRun(ws, t, starter, resumeWords(t, l.Words), resumes+1, false)
		}
		t.State, t.StateReason = factory.TaskInterrupted, taskInterruptedWhy
		if err := keepTask(tw, t, seen.Rev); err != nil {
			return err
		}
		if err := tw.DropRun(ws.ID, t.ID); err != nil {
			return err
		}
		settled = t
		return tw.Append(ws.ID, events...)
	})
	if err != nil {
		if !errors.Is(err, errSettledElsewhere) {
			// The run may have begun before the write failed; nothing says
			// it is running, so nothing may be.
			h.stopTaskTurn(ws.ID, seen.ID)
		}
		return
	}
	h.publishTasks(ws, afiBy.Name, settled)
}

// settleQuietRun marks a run that stopped beating while this server was up
// "Didn't report back", with Start.
func (h *Handler) settleQuietRun(ws factory.OrgWorkstream, seen factory.WorkstreamTask) {
	var settled factory.WorkstreamTask
	err := h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		t, ok := keptTask(tw, ws.ID, seen.ID)
		if !ok || t.State != factory.TaskRunning || t.Rev != seen.Rev {
			return errSettledElsewhere
		}
		stamp := h.clock().UTC().Format(time.RFC3339Nano)
		t.State, t.StateReason = factory.TaskNoReport, taskNoReportWhy
		t.Rev++
		t.UpdatedAt = stamp
		if err := keepTask(tw, t, seen.Rev); err != nil {
			return err
		}
		if err := tw.DropRun(ws.ID, t.ID); err != nil {
			return err
		}
		settled = t
		return tw.Append(ws.ID, factory.WorkstreamTaskEvent{ID: h.newTaskID("te_")(), Task: t.ID, At: stamp, By: afiBy,
			Kind: factory.TaskEventState, Text: factory.StateWords(factory.TaskNoReport)})
	})
	if err == nil {
		h.stopTaskTurn(ws.ID, seen.ID)
		h.publishTasks(ws, afiBy.Name, settled)
	}
}

// errSettledElsewhere ends a settling write whose task changed under it.
var errSettledElsewhere = errors.New("the task changed while it was being settled")

// keptTask is one kept task read inside a write.
func keptTask(tw store.TaskWrite, ws, id string) (factory.WorkstreamTask, bool) {
	all, err := tw.Tasks(ws)
	if err != nil {
		return factory.WorkstreamTask{}, false
	}
	return findTask(all, id)
}

// resumeWords is what Afi is asked when its run is taken up again after a
// restart: what the run was asked (words, kept on its lease; the task's own
// words when none was kept), after a line saying it was cut off. The run's
// folder keeps its conversation, so it carries on rather than beginning again.
func resumeWords(t factory.WorkstreamTask, words string) string {
	if strings.TrimSpace(words) == "" {
		words = taskWords(t)
	}
	return "The server restarted while you were working on this task, and your work was cut off. " +
		"Carry on from where you stopped: check what you already wrote to the workstream before writing it again, then finish the task.\n\n" +
		words
}

// fillRuns writes onto each running Afi task whether a run is working on it
// in this server now, and since when, so a person can tell a stuck task from
// a working one and its Start is offered. It is read-time only.
func (h *Handler) fillRuns(ws string, all []factory.WorkstreamTask) {
	for i := range all {
		t := &all[i]
		t.Run, t.Queue = nil, nil
		if t.State != factory.TaskRunning || t.DoneBy.Kind != factory.TaskByAfi {
			continue
		}
		run := &factory.TaskRunNow{Live: h.taskTurnRunning(ws, t.ID)}
		d := &h.agentDesk
		d.mu.Lock()
		place := d.placeLocked(taskRunKey{ws, t.ID})
		watch := d.watch[taskRunKey{ws, t.ID}]
		d.mu.Unlock()
		if watch != nil {
			run.Now, run.Steps, run.USD = watch.facts()
			run.USD = math.Round(run.USD*10000) / 10000
		}
		if place > 0 {
			t.Queue = &factory.TaskQueue{Place: place}
		}
		if h.config.Store != nil {
			if l, ok, err := h.config.Store.TaskRunOf(ws, t.ID); err == nil && ok {
				run.Since = l.Started
			}
		}
		t.Run = run
	}
}
