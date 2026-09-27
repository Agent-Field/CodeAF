package session

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestHeldRunReopensOnTheSameAdmissionWithoutPreparingFiles(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	engine := newBeltRunDouble("done")
	registerBeltRunEngine(t, engine)
	place, repo := t.TempDir(), newTestRepo(t)
	profile := t.TempDir()
	open := func() *Agent {
		a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
			c.Workspace, c.Place, c.SessionFile = repo, Place{Dir: place}, filepath.Join(place, placeTranscript)
			c.ProfileDir = profile
			c.AskConsent, c.TaskMaxLoad, c.TaskMinFreeMB = false, 0, 1<<40
		})
		return a
	}
	first := open()
	id, _, _, err := first.StartTask(context.Background(), "held original brief", false)
	if err != nil {
		t.Fatal(err)
	}
	first.beltMu.Lock()
	old := first.beltRun
	first.beltMu.Unlock()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-old.over:
	case <-time.After(5 * time.Second):
		t.Fatal("old driver did not leave")
	}
	second := open()
	second.beltMu.Lock()
	resumed := second.beltRun
	second.beltMu.Unlock()
	if resumed == nil || resumed.row != id || resumed.brief != "held original brief" || resumed.ground != repo {
		t.Fatalf("reopened run = %+v", resumed)
	}
	gate, ok := resumed.admission.(*runAdmission)
	if !ok || gate.governor.settings == nil {
		t.Fatal("recovered admission lost its live profile settings")
	}
	if resumed.workspace != "" || resumed.tree.branch != "" {
		t.Fatal("held recovery prepared a copy")
	}
	second.recoverBeltRun()
	second.beltMu.Lock()
	same := second.beltRun == resumed
	second.beltMu.Unlock()
	if !same || engine.didRun() {
		t.Fatal("repeated recovery duplicated or dispatched held work")
	}
	if _, err := second.Cancel("task:" + strconv.FormatUint(id, 10)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-resumed.over:
	case <-time.After(5 * time.Second):
		t.Fatal("held stop did not settle")
	}
	row := planRowFor(second.PlanTasks(), planStoreID(strconv.FormatUint(id, 10)))
	if row == nil || row.Status != string(plandb.StatusCancelled) {
		t.Fatalf("stopped row = %+v", row)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	third := open()
	third.beltMu.Lock()
	stopped := third.beltRun == nil
	third.beltMu.Unlock()
	if !stopped || engine.didRun() {
		t.Fatal("restart executed cancelled work")
	}
}

func TestRestartReattachesTheRecordedCopyOnce(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	engine := newBeltRunDouble("done")
	registerBeltRunEngine(t, engine)
	place, repo := t.TempDir(), newTestRepo(t)
	open := func() *Agent {
		a, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
			c.Workspace, c.Place, c.SessionFile = repo, Place{Dir: place}, filepath.Join(place, placeTranscript)
			c.AskConsent = false
		})
		return a
	}
	first := open()
	g := first.graph()
	id := g.reserve()
	tree, err := prepareTaskTree(first.config.Place, repo, "1111bbbb1111bbbb", id, "keep work")
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(tree.dir, "previous-work.txt")
	if err := os.WriteFile(sentinel, []byte("keep this"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(g.planPath(), "task", strconv.FormatUint(id, 10), "task", "original brief")
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	first.publishRunRow(g, TaskNotice{ID: id, Title: "task", State: TaskRunning, Copy: runCopyOf(tree), PlanTask: planStoreID(strconv.FormatUint(id, 10))})
	_ = first.Close()
	second := open()
	select {
	case <-engine.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("restart did not reach driver")
	}
	second.beltMu.Lock()
	resumed := second.beltRun
	second.beltMu.Unlock()
	second.recoverBeltRun()
	second.beltMu.Lock()
	same := second.beltRun == resumed
	second.beltMu.Unlock()
	if !same {
		t.Fatal("restart made a second driver")
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep this" {
		t.Fatalf("existing work changed: %q %v", data, err)
	}
	endBeltRun(t, second, engine)
}

func TestRecoveryKeepsJoinedRowsIndependentlyAddressable(t *testing.T) {
	g := &TaskGraph{}
	g.rehydrate(taskDocument{Runs: []runRecord{
		{ID: 1, State: TaskQueued, PlanTask: "t-1", PendingRun: &PendingRunRecord{Ground: "/exact", Brief: "work"}},
		{ID: 2, Parent: 1, State: TaskRunning, PlanTask: "t-2"},
		{ID: 3, Parent: 1, State: TaskFailed, Stopped: true, PlanTask: "t-3"},
	}}, "", "")
	if rows := g.runRows(1); len(rows) != 1 {
		t.Fatalf("root absorbed its children: %+v", rows)
	}
	if rows := g.runRows(2); len(rows) != 1 || rows[0].State != TaskInterrupted {
		t.Fatalf("joined row lost: %+v", rows)
	}
	ids := recoveredJoinedRows(g, 1)
	if len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("completion obligations = %v", ids)
	}
	if rows := g.runRows(3); len(rows) != 1 || !rows[0].Stopped {
		t.Fatalf("stopped child changed: %+v", rows)
	}
}

func TestRestartNeverExecutesUnrecoverableOrCompletedPlans(t *testing.T) {
	for _, scenario := range []string{"missing-copy", "completed", "bad-pending-ground", "bad-pending-mode"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", "bash")
			engine := newBeltRunDouble("must not run")
			registerBeltRunEngine(t, engine)
			place, repo := t.TempDir(), newTestRepo(t)
			open := func() *Agent {
				a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
					c.Place, c.Workspace, c.SessionFile = Place{Dir: place}, repo, filepath.Join(place, placeTranscript)
					c.AskConsent = false
				})
				return a
			}
			first := open()
			g := first.graph()
			id := g.reserve()
			store, err := plandb.Open(g.planPath(), "task", strconv.FormatUint(id, 10), "task", "work")
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "completed" {
				if err := store.CompleteRoot("real recorded output"); err != nil {
					t.Fatal(err)
				}
			}
			_ = store.Close()
			row := TaskNotice{ID: id, State: TaskRunning, Title: "task", PlanTask: planStoreID(strconv.FormatUint(id, 10))}
			if scenario == "bad-pending-ground" {
				row.PendingRun = &PendingRunRecord{Brief: "work", Ground: "relative", Mode: TaskModeWorktree}
			}
			if scenario == "bad-pending-mode" {
				row.PendingRun = &PendingRunRecord{Brief: "work", Ground: repo, Mode: "unknown"}
			}
			first.publishRunRow(g, row)
			_ = first.Close()
			second := open()
			nothingStarted(t, second, engine)
			got, found := runRowOf(second.graph(), id)
			if !found || got.State != TaskInterrupted || got.Report == "" {
				t.Fatalf("unrecoverable record not actionable: %+v", got)
			}
			_ = second.Close()
			third := open()
			nothingStarted(t, third, engine)
			got, _ = runRowOf(third.graph(), id)
			if got.State != TaskInterrupted {
				t.Fatalf("second restart changed interruption: %+v", got)
			}
		})
	}
}

func TestRecoveringAPendingProgramDoesNotEndItsNewDriver(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	engine := newBeltRunDouble("must remain held")
	registerBeltRunEngine(t, engine)
	place, repo := t.TempDir(), newTestRepo(t)
	open := func() *Agent {
		a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
			c.Place, c.Workspace, c.SessionFile = Place{Dir: place}, repo, filepath.Join(place, placeTranscript)
			c.AskConsent, c.TaskMaxLoad, c.TaskMinFreeMB = false, 0, 1<<40
			c.Delegates = testPrograms("fake")
		})
		return a
	}
	first := open()
	g := first.graph()
	id := g.reserve()
	store, err := plandb.Open(g.planPath(), "program", strconv.FormatUint(id, 10), "program", "work")
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	first.publishRunRow(g, TaskNotice{ID: id, Title: "program", State: TaskQueued, Program: "fake", PendingRun: &PendingRunRecord{Brief: "work", Ground: repo, Mode: TaskModeWorktree}})
	_ = first.Close()
	second := open()
	second.beltMu.Lock()
	run := second.beltRun
	second.beltMu.Unlock()
	if run == nil || run.delegate == nil || run.delegate.Name != "fake" {
		t.Fatal("pending program did not recover its own driver")
	}
	if row, _ := runRowOf(second.graph(), id); row.State != TaskQueued {
		t.Fatalf("program recovery ended a fresh driver: %+v", row)
	}
	if root := run.store.Task(run.root); root == nil || terminalStoreStatus(root.Status) {
		t.Fatalf("program recovery ended its plan: %+v", root)
	}
	if engine.didRun() {
		t.Fatal("held program reached engine")
	}
	_, _ = second.Cancel("task:" + strconv.FormatUint(id, 10))
	select {
	case <-run.over:
	case <-time.After(5 * time.Second):
		t.Fatal("pending program did not stop")
	}
}
