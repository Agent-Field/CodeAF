package serve

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A LIMIT IS AN ASK, NOT A FAILURE (owner decisions 2026-11-17 §10; DESIGN
// §11.3). live2 WL found no spending limit on an Afi run, no budget check
// while one ran, and no limit on its time or its steps: a run lasted as long as
// the model kept calling tools. Now every run on the organization's server is
// watched:
//
//   - SPENDING: past the task's cap, the workstream's "Start without asking"
//     when one is set and taskSpendCapDefault otherwise, the run pauses;
//   - BUDGET: a workstream that has spent its budget pauses the run, checked
//     when it starts and on every beat while it goes;
//   - STEPS AND TIME: past taskStepsMost tool calls or taskWallMost, it pauses.
//
// A pause ends the run and puts one Ask to the task's owner, in plain words,
// with "Keep going" and "Stop here". Keep going starts a new run on the same
// conversation with a fresh allowance; Stop here stops the task. Failed is
// kept for errors. And a model error that may pass by itself (a busy or
// failing provider, a dropped connection) is tried once more after
// taskRetryAfter before the task fails.

const (
	// taskSpendCapDefault is a run's spending cap when the workstream sets
	// no "Start without asking".
	taskSpendCapDefault = 0.50
	// taskStepsMost is how many tool calls a run makes before it asks.
	taskStepsMost = 60
)

// Variables only so a test need not wait them out.
var (
	// taskWallMost is how long a run works before it asks.
	taskWallMost = 15 * time.Minute
	// taskRetryAfter is how long a run waits before its one more try after a
	// model error that may pass.
	taskRetryAfter = 20 * time.Second
)

// The kinds of pause, and the answers a pause's Ask offers.
const (
	pauseSpend  = "spend"
	pauseBudget = "budget"
	pauseSteps  = "steps"
	pauseWall   = "time"

	limitAskPrefix = "ask_limit_"
	limitKeepGoing = "keep_going"
	limitStopHere  = "stop_here"
)

// runPause is why a run was paused: the cause its context is cancelled with.
type runPause struct {
	kind string
	usd  float64
}

func (p runPause) Error() string { return "paused at a limit: " + p.kind }

// taskSpendCap is the most one run on ws may spend before it asks.
func taskSpendCap(ws factory.OrgWorkstream) float64 {
	if ws.Rules != nil && ws.Rules.StartWithoutAskingUSD > 0 {
		return ws.Rules.StartWithoutAskingUSD
	}
	return taskSpendCapDefault
}

// overBudget is whether ws, with spent more from a run not yet posted, has
// reached its budget.
func (h *Handler) overBudget(wsID string, spent float64) bool {
	st := h.config.Store
	if st == nil {
		return false
	}
	ws, ok, err := st.Workstream(wsID)
	if err != nil || !ok || ws.BudgetUSD == nil {
		return false
	}
	all, err := st.WorkstreamSpend()
	if err != nil {
		return false
	}
	return all[wsID]+spent >= *ws.BudgetUSD
}

// runWatch is one run's spending and steps, as its runner reports them.
type runWatch struct {
	mu    sync.Mutex
	usd   float64
	steps int
	now   string
}

// noting wraps a run's Step so the run's last step is what it is doing now.
func (w *runWatch) noting(step func(kind, text, tool string, failed bool)) func(kind, text, tool string, failed bool) {
	return func(kind, text, tool string, failed bool) {
		if kind == api.AgentEntryStep && strings.TrimSpace(text) != "" {
			w.mu.Lock()
			w.now = strings.TrimSpace(text)
			w.mu.Unlock()
		}
		if step != nil {
			step(kind, text, tool, failed)
		}
	}
}

// facts is what the run has done so far.
func (w *runWatch) facts() (now string, steps int, usd float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.now, w.steps, w.usd
}

func (w *runWatch) spent() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.usd
}

// progress is the run's Progress: it notes what the run has spent and done,
// and pauses it past its cap or its steps.
func (w *runWatch) progress(cap float64, pause context.CancelCauseFunc) func(float64, int) {
	return func(usd float64, steps int) {
		w.mu.Lock()
		w.usd, w.steps = math.Max(w.usd, usd), max(w.steps, steps)
		usd, steps = w.usd, w.steps
		w.mu.Unlock()
		switch {
		case usd >= cap:
			pause(runPause{kind: pauseSpend, usd: usd})
		case steps >= taskStepsMost:
			pause(runPause{kind: pauseSteps, usd: usd})
		}
	}
}

// money is an amount as a person reads it.
func money(usd float64) string { return fmt.Sprintf("$%.2f", usd) }

// limitAsk is the Ask a pause puts to the task's owner.
func limitAsk(p runPause, cap float64, stamp string) factory.TaskAsk {
	ask := factory.TaskAsk{ID: limitAskPrefix + p.kind + "_" + stamp, By: &afiBy, Options: []factory.TaskAskOption{
		{ID: limitKeepGoing, Label: "Keep going"}, {ID: limitStopHere, Label: "Stop here"}}}
	switch p.kind {
	case pauseSpend:
		ask.Title = "Afi has spent " + money(p.usd) + " on this · keep going?"
		ask.Detail = "A task may spend " + money(cap) + " before it asks. Keep going lets it spend that much again."
	case pauseBudget:
		ask.Title = "This workstream has spent its budget · keep going?"
		ask.Detail = "Afi paused here. Raise the workstream's budget first, or Keep going pauses again."
	case pauseSteps:
		ask.Title = fmt.Sprintf("Afi has taken %d steps on this · keep going?", taskStepsMost)
		ask.Detail = fmt.Sprintf("A task takes %d steps before it asks. Keep going gives it that many again.", taskStepsMost)
	case pauseWall:
		ask.Title = fmt.Sprintf("Afi has worked %d minutes on this · keep going?", int(taskWallMost/time.Minute))
		ask.Detail = fmt.Sprintf("A task works %d minutes before it asks. Keep going gives it that long again.", int(taskWallMost/time.Minute))
	}
	return ask
}

// pauseTaskRun puts a pause's Ask on a task that still says running.
func (h *Handler) pauseTaskRun(ws factory.OrgWorkstream, taskID string, p runPause) {
	var paused factory.WorkstreamTask
	err := h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		t, ok := keptTask(tw, ws.ID, taskID)
		if !ok || t.State != factory.TaskRunning {
			return errSettledElsewhere
		}
		stamp := h.clock().UTC().Format(time.RFC3339Nano)
		ask := limitAsk(p, taskSpendCap(ws), stamp)
		ask.AskedAt = stamp
		read := t.Rev
		t.Ask, t.State = &ask, factory.TaskNeeds
		t.Rev++
		t.UpdatedAt = stamp
		if err := keepTask(tw, t, read); err != nil {
			return err
		}
		paused = t
		return tw.Append(ws.ID, factory.WorkstreamTaskEvent{ID: h.newTaskID("te_")(), Task: t.ID, At: stamp, By: afiBy,
			Kind: factory.TaskEventState, Text: "Paused: " + ask.Title})
	})
	if err == nil {
		h.publishTasks(ws, afiBy.Name, paused)
	}
}

// isLimitAsk is whether ask is a pause's.
func isLimitAsk(ask factory.TaskAsk) bool { return strings.HasPrefix(ask.ID, limitAskPrefix) }

// keepGoingWords is what Afi is told when a person answers Keep going.
func keepGoingWords(t factory.WorkstreamTask, ask factory.TaskAsk, who, text string) string {
	// An answer in their own words (Tell it…) is what Afi carries on with;
	// dropping it would ask the same question again.
	said := " said to keep going after you paused (“" + strings.TrimSpace(ask.Title) + "”)."
	if text = strings.TrimSpace(text); text != "" {
		said = " answered after you paused (“" + strings.TrimSpace(ask.Title) + "”): “" + text + "”. Do what they said."
	}
	return firstWords(who, "The task's owner") + said + " Carry on from where you stopped and finish the task.\n\n" + taskWords(t)
}

func firstWords(s, otherwise string) string {
	if strings.TrimSpace(s) == "" {
		return otherwise
	}
	return s
}

// retryLine is the thread line while a run waits for its one more try.
func retryLine() string {
	return fmt.Sprintf("Afi hit a model error and tries again in %d seconds", int(taskRetryAfter/time.Second))
}

// retryTaskRun gives a run cut by a model error that may pass its one more
// try, once taskRetryAfter has gone by, if its task still says running.
func (h *Handler) retryTaskRun(r pendingRun, cause error) {
	ws := r.ws
	stamp := h.clock().UTC().Format(time.RFC3339Nano)
	_ = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		return tw.Append(ws.ID, factory.WorkstreamTaskEvent{ID: h.newTaskID("te_")(), Task: r.key.task, At: stamp, By: afiBy,
			Kind: factory.TaskEventState, Text: retryLine()})
	})
	time.AfterFunc(taskRetryAfter, func() {
		d := &h.agentDesk
		d.mu.Lock()
		closing := d.ctx == nil || d.ctx.Err() != nil
		d.mu.Unlock()
		if closing {
			return
		}
		door := &agentDoor{h: h, id: ws.ID, asker: r.starter, wrote: &taskWrites{}}
		t, err := door.task(context.Background(), r.key.task)
		if err != nil || t.State != factory.TaskRunning {
			return
		}
		r.retried = true
		if err := h.admitTaskRun(r); err != nil && !errors.Is(err, errTaskBusy) {
			h.finishTaskRun(door, t.ID, cause)
		}
	})
}
