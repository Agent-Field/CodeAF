package session

// ONE RUN PER BATCH. A message that proposes several tasks, all approved at
// once, commits every hand-off at the same moment. They used to race to open
// the conversation's one store: each one found no live run, each one opened
// (or set aside) the same plandb.db, two of them started runs of their own and
// the rest fell back without a word to the older engine's tree. These tests
// hold the three laws that replaced it: the batch is one run with every
// hand-off in it, a run road that fails says so rather than becoming a node
// of the older tree, and a run row nothing drives still answers and can be
// stopped.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// batchRunDouble is a run engine that can be started more than once, because
// the defect under test is exactly that it WAS: every Start is counted, and
// each holds until the test lets the runs go.
type batchRunDouble struct {
	mu      sync.Mutex
	starts  int
	release chan struct{}
}

func newBatchRunDouble() *batchRunDouble {
	return &batchRunDouble{release: make(chan struct{})}
}

func (d *batchRunDouble) Start(ctx context.Context, spec RunSpec) RunSummary {
	d.mu.Lock()
	d.starts++
	d.mu.Unlock()
	select {
	case <-d.release:
	case <-ctx.Done():
	}
	if spec.Store != nil {
		_ = spec.Store.CompleteRoot("done")
	}
	return RunSummary{Outcome: beltRunOutcomeDone, Result: "done"}
}

func (d *batchRunDouble) Land(context.Context, *plandb.Store, string, string) (RunLanding, error) {
	return RunLanding{}, nil
}

func (d *batchRunDouble) started() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.starts
}

// batchAgent is a conversation on the bash belt with its session folder at
// dir, a fixed clock, and an older-engine runner that runs nothing, so a node
// admitted by a fallback is a node the test can count and never a worker.
func batchAgent(t *testing.T, dir string) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	clock := &fakeClock{at: time.Date(2026, time.September, 23, 9, 0, 0, 0, time.UTC)}
	agent.taskNow = clock.now
	agent.graph().run = func(*TaskNode) {}
	return agent
}

// stageApproved puts one proposal on the card and answers it yes, without
// committing it, so a test can commit a whole batch at the same moment.
func stageApproved(t *testing.T, agent *Agent, title string) *stagedProposal {
	t.Helper()
	staged := agent.stageTask(context.Background(), beltProposalArgs(title, "the part is done"))
	proposal, ok := staged.(*stagedProposal)
	if !ok {
		answer, _, _ := staged.Commit(context.Background())
		t.Fatalf("the proposal %q was not staged: %q", title, answer)
	}
	agent.ResolveTask(proposal.id, TaskAnswer{Approved: true})
	return proposal
}

// endBatchRun lets every run the double holds go, and waits for the
// conversation's run to be over.
func endBatchRun(t *testing.T, agent *Agent, double *batchRunDouble) {
	t.Helper()
	close(double.release)
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
}

// EIGHT HAND-OFFS APPROVED AT ONCE ARE ONE RUN. The first to arrive opens the
// run, and its working copy is slow to cut: that cut is the window the batch
// used to race through. Every other hand-off waits for the run to exist and
// joins it as a child, exactly as a hand-off made a minute later would, so the
// store holds all eight, one engine is started, nothing is set aside and no
// hand-off becomes a node of the older tree.
func TestEightHandoffsApprovedAtOnceAreOneRun(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBatchRunDouble()
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	agent := batchAgent(t, dir)

	slow := make(chan struct{})
	var cuts atomic.Int32
	previous := beltRunPrepare
	beltRunPrepare = func(ctx context.Context, place Place, workspace, session string, id uint64, title string, stand taskStand) (taskTree, error) {
		if cuts.Add(1) == 1 {
			<-slow
		}
		return previous(ctx, place, workspace, session, id, title, stand)
	}
	t.Cleanup(func() { beltRunPrepare = previous })
	var waiting atomic.Int32
	beltStartWaits = func() { waiting.Add(1) }
	t.Cleanup(func() { beltStartWaits = nil })

	const batch = 8
	proposals := make([]*stagedProposal, batch)
	for i := range proposals {
		proposals[i] = stageApproved(t, agent, fmt.Sprintf("Part %d of the batch", i+1))
	}
	type answer struct {
		text   string
		failed bool
		err    error
	}
	answers := make([]answer, batch)
	var returned atomic.Int32
	var wg sync.WaitGroup
	for i, proposal := range proposals {
		wg.Add(1)
		go func() {
			defer wg.Done()
			text, failed, err := proposal.Commit(context.Background())
			answers[i] = answer{text, failed, err}
			returned.Add(1)
		}()
	}
	// The first cut holds until every other hand-off has either come to wait
	// for the run it is starting, or gone on without it.
	beltRunWaitFor(t, "the rest of the batch to wait on the first run's start", func() bool {
		return waiting.Load() == batch-1 || returned.Load() == batch-1
	})
	close(slow)
	wg.Wait()

	for i, got := range answers {
		if got.err != nil || got.failed {
			t.Errorf("hand-off %d answered failed=%v err=%v: %q", proposals[i].id, got.failed, got.err, got.text)
		}
	}
	for _, proposal := range proposals {
		if agent.graph().node(proposal.id) != nil {
			t.Errorf("hand-off %d became a node of the older tree", proposal.id)
		}
	}
	store := beltRunStoreAt(t, dir)
	defer store.Close()
	root := store.RootID()
	joined := 0
	for _, proposal := range proposals {
		id := strconv.FormatUint(proposal.id, 10)
		task := store.Task(id)
		switch {
		case task == nil:
			t.Errorf("the run's store holds no task %s", id)
		case id == root:
		case task.ParentID != root:
			t.Errorf("task %s hangs under %q, want the run's root %s", id, task.ParentID, root)
		default:
			joined++
		}
	}
	if joined != batch-1 {
		t.Errorf("%d hand-offs joined the run, want %d", joined, batch-1)
	}
	if archives := planArchivePaths(filepath.Join(dir, planStoreFilename)); len(archives) != 0 {
		t.Errorf("the batch set %d stores aside, want none: %v", len(archives), archives)
	}
	beltRunWaitFor(t, "the run's engine to start", func() bool { return double.started() >= 1 })
	if got := double.started(); got != 1 {
		t.Errorf("the batch started %d runs, want one", got)
	}
	endBatchRun(t, agent, double)
}

// A HAND-OFF WHOSE RUN ROAD FAILS IS NOT A SILENT NODE. The copy would not cut
// (a disk that would not answer, here), and the hand-off used to fall through
// to the older engine's tree and answer `task N started` as if nothing had
// happened. It now says it did not start and why, on both doors, and reads as
// a failure rather than as the success it is not.
func TestAHandoffWhoseRunRoadFailsIsNotASilentNode(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBatchRunDouble()
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	agent := batchAgent(t, dir)
	previous := beltRunPrepare
	beltRunPrepare = func(context.Context, Place, string, string, uint64, string, taskStand) (taskTree, error) {
		return taskTree{}, errors.New("disk I/O error")
	}
	t.Cleanup(func() { beltRunPrepare = previous })

	proposal := stageApproved(t, agent, "Break the run road")
	answer, failed, err := proposal.Commit(context.Background())
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !failed {
		t.Errorf("the failed hand-off reads as a success: %q", answer)
	}
	if strings.Contains(answer, fmt.Sprintf("task %d started", proposal.id)) || !strings.Contains(answer, "did not start") || !strings.Contains(answer, "disk I/O error") {
		t.Errorf("the failed hand-off's receipt = %q, want it to say it did not start and why", answer)
	}
	if agent.graph().node(proposal.id) != nil {
		t.Errorf("the failed hand-off became a node of the older tree")
	}

	id, _, _, err := agent.StartTask(context.Background(), "break the typed road", false)
	if err == nil || !strings.Contains(err.Error(), "did not start") || !strings.Contains(err.Error(), "disk I/O error") {
		t.Errorf("the typed /task answered id=%d err=%v, want it to say it did not start and why", id, err)
	}
	if id != 0 && agent.graph().node(id) != nil {
		t.Errorf("the typed /task became a node of the older tree")
	}
	if double.started() != 0 {
		t.Errorf("a run road that failed started the engine")
	}
}

// A RUN ROW NOTHING DRIVES STILL ANSWERS, AND STOPS. A row the rail shows
// running, whose run is not the one this conversation is driving, used to open
// a page whose box answered `no task 1 in this session` and whose stop said
// nothing. It now says what the row is and that it can be stopped, and a stop
// settles it.
func TestARunRowNothingDrivesAnswersAMessageAndStops(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	registerBeltRunEngine(t, newBatchRunDouble())
	agent := batchAgent(t, t.TempDir())
	g := agent.graph()
	id := g.reserve()
	agent.publishRunRow(g, TaskNotice{
		ID: id, Title: "orphaned run", State: TaskRunning, StartedAt: agent.taskClockNow(),
		PlanTask: planStoreID(strconv.FormatUint(id, 10)),
	})

	_, err := agent.SteerTask(id, "can you hear me")
	if err == nil {
		t.Fatal("a message to a run row nothing drives was taken as delivered")
	}
	if strings.Contains(err.Error(), "in this session") || !strings.Contains(err.Error(), "stop") {
		t.Errorf("the message was answered %q, want what the row is and that it can be stopped", err)
	}
	line, err := agent.Cancel(CancelTask + ":" + strconv.FormatUint(id, 10))
	if err != nil || !strings.Contains(line, "stopped") {
		t.Fatalf("the stop answered %q, %v; want it stopped", line, err)
	}
	rows := g.runRows(id)
	if len(rows) != 1 || rows[0].State == TaskRunning || !rows[0].Stopped || rows[0].EndedAt.IsZero() {
		t.Fatalf("the stopped row = %+v, want it settled as stopped", rows)
	}
}

// A MESSAGE TO A LIVE RUN'S ROW REACHES ITS TASK, and a joined row whose store
// task is missing answers and stops like the orphan above. The run's rows are
// not nodes of the older tree, so the room's box used to answer every one of
// them `no task N in this session`.
func TestALiveRunsRowsTakeAMessageAndAMissingOneStops(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBatchRunDouble()
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	agent := batchAgent(t, dir)
	stand := taskStand{dir: agent.config.Workspace, mode: TaskModeWorktree}
	if err := agent.startKnownTaskRun(context.Background(), 61, "the run", "brief", nil, stand, ""); err != nil {
		t.Fatalf("start the run: %v", err)
	}
	if err := agent.startKnownTaskRun(context.Background(), 62, "a joined part", "brief", nil, stand, ""); err != nil {
		t.Fatalf("join the run: %v", err)
	}

	receipt, err := agent.SteerTask(62, "use the new schema")
	if err != nil || receipt.Landing == "" {
		t.Fatalf("a message to a joined row answered %+v, %v; want it delivered", receipt, err)
	}
	store := beltRunStoreAt(t, dir)
	said := false
	for _, note := range store.Notes("62", 0) {
		said = said || strings.Contains(note.Body, "use the new schema")
	}
	_ = store.Close()
	if !said {
		t.Fatal("the message is not on the joined task's page")
	}

	// A row the run holds whose store task is gone.
	g := agent.graph()
	agent.beltMu.Lock()
	agent.beltRun.joined = append(agent.beltRun.joined, 63)
	agent.beltMu.Unlock()
	agent.publishRunRow(g, TaskNotice{ID: 63, Title: "lost part", State: TaskRunning, Parent: 61, StartedAt: agent.taskClockNow()})
	if _, err := agent.SteerTask(63, "hello"); err == nil || strings.Contains(err.Error(), "in this session") || !strings.Contains(err.Error(), "stop") {
		t.Errorf("a message to a joined row with no store task answered %v, want what it is and that it can be stopped", err)
	}
	line, err := agent.Cancel(CancelTask + ":63")
	if err != nil || !strings.Contains(line, "stopped") {
		t.Fatalf("the stop answered %q, %v; want it stopped", line, err)
	}
	if rows := g.runRows(63); len(rows) != 1 || rows[0].State == TaskRunning || !rows[0].Stopped {
		t.Fatalf("the stopped row = %+v, want it settled as stopped", rows)
	}
	endBatchRun(t, agent, double)
}

// A RUN WORKER IS BOUND TO ITS OWN RUN, NOT ONLY TO A PATH. Its commands carry
// the run's root beside the store's path, so a later store at the same path
// cannot take its writes ([plandb.RunEnv]).
func TestABeltWorkerIsBoundToItsOwnRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	stub := filepath.Join(t.TempDir(), "stub-codeaf")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(planCLIBinEnv, stub)
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the work", "7", "the run", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	agent, err := NewBeltWorker(Config{Workspace: t.TempDir(), Model: "test/model"}, &scriptedCompleter{}, store.Task("7"), store.Path(), store.RootID())
	if err != nil {
		t.Fatalf("NewBeltWorker: %v", err)
	}
	defer agent.Close()
	got := agent.planCommand("plandb status")
	if want := plandb.RunEnv + "=" + quoteShWord("7"); !strings.Contains(got, want) {
		t.Fatalf("the worker's command %q does not carry its run %q", got, want)
	}
}
