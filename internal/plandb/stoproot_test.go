package plandb

import (
	"path/filepath"
	"testing"
)

// A PERSON'S STOP ENDS THE WHOLE RUN IN THE STORE, AND KEEPS WHAT HAD LANDED.
// The run's own task was the one task nothing could end from outside: every
// verb refused it, so a run a person stopped stayed open in the store and the
// next hand-off in the same conversation adopted it and picked the stopped work
// back up. The runtime's verb for the person's word cancels the run's own task
// and everything still open under it, at any depth, and
// leaves every task that had already ended exactly as it ended.
func TestStopRootEndsTheRunAndEverythingStillOpenUnderIt(t *testing.T) {
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	planAdd(t, store, planSpec("landed", "Landed"), planSpec("going", "Going"), planSpec("waiting", "Waiting"))
	planFinish(t, store, "landed", "worker", "landed delivered")
	// WORK TWO LEVELS DOWN is still the run's work.
	child := planSpec("late", "Late")
	child.ParentID = "waiting"
	planAdd(t, store, child)
	if _, err := store.Claim("going", "worker-b"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	if err := store.StopRoot("the person stopped it"); err != nil {
		t.Fatalf("stop root: %v", err)
	}
	if root := store.Task("root"); root.Status != StatusCancelled || root.Error != "the person stopped it" || root.CompletedAt.IsZero() {
		t.Fatalf("the run's own task after a stop = %s, %q, ended %v", root.Status, root.Error, root.CompletedAt)
	}
	for _, id := range []string{"going", "waiting", "late"} {
		if task := store.Task(id); task.Status != StatusCancelled || task.ClaimedBy != "" {
			t.Fatalf("task %s after the run was stopped = %s, claimed by %q", id, task.Status, task.ClaimedBy)
		}
	}
	if task := store.Task("landed"); task.Status != StatusDone || task.Result != "landed delivered" {
		t.Fatalf("work that had already landed was rewritten by the stop: %s, %q", task.Status, task.Result)
	}
	if ready := store.ReadyLeaves(); len(ready) != 0 {
		t.Fatalf("a stopped run still offers %d tasks to a worker", len(ready))
	}
	// TWO PRESSES ARE ONE STOP, and a run that ended by itself is left as it ended.
	if err := store.StopRoot("again"); err != nil {
		t.Fatalf("a second stop was refused: %v", err)
	}
	if root := store.Task("root"); root.Error != "the person stopped it" {
		t.Fatalf("a second stop rewrote the first one's reason: %q", root.Error)
	}
}

// A RUN THAT ENDS ON ITS OWN LIMIT OR ITS OWN WORKER IS ENDED IN THE STORE,
// AND NOT AS A PERSON'S STOP. The run's own task is failed with the reason,
// what was still open is cancelled under the same reason, what had landed keeps
// its ending, and a second call changes nothing.
func TestEndRootFailsTheRunAndCancelsWhatWasStillOpen(t *testing.T) {
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	planAdd(t, store, planSpec("landed", "Landed"), planSpec("going", "Going"))
	planFinish(t, store, "landed", "worker", "landed delivered")

	if err := store.EndRoot("a limit you set stopped it"); err != nil {
		t.Fatalf("end root: %v", err)
	}
	if root := store.Task("root"); root.Status != StatusFailed || root.Error != "a limit you set stopped it" || root.CompletedAt.IsZero() {
		t.Fatalf("the run's own task after its ending = %s, %q, ended %v", root.Status, root.Error, root.CompletedAt)
	}
	if task := store.Task("going"); task.Status != StatusCancelled || task.Error != "a limit you set stopped it" {
		t.Fatalf("open work after the run ended = %s, %q", task.Status, task.Error)
	}
	if task := store.Task("landed"); task.Status != StatusDone {
		t.Fatalf("work that had landed was rewritten: %s", task.Status)
	}
	if err := store.EndRoot("again"); err != nil {
		t.Fatalf("a second ending was refused: %v", err)
	}
	if root := store.Task("root"); root.Error != "a limit you set stopped it" {
		t.Fatalf("a second ending rewrote the first: %q", root.Error)
	}
}
