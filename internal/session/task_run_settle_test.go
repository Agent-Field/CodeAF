package session

// A RUN SETTLED AFTER ITS PROCESS WENT AWAY SAYS WHAT ENDED IT AND WHEN.
//
// The reopen road ([Agent.settleInterruptedProgramRow]) and the close road
// ([Agent.cutBeltRun]) each write a program's run's ending without the run
// there to say how it ended, and the project's index closes every run row its
// last process left running ([Agent.closeInflightTaskIndexRows]). These tests
// pin that each of them tells the truth the run left behind: a person's stop
// is a stop, a limit is the limit, a program that had already exited is not
// said to have been running, and every ending is dated by the run's own facts
// rather than by the moment somebody noticed.

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

// reopenedWith seeds a program's run the way a process that went away leaves
// it, lets change end it in its store (or not), closes the conversation that
// held its running row, and opens it again. It answers the reopened agent, the
// run's row id and the instant the run was last seen working.
func reopenedWith(t *testing.T, change func(store *plandb.Store, taskDir string, lastSeen time.Time)) (*Agent, uint64, time.Time) {
	t.Helper()
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
	started := time.Now().UTC()
	lastSeen := orphanProgramStore(t, g.planPath(), rootID, "the brief", g.planChat())
	store, err := plandb.Open(g.planPath(), "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	change(store, plandb.TaskDir(filepath.Dir(g.planPath()), rootID), lastSeen)
	_ = store.Close()
	life.publishRunRow(g, TaskNotice{ID: id, Title: "the dead run", State: TaskRunning, StartedAt: started})
	_ = life.Close()
	return open(), id, lastSeen
}

// reopenedRow is the one run row a reopened conversation holds under id.
func reopenedRow(t *testing.T, agent *Agent, id uint64) TaskNotice {
	t.Helper()
	rows := agent.graph().runRows(id)
	if len(rows) != 1 {
		t.Fatalf("the reopened conversation holds %d rows under %d, want one: %+v", len(rows), id, rows)
	}
	return rows[0]
}

// A PERSON'S STOP IS A STOP AFTER A REOPEN TOO. The person stopped the run and
// quit codeaf while the stop was still settling; the reopened row used to read
// as the program ending itself (`did not finish · stopped: enough`).
func TestAReopenedRunAPersonStoppedSettlesAsTheirStop(t *testing.T) {
	agent, id, _ := reopenedWith(t, func(store *plandb.Store, _ string, _ time.Time) {
		if err := store.StopRoot("stopped: enough"); err != nil {
			t.Fatal(err)
		}
	})
	row := reopenedRow(t, agent, id)
	if row.State != TaskFailed || !row.Stopped || row.Ending != TaskEndingStopped || row.Report != "stopped: enough" {
		t.Fatalf("the stopped run came back as %+v, want it settled as the person's stop", row)
	}
}

// A LIMIT ITS PERSON SET IS THE ROW'S ENDING AFTER A REOPEN TOO, and which
// limit it was is read off the run's own facts: the spend against the ceiling
// the run handed its program.
func TestAReopenedRunALimitEndedKeepsWhichLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ceiling float64
		want    TaskEnding
	}{
		{"the dollars ran out", 0.25, TaskEndingCostLimit},
		{"the time ran out", 3.56, TaskEndingTimeLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, id, _ := reopenedWith(t, func(store *plandb.Store, taskDir string, _ time.Time) {
				if err := delegate.WriteProgram(taskDir, delegate.ProgramRecord{Name: "fake", CeilingUSD: tc.ceiling}); err != nil {
					t.Fatal(err)
				}
				if err := store.FailRoot(runLimitSentence); err != nil {
					t.Fatal(err)
				}
			})
			row := reopenedRow(t, agent, id)
			if row.State != TaskFailed || row.Ending != tc.want || row.Stopped {
				t.Fatalf("the limit-ended run came back as %+v, want the ending %q", row, tc.want)
			}
		})
	}
}

// THE ROW CODEAF CLOSED UNDER READS CODEAF'S SENTENCE AS ITS REASON, which is
// what senior-dev.md promises beside `incomplete`; it used to read the fixed
// `was cut short from outside the work` of an ending machinery cut.
func TestAReopenedRunCodeafClosedUnderReadsItsSentence(t *testing.T) {
	agent, id, _ := reopenedWith(t, func(*plandb.Store, string, time.Time) {})
	row := reopenedRow(t, agent, id)
	if row.Ending != TaskEndingProgram || TaskReasonOf(row.Ending, row.Report) != "codeaf closed while fake was running" {
		t.Fatalf("the closed run came back as %+v with the reason %q, want codeaf's sentence", row, TaskReasonOf(row.Ending, row.Report))
	}
}

// THE PROGRAM'S RECORDED EXIT WINS OVER THE STORE'S LATER ENDING. A store
// ending written after the program had gone (the worker was settling owed
// receipts) used to date the reopened row, which counted the wait as run time.
func TestAReopenedRunEndsAtItsProgramsRecordedExit(t *testing.T) {
	var exited time.Time
	agent, id, _ := reopenedWith(t, func(store *plandb.Store, taskDir string, lastSeen time.Time) {
		exited = lastSeen.Add(time.Millisecond)
		if err := delegate.WriteProgram(taskDir, delegate.ProgramRecord{Name: "fake", StartedAt: lastSeen.Add(-time.Second), EndedAt: exited}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
		if err := store.FailRoot("fake did not finish: the tests fail"); err != nil {
			t.Fatal(err)
		}
	})
	row := reopenedRow(t, agent, id)
	if !row.EndedAt.Equal(exited) || row.Ending != TaskEndingProgram {
		t.Fatalf("the run came back ended at %v (%+v), want the program's exit %v", row.EndedAt, row, exited)
	}
}

// A PROGRAM THAT HAD ALREADY EXITED IS NOT SAID TO HAVE BEEN RUNNING. codeaf
// closed while the worker was settling the program's receipts, after the
// program was gone: the run is ended where the program ended, in a sentence
// that says the program had ended and its work was never brought in.
func TestClosingAfterTheProgramExitedSaysItHadEnded(t *testing.T) {
	place := t.TempDir()
	double := newBeltRunDouble("")
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: ""}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: place}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "the brief"); err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	store := double.spec.Store
	double.mu.Unlock()
	rootID := store.RootID()
	exited := time.Now().UTC()
	if err := delegate.WriteProgram(plandb.TaskDir(filepath.Dir(store.Path()), rootID), delegate.ProgramRecord{Name: "fake", StartedAt: exited.Add(-time.Second), EndedAt: exited}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	_ = agent.Close()
	kept := beltRunStoreAt(t, place)
	root := kept.Task(rootID)
	_ = kept.Close()
	if root.Error != "fake had ended; codeaf closed before its work was brought in" || !root.CompletedAt.Equal(exited) {
		t.Fatalf("after Close the run's task = %s (%q, ended %v), want it ended at the program's exit %v in a true sentence", root.Status, root.Error, root.CompletedAt, exited)
	}
	close(double.release)
	<-double.finished
}

// AND THE SAME ON THE REOPEN ROAD: a store left open over a program that had
// exited is ended at that exit, in the same sentence.
func TestAReopenedRunWhoseProgramHadExitedSaysItHadEnded(t *testing.T) {
	var exited time.Time
	agent, id, _ := reopenedWith(t, func(_ *plandb.Store, taskDir string, lastSeen time.Time) {
		exited = lastSeen.Add(time.Millisecond)
		if err := delegate.WriteProgram(taskDir, delegate.ProgramRecord{Name: "fake", StartedAt: lastSeen.Add(-time.Second), EndedAt: exited}); err != nil {
			t.Fatal(err)
		}
	})
	row := reopenedRow(t, agent, id)
	if row.Report != "fake had ended; codeaf closed before its work was brought in" || !row.EndedAt.Equal(exited) || row.Ending != TaskEndingProgram {
		t.Fatalf("the run came back as %+v, want it ended at the program's exit %v in a true sentence", row, exited)
	}
}

// A RUN ROW THE INDEX CLOSES ON REOPEN ENDS WHERE THE RUN DID, WITH ITS SPAN.
// A program that had finished (its store says done) but whose work codeaf
// closed before landing was closed in the project's index at the reopen
// instant, hours after the run, with no duration.
func TestTheIndexClosesAnInterruptedRunRowFromTheRunsOwnFacts(t *testing.T) {
	var exited time.Time
	agent, id, _ := reopenedWith(t, func(store *plandb.Store, taskDir string, lastSeen time.Time) {
		exited = lastSeen.Add(time.Millisecond)
		if err := delegate.WriteProgram(taskDir, delegate.ProgramRecord{Name: "fake", StartedAt: lastSeen.Add(-time.Second), EndedAt: exited}); err != nil {
			t.Fatal(err)
		}
		if err := store.CompleteRoot("finished: the change is in"); err != nil {
			t.Fatal(err)
		}
	})
	var row TaskIndexEntry
	for _, entry := range lastPerNode(ReadTaskIndex(agent.config.taskIndexFile())) {
		if entry.ID == strconv.FormatUint(id, 10) {
			row = entry
		}
	}
	if row.Live() || !row.EndedAt.Equal(exited) || row.DurationMS != exited.Sub(row.StartedAt).Milliseconds() || row.DurationMS <= 0 {
		t.Fatalf("the index closed the run as %+v, want it ended at the program's exit %v with its span", row, exited)
	}
}

// AND AN ORDINARY RUN — no program — is closed at its store's last evidence of
// life, not at the reopen instant.
func TestTheIndexClosesAnOrdinaryRunRowAtItsLastEvidenceOfLife(t *testing.T) {
	var seen time.Time
	agent, id, _ := reopenedWith(t, func(_ *plandb.Store, taskDir string, lastSeen time.Time) {
		if err := os.Remove(filepath.Join(taskDir, delegate.ProgramFile)); err != nil {
			t.Fatal(err)
		}
		seen = lastSeen
		// The reopen comes well after the run was last seen.
		time.Sleep(300 * time.Millisecond)
	})
	var row TaskIndexEntry
	for _, entry := range lastPerNode(ReadTaskIndex(agent.config.taskIndexFile())) {
		if entry.ID == strconv.FormatUint(id, 10) {
			row = entry
		}
	}
	if row.Live() || row.EndedAt.After(seen.Add(100*time.Millisecond)) || row.EndedAt.Before(seen) || row.DurationMS <= 0 {
		t.Fatalf("the index closed the ordinary run as %+v, want it ended where it was last seen, %v", row, seen)
	}
}
