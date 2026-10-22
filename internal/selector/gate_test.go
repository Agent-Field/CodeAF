package selector

import "testing"

// enter-on-UCB, rank-on-LCB.
//
// A wide-sigma model with a high posterior mean can have a UCB that clears the
// 0.8*top floor (it ENTERS) while its LCB (q - 1.28*sigma) ranks below a
// lower-mean, tight-sigma competitor that has a strictly lower UCB. The
// mechanic chooses by LCB among passers. This test locks both halves: the
// wide model enters on its UCB and yet is NOT chosen, because the chosen seat
// ranks on LCB.
func TestUCBEnterLCBRank(t *testing.T) {
	wide := mkM("aero/wide-20260901", "aero/wide-20260901", 1000, 300_000, true, false, 1)
	tight := mkM("aero/tight-20260901", "aero/tight-20260901", 1000, 300_000, true, false, 1)
	cat := mkCat(wide, tight)
	// wide: q=0.70, sigma=0.08 -> UCB 0.8024, LCB 0.5976
	// tight: q=0.55, sigma=0.01 -> UCB 0.5628, LCB 0.5372
	snap := mkSnap(nil,
		map[string]float64{
			"quality/worker/aero/wide-20260901":  0.70,
			"quality/worker/aero/tight-20260901": 0.55,
		},
		map[string]float64{
			"quality/worker/aero/wide-20260901":  0.0064, // 0.08^2
			"quality/worker/aero/tight-20260901": 0.0001, // 0.01^2
		},
	)
	e := NewEngine(snap, cat, []ClassShape{oneClass("worker", "worker", 0.08)})
	d, err := e.DecideClass(oneClass("worker", "worker", 0.08))
	if err != nil {
		t.Fatal(err)
	}
	// The top UCB belongs to wide (0.8024); floor = 0.8*0.8024 = 0.6419.
	// tight's UCB 0.5628 < floor: tight does NOT pass. wide does.
	// So the ONLY passer must be wide; it is chosen.
	if d.Chosen != wide.ID {
		t.Fatalf("expected chosen=%s (the wide UCB passer), got %s", wide.ID, d.Chosen)
	}
	if d.TopUCB < 0.80 {
		t.Fatalf("expected topUCB from wide ~0.8, got %v", d.TopUCB)
	}
	if d.PassedCount != 1 {
		t.Fatalf("expected exactly one passer (wide), got %d", d.PassedCount)
	}
}

// TestGateRejectsBelowFloor: a low-mean model whose UCB sits under 0.8*top is
// gated OUT even though its raw q looks plausible. This is the enter-on-UCB
// half with a genuine rejection.
func TestGateRejectsBelowFloor(t *testing.T) {
	strong := mkM("aero/strong-20260901", "aero/strong-20260901", 1000, 300_000, true, false, 1)
	weak := mkM("aero/weak-20260910", "aero/weak-20260910", 2000, 300_000, true, false, 1)
	cat := mkCat(strong, weak)
	// strong: q=0.9 sigma=0.01 -> UCB 0.9128, floor 0.730
	// weak:   q=0.4 sigma=0.01 -> UCB 0.4128 < floor
	snap := mkSnap(nil,
		map[string]float64{
			"quality/worker/aero/strong-20260901": 0.90,
			"quality/worker/aero/weak-20260910":   0.40,
		},
		map[string]float64{
			"quality/worker/aero/strong-20260901": 0.0001,
			"quality/worker/aero/weak-20260910":   0.0001,
		},
	)
	e := NewEngine(snap, cat, nil)
	d, err := e.DecideClass(oneClass("worker", "worker", 0.08))
	if err != nil {
		t.Fatal(err)
	}
	if d.Chosen != strong.ID {
		t.Fatalf("expected strong chosen, got %s", d.Chosen)
	}
	if d.PassedCount != 1 {
		t.Fatalf("expected weak gated out, passed=%d, chosen=%s", d.PassedCount, d.Chosen)
	}
}

// TestRankByLCBNotUCB: two models that both pass the gate; the one with the
// higher UCB has the lower LCB and must NOT be chosen. Locks rank-on-LCB.
func TestRankByLCBNotUCB(t *testing.T) {
	highUCB := mkM("aero/hi-20260901", "aero/hi-20260901", 1000, 300_000, true, false, 1)
	lowUname := mkM("aero/lo-20260901", "aero/lo-20260901", 1000, 300_000, true, false, 1)
	cat := mkCat(highUCB, lowUname)
	// hi: q=0.70 sigma=0.10 -> UCB 0.828, LCB 0.572
	// lo: q=0.64 sigma=0.01 -> UCB 0.653, LCB 0.627
	// floor = 0.8*0.828 = 0.662; hi and lo both pass (0.653<0.662?!)
	// => lo (UCB 0.653) is BELOW floor 0.662, so only hi enters.
	// For a clean both-pass case, raise lo's mean: q=0.66 sigma=0.01:
	// UCB 0.673 >= floor, LCB 0.647 > hi's 0.572.
	snap := mkSnap(nil,
		map[string]float64{
			"quality/worker/aero/hi-20260901": 0.70,
			"quality/worker/aero/lo-20260901": 0.66,
		},
		map[string]float64{
			"quality/worker/aero/hi-20260901": 0.0100, // 0.10^2
			"quality/worker/aero/lo-20260901": 0.0001, // 0.01^2
		},
	)
	e := NewEngine(snap, cat, nil)
	d, err := e.DecideClass(oneClass("worker", "worker", 0.08))
	if err != nil {
		t.Fatal(err)
	}
	// hi UCB 0.828, floor 0.662; lo 0.673 >= floor passes. lo LCB 0.647 >
	// hi LCB 0.572, so lo wins the seat on LCB despite having the lower UCB.
	if d.Chosen != lowUname.ID {
		t.Fatalf("expected %s chosen on LCB (lower UCB), got %s", lowUname.ID, d.Chosen)
	}
}
