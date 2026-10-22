package selector

import "testing"

// A held-out model (deepseek-v4.1-flash-1017 in the replay) has NO scored
// row; it inherits the vendor mean. Mechanism under test:
//   - on EVERY seat it passes the UCB entrance gate (ucb >= 0.8*top, because
//     the inherited prior has no competitor above it — it is the lone
//     candidate, so it is the top),
//   - yet because its prior is INHERITED (not scored), the class is
//     AllInherited and the model wins NO preset until it is measured.
//
// This is the cold-start verdict: enter on every seat, preset nowhere.
func TestColdStartEntersEverySeatWinsNoPreset(t *testing.T) {
	// The held-out model's lineage: a scored sibling (its/vendor donor) that
	// fails every class gate except the model's own, so only the held-out
	// model passes — mirroring "held out, no indexes, treated as new".
	donor := mkM("dcp/donor-20260801", "dcp/donor-20260801", 400, 300_000, false, false, 1)
	held := mkM("dcp/deepseek-v4.1-flash-1017", "dcp/deepseek-v4.1-flash-1017", 5000, 300_000, true, false, 1)
	snap := mkSnap(nil,
		map[string]float64{"quality/worker/dcp/donor-20260801": 0.9},
		map[string]float64{"quality/worker/dcp/donor-20260801": 0.0001},
	)
	cat := ModelCatalog{Models: []Model{donor, held}}
	e := NewEngine(snap, &cat, nil)

	// Several seat classes with different credential demands; the held-out
	// model must pass the UCB gate on every one and always inherit.
	seats := []string{"worker", "coder", "reasoner"}
	for _, role := range seats {
		cs := oneClass(role, role, 1)
		cs.NeedsTools = true // donor (non-tool) gated out; held-out (tool) passes
		d, err := e.DecideClass(cs)
		if err != nil {
			t.Fatal(err)
		}
		// The held-out model is the only passing candidate, so it IS chosen.
		if d.Chosen != held.ID {
			t.Fatalf("seat %s: held-out model (%s) must be chosen, got %s", role, held.ID, d.Chosen)
		}
		// It enters on UCB: PassedCount must include it (1 passer) and
		// TopLCB/TopUCB are its own.
		if d.PassedCount != 1 {
			t.Fatalf("seat %s: expected 1 passer (held-out), got %d", role, d.PassedCount)
		}
		// gate passed: the chosen (only) candidate's UCB satisfied the floor.
		if d.TopUCB <= 0 || d.TopLCB > d.TopUCB {
			t.Fatalf("seat %s: gate malformed topUCB %v topLCB %v", role, d.TopUCB, d.TopLCB)
		}
		// It wins NO preset: its seat is inherited, so the class is
		// AllInherited and Presets() names nothing.
		if !d.AllInherited {
			t.Fatalf("seat %s: held-out prior is inherited, class must be AllInherited (prior %s)", role, d.ChosenPrior)
		}
		if p := e.Presets([]SeatDecision{d}); len(p) != 0 {
			t.Fatalf("seat %s: held-out must win no preset, got %d", role, len(p))
		}
	}
}

// TestColdStartUntilMeasured makes the same held-out model SCORED and shows
// it then wins a preset — the "until measured" half of the verdict.
func TestColdStartUntilMeasured(t *testing.T) {
	held := mkM("dcp/deepseek-v4.1-flash-1017", "dcp/deepseek-v4.1-flash-1017", 5000, 300_000, true, false, 1)
	// now it has its own scored row
	snap := mkSnap(nil,
		map[string]float64{"quality/worker/dcp/deepseek-v4.1-flash-1017": 0.88},
		map[string]float64{"quality/worker/dcp/deepseek-v4.1-flash-1017": 0.0001},
	)
	cat := ModelCatalog{Models: []Model{held}}
	e := NewEngine(snap, &cat, nil)
	d, err := e.DecideClass(oneClass("worker", "worker", 1))
	if err != nil {
		t.Fatal(err)
	}
	if d.AllInherited {
		t.Fatalf("measured model must NOT be all-inherited (prior %s)", d.ChosenPrior)
	}
	if p := e.Presets([]SeatDecision{d}); len(p) != 1 || p[0].Seats["worker"] != held.ID {
		t.Fatalf("measured model must win a preset, got %v", p)
	}
}
