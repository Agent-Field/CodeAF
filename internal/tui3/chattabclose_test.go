package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// ── DISMISSING A TAB CLOSES A VIEW AND NEVER WORK ───────────────────────────
//
// This file holds the one claim the ✕ makes and the four ways it could quietly
// stop being true: that the agent behind a dismissed tab is still running, that
// the words a person had half typed are still there when they come back, that
// the tab does not reappear on the next frame, and that a press aimed at the ✕
// can never be read as a press aimed at the label beside it.

// tabCloseSpanFor is where one tab's close cells were drawn.
func tabCloseSpanFor(t *testing.T, a *app, word string) hudSpan {
	t.Helper()
	for _, hit := range a.chatTabHits {
		if hit.kind == tabClose && hit.tab.word == word {
			return hit.span
		}
	}
	t.Fatalf("the strip drew no close target for %q:\n%q\n%+v", word, plain(a.tabsRow(a.width)), a.chatTabHits)
	return hudSpan{}
}

// DISMISSING A TAB THIS WINDOW IS NOT IN takes it off the row and does nothing
// else at all: the agent is untouched, the keeper still holds it, and the
// switcher still lists it.
func TestDismissingAnInactiveTabClosesNothing(t *testing.T) {
	a, older, _ := tabApp(t)
	span := tabCloseSpanFor(t, a, "openrouter price scrape")
	clickTab(t, a, span.from)
	_ = a.tabsRow(a.width)

	for _, gone := range tabWords(a) {
		if gone == "openrouter price scrape" {
			t.Fatalf("the dismissed tab is still on the row: %+v", tabWords(a))
		}
	}
	if older.closes != 0 || older.stops != 0 {
		t.Fatalf("dismissing a tab closed it %d times and interrupted it %d", older.closes, older.stops)
	}
	if a.behind[a.convKey("/tmp/lab/price-scrape.jsonl")] == nil {
		t.Fatal("the keeper stopped holding a conversation that was only put away")
	}
	// AND IT STAYS OFF. The strip is rebuilt from the keeper every frame, so the
	// dismissal has to survive a rebuild or it is a flicker rather than a close.
	for i := 0; i < 3; i++ {
		a.touch()
		_ = a.tabsRow(a.width)
	}
	if strings.Contains(plain(a.tabsRow(a.width)), "openrouter price scrape") {
		t.Fatalf("the dismissed tab came back on a later frame:\n%q", plain(a.tabsRow(a.width)))
	}
}

// AND IT IS STILL ON THE SWITCHER, which is where it comes back from.
func TestADismissedConversationIsStillOnTheSwitcher(t *testing.T) {
	a, _, _ := tabApp(t)
	span := tabCloseSpanFor(t, a, "openrouter price scrape")
	clickTab(t, a, span.from)

	a.hopOpenAll()
	found := false
	for _, row := range a.hop.rows {
		if row.title == "openrouter price scrape" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a put-away conversation left the switcher: %+v", a.hop.rows)
	}
}

// AND GOING BACK TO IT BRINGS THE TAB AND THE UNSENT SENTENCE WITH IT. The
// dismissal is lifted by the one door every road forward goes through
// (keeper.go's [app.rememberOpen]).
func TestReopeningADismissedConversationRestoresItsTabAndItsDraft(t *testing.T) {
	a, _, _ := tabApp(t)
	held := a.behind[a.convKey("/tmp/lab/price-scrape.jsonl")]
	if held == nil {
		t.Fatal("the fixture holds no conversation to put away")
	}
	held.side.draft = "half a sentence"
	cursor := len("half a ")
	held.side.draftCursor = &cursor

	span := tabCloseSpanFor(t, a, "openrouter price scrape")
	clickTab(t, a, span.from)
	if _, ours := a.bringForward("/tmp/lab/price-scrape.jsonl"); !ours {
		t.Fatal("the keeper would not bring back the conversation it was still holding")
	}
	a.touch()
	_ = a.tabsRow(a.width)
	// The dismissal is lifted by the door the switch went through, so the tab is
	// back — as the one in front, wearing whatever this window now calls it.
	if a.tabShut[a.convKey("/tmp/lab/price-scrape.jsonl")] {
		t.Fatal("reopening left the conversation marked dismissed")
	}
	front := false
	for _, hit := range a.chatTabHits {
		if hit.kind == tabHere && hit.tab.file == "/tmp/lab/price-scrape.jsonl" {
			front = true
		}
	}
	if !front {
		t.Fatalf("reopening did not restore the tab:\n%q\n%+v", plain(a.tabsRow(a.width)), a.chatTabHits)
	}
	main := a.mainComposer()
	if got := main.box.String(); got != "half a sentence" {
		t.Fatalf("the draft came back as %q", got)
	}
	if got := main.box.cursor; got != cursor {
		t.Fatalf("the caret came back at %d, want %d", got, cursor)
	}
}

// DISMISSING THE TAB IN FRONT SWITCHES TO ANOTHER CHAT THIS WINDOW HOLDS, and
// closes neither of them.
func TestDismissingTheTabInFrontSwitchesRatherThanClosing(t *testing.T) {
	a, _, _ := tabApp(t)
	before := a.file
	span := tabCloseSpanFor(t, a, "Shipping the parser")
	clickTab(t, a, span.from)
	if a.file == before {
		t.Fatalf("the window stayed on the tab it dismissed: %q", a.file)
	}
	if a.at(pageHome) {
		t.Fatal("the window went home with another conversation to switch to")
	}
	if a.behind[a.convKey(before)] == nil {
		t.Fatal("the dismissed conversation left the keeper — it should still be running")
	}
	a.touch()
	if strings.Contains(plain(a.tabsRow(a.width)), "Shipping the parser") {
		t.Fatalf("the dismissed tab is still drawn:\n%q", plain(a.tabsRow(a.width)))
	}
}

// AND THE LAST TAB GOES HOME. It does not quit and it throws nothing away: the
// conversation is still in front behind the page.
func TestDismissingTheLastTabGoesHomeAndKeepsTheConversation(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.workspace, a.title = "/tmp/lab/this-one.jsonl", "/tmp/lab", "Shipping the parser"
	emptyMachine(a)
	a.width, a.height = 160, 40
	a.touch()
	_ = a.tabsRow(a.width)

	agent, file := a.agent, a.file
	span := tabCloseSpanFor(t, a, "Shipping the parser")
	clickTab(t, a, span.from)
	if !a.at(pageHome) {
		t.Fatalf("dismissing the last tab landed on page %v", a.page)
	}
	if a.file != file || a.agent != agent {
		t.Fatal("dismissing the last tab swapped the conversation out from under home")
	}
	if fake, ok := agent.(*fakeAgent); ok && (fake.closes != 0 || fake.stops != 0) {
		t.Fatal("dismissing the last tab ended the conversation it was standing on")
	}
}

// AND OVER A SHARED ENGINE HANDLE THE TAB IN FRONT GOES HOME TOO, rather than
// switching: that connection holds one conversation at a time, so a switch
// meant only to put a row away would end the work it promised to leave alone.
func TestDismissingTheTabInFrontOverASharedHandleGoesHome(t *testing.T) {
	a, _, _ := tabApp(t)
	a.shared = true
	before := a.file
	span := tabCloseSpanFor(t, a, "Shipping the parser")
	clickTab(t, a, span.from)
	if !a.at(pageHome) {
		t.Fatalf("a shared handle dismissed its front tab to page %v", a.page)
	}
	if a.file != before {
		t.Fatalf("a shared handle switched conversations to dismiss a tab: %q", a.file)
	}
	a.showPage(pageNone)
	a.touch()
	if !strings.Contains(plain(a.tabsRow(a.width)), "Shipping the parser") {
		t.Fatalf("the conversation still in front lost its tab:\n%q", plain(a.tabsRow(a.width)))
	}
}

// ── THE TARGETS ─────────────────────────────────────────────────────────────

// THE CLOSE CELLS ARE HIT-TESTED ON THEIR OWN, and a press on them never falls
// through into selecting the tab.
func TestTheCloseTargetNeverFallsThroughIntoSelectingTheTab(t *testing.T) {
	a, _, _ := tabApp(t)
	before := a.file
	span := tabCloseSpanFor(t, a, "openrouter price scrape")
	label := tabSpanFor(t, a, "openrouter price scrape")
	if span.from < label.to {
		t.Fatalf("the close target overlaps the label: label=%+v close=%+v", label, span)
	}
	for x := span.from; x < span.to; x++ {
		hit, ok := a.tabAt(x, a.tabsLineRow())
		if !ok || hit.kind != tabClose {
			t.Fatalf("column %d of the close target answers as %+v", x, hit)
		}
	}
	clickTab(t, a, span.from)
	if a.file != before {
		t.Fatalf("a press on the ✕ switched conversations: %q", a.file)
	}
}

// AND THE SEPARATORS BETWEEN THE TABS ARE INERT, but the row is still the
// strip's: a press on furniture stops there rather than falling through to
// whatever the next rung of [app.press] would have made of it.
func TestTheSeparatorsAreInertAndTheRowIsStillTheStrips(t *testing.T) {
	a, _, _ := tabApp(t)
	before := a.file
	label := tabSpanFor(t, a, "openrouter price scrape")
	sep := label.from - 1 // the rule in front of the first tab
	if _, ok := a.tabAt(sep, a.tabsLineRow()); ok {
		t.Fatalf("column %d is a separator and answers as a target", sep)
	}
	if _, took := a.tabPress(sep, a.tabsLineRow()); !took {
		t.Fatal("a press on the strip's own furniture fell through the row")
	}
	if a.file != before || a.hop.open {
		t.Fatalf("a press on a separator did something: file=%q card=%v", a.file, a.hop.open)
	}
}

// ── WHAT IS DRAWN ───────────────────────────────────────────────────────────

// THE MARK IS ON THE TAB IN FRONT AND ON THE TAB UNDER THE POINTER, and the
// cells it sits in never change width — so nothing re-packs under a hand moving
// across the row.
func TestTheCloseMarkAppearsUnderThePointerAndTheRowNeverRepacks(t *testing.T) {
	a, _, _ := tabApp(t)
	rest := plain(a.tabsRow(a.width))
	restSpans := append([]tabHit(nil), a.chatTabHits...)
	if got := strings.Count(rest, a.tabCloseWord()); got != 1 {
		t.Fatalf("at rest the row draws %d close marks, want the one on the tab in front:\n%q", got, rest)
	}

	label := tabSpanFor(t, a, "openrouter price scrape")
	a.hot = hoverAt{kind: hoverTab, index: label.from}
	a.chatTabBar = tabBar{}
	hovered := plain(a.tabsRow(a.width))
	if got := strings.Count(hovered, a.tabCloseWord()); got != 2 {
		t.Fatalf("the hovered tab grew no close mark:\n%q", hovered)
	}
	if ansi.StringWidth(hovered) != ansi.StringWidth(rest) {
		t.Fatalf("the row changed width under the pointer: %d then %d\n%q\n%q",
			ansi.StringWidth(rest), ansi.StringWidth(hovered), rest, hovered)
	}
	for at, hit := range a.chatTabHits {
		if at < len(restSpans) && hit.span != restSpans[at].span {
			t.Fatalf("target %d moved under the pointer: %+v then %+v", at, restSpans[at].span, hit.span)
		}
	}
}

// AND THE TAB IN FRONT IS MARKED IN PLAIN TEXT, so a terminal with no colour at
// all still says which conversation you are in.
func TestTheTabInFrontIsMarkedWithoutColour(t *testing.T) {
	a, _, _ := tabApp(t)
	a.pal = newPalette(tokens.NoColor, false)
	a.chatTabBar = tabBar{}
	if got := plain(a.tabsRow(a.width)); !strings.Contains(got, "[Shipping the parser]") {
		t.Fatalf("the tab in front carries no plain-text mark:\n%q", got)
	}
}

// ── THE SWITCHER'S OWN KEY ──────────────────────────────────────────────────

// `ctrl+w` ON THE CARD PUTS THE ROW AWAY AND CLOSES NOTHING, which is the same
// act the ✕ is, said with the key every browser closes a tab with.
func TestCtrlWOnTheSwitcherPutsTheRowAwayWithoutClosingIt(t *testing.T) {
	a, older, _ := tabApp(t)
	a.hopOpenAll()
	at := -1
	for i, row := range a.hop.rows {
		if row.title == "openrouter price scrape" {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("the card has no row to put away: %+v", a.hop.rows)
	}
	a.hop.at = at
	_ = a.hopAway()

	if older.closes != 0 || older.stops != 0 {
		t.Fatalf("ctrl+w closed the agent %d times and interrupted it %d", older.closes, older.stops)
	}
	if a.behind[a.convKey("/tmp/lab/price-scrape.jsonl")] == nil {
		t.Fatal("ctrl+w took the conversation out of the keeper")
	}
	if a.hop.say != hopAwayWord+" · openrouter price scrape" {
		t.Fatalf("the card said %q", a.hop.say)
	}
	if !a.hop.open {
		t.Fatal("the card came down after one row was put away")
	}
	a.touch()
	if strings.Contains(plain(a.tabsRow(a.width)), "openrouter price scrape") {
		t.Fatalf("ctrl+w left the tab on the row:\n%q", plain(a.tabsRow(a.width)))
	}
}

// ── THE `Chats` CONTROL ─────────────────────────────────────────────────────

// IT IS LABELLED, IT FOLLOWS THE TABS, AND IT OPENS THE SWITCHER ON EVERY
// CHAT — not on the ones this window happens to hold.
func TestTheChatsControlOpensTheSwitcherOnEveryChat(t *testing.T) {
	a, _, _ := tabApp(t)
	strip := plain(a.tabsRow(a.width))
	if !strings.Contains(strip, tabsWord) {
		t.Fatalf("the row's right end is not labelled:\n%q", strip)
	}
	var more tabHit
	for _, hit := range a.chatTabHits {
		if hit.kind == tabMore {
			more = hit
		}
	}
	if more.span.to == 0 {
		t.Fatalf("the row drew no switcher control:\n%q", strip)
	}
	// It ends the run of navigation controls, after the tabs and new-chat door.
	if more.span.to < ansi.StringWidth(strip)-1 {
		t.Fatalf("the control is not right-aligned: %+v on a %d-cell row", more.span, ansi.StringWidth(strip))
	}
	clickTab(t, a, more.span.from)
	if !a.hop.open || !a.hop.all {
		t.Fatalf("Chats opened the card as open=%v all=%v", a.hop.open, a.hop.all)
	}
}

// AND THE WORD SURVIVES A NARROW FRAME. What goes first is the `▾`, then the
// count: both are decoration, and the word is what says the control is a door.
func TestTheChatsWordOutlivesItsDecorationOnANarrowFrame(t *testing.T) {
	a, _, _ := tabApp(t)
	seen := false
	for _, width := range []int{160, 120, 100, 80, 70, 60, 50, 44, 40, 34, 30} {
		a.width = width
		a.touch()
		strip := plain(a.tabsRow(width))
		if ansi.StringWidth(strip) > width {
			t.Fatalf("at %d columns the row is %d cells:\n%q", width, ansi.StringWidth(strip), strip)
		}
		if !strings.Contains(strip, tabsWord) {
			continue
		}
		seen = true
		if strings.Contains(strip, tabMoreWord) {
			continue // the widest spelling still fits
		}
		// The mark went and the word stayed, which is the whole of the ladder.
		if !strings.Contains(strip, tabsWord) {
			t.Fatalf("at %d columns the word went before its decoration:\n%q", width, strip)
		}
	}
	if !seen {
		t.Fatal("no width drew the control at all")
	}
}

// ── THE HEADER'S GEOMETRY ───────────────────────────────────────────────────

// EVERY ROW THE HEADER DRAWS IS A ROW THE SCROLLING SUBTRACTED, at every width
// and on the short frames where pieces of it stand down. A row the frame drew
// and the geometry did not charge for puts the page's last line under the box.
func TestTheHeaderPanelIsDrawnAndBudgetedAtEveryFrame(t *testing.T) {
	a := crumbApp(t)
	for _, size := range []struct{ w, h int }{
		{160, 40}, {120, 40}, {80, 40}, {60, 40}, {60, 24}, {80, airyFloor},
		{80, airyFloor - 1}, {80, roomyFloor}, {roomHeadFloor - 1, 40}, {40, 8},
	} {
		a.width, a.height = size.w, size.h
		a.touch()
		rows := strings.Split(frame(a), "\n")
		drawn := a.tabsHeight(size.w) + a.chatRuleHeight(size.w) + len(a.roomHeadRows(size.w)) +
			len(a.roomKinRows(size.w))
		if a.room != nil && a.headHeight() == 0 {
			drawn = 0
		}
		if drawn != a.headHeight() {
			t.Fatalf("at %dx%d the header draws %d rows and is charged %d",
				size.w, size.h, drawn, a.headHeight())
		}
		// A frame with no body region left answers -1 and has nothing to check
		// (view.go's [app.bodyTop]); everywhere else the body starts exactly under
		// what the chrome above it was charged for.
		if a.viewHeight() > 0 && a.bodyTop() != a.headHeight()+a.stripHeight() {
			t.Fatalf("at %dx%d the body starts at %d under a %d-row header",
				size.w, size.h, a.bodyTop(), a.headHeight())
		}
		for _, row := range rows {
			if got := ansi.StringWidth(plain(row)); got > size.w {
				t.Fatalf("at %dx%d a frame row is %d cells:\n%q", size.w, size.h, got, plain(row))
			}
		}
		if len(rows) != size.h {
			t.Fatalf("at %dx%d the frame is %d rows", size.w, size.h, len(rows))
		}
	}
}

// AND WHAT ANSWERS THE POINTER ON EACH OF THOSE ROWS IS WHAT WAS DRAWN ON IT.
// Three controls stack here in whatever order the frame can afford, and every
// one of them resolves through the geometry rather than through a hard-coded
// row number.
func TestEachHeaderRowAnswersForItselfAndForNoOther(t *testing.T) {
	a := crumbApp(t)
	for _, width := range []int{160, 120, 80, 60} {
		a.width = width
		a.touch()
		strings.Join(a.roomHeadRows(width), "\n")
		_ = a.tabsRow(width)
		if _, ok := a.tabAt(headLabelAt+1, a.roomHeadRow()); ok {
			t.Fatalf("at %d columns the tab row answers on the trail's row", width)
		}
		if _, ok := a.crumbAt(headLabelAt+1, a.tabsLineRow()); ok {
			t.Fatalf("at %d columns the trail answers on the tab row", width)
		}
		if _, ok := a.crumbAt(headLabelAt+1, a.roomFactsRow()); ok {
			t.Fatalf("at %d columns the trail answers on the facts row", width)
		}
		if a.roomBackAt(a.roomBackSpan.from, a.roomFactsRow()) {
			t.Fatalf("at %d columns the facts row is the way out", width)
		}
		if a.roomBackSpan.pressable() && !a.roomBackAt(a.roomBackSpan.from, a.roomHeadRow()) {
			t.Fatalf("at %d columns the trail row is not the way out", width)
		}
	}
}

// ── THE ROW COSTS NOTHING TO KEEP DRAWING ───────────────────────────────────

// THE STRIP'S MODEL IS BOUNDED BY THE NUMBER OF TABS IT CAN DRAW and not by the
// number of conversations this window has been in — which the keeper
// deliberately does not cap. It used to walk the whole previous-stack asking a
// linear membership question per key, which is quadratic in something a person
// can grow all day.
func TestTheStripsModelIsBoundedByWhatItCanDraw(t *testing.T) {
	a, _, _ := tabApp(t)
	for i := 0; i < 200; i++ {
		key := "/tmp/lab/held-" + itoa(i) + ".jsonl"
		a.behind[key] = &kept{conv: Conversation{
			Agent: &fakeAgent{model: "m"}, SessionFile: key, Workspace: "/tmp/lab",
		}, side: &aside{since: a.now().Add(-time.Duration(i) * time.Minute), title: "held " + itoa(i)}}
		a.prev = append(a.prev, key)
	}
	a.chatTabs = nil
	a.touch()
	tabs := a.tabList()
	if len(tabs) > tabsCap {
		t.Fatalf("the strip's model is %d tabs and the cap is %d", len(tabs), tabsCap)
	}
	// And the one in front is still on it, which is the law the cap may not break.
	front := false
	for _, tab := range tabs {
		if tab.here {
			front = true
		}
	}
	if !front {
		t.Fatalf("the cap dropped the conversation in front: %+v", tabs)
	}
}
