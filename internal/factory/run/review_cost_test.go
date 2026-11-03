package run_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func reviewApply(t *testing.T, it *factory.Item, round int, cost float64) {
	t.Helper()
	r := factory.StageResult{Spent: cost, Chat: map[int]string{1: "first", 2: "second"}[round]}
	if err := run.ApplyReport(it, factory.RunReport{Stages: []factory.StageReport{{Name: "proof", Round: round, Result: &r}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func reviewItem() factory.Item {
	return factory.Item{State: factory.StateRunning,
		Stages: []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat}}}}
}

func TestReviewOldPendingRound(t *testing.T) {
	var it factory.Item
	if err := json.Unmarshal([]byte(`{"State":"running","Stages":[{"Name":"proof","Kind":"chat","On":true}],"Stream":{"Phases":[{"Name":"proof","Kind":"chat","State":"pending","Round":2,"Result":{"Spent":0.2025,"Chat":"first"},"Rounds":[{"Round":1,"Chat":"first"}]}]}}`), &it); err != nil {
		t.Fatal(err)
	}
	reviewApply(t, &it, 2, .0153)
	if got := api.FromItem(it).Steps[0].Cost; math.Abs(got-.2178) > 1e-9 {
		t.Fatalf("old pending round: step cost = %.4f, want .2178", got)
	}
}

func TestReviewCoalescedWorkerRounds(t *testing.T) {
	prev, cur := reviewItem(), reviewItem()
	reviewApply(t, &cur, 1, .2025)
	reviewApply(t, &cur, 2, .0153)
	report, _ := run.Diff(prev, cur)
	report = api.ToWorkerReport(api.FromRunReport(report))
	if err := run.ApplyReport(&prev, report, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := api.FromItem(prev).Steps[0].Cost; math.Abs(got-.2178) > 1e-9 {
		t.Fatalf("coalesced worker rounds: step cost = %.4f, want .2178", got)
	}
}

func TestReviewRerunRetainsStepCost(t *testing.T) {
	it := factory.Item{State: factory.StateNeedsYou, Cap: 10,
		Stages: []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Spent: .2, Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat, State: factory.PhaseFailed, Round: 1,
			Cost: .2, CostRound: 1, RoundSpent: .2, Result: &factory.StageResult{Spent: .2}}}}}
	if err := run.StepControl(&it, "proof", true, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	result := factory.StageResult{Spent: .01}
	if err := run.ApplyReport(&it, factory.RunReport{Stages: []factory.StageReport{{Name: "proof", Round: 2, Result: &result}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := api.FromItem(it).Steps[0].Cost; math.Abs(got-.21) > 1e-9 {
		t.Fatalf("rerun step cost = %.4f, want .2100", got)
	}
}

func TestReviewOldPendingWorkerWithoutChatHistory(t *testing.T) {
	it := reviewItem()
	it.Stream.Spent = .2025
	it.Stream.Phases[0] = factory.Phase{Name: "proof", Kind: factory.StageChat, State: factory.PhasePending,
		Round: 2, Result: &factory.StageResult{Spent: .2025}}
	pulled := api.ToItem(api.WorkerJob(it))
	reviewApply(t, &pulled, 2, .0153)
	if got := api.FromItem(pulled).Steps[0].Cost; math.Abs(got-.2178) > 1e-9 {
		t.Fatalf("old pending worker step cost = %.4f, want .2178", got)
	}
}

func TestReviewCoalescedCostPreservesPersonTailEdit(t *testing.T) {
	prev, cur := reviewItemWithDocs(), reviewItemWithDocs()
	reviewApply(t, &cur, 1, .2025)
	reviewApply(t, &cur, 2, .0153)
	if _, err := run.EditStepsItem(&prev, factory.RunEdit{By: factory.ByYou, Remove: []string{"docs"}}, factory.DefaultRecipe()); err != nil {
		t.Fatal(err)
	}
	report, _ := run.Diff(reviewItemWithDocs(), cur)
	report = api.ToWorkerReport(api.FromRunReport(report))
	if err := run.ApplyReport(&prev, report, time.Now()); err != nil {
		t.Fatal(err)
	}
	if factory.StageIndex(prev.Stages, "docs") >= 0 {
		t.Fatal("cost snapshot restored the tail step removed by the person")
	}
	if got := api.FromItem(prev).Steps[0].Cost; math.Abs(got-.2178) > 1e-9 {
		t.Fatalf("coalesced worker step cost after edit = %.4f, want .2178", got)
	}
}

func reviewItemWithDocs() factory.Item {
	it := reviewItem()
	it.Stages = append(it.Stages, factory.Stage{Name: "docs", Kind: factory.StageChat, Ask: "write docs", On: true})
	it.Stream.Phases = append(it.Stream.Phases, factory.Phase{Name: "docs", Kind: factory.StageChat, State: factory.PhasePending})
	return it
}
