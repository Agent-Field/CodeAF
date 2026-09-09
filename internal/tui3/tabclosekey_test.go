package tui3

import (
	"strings"
	"testing"
)

// ── ctrl+w SHUTS THE TAB IN FRONT ───────────────────────────────────────────
//
// This file holds the claim [app.closeTabKey] makes — that the keyboard closes a
// tab exactly the way the ✕ on it does — and the four ways it could quietly stop
// being true: that it ends no work, that it keeps every draft, that the modal
// surfaces which spell ctrl+w their own way still get it, and that the two other
// names of the word kill still reach the message box.

// closeKeyApp is [tabApp]'s fixture with a handle on the agent IN FRONT, which
// is what the claims below about ending nothing are asserted against.
func closeKeyApp(t *testing.T) (*app, *fakeAgent, *fakeAgent, *fakeAgent) {
	t.Helper()
	front := &fakeAgent{model: "m"}
	a := newTestApp(front)
	a.file, a.workspace, a.title = "/tmp/lab/this-one.jsonl", "/tmp/lab", "Shipping the parser"
	older, newer := keepThree(t, a)
	a.width, a.height = 160, 40
	a.touch()
	_ = a.tabsRow(a.width)
	return a, front, older, newer
}

// THE CONVERSATION ON SCREEN GOES AWAY AND EVERYTHING IN IT KEEPS RUNNING. The
// window lands on another tab it is holding, which is what the ✕ on the front tab
// does (chattabs.go's [app.tabDismiss]), and the agent behind the one that left
// is neither closed nor interrupted.
func TestCtrlWClosesTheTabInFrontAndEndsNothing(t *testing.T) {
	a, front, older, newer := closeKeyApp(t)
	a.input.setText("half a sentence")
	a.input.cursor = len("half a ")

	drive(t, a, key(closeTabChord))
	a.touch()
	strip := plain(a.tabsRow(a.width))

	if strings.Contains(strip, "Shipping the parser") {
		t.Fatalf("ctrl+w left the closed tab on the row:\n%q", strip)
	}
	if a.file != "/tmp/lab/rail-scope.jsonl" {
		t.Fatalf("ctrl+w landed on %q, and the window was holding rail-scope", a.file)
	}
	for name, agent := range map[string]*fakeAgent{"the one closed": front, "an older one": older, "the newer one": newer} {
		if agent.closes != 0 || agent.stops != 0 {
			t.Fatalf("ctrl+w closed %s %d times and interrupted it %d", name, agent.closes, agent.stops)
		}
	}
	if a.behind[a.convKey("/tmp/lab/this-one.jsonl")] == nil {
		t.Fatal("ctrl+w took the conversation out of the keeper")
	}
	if !a.tabShut[a.convKey("/tmp/lab/this-one.jsonl")] {
		t.Fatal("ctrl+w left the tab un-dismissed, so the next frame will draw it again")
	}
}

// AND THE UNSENT SENTENCE AND ITS CARET ARE WAITING WHEN YOU COME BACK, which is
// the half of the promise a person actually notices.
func TestCtrlWKeepsTheDraftAndTheCaretOfTheTabItClosed(t *testing.T) {
	a, _, _, _ := closeKeyApp(t)
	a.input.setText("half a sentence")
	a.input.cursor = len("half a ")

	drive(t, a, key(closeTabChord))
	if _, ours := a.bringForward("/tmp/lab/this-one.jsonl"); !ours {
		t.Fatal("the keeper would not bring back the conversation ctrl+w put away")
	}
	a.touch()
	_ = a.tabsRow(a.width)

	main := a.mainComposer()
	if got := main.box.String(); got != "half a sentence" {
		t.Fatalf("the draft came back as %q", got)
	}
	if got := main.box.cursor; got != len("half a ") {
		t.Fatalf("the caret came back at %d, want %d", got, len("half a "))
	}
	if a.tabShut[a.convKey("/tmp/lab/this-one.jsonl")] {
		t.Fatal("coming back left the conversation marked dismissed")
	}
}

// LEANING ON IT IS CLICKING THE ✕ OVER AND OVER. Each press takes the tab in
// front off the row and never touches the work behind it; the last one leaves the
// window on home with every conversation still alive.
func TestCtrlWPressedAgainAndAgainClosesTabsAndNeverInterrupts(t *testing.T) {
	a, front, older, newer := closeKeyApp(t)
	for i := 0; i < 4; i++ {
		drive(t, a, key(closeTabChord))
		a.touch()
	}
	if !a.at(pageHome) {
		t.Fatal("closing the last tab did not leave the window on home")
	}
	for name, agent := range map[string]*fakeAgent{"the front one": front, "an older one": older, "the newer one": newer} {
		if agent.closes != 0 || agent.stops != 0 {
			t.Fatalf("repeated ctrl+w closed %s %d times and interrupted it %d", name, agent.closes, agent.stops)
		}
	}
}

// A WINDOW WITH ONE TAB GOES HOME, with the conversation still in front behind
// it — home is a place in this window and not a door out of the program.
func TestCtrlWOnTheLastTabGoesHomeWithTheWorkAlive(t *testing.T) {
	only := &fakeAgent{model: "m"}
	a := newTestApp(only)
	a.file, a.workspace, a.title = "/tmp/lab/this-one.jsonl", "/tmp/lab", "Shipping the parser"
	emptyMachine(a)
	a.width, a.height = 160, 40
	a.touch()
	_ = a.tabsRow(a.width)

	drive(t, a, key(closeTabChord))
	if !a.at(pageHome) {
		t.Fatal("ctrl+w on the only tab did not go home")
	}
	if only.closes != 0 || only.stops != 0 {
		t.Fatalf("ctrl+w closed the conversation %d times and interrupted it %d", only.closes, only.stops)
	}
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("the conversation behind home is %q", a.file)
	}
}

// IN A TASK ROOM IT IS THE SAME KEY DOING THE SAME THING. The room is drawn over
// the conversation rather than instead of it, so the tab in front is still the
// conversation's — and nothing running in the task is disturbed.
//
// IT NOW ASKS ON THE WAY, and that is the only thing about this that moved: a
// conversation with a task running is one the close card is raised for
// (tabclose.go), and `keep running` — the answer under `enter`, the one the
// cursor opens on — is precisely the act this test has always described.
func TestCtrlWInATaskRoomClosesTheTabAndLeavesTheTaskRunning(t *testing.T) {
	a, fake, _ := roomApp(t)
	a.width, a.height = 160, 40
	drive(t, a, key("right"))
	if !a.roomOpen() {
		t.Fatal("the fixture did not open a room to close from")
	}
	a.input.setText("a note about the crash")
	_ = a.tabsRow(a.width)

	drive(t, a, key(closeTabChord))
	if !a.closingTab() {
		t.Fatal("ctrl+w over a running task did not ask before closing the tab")
	}
	if a.tabClose.pick != tabCloseKeepAt {
		t.Fatalf("the card opened on %q rather than on keep running", tabCloseAnswers[a.tabClose.pick])
	}
	drive(t, a, key("enter"))
	if !a.at(pageHome) {
		t.Fatal("ctrl+w in a room did not take the window off the conversation")
	}
	if fake.stops != 0 {
		t.Fatalf("ctrl+w interrupted the task %d times", fake.stops)
	}
}

// ON THE NEW-CHAT PAGE IT IS THAT PAGE'S OWN ✕, which is `esc`: the page comes
// down, the conversation underneath comes back, and the half-written first
// message is parked rather than thrown away ([app.cancelChatStart]).
func TestCtrlWOnTheNewChatPageClosesThePageAndParksItsDraft(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.input.setText("half a sentence about the parser")

	drive(t, a, key(newChatChord))
	if !a.startingChat() {
		t.Fatal("ctrl+t did not open the start page")
	}
	typeInto(t, a, "a first message nobody has sent")

	drive(t, a, key(closeTabChord))
	if a.startingChat() {
		t.Fatal("ctrl+w left the start page standing")
	}
	// THE CONVERSATION IT WAS DRAWN OVER IS BACK, WITH ITS OWN SENTENCE, AND WAS
	// NEVER CLOSED. Nothing is created by the new-tab chord, so there is nothing
	// for this one to end.
	if a.file != "/tmp/lab/one.jsonl" {
		t.Fatalf("closing the start page landed on %q", a.file)
	}
	if got := a.input.String(); got != "half a sentence about the parser" {
		t.Fatalf("the conversation came back holding %q", got)
	}
	if lab.made != 0 {
		t.Fatalf("the page made %d conversations on its way up and down", lab.made)
	}
	if lab.agent.closes != 0 || lab.agent.stops != 0 {
		t.Fatal("closing the start page ended the conversation underneath it")
	}
	parked := a.startKept
	if got := parked.box.String(); got != "a first message nobody has sent" {
		t.Fatalf("the parked first message came out as %q", got)
	}
}

// A HELD ROSTER GIVES THE KEYBOARD BACK ON THE WAY, on [app.newChatKey]'s
// principle: the screen about to be drawn is somewhere else, and a keyboard
// pointed at a list nobody can see is a keyboard nobody can find.
func TestCtrlWFromAHeldRosterReleasesTheHoldAndCloses(t *testing.T) {
	a, _, _, _ := closeKeyApp(t)
	a.railHold = true

	drive(t, a, key(closeTabChord))
	if a.railHold {
		t.Fatal("ctrl+w left the roster holding the keyboard")
	}
	if !a.tabShut[a.convKey("/tmp/lab/this-one.jsonl")] {
		t.Fatal("ctrl+w was eaten by the held roster instead of closing the tab")
	}
}

// ── WHAT THE CHORD DOES NOT TAKE ────────────────────────────────────────────

// THE MODAL FILTERS KEEP IT. Every overlay that has a search box is looking at
// the person who pressed the key and is read far above this chord (input.go), so
// ctrl+w still edits the filter there — and closes no tab while it does.
func TestCtrlWStillEditsAModalFilterAndClosesNoTab(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "m"}, []Model{{ID: "deepseek/deepseek-v4-flash"}})
	a.file, a.workspace = "/tmp/lab/this-one.jsonl", "/tmp/lab"
	a.width, a.height = 160, 40
	a.openPicker()
	a.pick.filter.setText("read the config file")

	drive(t, a, key(closeTabChord))
	if got := a.pick.filter.String(); got != "read the config " {
		t.Fatalf("ctrl+w left the picker's filter as %q", got)
	}
	if a.tabShut[a.convKey("/tmp/lab/this-one.jsonl")] {
		t.Fatal("ctrl+w closed the tab under an open picker")
	}
	if a.at(pageHome) {
		t.Fatal("ctrl+w under an open picker left the conversation")
	}
}

// AND THE WORD KILL KEEPS THE TWO NAMES A HAND ACTUALLY PRESSES. What the chord
// took from the message box is readline's third spelling of the edit; the Mac
// one and the Windows one both still delete the word behind the caret, which is
// what makes the trade payable.
func TestTheWordKillKeepsItsOtherTwoNamesInTheMessageBox(t *testing.T) {
	for _, name := range []string{"alt+backspace", "ctrl+backspace"} {
		_, a := wired(nil)
		a.input.setText("read the config file")
		drive(t, a, key(name))
		if got := a.input.String(); got != "read the config " {
			t.Fatalf("%s deleted %q, want the last word", name, got)
		}
	}
	// AND ctrl+w NO LONGER EATS A WORD OF THE SENTENCE. A key that both closed
	// the tab and edited the draft would be a key nobody could predict.
	_, a := wired(nil)
	a.input.setText("read the config file")
	drive(t, a, key(closeTabChord))
	if got := a.input.String(); got != "read the config file" {
		t.Fatalf("ctrl+w changed the draft to %q", got)
	}
}
