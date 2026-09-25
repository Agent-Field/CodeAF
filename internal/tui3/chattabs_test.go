package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ── THE TAB STRIP ───────────────────────────────────────────────────────────
//
// The top of the frame is two rows now: WHICH CONVERSATION (this file) and
// WHERE INSIDE IT (roomcrumbs.go). These hold what the strip claims — that a tab
// is a conversation this window has been in and can go back to — and the two
// things that can quietly stop being true about any strip of tabs: that the
// order stays where a person left it, and that the one they are in is on it.

// tabApp is a window in one conversation with two more in the keeper.
func tabApp(t *testing.T) (*app, *fakeAgent, *fakeAgent) {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.workspace, a.title = "/tmp/lab/this-one.jsonl", "/tmp/lab", "Shipping the parser"
	older, newer := keepThree(t, a)
	a.width, a.height = 160, 40
	a.touch()
	_ = a.tabsRow(a.width)
	return a, older, newer
}

// tabWords is what the strip says, tab by tab, off the map it recorded.
func tabWords(a *app) []string {
	words := make([]string, 0, len(a.chatTabHits))
	for _, hit := range a.chatTabHits {
		// The close cells are a target of their own on every tab (chattabs.go),
		// so a walk of the hit map that counted them would count every tab twice.
		if hit.kind == tabFold || hit.kind == tabClose || hit.kind == tabNew || hit.kind == tabScrollLeft || hit.kind == tabScrollRight {
			continue
		}
		words = append(words, hit.tab.word)
	}
	return words
}

// clickTab presses one column of the frame's first row, which is the strip's.
func clickTab(t *testing.T, a *app, x int) {
	t.Helper()
	if cmd, took := a.tabPress(x, tabStripRow); took {
		_ = cmd
		return
	}
	t.Fatalf("the strip did not answer for column %d:\n%q\n%+v", x, plain(a.tabsRow(a.width)), a.chatTabHits)
}

// tabSpanFor is where one tab was drawn on the last laid-out strip.
func tabSpanFor(t *testing.T, a *app, word string) hudSpan {
	t.Helper()
	for _, hit := range a.chatTabHits {
		if hit.tab.word == word {
			return hit.span
		}
	}
	t.Fatalf("the strip drew no tab saying %q:\n%q\n%+v", word, plain(a.tabsRow(a.width)), a.chatTabHits)
	return hudSpan{}
}

// ── WHAT IS ON IT, AND IN WHAT ORDER ────────────────────────────────────────

// EVERY CONVERSATION THIS WINDOW HAS IS A TAB, and the order is the order they
// were first entered — NOT the recency stack, which re-orders itself on every
// switch and would move the tab out from under a person's finger.
func TestTheStripNamesEveryConversationAndKeepsItsOrderAcrossASwitch(t *testing.T) {
	a, _, _ := tabApp(t)
	strip := plain(a.tabsRow(a.width))
	// The names as a tab spells them: a tab is a label somebody recognises, so a
	// long one is cut to [tabWordCap] rather than given the whole row.
	for _, want := range []string{"openrouter price scrape", "Refactor the rail scop...", "Shipping the parser"} {
		if !strings.Contains(strip, want) {
			t.Fatalf("the strip is missing %q:\n%q", want, strip)
		}
	}
	before := tabWords(a)
	if len(before) != 3 {
		t.Fatalf("the strip drew %d tabs, want three: %+v", len(before), before)
	}
	// AND ONE OF THEM IS THE ONE IN FRONT. Exactly one, whatever the frame.
	lit := 0
	for _, hit := range a.chatTabHits {
		if hit.kind == tabHere {
			lit++
		}
	}
	if lit != 1 {
		t.Fatalf("%d tabs claim to be the conversation in front: %+v", lit, a.chatTabHits)
	}

	// Now switch into another one. The recency stack has re-ordered underneath
	// (keeper.go's [app.rememberOpen]) and the strip has not.
	span := tabSpanFor(t, a, "Refactor the rail scope model")
	clickTab(t, a, span.from+1)
	a.touch()
	_ = a.tabsRow(a.width)
	if got := tabWords(a); strings.Join(got, "|") != strings.Join(before, "|") {
		t.Fatalf("the strip re-ordered itself on a switch:\n before %+v\n after  %+v", before, got)
	}
}

// AND A CONVERSATION THIS WINDOW CANNOT NAME IS NOT DRAWN. A tab spelling a
// transcript's file name is a tab nobody can read, and the strip would rather be
// one shorter than say `20260816-150405_a1b2c3` to somebody.
func TestTheStripDrawsNoTabItCannotName(t *testing.T) {
	a, _, _ := tabApp(t)
	a.prev = append(a.prev, "/tmp/lab/never-seen.jsonl")
	a.touch()
	strip := plain(a.tabsRow(a.width))
	if strings.Contains(strip, "never-seen") {
		t.Fatalf("the strip drew a tab out of a path:\n%q", strip)
	}
	if got := len(tabWords(a)); got != 3 {
		t.Fatalf("the strip grew to %d tabs on a key it cannot name", got)
	}
}

// ── THE DOORS ───────────────────────────────────────────────────────────────

// PRESSING ANOTHER TAB GOES THERE, through the keeper's own door — the one
// `alt+k`'s ring and home's `enter` use, which attaches rather than reopens.
func TestPressingAnotherTabSwitchesToThatConversation(t *testing.T) {
	a, _, newer := tabApp(t)
	span := tabSpanFor(t, a, "Refactor the rail scope model")
	clickTab(t, a, span.from+1)
	if a.agent != Agent(newer) {
		t.Fatalf("the press did not attach the conversation behind that tab: %v", a.agent)
	}
	if a.file != "/tmp/lab/rail-scope.jsonl" {
		t.Fatalf("the surface is pointed at %q", a.file)
	}
	// AND THE ONE THAT WAS IN FRONT IS STILL A TAB, held by the keeper.
	a.touch()
	_ = a.tabsRow(a.width)
	if strip := plain(a.tabsRow(a.width)); !strings.Contains(strip, "Shipping the parser") {
		t.Fatalf("the conversation that stepped aside left the strip:\n%q", strip)
	}
}

// THE TAB IN FRONT IS THE WAY BACK OUT OF A PAGE, and it is INERT on the
// conversation itself: a control that reopened the page you are standing on is a
// control that does nothing, and this surface does not draw those.
func TestTheTabInFrontLeavesAPageAndIsInertOnTheConversation(t *testing.T) {
	a := crumbApp(t)
	a.title = "Shipping the parser"
	a.touch()
	_ = a.tabsRow(a.width)
	span := tabSpanFor(t, a, "Shipping the parser")
	clickTab(t, a, span.from+1)
	if a.roomOpen() {
		t.Fatal("the conversation's own tab did not come back out of the page")
	}
	// Standing on the conversation, the same press takes the row and goes nowhere.
	_ = a.tabsRow(a.width)
	again := tabSpanFor(t, a, "Shipping the parser")
	clickTab(t, a, again.from+1)
	if a.roomOpen() || a.page != pageNone {
		t.Fatalf("a press on the tab already up went somewhere: room=%v page=%v", a.roomOpen(), a.page)
	}
	// AND IT STILL ANSWERS THE POINTER THERE, which is the one place this row
	// departs from the surface's "what lights is what a press acts on" law: a
	// strip where every tab reacts except the one you are on reads as the current
	// tab being broken (chattabs.go's [tabHit.lights]).
	a.hot = hoverAt{kind: hoverTab, index: again.from}
	hit, lit := a.hotTab()
	if !lit || hit.kind != tabHere {
		t.Fatalf("the tab already up does not answer the pointer: %+v lit=%v", hit, lit)
	}
}

// ── THE NARROW FRAME ────────────────────────────────────────────────────────

// THE TAB IN FRONT IS ON THE STRIP AT EVERY WIDTH, and what it cost is COUNTED
// rather than dropped in silence: a strip that quietly drew one of somebody's
// three conversations would be a strip saying they have one.
func TestANarrowStripKeepsTheTabInFrontAndCountsWhatItHid(t *testing.T) {
	a, _, _ := tabApp(t)
	for _, width := range []int{160, 80, 60, 40, 24, roomHeadFloor} {
		a.width = width
		a.touch()
		strip := plain(a.tabsRow(width))
		if got := ansi.StringWidth(strip); got > width {
			t.Fatalf("at %d columns the strip is %d cells:\n%q", width, got, strip)
		}
		lit := 0
		for _, hit := range a.chatTabHits {
			if hit.kind == tabHere {
				lit++
			}
		}
		if lit != 1 {
			t.Fatalf("at %d columns %d tabs are the one in front:\n%q", width, lit, strip)
		}
		if len(tabWords(a)) == 3 {
			continue // everything is spelled; there is nothing hidden to mark
		}
		if !strings.Contains(strip, ".") && !strings.Contains(strip, tabHiddenLead+itoa(len(a.chatTabs)-len(tabWords(a)))) {
			t.Fatalf("at %d columns the strip dropped tabs in silence:\n%q", width, strip)
		}
	}
}

// hopSpreadAll raises the card and opens its fold, which is the gesture
// `alt+k` then `→` makes — every conversation on this machine rather than only
// the ones this window holds. It is spelled here rather than being a second
// constructor in the surface: `hopOpenAll` was that, it existed for the deleted
// `Chats ▾` control alone, and a door in the product that only tests press is a
// door the product does not have (hop.go).
func hopSpreadAll(t *testing.T, a *app) {
	t.Helper()
	drive(t, a, key(hopOpenKey), key(hopFoldKey))
	if !a.hop.open {
		t.Fatal("the switcher did not open")
	}
	// AND THE FOLD IS OPEN IF THERE WAS ANYTHING BEHIND IT. `→` is a no-op when
	// the ring already holds every conversation the machine has
	// ([app.hopSpread] returns on `rest == 0`), and on those fixtures the
	// un-spread card is already the whole list — so demanding `all` there would
	// be the helper asserting a flag rather than the thing the flag is for.
	if a.hop.rest > 0 && !a.hop.all {
		t.Fatalf("%s left %d conversations behind the fold", hopFoldKey, a.hop.rest)
	}
}

// AND THE COUNT IS A FACT AND NOT A DOOR. It says this row could not spell
// every tab it has — which the strip must never go quiet about, or it is back to
// claiming the window holds exactly what fits — and pressing it does nothing.
// The `Chats ▾` control that used to be wrapped around it is deleted: the way to
// the card is the key, named on the legend under the box in this keyboard's own
// spelling (render.go's [hopDoorWord]).
func TestTheCountAtTheEndIsAFactAndNotADoor(t *testing.T) {
	a, _, _ := tabApp(t)
	a.width = 40
	a.touch()
	strip := plain(a.tabsRow(a.width))
	var fold tabHit
	for _, hit := range a.chatTabHits {
		if hit.kind == tabFold {
			fold = hit
		}
	}
	if fold.span.to == 0 {
		t.Fatalf("a forty-column strip hid tabs and said nothing about them:\n%q", strip)
	}
	if fold.door(a) || fold.lights() {
		t.Fatal("the count answers the pointer as though it were a control")
	}
	// AND THE DELETED LABEL IS NOWHERE ON THE ROW, under either of its marks.
	for _, gone := range []string{"Chats", "▾"} {
		if strings.Contains(strip, gone) {
			t.Fatalf("the strip still draws %q:\n%q", gone, strip)
		}
	}
	before, open := a.file, a.hop.open
	clickTab(t, a, fold.span.from)
	if a.hop.open != open {
		t.Fatal("pressing the count opened the picker")
	}
	if a.file != before {
		t.Fatalf("pressing the count switched conversations: %q", a.file)
	}
}

// ── THE GEOMETRY ────────────────────────────────────────────────────────────

// WHAT WAS RECORDED IS WHAT WAS DRAWN, at every width and with a name made of
// wide glyphs. The spans are measured in DISPLAY CELLS — a map built in bytes or
// runes would put every tab after a Japanese name several columns from where a
// person sees it.
func TestEveryTabIsRecordedOnTheCellsItWasDrawnOn(t *testing.T) {
	a, _, _ := tabApp(t)
	a.title = "日本語のタイトル"
	for _, width := range []int{160, 120, 100, 80, 60, 40, 24, roomHeadFloor} {
		a.width = width
		a.touch()
		line := plain(a.tabsRow(width))
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("at %d columns the strip is %d cells:\n%q", width, got, line)
		}
		for _, hit := range a.chatTabHits {
			if hit.span.to > ansi.StringWidth(line) {
				t.Fatalf("at %d columns a tab was recorded past the end of the row: %+v\n%q",
					width, hit.span, line)
			}
			if _, ok := a.tabAt(hit.span.from, tabStripRow); !ok {
				t.Fatalf("at %d columns the strip does not answer for its own cell %d", width, hit.span.from)
			}
			if _, ok := a.tabAt(hit.span.from, tabStripRow+1); ok {
				t.Fatalf("at %d columns the strip answers for the row under it", width)
			}
		}
	}
}

// AND THE STRIP IS BUDGETED WHEREVER IT IS DRAWN. A row the frame drew and the
// scrolling did not subtract puts the last row of the page under the input box —
// and every pointer target above the body resolves through the same number.
func TestTheStripIsChargedToTheBodyRegionAndMovesTheHeaderUnderIt(t *testing.T) {
	a := crumbApp(t)
	a.title = "building the task navigation tree"
	rows := strings.Split(frame(a), "\n")
	if got := plain(rows[0]); !strings.HasPrefix(got, " "+plain(a.pal.wordmark(a.width))) {
		t.Fatalf("the frame's first row is not the pulse: %q", got)
	}
	if got := plain(rows[tabStripRow]); !strings.Contains(got, a.chatDisplayName()) {
		t.Fatalf("the row under the pulse is not the strip: %q", got)
	}
	// THE ROOM'S TRAIL IS THE FIRST ROW UNDER THE WHOLE HEAD — the rule and the
	// blank under the strip are drawn in a room too (head.go).
	if a.roomHeadRow() != placeHeadRows {
		t.Fatalf("the room's header is on row %d, not under the %d-row head", a.roomHeadRow(), placeHeadRows)
	}
	if got := plain(rows[a.roomHeadRow()]); !strings.Contains(got, "Write the tree") {
		t.Fatalf("the trail is not on the row under the head: %q", got)
	}
	if got := plain(rows[a.roomHeadRow()+1]); !strings.Contains(got, "Cut the goldens") {
		t.Fatalf("task title is not below its ancestors: %q", got)
	}
	if a.bodyTop() != a.headHeight()+a.stripHeight() || a.headHeight() < 2 {
		t.Fatalf("the pinned rows are drawn but not budgeted: head=%d top=%d",
			a.headHeight(), a.bodyTop())
	}
	// AND `Stop` ANSWERS FOR THE ROW IT WAS DRAWN ON — the facts row, two under
	// the strip — and for neither of the rows above it.
	strings.Join(a.roomHeadRows(a.width), "\n")
	if a.roomStop.pressable() {
		if !a.stopMarkAt(a.roomStop.from, a.roomFactsRow()) {
			t.Fatal("Stop does not answer for the facts row it was drawn on")
		}
		if a.stopMarkAt(a.roomStop.from, 0) {
			t.Fatal("Stop answers for the strip's row above it")
		}
		if a.stopMarkAt(a.roomStop.from, a.roomHeadRow()) {
			t.Fatal("Stop answers for the trail row above it")
		}
	}
}

// ── A PAGE READ THROUGH SOMEBODY ELSE'S CONVERSATION ────────────────────────

// THE TAB IN FRONT IS STILL THIS WINDOW'S OWN CONVERSATION. A guest page belongs
// to another one, and a strip that lit a tab for the owner would be this window
// claiming to hold a conversation it has never opened.
func TestAGuestPageDoesNotPutTheOwnerOnTheStrip(t *testing.T) {
	a, _ := guestLab(t)
	enterAway(t, a)
	if !a.roomIsGuest() {
		t.Fatal("the row opened no reading page")
	}
	a.width, a.height = 160, 40
	a.touch()
	strip := plain(a.tabsRow(a.width))
	if strings.Contains(strip, roomGuestOwnerWord) {
		t.Fatalf("the strip drew the owner of a page this window is reading:\n%q", strip)
	}
	for _, hit := range a.chatTabHits {
		if hit.kind == tabHere && hit.tab.key != a.frontTabKey() {
			t.Fatalf("the lit tab is not this window's conversation: %+v", hit.tab)
		}
	}
}

// ── THE UNSENT SENTENCE SURVIVES A SHARED HANDLE ────────────────────────────

// OVER A SHARED ENGINE HANDLE THE KEEPER HOLDS NOTHING — the far side ends the
// conversation it swaps away from ([Options.SharedAgent]) — AND THE WORDS IN THE
// BOX ARE STILL THE PERSON'S. They are not on the wire, nothing has been sent,
// and a switch that dropped them would lose a paragraph somebody was in the
// middle of every time they pressed a tab.
func TestASharedSwitchKeepsTheUnsentSentenceOfTheConversationItLeaves(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.shared = true
	dir := t.TempDir()
	a.file, a.workspace = filepath.Join(dir, "this-one.jsonl"), dir
	a.draftFile = filepath.Join(dir, "draft.txt")
	a.input.setText("half a sentence nobody has sent")

	conv, side := a.front(), a.detachConversation()
	a.stow(conv, side)

	if len(a.behind) != 0 {
		t.Fatalf("a shared handle put an agent in the keeper: %+v", a.behind)
	}
	text, err := os.ReadFile(conv.DraftFile)
	if err != nil || !strings.Contains(string(text), "half a sentence nobody has sent") {
		t.Fatalf("the unsent sentence did not survive the switch: %q err=%v", string(text), err)
	}
	if keep, how := readDraftKeep(draftKeepPath(conv.DraftFile)); how != draftKeepFound || len(keep.Slots) == 0 {
		t.Fatalf("the composer left no record under its own identity: how=%v keep=%+v", how, keep)
	}
}
