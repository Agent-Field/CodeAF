package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
)

// Contract: scheduler pumps run concurrently with tool execution, so a
// guard-bound task with an in-flight tool call is live work — the drain must
// not sweep-close it, must not release it, and must not accrue stall credit
// while it runs. Once the tool call ends the ordinary sweep applies. This
// drives the real registry union (runner.runtime.registry), the path run N
// exposed when the sweep closed t-8zxe seventeen seconds before its command
// finished.
func TestRootSchedulerTreatsInFlightGuardTaskAsLiveWork(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("drain-inflight-guard")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-inflight-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DoneTask(root.ID, plandb.DoneOpts{Result: "root complete"}); err != nil {
		t.Fatal(err)
	}
	description := "Harness-created direct verification package.\n\ntask_role: qa\naccess: read"
	bookkeeping, err := db.AddTask(plandb.AddTaskInput{
		Title: "Direct verification: go test ./...", Description: &description,
		Project: projectRow.ID, Parent: root.ID, CustomID: "t-inflight-pkg", Kind: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.ClaimTask(bookkeeping.ID, "root-orchestrator") == nil || db.StartTask(bookkeeping.ID) == nil {
		t.Fatal("could not stage the running bookkeeping task")
	}

	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{Events: newEventWriter(io.Discard), Notes: io.Discard})
	t.Cleanup(runner.runtime.Close)
	endGuardTask := runner.runtime.registry.BeginGuardTask(string(bookkeeping.ID))
	t.Cleanup(endGuardTask)

	plan := steploop.PlanDBInfo{ProjectID: projectRow.ID, RootTaskID: root.ID}
	pump := runner.rootScheduler(&rootSchedulerScript{t: t, wantPlan: plan}).(*pipelineRootScheduler)
	pump.sweepMinAge = 0
	for cycle := 1; cycle <= stallCycleThreshold+1; cycle++ {
		summary, pumpErr := pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
		if pumpErr != nil {
			t.Fatalf("cycle %d stalled while a guard-bound tool call was in flight: %v", cycle, pumpErr)
		}
		if !strings.Contains(summary, "open descendant") {
			t.Fatalf("cycle %d summary = %q, want a keep-alive continuation", cycle, summary)
		}
	}
	if got := db.GetTask(bookkeeping.ID); got == nil || got.Status != plandb.StatusRunning {
		t.Fatalf("in-flight guard task was swept or released: %#v", got)
	}
	if pump.quietCycles != 0 {
		t.Fatalf("in-flight guard task accrued stall credit: quietCycles=%d", pump.quietCycles)
	}

	// Once the tool call ends the task is a leak again: the ordinary sweep
	// closes it and the cycle does not count as quiet.
	endGuardTask()
	summary, pumpErr := pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
	if pumpErr != nil || summary != "" {
		t.Fatalf("post-completion sweep cycle summary=%q error=%v", summary, pumpErr)
	}
	if got := db.GetTask(bookkeeping.ID); got == nil || got.Status != plandb.StatusDone ||
		!strings.Contains(string(got.Result), "auto-closed by root drain") {
		t.Fatalf("completed guard task was not swept closed: %#v", got)
	}
}
