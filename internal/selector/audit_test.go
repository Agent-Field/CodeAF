package selector

import (
	"strings"
	"testing"
)

// scopedClass decides one class with a catalog of exactly these models,
// returning the SeatDecision.
func scopedClass(t *testing.T, classID string, snap *SharedSnapshot, mods ...Model) SeatDecision {
	t.Helper()
	cat := ModelCatalog{Models: mods}
	e := NewEngine(snap, &cat, nil)
	d, err := e.DecideClass(oneClass(classID, "worker", 1))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestPresetsNameOnlyVerifiedCrews: a class whose chosen seat has a SCORED
// prior is preset-worthy; a class whose every passing candidate INHERITS its
// prior is not. Presets() must name only the scored crew.
func TestPresetsNameOnlyVerifiedCrews(t *testing.T) {
	// Verified class: one scored model -> chosen is scored, not all-inherited.
	verified := mkM("aero/verified-20260901", "aero/verified-20260901", 1000, 300_000, true, false, 1)
	snapV := mkSnap(nil,
		map[string]float64{"quality/worker/aero/verified-20260901": 0.85},
		map[string]float64{"quality/worker/aero/verified-20260901": 0.0001},
	)
	dv := scopedClass(t, "verify", snapV, verified)
	if dv.AllInherited {
		t.Fatalf("verify class must NOT be all-inherited (chosen %s prior %s)", dv.Chosen, dv.ChosenPrior)
	}

	// Inherited class: donor scored, fresh model inherits the vendor mean.
	// The donor FAILS the class gate (NeedsTools, no tool mod) so it is not a
	// candidate; only the inheritor competes, and it inherits the vendor mean.
	donor := mkM("acme/donor-20260801", "acme/donor-20260801", 400, 300_000, false, false, 1)
	fresh := mkM("acme/fresh-20260915", "acme/fresh-20260915", 5000, 300_000, true, false, 1)
	snapI := mkSnap(nil,
		map[string]float64{"quality/worker/acme/donor-20260801": 0.9},
		map[string]float64{"quality/worker/acme/donor-20260801": 0.0001},
	)
	csi := oneClass("inherit", "worker", 1)
	csi.NeedsTools = true
	// scopedClass uses oneClass default (no tools); build directly.
	catI := ModelCatalog{Models: []Model{donor, fresh}}
	eI := NewEngine(snapI, &catI, nil)
	di, err := eI.DecideClass(csi)
	if err != nil {
		t.Fatal(err)
	}
	_ = di
	if !di.AllInherited {
		t.Fatalf("inherit class must be all-inherited (chosen %s prior %s)", di.Chosen, di.ChosenPrior)
	}

	if p := NewEngine(snapI, &ModelCatalog{Models: []Model{donor, fresh}}, nil).
		Presets([]SeatDecision{di}); len(p) != 0 {
		t.Fatalf("all-inherited class must yield no preset, got %d", len(p))
	}
	pv := NewEngine(snapV, &ModelCatalog{Models: []Model{verified}}, nil).
		Presets([]SeatDecision{dv})
	if len(pv) != 1 || pv[0].Seats["worker"] != verified.ID {
		t.Fatalf("verified class must yield one preset naming %s, got %v", verified.ID, pv)
	}
}

// TestPresetsAuditLine: the audit builds one line per preset proving the seat
// is verified; an all-inherited class names no line.
func TestPresetsAuditLine(t *testing.T) {
	verified := mkM("aero/audit-20260901", "aero/audit-20260901", 1000, 300_000, true, false, 1)
	snap := mkSnap(nil,
		map[string]float64{"quality/worker/aero/audit-20260901": 0.9},
		map[string]float64{"quality/worker/aero/audit-20260901": 0.0001},
	)
	dv := scopedClass(t, "worker", snap, verified)
	if dv.AllInherited {
		t.Fatalf("worker scored class must not be all inherited (prior %s)", dv.ChosenPrior)
	}

	var audit []string
	for _, c := range NewEngine(snap, &ModelCatalog{Models: []Model{verified}}, nil).
		Presets([]SeatDecision{dv}) {
		audit = append(audit, "preset="+c.Class+" seat="+c.Seats["worker"]+" prior="+dv.ChosenPrior)
	}
	found := false
	for _, line := range audit {
		if strings.HasPrefix(line, "preset=worker ") && strings.HasSuffix(line, "prior=scored") {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit must name a verified preset line, got %v", audit)
	}
}
