package session

// The person's own door onto a run's plan: the hard verbs a surface offers
// beside the soft note, and the one place this package writes the store the
// worker's own CLI also writes. Every write goes through a store method — the
// graph laws, the cascade to descendants and dependents, and the ownership
// checks are the store's and are not repeated here — and the only thing this
// layer owns is the boundary: an id is resolved inside THIS conversation's
// plan, so a task another chat spawned is never reachable, and the store's own
// sentence is what a store refusal answers with.
//
// LOCK ORDER: every verb opens the store under the plan gate the way the
// reading verbs and every pulse do (openPlanHandle), so a steering write can
// never interleave with a pulse mid-pass; the store's own mutex and its one
// transaction serialize the writers on either side of the process.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// errPlanOtherChat is the refusal every steering verb answers with when the id
// names a task another conversation spawned. It is shared so the six verbs
// cannot drift apart on the sentence a surface reads back.
var errPlanOtherChat = errors.New("that task belongs to another conversation")

// errPlanEndedRun is the one sentence every steering verb answers when its
// task belongs to an earlier run. An ended run stays readable, but is not
// steered.
var errPlanEndedRun = errors.New("that task's run has ended")

// planNoTask is the refusal for an id the conversation's plan does not hold,
// naming the id exactly as the caller wrote it so a surface can echo it.
func planNoTask(id string) error {
	return fmt.Errorf("no task %s in this conversation", strings.TrimSpace(id))
}

// planSteer is the one road all six verbs take: open the conversation's store,
// resolve the id inside it, refuse a task another chat spawned or an id the
// plan does not hold, and otherwise run the store verb the caller names. A
// store refusal — the root is the harness's, a terminal task cannot be
// cancelled, a revision is only for work that has not started — travels back
// as the store wrote it, because the store is the one that knows its own laws.
func (a *Agent) planSteer(id string, write func(*plandb.Store, *plandb.Task) error) error {
	stores, plan, closeStores := a.openPlanReadHandles()
	defer closeStores()
	if len(stores) == 0 {
		return errPlanNoStore
	}

	key := planTaskID(id)
	live := stores[len(stores)-1]
	if task := live.Task(key); task != nil {
		if task.Chat != plan.chat {
			return errPlanOtherChat
		}
		return write(live, task)
	}
	for _, store := range stores[:len(stores)-1] {
		if task := store.Task(key); task != nil {
			if task.Chat != plan.chat {
				return errPlanOtherChat
			}
			return errPlanEndedRun
		}
	}
	return planNoTask(id)
}

// PlanNote leaves a note in the person's own voice on one task. It is the soft
// steering beside the hard verbs: the worker reads it in its next frame, and
// the note carries the person as its author the way the store spells that
// (AddPersonNote), so a surface draws the two voices apart.
func (a *Agent) PlanNote(id, text string) error {
	return a.planSteer(id, func(store *plandb.Store, task *plandb.Task) error {
		_, err := store.AddPersonNote(task.ID, text)
		return err
	})
}

// PlanPause holds a task and everything under it out of the ready frontier
// without changing its rung: running steps finish, and nothing new in the
// subtree is launched until it is resumed.
func (a *Agent) PlanPause(id string) error {
	return a.planSteer(id, func(store *plandb.Store, task *plandb.Task) error {
		_, err := store.Pause(task.ID)
		return err
	})
}

// PlanResume releases the hold PlanPause set. A task that was not held is left
// alone and answers no error, so a surface may resume without asking first.
func (a *Agent) PlanResume(id string) error {
	return a.planSteer(id, func(store *plandb.Store, task *plandb.Task) error {
		_, err := store.Resume(task.ID)
		return err
	})
}

// PlanCancel ends a task, its descendants and the work hard-depending on it.
// The cascade is the store's own law; the person's cancel carries no reason,
// because the store records the ending and the surface reads the word.
func (a *Agent) PlanCancel(id string) error {
	return a.planSteer(id, func(store *plandb.Store, task *plandb.Task) error {
		_, err := store.Cancel(task.ID, "")
		return err
	})
}

// PlanAmend prepends text to a task's description, the way the CLI's `task
// amend --prepend` does: the plan learns while it runs, and the text lands at
// the front of the work order the next worker reads.
func (a *Agent) PlanAmend(id, text string) error {
	return a.planSteer(id, func(store *plandb.Store, task *plandb.Task) error {
		_, err := store.Amend(task.ID, text)
		return err
	})
}

// PlanPriority sets a task's priority through the store's revision verb, the
// one road the store has for it. The revision is a contract change, so the
// store refuses a task that has already started.
func (a *Agent) PlanPriority(id string, n int) error {
	return a.planSteer(id, func(store *plandb.Store, task *plandb.Task) error {
		_, err := store.Revise(task.ID, plandb.TaskPatch{Priority: &n})
		return err
	})
}
