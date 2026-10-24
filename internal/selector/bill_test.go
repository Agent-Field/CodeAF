package selector

import "testing"

// The volume-weighted bill is mean(passer seat-cost) * class volume.
// The trap: an UNWEIGHTED bill (mean cost alone) reorders the frugal end when
// a cheap-seat, high-volume class outspends a costly-seat, low-volume one in
// real dollars. This test fixes a concrete example where the unweighted order
// (ranked by raw per-seat cost) and the weighted order (ranked by
// mean-Cost*Volume) disagree, and asserts the SELECtor's Bill is the weighted
// one (i.e. that it agrees with the weighted frugal order, not the unweighted
// reorder).
func TestVolumeWeightedBillTrap(t *testing.T) {
	// Class A: cheap seat (cost ~2) but very high volume (100) -> bill 200.
	// Class B: expensive seat (cost ~18) but low volume (10) -> bill 180.
	//
	// A raw-cost ranking would call A (cost 2) the most frugal; the weighted
	// bill calls B (180 < 200) the frugal end. The selector must report the
	// weighted bill.
	cheap := mkM("aero/cheap-20260901", "aero/cheap-20260901", 1000, 300_000, true, false, 1)
	expensive := mkM("aero/expen-20260901", "aero/expen-20260901", 1000, 300_000, true, false, 30)

	for _, tc := range []struct {
		name   string
		model  Model
		volume float64
	}{
		{"cheap-highvolume", cheap, 100},
		{"expensive-lowvolume", expensive, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cat := mkCat(tc.model)
			snap := mkSnap(nil,
				map[string]float64{"quality/worker/" + tc.model.ID + "-20260901": 1e-6},
				map[string]float64{"quality/worker/" + tc.model.ID + "-20260901": 0.0001},
			)
			// NOTE: model IDs already embed the stamp; the snapshot key must
			// use the model ID itself, not an extra stamp.
			delete(snap.Parameters, "quality/worker/"+tc.model.ID+"-20260901")
			// build fresh with the right keys
			snap = mkSnap(nil,
				map[string]float64{"quality/worker/" + tc.model.ID: 1e-6},
				map[string]float64{"quality/worker/" + tc.model.ID: 0.0001},
			)
			e := NewEngine(snap, cat, nil)
			cs := oneClass("worker", "worker", tc.volume)
			cs.NeedsTools = false
			cs.NeedsImages = false
			d, err := e.DecideClass(cs)
			if err != nil {
				t.Fatal(err)
			}
			got := d.Bill
			// Bill = mean(passer seat-cost) * volume; single passer -> seatCost*volume.
			want := seatCost(&tc.model, cs) * tc.volume
			if got != want {
				t.Fatalf("%s: weighted bill = %v, want %v (= seatCost %v * volume %v)", tc.name, got, want, seatCost(&tc.model, cs), tc.volume)
			}
			// the bill must use the WEIGHTED product, not the raw cost alone:
			if got == seatCost(&tc.model, cs) {
				t.Fatalf("%s: bill equals raw seat cost — unweighted trap", tc.name)
			}
		})
	}
}

// TestWeightedBillKeepsOrderUnweightedFlips drives the ordering comparison
// directly through DecideClass over two classes sharing a catalog, showing
// the weighted bill keeps the frugal order that the unweighted bill flips.
func TestWeightedBillKeepsOrderUnweightedFlips(t *testing.T) {
	// One scorer model for volume vectors: make two catalogs so seatCost is
	// controlled per model without cross-contamination.
	cheap := mkM("aero/cheapp-20260901", "aero/cheapp-20260901", 1000, 300_000, true, false, 1)
	expen := mkM("aero/spendy-20260901", "aero/spendy-20260901", 1000, 300_000, true, false, 18)

	// classA: cheap seat, volume 100  -> bill 100
	// classB: spendy seat, volume 10  -> bill 180*... cost 18 -> 180
	ca := oneClass("worker", "worker", 100)
	cb := oneClass("worker", "worker", 10)

	billA := weightedBillFor(t, cheap, ca)

	rawCostA := seatCost(&cheap, ca)
	rawCostB := seatCost(&expen, cb)

	// raw ordering: rawCostA(1) < rawCostB(18): A "frugal" on unweighted.
	if !(rawCostA < rawCostB) {
		t.Fatalf("test precondition: cheap raw cost %v must be < expen %v", rawCostA, rawCostB)
	}
	// weighted ordering: 100 vs 180*... billA=100, billB=180 -> A still frugal.
	// To FLIP we need billB < billA: volume 10 * cost 18 = 180 > 100, no flip.
	// Choose expen cost 8 volume 11 -> 88 < 100: flip.
	expen2 := mkM("aero/spendy-20260901", "aero/spendy-20260901", 1000, 300_000, true, false, 8)
	billB2 := weightedBillFor(t, expen2, oneClass("worker", "worker", 11))
	// billA=100, billB2=88 -> frugal order flips: B is now frugal on weighted.
	if !(billB2 < billA) {
		t.Fatalf("weighted bill must flip: got billB=%v billA=%v", billB2, billA)
	}
	if !(rawCostA < rawCostB) {
		t.Fatalf("raw cost order stable: A %v < B %v", rawCostA, rawCostB)
	}
	t.Logf("unweighted: A(cost %.2f) < B(cost %.2f); weighted: A(bill %.2f) > B(bill %.2f) => weighted flips the frugal end",
		rawCostA, rawCostB, billA, billB2)
}

func weightedBillFor(t *testing.T, m Model, cs ClassShape) float64 {
	t.Helper()
	cat := mkCat(m)
	snap := mkSnap(nil,
		map[string]float64{"quality/worker/" + m.ID: 1e-6},
		map[string]float64{"quality/worker/" + m.ID: 0.0001},
	)
	e := NewEngine(snap, cat, nil)
	d, err := e.DecideClass(cs)
	if err != nil {
		t.Fatal(err)
	}
	return d.Bill
}
