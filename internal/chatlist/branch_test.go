package chatlist

import "testing"

func TestBranchLineSpellings(t *testing.T) {
	r := Row{Status: Branch, Device: "laptop", OrphanTurns: 2}
	for got, want := range map[string]string{
		BranchLine(r, true):  "2 turns from laptop: merge / discard",
		BranchLine(r, false): "2 turns from laptop: discard",
		BranchShort(r):       "2 turns · laptop",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	// The frozen sentence and the list's own status line are one spelling.
	if BranchLine(r, true) != StatusLine(r) {
		t.Errorf("BranchLine and StatusLine disagree")
	}
}

func TestOneTurnIsNotPlural(t *testing.T) {
	for _, c := range []struct {
		n    uint32
		unit string
	}{{1, "1 turn"}, {2, "2 turns"}} {
		r := Row{Status: Branch, Device: "mac", OrphanTurns: c.n, Pending: c.n}
		want := map[string]string{
			BranchLine(r, true):   c.unit + " from mac: merge / discard",
			BranchLine(r, false):  c.unit + " from mac: discard",
			BranchShort(r):        c.unit + " · mac",
			TakeoverLine(r):       "last durable turn 0s ago; up to " + c.unit + " may still be on mac",
			KeptEdits(c.n, "mac"): "your unsaved edits here were kept as " + c.unit + " from mac",
		}
		for got, w := range want {
			if got != w {
				t.Errorf("K=%d: got %q, want %q", c.n, got, w)
			}
		}
	}
}
