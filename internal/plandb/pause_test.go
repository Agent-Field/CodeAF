package plandb

// Tests for the hold the runtime and a person put on a task subtree: the
// status-independent `paused` flag, and the readiness reads that consult it.
// They live apart from store_test.go so the store's own laws stay where they
// were and this addition's law reads on its own.

import (
	"strings"
	"testing"
)

// Pause is a status-independent hold: a paused task keeps its status, its
// subtree leaves every readiness read whole, and Resume lifts it. ClaimNext
// obeys the same law, because readiness is one definition and dispatch reads
// it.
func TestPlandbCliPauseHoldsPausedSubtreesOffTheFrontier(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store,
		TaskSpec{ID: "p", Title: "Parent", Description: "coordinate"},
		TaskSpec{ID: "c1", Title: "Child one", Description: "work", ParentID: "p"},
		TaskSpec{ID: "c2", Title: "Child two", Description: "work", ParentID: "p"},
		planSpec("free", "Free"),
	)
	// The parent is composite; its two children and the free task are the
	// ready leaves.
	if got := planIDs(store.ReadyLeaves()); len(got) != 3 {
		t.Fatalf("ready leaves = %v, want the two children and the free task", got)
	}
	if _, err := store.Pause("p"); err != nil {
		t.Fatalf("pause parent: %v", err)
	}
	if got := planIDs(store.ReadyLeaves()); len(got) != 1 || got[0] != "free" {
		t.Fatalf("ready leaves under a paused parent = %v, want only free", got)
	}
	// The status is untouched: pause is a flag, not a rung.
	if task := store.Task("p"); !task.Paused || task.Status != StatusReady {
		t.Fatalf("paused parent = %+v, want paused and still ready", task)
	}
	if task := store.Task("c1"); task.Status != StatusReady {
		t.Fatalf("child status = %s, want ready under a paused parent", task.Status)
	}
	// The ready set drops the paused subtree whole rather than calling it
	// blocked: nothing about it is waiting on another task.
	if set := store.ReadySet(); len(set.Runnable) != 1 || set.Runnable[0].ID != "free" || len(set.Blocked) != 0 {
		t.Fatalf("ready set = %+v, want only free runnable and nothing blocked", set)
	}
	// Dispatch reads the same law: with the parent paused, the next claim
	// takes the free task and never a child.
	if claimed, err := store.ClaimNext("w"); err != nil || claimed == nil || claimed.ID != "free" {
		t.Fatalf("claim under a paused parent = %+v (%v), want free", claimed, err)
	}
	if _, err := store.Resume("p"); err != nil {
		t.Fatalf("resume parent: %v", err)
	}
	if store.Task("p").Paused {
		t.Fatal("resume left the parent paused")
	}
	// A leaf hold is independent of the parent's.
	if _, err := store.Pause("c1"); err != nil {
		t.Fatalf("pause child: %v", err)
	}
	if ids := planIDs(store.ReadyLeaves()); len(ids) != 1 || ids[0] != "c2" {
		t.Fatalf("ready leaves with one leaf paused = %v, want only c2", ids)
	}
	// A second pause of an already-paused task changes nothing and is no
	// error, so a caller need not ask first.
	if _, err := store.Pause("c1"); err != nil {
		t.Fatalf("re-pause: %v", err)
	}
	if _, err := store.Resume("c1"); err != nil {
		t.Fatalf("resume child: %v", err)
	}
	// The root is the run itself, and is nobody's to pause.
	if _, err := store.Pause("root"); err == nil || !strings.Contains(err.Error(), "harness") {
		t.Fatalf("pausing the root = %v, want the harness refusal", err)
	}
}

// The CLI verbs a runtime or a person drives: `task pause` and `task resume`.
// The bare lifecycle verb stays the supervisor's, which is the refusal the
// worker page teaches.
func TestPlandbCliPauseAndResumeVerbs(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("Parent", "p")
	h.cliAdd("Child", "c", "--parent", "t-p")

	code := h.run("--db", h.db, "task", "pause", "t-p")
	cliWantCode(t, code, 0)
	if got := h.out.String(); got != "paused t-p\n" {
		t.Fatalf("pause shape %q, want \"paused t-p\\n\"", got)
	}
	// The paused subtree leaves the frontier: nothing is ready to claim.
	code = h.run("--db", h.db, "go")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "nothing ready to claim") {
		t.Fatalf("go found work under a paused parent:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "task", "resume", "t-p")
	cliWantCode(t, code, 0)
	if got := h.out.String(); got != "resumed t-p\n" {
		t.Fatalf("resume shape %q, want \"resumed t-p\\n\"", got)
	}
	code = h.run("--db", h.db, "go", "--agent", "w1")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), `→ t-c "Child"`) {
		t.Fatalf("go after resume did not find the child:\n%s", h.out.String())
	}
	// The bare lifecycle verb is still the supervisor's.
	code = h.run("--db", h.db, "pause")
	cliWantError(t, h, code, "Task lifecycle and scope are managed by the supervisor")
	// A missing task is the one plain miss.
	code = h.run("--db", h.db, "task", "pause", "t-missing")
	cliWantError(t, h, code, "not found: task t-missing")
}
