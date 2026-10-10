package placegraph

import (
	"testing"
	"time"
)

func openIDs(t *testing.T, s *Store) []string {
	t.Helper()
	var out []string
	for _, r := range snap(t, s).Open {
		out = append(out, r.PlaceID)
	}
	return out
}

func TestVisitPutsThePlaceAtTheTopOfOpen(t *testing.T) {
	s, _ := newStore(t)
	a, b := mk(t, s, "a"), mk(t, s, "b")
	for _, id := range []string{a.ID, b.ID, a.ID} {
		if err := s.Visit(id); err != nil {
			t.Fatal(err)
		}
	}
	if got := openIDs(t, s); !eq(got, []string{a.ID, b.ID}) || got[0] != a.ID {
		t.Fatalf("open = %v", got)
	}
	if snap(t, s).find(a.ID).LastOpenedAt.IsZero() {
		t.Fatal("visit did not stamp LastOpenedAt")
	}
}

func TestClosingABusyPlaceKeepsItMutedUntilItSettles(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "a")
	s.Visit(a.ID)
	busy := true
	if err := s.Close(a.ID, func(string) bool { return busy }); err != nil {
		t.Fatal(err)
	}
	if o := snap(t, s).Open; len(o) != 1 || !o[0].Closed {
		t.Fatalf("want one closed row, got %+v", o)
	}
	s.Sweep(time.Now(), func(string) bool { return busy })
	if len(snap(t, s).Open) != 1 {
		t.Fatal("swept while still busy")
	}
	busy = false
	s.Sweep(time.Now(), func(string) bool { return busy })
	if len(snap(t, s).Open) != 0 {
		t.Fatal("settled closed row not dropped")
	}
	s.Visit(a.ID)
	s.Close(a.ID, nil)
	if len(snap(t, s).Open) != 0 {
		t.Fatal("idle close should remove the row")
	}
}

func TestOpenPlacesIdleTwelveHoursCloseThemselves(t *testing.T) {
	s, _ := newStore(t)
	a, b := mk(t, s, "a"), mk(t, s, "b")
	s.Visit(a.ID)
	s.Visit(b.ID)
	at := snap(t, s).Open[1].TouchedAt // a's stamp
	busyA := func(id string) bool { return id == a.ID }
	s.Sweep(at.Add(OpenIdle-time.Second), nil)
	if len(snap(t, s).Open) != 2 {
		t.Fatal("closed before 12h")
	}
	s.Sweep(at.Add(OpenIdle+time.Minute), busyA)
	if got := openIDs(t, s); !eq(got, []string{a.ID}) {
		t.Fatalf("busy place must survive the sweep, got %v", got)
	}
	s.Sweep(at.Add(OpenIdle+time.Minute), nil)
	if len(snap(t, s).Open) != 0 {
		t.Fatal("idle place not closed")
	}
}

func TestPinnedNeverAutoClose(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "a")
	s.Visit(a.ID)
	if _, err := s.Pin(a.ID, 0); err != nil {
		t.Fatal(err)
	}
	if len(snap(t, s).Open) != 0 {
		t.Fatal("pinning must lift the place out of Open")
	}
	s.Visit(a.ID)
	s.Close(a.ID, nil)
	s.Sweep(time.Now().Add(100*time.Hour), nil)
	if got := snap(t, s).Pinned; !eq(got, []string{a.ID}) || len(snap(t, s).Open) != 0 {
		t.Fatalf("pinned=%v open=%v", got, snap(t, s).Open)
	}
}

func TestPinnedParentDoesNotPullChildren(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "p")
	c := mk(t, s, "c", p.ID)
	s.Pin(p.ID, 0)
	s.Visit(p.ID)
	sn := snap(t, s)
	if !eq(sn.Pinned, []string{p.ID}) || len(sn.Open) != 0 {
		t.Fatalf("pinned=%v open=%v (child %s)", sn.Pinned, sn.Open, c.ID)
	}
}

func TestReorderSetsThePinnedOrder(t *testing.T) {
	s, _ := newStore(t)
	a, b, c := mk(t, s, "a"), mk(t, s, "b"), mk(t, s, "c")
	for _, p := range []Place{a, b, c} {
		s.Pin(p.ID, -1)
	}
	if _, err := s.Reorder([]string{c.ID, "ghost", a.ID}); err != nil {
		t.Fatal(err)
	}
	if got := snap(t, s).Pinned; got[0] != c.ID || got[1] != a.ID || got[2] != b.ID {
		t.Fatalf("pinned = %v", got)
	}
}

func TestUndoKeepsTheRailAndDeletedPlacesLeaveIt(t *testing.T) {
	s, _ := newStore(t)
	a, b := mk(t, s, "a"), mk(t, s, "b")
	s.Visit(a.ID)
	rc, err := s.Rename(a.ID, "a2")
	if err != nil {
		t.Fatal(err)
	}
	s.Visit(b.ID)
	if _, err := s.Undo(rc.ID); err != nil {
		t.Fatal(err)
	}
	if got := openIDs(t, s); !eq(got, []string{b.ID, a.ID}) || got[0] != b.ID {
		t.Fatalf("undo rolled the rail back: %v", got)
	}
	if _, _, err := s.DeletePlace(b.ID); err != nil {
		t.Fatal(err)
	}
	if got := openIDs(t, s); !eq(got, []string{a.ID}) {
		t.Fatalf("deleted place still open: %v", got)
	}
}
