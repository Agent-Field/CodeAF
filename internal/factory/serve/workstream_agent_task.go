package serve

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// A TASK AFI WORKS ON THE SERVER RUNS IN A SEAT OF ITS OWN, NEVER IN A
// PERSON'S PANE. Each person's conversation with Afi is private to them (L1
// F2), so a task somebody started cannot be answered into anybody's
// conversation: it has its own folder, its own memory of the work, and
// publishes no agent event at all. What it did is told where everyone who can
// see the task reads it, the task itself: its state, its results and a line
// in its thread, all written through the same door and the same routes a
// person's client uses.
//
// The run's rights are the rights of the person who started it, checked on
// every write as they are now, so a starter who loses edit mid-run stops the
// run's writes; the run's doer is Afi.

// errTaskBusy is the refusal a second start gets while a task's run is going.
var errTaskBusy = errors.New("this task is already being worked on")

// errTaskNoRunner is the refusal when this server cannot run Afi at all: no
// runner, or no model key from the operator.
var errTaskNoRunner = errors.New("this organization's server cannot run Afi: it has no model key")

// taskRunKey names one task's run.
type taskRunKey struct{ ws, task string }

// taskRunDir is the folder of a task's run: the agent's own conversation for
// that task and nothing else.
func (h *Handler) taskRunDir(ws, task string) string {
	return filepath.Join(h.config.Store.Root(), agentDirName, ws, "tasks", task)
}

// runTaskTurn starts Afi on task, as starter, with words as what it is asked
// to do, and returns once the run is accepted. It is errTaskBusy while the
// task's previous run is still going and errTaskNoRunner when this server
// cannot run Afi; on either the caller leaves the task where it was. The run
// itself sets the task done or failed through ws_task_update; a run that ends
// without doing either, and was not stopped, is settled by finishTaskRun: done
// with what it wrote, or failed with the reason, so a task never says running
// with nothing at work on it.
//
// IT TOUCHES NO WORKSTREAMS DATABASE BEFORE IT RETURNS, so a task write may call
// it inside its own EditWorkstreamTasks transaction: that store has one
// connection, and a synchronous read here would wait on the transaction that
// is waiting on it. The organization's key is a file of its own. Every read of
// the task happens in the run, after the caller's transaction has ended; a
// caller whose transaction fails after a nil return calls stopTaskTurn.
func (h *Handler) runTaskTurn(ws factory.OrgWorkstream, task factory.WorkstreamTask, starter, words string) error {
	return h.startTaskRun(ws, task, starter, words, 0)
}

// startTaskRun is runTaskTurn for a run that has already been resumed
// `resumes` times after a restart cut it off (workstream_task_lease.go).
func (h *Handler) startTaskRun(ws factory.OrgWorkstream, task factory.WorkstreamTask, starter, words string, resumes int) error {
	if !h.serverAgentServed() {
		return errTaskNoRunner
	}
	starter = factory.NormalizeHandle(starter)
	if starter == "" {
		return errors.New("a task's run needs the person who started it")
	}
	now := h.clock().UTC()
	tid, err := cell.NewID(now)
	if err != nil {
		return err
	}
	turn := "turn_" + tid
	key := taskRunKey{ws.ID, task.ID}
	d := &h.agentDesk
	d.mu.Lock()
	if d.ctx == nil {
		d.ctx, d.stop = context.WithCancel(context.Background())
	}
	if d.ctx.Err() != nil {
		d.mu.Unlock()
		return errors.New("this factory is closing")
	}
	if d.tasks == nil {
		d.tasks = map[taskRunKey]context.CancelFunc{}
	}
	if _, busy := d.tasks[key]; busy {
		d.mu.Unlock()
		return errTaskBusy
	}
	ctx, cancel := context.WithCancel(d.ctx)
	d.tasks[key] = cancel
	d.wg.Add(1)
	owner := d.ownerLocked()
	d.mu.Unlock()

	door := &agentDoor{h: h, id: ws.ID, asker: starter, wrote: &taskWrites{}}
	run := AgentTurn{Workstream: ws.ID, Name: ws.Name, Turn: turn, Asker: starter, Text: strings.TrimSpace(words), Now: now,
		Task: &AgentTask{ID: task.ID, Title: task.Title},
		Key:  h.orgModelKey(), Dir: filepath.Join(h.taskRunDir(ws.ID, task.ID), "agent"), Door: door,
		// Nobody reads a task run as a conversation: its lines go nowhere.
		Step: func(string, string, string, bool) {}}
	guard.Go("factory workstream task", func() {
		defer d.wg.Done()
		defer func() {
			d.mu.Lock()
			delete(d.tasks, key)
			d.mu.Unlock()
			cancel()
		}()
		// The lease is kept first and beats while the run goes, so a restart
		// that cuts the run off leaves a row the next start settles; it is
		// dropped when the run ends here, unless the server is closing, which
		// is exactly the cut the next start must find.
		lease := h.holdTaskLease(ctx, key, owner, starter, run.Text, now, resumes)
		defer func() {
			if d.ctx.Err() == nil {
				lease()
			}
		}()
		end, err := h.config.WorkstreamAgent(ctx, run)
		if end.USD > 0 {
			h.postAgentSpend(run, end.USD)
		}
		if ctx.Err() != nil {
			// Stopped: whoever stopped it has already said so on the task.
			return
		}
		h.finishTaskRun(door, task.ID, err)
	})
	return nil
}

// stopTaskTurn stops task's run, if one is going. The task's own Stop says
// so on the task; this only ends the work.
func (h *Handler) stopTaskTurn(ws, task string) bool {
	d := &h.agentDesk
	d.mu.Lock()
	defer d.mu.Unlock()
	cancel, ok := d.tasks[taskRunKey{ws, task}]
	if ok {
		cancel()
	}
	return ok
}

// taskTurnRunning is whether task has a run going now.
func (h *Handler) taskTurnRunning(ws, task string) bool {
	d := &h.agentDesk
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.tasks[taskRunKey{ws, task}]
	return ok
}

// finishTaskRun settles a task that still says running once its run has
// ended without setting it done or failed, so a task never says running with
// nothing at work on it.
//
// A RUN THAT ENDED CLEANLY HAVING WRITTEN ITS WORK IS DONE, NOT FAILED: the
// files it made or rewrote become the task's results, beside any it already
// had. Afi often writes the page and says so in the thread without the last
// ws_task_update, and calling that a failure put finished work under Failed
// (E4 F8). A run that wrote nothing, or ended on an error, is failed with why
// in plain words.
func (h *Handler) finishTaskRun(door *agentDoor, task string, err error) {
	ctx := context.Background()
	t, terr := door.task(ctx, task)
	if terr != nil || t.State != factory.TaskRunning {
		return
	}
	if wrote := door.wrote.list(); err == nil && len(wrote) > 0 {
		results := []map[string]string{}
		for _, r := range t.Results {
			switch {
			case r.FileID != "":
				results = append(results, map[string]string{"file": r.FileID})
			case r.URL != "":
				results = append(results, map[string]string{"url": r.URL, "label": r.Label})
			}
		}
		for _, id := range wrote {
			if !slices.ContainsFunc(t.Results, func(r factory.TaskResult) bool { return r.FileID == id }) {
				results = append(results, map[string]string{"file": id})
			}
		}
		args, _ := json.Marshal(map[string]any{"task": task, "rev": t.Rev, "state": factory.TaskDone, "results": results})
		if _, derr := door.Work(ctx, "ws_task_update", args); derr == nil {
			return
		}
		// A result the store refused (a file deleted since) still must not
		// leave the task running: fall through to failed, with why.
		t, terr = door.task(ctx, task)
		if terr != nil || t.State != factory.TaskRunning {
			return
		}
	}
	why := "Afi stopped before saying the task was done"
	if err != nil {
		why = "Afi could not finish: " + cutRunes(err.Error(), 300)
	}
	args, _ := json.Marshal(map[string]any{"task": task, "rev": t.Rev, "state": factory.TaskFailed, "state_reason": why})
	_, _ = door.Work(ctx, "ws_task_update", args)
}
