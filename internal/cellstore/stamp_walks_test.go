package cellstore

import "testing"

// Contract C1.5: a seal walks the folder once, as it did before this change.
func TestASealStampsTheFolderOnce(t *testing.T) {
	r := newRig(t)
	e := inPlaceEngine(t, r)
	r.write("README.md", "one\n")
	if _, err := e.Seal(t.Context(), r.cell, TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	for _, info := range []TurnInfo{{Changed: []string{}}, {Changed: []string{"README.md"}}, {}} {
		before := stampWalks.Load()
		if _, err := e.Seal(t.Context(), r.cell, info); err != nil {
			t.Fatal(err)
		}
		if got := stampWalks.Load() - before; got != 1 {
			t.Errorf("a seal with Changed=%v walked the folder %d times; it is once", info.Changed, got)
		}
	}
}
