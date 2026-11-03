package run_test

import (
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestRestartKeepsJobLifetimeSpend(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	it := factory.Item{State: factory.StateNew, Cap: .1,
		Stages: []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Spent: .138, Phases: []factory.Phase{{Name: "proof", State: factory.PhaseFailed, Round: 1}}}}
	if err := run.QueueItem(&it, now); err != nil {
		t.Fatal(err)
	}
	if err := run.ApplyReport(&it, factory.RunReport{Cost: .03}, now); err != nil {
		t.Fatal(err)
	}
	got, in := factory.SpentIn(it, now.Add(-time.Hour), now.Add(time.Hour))
	if !in || math.Abs(got-.168) > 1e-9 {
		t.Fatalf("restart lifetime = %.6f, want 0.168000", got)
	}
	// The cap deliberately reads only this run, pending the owner's ruling.
	if (*run.Money)(nil).CapReached(it) || factory.JobSpent(it) != .03 {
		t.Fatalf("cap must read only current run: %+v", it)
	}
}
