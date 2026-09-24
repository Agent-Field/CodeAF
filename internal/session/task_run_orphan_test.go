package session

// A RUN NO PROCESS HOLDS IS A RECORD, NEVER MORE WORK.
//
// On 2026-09-24 a CSSTree `/senior-dev` ran inside the store an earlier
// happy-dom run had left open when codeaf went away under it: the program was
// handed happy-dom's brief in the CSSTree copy, its calls, spend, ceiling and
// ending were written into happy-dom's record folder, and the CSSTree task had
// no page. These tests pin the three doors that close that: the conversation
// closing under a program's run ends it before anything is cut, a new hand-off
// archives whatever store it finds and seeds its own, and a conversation read
// back from disk ends a program's run the last process left open — each at the
// run's last evidence of life, never at the moment somebody noticed.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// orphanProgramStore writes, at the conversation's plan path, the store a
// program's run leaves when its process goes away mid-run: its run task still
// open, the program's record beside it, a charge on the ledger, and a model
// call that came back at lastSeen. It answers lastSeen.
func orphanProgramStore(t *testing.T, path, rootID, brief string, chat ...string) time.Time {
	t.Helper()
	store, err := plandb.Open(path, "the dead run", rootID, "the dead run", brief, chat...)
	if err != nil {
		t.Fatalf("seed the dead run's store: %v", err)
	}
	defer store.Close()
	if err := store.AddSpend(rootID, "delegate/fake", "work", 0.25, 100, 20); err != nil {
		t.Fatal(err)
	}
	taskDir := plandb.TaskDir(filepath.Dir(path), rootID)
	if err := delegate.WriteProgram(taskDir, delegate.ProgramRecord{Name: "fake", CeilingUSD: 3.56}); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	lastSeen := started.Add(time.Millisecond)
	if err := delegate.AppendTurn(taskDir, delegate.Turn{Seq: 1, Started: started, Ended: lastSeen, Model: "vendor/m"}); err != nil {
		t.Fatal(err)
	}
	return lastSeen
}

// THE CRASH ROAD: a store a dead program's run left open is ended at its last
// evidence of life and archived, and the new hand-off runs in a store of its
// own, under its own number, on its own brief.
func TestANewHandOffNeverRunsInsideADeadProgramsStore(t *testing.T) {
	double := newBeltRunDouble("")
	registerBeltRunEngine(t, double)
	place := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: ""}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: place}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	path := agent.graph().planPath()
	lastSeen := orphanProgramStore(t, path, "3", "the FIRST brief: implement happy-dom")

	id, _, _, err := agent.StartDelegate(context.Background(), "fake", "the SECOND brief: implement csstree")
	if err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	rootID := spec.Store.RootID()
	if rootID != strconv.FormatUint(id, 10) {
		t.Fatalf("the new run was seated on store root %q, want its own number %d", rootID, id)
	}
	if got := spec.Store.Task(rootID).Description; got != "the SECOND brief: implement csstree" {
		t.Fatalf("the new run's task holds the brief %q, want its own", got)
	}
	if _, ok := agent.PlanTaskPage(strconv.FormatUint(id, 10)); !ok {
		t.Fatal("the new run has no page of its own")
	}
	endBeltRun(t, agent, double)

	// THE DEAD RUN IS A RECORD NOW: archived, ended where it was last seen.
	archived, err := plandb.Open(path+".1", "", "", "", "")
	if err != nil {
		t.Fatalf("the dead run's store was not archived: %v", err)
	}
	defer archived.Close()
	root := archived.Task("3")
	if root == nil || root.Status != plandb.StatusFailed || root.Error != "codeaf closed while fake was running" {
		t.Fatalf("the dead run's task = %+v, want failed with the plain sentence", root)
	}
	if !root.CompletedAt.Equal(lastSeen) {
		t.Fatalf("the dead run ended at %v, want its last model call's end %v", root.CompletedAt, lastSeen)
	}
	if record, ok := delegate.ReadProgram(plandb.TaskDir(place, "3")); !ok || record.CeilingUSD != 3.56 {
		t.Fatalf("the dead run's record was written over: %+v %v", record, ok)
	}
}

// AN ORDINARY RUN A LIMIT ENDED IS ARCHIVED INTACT. Its store is the record of
// what it did, so it is not ended; and the next hand-off runs its own brief
// under its own number rather than resuming the old one under the new title.
func TestALimitEndedRunIsArchivedIntactAndTheNextHandOffRunsItsOwnBrief(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	conversation := beltRunCommittedRepo(t)
	dir := t.TempDir()
	double := newBeltRunDouble("")
	double.real = true
	double.leaveOpen = true
	double.summary = RunSummary{Outcome: "a limit you set stopped it", Limit: RunLimitCost, Nodes: 1, Steps: 3, USD: 1.5}
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: ""}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: dir}
	})
	if err := agent.startKnownTaskRun(context.Background(), 91, "the limited run", "brief one", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	endBeltRun(t, agent, double)

	second := newBeltRunDouble("second")
	second.real = true
	registerBeltRunEngine(t, second)
	if err := agent.startKnownTaskRun(context.Background(), 92, "an unrelated change", "brief two", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-second.entered
	second.mu.Lock()
	store := second.spec.Store
	second.mu.Unlock()
	if root := store.Task(store.RootID()); store.RootID() != "92" || root.Description != "brief two" || store.Task("91") != nil {
		t.Fatalf("the next hand-off runs on root %q with brief %q (holds 91: %v), want its own", store.RootID(), root.Description, store.Task("91") != nil)
	}
	endBeltRun(t, agent, second)

	archived, err := plandb.Open(filepath.Join(dir, planStoreFilename)+".1", "", "", "", "")
	if err != nil {
		t.Fatalf("the limited run's store was not archived: %v", err)
	}
	defer archived.Close()
	if root := archived.Task("91"); root == nil || terminalStoreStatus(root.Status) || root.Description != "brief one" {
		t.Fatalf("the limited run's task = %+v, want it archived as it was left", root)
	}
}

// THE CLOSE ROAD: the conversation (or the engine) closing under a program's
// run writes its ending before anything is cut, so the store never outlives
// the process saying `running`, and the next hand-off gets its own store.
func TestClosingUnderAProgramsRunEndsItInItsStoreFirst(t *testing.T) {
	place := t.TempDir()
	workspace := newTestRepo(t)
	first := newBeltRunDouble("")
	registerBeltRunEngine(t, first)
	agent, _ := newTestAgent(t, beltRunCompleter{text: ""}, func(config *Config) {
		config.Workspace = workspace
		config.Place = Place{Dir: place}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "the FIRST brief: implement happy-dom"); err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-first.entered
	first.mu.Lock()
	firstRoot := first.spec.Store.RootID()
	first.mu.Unlock()
	// The double plays a program still inside its grace when Close returns:
	// the process that closed would be gone before the run wrote anything.
	_ = agent.Close()
	store := beltRunStoreAt(t, place)
	root := store.Task(firstRoot)
	_ = store.Close()
	if root.Status != plandb.StatusFailed || root.Error != "codeaf closed while fake was running" || root.CompletedAt.IsZero() {
		t.Fatalf("after Close the run's task = %s (%q, ended %v), want it ended in the store", root.Status, root.Error, root.CompletedAt)
	}
	// AND ITS PAGE SAYS WHY: the page draws the task's newest note.
	if page, ok := agent.PlanTaskPage(firstRoot); !ok || page.Row.Note != "codeaf closed while fake was running" {
		t.Fatalf("the closed run's page row = %+v (%v), want the plain sentence beside it", page.Row, ok)
	}

	second := newBeltRunDouble("")
	registerBeltRunEngine(t, second)
	again, _ := newTestAgent(t, beltRunCompleter{text: ""}, func(config *Config) {
		config.Workspace = workspace
		config.Place = Place{Dir: place}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	id, _, _, err := again.StartDelegate(context.Background(), "fake", "the SECOND brief: implement csstree")
	if err != nil {
		t.Fatalf("second StartDelegate: %v", err)
	}
	<-second.entered
	second.mu.Lock()
	spec := second.spec
	second.mu.Unlock()
	if spec.Store.RootID() != strconv.FormatUint(id, 10) || spec.Store.Task(spec.Store.RootID()).Description != "the SECOND brief: implement csstree" {
		t.Fatalf("the second run is on root %q with brief %q", spec.Store.RootID(), spec.Store.Task(spec.Store.RootID()).Description)
	}
	endBeltRun(t, again, second)
	close(first.release)
	<-first.finished
}

// THE RESTORE ROAD: a conversation read back from disk whose run row comes
// back interrupted over a program's store the last process left open has that
// run ended at its last evidence of life, before anybody hands off again.
func TestAReopenedConversationEndsTheProgramRunItsLastProcessLeftOpen(t *testing.T) {
	place := t.TempDir()
	workspace := newTestRepo(t)
	registerBeltRunEngine(t, newBeltRunDouble(""))
	open := func() *Agent {
		agent, _ := newTestAgent(t, beltRunCompleter{text: ""}, func(config *Config) {
			config.Workspace = workspace
			config.Place = Place{Dir: place}
			config.SessionFile = filepath.Join(place, placeTranscript)
			config.AskConsent = false
			config.Delegates = testPrograms("fake")
		})
		return agent
	}
	life := open()
	g := life.graph()
	id := g.reserve()
	rootID := strconv.FormatUint(id, 10)
	lastSeen := orphanProgramStore(t, g.planPath(), rootID, "the brief", g.planChat())
	// The row as the process that died last wrote it down: running.
	life.publishRunRow(g, TaskNotice{ID: id, Title: "the dead run", State: TaskRunning, StartedAt: time.Now()})
	_ = life.Close()

	reopened := open()
	// THE ROW SETTLES WITH ITS STORE. A program's run is one nothing can carry
	// on, so the row the reopen restored as interrupted — which the side list
	// drew as `?`, waiting on a person — is settled where the page already
	// stood: ended, in codeaf's sentence, not a fault, at the instant it was
	// last seen, with its span. The sentence is the row's reason, so its ending
	// is the one whose reason is read from its report.
	rows := reopened.graph().runRows(id)
	if len(rows) != 1 || rows[0].State != TaskFailed || rows[0].Ending != TaskEndingProgram ||
		rows[0].Report != "codeaf closed while fake was running" || !rows[0].EndedAt.Equal(lastSeen) {
		t.Fatalf("the row came back as %+v, want it ended in codeaf's sentence, at %v", rows, lastSeen)
	}
	page, ok := reopened.PlanTaskPage(rootID)
	if !ok || page.Row.Status != string(plandb.StatusFailed) || page.Row.Stage != "" || !page.Row.Live.Empty() ||
		page.Row.Ended.IsZero() || !page.Row.Ended.Equal(lastSeen) {
		t.Fatalf("the dead run's page row = %+v (%v), want it ended at %v with no live stage", page.Row, ok, lastSeen)
	}
	store := beltRunStoreAt(t, place)
	defer store.Close()
	root := store.Task(rootID)
	if root.Status != plandb.StatusFailed || root.Error != "codeaf closed while fake was running" {
		t.Fatalf("the reopened conversation left the dead run's task %s (%q)", root.Status, root.Error)
	}
	if !root.CompletedAt.Equal(lastSeen) {
		t.Fatalf("the dead run ended at %v, want where it was last seen, %v", root.CompletedAt, lastSeen)
	}
	if _, err := os.Stat(filepath.Join(place, planStoreFilename+".1")); !os.IsNotExist(err) {
		t.Fatalf("opening a conversation archived its store: %v", err)
	}
}

// A RUN CODEAF CLOSED UNDER READS ENDED ON ITS PAGE, NOT WORKING. Closing the
// conversation writes the run's ending before it cuts the program, and the
// program never lives to clear the stage it was in, so the page's line read
// `working` with no time over a run nothing was driving (found by killing the
// engine under a real senior-dev run). A task the store has ended has no live
// stage, and its time ends where the store says it ended.
func TestAProgramsTaskTheStoreHasEndedHasNoLiveStageAndStopsItsClock(t *testing.T) {
	place := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: ""}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: place}
		config.Delegates = testPrograms("fake")
	})
	g := agent.graph()
	id := g.reserve()
	rootID := strconv.FormatUint(id, 10)
	orphanProgramStore(t, g.planPath(), rootID, "the brief", g.planChat())
	store := beltRunStoreAt(t, place)
	if err := store.SetLive(rootID, 3, "fake: working"); err != nil {
		t.Fatal(err)
	}
	endedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	if err := store.FailRootAt("codeaf closed while fake was running", endedAt); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	agent.publishRunRow(g, TaskNotice{ID: id, Title: "the dead run", State: TaskRunning, StartedAt: endedAt.Add(-5 * time.Minute)})
	page, ok := agent.PlanTaskPage(rootID)
	if !ok || page.Row.Stage != "" || !page.Row.Live.Empty() {
		t.Fatalf("an ended program task's page row = %+v (%v), want no live stage", page.Row, ok)
	}
	if page.Row.Ended.IsZero() {
		t.Fatalf("an ended program task's page row has no end, want its clock stopped where the store ended it")
	}
}
