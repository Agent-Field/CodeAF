package run

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A PLAN WRITE A SCRIPT MADE IS THE SCRIPT STEP'S WRITE. The step's command is
// the script's source, so the plandb verbs its bash calls ran are read off them.
func TestScriptStepRecordsThePlanWritesOfItsCalls(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "script", "root", "Script", "Run a script")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root, task := filepath.Dir(store.Path()), store.RootID()
	rec := stepRecorder{store: store, storeDir: root, taskID: task, children: childrenOf(store, task)}
	rec.nest(session.Event{Kind: session.EventToolEnd, Tool: "bash", Parent: "s", Args: `{"command":"plandb note 'found it'"}`})
	rec.nest(session.Event{Kind: session.EventToolEnd, Tool: "bash", Parent: "s", Args: `{"command":"plandb add 'next'"}`})
	if err := rec.record(1, session.Event{Kind: session.EventToolEnd, Tool: "script", CallID: "s", Args: `{"source":"return 1"}`}); err != nil {
		t.Fatal(err)
	}
	steps, err := Trajectory(root, task)
	if err != nil || len(steps) != 1 {
		t.Fatalf("steps = %+v, err = %v", steps, err)
	}
	got := map[string]bool{}
	for _, verb := range steps[0].Writes {
		got[verb] = true
	}
	if !got["note"] || !got["add"] {
		t.Errorf("writes = %v; want the script's plandb note and add", steps[0].Writes)
	}
}

// A SCRIPT'S SOURCE IS NOT A PLAN WRITE. Words in it that name a plandb verb
// ran nothing; only its bash calls' commands are read.
func TestScriptSourceWordsAreNotPlanWrites(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "script", "root", "Script", "Run a script")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root, task := filepath.Dir(store.Path()), store.RootID()
	rec := stepRecorder{store: store, storeDir: root, taskID: task, children: childrenOf(store, task)}
	source := `{"source":"// then plandb done\ntext(\"run plandb add next\")"}`
	if err := rec.record(1, session.Event{Kind: session.EventToolEnd, Tool: "script", CallID: "s", Args: source}); err != nil {
		t.Fatal(err)
	}
	steps, err := Trajectory(root, task)
	if err != nil || len(steps) != 1 {
		t.Fatalf("steps = %+v, err = %v", steps, err)
	}
	if len(steps[0].Writes) != 0 {
		t.Errorf("writes = %v; a script's source ran no plandb command", steps[0].Writes)
	}
}
