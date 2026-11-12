package session

// WAITING ON ANOTHER CONVERSATION'S TASK: the link is stored, survives a reopen,
// is enforced by the frontier (not merely drawn), and the frontier never reads
// another conversation under a lock.

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeElsewhere is a reader over a table of other conversations' tasks. Every
// ask is counted, and `gate`, when set, blocks each ask until it is closed.
type fakeElsewhere struct {
	mu    sync.Mutex
	tasks map[TaskRef]ExternalTask
	asks  int
	gate  chan struct{}
}

func (f *fakeElsewhere) read(ctx context.Context, ref TaskRef) (ExternalTask, error) {
	f.mu.Lock()
	f.asks++
	gate := f.gate
	got, ok := f.tasks[ref]
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ExternalTask{}, ctx.Err()
		}
	}
	if !ok {
		return ExternalTask{}, nil
	}
	return got, nil
}

func (f *fakeElsewhere) set(ref TaskRef, task ExternalTask) {
	f.mu.Lock()
	f.tasks[ref] = task
	f.mu.Unlock()
}

func (f *fakeElsewhere) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asks
}

// waiterGraph is a conversation whose task 1 is held running (so task 2, which
// depends_on it, is queued) and whose reader is the fake. It returns the agent,
// the graph, the journal's checkpoint path and a release for task 1.
func waiterGraph(t *testing.T, elsewhere *fakeElsewhere) (*Agent, *TaskGraph, string, func()) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "aaaaaaaaaaaaaaaa")
	journal := filepath.Join(dir, placeTranscript)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
		config.Place = Place{Dir: dir}
		config.ExternalTasks = elsewhere.read
	})
	graph := agent.graph()
	release := make(chan struct{})
	var once sync.Once
	graph.mu.Lock()
	graph.ext.every = 10 * time.Millisecond
	graph.run = func(node *TaskNode) {
		if node.id == 1 {
			<-release
		}
		node.finish("done", nil, "", mergeMerged)
		node.graph.complete(node, TaskDone)
	}
	graph.mu.Unlock()
	one := graph.reserve()
	graph.admit(one, taskSpec{title: "first", summary: "s", brief: "b", acceptance: "a"})
	two := graph.reserve()
	graph.admit(two, taskSpec{title: "second", summary: "s", brief: "b", acceptance: "a", dependsOn: []uint64{one}})
	return agent, graph, taskCheckpointPath(journal), func() { once.Do(func() { close(release) }) }
}

func stateOf(g *TaskGraph, id uint64) TaskState {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.nodes[id].state
}

func awaitState(t *testing.T, g *TaskGraph, id uint64, want TaskState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for stateOf(g, id) != want {
		if time.Now().After(deadline) {
			t.Fatalf("task %d is %q, want %q", id, stateOf(g, id), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

var elsewhereTask = TaskRef{Session: "bbbbbbbbbbbbbbbb", ID: 7}

// THE LINK IS ENFORCED, NOT DRAWN: the dependent stays queued after its local
// prerequisite is done, until the other conversation's task is done.
func TestAWaitOnAnotherConversationsTaskHoldsTheTaskUntilItIsDone(t *testing.T) {
	elsewhere := &fakeElsewhere{tasks: map[TaskRef]ExternalTask{elsewhereTask: {Exists: true, State: TaskRunning}}}
	agent, graph, checkpoint, release := waiterGraph(t, elsewhere)
	if err := agent.WaitOnTask(context.Background(), 2, elsewhereTask); err != nil {
		t.Fatal(err)
	}
	// Persisted at once, before anything could have run.
	record := recordOf(t, readCheckpoint(t, checkpoint), 2)
	if len(record.WaitsExternal) != 1 || record.WaitsExternal[0].Session != elsewhereTask.Session || record.WaitsExternal[0].Task != 7 {
		t.Fatalf("the link did not reach the checkpoint: %+v", record.WaitsExternal)
	}
	if record.AdmittedAt.IsZero() || record.StateAt.IsZero() {
		t.Fatalf("a new record carries its own clock: %+v", record)
	}
	release()
	awaitState(t, graph, 1, TaskDone)
	// Give the poll several beats: the prerequisite elsewhere is still running.
	time.Sleep(80 * time.Millisecond)
	if got := stateOf(graph, 2); got != TaskQueued {
		t.Fatalf("task 2 is %q while its prerequisite elsewhere runs", got)
	}
	elsewhere.set(elsewhereTask, ExternalTask{Exists: true, State: TaskDone})
	awaitState(t, graph, 2, TaskDone)
}

func TestAWaitOnATaskThatFailedElsewhereFailsTheDependent(t *testing.T) {
	elsewhere := &fakeElsewhere{tasks: map[TaskRef]ExternalTask{elsewhereTask: {Exists: true, State: TaskRunning}}}
	agent, graph, _, release := waiterGraph(t, elsewhere)
	if err := agent.WaitOnTask(context.Background(), 2, elsewhereTask); err != nil {
		t.Fatal(err)
	}
	release()
	elsewhere.set(elsewhereTask, ExternalTask{Exists: true, State: TaskFailed})
	awaitState(t, graph, 2, TaskFailed)
	graph.mu.Lock()
	report := graph.nodes[2].report
	graph.mu.Unlock()
	if report == "" {
		t.Fatal("a failed dependent says why")
	}
}

// A reopen reads the link back, validated, and the restored node still waits.
func TestAWaitSurvivesAReopen(t *testing.T) {
	elsewhere := &fakeElsewhere{tasks: map[TaskRef]ExternalTask{elsewhereTask: {Exists: true, State: TaskRunning}}}
	agent, _, checkpoint, release := waiterGraph(t, elsewhere)
	defer release()
	if err := agent.WaitOnTask(context.Background(), 2, elsewhereTask); err != nil {
		t.Fatal(err)
	}
	restored := restoreNode(newTaskGraph(), recordOf(t, readCheckpoint(t, checkpoint), 2))
	if len(restored.waitsExt) != 1 || restored.waitsExt[0] != elsewhereTask {
		t.Fatalf("restored waits = %+v", restored.waitsExt)
	}
	// A hand-edited file never makes the frontier open an unsafe name.
	bad := taskRecord{ID: 3, WaitsExternal: []taskWaitRecord{{Session: "../etc", Task: 1}, {Session: "ok", Task: 0}, {Session: "a:b", Task: 1}}}
	if got := waitRefs(bad.WaitsExternal); len(got) != 0 {
		t.Fatalf("unsafe refs were kept: %+v", got)
	}
}

func TestWaitOnTaskRefusals(t *testing.T) {
	elsewhere := &fakeElsewhere{tasks: map[TaskRef]ExternalTask{
		elsewhereTask: {Exists: true, State: TaskRunning},
		// The other conversation's task 9 waits on our task 2: waiting on it from
		// task 2 would close a loop.
		{Session: "bbbbbbbbbbbbbbbb", ID: 9}: {Exists: true, State: TaskQueued, Waits: []TaskRef{{Session: "aaaaaaaaaaaaaaaa", ID: 2}}},
	}}
	agent, _, _, release := waiterGraph(t, elsewhere)
	defer release()
	ctx := context.Background()
	for name, test := range map[string]struct {
		id   uint64
		on   TaskRef
		want error
	}{
		"invalid session":   {2, TaskRef{Session: "../x", ID: 1}, ErrWaitInvalid},
		"zero id":           {2, TaskRef{Session: "bbbbbbbbbbbbbbbb"}, ErrWaitInvalid},
		"same conversation": {2, TaskRef{Session: "aaaaaaaaaaaaaaaa", ID: 1}, ErrWaitSameSession},
		"itself":            {2, TaskRef{Session: "aaaaaaaaaaaaaaaa", ID: 2}, ErrWaitSelf},
		"started task":      {1, elsewhereTask, ErrWaitNotQueued},
		"no such task":      {99, elsewhereTask, ErrWaitNoTask},
		"missing target":    {2, TaskRef{Session: "bbbbbbbbbbbbbbbb", ID: 123}, ErrWaitMissing},
		"loop":              {2, TaskRef{Session: "bbbbbbbbbbbbbbbb", ID: 9}, ErrWaitCycle},
	} {
		if err := agent.WaitOnTask(ctx, test.id, test.on); !errors.Is(err, test.want) {
			t.Errorf("%s: err = %v, want %v", name, err, test.want)
		}
	}
	if err := agent.WaitOnTask(ctx, 2, elsewhereTask); err != nil {
		t.Fatal(err)
	}
	if err := agent.WaitOnTask(ctx, 2, elsewhereTask); !errors.Is(err, ErrWaitDuplicate) {
		t.Errorf("duplicate: err = %v", err)
	}
}

// Without a reader the capability is absent, not broken.
func TestWaitOnTaskIsAbsentWithoutAReader(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if err := agent.WaitOnTask(context.Background(), 1, elsewhereTask); !errors.Is(err, ErrWaitAbsent) {
		t.Fatalf("err = %v", err)
	}
}

// THE FRONTIER NEVER READS UNDER A LOCK: a reader that blocks forever leaves
// every control of the graph answering, and the node stays queued (it never runs
// on a guess).
func TestABlockedExternalReaderFreezesNothing(t *testing.T) {
	gate := make(chan struct{})
	elsewhere := &fakeElsewhere{tasks: map[TaskRef]ExternalTask{elsewhereTask: {Exists: true, State: TaskRunning}}}
	agent, graph, _, release := waiterGraph(t, elsewhere)
	defer release()
	if err := agent.WaitOnTask(context.Background(), 2, elsewhereTask); err != nil {
		t.Fatal(err)
	}
	// The link is in. Now the disk goes away: every later read blocks.
	elsewhere.mu.Lock()
	elsewhere.gate = gate
	elsewhere.tasks[elsewhereTask] = ExternalTask{Exists: true, State: TaskDone}
	graph.mu.Lock()
	delete(graph.ext.seen, elsewhereTask)
	graph.mu.Unlock()
	elsewhere.mu.Unlock()
	release()
	awaitState(t, graph, 1, TaskDone)

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		graph.runFrontier() // asks for a refresh; must not wait for it
		_ = graph.document()
		_ = graph.nodes[2].stateNow()
		graph.stopAll(10 * time.Millisecond)
	}()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("the graph's controls waited on another conversation's disk")
	}
	if got := stateOf(graph, 2); got != TaskQueued {
		t.Fatalf("task 2 ran on a guess: %q", got)
	}
	if elsewhere.count() < 2 {
		t.Fatalf("the reader was never asked (asks = %d)", elsewhere.count())
	}
	close(gate)
}

// A graph with no external waits does no reading at all, however often the
// frontier turns.
func TestNoExternalWaitsMeansNoReads(t *testing.T) {
	elsewhere := &fakeElsewhere{tasks: map[TaskRef]ExternalTask{}}
	_, graph, _, release := waiterGraph(t, elsewhere)
	release()
	awaitState(t, graph, 2, TaskDone)
	for i := 0; i < 20; i++ {
		graph.runFrontier()
	}
	if n := elsewhere.count(); n != 0 {
		t.Fatalf("a graph with no external waits asked the reader %d times", n)
	}
}

// Genuine event clocks: a transition stamps its own instant, which is not later
// than the checkpoint read.
func TestAStateChangeStampsItsOwnInstant(t *testing.T) {
	elsewhere := &fakeElsewhere{tasks: map[TaskRef]ExternalTask{}}
	_, graph, _, release := waiterGraph(t, elsewhere)
	graph.mu.Lock()
	queued := graph.nodes[2].stateAt
	admitted := graph.nodes[2].admittedAt
	graph.mu.Unlock()
	if queued.IsZero() || admitted.IsZero() {
		t.Fatal("admission stamps both clocks")
	}
	time.Sleep(5 * time.Millisecond)
	release()
	awaitState(t, graph, 2, TaskDone)
	graph.mu.Lock()
	defer graph.mu.Unlock()
	if !graph.nodes[2].stateAt.After(queued) {
		t.Fatalf("stateAt did not move with the state: %v then %v", queued, graph.nodes[2].stateAt)
	}
	if !graph.nodes[2].admittedAt.Equal(admitted) {
		t.Fatal("admittedAt moved")
	}
}
