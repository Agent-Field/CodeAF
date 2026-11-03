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

func TestStepCostAcrossRounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		rounds []int
		costs []float64
		want float64
	}{
		{"two rounds sum", []int{1, 2}, []float64{0.2025, 0.0153}, 0.2178},
		{"duplicate result counts once", []int{1, 2, 2}, []float64{0.2025, 0.0153, 0.0153}, 0.2178},
		{"resumed running total replaces round", []int{1, 2, 2}, []float64{0.2025, 0.01, 0.0153}, 0.2178},
		{"one round unchanged", []int{1}, []float64{0.2025}, 0.2025},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := factory.Item{State: factory.StateRunning,
				Stages: []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true}},
				Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat}}}}
			for i, round := range tc.rounds {
				err := run.ApplyReport(&it, factory.RunReport{Stages: []factory.StageReport{{
					Name: "proof", Round: round, State: factory.PhaseRunning,
					Result: &factory.StageResult{Done: true, Spent: tc.costs[i]},
				}}}, time.Date(2026, 11, 3, 0, i, 0, 0, time.UTC))
				if err != nil { t.Fatal(err) }
				// Resume from persisted state, rather than a process-local accumulator.
				data, err := json.Marshal(it)
				if err != nil { t.Fatal(err) }
				if err := json.Unmarshal(data, &it); err != nil { t.Fatal(err) }
			}
			got := api.FromItem(it).Steps[0].Cost
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("step cost = %.4f, want %.4f", got, tc.want)
			}
		})
	}
}

func TestStepCostAfterWorkerPullBetweenRounds(t *testing.T) {
 it:=factory.Item{State:factory.StateRunning,Stages:[]factory.Stage{{Name:"proof",Kind:factory.StageChat,On:true}},Stream:&factory.Stream{Phases:[]factory.Phase{{Name:"proof",Kind:factory.StageChat}}}}
 apply:=func(item *factory.Item,round int,cost float64,chat string) {
  t.Helper()
  result:=factory.StageResult{Spent:cost,Chat:chat}
  if err:=run.ApplyReport(item,factory.RunReport{Stages:[]factory.StageReport{{Name:"proof",Round:round,Result:&result}}},time.Now());err!=nil {t.Fatal(err)}
 }
 apply(&it,1,.2025,"first")
 if err:=run.ApplyReport(&it,factory.RunReport{Stages:[]factory.StageReport{{Name:"proof",Round:2,State:factory.PhaseRunning}}},time.Now());err!=nil {t.Fatal(err)}
 pulled:=api.ToItem(api.WorkerJob(it))
 apply(&pulled,2,.0153,"second")
 if got:=api.FromItem(pulled).Steps[0].Cost;math.Abs(got-.2178)>1e-9 {t.Fatalf("step cost after worker pull = %.4f, want .2178",got)}
}

func TestStepCostOldSavedJobNextRound(t *testing.T) {
 var it factory.Item
 if err:=json.Unmarshal([]byte(`{"State":"running","Stages":[{"Name":"proof","Kind":"chat","On":true}],"Stream":{"Phases":[{"Name":"proof","Kind":"chat","Round":1,"Result":{"Spent":0.2025,"Chat":"first"},"Rounds":[{"Round":1,"Chat":"first"}]}]}}`),&it);err!=nil {t.Fatal(err)}
 result:=factory.StageResult{Spent:.0153,Chat:"second"}
 if err:=run.ApplyReport(&it,factory.RunReport{Stages:[]factory.StageReport{{Name:"proof",Round:2,Result:&result}}},time.Now());err!=nil {t.Fatal(err)}
 if got:=api.FromItem(it).Steps[0].Cost;math.Abs(got-.2178)>1e-9 {t.Fatalf("old saved step cost = %.4f, want .2178",got)}
}
