package worker

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestWorkerOldFactoryTableSpendIsIncremental(t *testing.T) {
	var costs []float64
	j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report struct {
			Cost  float64 `json:"cost,omitempty"`
			Steps []struct {
				Name   string          `json:"name"`
				Round  int             `json:"round,omitempty"`
				State  string          `json:"state,omitempty"`
				Result *api.StepResult `json:"result,omitempty"`
			} `json:"steps,omitempty"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&report); err != nil {
			t.Errorf("old factory rejected report: %v", err)
			w.WriteHeader(400)
			return
		}
		costs = append(costs, report.Cost)
	}))
	for _, receipt := range []struct{ table, job float64 }{{.2, .23}, {.25, .28}} {
		if err := j.o.Mirror.Update(j.id, func(it *factory.Item) error {
			it.Stream.Spent = receipt.job
			it.Stream.Phases[0].RoundCosts = map[int]float64{1: receipt.table}
			it.Stream.Phases[0].Result = &factory.StageResult{Spent: receipt.table}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if !j.flush(context.Background()) || j.pending != nil {
			t.Fatal("paid report was not acknowledged")
		}
	}
	if len(costs) != 2 || math.Abs(costs[0]-.23) > 1e-9 || math.Abs(costs[1]-.05) > 1e-9 {
		t.Fatalf("legacy deltas = %v, want [0.23 0.05] including manager spend", costs)
	}
}
