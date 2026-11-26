package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// keyboardOf builds a live view whose keyboard fact the test moves, with a Take
// that behaves as the engine does (it hands the keyboard to this view) and counts
// how many times it was asked.
func keyboardOf(t *testing.T, holder Driving) (*app, *fakeAgent, *Driving, *int) {
	t.Helper()
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.width = 100
	a.title = "chat"
	a.dismissWelcome()
	driving := holder
	taken := 0
	a.takeUp(Conversation{
		Agent: agent,
		Link: &LinkSeam{
			Driving: func() Driving { return driving },
			Take: func() error {
				taken++
				driving = Driving{Yours: true}
				return nil
			},
		},
		Live: &LiveSeam{
			Host: "Desktop",
			Read: func() LiveRead { return LiveRead{Kind: LiveOn} },
		},
	}, true)
	return a, agent, &driving, &taken
}

// R6: a live view that does not hold the keyboard says so on its frame, whether
// the box is empty or holds a draft, and whether or not the engine has told it
// the holder's name. When it holds the keyboard that line is not shown.
func TestR6ALiveViewWithoutTheKeyboardSaysSoWhateverTheBoxHolds(t *testing.T) {
	const draft = "words kept in the box"
	for _, tc := range []struct {
		name    string
		driving Driving
		text    string
	}{
		{"named holder, empty box", Driving{Yours: false, Holder: "Desktop"}, ""},
		{"named holder, a draft", Driving{Yours: false, Holder: "Desktop"}, draft},
		{"unnamed holder, empty box", Driving{Yours: false}, ""},
		{"unnamed holder, a draft", Driving{Yours: false}, draft},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, _, _ := keyboardOf(t, tc.driving)
			if tc.text != "" {
				a.input.setText(tc.text)
			}
			want := chatlist.LiveTypingFrom(tc.driving.Holder)
			if !a.watching() {
				t.Fatalf("a live view without the keyboard did not report watching")
			}
			screen := plain(frame(a))
			if !strings.Contains(screen, want) {
				t.Fatalf("the frame never says %q:\n%s", want, screen)
			}
			if tc.text != "" && strings.Contains(screen, tc.text) {
				t.Fatalf("the draft was drawn beside the line that says the keyboard is elsewhere:\n%s", screen)
			}
			if tc.text != "" && a.input.String() != tc.text {
				t.Fatalf("the draft is %q, want it kept as %q", a.input.String(), tc.text)
			}
		})
	}

	for _, text := range []string{"", draft} {
		a, _, _, _ := keyboardOf(t, Driving{Yours: true})
		if text != "" {
			a.input.setText(text)
		}
		if a.watching() {
			t.Fatalf("a live view that holds the keyboard reported watching (box %q)", text)
		}
		if screen := plain(frame(a)); strings.Contains(screen, "typing from") {
			t.Fatalf("a live view that holds the keyboard still says it does not (box %q):\n%s", text, screen)
		}
	}
}

// R7: enter on a live view that does not hold the keyboard takes it and sends
// nothing, a draft in the box stays put, and the next enter sends it once.
func TestR7EnterOnAWatchingViewTakesTheKeyboardAndTheNextEnterSendsOnce(t *testing.T) {
	const draft = "send me after the take"
	for _, tc := range []struct {
		name    string
		driving Driving
	}{
		{"named holder", Driving{Yours: false, Holder: "Desktop"}},
		{"unnamed holder", Driving{Yours: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, agent, driving, taken := keyboardOf(t, tc.driving)
			a.input.setText(draft)

			drive(t, a, key("enter"))
			if *taken != 1 {
				t.Fatalf("enter on a watching view asked for the keyboard %d times, want once", *taken)
			}
			if len(agent.sent) != 0 {
				t.Fatalf("the enter that took the keyboard also sent %v, want nothing", agent.sent)
			}
			if got := a.input.String(); got != draft {
				t.Fatalf("taking the keyboard changed the draft to %q, want %q", got, draft)
			}
			if !driving.Yours || a.watching() {
				t.Fatalf("the view still reports another window typing after the take: %+v", *driving)
			}

			drive(t, a, key("enter"))
			if len(agent.sent) != 1 || agent.sent[0] != draft {
				t.Fatalf("the enter after the take sent %v, want [%s] once", agent.sent, draft)
			}
			if *taken != 1 {
				t.Fatalf("the sending enter asked for the keyboard again (%d asks in all)", *taken)
			}
		})
	}
}

// R8: with remote control off, which is every conversation that is not a live
// view, nothing changes: the first enter sends, no line says the keyboard is
// elsewhere, and a window that watches a local conversation another window is
// typing into keeps its own wording.
func TestR8WithRemoteControlOffAConversationKeepsItsKeyboardRoad(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.width = 100
	a.dismissWelcome()
	a.takeUp(Conversation{Agent: agent}, true)
	a.input.setText("an ordinary line")
	if a.watching() {
		t.Fatal("a conversation with no live view and no link reported watching")
	}
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || agent.sent[0] != "an ordinary line" {
		t.Fatalf("the first enter on an ordinary conversation sent %v, want [an ordinary line]", agent.sent)
	}

	// A local conversation another window is typing into has no live view, and it
	// keeps the bar and the keys it always had, with no machine named.
	local := &fakeAgent{model: "m"}
	w := newTestApp(local)
	w.width = 100
	w.dismissWelcome()
	taken := 0
	w.takeUp(Conversation{
		Agent: local,
		Link: &LinkSeam{
			Driving: func() Driving { return Driving{Yours: false} },
			Take:    func() error { taken++; return nil },
		},
	}, true)
	if !w.watching() {
		t.Fatal("a local watcher stopped reporting watching")
	}
	screen := plain(frame(w))
	if !strings.Contains(screen, "typing from another window now") || !strings.Contains(screen, watchKeysWord) {
		t.Fatalf("a local watcher lost its own words:\n%s", screen)
	}
	if strings.Contains(screen, chatlist.LiveTypingFrom("")) {
		t.Fatalf("a local watcher was given the live view's sentence:\n%s", screen)
	}
}
