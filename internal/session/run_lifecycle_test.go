package session

// A RUN'S LIFE, FROM THE REQUEST THAT OPENS IT TO THE LAST WORD ON ITS RECORD.
//
// Each test here is one defect a reviewer measured on the bash belt's run door,
// held at the door a person reaches it through: a new hand-off adopting a run it
// did not start, an ending that wrote nothing on the run's own task, a hand-off
// joining a run already on its way out, a closed conversation landing work and
// failing its row, a steer taken on a run that had ended, a stop that read as a
// failure, and an interrupted row asking a question nothing could answer.
//
// The engine is a double, for the reason task_run_belt_test.go gives: the real
// one is built on this package and cannot be imported here. Every double in this
// file ends only when the test says so, so no assertion races a run.

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// relayEngine is a run engine that can run more than once: every Start is
// handed back on a channel, and each Start answers the summary the test queued
// for it (a whole run when nothing is queued). A done run completes its root the
// way the real engine does; any other answer writes nothing on the store, which
// is the engine the reviewer measured against. Land holds the first landing
// until the test releases it, so "a run is on its way out" is a state the test
// owns.
type relayEngine struct {
	mu          sync.Mutex
	summaries   []RunSummary
	starts      int
	started     chan RunSpec
	landOnce    sync.Once
	landEntered chan struct{}
	landRelease chan struct{}
}

func newRelayEngine(summaries ...RunSummary) *relayEngine {
	return &relayEngine{
		summaries:   summaries,
		started:     make(chan RunSpec, 8),
		landEntered: make(chan struct{}),
		landRelease: make(chan struct{}),
	}
}

func (e *relayEngine) Start(_ context.Context, spec RunSpec) RunSummary {
	e.mu.Lock()
	summary := RunSummary{Outcome: beltRunOutcomeDone, Result: "done"}
	if e.starts < len(e.summaries) {
		summary = e.summaries[e.starts]
	}
	e.starts++
	e.mu.Unlock()
	e.started <- spec
	if summary.Outcome == beltRunOutcomeDone && spec.Store != nil {
		_ = spec.Store.CompleteRoot(summary.Result)
	}
	return summary
}

func (e *relayEngine) Land(context.Context, *plandb.Store, string, string) (RunLanding, error) {
	e.landOnce.Do(func() { close(e.landEntered) })
	<-e.landRelease
	return RunLanding{}, nil
}

// lifecycleAgent is a conversation on the bash belt with a session folder of its
// own and a repository to cut run copies from.
func lifecycleAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the run did the work"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.SessionFile = filepath.Join(dir, placeTranscript)
		config.AskConsent = false
	})
	return agent, dir
}

// TestANewHandOffNeverAdoptsARunNothingIsDriving is the reviewer's first
// measurement, at the chat's own door: a store left open by a run nothing is
// driving any more (the process that drove it died with it open), and a new
// hand-off in the same conversation. The new request must run under its own
// root with its own words, and the old run must be set aside readable, not run
// again.
func TestANewHandOffNeverAdoptsARunNothingIsDriving(t *testing.T) {
	agent, dir := lifecycleAgent(t)
	engine := newRelayEngine()
	registerBeltRunEngine(t, engine)

	left, err := plandb.Open(filepath.Join(dir, planStoreFilename), "first", "71", "the first task", "FIRST BRIEF", agent.graph().planChat())
	if err != nil {
		t.Fatalf("seed the store a dead process left open: %v", err)
	}
	_ = left.Close()

	id, _, _, err := agent.StartTask(context.Background(), "SECOND BRIEF: rename the logger", false)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	spec := <-engine.started
	close(engine.landRelease)
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})

	want := strconv.FormatUint(id, 10)
	if spec.Store == nil {
		t.Fatal("the engine was handed no store")
	}
	if spec.Brief != "SECOND BRIEF: rename the logger" {
		t.Fatalf("the engine was handed the brief %q", spec.Brief)
	}
	root := beltRunTaskAt(t, dir, want)
	if root == nil {
		t.Fatalf("the live store holds no task %s: the new hand-off ran under the old run's root", want)
	}
	if !strings.Contains(root.Description, "SECOND BRIEF") {
		t.Fatalf("the new run's root reads %q, want the new request's own words", root.Description)
	}
	if old := beltRunTaskAt(t, dir, "71"); old != nil {
		t.Fatalf("the old run's root %q is still in the live store, reading %q", old.ID, old.Description)
	}
	// THE OLD RUN IS KEPT, SET ASIDE AS INTERRUPTED, AND NOT LEFT READING
	// RUNNING: an archived store is read as it stands for good.
	archived, err := plandb.Open(filepath.Join(dir, planStoreFilename)+".1", "", "", "", "")
	if err != nil {
		t.Fatalf("the old run was not set aside beside the session: %v", err)
	}
	defer archived.Close()
	oldRoot := archived.Task("71")
	if oldRoot == nil || !terminalStoreStatus(oldRoot.Status) || oldRoot.Error != taskWordInterrupted {
		t.Fatalf("the set-aside run's root = %+v, want it ended as interrupted", oldRoot)
	}
	if oldRoot.Description != "FIRST BRIEF" {
		t.Fatalf("the set-aside run's words were rewritten: %q", oldRoot.Description)
	}
}

// TestEveryRunEndingWritesTheRootsEnding is the root cause measured from the
// door: an engine that ended on a limit and wrote nothing on the store left the
// run's own task `running`, which is the store the next request adopted.
func TestEveryRunEndingWritesTheRootsEnding(t *testing.T) {
	agent, _, run, dir := landingSummaryFixture(t, &scriptedCompleter{})
	engine := landingRunDouble{summary: RunSummary{Outcome: "a limit you set stopped it", Limit: RunLimitCost}}

	agent.driveBeltRun(context.Background(), engine, run, RunSpec{})

	root := beltRunTaskAt(t, dir, planRootID)
	if root == nil || !terminalStoreStatus(root.Status) {
		t.Fatalf("the run's own task after a limit ended it = %+v, want an ending on it", root)
	}
	if root.Status == plandb.StatusCancelled {
		t.Fatal("a limit's ending was written as a person's stop")
	}
}

// TestOpenRunPlanSetsAsideARunLeftOpen is the same defect at the headless door
// (`codeaf do`): a directory holding a store a timed-out or interrupted errand
// left open answered the next errand with the old errand's root.
func TestOpenRunPlanSetsAsideARunLeftOpen(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenRunPlan(dir, "rename the logger", "rename the logger")
	if err != nil {
		t.Fatalf("first errand: %v", err)
	}
	_ = first.Close()

	second, err := OpenRunPlan(dir, "fix the parser", "fix the parser")
	if err != nil {
		t.Fatalf("second errand: %v", err)
	}
	defer second.Close()
	root := second.Task(second.RootID())
	if root == nil || root.Title != "fix the parser" || root.Description != "fix the parser" {
		t.Fatalf("the second errand opened on root %+v, want its own words", root)
	}
	if terminalStoreStatus(root.Status) {
		t.Fatalf("the second errand's root is already %s", root.Status)
	}
	archived, err := plandb.Open(PlanStorePath(dir)+".1", "", "", "", "")
	if err != nil {
		t.Fatalf("the first errand was not set aside: %v", err)
	}
	defer archived.Close()
	if old := archived.Task(archived.RootID()); old == nil || old.Title != "rename the logger" || old.Error != taskWordInterrupted {
		t.Fatalf("the set-aside errand = %+v, want the first errand ended as interrupted", old)
	}
}

// TestAHandOffDuringARunsLandingStartsAFreshRun is the reviewer's join measured
// in the gap it lives in: the engine has answered and the run is landing when a
// second hand-off arrives. Joined, its work went into a store nothing would ever
// run again and its row settled failed with no report. It must wait for the run
// to be over and start a run of its own.
func TestAHandOffDuringARunsLandingStartsAFreshRun(t *testing.T) {
	agent, dir := lifecycleAgent(t)
	engine := newRelayEngine(RunSummary{Outcome: "a limit you set stopped it", Limit: RunLimitCost})
	registerBeltRunEngine(t, engine)

	first, _, _, err := agent.StartTask(context.Background(), "the first piece of work", false)
	if err != nil {
		t.Fatalf("StartTask (first): %v", err)
	}
	<-engine.started
	<-engine.landEntered

	type answer struct {
		id  uint64
		err error
	}
	// THE SECOND HAND-OFF ARRIVES WHILE THE FIRST RUN IS LANDING, and the test
	// knows which of the two things it did before the landing is let go: it
	// either answered at once, having joined the run on its way out, or it is
	// waiting for that run to be over ([beltJoinWaits] is the door's one word
	// that it is waiting, and nothing else reads it).
	waiting := make(chan struct{})
	var waitOnce sync.Once
	previous := beltJoinWaits
	beltJoinWaits = func() { waitOnce.Do(func() { close(waiting) }) }
	t.Cleanup(func() { beltJoinWaits = previous })
	second := make(chan answer, 1)
	go func() {
		id, _, _, err := agent.StartTask(context.Background(), "a second piece of work", false)
		second <- answer{id, err}
	}()
	var got answer
	select {
	case got = <-second:
		joined := beltRunTaskAt(t, dir, strconv.FormatUint(got.id, 10))
		t.Fatalf("the second hand-off answered while the first run was landing, as %+v in the first run's store", joined)
	case <-waiting:
	}
	close(engine.landRelease)
	got = <-second
	if got.err != nil {
		t.Fatalf("StartTask (second): %v", got.err)
	}

	var spec RunSpec
	beltRunWaitFor(t, "the second hand-off's own run", func() bool {
		select {
		case spec = <-engine.started:
			return true
		default:
			return false
		}
	})
	if spec.Store == nil || spec.Store.RootID() != strconv.FormatUint(got.id, 10) {
		t.Fatalf("the second run was handed root %q, want its own %d", spec.Store.RootID(), got.id)
	}
	beltRunWaitFor(t, "the second run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	archived, err := plandb.Open(filepath.Join(dir, planStoreFilename)+".1", "", "", "", "")
	if err != nil {
		t.Fatalf("the first run was not set aside: %v", err)
	}
	defer archived.Close()
	if joined := archived.Task(strconv.FormatUint(got.id, 10)); joined != nil {
		t.Fatalf("the second hand-off's work went into the first run's store as %+v", joined)
	}
	if archived.RootID() != strconv.FormatUint(first, 10) {
		t.Fatalf("the set-aside store is run %q, want the first run %d", archived.RootID(), first)
	}
}

// TestClosingTheConversationLandsNothingAndSettlesNothing is #1291's promise
// held at the door: closing the conversation ends its run and writes nothing on
// its record. The drive loop used to go on after the close, land the work into
// the folder and settle the row failed, while the row read back tomorrow said
// interrupted — two readers of one run disagreeing.
func TestClosingTheConversationLandsNothingAndSettlesNothing(t *testing.T) {
	agent, dir := lifecycleAgent(t)
	double := newBeltRunDouble("never reached")
	double.honoursStop = true
	registerBeltRunEngine(t, double)

	id, _, _, err := agent.StartTask(context.Background(), "a long piece of work", false)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	<-double.entered
	journal := agent.file.journalPath()
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-double.finished
	beltRunWaitFor(t, "the run's driver to let go", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})

	if lands := double.lands(); lands != 0 {
		t.Fatalf("a run the closing conversation cut was landed %d times", lands)
	}
	rows := agent.graph().runRows(id)
	if len(rows) != 1 || rows[0].State != TaskRunning {
		t.Fatalf("the run's row after the close = %+v, want it left as it was, not settled", rows)
	}
	if root := beltRunTaskAt(t, dir, strconv.FormatUint(id, 10)); root == nil || terminalStoreStatus(root.Status) {
		t.Fatalf("the run's own task after the close = %+v, want nothing written on it", root)
	}

	reopened, err := newAgent(Config{Workspace: agent.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: journal}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	kept := reopened.graph().runRows(id)
	if len(kept) != 1 || kept[0].State != TaskInterrupted {
		t.Fatalf("the reopened conversation reads the run as %+v, want interrupted", kept)
	}
}

// TestAPartStoppedFromItsPageReadsStopped is the second reviewer's S3, through
// the door a person presses: `x` on one part of a run. The cancel carried no
// reason, and a row reads stopped only off the stop's own word, so a part a
// person stopped read `incomplete` like work that failed on its own.
func TestAPartStoppedFromItsPageReadsStopped(t *testing.T) {
	agent, dir := lifecycleAgent(t)
	double := newBeltRunDouble("the run did the work")
	registerBeltRunEngine(t, double)

	id, _, _, err := agent.StartTask(context.Background(), "the whole of the work", false)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	<-double.entered
	store := beltRunStoreAt(t, dir)
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "alpha", Title: "one part", ParentID: strconv.FormatUint(id, 10)}}); err != nil {
		t.Fatalf("add a part: %v", err)
	}
	_ = store.Close()

	if err := agent.PlanCancel(planStoreID("alpha")); err != nil {
		t.Fatalf("PlanCancel: %v", err)
	}
	row := planRowFor(agent.PlanTasks(), planStoreID("alpha"))
	if row == nil || row.Status != string(plandb.StatusCancelled) || !row.Stopped {
		t.Fatalf("the part a person stopped reads %+v, want cancelled and stopped", row)
	}
	endBeltRun(t, agent, double)
}

// TestASteerOnAFinishedRunIsRefused is #1234's sentence held for the newest
// run: a run that finished is still the live store until the next request sets
// it aside, and a note, a hold or an amendment on it was written where nothing
// would ever read it.
func TestASteerOnAFinishedRunIsRefused(t *testing.T) {
	agent, _ := lifecycleAgent(t)
	double := newBeltRunDouble("the run did the work")
	registerBeltRunEngine(t, double)

	id, _, _, err := agent.StartTask(context.Background(), "a small piece of work", false)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	<-double.entered
	endBeltRun(t, agent, double)

	root := planStoreID(strconv.FormatUint(id, 10))
	if err := agent.PlanNote(root, "one more thing"); !errors.Is(err, errPlanEndedRun) {
		t.Fatalf("a note on a finished run answered %v, want %q", err, errPlanEndedRun)
	}
	if err := agent.PlanAmend(root, "and also this"); !errors.Is(err, errPlanEndedRun) {
		t.Fatalf("an amendment on a finished run answered %v, want %q", err, errPlanEndedRun)
	}
}

// TestAnInterruptedRowRaisesNoMarkNothingCanAnswer: the door that carries a run
// on has no caller, so an interrupted row that raised `needs you` and offered
// `continue it` raised a mark no press could clear.
func TestAnInterruptedRowRaisesNoMarkNothingCanAnswer(t *testing.T) {
	status := ProjectTask(TaskFacts{State: TaskInterrupted})
	if status.Attention {
		t.Fatal("an interrupted row raises the needs-you mark, and nothing can answer it")
	}
	if status.Ask.Yes != "" || status.Ask.No != "" {
		t.Fatalf("an interrupted row offers %q and %q, and no door takes either", status.Ask.Yes, status.Ask.No)
	}
	if status.On == TaskWaitPerson {
		t.Fatal("an interrupted row says it is waiting on its person, who can do nothing about it")
	}
	if status.Tier == TaskTierYourCall {
		t.Fatal("an interrupted row sits in the tier that asks a person to decide")
	}
	if status.Word != "interrupted" || status.Reason != "nothing is driving it; everything it did is kept" {
		t.Fatalf("an interrupted row reads %q · %q", status.Word, status.Reason)
	}
}

// TestAJoinedRowComesBackInterruptedWithoutAFalseSentence: a hand-off that
// joined a run shares the run's copy and never had one of its own written down,
// so each came back after a restart saying its working copy was not written
// down — false for every one of them.
func TestAJoinedRowComesBackInterruptedWithoutAFalseSentence(t *testing.T) {
	joined := runRowNotice(runRecord{ID: 72, Parent: 71, Title: "a second piece", State: TaskRunning})
	status := ProjectTask(joined.StatusFacts())
	if joined.State != TaskInterrupted {
		t.Fatalf("a joined row that was live came back %s", joined.State)
	}
	if strings.Contains(status.Reason, "not written down") {
		t.Fatalf("a joined row says %q about a copy it never owned", status.Reason)
	}
	// AND THE RUN'S OWN ROW STILL SAYS IT, where it is true.
	own := runRowNotice(runRecord{ID: 71, Title: "the run", State: TaskRunning})
	if reason := ProjectTask(own.StatusFacts()).Reason; !strings.Contains(reason, "not written down") {
		t.Fatalf("a run whose copy was never written down reads %q", reason)
	}
}
