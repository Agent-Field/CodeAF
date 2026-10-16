package session

// DISMISS HIDES A SETTLED FAILURE AND CHANGES NOTHING ELSE. These tests drive
// the one door every road goes through ([Agent.AppTaskAction]) and then read the
// checkpoint and a reopened conversation, because a hide that a restart brings
// back is not a hide.

import (
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failedConversation reopens a checkpoint holding one settled failure (1), a
// settled failure something live waits on (2) with its waiting dependent (3),
// a finished task (4), a failed task with a live child (5, 6) and a run row that
// failed (7). Nothing is scheduled: the runner counts what it is asked to start.
func failedConversation(t *testing.T) (*Agent, *scriptedCompleter, *atomic.Int64, string) {
	t.Helper()
	// A SESSION FOLDER, so the work graph reads the same tasks.json the engine writes.
	journal := filepath.Join(t.TempDir(), placeTranscript)
	checkpoint := taskCheckpointPath(journal)
	writeCheckpoint(t, checkpoint, taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 12,
		Nodes: []taskRecord{
			{ID: 1, Title: "old failure", Brief: "b", Acceptance: "a", State: TaskFailed, Report: "exit 1"},
			{ID: 2, Title: "prerequisite", Brief: "b", Acceptance: "a", State: TaskFailed, Report: "exit 2"},
			{ID: 3, Title: "waiting dependent", Brief: "b", Acceptance: "a", State: TaskQueued, DependsOn: []uint64{2}},
			{ID: 4, Title: "landed", Brief: "b", Acceptance: "a", State: TaskDone},
			{ID: 5, Title: "parent", Brief: "b", Acceptance: "a", State: TaskFailed},
			{ID: 6, Title: "live child", Brief: "b", Acceptance: "a", State: TaskQueued, Parent: 5, Depth: 1, DependsOn: []uint64{4}},
			{ID: 8, Title: "stopped by you", Brief: "b", Acceptance: "a", State: TaskFailed, Report: "stopped", Ending: TaskEndingStopped},
			{ID: 9, Title: "declined by you", Brief: "b", Acceptance: "a", State: TaskFailed, Report: "Declined by you", App: appTaskRecord{Declined: true}},
			{ID: 11, Title: "settled dependent of 1", Brief: "b", Acceptance: "a", State: TaskFailed, DependsOn: []uint64{1}},
			{ID: 12, Title: "plain failure, nobody waits", Brief: "b", Acceptance: "a", State: TaskFailed, Report: "exit 3"},
			{ID: 10, Title: "failed but held", Brief: "b", Acceptance: "a", State: TaskFailed, App: appTaskRecord{Paused: true}},
		},
		Runs: []runRecord{{ID: 7, Title: "watch places-v1", State: TaskFailed, Report: "exit 1"}},
	})
	completer := &scriptedCompleter{}
	started := new(atomic.Int64)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.InTask, config.SessionFile = true, journal
	})
	graph := agent.graph()
	graph.mu.Lock()
	graph.run = func(*TaskNode) { started.Add(1) }
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
		if started.Load() != 0 || len(completer.seen) != 0 {
			t.Fatalf("hiding and restoring started %d tasks and made %d model calls", started.Load(), len(completer.seen))
		}
	}
}

// WHAT IS NOT A SETTLED FAILURE WITH NOTHING WAITING ON IT IS REFUSED, in words
// that say which, and nothing is written.
func TestDismissRefusesWhatIsNotASafeSettledFailure(t *testing.T) {
	agent, _, _, checkpoint := failedConversation(t)
	cases := map[string]string{
		"2":  "still waits on it",        // something live waits on this failure
		"3":  "is waiting",               // a task that has not finished
		"4":  "it finished",              // landed work
		"5":  "still has",                // a failure with a live child under it
		"8":  "is stopped",               // the person's own stop is kept as it is
		"9":  "is declined",              // so is a declined proposal
		"10": "is not a settled failure", // failed but still on hold: unresolved
		"99": "no such task",
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

// A STOPPED OR DECLINED TASK KEEPS ITS OWN STATE AND RESULT, whatever is asked.
func TestDismissLeavesStoppedAndDeclinedTasksExactlyAsTheyWere(t *testing.T) {
	agent, _, started, checkpoint := failedConversation(t)
	before8, before9 := viewOf(t, agent, "8"), viewOf(t, agent, "9")
	if before8.State != "failed" || before9.State != "declined" {
		t.Fatalf("fixture states = %q / %q", before8.State, before9.State)
	}
	for _, id := range []string{"8", "9"} {
		if err := act(agent, id, "dismiss"); err == nil {
			t.Fatalf("task %s was hidden", id)
		}
	}
	if after := viewOf(t, agent, "8"); after.State != "failed" || after.Result != before8.Result || after.Dismissed {
		t.Fatalf("task 8 = %+v", after)
	}
	if document := readCheckpoint(t, checkpoint); len(document.Dismissed) != 0 || started.Load() != 0 {
		t.Fatalf("dismissed %v, started %d", document.Dismissed, started.Load())
	}
}

// A RETRY THAT IS ADMITTED CLEARS THE HIDE, IN THE SAME LOCKED STEP THAT RE-ARMS
// THE TASK, and the clearing is on disk and across a reopen. A retry that is
// refused clears nothing.
func TestAdmittedRetryClearsTheDismissalAndARefusedRetryKeepsIt(t *testing.T) {
	agent, completer, _, checkpoint := failedConversation(t)
	if err := act(agent, "1", "dismiss"); err != nil {
		t.Fatal(err)
	}
	// REFUSED: the session is closing, so nothing is admitted.
	graph := agent.graph()
	graph.mu.Lock()
	graph.quitting = true
	graph.mu.Unlock()
	if err := agent.RetryTask(1); err == nil {
		t.Fatal("a closing session admitted a retry")
	}
	if !viewOf(t, agent, "1").Dismissed || len(readCheckpoint(t, checkpoint).Dismissed) != 1 {
		t.Fatal("a refused retry changed the dismissal")
	}
	graph.mu.Lock()
	graph.quitting = false
	graph.mu.Unlock()

	// ADMITTED.
	if err := agent.RetryTask(1); err != nil {
		t.Fatalf("retry: %v", err)
	}
	view := viewOf(t, agent, "1")
	if view.Dismissed || view.State == "failed" {
		t.Fatalf("a working task is still hidden or still failed: %+v", view)
	}
	document := readCheckpoint(t, checkpoint)
	if len(document.Dismissed) != 0 {
		t.Fatalf("the cleared set is not on disk: %v", document.Dismissed)
	}
	reopened, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.InTask = true })
	reopened.graph().rehydrate(document, t.TempDir(), TaskSettleAsk)
	if got := viewOf(t, reopened, "1"); got.Dismissed {
		t.Fatalf("a reopened retried task is hidden: %+v", got)
	}
	if len(completer.seen) != 0 {
		t.Fatalf("%d model calls", len(completer.seen))
	}
	// It fails again later and is NOT hidden: the old dismissal belonged to the old failure.
	graph.mu.Lock()
	graph.nodes[1].state = TaskFailed
	graph.mu.Unlock()
	if viewOf(t, agent, "1").Dismissed {
		t.Fatal("a new failure inherited the old dismissal")
	}
}

// A STALE DISMISSAL ON A TASK THAT IS NOT A PLAIN FAILURE IS NEVER SHOWN: whatever road
// re-armed it, visibility follows the state, so a working task is never hidden.
func TestAWorkingTaskIsNeverShownHiddenEvenWithAStaleEntry(t *testing.T) {
	agent, _, _, _ := failedConversation(t)
	graph := agent.graph()
	graph.mu.Lock()
	graph.dismissed = map[string]bool{"3": true, "6": true, "4": true}
	graph.mu.Unlock()
	for _, id := range []string{"3", "6", "4"} {
		if viewOf(t, agent, id).Dismissed {
			t.Fatalf("task %s (%s) is shown hidden", id, viewOf(t, agent, id).State)
		}
	}
	if notice := graph.node(3).notice(); notice.Dismissed {
		t.Fatal("a waiting task's notice says it is hidden")
	}
}

// THE REFUSAL IS ASKED UNDER THE SAME LOCK THAT WRITES THE CHANGE: a read gone stale
// between the answer and the write cannot hide a task that has since been re-armed or
// has since gained a live dependent.
func TestDismissSettlesTheStateAndTheDependentsAtomicallyWithTheWrite(t *testing.T) {
	agent, _, _, checkpoint := failedConversation(t)
	stale, err := agent.AppTasks()
	if err != nil {
		t.Fatal(err)
	}
	// A settled dependent of task 1 is re-armed after the read — under the graph's lock,
	// as every producer does it — so a live task now waits on it.
	graph := agent.graph()
	graph.mu.Lock()
	graph.nodes[11].state, graph.nodes[11].done = TaskQueued, make(chan struct{})
	graph.mu.Unlock()
	if err := agent.appDismissWith(AppTaskIntent{ID: "1", Action: "dismiss"}, stale); err == nil || !strings.Contains(err.Error(), "still waits on it") {
		t.Fatalf("a stale read hid a task a live dependent waits on: %v", err)
	}
	// A retry arrives after the read.
	if err := agent.RetryTask(12); err != nil {
		t.Fatal(err)
	}
	if err := agent.appDismissWith(AppTaskIntent{ID: "12", Action: "dismiss"}, stale); err == nil || !strings.Contains(err.Error(), "not a settled failure") {
		t.Fatalf("a stale read hid a task that is working again: %v", err)
	}
	if document := readCheckpoint(t, checkpoint); len(document.Dismissed) != 0 {
		t.Fatalf("a refused dismissal wrote %v", document.Dismissed)
	}
}

// DISMISS AGAINST RETRY, RUN ON REAL GOROUTINES FROM THE SAME STARTING POINT: whichever
// wins, a task that is not failed is never left in the hidden set.
func TestDismissAndRetryInterleavedNeverLeaveAWorkingTaskHidden(t *testing.T) {
	for i := 0; i < 40; i++ {
		agent, _, _, checkpoint := failedConversation(t)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = act(agent, "1", "dismiss") }()
		go func() { defer wg.Done(); _ = agent.RetryTask(1) }()
		wg.Wait()
		graph := agent.graph()
		graph.mu.Lock()
		state, hidden := graph.nodes[1].state, graph.dismissed["1"]
		graph.mu.Unlock()
		if state != TaskFailed && hidden {
			t.Fatalf("round %d: the task is %s and hidden", i, state)
		}
		for _, id := range readCheckpoint(t, checkpoint).Dismissed {
			if id == "1" && state != TaskFailed {
				t.Fatalf("round %d: the checkpoint hides a %s task", i, state)
			}
		}
	}
}

// A PLAN ROW'S DEPENDENTS ARE FILED IN ITS OWN STORE: one that arrives between the
// decision and the write is caught by the read after it, and the dismissal is put back.
func TestAPlanRowThatGainsADependentDuringDismissalIsPutBack(t *testing.T) {
	folder := filepath.Join(t.TempDir(), sourceOwner)
	place := Place{Dir: folder}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile, config.Place = place.Transcript(), place
	})
	path := filepath.Join(folder, planStoreFilename)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(path, "the run", planRootID, "The run", "drive", sourceOwner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "bad", Title: "fails"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim("bad", "w"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Fail("bad", "w", "exit 1"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	armPlanStore(t, agent, path, sourceOwner)
	dismissInterleave = func() {
		s, err := plandb.Open(path, "the run", planRootID, "The run", "drive", sourceOwner)
		if err != nil {
			t.Error(err)
			return
		}
		defer s.Close()
		if _, err := s.AddMany([]plandb.TaskSpec{{ID: "late", Title: "arrives mid-dismissal", Dependencies: []plandb.Dependency{{TaskID: "bad", Kind: plandb.DepBlocks}}}}); err != nil {
			t.Error(err)
		}
	}
	defer func() { dismissInterleave = nil }()
	if err := act(agent, "t-bad", "dismiss"); err == nil || !strings.Contains(err.Error(), "still waits on it") {
		t.Fatalf("dismissal = %v, want it refused for the dependent that arrived", err)
	}
	if viewOf(t, agent, "t-bad").Dismissed || agent.graph().isDismissed("t-bad") {
		t.Fatal("the plan row stayed hidden")
	}
}
