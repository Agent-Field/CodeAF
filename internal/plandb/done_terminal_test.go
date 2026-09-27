package plandb

import (
	"reflect"
	"testing"
)

func TestDoneRefusesAlreadyTerminalTaskExplicitly(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, TaskSpec{ID: "leaf", Title: "cancelled leaf"})
	if _, err := store.Cancel("leaf", "cancelled by supervisor"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	before := *store.Task("leaf")
	_, err := store.Done("leaf", "worker1", "all finished", nil, nil)
	if err == nil {
		t.Fatalf("expected error finishing cancelled task, got nil")
	}
	if want := `task "leaf" is already terminal (cancelled)`; err.Error() != want {
		t.Fatalf("got error %q, want %q", err.Error(), want)
	}
	if after := *store.Task("leaf"); !reflect.DeepEqual(before, after) {
		t.Fatalf("refused completion mutated task: before=%+v after=%+v", before, after)
	}
}

func TestDoneCancelledCheckReportsTerminalBeforeReviewValidation(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, TaskSpec{ID: "check", Title: "review", Role: RoleCheck})
	if _, err := store.Cancel("check", "stopped"); err != nil {
		t.Fatal(err)
	}
	_, err := store.Done("check", "worker", "holds: finished", nil, nil)
	if err == nil || err.Error() != `task "check" is already terminal (cancelled)` {
		t.Fatalf("cancelled review completion: %v", err)
	}
}
