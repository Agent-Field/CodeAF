package plandb

// Tests for the park: `plandb wait` releases the claim and leaves the task
// open, refuses a task with nothing open to wait on, and the store's own wake
// clears the flag. Done's new root rule is pinned here too: the root's own
// worker may finish the root.

import (
	"strings"
	"testing"
)

func TestWaitParksATaskWithSomethingOpenToWaitOn(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store,
		TaskSpec{ID: "dep", Title: "Dependency"},
		TaskSpec{ID: "waiter", Title: "Waiter", Dependencies: []Dependency{{TaskID: "dep", Kind: DepSuggests}}},
		TaskSpec{ID: "empty", Title: "Nothing to wait on"},
	)

	// A task with nothing open to wait on is refused, naming the reason, so a
	// worker cannot park forever on nothing.
	if _, err := store.Claim("empty", "empty"); err != nil {
		t.Fatalf("claim empty: %v", err)
	}
	if _, err := store.Wait("empty", "empty"); err == nil || !strings.Contains(err.Error(), "nothing to wait for") {
		t.Fatalf("Wait with nothing open = %v, want the refusal naming it", err)
	}
	if task := store.Task("empty"); task.Waiting {
		t.Fatal("a refused wait parked the task anyway")
	}

	// The waiter has an open dependency, so it parks: the claim is released,
	// the flag is set with the moment it parked, and the task is not done.
	if _, err := store.Claim("waiter", "waiter"); err != nil {
		t.Fatalf("claim waiter: %v", err)
	}
	parked, err := store.Wait("waiter", "waiter")
	if err != nil {
		t.Fatalf("Wait with an open dependency: %v", err)
	}
	if !parked.Waiting || parked.WaitedAt.IsZero() {
		t.Fatalf("the parked task = waiting %v from %v, want the flag and the moment", parked.Waiting, parked.WaitedAt)
	}
	if parked.ClaimedBy != "" {
		t.Fatalf("the parked task is still claimed by %q, want its claim released", parked.ClaimedBy)
	}
	if parked.Status == StatusDone || terminal(parked.Status) {
		t.Fatalf("the parked task is %s, want it open and not done", parked.Status)
	}
	// A parked task counts as open for the finish law: the dependency could not
	// complete past it. Here the waiter itself has only advice in its way, so
	// its own finish is the pass.
	if ok, reason := store.CanFinish("waiter"); !ok {
		t.Fatalf("CanFinish on the parked task = false %q, want true — a park is open, not blocked", reason)
	}
	// It is off the ready frontier while it waits: the runtime brings it back
	// on the wake road, not the ordinary dispatch.
	for _, ready := range store.ReadySet().Runnable {
		if ready.ID == "waiter" {
			t.Fatal("a parked task is on the ready frontier, want it held for the wake")
		}
	}

	// The wake clears the flag, and the task is claimable again.
	woken, err := store.Wake("waiter")
	if err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if woken.Waiting || !woken.WaitedAt.IsZero() {
		t.Fatalf("the woken task = waiting %v from %v, want both cleared", woken.Waiting, woken.WaitedAt)
	}
	if _, err := store.Claim("waiter", "waiter"); err != nil {
		t.Fatalf("claim after the wake: %v", err)
	}
}

// TestDoneLetsTheRootsOwnWorkerFinishTheRoot: the one rule changed for c174.
// The root is never claimed, so its worker names the root's id instead; any
// other agent is still refused, and the root still cannot close over an open
// child.
func TestDoneLetsTheRootsOwnWorkerFinishTheRoot(t *testing.T) {
	store := planOpen(t, "")
	root := store.RootID()

	// A worker that is not the root's own is refused, exactly as before.
	if _, err := store.Done(root, "someone-else", "not mine", nil, nil); err == nil || !strings.Contains(err.Error(), "harness owns root completion") {
		t.Fatalf("Done on the root by another agent = %v, want the harness-owns refusal", err)
	}

	// An open child still blocks the root: the finish law holds for the root's
	// own worker too.
	planAdd(t, store, TaskSpec{ID: "kid", Title: "Child"})
	if _, err := store.Done(root, root, "too early", nil, nil); err == nil || !strings.Contains(err.Error(), `"kid"`) {
		t.Fatalf("Done on the root over an open child = %v, want the child named", err)
	}

	// With the child landed, the root's own worker finishes it.
	planFinish(t, store, "kid", "kid", "done")
	rootTask, err := store.Done(root, root, "the run is done", nil, nil)
	if err != nil {
		t.Fatalf("Done on the root by its own worker: %v", err)
	}
	if rootTask.Status != StatusDone || rootTask.Result != "the run is done" {
		t.Fatalf("the root = %s with %q, want done with the worker's result", rootTask.Status, rootTask.Result)
	}
}
