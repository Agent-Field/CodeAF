package tui3

import (
	"testing"
)

// THE PAGE OVER THE RING: a list a person browses with ↑/↓, with enter to
// restore and d to let one go. A restored line LEAVES the ring, so the walk
// above and the page below see the same lines; the one line the page draws
// when nothing is in the ring is the emptiness law.
func TestTheDraftPageListsNewestFirst(t *testing.T) {
	a := &app{draftPage: draftPanel{}}
	a.drafts.push([]rune("first"))
	a.drafts.push([]rune("second"))
	a.openDrafts()
	if got := a.drafts.list(); string(got[0].text) != "second" {
		t.Fatalf("newest-first order flipped: %q", string(got[0].text))
	}
	if len(a.draftPage.entries) != 2 {
		t.Fatalf("page read the ring once: got %d entries", len(a.draftPage.entries))
	}
}

// ENTER RESTORES OVER WHAT IS THERE — a box with words gets pushed BEFORE the
// restored one lands, so recovery never costs the other sentence.
func TestDraftRestorePushesTheLiveBox(t *testing.T) {
	a := &app{draftPage: draftPanel{}}
	a.drafts.push([]rune("lost words"))
	a.input.setText("current")
	a.openDrafts()
	a.draftRestore(0)
	if got := a.input.String(); got != "lost words" {
		t.Fatalf("restore put the box elsewhere: %q", got)
	}
	if list := a.drafts.list(); len(list) != 1 || string(list[0].text) != "current" {
		t.Fatalf("the box that was live should have joined the ring: %v", list)
	}
}
