package plandb

import (
	"testing"
)

func TestDoneRefusesAlreadyTerminalTaskExplicitly(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, TaskSpec{ID: "leaf", Title: "cancelled leaf"})
	if _, err := store.Cancel("leaf", "cancelled by supervisor"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	_, err := store.Done("leaf", "worker1", "all finished", nil, nil)
	if err == nil {
		t.Fatalf("expected error finishing cancelled task, got nil")
	}
	if want := `task "leaf" is already terminal (cancelled)`; err.Error() != want {
		t.Fatalf("got error %q, want %q", err.Error(), want)
	}
}
