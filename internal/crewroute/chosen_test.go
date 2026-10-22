package crewroute

import (
	"strings"
	"testing"
)

// THE CALLER'S PICK IS SEATED, NOT PINNED. A request that names a model for a
// seat gets that model on the crew — through the same machinery a pin runs,
// so its quality, its cost and its route are read the same way — while the
// receipt still says routed, the ladder is still carried, and the person's
// own pin still wins. This is the seam the pareto selector rides
// (internal/config's wiring); everything a decision promises is unchanged by
// who named the model.
func TestAChosenSeatRunsTheNamedModelAndStaysRouted(t *testing.T) {
	cands := catalogCandidates()
	d, err := Decide(Request{Class: Bugfix, Candidates: cands, Chosen: map[Seat]string{
		Worker: kimiK3.ID, Checker: glmFlash.ID,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Seat(Worker); got.Model != kimiK3.ID || got.Pinned {
		t.Errorf("worker %+v, want %s and not pinned", got, kimiK3.ID)
	}
	if got := d.Seat(Checker); got.Model != glmFlash.ID || got.Pinned {
		t.Errorf("checker %+v, want %s and not pinned", got, glmFlash.ID)
	}
	// The seat nobody named keeps the engine's own pick.
	if d.Seat(Planner).Model != d.Seat(Planner).Model {
		t.Error("the planner vanished")
	}
	for _, seat := range Seats {
		if len(d.Ladder[seat]) == 0 {
			t.Errorf("%s carries no ladder — a seat the caller named must still fail over", seat)
		}
	}
	if d.EstUSD <= 0 {
		t.Errorf("the estimate was not recomputed for the seated crew: $%.3f", d.EstUSD)
	}
	// The routed seat reads its route from the candidates, as any pick does.
	if got := d.Seat(Worker); got.Send != kimiK3.ID || got.Provider != "openrouter" {
		t.Errorf("worker route %+v, want the candidate's own route", got)
	}
}

// A PIN IS THE PERSON'S, and a named seat never overrules one: the pin runs.
func TestAChosenSeatNeverOverrulesAPin(t *testing.T) {
	d, err := Decide(Request{Class: Bugfix, Candidates: catalogCandidates(),
		Pins:   map[Seat]Pin{Worker: {Model: v4Flash.ID, Send: v4Flash.ID, Kind: Metered}},
		Chosen: map[Seat]string{Worker: kimiK3.ID}})
	if err != nil {
		t.Fatal(err)
	}
	got := d.Seat(Worker)
	if got.Model != v4Flash.ID || !got.Pinned {
		t.Errorf("worker %+v, want the person's pin kept", got)
	}
}

// A PICK MADE OUTSIDE THE CANDIDATES CANNOT BE SEATED: nobody offers the
// model, so the engine's own pick stands rather than a seat nobody can run.
func TestAChosenModelNobodyOffersLeavesThePick(t *testing.T) {
	plain, err := Decide(Request{Class: Bugfix, Candidates: catalogCandidates()})
	if err != nil {
		t.Fatal(err)
	}
	d, err := Decide(Request{Class: Bugfix, Candidates: catalogCandidates(),
		Chosen: map[Seat]string{Worker: "acme/no-such-model"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Seat(Worker).Model != plain.Seat(Worker).Model {
		t.Errorf("worker %s moved to an unofferable pick, want %s", d.Seat(Worker).Model, plain.Seat(Worker).Model)
	}
}

// A REDO ROUTES BY THIS ENGINE ALONE: the caller's pick is ignored on a redo,
// where the seats move relative to the crew that ran — a redo decided with
// the pick named is the redo decided without it, to the last byte of a pick.
func TestARedoIgnoresTheChosenSeats(t *testing.T) {
	cands := catalogCandidates()
	ran, err := Decide(Request{Class: OpenEnded, Candidates: cands})
	if err != nil {
		t.Fatal(err)
	}
	chosen := map[Seat]string{Worker: kimiK3.ID, Checker: kimiK3.ID, Planner: kimiK3.ID}
	with, err := Decide(Request{Class: OpenEnded, Candidates: cands, Stronger: &ran, Chosen: chosen})
	if err != nil {
		t.Fatal(err)
	}
	without, err := Decide(Request{Class: OpenEnded, Candidates: cands, Stronger: &ran})
	if err != nil {
		t.Fatal(err)
	}
	for _, seat := range Seats {
		if with.Seat(seat) != without.Seat(seat) {
			t.Errorf("%s: a redo with the pick named is %+v, without it %+v", seat, with.Seat(seat), without.Seat(seat))
		}
	}
}

// NOTHING NAMED, NOTHING CHANGES: a request with no Chosen at all — the
// no-snapshot case — decides exactly the crew it decided before the field
// existed.
func TestNoChosenChangesNothing(t *testing.T) {
	cands := frontierCandidates()
	plain, err := Decide(Request{Class: OpenEnded, Candidates: cands})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(plain.Seat(Worker).Model) == "" {
		t.Fatal("the plain decision seated nothing")
	}
	d, err := Decide(Request{Class: OpenEnded, Candidates: cands, Chosen: nil})
	if err != nil {
		t.Fatal(err)
	}
	for _, seat := range Seats {
		if d.Seat(seat) != plain.Seat(seat) {
			t.Errorf("%s moved with no Chosen: %+v vs %+v", seat, d.Seat(seat), plain.Seat(seat))
		}
	}
}
