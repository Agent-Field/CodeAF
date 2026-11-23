package plandb

import (
	"path/filepath"
	"testing"
	"time"
)

// L-LOOP2 C8a: an already-ended root cannot shelter claimed or running work
// from a person's stop, and a late worker cannot overwrite that cancellation.
func TestStopRootCancelsOpenTasksEvenWhenRootAlreadyEnded(t *testing.T) {
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.db"))
	planAdd(t, store, planSpec("claimed", "Claimed"), planSpec("running", "Running"))
	for _, id := range []string{"claimed", "running"} {
		if _, err := store.Claim(id, id, "test-owner"); err != nil {
			t.Fatal(err)
		}
	}
	// Retained stores can contain a terminal root with still-open children.
	store.mu.Lock()
	err := store.transact(func(next *state, now time.Time) error {
		next.Tasks[next.RootID].Status = StatusDone
		next.Tasks[next.RootID].Result = "root's earlier result"
		next.Tasks[next.RootID].CompletedAt = now
		next.Tasks["running"].Status = StatusRunning
		next.Tasks["claimed"].Paused = true
		return nil
	})
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StopRoot("stopped"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"claimed", "running"} {
		task := store.Task(id)
		if task.Status != StatusCancelled || task.ClaimedBy != "" || task.Owner != "" || task.Paused || task.Waiting || task.CompletedAt.IsZero() {
			t.Fatalf("stop left open work: %+v", task)
		}
		_, _ = store.Done(id, id, "late worker result", nil, nil)
		if store.Task(id).Status != StatusCancelled {
			t.Fatalf("worker return overwrote stop: %+v", store.Task(id))
		}
	}
	if root := store.Task(store.RootID()); root.Status != StatusDone || root.Result != "root's earlier result" {
		t.Fatalf("stop rewrote completed root: %+v", root)
	}
	// Late reviews run through a fresh handle, just like a returning engine.
	fresh, err := Open(store.Path(), "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if _, err := fresh.AddReviewCheck(TaskSpec{ID: "late-review", Title: "Late review", Role: RoleCheck}); err == nil {
		t.Fatal("a late worker return reopened the stopped tree for a review")
	}
	if len(store.Tasks()) != 3 || store.Task(store.RootID()).Status != StatusDone {
		t.Fatal("a refused late review changed the stopped store")
	}
}
