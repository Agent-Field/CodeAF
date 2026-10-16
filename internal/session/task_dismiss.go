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
// back and the error returned.
func (g *TaskGraph) setDismissed(id string, hidden bool) error {
	return g.writeAppControl(func() (func(), error) {
		was, had := g.dismissed[id]
		if was == hidden {
			return nil, nil
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
// rows is every task the app lists, so a dependent is found by the same read the
// app draws.
func appDismissible(row AppTaskView, rows []AppTaskView) error {
	switch row.State {
	case "failed", "stopped", "declined":
	case "done":
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

// appDismissAction is tasks.dismiss and tasks.restore for every road. Dismissing
// what is already dismissed, and restoring what is not, succeed and change
// nothing, so a replayed request is not an error.
func (a *Agent) appDismissAction(intent AppTaskIntent) error {
	rows, err := a.AppTasks()
	if err != nil {
		return err
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
	if err := graph.setDismissed(row.ID, hide); err != nil {
		return fmt.Errorf("could not save that: %w", err)
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
