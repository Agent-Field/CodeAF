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

// errPlanRunNotHeld is what a hold on the run's own task answers. Nothing holds
// a whole run: the store holds a part and everything under it, and refuses the
// run's task for every caller in a sentence about who owns what, which is no
// sentence for a person. No surface built with this file offers the key there
// (internal/tui3's planOwnTask); a window built before it still does.
var errPlanRunNotHeld = errors.New("a run is not held as a whole: hold one of its parts, or stop it")

// planNoTask is the refusal for an id the conversation's plan does not hold,
// naming the id exactly as the caller wrote it so a surface can echo it.
func planNoTask(id string) error {
	return fmt.Errorf("no task %s in this conversation", strings.TrimSpace(id))
}

// planSteer is the one road all six verbs take: open the conversation's store,
// resolve the id inside it, refuse a task another chat spawned or an id the
// plan does not hold, and otherwise run the store verb the caller names. A
// store refusal — a whole run is not held, a terminal task cannot be
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
		// THE NEWEST RUN IS NOT A LIVE ONE ONCE ITS OWN TASK HAS ENDED. A run's
		// store is set aside only when the NEXT request arrives, so the run that
		// finished last is still the live file, and this road used to take a
		// note, a hold, an amendment or a priority on it as though somebody were
		// there to read them: the write landed and nothing ever read it. An ended
		// run answers the one sentence every earlier run answers.
		if root := live.Task(live.RootID()); root != nil && terminalStoreStatus(root.Status) {
			return errPlanEndedRun
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
// steering beside the hard verbs: the task's worker is handed it between its own
// steps, on the road internal/run's note channel carries, and
// the note carries the person as its author the way the store spells that
// (AddPersonNote), so a surface draws the two voices apart.
func (a *Agent) PlanNote(id, text string) error {
	return a.planSteer(id, func(store *plandb.Store, task *plandb.Task) error {
		_, err := store.AddPersonNote(task.ID, text)
		return err
	})
}

// PlanNoteFromChat leaves a note on one task in THE CONVERSATION'S voice, and
// it is [Agent.PlanNote] with the one difference that matters: the author.
//
// THE MODEL IS NOT THE PERSON, AND THE WORKER MUST BE ABLE TO TELL. A note
// arriving named as the person is a note a worker may read as authority, and a
// conversation that could write in the person's voice could grant itself
// permissions nobody gave it — the same hazard, and the same answer, as the
// `say` door's ([Agent.relayToTask]). So the note carries [plandb.NoteAgentChat]
// and is a worker-side note by the store's own column, and every reader draws it
// apart from the person's own.
func (a *Agent) PlanNoteFromChat(id, text string) error {
	return a.planSteer(id, func(store *plandb.Store, task *plandb.Task) error {
		_, err := store.AddNote(task.ID, plandb.NoteAgentChat, text)
		return err
	})
}

// PlanPause holds a task and everything under it out of the ready frontier
// without changing its rung: running steps finish, and nothing new in the
// subtree is launched until it is resumed.
func (a *Agent) PlanPause(id string) error {
	return a.planSteer(id, func(store *plandb.Store, task *plandb.Task) error {
		if task.ID == store.RootID() {
			return errPlanRunNotHeld
		}
		_, err := store.Pause(task.ID)
		return err
	})
}

// PlanResume releases the hold PlanPause set. A task that was not held is left
// alone and answers no error, so a surface may resume without asking first.
func (a *Agent) PlanResume(id string) error {
	return a.planSteer(id, func(store *plandb.Store, task *plandb.Task) error {
		if task.ID == store.RootID() {
			return errPlanRunNotHeld
		}
		_, err := store.Resume(task.ID)
		return err
	})
}

// PlanCancel ends a task, its descendants and the work hard-depending on it.
// The cascade is the store's own law.
//
// THE CANCEL CARRIES THE STOP'S OWN WORD, because a person pressed it. The
// store keeps the reason with the ending and a row reads `stopped` only off
// that word ([planTaskStopped]); this cancel used to carry none, so a part a
// person stopped — and everything its cascade took down — read `incomplete`,
// the word for work that ran and came up short on its own.
//
// THE RUN'S OWN TASK IS STOPPED AS THE RUN. The store refuses every verb on it,
// because no worker may end the run it is part of; a person may, and the stop
// they are owed is the run's (stoprun.go), never the store's sentence about who
// owns what.
func (a *Agent) PlanCancel(id string) error {
	if row, ok := a.beltRunRootRow(id); ok {
		_, err := a.cancelTask(row, "")
		return err
	}
	return a.planSteer(id, func(store *plandb.Store, task *plandb.Task) error {
		if task.ID == store.RootID() {
			// A RUN NOBODY IS RUNNING ANY MORE. The store holds an open run and this
			// conversation has none going (the program ended under it), so there is
			// no context to cut and the whole stop is the store's: the run and what
			// is open under it end, and the next hand-off starts fresh.
			return store.StopRoot(taskStoppedWord)
		}
		_, err := store.Cancel(task.ID, taskStoppedWord)
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
