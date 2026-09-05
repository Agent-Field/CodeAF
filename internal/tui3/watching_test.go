package tui3

// watching_test.go is the half of the one keyboard a person sees: the line that
// stands where the composer was, the draft that survives being unable to send
// it, and the key that takes the keyboard back.
//
// The engine's half — who drives, and the sentence a Submit from the wrong
// window is refused with — is internal/remote's driver_test.go. Nothing here
// opens a pipe: [LinkSeam] is closures precisely so that "what does the screen
// say while another machine is typing" is a string handed over.

import (
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// watched is a surface attached to a conversation another window is typing
// into, with the seam wired the way the --host door wires it.
func watched(t *testing.T, driving Driving) (*app, *[]string) {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "devbox"
	var taken []string
	held := driving
	a.link = LinkSeam{
		Driving: func() Driving { return held },
		Take: func() error {
			taken = append(taken, "take")
			held = Driving{Yours: true}
			return nil
		},
	}
	return a, &taken
}

// ── the line ────────────────────────────────────────────────────────────────

// THE COMPOSER IS REPLACED AND NOT DECORATED. A box a person can type into that
// will not send is worse than no box: the whole of what they need to know is
// where the conversation went and which key brings it back.
func TestAWatcherDrawsOneLineWhereItsComposerWas(t *testing.T) {
	a, _ := watched(t, Driving{Machine: "spark"})

	got := plain(frame(a))
	for _, want := range []string{"typing from spark now", "enter takes it back"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the watcher's line is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, prompt) {
		t.Fatalf("a window that cannot type was still drawn a composer:\n%s", got)
	}
}

// A SECOND WINDOW ON THIS SAME MACHINE IS `another window`, which is the word
// aforge already uses at home for a conversation open somewhere else.
func TestAWindowOnThisMachineIsCalledAnotherWindow(t *testing.T) {
	for _, driving := range []Driving{
		{Machine: "macbook", Here: true},
		{}, // a far end that could not say — the weaker claim, and the true one
	} {
		a, _ := watched(t, driving)
		if got := plain(frame(a)); !strings.Contains(got, "typing from another window now") {
			t.Fatalf("a window on this machine (%+v) reads as:\n%s", driving, got)
		}
	}
}

// The ordinary case draws nothing at all about any of this, which is the whole
// test a good indicator passes.
func TestTheWindowHoldingTheKeyboardIsToldNothing(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "devbox"
	a.link = LinkSeam{Driving: func() Driving { return Driving{Yours: true} }}

	if a.watching() {
		t.Fatal("the window holding the keyboard believes it is watching")
	}
	if got := plain(frame(a)); strings.Contains(got, "takes it back") {
		t.Fatalf("a window that can type was told about the keyboard:\n%s", got)
	}
}

// AND A LOCAL SESSION HAS NO ROOM TO BE A WATCHER IN. No seam, no line, no
// waiting for a hand-over that cannot happen.
func TestALocalSessionIsNeverAWatcher(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if a.watching() {
		t.Fatal("a local session believes another window is typing into it")
	}
	if a.watchDriving() != nil {
		t.Fatal("a local session waits for a keyboard that cannot move")
	}
	if a.takeKeyboard() != nil {
		t.Fatal("a local session has a keyboard to take back")
	}
}

// ── the draft ───────────────────────────────────────────────────────────────

// THE DRAFT IS KEPT. It is not cleared, not sent and not lost — the box is
// simply not on the frame, and the words are exactly where they were when the
// keyboard comes back. A surface that emptied the box because somebody else
// started typing would be throwing away the one thing it owns.
func TestAWatcherKeepsTheDraftItCouldNotSend(t *testing.T) {
	a, taken := watched(t, Driving{Machine: "spark"})
	a.input.insert("the thing I was about to say")

	// Enter does not send it and does not clear it; it asks for the keyboard.
	drive(t, a, key("enter"))
	if len(*taken) != 1 {
		t.Fatalf("enter on a watcher asked for the keyboard %d times", len(*taken))
	}
	if got := a.input.String(); got != "the thing I was about to say" {
		t.Fatalf("the draft is now %q", got)
	}

	// And with the keyboard back, the box is drawn again with the words in it.
	got := plain(frame(a))
	if !strings.Contains(got, "the thing I was about to say") {
		t.Fatalf("the draft did not come back with the keyboard:\n%s", got)
	}
	if strings.Contains(got, "takes it back") {
		t.Fatalf("the watcher's line outlived the watching:\n%s", got)
	}
}

// A CHARACTER TYPED INTO A BOX THAT IS NOT ON THE FRAME IS SWALLOWED, so a
// person cannot fill a draft they cannot see. The line on screen is the answer
// to why nothing happened.
func TestTypingAtAWatcherDoesNotFillAnInvisibleBox(t *testing.T) {
	a, _ := watched(t, Driving{Machine: "spark"})

	drive(t, a, key("h"), key("i"))

	if got := a.input.String(); got != "" {
		t.Fatalf("typing at a watcher wrote %q into the hidden box", got)
	}
}

// EVERYTHING THAT READS STILL WORKS. A watcher is a person watching their own
// work, not a guest — so the door home is exactly where it always was, and the
// gesture that opens it still reaches the bottom of the router.
func TestAWatcherCanStillWalkAwayToHome(t *testing.T) {
	a, _ := watched(t, Driving{Machine: "spark"})

	// Nothing typed reaches the box, spaces included — this was driven over a
	// real connection and `this should be swallowed` walked out of the
	// conversation, because letters were swallowed and the spaces between them
	// were not.
	drive(t, a, key("t"), key("h"), key("i"), key("s"), key(" "), key("w"), key("a"), key("s"), key(" "))
	if got := a.input.String(); got != "" {
		t.Fatalf("typing a sentence at a watcher left %q in the box", got)
	}
	if a.at(pageHome) {
		t.Fatal("the spaces inside a typed sentence opened home")
	}

	// And two CONSECUTIVE spaces are still the door, counted where the keys are.
	if !a.homeDoorOpen() {
		t.Skip("home is not reachable from this test surface")
	}
	drive(t, a, key(" "), key(" "))
	if !a.at(pageHome) {
		t.Fatal("two spaces at a watcher did not open home")
	}
}

// ── taking it back ──────────────────────────────────────────────────────────

// A TAKE-BACK THAT FAILED IS SAID. The two ways it fails are a link that has
// dropped — which the status line is already saying in its own words — and an
// engine that did not answer, and the second is news a person acting on this
// key needs.
func TestATakeBackThatFailedIsSaidAndNotSwallowed(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "devbox"
	a.link = LinkSeam{
		Driving: func() Driving { return Driving{Machine: "spark"} },
		Take:    func() error { return errors.New("the connection to devbox is gone") },
	}

	drive(t, a, runCmd(a.takeKeyboard())...)

	var said bool
	for _, e := range a.entries {
		if e.kind == entryNote && strings.Contains(e.text, "the connection to devbox is gone") {
			said = true
		}
	}
	if !said {
		t.Fatal("a take-back that failed said nothing")
	}
}

// ── the turns this window did not start ─────────────────────────────────────

// A WINDOW THAT IS NOT TYPING IS STILL A WINDOW ONTO THE WORK. Driven over a
// real connection, a watcher sat on a still frame while the other machine's turn
// ran to completion — attached, and showing nothing. The turn is drawn here by
// the code that draws every turn, with the sentence that opened it above the
// reply so the screen has not lost the thread.
func TestAWatcherDrawsTheTurnAnotherWindowStarted(t *testing.T) {
	a, _ := watched(t, Driving{Machine: "spark"})
	events := make(chan session.Event, 4)
	turns := make(chan Following, 1)
	a.link.Follow = func() <-chan Following { return turns }
	turns <- Following{Said: "count to three", Events: events}

	events <- session.Event{Kind: session.EventTextDelta, Text: "one two three"}
	drive(t, a, runCmd(a.watchFollowing())...)

	got := plain(frame(a))
	if !strings.Contains(got, "count to three") {
		t.Fatalf("the message that opened the turn is not above the reply:\n%s", got)
	}
	if !strings.Contains(got, "one two three") {
		t.Fatalf("the watcher did not draw the turn it was handed:\n%s", got)
	}
	// AND THE GREETING GOES. It is dismissed by a keystroke everywhere else and a
	// watcher presses none, so the reply landed under it until this was fixed.
	if a.welcome.open {
		t.Fatalf("the greeting sat on top of the turn:\n%s", got)
	}
}

// A turn already running when this window ARRIVED carries no sentence, because
// the transcript this surface read on its way in already has that message. A
// second copy of it would be the same question asked twice.
func TestATurnAlreadyRunningAddsNoSecondCopyOfTheQuestion(t *testing.T) {
	a, _ := watched(t, Driving{Machine: "spark"})
	events := make(chan session.Event, 4)
	turns := make(chan Following, 1)
	a.link.Follow = func() <-chan Following { return turns }
	turns <- Following{Events: events}

	before := len(a.entries)
	events <- session.Event{Kind: session.EventTextDelta, Text: "carrying on"}
	drive(t, a, runCmd(a.watchFollowing())...)

	for _, e := range a.entries[min(before, len(a.entries)):] {
		if e.kind == entryUser {
			t.Fatalf("a turn with no sentence on it invented one: %q", e.text)
		}
	}
	if got := plain(frame(a)); !strings.Contains(got, "carrying on") {
		t.Fatalf("the turn in flight was not drawn:\n%s", got)
	}
}

// AND A LOCAL SESSION FOLLOWS NOTHING: there is no other window to follow.
func TestALocalSessionFollowsNoOtherWindowsTurn(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if a.watchFollowing() != nil {
		t.Fatal("a local session waited for a turn another window would start")
	}
}

// A hosted wake must not reuse the preceding person's turn. Reusing it makes
// the new tool work hide that person's answer inside the previous work fold.
func TestAHostedWakeKeepsThePreviousAnswerVisible(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.turn = 1
	a.entries = append(a.entries, entry{kind: entryUser, text: "reverse the word", turn: 1})
	a.say("RABANNIC")
	a.closeLive()
	a.settledTurn = 1
	events := make(chan session.Event, 4)
	events <- session.Event{Kind: session.EventToolBegin, Tool: "read", ID: 101}
	events <- session.Event{Kind: session.EventToolEnd, Tool: "read", ID: 101, Output: "BUILD-OK"}
	events <- session.Event{Kind: session.EventTextDelta, Text: "BUILD-OK marker=QUARTZLINE"}
	events <- session.Event{Kind: session.EventTurnDone}
	close(events)
	drive(t, a, followingMsg{turn: Following{Events: events}})
	if a.turn != 2 {
		t.Fatalf("wake inherited previous turn: %d", a.turn)
	}
	got := plain(frame(a))
	for _, want := range []string{"RABANNIC", "BUILD-OK marker=QUARTZLINE"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q after wake:\n%s", want, got)
		}
	}
}

// The engine can open its next turn before this window drains the old tail.
// Queue the new stream instead of discarding an answer already being produced.
func TestAHostedTurnWaitsForThePreviousStreamTail(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	old := make(chan session.Event)
	a.stream = old
	a.turn = 1
	next := make(chan session.Event, 2)
	next <- session.Event{Kind: session.EventTextDelta, Text: "the later answer"}
	next <- session.Event{Kind: session.EventTurnDone}
	close(next)
	a.followTurn(followingMsg{turn: Following{Said: "another question", Events: next}})
	if len(a.follows) != 1 || a.stream != old {
		t.Fatal("the later hosted stream was dropped or interrupted the current one")
	}
	drive(t, a, streamClosedMsg{gen: a.gen})
	if got := plain(frame(a)); !strings.Contains(got, "the later answer") || !strings.Contains(got, "another question") {
		t.Fatalf("queued hosted turn was not drawn:\n%s", got)
	}
}
