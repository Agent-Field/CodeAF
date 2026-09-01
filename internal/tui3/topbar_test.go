package tui3

import (
	"strings"
	"testing"
)

// ── the crumb's ladder ──────────────────────────────────────────────────────
//
// fitTrail is the give-way ladder every crumb on this surface walks, one rung
// at a time: full → middles folded to one … (first and last two kept) →
// project dropped → parent dropped → title truncated to roomHeadFloor with
// the handle attached. The current segment is never sacrificed for a parent,
// and its handle stays attached through every rung.

// trailOf is a four-step trail the way a room two tasks deep draws it:
// project › chat › parent task › this task, with the handle on the current
// step.
func trailOf() []crumbSeg {
	return []crumbSeg{
		{text: "af-chrome", door: topDoorCrumbHome},
		{text: "fix the parser", door: topDoorCrumbChat},
		{text: "rebuild the index"},
		{text: "handle the empty file", handle: " #2.1"},
	}
}

// trailTexts is the trail as a reader sees it: the steps' words, joined the
// way the bar joins them.
func trailTexts(segs []crumbSeg) string {
	var parts []string
	for _, seg := range segs {
		parts = append(parts, seg.text+seg.handle)
	}
	return strings.Join(parts, topBarSep)
}

// THE FULL RUNG IS THE TRAIL AS IT WAS WRITTEN. Nothing is folded, nothing is
// dropped, and the handle rides the current step.
func TestFitTrailFullKeepsEveryStep(t *testing.T) {
	got := trailTexts(fitTrail(trailOf(), trailFull))
	want := "af-chrome › fix the parser › rebuild the index › handle the empty file #2.1"
	if got != want {
		t.Fatalf("fitTrail(trailFull) = %q, want %q", got, want)
	}
}

// THE FIRST GIVE IS THE MIDDLES: everything between the first step and the
// last two folds to one …, so a shortened crumb still says that it was
// shortened. The first step, the parent and the current step — with its
// handle — are what survive.
func TestFitTrailMiddlesFoldToOneEllipsis(t *testing.T) {
	got := fitTrail(trailOf(), trailMiddles)
	if len(got) != 4 {
		t.Fatalf("fitTrail(trailMiddles) kept %d steps, want 4 (first, …, last two)", len(got))
	}
	if got[0].text != "af-chrome" {
		t.Fatalf("the first step = %q, want the project kept", got[0].text)
	}
	if got[1].text != trailEllipsis {
		t.Fatalf("the second step = %q, want the one … the middles folded to", got[1].text)
	}
	if got[2].text != "rebuild the index" {
		t.Fatalf("the third step = %q, want the parent kept", got[2].text)
	}
	if got[3].text != "handle the empty file" || got[3].handle != " #2.1" {
		t.Fatalf("the current step = %q%s, want the title with its handle", got[3].text, got[3].handle)
	}
}

// THE PROJECT GOES NEXT, and the … it leaves behind is the marker of the
// drop: the trail is now … › parent › current.
func TestFitTrailNoProjectDropsTheProject(t *testing.T) {
	got := fitTrail(trailOf(), trailNoProject)
	if len(got) != 3 {
		t.Fatalf("fitTrail(trailNoProject) kept %d steps, want 3 (…, parent, current)", len(got))
	}
	if got[0].text != trailEllipsis {
		t.Fatalf("the first step = %q, want the … the project left behind", got[0].text)
	}
	if got[1].text != "rebuild the index" {
		t.Fatalf("the second step = %q, want the parent kept", got[1].text)
	}
	if got[2].text != "handle the empty file" || got[2].handle != " #2.1" {
		t.Fatalf("the current step = %q%s, want the title with its handle", got[2].text, got[2].handle)
	}
}

// THEN THE PARENT, and the … stands for it too: the trail is … › current.
func TestFitTrailNoParentDropsTheParent(t *testing.T) {
	got := fitTrail(trailOf(), trailNoParent)
	if len(got) != 2 {
		t.Fatalf("fitTrail(trailNoParent) kept %d steps, want 2 (…, current)", len(got))
	}
	if got[0].text != trailEllipsis {
		t.Fatalf("the first step = %q, want the … the parent left behind", got[0].text)
	}
	if got[1].text != "handle the empty file" || got[1].handle != " #2.1" {
		t.Fatalf("the current step = %q%s, want the title with its handle", got[1].text, got[1].handle)
	}
}

// THE LAST RUNG TRUNCATES THE TITLE TO ITS FLOOR and never drops it: the
// current segment is the thing the bar exists to confirm, so it yields last
// of all — and the handle stays attached, because a title the handle has left
// is a title that names nothing.
func TestFitTrailTitleTruncatesToTheFloorWithTheHandle(t *testing.T) {
	got := fitTrail(trailOf(), trailTitle)
	last := got[len(got)-1]
	if w := len(last.text); w > roomHeadFloor {
		t.Fatalf("the truncated title is %d cells, want at most the floor %d", w, roomHeadFloor)
	}
	if last.handle != " #2.1" {
		t.Fatalf("the handle = %q, want it still attached to the truncated title", last.handle)
	}
	// And the title is TRUNCATED, not dropped: the words it opens with are
	// still there.
	if !strings.HasPrefix("handle the empty file", strings.TrimRight(last.text, "…")) && !strings.HasPrefix(last.text, "handle") {
		t.Fatalf("the truncated title = %q, want the title's own words, cut", last.text)
	}
}

// A SHORT TRAIL WALKS THE SAME LADDER WITHOUT INVENTING STEPS: a chat's trail
// is two steps, and the middles rung has nothing to fold — the ladder is
// measured against what is there, not against what a deeper room would have.
func TestFitTrailOnAChatsTwoSteps(t *testing.T) {
	chat := []crumbSeg{
		{text: "af-chrome", door: topDoorCrumbHome},
		{text: "fix the parser"},
	}
	if got := trailTexts(fitTrail(chat, trailMiddles)); got != "af-chrome › fix the parser" {
		t.Fatalf("fitTrail(a chat's trail, trailMiddles) = %q, want the trail untouched", got)
	}
	if got := trailTexts(fitTrail(chat, trailNoParent)); got != "af-chrome › fix the parser" {
		t.Fatalf("fitTrail(a chat's trail, trailNoParent) = %q, want the trail untouched", got)
	}
}

// ── the give-way order ──────────────────────────────────────────────────────
//
// The top bar spends width in one order, stated three times in the spec: the
// trimmings first ($, then clock, then state, then the glyph — the crumb
// outranks its own trimmings), then the crumb walks its ladder, and only then
// does the right cluster give a term up. The current segment of the crumb
// yields last of all.

// roomBarApp is a room open on a running task with every trimming published —
// a state, a clock, a spend — so the give-way has something to give.
func roomBarApp(t *testing.T) *app {
	t.Helper()
	a, _, _ := roomApp(t)
	a.dismissWelcome()
	a.openRoom(7, "Fix the nil-map crash")
	if !a.roomOpen() {
		t.Fatal("the room did not open")
	}
	node := a.roomNode()
	if node == nil {
		t.Fatal("the room has no node")
	}
	node.cost = 0.42 // the spend trimming
	return a
}

// barHas reports whether the plain bar carries the word.
func barHas(bar, word string) bool { return strings.Contains(bar, word) }

// AT FULL WIDTH THE BAR CARRIES EVERYTHING: the glyph, the crumb, the three
// trimmings, and the right cluster's terms. This is the pin the give-way
// tests measure against — a term that is absent here was never drawn, and its
// absence lower down is not the ladder's doing.
func TestTopBarAtFullWidthCarriesEverything(t *testing.T) {
	a := roomBarApp(t)
	bar := plain(a.topBarWord(200))
	for _, want := range []string{
		"Fix the nil-map crash", // the crumb's current step
		"#7",                    // its handle
		"working",               // the state trimming
		"$0.42",                 // the spend trimming
		"task glm-4.6",          // the room's model
		"chat-v3-task*",         // the branch, dirty star and all
		roomBackWord,            // the way out
	} {
		if !barHas(bar, want) {
			t.Fatalf("the full bar = %q, want %q on it", bar, want)
		}
	}
}

// THE TRIMMINGS GO FIRST, IN THE SPEC'S ORDER: the spend, then the clock,
// then the state, then the glyph — each gone before the crumb gives up a
// step. Narrow the bar one give at a time and the trimmings leave in that
// order while the crumb still stands whole.
func TestTopBarGiveWayTrimmingsBeforeTheCrumb(t *testing.T) {
	a := roomBarApp(t)
	// Find the width where the spend has gone but the crumb is still whole.
	// The trimmings are the room's three facts; the crumb is the title.
	var sawSpendGone, sawClockGone, sawStateGone bool
	for width := 200; width >= 40; width-- {
		bar := plain(a.topBarWord(width))
		whole := barHas(bar, "Fix the nil-map crash")
		if !sawSpendGone && !barHas(bar, "$0.42") && whole {
			sawSpendGone = true
		}
		if sawSpendGone && !sawClockGone && whole {
			// The clock is a count-up word ("4s", "1m"); the state is "working".
			// Once the spend is gone, the next trimming to go is the clock —
			// but the state must still be there.
			if !barHas(bar, "working") {
				t.Fatalf("at width %d the state went before the clock: %q", width, bar)
			}
			sawClockGone = true
		}
		if sawClockGone && !sawStateGone && whole && !barHas(bar, "working") {
			sawStateGone = true
		}
		if sawStateGone {
			break
		}
	}
	if !sawSpendGone {
		t.Fatal("the spend never gave way while the crumb stood whole")
	}
	if !sawStateGone {
		t.Fatal("the state never gave way while the crumb stood whole")
	}
}

// THE CRUMB WALKS ITS LADDER BEFORE THE RIGHT CLUSTER GIVES A TERM UP: as the
// bar narrows past the trimmings, the crumb's steps fold and drop while the
// model, the branch and the back word still stand.
func TestTopBarGiveWayCrumbBeforeTheRightCluster(t *testing.T) {
	a := roomBarApp(t)
	// Find a width where the crumb has folded (the … is on the bar) and check
	// the right cluster is still whole.
	for width := 200; width >= 40; width-- {
		bar := plain(a.topBarWord(width))
		if barHas(bar, trailEllipsis) {
			if !barHas(bar, "task glm-4.6") {
				t.Fatalf("at width %d the model gave way before the crumb finished its ladder: %q", width, bar)
			}
			if !barHas(bar, "chat-v3-task*") {
				t.Fatalf("at width %d the branch gave way before the crumb finished its ladder: %q", width, bar)
			}
			return
		}
	}
	t.Fatal("the crumb never folded — the bar never narrowed onto its ladder")
}

// THE CURRENT SEGMENT YIELDS LAST OF ALL: at the narrowest width the bar is
// still drawn at, the title is there — truncated to its floor, handle
// attached — after every trimming, every parent and every right-cluster term
// that was going to go has gone.
func TestTopBarGiveWayTheCurrentSegmentYieldsLast(t *testing.T) {
	a := roomBarApp(t)
	// The floor is the narrowest frame that gets a pinned header at all; below
	// it the bar is the deck's, not this row's.
	bar := plain(a.topBarWord(60))
	if !barHas(bar, "Fix the nil") && !barHas(bar, "Fix the") && !barHas(bar, "Fix") {
		t.Fatalf("the narrow bar = %q, want the current step's title still on it, truncated", bar)
	}
	if !barHas(bar, "#7") {
		t.Fatalf("the narrow bar = %q, want the handle still attached", bar)
	}
}

// THE WELCOME BOX QUIETS THE BAR TO ITS CRUMB: a greeting is the one moment
// the surface is about nothing yet, and a bar that named a model nobody has
// chosen over a box asking for the first sentence would be answering a
// question nobody asked.
func TestTopBarQuietUnderTheWelcomeBox(t *testing.T) {
	a, _, _ := hudApp(t)
	// hudApp dismisses the welcome; open it again the way a fresh session
	// would.
	a.welcome.open = true
	if !a.statusQuiet() {
		t.Fatal("the welcome box is open and the surface is not quiet")
	}
	bar := plain(a.topBarWord(200))
	if !barHas(bar, "aforge-v2") {
		t.Fatalf("the quiet bar = %q, want the crumb on it", bar)
	}
	if barHas(bar, "deepseek") {
		t.Fatalf("the quiet bar = %q, want no model named over the greeting", bar)
	}
	if barHas(bar, "chat-v3-task") {
		t.Fatalf("the quiet bar = %q, want no branch named over the greeting", bar)
	}
}
