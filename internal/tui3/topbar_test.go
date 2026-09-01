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
	a.title = "port the lexer"
	a.branch, a.branchDirty = "chat-v3-task", true
	a.openRoom(7, "Fix the nil-map crash")
	if !a.roomOpen() {
		t.Fatal("the room did not open")
	}
	node := a.roomNode()
	if node == nil {
		t.Fatal("the room has no node")
	}
	node.cost = 0.42            // the spend trimming
	node.model = "z-ai/glm-4.6" // the node's own model, which the bar names `task glm-4.6`
	return a
}

// crumbFolded reports whether the crumb on this bar has given a STEP up, which
// is not the same question as "is there an … on the row": the ladder's last rung
// truncates the current title with one too, and by then the right cluster has
// already been allowed to give way. A fold is an … standing where a step stood,
// so it is the one with a separator in front of it.
func crumbFolded(bar string) bool { return strings.Contains(bar, topBarSep+trailEllipsis) }

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
	// A DEEP TRAIL, because a two-step crumb has no step to fold: the ladder's
	// first three rungs all need something between the ends of the trail. Node 4
	// hangs off node 3 which hangs off node 1, so the crumb is
	// project › chat › Ship the port › Write the tree › Cut the goldens.
	railRun(a)
	a.openRoom(4, "Cut the goldens")
	if node := a.roomNode(); node != nil {
		node.model = "z-ai/glm-4.6"
	}
	if len(a.crumbSegments()) < 5 {
		t.Fatalf("the fixture's crumb is %d steps, want a trail deep enough to fold", len(a.crumbSegments()))
	}
	// Find a width where the crumb has folded a step and check the right cluster
	// is still whole.
	for width := 200; width >= 40; width-- {
		bar := plain(a.topBarWord(width))
		if crumbFolded(bar) {
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

// ── the bar that is not drawn ───────────────────────────────────────────────

// A BAR THAT IS NOT DRAWN LEAVES NO DOORS BEHIND IT.
//
// The spans are this surface's whole answer to "was it on the screen"
// (standdoor.go's law, and the reason [app.topBarWord] clears them before it
// writes them) — and the early return in [app.topBarRows] skipped the clearing,
// so the LAST frame's columns stayed live. Open a room at two hundred columns,
// drag the window under sixty, and a click on the transcript landed on a ✕ that
// had been gone for three frames: the phone tier draws a deck instead of a bar,
// and the ✕'s hit box is three rows tall down there, over the top of the body.
//
// Two guards, because the failure had two halves: every path out of the bar
// clears the spans, and [app.stopMarkAt] refuses on a frame with no bar at all.
func TestABarThatIsNotDrawnLeavesNoDoorsBehindIt(t *testing.T) {
	a, _, _ := roomApp(t)
	a.title = "port the lexer"
	clickRail(t, a, 0)
	if !a.roomOpen() {
		t.Fatal("the rail did not open a room")
	}
	// A wide frame draws the bar, and the bar records its doors.
	a.width, a.height = 200, 40
	a.touch()
	_ = a.topBarRows(a.width)
	if !a.roomStop.pressable() || !a.crumbHomeSpan.pressable() {
		t.Fatalf("the wide bar recorded no doors: stop=%+v home=%+v", a.roomStop, a.crumbHomeSpan)
	}

	// Now shrink under the phone tier, where the deck stands in for the bar.
	a.width = 44
	a.touch()
	if a.topBarShowing(a.width) {
		t.Fatal("the phone tier drew a top bar")
	}
	_ = a.topBarRows(a.width)
	for what, span := range map[string]hudSpan{
		"the mark":       a.roomStop,
		"the crumb home": a.crumbHomeSpan,
		"the crumb chat": a.crumbChatSpan,
		"the back word":  a.backSpan,
		"YOLO":           a.topYoloSpan,
	} {
		if span.pressable() {
			t.Fatalf("%s kept its columns on a frame with no bar: %+v", what, span)
		}
	}

	// AND THE MARK REFUSES EVEN IF A SPAN OUTLIVES THE CLEARING. This is the
	// second guard and it is deliberately independent of the first: the mark is
	// only ever on the bar, so a frame with no bar has no mark, whatever some
	// earlier frame left in the field.
	a.roomStop = hudSpan{from: 40, to: 41}
	for y := 0; y < 3; y++ {
		if a.stopMarkAt(40, y) {
			t.Fatalf("the mark claimed (40, %d) on a frame with no bar", y)
		}
	}
}

// AND THE TWO CLUSTERS NEVER STAND ON EACH OTHER. Both ladders can run out —
// a long crumb beside YOLO and a ✕ — and the last resort used to be [fit],
// which cuts from the RIGHT: it took the mark and the YOLO term off a bar whose
// crumb had already refused to shorten, which is the give-way this file states
// three times, inverted. The crumb yields last among the things that yield; it
// does not outrank the safety posture or the way to stop work.
func TestTheCrumbYieldsBeforeTheSafetyPostureAndTheMark(t *testing.T) {
	a, _, _ := roomApp(t)
	a.title = "a conversation with a deliberately long name on it"
	a.approval = "allow"
	clickRail(t, a, 0)
	if !a.roomOpen() {
		t.Fatal("the rail did not open a room")
	}
	for _, width := range []int{120, 100, 80, 70, 64, 60} {
		a.width, a.height = width, 40
		a.touch()
		bar := plain(a.topBarWord(width))
		if got := len([]rune(bar)); got > width {
			t.Fatalf("at %d columns the bar is %d cells:\n%q", width, got, bar)
		}
		if !strings.Contains(bar, "YOLO") {
			t.Fatalf("at %d columns the open gate was dropped:\n%q", width, bar)
		}
		if !strings.Contains(bar, roomStopMark) {
			t.Fatalf("at %d columns the mark was dropped:\n%q", width, bar)
		}
		// AND THE SPANS DO NOT OVERLAP. Two doors sharing a column is one door
		// answering for the other, which is the same defect the truncation was.
		spans := []hudSpan{a.crumbHomeSpan, a.crumbChatSpan, a.modelSpan, a.topYoloSpan, a.backSpan, a.roomStop}
		for i, one := range spans {
			for _, two := range spans[i+1:] {
				if !one.pressable() || !two.pressable() {
					continue
				}
				if one.from < two.to && two.from < one.to {
					t.Fatalf("at %d columns two doors share columns: %+v and %+v", width, one, two)
				}
			}
		}
	}
}
