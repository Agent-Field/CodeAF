package tui3

import (
	"strings"
	"testing"
)

// ── WHAT `+` PUT DOWN, AND WHOSE IT WAS ─────────────────────────────────────
//
// The start page stands over whatever was on the frame, and `esc` puts that
// thing back. Both halves of that sentence have an owner in them: the page has a
// KIND, and a task has a CONVERSATION. An `esc` that remembered only a number
// got both wrong in the two places where a number is not enough — a run's page
// carries the id zero on purpose, and a task read out of somebody else's
// conversation carries an id this window very probably also uses.
//
// And the third promise the page makes is about a sentence rather than a page:
// the conversation you were in keeps its draft. These hold the door to that
// promise — the create may not CLOSE a conversation that is holding one.

// startDoor wires a lab window with a door onto new conversations and counts
// what it was asked for, so "opening and escaping made nothing" stays an
// assertion in every one of these.
func startDoor(a *app, made *int) {
	a.start = func(string) (Conversation, error) {
		*made++
		return Conversation{
			Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/two.jsonl",
		}, nil
	}
}

// AN ADAPTIVE RUN'S GRAPH COMES BACK, AND SO DOES THE CARD THAT WAS OPEN ON IT.
// A run's page is built with the node id zero on purpose (roomorch.go), which is
// the same zero a window with no page open has — so remembering the id alone
// read this page as "there was nothing here" and dropped somebody's graph on the
// way back.
func TestEscapeFromAnAdaptiveRunPutsTheRunBackAndNotTheConversation(t *testing.T) {
	a, _ := orchApp(t, orchRun4())
	made := 0
	startDoor(a, &made)
	a.orchCardOpen("rfcs")
	if run := a.orchOf(); run == nil || run.card != "rfcs" {
		t.Fatal("the fixture did not open a card on the run's page")
	}

	openStart(t, a)
	if a.roomOpen() {
		t.Fatal("the run's page is still open under the start page")
	}
	drive(t, a, key("esc"))

	run := a.orchOf()
	if run == nil {
		t.Fatalf("esc came back to the conversation instead of the run (room %v)", a.room)
	}
	if run.id != "r1" {
		t.Fatalf("esc opened run %q, want r1", run.id)
	}
	if run.card != "rfcs" {
		t.Fatalf("the card that was open came back as %q, want rfcs", run.card)
	}
	if made != 0 {
		t.Fatalf("opening and cancelling the page over a run made %d conversations", made)
	}
}

// AND ANOTHER CONVERSATION'S TASK NEVER COMES BACK AS THIS ONE'S. The guest page
// is a view onto somebody else's engine and the view was given back when the
// page came down; task numbers restart with every conversation, so reopening by
// number alone drew THIS window's task 7 under the name of the task 7 next door.
// Nothing is opened instead, the surface says so, and no second attach is made
// behind the owner's back.
func TestEscapeFromAnotherConversationsTaskOpensNoLocalTaskOfTheSameNumber(t *testing.T) {
	a, door := guestLab(t)
	made := 0
	startDoor(a, &made)
	enterAway(t, a)
	if !a.roomOpen() || !a.roomIsGuest() {
		t.Fatal("the fixture did not open the other conversation's task")
	}

	openStart(t, a)
	drive(t, a, key("esc"))

	if !a.roomIsGuest() {
		t.Fatal("Escape did not reacquire the original task owner")
	}
	if door.closed != 1 || len(door.asked) != 2 {
		t.Fatalf("guest connection lifecycle: closed=%d asks=%d", door.closed, len(door.asked))
	}
	if door.asked[0] != door.asked[1] {
		t.Fatal("Escape attached a different owner")
	}
	if made != 0 {
		t.Fatalf("opening and cancelling the page over a guest made %d conversations", made)
	}
}

// AND THIS CONVERSATION'S OWN NODE COMES BACK UNDER THE NAME IT WAS WEARING,
// rather than under its number. The page is rebuilt through the door a rail
// click uses, and that door takes a title for exactly this reason.
func TestEscapeFromANodePutsItBackUnderTheNameItHad(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = roomJournal(t, `{"type":"message","role":"user","content":"go"}`)
	made := 0
	startDoor(a, &made)
	a.openRoom(7, "Fix the nil-map crash")
	if !a.roomOpen() {
		t.Fatal("the fixture did not open the node's page")
	}

	openStart(t, a)
	drive(t, a, key("esc"))

	if !a.roomOpen() || a.room.id != 7 {
		t.Fatalf("esc did not put them back on the node's page (open %v)", a.roomOpen())
	}
	if a.room.title != "Fix the nil-map crash" {
		t.Fatalf("the node came back titled %q", a.room.title)
	}
	if made != 0 {
		t.Fatalf("opening and cancelling the page over a node made %d conversations", made)
	}
}

// ── THE DRAFT STAYS WITH ITS OWN CONVERSATION ───────────────────────────────

// A FRESH CONVERSATION HOLDING AN UNSENT SENTENCE IS KEPT AND NOT CLOSED. /new
// may replace a fresh and empty conversation because nothing is in it — but a
// half-written message IS something in it, and `+`'s whole promise is that the
// conversation you were in keeps its draft. Closing it would leave that sentence
// with no owner, which is how it used to end up in the box of a conversation it
// was never addressed to.
func TestAFreshConversationHoldingAnUnsentLineIsKeptWithIt(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.turn = 0 // fresh and empty but for the words in the box
	a.input.setText("something I never sent")
	openStart(t, a)
	drive(t, a, key("g"), key("o"))
	drive(t, a, key("enter"))

	if lab.made != 1 {
		t.Fatalf("the door was asked %d times", lab.made)
	}
	if lab.agent.closes != 0 {
		t.Fatal("the create closed the conversation that was holding an unsent sentence")
	}
	held := a.behind[a.convKey("/tmp/lab/one.jsonl")]
	if held == nil {
		t.Fatalf("the conversation was not kept: %d held", len(a.behind))
	}
	if held.side == nil || held.side.draft != "something I never sent" {
		t.Fatalf("the kept conversation's own sentence is %q", held.side.draft)
	}
	// AND NOTHING RODE OUT ON THE FIRST MESSAGE. The new conversation's box is
	// the page's own, emptied by the send, and it is not holding somebody else's
	// paragraph.
	if got := a.input.String(); got != "" {
		t.Fatalf("the new conversation's box holds %q", got)
	}
	if notes := strings.Join(noteTexts(a), "\n"); strings.Contains(notes, "kept your unsent sentence") {
		t.Fatalf("the old sentence was handed forward instead of kept:\n%v", notes)
	}
	if len(lab.next.sent) != 1 || lab.next.sent[0] != "go" {
		t.Fatalf("the new conversation was sent %v", lab.next.sent)
	}
}

// AND THE ONE CONVERSATION THE KEEPER CANNOT HOLD HANDS ITS SENTENCE BACK. With
// no transcript to key it by there is no tab to reach it through, so keeping it
// would mean dropping it silently; it is closed as before and the words go where
// the person can see them (keeper.go's [app.stow] refuses on exactly this).
func TestAnUnnamedChatsDraftRefusesCreationBeforeAnythingIsLost(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.turn = 0
	a.file = ""
	a.input.setText("old unnamed draft")
	openStart(t, a)
	a.input.setText("new draft")
	drive(t, a, key("enter"))
	if lab.made != 0 || !a.startingChat() || a.input.String() != "new draft" || a.welcome.msg != startDraftUnownedWord {
		t.Fatal("unnamed draft protection failed")
	}
	drive(t, a, key("esc"))
	if a.input.String() != "old unnamed draft" {
		t.Fatal("old draft was lost")
	}
}

// AND A CONVERSATION WITH WORK IN IT IS KEPT WHETHER OR NOT ITS BOX IS EMPTY,
// which is the case this door has always got right and must go on getting right:
// the amendment above may not turn into a second rule about when /new replaces.
func TestAConversationWithWorkInItIsStillKeptAndItsEmptyBoxChangesNothing(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app() // the lab's conversation has run a turn
	openStart(t, a)
	drive(t, a, key("g"), key("o"))
	drive(t, a, key("enter"))

	if lab.agent.closes != 0 {
		t.Fatal("the create closed a conversation that had run something")
	}
	if a.behind[a.convKey("/tmp/lab/one.jsonl")] == nil {
		t.Fatalf("the running conversation was not kept: %d held", len(a.behind))
	}
}

// ── A COMMAND TYPED ON THE PAGE ─────────────────────────────────────────────

// A SLASH COMMAND RUN OFF THE START PAGE MAY NOT ACT ON THE CONVERSATION BEHIND
// IT. The command list hangs under the page's own box, and the conversation it
// is drawn over is hidden — so a command that reached it would close, clear or
// retarget work the person cannot see, at a page whose entire promise is that it
// touches nothing.
func TestACommandChosenOnTheStartPageDoesNotReachTheConversationBehind(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	openStart(t, a)
	for _, r := range "/quit" {
		drive(t, a, key(string(r)))
	}
	if !a.menu.open {
		t.Fatal("typing a command on the start page opened no list")
	}
	drive(t, a, key("enter"))

	if lab.agent.closes != 0 {
		t.Fatal("/quit chosen on the start page closed the conversation behind it")
	}
	// AND IT WENT THE WAY A COMMAND TYPED OUT IN FULL GOES: through the page, so
	// the conversation it acts on is the one the page made.
	if lab.made != 1 {
		t.Fatalf("the row was dispatched without the page's own road: %d creates", lab.made)
	}
	if a.startingChat() {
		t.Fatal("the page is still standing over a command it ran")
	}
}
