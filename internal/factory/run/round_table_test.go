package run_test

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// tableReport exercises the worker JSON, including workers that coalesce or
// resend reports. The table is the worker's complete receipt, not a delta.
func tableReport(t *testing.T, round int, rows map[int]float64) factory.RunReport {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"steps": []any{map[string]any{
		"name": "proof", "round": round, "state": "running", "round_costs": rows,
		"result": map[string]any{"done": true, "cost": rows[round]},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var wire api.WorkerReport
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	return api.ToWorkerReport(wire)
}

func checkTableCost(t *testing.T, it factory.Item, want float64) {
	t.Helper()
	if got := api.FromItem(it).Steps[0].Cost; math.Abs(got-want) > 1e-9 {
		t.Fatalf("worker step cost = %.4f, want %.4f", got, want)
	}
	if got := factory.JobSpent(it); got+1e-9 < want {
		t.Fatalf("job spend = %.4f, below real spend %.4f", got, want)
	}
}

func TestStepCostWorkerTableCoalescedRetryRestart(t *testing.T) {
	it := reviewItem()
	apply := func(r factory.RunReport) {
		t.Helper()
		if err := run.ApplyReport(&it, r, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	apply(tableReport(t, 2, map[int]float64{1: .2025, 2: .0153}))
	checkTableCost(t, it, .2178)
	stale := tableReport(t, 1, map[int]float64{1: .2025})
	apply(stale)
	checkTableCost(t, it, .2178)
	// Restart mid-round through the actual worker admission wire.
	it = api.ToItem(api.WorkerJob(it))
	apply(tableReport(t, 3, map[int]float64{1: .2025, 2: .0153, 3: .004}))
	it = api.ToItem(api.WorkerJob(it))
	final := tableReport(t, 3, map[int]float64{1: .2025, 2: .0153, 3: .01})
	apply(final)
	apply(final)
	apply(stale)
	checkTableCost(t, it, .2278)
}

func TestJobSpendOldPendingWorkerTable(t *testing.T) {
	it := reviewItem()
	it.Stream.Spent = .2025
	it.Stream.Phases[0] = factory.Phase{Name: "proof", Kind: factory.StageChat, State: factory.PhasePending, Round: 2, Result: &factory.StageResult{Spent: .2025}}
	if err := run.ApplyReport(&it, tableReport(t, 2, map[int]float64{1: .2025, 2: .0153}), time.Now()); err != nil {
		t.Fatal(err)
	}
	checkTableCost(t, it, .2178)
}

func TestStepCostWorkerTableSeededSequences(t *testing.T) {
	for seed := int64(0); seed < 32; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			it := reviewItem()
			worker := reviewItem()
			worker.State = factory.StateNew
			if err := run.QueueItem(&worker, time.Now()); err != nil {
				t.Fatal(err)
			}
			worker.State = factory.StateRunning
			worker.Stream.Phases[0].Round = 1
			worker.Stream.Phases[0].State = factory.PhaseRunning
			clone := func(it factory.Item) factory.Item {
				raw, _ := json.Marshal(it)
				var out factory.Item
				if err := json.Unmarshal(raw, &out); err != nil {
					t.Fatal(err)
				}
				return out
			}
			baseline := clone(worker)
			report := func() factory.RunReport {
				r, _ := run.Diff(baseline, worker)
				raw, _ := json.Marshal(api.FromRunReport(r))
				var wire api.WorkerReport
				if err := json.Unmarshal(raw, &wire); err != nil {
					t.Fatal(err)
				}
				return api.ToWorkerReport(wire)
			}
			real := 0.0
			var pending []factory.RunReport
			flush := func(r factory.RunReport) {
				t.Helper()
				if err := run.ApplyReport(&it, r, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			for n := 0; n < 120; n++ {
				switch rng.Intn(8) {
				case 0, 1, 2: // Spend, including resumed work in the same logical round.
					usd := float64(1+rng.Intn(20)) / 10000
					ph := &worker.Stream.Phases[0]
					ph.RecordRoundSpend(ph.Round, ph.RoundCosts[ph.Round]+usd)
					worker.Stream.Spent += usd
					real += usd
				case 3: // Retry starts another row; old rounds stay in the worker table.
					if err := run.StepControl(&worker, "proof", true, "", time.Now()); err != nil {
						t.Fatal(err)
					}
					worker.State = factory.StateRunning
					worker.Stream.Phases[0].State = factory.PhaseRunning
				case 4: // Reports can be coalesced or dropped before a later full table.
					pending = append(pending, report())
					baseline = clone(worker)
				case 5: // Reorder and repeat a report.
					if len(pending) > 0 {
						r := pending[rng.Intn(len(pending))]
						flush(r)
						flush(r)
					}
				case 6: // Factory restart and worker admission mid-round.
					raw, _ := json.Marshal(it)
					if err := json.Unmarshal(raw, &it); err != nil {
						t.Fatal(err)
					}
					if err := run.PauseItem(&worker, time.Now()); err != nil {
						t.Fatal(err)
					}
					worker = api.ToItem(api.WorkerJob(worker))
					if err := run.ResumeItem(&worker, time.Now()); err != nil {
						t.Fatal(err)
					}
					worker.State = factory.StateRunning
				case 7: // A fresh snapshot recovers all dropped and coalesced rounds.
					baseline = reviewItem()
					flush(report())
					checkTableCost(t, it, real)
				}
			}
			baseline = reviewItem()
			flush(report())
			checkTableCost(t, it, real)
			if got := factory.JobSpent(it); math.Abs(got-real) > 1e-9 {
				t.Fatalf("job spend doubled: %.4f, real %.4f", got, real)
			}
		})
	}
}

func TestStepCostWorkerTableRetryPullAndFreshRun(t *testing.T) {
	it := reviewItem()
	if err := run.ApplyReport(&it, tableReport(t, 1, map[int]float64{1: .2}), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := run.StepControl(&it, "proof", true, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	it = api.ToItem(api.WorkerJob(it))
	if err := run.ApplyReport(&it, tableReport(t, 2, map[int]float64{1: .2, 2: .01}), time.Now()); err != nil {
		t.Fatal(err)
	}
	it = api.ToItem(api.WorkerJob(it))
	checkTableCost(t, it, .21)
	// Worker admission must carry the receipt, not reconstruct a baseline
	// from the single app-facing cost number.
	raw, _ := json.Marshal(api.WorkerJob(it))
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	steps := wire["steps"].([]any)
	if _, ok := steps[0].(map[string]any)["round_costs"]; !ok {
		t.Fatal("worker pull lost round table")
	}
	it.State = factory.StateNew
	if err := run.QueueItem(&it, time.Now()); err != nil {
		t.Fatal(err)
	}
	checkTableCost(t, it, 0)
	if got := factory.JobSpent(it); got != 0 {
		t.Fatalf("fresh run job spend = %g, want 0", got)
	}
}

func TestStepCostLegacyWorkerLastSpentFallback(t *testing.T) {
	var it factory.Item
	raw := []byte(`{"State":"running","Stages":[{"Name":"proof","Kind":"chat","On":true}],"Stream":{"Spent":0.2,"Phases":[{"Name":"proof","Kind":"chat","Round":1,"Result":{"Spent":0.2}}]}}`)
	if err := json.Unmarshal(raw, &it); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(it)
	if got := api.FromItem(it).Steps[0].Cost; got != .2 {
		t.Fatalf("old step cost %g, want .2", got)
	}
	after, _ := json.Marshal(it)
	if string(before) != string(after) {
		t.Fatal("reading old job rewrote saved rows")
	}
	it = api.ToItem(api.WorkerJob(it))
	old := api.WorkerReport{Cost: .01, Steps: []api.StepReport{{Name: "proof", Round: 2, Result: &api.StepResult{Cost: .01}}}}
	if err := run.ApplyReport(&it, api.ToWorkerReport(old), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := api.FromItem(it).Steps[0].Cost; got != .01 {
		t.Fatalf("old worker cost %g, want last Spent .01", got)
	}
	if got := factory.JobSpent(it); math.Abs(got-.21) > 1e-9 {
		t.Fatalf("old worker job spend %g, want .21", got)
	}
}
