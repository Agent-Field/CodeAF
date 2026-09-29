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
