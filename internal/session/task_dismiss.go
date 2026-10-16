package session

// DISMISS: A REVERSIBLE HIDE, AND NOTHING ELSE.
//
// A conversation that left forty failed tasks behind does not need forty cards
// in front of it, and it does not need them deleted either — they are the
// record of what went wrong. Dismissing is the engine's one generic way to say
// "I have seen this, stop counting it", on every road a task can be on: a graph
// node, a run row and a row of a run's plan store.
//
// THE LAWS.
//
//   - IT CHANGES NO WORK. The task's state, failure, report, files and spend are
//     exactly what they were; nothing is retried, cancelled, declined, deleted or
//     dispatched, so nothing here can spend money. Restoring is the same fact
//     turned off, and it is an explicit action, never a side effect.
//   - ONLY A SETTLED FAILURE WITH NOTHING WAITING ON IT. A task still moving, a
//     task waiting for the person and a task something live waits on or hangs
//     under cannot be hidden: hiding the one thing a live task is blocked on
//     would bury the reason it is blocked. The refusal says which.
//   - IT IS PERSISTED BEFORE IT IS REPORTED. The set is part of the checkpoint;
//     a failed write puts the set back and the caller hears the error, so there
//     is no yes that a restart would take back.
//   - ONE SET, ONE PLACE. The set lives on the graph, keyed by the id the app
//     reads the task under, so a node, a run row and a plan row cannot disagree
//     about what a restart brings back.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The two actions [Agent.AppTaskAction] takes for this, spelled as the app
// wire's methods (tasks.dismiss, tasks.restore) spell them.
const (
	AppActionDismiss = "dismiss"
	AppActionRestore = "restore"
)

// isDismissed says whether the task the app calls id is hidden. Nil-safe.
func (g *TaskGraph) isDismissed(id string) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.dismissed[id]
}

// setDismissed changes one task's visibility and writes the checkpoint under the
// same hold ([TaskGraph.writeAppControl]); on a failed write the set is put
// back and the error returned. With guard set, the graph's own refusal
// ([TaskGraph.dismissibleLocked]) is asked under that same hold.
func (g *TaskGraph) setDismissed(id string, hidden, guard bool) error {
	return g.writeAppControl(func() (func(), error) {
		was, had := g.dismissed[id]
		if was == hidden {
			return nil, nil
		}
		if guard {
			if err := g.dismissibleLocked(id); err != nil {
				return nil, err
			}
		}
		if g.dismissed == nil {
			g.dismissed = map[string]bool{}
		}
		if hidden {
			g.dismissed[id] = true
		} else {
			delete(g.dismissed, id)
		}
		return func() {
			if had {
				g.dismissed[id] = was
			} else {
				delete(g.dismissed, id)
			}
		}, nil
	})
}

// appSettled is the app states in which work is over.
func appSettled(state string) bool {
	switch state {
	case "done", "failed", "stopped", "declined":
		return true
	}
	return false
}

// appDismissible refuses what must not be hidden, in words a person can act on.
// ONLY A PLAIN FAILURE IS HIDDEN. A task the person stopped or declined keeps its
// own state and its own place on the page, and one that is waiting on an answer, a
// model or a hold is unresolved however it came to be failed. rows is every task
// the app lists, so a dependent is found by the same read the app draws.
func appDismissible(row AppTaskView, rows []AppTaskView) error {
	switch {
	case row.State == "failed" && !row.Held && row.RoutingState != TaskRoutingBlocked:
	case row.State == "failed":
		return fmt.Errorf("task %s is not a settled failure: it is on hold or waiting on a model", row.ID)
	case row.State == "done":
		return fmt.Errorf("task %s is not a settled failure: it finished", row.ID)
	default:
		return fmt.Errorf("task %s is not a settled failure: it is %s", row.ID, row.State)
	}
	for _, other := range rows {
		if other.ID == row.ID || appSettled(other.State) {
			continue
		}
		if other.Parent == row.ID {
			return fmt.Errorf("task %s still has %s under it, which is %s", row.ID, other.ID, other.State)
		}
		for _, waited := range other.Waits {
			if waited == row.ID {
				return fmt.Errorf("task %s is not settled work to hide: %s still waits on it", row.ID, other.ID)
			}
		}
	}
	return nil
}

// dismissibleLocked is the same refusal asked of the graph's own records, with
// the graph's lock held. IT IS WHAT MAKES THE CHECK AND THE CHANGE ONE STEP for
// every road the graph holds — nodes and run rows: a retry, a resume or a new
// dependent is admitted under this lock, so none can land between this answer
// and the write that follows it. A plan row lives in its own store and is
// refused by [Agent.appDismissAction] before any read or mutation.
func (g *TaskGraph) dismissibleLocked(id string) error {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return fmt.Errorf("task %s: no such task", id)
	}
	if node := g.nodes[n]; node != nil {
		// A STOP IS KNOWN BY THE ENDING AS WELL AS THE FLAG: a restored node has only the
		// ending, and its stop is still the person's, kept as it is.
		if node.endingLocked() == TaskEndingStopped {
			return fmt.Errorf("task %s is stopped: a stop is kept as it was", id)
		}
		if node.spec.quick != nil || node.state != TaskFailed || node.stopped || node.app.Declined || node.app.Paused || g.deleted[n] {
			return fmt.Errorf("task %s is not a settled failure: it is %s", id, appNodeState(node))
		}
	} else if row, ok := g.runRowLocked(n); ok {
		if row.Ending == TaskEndingStopped {
			return fmt.Errorf("task %s is stopped: a stop is kept as it was", id)
		}
		if row.State != TaskFailed || row.Stopped || row.Paused || row.park != nil {
			return fmt.Errorf("task %s is not a settled failure: it is %s", id, appRunRowState(row))
		}
	} else {
		return fmt.Errorf("task %s: no such task", id)
	}
	for _, other := range g.nodes {
		if other == nil || other.id == n || other.state.settled() {
			continue
		}
		if other.parent == n {
			return fmt.Errorf("task %s still has %d under it, which is %s", id, other.id, appNodeState(other))
		}
		for _, waited := range other.dependsOn {
			if waited == n {
				return fmt.Errorf("task %s is not settled work to hide: %d still waits on it", id, other.id)
			}
		}
	}
	for _, row := range g.runRowsLocked() {
		if row.ID == n || row.State.settled() {
			continue
		}
		if row.Parent == n && row.Run != "" {
			return fmt.Errorf("task %s still has %d under it, which is %s", id, row.ID, appRunRowState(row))
		}
		for _, waited := range row.DependsOn {
			if waited == n {
				return fmt.Errorf("task %s is not settled work to hide: %d still waits on it", id, row.ID)
			}
		}
	}
	return nil
}

// ErrPlanDismissUnsupported refuses external-plan hiding before any read or mutation.
var ErrPlanDismissUnsupported = errors.New("dismissing plan tasks is not supported yet; nothing was changed")

// appDismissAction applies reversible visibility only where admission is atomic.
func (a *Agent) appDismissAction(intent AppTaskIntent) error {
	if intent.Action == AppActionDismiss && strings.HasPrefix(intent.ID, "t-") {
		return ErrPlanDismissUnsupported
	}
	rows, err := a.AppTasks()
	if err != nil {
		return err
	}
	return a.appDismissWith(intent, rows)
}

// appDismissWith is [Agent.appDismissAction] over rows already read, so a test can
// hand it a read that has gone stale.
func (a *Agent) appDismissWith(intent AppTaskIntent, rows []AppTaskView) error {
	if intent.Action == AppActionDismiss && strings.HasPrefix(intent.ID, "t-") {
		return ErrPlanDismissUnsupported
	}
	var row AppTaskView
	found := false
	for _, candidate := range rows {
		if candidate.ID == intent.ID {
			row, found = candidate, true
			break
		}
	}
	if !found {
		return errors.New("no such task")
	}
	if err := appExpect(intent, row.State, row.Version); err != nil {
		return err
	}
	hide := intent.Action == AppActionDismiss
	if row.Dismissed == hide {
		return nil
	}
	if hide {
		if err := appDismissible(row, rows); err != nil {
			return err
		}
	}
	graph := a.tasker()
	if graph == nil {
		return errors.New("no such task")
	}
	if err := graph.setDismissed(row.ID, hide, hide); err != nil {
		return err
	}

	a.announceDismissal(graph, row.ID, hide)
	return nil
}

// announceDismissal says the change on the road the task is on: a node through
// the graph's own report, a run row by republishing it. A plan row has no notice
// road; the app's door reads it back after the action and says task.changed
// itself.
func (a *Agent) announceDismissal(g *TaskGraph, id string, hidden bool) {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return
	}
	if node := g.node(n); node != nil {
		g.announce(node)
		return
	}
	g.mu.Lock()
	row, ok := g.runRowLocked(n)
	g.mu.Unlock()
	if ok {
		row.Dismissed = hidden
		a.emitTaskUpdate(row)
	}
}
