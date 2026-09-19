package plandb

import (
	"strings"
	"testing"
)

const parkedFinishGuidance = "waiting and can only be finished after it is woken"

func requireParkedFinishRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), parkedFinishGuidance) {
		t.Fatalf("parked finish error = %v, want actionable %q refusal", err, parkedFinishGuidance)
	}
}

func TestDoneRefusesParkedTaskUntilWake(t *testing.T) {
	t.Run("root worker late Done", func(t *testing.T) {
		store := planOpen(t, "")
		root := store.RootID()
		planAdd(t, store, TaskSpec{ID: "child", Title: "child"})
		if _, err := store.Wait(root, root); err != nil {
			t.Fatalf("Wait root: %v", err)
		}
		planFinish(t, store, "child", "child", "done")
		requireParkedFinishRefusal(t, func() error { _, err := store.Done(root, root, "late", nil, nil); return err }())
		got := store.Task(root)
		if !got.Waiting || terminal(got.Status) {
			t.Fatalf("refused root = status %s waiting %v", got.Status, got.Waiting)
		}
	})

	t.Run("ordinary leaf late Done", func(t *testing.T) {
		store := planOpen(t, "")
		planAdd(t, store,
			TaskSpec{ID: "dep", Title: "dependency"},
			TaskSpec{ID: "leaf", Title: "leaf", Dependencies: []Dependency{{TaskID: "dep", Kind: DepSuggests}}},
		)
		if _, err := store.Claim("leaf", "leaf"); err != nil {
			t.Fatalf("Claim leaf: %v", err)
		}
		if _, err := store.Wait("leaf", "leaf"); err != nil {
			t.Fatalf("Wait leaf: %v", err)
		}
		planFinish(t, store, "dep", "dep", "done")
		requireParkedFinishRefusal(t, func() error { _, err := store.Done("leaf", "leaf", "late", nil, nil); return err }())
		got := store.Task("leaf")
		if !got.Waiting || terminal(got.Status) {
			t.Fatalf("refused leaf = status %s waiting %v", got.Status, got.Waiting)
		}

		if _, err := store.Wake("leaf"); err != nil {
			t.Fatalf("Wake leaf: %v", err)
		}
		if _, err := store.Claim("leaf", "leaf"); err != nil {
			t.Fatalf("Claim woken leaf: %v", err)
		}
		if _, err := store.Done("leaf", "leaf", "after wake", nil, nil); err != nil {
			t.Fatalf("Done after Wake: %v", err)
		}
	})
}

func TestFinishRoadsNeverLeaveDoneAndWaiting(t *testing.T) {
	assertLaw := func(t *testing.T, store *Store) {
		t.Helper()
		for _, task := range store.Tasks() {
			if task.Status == StatusDone && task.Waiting {
				t.Fatalf("task %q is done and waiting", task.ID)
			}
		}
	}
	t.Run("Done never parked", func(t *testing.T) {
		store := planOpen(t, "")
		planAdd(t, store, TaskSpec{ID: "leaf", Title: "leaf"})
		planFinish(t, store, "leaf", "leaf", "done")
		if got := store.Task("leaf"); got.Status != StatusDone {
			t.Fatalf("never-parked leaf = %s, want done", got.Status)
		}
		assertLaw(t, store)
	})
	t.Run("Done after Wake", func(t *testing.T) {
		store := planOpen(t, "")
		planAdd(t, store, TaskSpec{ID: "dep", Title: "dep"}, TaskSpec{ID: "leaf", Title: "leaf", Dependencies: []Dependency{{TaskID: "dep", Kind: DepSuggests}}})
		if _, err := store.Claim("leaf", "leaf"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Wait("leaf", "leaf"); err != nil {
			t.Fatal(err)
		}
		planFinish(t, store, "dep", "dep", "done")
		if _, err := store.Wake("leaf"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Claim("leaf", "leaf"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Done("leaf", "leaf", "done", nil, nil); err != nil {
			t.Fatalf("Done woken leaf: %v", err)
		}
		assertLaw(t, store)
	})
	t.Run("composite auto completion", func(t *testing.T) {
		store := planOpen(t, "")
		planAdd(t, store, TaskSpec{ID: "parent", Title: "parent"}, TaskSpec{ID: "child", Title: "child", ParentID: "parent"})
		planFinish(t, store, "child", "child", "done")
		if got := store.Task("parent"); got.Status != StatusDone {
			t.Fatalf("auto-completed parent = %s, want done", got.Status)
		}
		assertLaw(t, store)
	})
	t.Run("CompleteRoot never parked", func(t *testing.T) {
		store := planOpen(t, "")
		if err := store.CompleteRoot("done"); err != nil {
			t.Fatalf("CompleteRoot: %v", err)
		}
		if got := store.Task(store.RootID()); got.Status != StatusDone || got.Result != "done" {
			t.Fatalf("completed root = %s %q", got.Status, got.Result)
		}
		assertLaw(t, store)
	})
	t.Run("CompleteRoot parked until Wake", func(t *testing.T) {
		store := planOpen(t, "")
		root := store.RootID()
		planAdd(t, store, TaskSpec{ID: "child", Title: "child"})
		if _, err := store.Wait(root, root); err != nil {
			t.Fatalf("Wait root: %v", err)
		}
		planFinish(t, store, "child", "child", "done")
		requireParkedFinishRefusal(t, store.CompleteRoot("late"))
		if got := store.Task(root); !got.Waiting || terminal(got.Status) || got.Result != "" {
			t.Fatalf("refused CompleteRoot = status %s waiting %v result %q", got.Status, got.Waiting, got.Result)
		}
		if _, err := store.Wake(root); err != nil {
			t.Fatalf("Wake root: %v", err)
		}
		if err := store.CompleteRoot("after wake"); err != nil {
			t.Fatalf("CompleteRoot after Wake: %v", err)
		}
		if got := store.Task(root); got.Status != StatusDone || got.Result != "after wake" {
			t.Fatalf("completed root = %s %q", got.Status, got.Result)
		}
		assertLaw(t, store)
	})
}

func TestFailRefusesParkedTaskUntilWake(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, TaskSpec{ID: "dep", Title: "dep"}, TaskSpec{ID: "leaf", Title: "leaf", Dependencies: []Dependency{{TaskID: "dep", Kind: DepSuggests}}})
	if _, err := store.Claim("leaf", "leaf"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Wait("leaf", "leaf"); err != nil {
		t.Fatal(err)
	}
	requireParkedFinishRefusal(t, func() error { _, err := store.Fail("leaf", "leaf", "late failure"); return err }())
	if got := store.Task("leaf"); !got.Waiting || terminal(got.Status) {
		t.Fatalf("refused Fail = status %s waiting %v", got.Status, got.Waiting)
	}
	if _, err := store.Wake("leaf"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim("leaf", "leaf"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Fail("leaf", "leaf", "after wake"); err != nil {
		t.Fatalf("Fail after Wake: %v", err)
	}
}
