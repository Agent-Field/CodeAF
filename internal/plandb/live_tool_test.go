package plandb

import (
	"path/filepath"
	"testing"
)

// A LIVE STEP KEEPS THE TOOL IT IS RUNNING, and one published without a tool
// reads back with none — the bash step every older writer publishes.
func TestLiveStepKeepsItsTool(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "plan.db"), "live", "root", "Live", "keep the tool")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetLiveCall(store.RootID(), 2, "script", `{"source":"return 1"}`); err != nil {
		t.Fatal(err)
	}
	if live := store.Live(store.RootID()); live.Tool != "script" || live.Step != 2 {
		t.Errorf("live = %#v, want step 2 running script", live)
	}
	if err := store.SetLive(store.RootID(), 3, "go test ./..."); err != nil {
		t.Fatal(err)
	}
	if live := store.Live(store.RootID()); live.Tool != "" || live.Command != "go test ./..." {
		t.Errorf("live = %#v, want a bash step with no tool", live)
	}
}
