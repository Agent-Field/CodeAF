package codeaf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
)

func TestDispatchLoopExhaustionStopsBeforeAudit(t *testing.T) {
	// C3/C6: the loop cap is a bound, not permission to audit open work.
	t.Setenv("CODEAF_DISPATCH_MAX_LOOPS", "1")
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	var events bytes.Buffer
	runner := newExitGuardTestPipeline(t, nil)
	runner.events = newEventWriter(&events)
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	runner.runSchedulerCycle = func(
		context.Context, *scheduler.Scheduler, scheduler.SchedulerInput,
	) (scheduler.SchedulerCycleResult, error) {
		return scheduler.SchedulerCycleResult{}, nil
	}
	parent := plandb.TaskID("root")
	runner.listPlanTasks = func(*plandb.ListTasksFilter) []*plandb.Task {
		return []*plandb.Task{
			{ID: "root", Title: "root", ProjectID: "project", Status: plandb.StatusRunning},
			{ID: "active-work", Title: "unfinished work", ProjectID: "project", ParentTaskID: &parent, Status: plandb.StatusReady},
		}
	}

	err := runner.dispatchUntilQuiet(context.Background(), "project", "root", "")
	if !errors.Is(err, errRootDrainStalled) || !strings.Contains(err.Error(), "active-work") ||
		!strings.Contains(err.Error(), "unfinished work") {
		t.Fatalf("dispatch exhaustion = %v", err)
	}
	if !strings.Contains(events.String(), `"status":"exhausted"`) {
		t.Fatalf("missing exhaustion event: %s", events.String())
	}
}

func TestDispatchStuckSilenceContractAndKillSwitch(t *testing.T) {
	// C3/C6 preserves the Round 3 silence timing and kill switch, but neither a
	// silence cut nor the hard loop cap may send an open graph to audit.
	for _, test := range []struct {
		name       string
		noSilence  string
		wantCycles int
		wantStuck  bool
	}{
		{name: "fires", wantCycles: 2, wantStuck: true},
		{name: "kill-switch", noSilence: "1", wantCycles: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
			t.Setenv("CODEAF_NO_SILENCE", test.noSilence)
			t.Setenv("CODEAF_DISPATCH_MAX_LOOPS", "3")
			var events bytes.Buffer
			runner := newExitGuardTestPipeline(t, nil)
			runner.events = newEventWriter(&events)
			now := time.UnixMilli(1_000)
			runner.now = func() time.Time { return now }
			runner.runtime.now = runner.now
			runner.sleep = func(context.Context, time.Duration) error {
				now = now.Add(91 * time.Second)
				return nil
			}
			cycles := 0
			runner.runSchedulerCycle = func(
				context.Context, *scheduler.Scheduler, scheduler.SchedulerInput,
			) (scheduler.SchedulerCycleResult, error) {
				cycles++
				return scheduler.SchedulerCycleResult{}, nil
			}
			parent := plandb.TaskID("root")
			runner.listPlanTasks = func(*plandb.ListTasksFilter) []*plandb.Task {
				return []*plandb.Task{
					{ID: "root", Title: "root", ProjectID: "project", Status: plandb.StatusRunning},
					{ID: "ready", Title: "still open", ProjectID: "project", ParentTaskID: &parent, Status: plandb.StatusReady},
				}
			}
			err := runner.dispatchUntilQuiet(context.Background(), "project", "root", "")
			if !errors.Is(err, errRootDrainStalled) {
				t.Fatalf("open graph result = %v", err)
			}
			if cycles != test.wantCycles {
				t.Fatalf("scheduler cycles = %d, want %d", cycles, test.wantCycles)
			}
			gotStuck := strings.Contains(events.String(), `"status":"stuck-silence"`)
			if gotStuck != test.wantStuck {
				t.Fatalf("stuck event=%v, want %v: %s", gotStuck, test.wantStuck, events.String())
			}
		})
	}
}

func TestDispatchStopsImmediatelyWhenDependenciesAreImpossible(t *testing.T) {
	// F11.1: a pending descendant whose hard dependency is terminal can never
	// enter the ready frontier, so the drain diagnoses it before any sleep.
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	t.Setenv("CODEAF_DISPATCH_MAX_LOOPS", "8")
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	project := db.Init("dependency-impossible")
	root, err := db.AddTask(plandb.AddTaskInput{Title: "root", Project: project.ID, CustomID: "root"})
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := db.AddTask(plandb.AddTaskInput{Title: "failed prerequisite", Project: project.ID, Parent: root.ID, CustomID: "failed-dep"})
	if err != nil {
		t.Fatal(err)
	}
	db.FailTask(upstream.ID, "cannot complete", nil)
	blocked, err := db.AddTask(plandb.AddTaskInput{
		Title: "blocked child", Project: project.ID, Parent: root.ID, CustomID: "blocked-child",
		Deps: []plandb.DepSpec{{TaskID: upstream.ID}},
	})
	if err != nil || blocked.Status != plandb.StatusPending {
		t.Fatalf("blocked task = %#v, %v", blocked, err)
	}
	runner := newExitGuardTestPipeline(t, nil)
	sleeps, cycles := 0, 0
	runner.sleep = func(context.Context, time.Duration) error { sleeps++; return nil }
	runner.runSchedulerCycle = func(context.Context, *scheduler.Scheduler, scheduler.SchedulerInput) (scheduler.SchedulerCycleResult, error) {
		cycles++
		return scheduler.SchedulerCycleResult{QuietReason: scheduler.CycleQuietNothingReady}, nil
	}
	err = runner.dispatchUntilQuiet(context.Background(), project.ID, root.ID, "")
	if !errors.Is(err, errRootDrainStalled) || !strings.Contains(err.Error(), "dependency-impossible") ||
		!strings.Contains(err.Error(), "failed-dep (failed)") {
		t.Fatalf("dependency-impossible result = %v", err)
	}
	if sleeps != 0 || cycles != 1 {
		t.Fatalf("dependency-impossible drain sleeps=%d cycles=%d, want 0/1", sleeps, cycles)
	}
}

func TestDependencyImpossibleRequiresTerminalBlockerForEveryOpenTask(t *testing.T) {
	parent := plandb.TaskID("root")
	all := []*plandb.Task{
		{ID: "root", Status: plandb.StatusRunning},
		{ID: "cancelled", Status: plandb.StatusCancelled, ParentTaskID: &parent},
		{ID: "blocked", Status: plandb.StatusPending, ParentTaskID: &parent},
	}
	deps := []plandb.Dependency{{FromTask: "cancelled", ToTask: "blocked", Kind: plandb.DepBlocks}}
	if impossible, diagnosis := dependencyImpossibleDrain(all[2:], all, deps); !impossible || !strings.Contains(diagnosis, "cancelled") {
		t.Fatalf("cancelled dependency diagnosis = %v, %q", impossible, diagnosis)
	}
	ready := &plandb.Task{ID: "ready", Status: plandb.StatusReady, ParentTaskID: &parent}
	if impossible, _ := dependencyImpossibleDrain([]*plandb.Task{all[2], ready}, append(all, ready), deps); impossible {
		t.Fatal("ready work was classified as dependency-impossible")
	}
}
