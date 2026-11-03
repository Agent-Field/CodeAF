package api

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestJobWireKeepsRunAndLifetimeCostsSeparate(t *testing.T) {
	for _, stream := range []*factory.Stream{nil, {Spent: .03}} {
		it := factory.Item{SpentBefore: .138, Stream: stream}
		job := FromItem(it)
		raw, err := json.Marshal(job)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		got, ok := fields["lifetime_cost"].(float64)
		if !ok || math.Abs(got-factory.JobLifetimeSpent(it)) > 1e-9 {
			t.Fatalf("lifetime_cost = %v, want %g", fields["lifetime_cost"], factory.JobLifetimeSpent(it))
		}
		if job.Cost != factory.JobSpent(it) {
			t.Fatalf("run cost changed: %g", job.Cost)
		}
		var decoded Job
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if got := factory.JobLifetimeSpent(ToItem(decoded)); math.Abs(got-factory.JobLifetimeSpent(it)) > 1e-9 {
			t.Fatalf("mirror lifetime = %g", got)
		}
	}
}
