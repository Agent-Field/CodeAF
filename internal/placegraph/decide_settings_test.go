package placegraph

import (
	"errors"
	"testing"
)

func TestDecideDefaultsToNinetyAndNeverAsksForAFileWrittenBeforeIt(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	d, from := snap(t, s).EffectiveDecide(a.ID)
	if d != (Decide{false, 90}) || from != "" {
		t.Fatalf("%+v from %q", d, from)
	}
	if d, from := snap(t, s).EffectiveDecide("pl_nope"); d != DefaultDecide() || from != "" {
		t.Fatalf("unknown place: %+v %q", d, from)
	}
}

func TestNearestAncestorWithAnExplicitValueAnswers(t *testing.T) {
	s, _ := newStore(t)
	top := mk(t, s, "Top")
	mid := mk(t, s, "Mid", top.ID)
	leaf := mk(t, s, "Leaf", mid.ID)
	ok(t)(s.SetDecide(top.ID, Decide{true, 70}))
	if d, from := snap(t, s).EffectiveDecide(leaf.ID); d != (Decide{true, 70}) || from != top.ID {
		t.Fatalf("%+v from %q", d, from)
	}
	ok(t)(s.SetDecide(mid.ID, Decide{false, 95}))
	if d, from := snap(t, s).EffectiveDecide(leaf.ID); d != (Decide{false, 95}) || from != mid.ID {
		t.Fatalf("%+v from %q", d, from)
	}
	// An explicit "off, 90" is a choice, not an absence: it beats an asking parent.
	ok(t)(s.SetDecide(leaf.ID, Decide{false, 90}))
	if d, from := snap(t, s).EffectiveDecide(leaf.ID); from != leaf.ID || d.AlwaysAsk {
		t.Fatalf("%+v from %q", d, from)
	}
	ok(t)(s.ClearDecide(leaf.ID))
	if _, from := snap(t, s).EffectiveDecide(leaf.ID); from != mid.ID {
		t.Fatalf("after clear from %q", from)
	}
}

func TestFirstParentWinsATieAndArchivedAncestorsLendNothing(t *testing.T) {
	s, _ := newStore(t)
	p1, p2 := mk(t, s, "P1"), mk(t, s, "P2")
	c := mk(t, s, "C", p1.ID, p2.ID)
	ok(t)(s.SetDecide(p2.ID, Decide{true, 60}))
	ok(t)(s.SetDecide(p1.ID, Decide{true, 80}))
	if d, _ := snap(t, s).EffectiveDecide(c.ID); d.Threshold != 80 {
		t.Fatalf("%+v", d)
	}
	ok(t)(s.Archive(p1.ID))
	if d, from := snap(t, s).EffectiveDecide(c.ID); d.Threshold != 60 || from != p2.ID {
		t.Fatalf("%+v from %q", d, from)
	}
}

func TestSetDecideRefusesOutOfBoundsAndUndoesInOneToken(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	for _, bad := range []int{0, -5, 101} {
		if _, err := s.SetDecide(a.ID, Decide{false, bad}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%d: %v", bad, err)
		}
	}
	if _, err := s.SetDecide("pl_nope", DefaultDecide()); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	rc, err := s.SetDecide(a.ID, Decide{true, 55})
	if err != nil || rc.Action != ActionDecide || rc.Noop() {
		t.Fatalf("%+v %v", rc, err)
	}
	if again, _ := s.SetDecide(a.ID, Decide{true, 55}); !again.Noop() {
		t.Fatal("same value was a commit")
	}
	if _, err := s.Undo(rc.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := snap(t, s).Place(a.ID); got.Decide != nil {
		t.Fatalf("undo left %+v", got.Decide)
	}
}

func TestDecideSurvivesReopenAndMerge(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	ok(t)(s.SetDecide(a.ID, Decide{true, 66}))
	got, _ := snap(t, s).Place(a.ID)
	if got.Decide == nil || *got.Decide != (Decide{true, 66}) {
		t.Fatalf("%+v", got.Decide)
	}
	// A snapshot copy must not alias the stored value.
	got.Decide.Threshold = 1
	again, _ := snap(t, s).Place(a.ID)
	if again.Decide.Threshold != 66 {
		t.Fatal("snapshot aliases store")
	}
}
