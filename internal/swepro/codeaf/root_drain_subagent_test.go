package codeaf

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
)

// Contract (run-T shape): leaked "Harness-created subagent task." bookkeeping
// packages are closed by the quiet-drain sweep like the Direct packages —
// including a composite parent whose nested subagent child leaked under it,
// which must close after its child in the same sweep pass. Live in-flight
// packages and real planner tasks keep their existing treatment.
func TestRootDrainSweepClosesLeakedSubagentPackages(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("drain-subagent-leak")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-subroot",
	})
	if err != nil {
		t.Fatal(err)
	}

	subagentDescription := "Harness-created subagent task.\n\nparent_session_id: ses-root\nsession_id: ses-audit\nsubagent_type: explorer"
	parentPkg, err := db.AddTask(plandb.AddTaskInput{
		Title: "Audit completion of clock.go program", Description: &subagentDescription,
		Project: projectRow.ID, Parent: root.ID, CustomID: "t-audit-parent", Kind: "research",
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.ClaimTask(parentPkg.ID, "explorer:ses-audit") == nil || db.StartTask(parentPkg.ID) == nil {
		t.Fatal("could not stage the running parent package")
	}
	nestedDescription := "Harness-created subagent task.\n\nparent_session_id: ses-audit\nsession_id: ses-nested\nsubagent_type: explorer"
	nestedPkg, err := db.AddTask(plandb.AddTaskInput{
		Title: "Audit completion of clock.go program", Description: &nestedDescription,
		Project: projectRow.ID, Parent: parentPkg.ID, CustomID: "t-audit-nested", Kind: "research",
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.ClaimTask(nestedPkg.ID, "explorer:ses-nested") == nil || db.StartTask(nestedPkg.ID) == nil {
		t.Fatal("could not stage the running nested package")
	}

	liveDescription := "Harness-created subagent task.\n\nparent_session_id: ses-root\nsession_id: ses-live\nsubagent_type: coder"
	livePkg, err := db.AddTask(plandb.AddTaskInput{
		Title: "Implement helper", Description: &liveDescription,
		Project: projectRow.ID, Parent: root.ID, CustomID: "t-live-dispatch", Kind: "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.ClaimTask(livePkg.ID, "coder:ses-live") == nil || db.StartTask(livePkg.ID) == nil {
		t.Fatal("could not stage the live package")
	}

	plannerDescription := "Real planner work the model claimed directly."
	plannerTask, err := db.AddTask(plandb.AddTaskInput{
		Title: "Polish docs", Description: &plannerDescription,
		Project: projectRow.ID, Parent: root.ID, CustomID: "t-planner", Kind: "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.ClaimTask(plannerTask.ID, "root-orchestrator") == nil {
		t.Fatal("could not claim the planner task")
	}

	open := openDescendantTasksFrom(
		db.ListTasks(&plandb.ListTasksFilter{Project: projectRow.ID}), string(root.ID),
	)
	youngSweep := sweepRootDrainTasks(db, open, []string{string(livePkg.ID)}, rootDrainSweepMinAge)
	if youngSweep.madeProgress() {
		t.Fatalf("sweep touched recently updated tasks: closed=%v released=%v", youngSweep.closed, youngSweep.released)
	}
	sweep := sweepRootDrainTasks(db, open, []string{string(livePkg.ID)}, 0)

	closed := map[string]bool{}
	for _, id := range sweep.closed {
		closed[id] = true
	}
	if !closed[string(nestedPkg.ID)] || !closed[string(parentPkg.ID)] {
		t.Fatalf("sweep closed = %v, want both leaked subagent packages", sweep.closed)
	}
	if closed[string(livePkg.ID)] {
		t.Fatalf("sweep closed the live in-flight package: %v", sweep.closed)
	}
	if len(sweep.released) != 1 || sweep.released[0] != string(plannerTask.ID) {
		t.Fatalf("sweep released = %v, want the root-claimed planner task", sweep.released)
	}
	for id, wantStatus := range map[plandb.TaskID]plandb.TaskStatus{
		nestedPkg.ID: plandb.StatusDone,
		parentPkg.ID: plandb.StatusDone,
		livePkg.ID:   plandb.StatusRunning,
	} {
		row := db.GetTask(id)
		if row == nil || row.Status != wantStatus {
			t.Fatalf("task %s status = %v, want %s", id, row, wantStatus)
		}
	}
}
