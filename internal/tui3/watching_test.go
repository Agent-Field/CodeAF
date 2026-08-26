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

	// The gesture is read at the very bottom of the router, so the character
	// has to reach it: a watcher that swallowed the space would have the one
	// door out of it quietly bricked.
	if _, taken := a.watchKey(key(" ")); taken {
		t.Fatal("a watcher swallowed the space the door home is made of")
	}
	if _, taken := a.watchKey(key("h")); !taken {
		t.Fatal("a watcher let an ordinary character into a box that is not drawn")
	}
	drive(t, a, key(" "))
	if got := a.input.String(); got != " " {
		t.Fatalf("the space did not reach the box the gesture reads: %q", got)
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
