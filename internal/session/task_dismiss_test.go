package session

// DISMISS HIDES A SETTLED FAILURE AND CHANGES NOTHING ELSE. These tests drive
// the one door every road goes through ([Agent.AppTaskAction]) and then read the
// checkpoint and a reopened conversation, because a hide that a restart brings
// back is not a hide.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failedConversation reopens a checkpoint holding one settled failure (1), a
// settled failure something live waits on (2) with its waiting dependent (3),
// a finished task (4), a failed task with a live child (5, 6) and a run row that
// failed (7). Nothing is scheduled: the runner counts what it is asked to start.
func failedConversation(t *testing.T) (*Agent, *scriptedCompleter, *int, string) {
	t.Helper()
	// A SESSION FOLDER, so the work graph reads the same tasks.json the engine writes.
	journal := filepath.Join(t.TempDir(), placeTranscript)
	checkpoint := taskCheckpointPath(journal)
	writeCheckpoint(t, checkpoint, taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 7,
		Nodes: []taskRecord{
			{ID: 1, Title: "old failure", Brief: "b", Acceptance: "a", State: TaskFailed, Report: "exit 1"},
			{ID: 2, Title: "prerequisite", Brief: "b", Acceptance: "a", State: TaskFailed, Report: "exit 2"},
			{ID: 3, Title: "waiting dependent", Brief: "b", Acceptance: "a", State: TaskQueued, DependsOn: []uint64{2}},
			{ID: 4, Title: "landed", Brief: "b", Acceptance: "a", State: TaskDone},
			{ID: 5, Title: "parent", Brief: "b", Acceptance: "a", State: TaskFailed},
			{ID: 6, Title: "live child", Brief: "b", Acceptance: "a", State: TaskQueued, Parent: 5, Depth: 1, DependsOn: []uint64{4}},
		},
		Runs: []runRecord{{ID: 7, Title: "watch places-v1", State: TaskFailed, Report: "exit 1"}},
	})
	completer := &scriptedCompleter{}
	started := new(int)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.InTask, config.SessionFile = true, journal
	})
	graph := agent.graph()
	graph.mu.Lock()
	graph.run = func(*TaskNode) { *started++ }
	graph.mu.Unlock()
	document, ok := loadTaskCheckpoint(checkpoint)
	if !ok {
		t.Fatal("no checkpoint")
	}
	graph.rehydrate(document, t.TempDir(), TaskSettleAsk)
	return agent, completer, started, checkpoint
}

func viewOf(t *testing.T, agent *Agent, id string) AppTaskView {
	t.Helper()
	view, found, err := agent.AppTask(id)
	if err != nil || !found {
		t.Fatalf("task %s: found=%v err=%v", id, found, err)
	}
	return view
}

func act(agent *Agent, id, action string) error {
	return agent.AppTaskAction(AppTaskIntent{ID: id, Action: action})
}

// DISMISS WRITES THE CHECKPOINT, KEEPS THE FAILURE WHOLE, AND A REOPEN AGREES;
// RESTORE IS EXPLICIT AND ALSO PERSISTED; NOTHING IS DISPATCHED OR ASKED OF A MODEL.
func TestDismissHidesAFailureReversiblyAndPersistsBothWays(t *testing.T) {
	for _, id := range []string{"1", "7"} { // a graph node and a run row
		agent, completer, started, checkpoint := failedConversation(t)
		before := viewOf(t, agent, id)
		if before.Dismissed || before.State != "failed" {
			t.Fatalf("%s before = %+v", id, before)
		}
		if err := act(agent, id, "dismiss"); err != nil {
			t.Fatalf("dismiss %s: %v", id, err)
		}
		after := viewOf(t, agent, id)
		if !after.Dismissed || after.State != before.State || after.Result != before.Result || after.Title != before.Title {
			t.Fatalf("%s after dismiss = %+v, want the same failure, hidden", id, after)
		}
		document := readCheckpoint(t, checkpoint)
		if len(document.Dismissed) != 1 || document.Dismissed[0] != id {
			t.Fatalf("the checkpoint holds %v, want [%s]", document.Dismissed, id)
		}
		// A reopen agrees, with the failure still a failure.
		reopened, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.InTask = true })
		reopened.graph().rehydrate(document, t.TempDir(), TaskSettleAsk)
		if got := viewOf(t, reopened, id); !got.Dismissed || got.State != "failed" {
			t.Fatalf("%s reopened = %+v", id, got)
		}
		// Dismissing twice is not an error and writes nothing new.
		if err := act(agent, id, "dismiss"); err != nil {
			t.Fatalf("a repeated dismiss: %v", err)
		}
		// Restore is the explicit undo, persisted too.
		if err := act(agent, id, "restore"); err != nil {
			t.Fatalf("restore %s: %v", id, err)
		}
		if got := viewOf(t, agent, id); got.Dismissed || got.State != "failed" {
			t.Fatalf("%s after restore = %+v", id, got)
		}
		if document := readCheckpoint(t, checkpoint); len(document.Dismissed) != 0 {
			t.Fatalf("restore left %v on disk", document.Dismissed)
		}
		if err := act(agent, id, "restore"); err != nil {
			t.Fatalf("restoring what is not hidden: %v", err)
		}
		if *started != 0 || len(completer.seen) != 0 {
			t.Fatalf("hiding and restoring started %d tasks and made %d model calls", *started, len(completer.seen))
		}
	}
}

// WHAT IS NOT A SETTLED FAILURE WITH NOTHING WAITING ON IT IS REFUSED, in words
// that say which, and nothing is written.
func TestDismissRefusesWhatIsNotASafeSettledFailure(t *testing.T) {
	agent, _, _, checkpoint := failedConversation(t)
	cases := map[string]string{
		"2": "still waits on it", // something live waits on this failure
		"3": "is waiting",        // a task that has not finished
		"4": "it finished",       // landed work
		"5": "still has",         // a failure with a live child under it
		"9": "no such task",
	}
	for id, want := range cases {
		err := act(agent, id, "dismiss")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("dismiss %s = %v, want a refusal containing %q", id, err, want)
		}
	}
	if document := readCheckpoint(t, checkpoint); len(document.Dismissed) != 0 {
		t.Fatalf("a refusal wrote %v", document.Dismissed)
	}
	if viewOf(t, agent, "2").Dismissed {
		t.Fatal("a refused dismiss hid the task")
	}
}

// A FAILED WRITE ROLLS BACK: no phantom success, no half-hidden task.
func TestDismissRollsBackWhenTheCheckpointCannotBeWritten(t *testing.T) {
	agent, _, _, checkpoint := failedConversation(t)
	// A directory where the temporary file must go makes every write fail.
	if err := os.MkdirAll(checkpoint+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := act(agent, "1", "dismiss"); err == nil {
		t.Fatal("a dismiss that could not be saved said it worked")
	}
	if viewOf(t, agent, "1").Dismissed {
		t.Fatal("the task stayed hidden after its write failed")
	}
	if err := os.Remove(checkpoint + ".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := act(agent, "1", "dismiss"); err != nil {
		t.Fatalf("the retry after the disk recovered: %v", err)
	}
	// And a restore whose write fails keeps the task hidden.
	if err := os.MkdirAll(checkpoint+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := act(agent, "1", "restore"); err == nil || !viewOf(t, agent, "1").Dismissed {
		t.Fatalf("a restore that could not be saved changed the task: %v", err)
	}
}

// A DISMISSED TASK IS STILL LISTED (the app filters), and the work graph says
// both the hidden task and how many are hidden; the graph's revision moves.
func TestDismissIsReadBackOnTheWorkGraph(t *testing.T) {
	agent, _, _, checkpoint := failedConversation(t)
	source := NewWorkSource()
	read := func() OwnerFacts {
		facts, err := source.readOwner(t.Context(), "conv", filepath.Dir(checkpoint), wgStamp(checkpoint))
		if err != nil {
			t.Fatal(err)
		}
		return *facts
	}
	hidden := func(f OwnerFacts) map[string]bool {
		out := map[string]bool{}
		for _, fact := range f.Facts {
			if fact.Node.Dismissed {
				out[fact.Node.Ref.Task] = true
			}
		}
		return out
	}
	if got := hidden(read()); len(got) != 0 {
		t.Fatalf("before: %v", got)
	}
	stampBefore := wgStamp(checkpoint)
	for _, id := range []string{"1", "7"} {
		if err := act(agent, id, "dismiss"); err != nil {
			t.Fatal(err)
		}
	}
	if wgStamp(checkpoint) == stampBefore {
		t.Fatal("the checkpoint stamp (the work graph's revision input) did not move")
	}
	if got := hidden(read()); !got["1"] || !got["7"] || len(got) != 2 {
		t.Fatalf("the work graph hides %v, want 1 and 7", got)
	}
	for _, fact := range read().Facts {
		if fact.Node.Ref.Task == "1" && fact.Node.State != "failed" {
			t.Fatalf("hiding changed the state to %q", fact.Node.State)
		}
	}
}
