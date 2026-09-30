package session

// A RUN IS WORK, AND ITS LIFE IS THE CONVERSATION'S.
//
// A run's rows are kept beside the task graph rather than in it, so the walk
// over the graph's own nodes never saw one and a conversation whose only live
// work was a run answered that it was working on nothing. The engine retires an
// idle conversation, and it asks this door what idle means, so that answer was
// a run whose room was taken down from under it while its workers were out.
//
// The other half of the same fact: the run holds a context no turn cancels, on
// purpose, and until the conversation could reach that context the run's life
// belonged to neither the turn nor the room but to the process.

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// beltRunOnAgent puts one live run on a conversation, as the road that starts
// one does ([Agent.startKnownTaskRun]): a real store, the row the surface knows
// it by, and a context of its own to be cut.
func beltRunOnAgent(t *testing.T, agent *Agent, row uint64, title string, born time.Time) (*beltRun, context.Context) {
	t.Helper()
	path := filepath.Join(t.TempDir(), planStoreFilename)
	store, err := plandb.Open(path, "run", planRootID, title, "person ask")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cut := context.WithCancel(context.Background())
	run := &beltRun{
		store: store, root: store.RootID(), row: row, title: title,
		cut: cut, born: born,
	}
	agent.beltMu.Lock()
	agent.beltRun = run
	agent.beltMu.Unlock()
	return run, ctx
}

func TestAConversationDrivingARunIsWorking(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	born := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	beltRunOnAgent(t, agent, 7, "repair the landing", born)

	tree := agent.WorkingNow()
	if len(tree) != 1 {
		t.Fatalf("a conversation driving a run says it is working on %d things, want the run", len(tree))
	}
	row := tree[0]
	if row.State != WorkRunning {
		t.Fatalf("the run's row reads %v, want it running", row.State)
	}
	if row.Title != "repair the landing" {
		t.Fatalf("the run's row is called %q", row.Title)
	}
	if !row.Born.Equal(born) {
		t.Fatalf("the run's row was born %v, want the clock's own %v", row.Born, born)
	}
	// AND THE ID IS ONE A PERSON CAN ACT ON, which is the row's number: it is
	// what the stop road already resolves to this run's owner (stoprun.go), so
	// a surface handing this id back does not need to know a run from a task.
	if row.ID != CancelTask+":7" {
		t.Fatalf("the run's row is %q, want the number every other surface knows it by", row.ID)
	}
	if n := CountWorking(agent.WorkingNow()); n != 1 {
		t.Fatalf("a conversation driving a run counts %d workers", n)
	}
}

// AND THIS IS THE READING THAT KEEPS THE ROOM OPEN. The engine asks the same
// door before it retires a conversation for being idle, so a run that did not
// appear above was a run retired out from under.
func TestAConversationDrivingARunIsNotIdle(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if len(agent.WorkingNow()) != 0 {
		t.Fatal("the conversation was not idle before the run started")
	}
	beltRunOnAgent(t, agent, 3, "the run", time.Now())
	if len(agent.WorkingNow()) == 0 {
		t.Fatal("a conversation whose only live work is a run reads idle, which is what retires it mid-run")
	}
}

// A RUN A PERSON STOPPED IS STILL LIVE UNTIL IT SETTLES. Its workers are out
// until the run's own road takes it off the conversation, and "is anything
// happening" is a question about them, not about who asked for it to end.
func TestARunAPersonStoppedIsStillWorkUntilItSettles(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	run, _ := beltRunOnAgent(t, agent, 4, "the run", time.Now())
	agent.beltMu.Lock()
	run.stopped, run.stopReason = true, "changed my mind"
	agent.beltMu.Unlock()

	if len(agent.WorkingNow()) != 1 {
		t.Fatal("a run that is stopping stopped counting as work before its workers were home")
	}

	agent.beltMu.Lock()
	agent.beltRun = nil
	agent.beltMu.Unlock()
	if nodes := agent.WorkingNow(); len(nodes) != 0 {
		t.Fatalf("the run left the conversation and still draws %d rows", len(nodes))
	}
}

// THE ROOM CLOSING ENDS THE RUN. The run's context outlives the turn that
// proposed it on purpose; what it must not outlive is the conversation, or its
// workers go on spending against a room nobody can read or stop.
func TestClosingTheConversationEndsTheRun(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	_, ctx := beltRunOnAgent(t, agent, 5, "the run", time.Now())

	select {
	case <-ctx.Done():
		t.Fatal("the run's context was already cut before the conversation closed")
	default:
	}

	if err := agent.Close(); err != nil {
		t.Fatalf("closing the conversation: %v", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the conversation closed and the run was left running against a room nobody can read")
	}
}

// AND IT IS NOT A PERSON'S STOP. A stop writes the person's reason on the
// store's root and settles the row in their words; the room closing says
// nothing, because nobody asked for anything.
func TestClosingTheConversationWritesNoStopOnTheRun(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	run, _ := beltRunOnAgent(t, agent, 6, "the run", time.Now())

	if err := agent.Close(); err != nil {
		t.Fatalf("closing the conversation: %v", err)
	}

	agent.beltMu.Lock()
	stopped, why := run.stopped, run.stopReason
	agent.beltMu.Unlock()
	if stopped || why != "" {
		t.Fatalf("the room closing was recorded as a person's stop (%v, %q)", stopped, why)
	}
	if task := run.store.Task(run.root); task == nil || task.Status == plandb.StatusCancelled {
		t.Fatal("the room closing wrote a stop on the run's own record")
	}
}

// A NOTE ON A RUN'S ROW SAYS WHEN THE WORKER READS IT, in the task room's own
// sentence. The row has no worker to splice a line into, so the words are a
// note, and the receipt is when that note is read.
func TestANoteOnARunRowSaysWhenTheWorkerReadsIt(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	const row uint64 = 7
	path := filepath.Join(t.TempDir(), planStoreFilename)
	store, err := plandb.Open(path, "run", strconv.FormatUint(row, 10), "the run", "person ask")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, cut := context.WithCancel(context.Background())
	t.Cleanup(cut)
	agent.beltMu.Lock()
	agent.beltRun = &beltRun{store: store, root: store.RootID(), row: row, title: "the run", cut: cut}
	agent.beltMu.Unlock()

	receipt, err := agent.SteerTask(row, "keep the middleware order")
	if err != nil {
		t.Fatalf("a note on a run row: %v", err)
	}
	const want = "the worker reads a note at its next step"
	if receipt.Landing != want {
		t.Fatalf("a note on a run row answered %q, want %q", receipt.Landing, want)
	}
}
