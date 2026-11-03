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
