package tui3

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ── ctrl+shift+t PUTS THE LAST TAB BACK ─────────────────────────────────────
//
// This file holds the claim [app.reopenTabKey] makes — that the chord undoes the
// last `ctrl+w`, again and again, in reverse — and the ways it could quietly stop
// being true: that it never fabricates a conversation, that it skips a tab
// somebody already brought back by hand, that leaning on it cannot reopen one
// conversation twice in a row, that `ctrl+t` is untouched, and that the new-chat
// page is deliberately not on the stack at all.

// reopenPress is the chord as a terminal that can spell it sends it. It is built
// from the fields rather than through the suite's own `key` helper, because a
// shifted control key is exactly the event that helper has no spelling for — and
// what this whole feature hangs off is that the terminal CAN tell it from ctrl+t.
func reopenPress() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl | tea.ModShift}
}

// reopenApp is [closeKeyApp]'s fixture: a conversation in front and two more in
// the keeper, with the strip laid out once so the row holds their names.
func reopenApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.workspace, a.title = "/tmp/lab/this-one.jsonl", "/tmp/lab", "Shipping the parser"
	keepThree(t, a)
	a.width, a.height = 160, 40
	a.touch()
	_ = a.tabsRow(a.width)
	return a
}

// THE CHORD IS A KEY THE TERMINAL CAN NAME. Everything below is asserted through
// the real dispatch, so this is the one thing worth asserting directly: the event
// a terminal sends for the shifted chord spells the constant this surface matches.
func TestTheReopenChordIsADistinctKeyEvent(t *testing.T) {
	if got := reopenPress().String(); got != reopenTabChord {
		t.Fatalf("the shifted chord arrives as %q, and the surface matches %q", got, reopenTabChord)
	}
	if reopenTabChord == newChatChord {
		t.Fatal("the reopen chord and the new-chat chord are the same string")
	}
}

// THE TAB YOU JUST SHUT COMES BACK, through the real key dispatch, and the
// conversation it comes back to is the one that was there — not a fresh one.
func TestCtrlShiftTReopensTheTabJustClosed(t *testing.T) {
	a := reopenApp(t)
	shut := a.convKey("/tmp/lab/this-one.jsonl")

	drive(t, a, key(closeTabChord))
	if !a.tabShut[shut] {
		t.Fatal("ctrl+w did not dismiss the tab in front")
	}

	drive(t, a, reopenPress())
	if a.tabShut[shut] {
		t.Fatal("ctrl+shift+t left the conversation marked dismissed")
	}
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("ctrl+shift+t landed on %q", a.file)
	}
}

// AND LEANING ON IT WALKS BACK THROUGH THE CLOSURES IN REVERSE. Two tabs shut in
// order come back newest first, which is the only order a person can predict.
func TestCtrlShiftTWalksBackThroughSuccessiveClosures(t *testing.T) {
	a := reopenApp(t)
	first := a.convKey("/tmp/lab/this-one.jsonl")

	drive(t, a, key(closeTabChord))
	second := a.convKey(a.file)
	landed := a.file
	if landed == "/tmp/lab/this-one.jsonl" {
		t.Fatal("the first close did not leave the conversation it shut")
	}
	drive(t, a, key(closeTabChord))

	drive(t, a, reopenPress())
	if a.file != landed {
		t.Fatalf("the first reopen landed on %q, and %q was shut last", a.file, landed)
	}
	if a.tabShut[second] {
		t.Fatal("the most recent closure was reopened without lifting its dismissal")
	}

	drive(t, a, reopenPress())
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("the second reopen landed on %q, and the first closure was this-one", a.file)
	}
	if a.tabShut[first] {
		t.Fatal("the older closure came back still marked dismissed")
	}
}

// A TAB DISMISSED WHILE IT WAS NOT IN FRONT COMES BACK TOO, with the address and
// the workspace the row was holding for it — that metadata is the whole of what
// the stack keeps, and it is read off the row before the row loses it.
func TestCtrlShiftTReopensATabDismissedFromBehind(t *testing.T) {
	a := reopenApp(t)
	var behind chatTab
	for _, tab := range a.chatTabs {
		if !tab.here {
			behind = tab
			break
		}
	}
	if behind.key == "" {
		t.Fatal("the fixture drew no tab that was not in front")
	}

	_ = a.tabDismiss(behind)
	if !a.tabShut[behind.key] {
		t.Fatal("dismissing a tab from behind did not mark it")
	}
	if got := a.closedTabs[len(a.closedTabs)-1]; got.file != behind.file || got.where != behind.where || got.word != behind.word {
		t.Fatalf("the stack kept %+v, and the row said file=%q where=%q word=%q", got, behind.file, behind.where, behind.word)
	}
	if got := a.closedTabs[len(a.closedTabs)-1]; got.here || got.held || got.start {
		t.Fatalf("the stack kept a claim about a frame: %+v", got)
	}

	drive(t, a, reopenPress())
	if a.tabShut[behind.key] {
		t.Fatal("ctrl+shift+t left the tab dismissed")
	}
	if a.file != behind.file {
		t.Fatalf("ctrl+shift+t landed on %q, and the dismissed tab was %q", a.file, behind.file)
	}
}

// THE UNSENT SENTENCE AND ITS CARET COME BACK WITH THE TAB, because the reopen
// is [app.tabGo] and nothing else — the same switch the strip and the switcher
// make.
func TestCtrlShiftTKeepsTheDraftOfTheTabItReopens(t *testing.T) {
	a := reopenApp(t)
	a.input.setText("half a sentence")
	a.input.cursor = len("half a ")

	drive(t, a, key(closeTabChord))
	drive(t, a, reopenPress())
	a.touch()
	_ = a.tabsRow(a.width)

	main := a.mainComposer()
	if got := main.box.String(); got != "half a sentence" {
		t.Fatalf("the draft came back as %q", got)
	}
	if got := main.box.cursor; got != len("half a ") {
		t.Fatalf("the caret came back at %d, want %d", got, len("half a "))
	}
}

// A TAB SOMEBODY ALREADY BROUGHT BACK BY HAND IS STEPPED OVER. Every road to the
// front lifts the dismissal ([app.rememberOpen]), so an entry whose tab is on the
// row again is not a tab anybody is asking for.
func TestCtrlShiftTSkipsATabAlreadyReopenedByHand(t *testing.T) {
	a := reopenApp(t)
	shut := a.convKey("/tmp/lab/this-one.jsonl")

	drive(t, a, key(closeTabChord))
	landed := a.file
	// The road a person takes from the switcher or from home.
	if _, ours := a.bringForward("/tmp/lab/this-one.jsonl"); !ours {
		t.Fatal("the keeper would not bring the conversation back by hand")
	}
	if a.tabShut[shut] {
		t.Fatal("coming back by hand left the tab dismissed")
	}

	drive(t, a, reopenPress())
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("ctrl+shift+t moved the window to %q with nothing left to reopen", a.file)
	}
	if len(a.closedTabs) != 0 {
		t.Fatalf("the stack still holds %d entries after its only one was reopened by hand", len(a.closedTabs))
	}
	_ = landed
}

// SHUTTING THE SAME TAB TWICE IS ONE THING ON THE STACK. Without that, leaning on
// the chord would reopen one conversation twice with nothing in between.
func TestReclosingATabDoesNotDuplicateItOnTheStack(t *testing.T) {
	a := reopenApp(t)

	drive(t, a, key(closeTabChord))
	drive(t, a, reopenPress())
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("the reopen landed on %q", a.file)
	}
	drive(t, a, key(closeTabChord))

	count := 0
	for _, tab := range a.closedTabs {
		if tab.file == "/tmp/lab/this-one.jsonl" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the stack holds %d entries for one conversation", count)
	}
}

// AND THE STACK IS BOUNDED BY THE ROW'S OWN CAP. A window may be in more
// conversations than that; a list without a bound would hold every name of a long
// day for a gesture that walks the last few.
func TestTheReopenStackIsBoundedByTheTabCap(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	emptyMachine(a)
	made := make([]chatTab, 0, tabsCap+8)
	for at := 0; at < tabsCap+8; at++ {
		made = append(made, chatTab{key: "k" + itoa(at), file: "/tmp/lab/" + itoa(at) + ".jsonl", where: "/tmp/lab", word: "chat " + itoa(at)})
	}
	a.chatTabs = made
	for _, tab := range made {
		a.rememberClosedTab(tab.key)
	}
	if len(a.closedTabs) != tabsCap {
		t.Fatalf("the stack holds %d entries, and the cap is %d", len(a.closedTabs), tabsCap)
	}
	if got := a.closedTabs[len(a.closedTabs)-1].key; got != made[len(made)-1].key {
		t.Fatalf("the newest closure came out as %q", got)
	}
	if got := a.closedTabs[0].key; got != made[len(made)-tabsCap].key {
		t.Fatalf("the oldest kept closure is %q, and the cap should have dropped everything before %q", got, made[len(made)-tabsCap].key)
	}
}

// WITH NOTHING SHUT IT DOES NOTHING, AND TYPES NOTHING. A chord is never a letter
// of anybody's sentence, and one whose effect depended on how many tabs you had
// closed would be a chord nobody could trust.
func TestCtrlShiftTWithNothingShutDoesNothing(t *testing.T) {
	a := reopenApp(t)
	a.input.setText("read the config file")

	drive(t, a, reopenPress())
	if got := a.input.String(); got != "read the config file" {
		t.Fatalf("ctrl+shift+t changed the draft to %q", got)
	}
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("ctrl+shift+t moved the window to %q", a.file)
	}
	if a.at(pageHome) {
		t.Fatal("ctrl+shift+t left the conversation")
	}
}

// IT WORKS FROM HOME, WHICH IS WHERE SHUTTING THE LAST TAB PUTS YOU. That is the
// press this chord most has to undo, and home is a place — modal above the rung
// the chord is ordinarily read at — so the reopen is read on home explicitly
// (input.go) and takes the place down on its way through.
func TestCtrlShiftTReopensTheLastTabFromHome(t *testing.T) {
	only := &fakeAgent{model: "m"}
	a := newTestApp(only)
	a.file, a.workspace, a.title = "/tmp/lab/this-one.jsonl", "/tmp/lab", "Shipping the parser"
	emptyMachine(a)
	a.width, a.height = 160, 40
	a.touch()
	_ = a.tabsRow(a.width)
	shut := a.convKey("/tmp/lab/this-one.jsonl")

	drive(t, a, key(closeTabChord))
	if !a.at(pageHome) {
		t.Fatal("shutting the only tab did not leave the window on home")
	}

	drive(t, a, reopenPress())
	if a.at(pageHome) {
		t.Fatal("ctrl+shift+t on home left the window standing on home")
	}
	if a.tabShut[shut] {
		t.Fatal("ctrl+shift+t left the conversation marked dismissed")
	}
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("ctrl+shift+t landed on %q", a.file)
	}
	if only.closes != 0 || only.stops != 0 {
		t.Fatalf("the round trip closed the conversation %d times and interrupted it %d", only.closes, only.stops)
	}
}

// ── WHAT THE CHORD DOES NOT TAKE ────────────────────────────────────────────

// ctrl+t IS STILL A NEW CHAT, and a terminal that collapses the shifted chord
// into it gets exactly that rather than a reopen. This is the trade stated on the
// key sheet: the plain chord is never intercepted here.
func TestCtrlTIsStillANewChatAndIsNeverReadAsAReopen(t *testing.T) {
	a := reopenApp(t)
	drive(t, a, key(closeTabChord))
	shut := len(a.closedTabs)
	if shut == 0 {
		t.Fatal("the fixture shut no tab to reopen")
	}

	// The event a legacy terminal sends for both presses. It is the new-chat
	// chord's own spelling, and it reopens nothing.
	legacy := tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}
	if got := legacy.String(); got != newChatChord {
		t.Fatalf("the collapsed chord arrives as %q, want %q", got, newChatChord)
	}
	was := a.file
	drive(t, a, legacy)
	if len(a.closedTabs) != shut {
		t.Fatalf("ctrl+t consumed %d entries off the reopen stack", shut-len(a.closedTabs))
	}
	if a.file != was {
		t.Fatalf("ctrl+t brought a shut tab back: the window moved to %q", a.file)
	}
}

// A MODAL THAT IS LOOKING AT THE PERSON KEEPS THE KEY. The chord is read at
// [app.closeTabKey]'s rung, under every overlay, so nothing reopens behind one.
func TestCtrlShiftTUnderAModalReopensNothing(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	a := reopenApp(t)
	a.models = func() []Model { return []Model{{ID: "deepseek/deepseek-v4-flash"}} }
	drive(t, a, key(closeTabChord))
	was, shut := a.file, len(a.closedTabs)
	a.openPicker()

	drive(t, a, reopenPress())
	if !a.pick.open {
		t.Fatal("ctrl+shift+t closed the model picker")
	}
	if a.file != was {
		t.Fatalf("ctrl+shift+t reopened a tab under an open picker: the window is on %q", a.file)
	}
	if len(a.closedTabs) != shut {
		t.Fatal("ctrl+shift+t consumed the reopen stack under an open picker")
	}
}

// THE NEW-CHAT PAGE IS DELIBERATELY NOT ON THE STACK. Its tab is synthetic —
// nothing has been created — and its own way back is `ctrl+t`, which hands back
// the first message it parked.
func TestTheNewChatPageIsNotOnTheReopenStack(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()

	drive(t, a, key(newChatChord))
	if !a.startingChat() {
		t.Fatal("ctrl+t did not open the start page")
	}
	typeInto(t, a, "a first message nobody has sent")
	drive(t, a, key(closeTabChord))
	if a.startingChat() {
		t.Fatal("ctrl+w left the start page standing")
	}
	if len(a.closedTabs) != 0 {
		t.Fatalf("closing the start page put %d entries on the reopen stack", len(a.closedTabs))
	}

	// The chord finds nothing, and the conversation the page was drawn over stays.
	drive(t, a, reopenPress())
	if a.startingChat() {
		t.Fatal("ctrl+shift+t raised the start page again")
	}
	if a.file != "/tmp/lab/one.jsonl" {
		t.Fatalf("ctrl+shift+t moved the window to %q", a.file)
	}

	// AND THE PAGE'S OWN WAY BACK STILL CARRIES THE PARKED SENTENCE.
	drive(t, a, key(newChatChord))
	if !a.startingChat() {
		t.Fatal("ctrl+t did not open the start page again")
	}
	if got := a.input.String(); got != "a first message nobody has sent" {
		t.Fatalf("the start page came back holding %q", got)
	}
	if lab.made != 0 {
		t.Fatalf("the page made %d conversations", lab.made)
	}
}
