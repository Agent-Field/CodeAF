package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A SESSION KEEPS WORKING ON A WINDOW NOBODY IS LOOKING AT.
//
// The report this file is written from: several aforge windows open on one
// machine, and whichever one the person walked away from seemed to stop. It
// was not the picture and it was not the engine — the engine's turn runs in
// its own goroutine behind an unbounded queue (session's agent.go), and the
// frame clock has never read focus at all. It was the approval countdown:
// ten seconds after the gate stopped a call, [app.tickAsk] denied it on behalf
// of somebody who was two windows away and could not have known the question
// existed. Every call that turn tried to make came back refused, so the session
// really had stopped working, and the reason was on a screen behind them.
//
// So the clock is a clock about READING, and it only runs where there is
// somebody to read.

// awayAsk is one window with an approval question up and a countdown on it,
// plus the door that moves its clock.
func awayAsk(t *testing.T) (*wiredAgent, *app, func(time.Duration)) {
	t.Helper()
	agent, a := wired([]session.Event{
		toolBegin("bash", "bash go test ./..."),
		consentEvent(7, "bash", "bash go test ./...", `bash pattern "go test *"`),
	})
	at := time.Now()
	a.clock = func() time.Time { return at }
	a.title = "porting the parser"
	typeLine(t, a, "run the tests")
	if !a.asking() {
		t.Fatal("the question never came up")
	}
	// The countdown is the setting's default, stamped here because a test app
	// never ran the boot that reads it (app.go's [app.consentWait]).
	a.askWait = 10 * time.Second
	a.askAt, a.askPaused = at, false
	return agent, a, func(d time.Duration) { at = at.Add(d) }
}

// THE WHOLE BUG, AND THE WHOLE FIX. A blurred window holds the countdown for as
// long as it is blurred, and the person who comes back finds the question they
// left rather than a call somebody else refused for them.
func TestAnUnfocusedWindowDoesNotAnswerTheQuestionForYou(t *testing.T) {
	agent, a, advance := awayAsk(t)

	drive(t, a, tea.BlurMsg{})
	// Long past the ten seconds, and past any patience a person could be said
	// to have run out of: the clock is not running at all.
	advance(11 * time.Second)
	drive(t, a, frameMsg{})
	if len(agent.answers) != 0 {
		t.Fatalf("an unfocused window answered for the person: %+v", agent.answers)
	}
	advance(time.Hour)
	drive(t, a, frameMsg{})
	if !a.asking() || len(agent.answers) != 0 {
		t.Fatalf("an hour on a blurred window answered the question: asking=%v answers=%+v",
			a.asking(), agent.answers)
	}

	// AND THE COUNTDOWN STARTS AGAIN WHOLE when the keyboard comes back: an
	// hour of not being looked at is not an hour of deciding, so the first
	// frame back must not be the frame that expires.
	drive(t, a, tea.FocusMsg{})
	advance(9 * time.Second)
	drive(t, a, frameMsg{})
	if len(agent.answers) != 0 {
		t.Fatalf("the countdown did not restart when focus came back: %+v", agent.answers)
	}
	advance(2 * time.Second)
	drive(t, a, frameMsg{})
	if len(agent.answers) != 1 || agent.answers[0] != (answered{id: 7, allow: false, scope: session.ConsentOnce}) {
		t.Fatalf("a question in front of a person never expired: %+v", agent.answers)
	}
}

// The expiry itself is untouched where there IS somebody to read it. This is
// the guard against fixing the countdown by removing it: a window with the
// keyboard still denies at ten seconds, and the row still says who said so.
func TestAFocusedWindowStillExpiresTheQuestion(t *testing.T) {
	agent, a, advance := awayAsk(t)

	advance(11 * time.Second)
	drive(t, a, frameMsg{})
	if len(agent.answers) != 1 || agent.answers[0].allow {
		t.Fatalf("the countdown stopped denying in front of a person: %+v", agent.answers)
	}
	if got := plain(frame(a)); !strings.Contains(got, consentExpiredWord) {
		t.Fatalf("the row does not say the clock answered:\n%s", got)
	}
}

// A PERSON WHO TOUCHED THE KEYS IS STILL DECIDING, and alt-tabbing away and
// back is not them changing their mind. [app.pauseAsk] is a one-way door and
// [app.refocusAsk] must not be the way back through it.
func TestARefocusDoesNotRestartAPausedQuestion(t *testing.T) {
	agent, a, advance := awayAsk(t)

	// A key that is not an answer pauses the clock for good (consent.go).
	drive(t, a, key("x"))
	if !a.askPaused {
		t.Fatal("a key did not pause the countdown")
	}
	drive(t, a, tea.BlurMsg{}, tea.FocusMsg{})
	if !a.askPaused {
		t.Fatal("a trip to another window un-paused a question somebody was answering")
	}
	advance(time.Hour)
	drive(t, a, frameMsg{})
	if len(agent.answers) != 0 {
		t.Fatalf("a paused question expired anyway: %+v", agent.answers)
	}
}

// AND THE WINDOW SAYS SO OUT LOUD. A question that now waits indefinitely has
// to be a question the person was told about, so it sends the same OSC 777 a
// finished turn sends — and says nothing at all to somebody who is looking
// straight at it.
func TestAQuestionOnAnUnfocusedWindowNotifiesTheDesktop(t *testing.T) {
	_, a, _ := awayAsk(t)

	if cmd := a.notifyAsk(); cmd != nil {
		t.Fatal("a focused terminal was notified about a question on its own screen")
	}

	drive(t, a, tea.BlurMsg{})
	cmd := a.notifyAsk()
	if cmd == nil {
		t.Fatal("an unfocused terminal was not told its session is waiting")
	}
	raw, ok := cmd().(tea.RawMsg)
	if !ok {
		t.Fatalf("the notification is a %T, not a raw write", cmd())
	}
	seq, _ := raw.Msg.(string)
	if !strings.HasPrefix(seq, "\x1b]777;notify;"+product+";") {
		t.Fatalf("the sequence is not an OSC 777 notify: %q", seq)
	}
	// It names the conversation, because a banner's whole job is to say WHICH
	// terminal to go back to, and it uses the presence file's own word for the
	// state so the desktop and another window's home page agree.
	if !strings.Contains(seq, "porting the parser · "+notifyAskWord) {
		t.Fatalf("the banner does not name the waiting conversation: %q", seq)
	}
}

// AND THE QUESTION ARRIVING IS WHAT SENDS IT. The banner is wired to the gate's
// own event rather than to a clock, so it goes out on the frame the session
// stops on.
func TestTheRaisedQuestionCarriesTheBanner(t *testing.T) {
	agent, a := wired([]session.Event{})
	a.title = "porting the parser"
	drive(t, a, tea.BlurMsg{})

	cmd := a.event(consentEvent(7, "bash", "bash go test ./...", `bash pattern "go test *"`))
	if !a.asking() {
		t.Fatal("the question never came up")
	}
	if len(agent.answers) != 0 {
		t.Fatalf("the question was answered on arrival: %+v", agent.answers)
	}
	var sent string
	for _, msg := range runCmd(cmd) {
		if raw, ok := msg.(tea.RawMsg); ok {
			sent, _ = raw.Msg.(string)
		}
	}
	if !strings.Contains(sent, notifyAskWord) {
		t.Fatalf("a question raised on a blurred window sent no banner: %q", sent)
	}
}
