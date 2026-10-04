package tui3

import "testing"

func TestSayOnceSpeaksASentenceOncePerRun(t *testing.T) {
	n := NewNotices()
	n.SayOnce("removed")
	n.SayOnce("removed")
	n.Say("removed")
	n.SayOnce("other")
	if got := n.take(); len(got) != 3 {
		t.Fatalf("desk = %q, want removed, removed (plain Say), other", got)
	}
}
