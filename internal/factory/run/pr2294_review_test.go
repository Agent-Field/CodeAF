package run_test

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"math"
	"testing"
	"time"
)

func TestPR2294StaleWorkerNewSpend(t *testing.T) {
	it := reviewItem()
	for _, rows := range []map[int]float64{{1: .2, 2: .1}, {1: .25}} {
		r := factory.RunReport{Stages: []factory.StageReport{{Name: "proof", RoundCosts: rows}}}
		if err := run.ApplyReport(&it, r, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if got := factory.JobSpent(it); math.Abs(got-.35) > 1e-9 {
		t.Fatalf("stale worker spend lost: job=%.2f, want 0.35", got)
	}
}

func TestPR2294OldWorkerAfterTableAdmission(t *testing.T) {
	it := reviewItem()
	if err := run.ApplyReport(&it, tableReport(t, 1, map[int]float64{1: .2}), time.Now()); err != nil {
		t.Fatal(err)
	}
	old := api.WorkerReport{Cost: .01, Steps: []api.StepReport{{Name: "proof", Round: 2, Result: &api.StepResult{Cost: .01}}}}
	if err := run.ApplyReport(&it, api.ToWorkerReport(old), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := api.FromItem(it).Steps[0].Cost; math.Abs(got-.01) > 1e-9 {
		t.Fatalf("old worker fallback step cost=%.2f, want last Spent 0.01", got)
	}
}

func TestPR2294NewWorkerNormalReportsKeepTable(t *testing.T) {
	it := reviewItem()
	if err := run.ApplyReport(&it, tableReport(t, 1, map[int]float64{1: .2}), time.Now()); err != nil {
		t.Fatal(err)
	}
	previous := api.ToItem(api.WorkerJob(it))
	raw, _ := json.Marshal(previous)
	var current factory.Item
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	current.Stream.Phases[0].Result = &factory.StageResult{Spent: .01, Output: "progress"}
	report, _ := run.Diff(previous, current)
	if err := run.ApplyReport(&it, api.ToWorkerReport(api.FromRunReport(report, true)), time.Now()); err != nil {
		t.Fatal(err)
	}
	checkTableCost(t, it, .2)
	if err := run.ApplyReport(&it, factory.RunReport{Stages: []factory.StageReport{{Name: "proof", State: factory.PhaseRunning}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	checkTableCost(t, it, .2)
}
