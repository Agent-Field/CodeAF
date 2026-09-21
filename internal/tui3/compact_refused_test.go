package tui3

// A PASS THAT COMPACTED NOTHING MUST NOT MOVE ANYBODY'S PLACE IN THE HISTORY.
//
// [session.EventCompacted] is sent whether the pass edited the transcript or
// found nothing to do, because a surface opens a row on EventCompacting and has
// to be able to settle it either way. [app.rebase] read every one of them as a
// replacement and handed the scrollback over to it: replayFrom was dropped to a
// floor the reader was nowhere near, so the conversation between the two was
// declared already drawn, and earlierSeam was marked true without the seam ever
// being drawn. The surface then said there was nothing above the screen.
//
// Nothing had moved, so there is nothing to carry over.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// refusedPass is the announcement of a pass that found nothing to stub and
// nothing to fold (internal/session's loop.go).
func refusedPass() session.Event {
	return session.Event{Kind: session.EventCompacted, Hint: "nothing to compact", Unchanged: true}
}

func TestAPassThatCompactedNothingLeavesTheConversationReachable(t *testing.T) {
	a := compactedApp(t, namedPast("old", 6), shortened("old", 6), namedPast("kept", 40))
	if !a.moreHistory() {
		t.Fatal("the resumed surface does not know there is conversation above the screen")
	}
	place := a.replayFrom

	a.event(refusedPass())
	a.touch()

	if a.replayFrom != place {
		t.Fatalf("a pass that compacted nothing moved the place a person was reading, from %d to %d", place, a.replayFrom)
	}

	// AND EVERY WORD OF IT IS STILL REACHABLE BY SCROLLING, which is the fact
	// the number above is only the mechanism of.
	scrollToTop(t, a)
	page := drawnText(a)
	var missing []string
	for i := 0; i < 40; i++ {
		if !strings.Contains(page, "kept answer "+itoa(i)) {
			missing = append(missing, itoa(i))
		}
	}
	if len(missing) > 0 {
		t.Fatalf("scrolling to the top never drew %d of the 40 answers said since the pass: %v",
			len(missing), missing)
	}
	if !strings.Contains(page, "old question 0") {
		t.Fatal("the first message of the conversation is unreachable after a pass that compacted nothing")
	}
	// AND THE SEAM IS DRAWN RATHER THAN MARKED DRAWN. The boundary is a line a
	// person scrolls past; a surface that recorded having drawn it and did not
	// splices the region straight onto later conversation with nothing said.
	if !strings.Contains(page, strings.Fields(seamMark)[0]) {
		t.Fatalf("the seam was never drawn:\n%s", strings.Join(plainRows(a)[:8], "\n"))
	}
}

// AND A PASS THAT HAPPENED STILL HANDS THE BOOKKEEPING OVER, which is what
// keeps the test above from passing on a build that simply stopped rebasing.
func TestAPassThatHappenedStillCarriesThePlaceIntoTheRegion(t *testing.T) {
	a := compactedApp(t, namedPast("old", 6), shortened("old", 6), namedPast("kept", 40))
	place := a.replayFrom

	a.event(session.Event{Kind: session.EventCompacted, Hint: "compacted from ~84k tokens"})
	a.touch()

	if a.replayFrom == place {
		t.Fatalf("a pass that rewrote the transcript left the mark at %d, still pointing into the list it replaced", place)
	}
	if a.replayFrom != a.earlierFloor {
		t.Fatalf("the mark landed at %d rather than on the new floor %d", a.replayFrom, a.earlierFloor)
	}
	// AND THE WHOLE REGION IS OFFERED, because in this fixture the mark stood
	// further into the transcript than the region is long: the list it was taken
	// against is gone and cannot be squared with what replaced it, which is
	// [app.rebase]'s own third case.
	if a.earlierFrom != len(a.earlier) {
		t.Fatalf("earlierFrom = %d, want the whole %d-entry region offered", a.earlierFrom, len(a.earlier))
	}
	if !a.earlierSeam {
		t.Fatal("the pass drew its own block at the boundary and left the seam unclaimed")
	}
}
