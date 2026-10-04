package tui3

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/pair"
)

// The pairing surface's tests (pair.go, pair_firstrun.go). The door is a fake
// whose two errands are plain functions, so each test writes the conversation
// the other computer would have and reads what the screen says back.

type fakePairing struct {
	offer func(ctx context.Context, ui pair.OfferUI) (string, error)
	join  func(ctx context.Context, typed string, ui pair.JoinUI) (pair.Joined, error)
}

func (f *fakePairing) Offer(ctx context.Context, ui pair.OfferUI) (string, error) {
	return f.offer(ctx, ui)
}

func (f *fakePairing) Join(ctx context.Context, typed string, ui pair.JoinUI) (pair.Joined, error) {
	return f.join(ctx, typed, ui)
}

// pairRig runs the commands a pairing returns the way the program would: each
// on its own goroutine, its message fed back through Update, and whatever
// command that returns run in turn.
type pairRig struct {
	t  *testing.T
	a  *app
	in chan tea.Msg
}

func newPairRig(t *testing.T, a *app) *pairRig {
	return &pairRig{t: t, a: a, in: make(chan tea.Msg, 32)}
}

func (r *pairRig) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		if msg := cmd(); msg != nil {
			r.in <- msg
		}
	}()
}

// feed is one message through the app, and the command it returns run on.
func (r *pairRig) feed(msg tea.Msg) {
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, cmd := range batch {
			r.run(cmd)
		}
		return
	}
	_, cmd := r.a.Update(msg)
	r.run(cmd)
}

// until feeds what arrives until the condition holds, or fails the test.
func (r *pairRig) until(what string, held func() bool) {
	r.t.Helper()
	deadline := time.After(3 * time.Second)
	for !held() {
		select {
		case msg := <-r.in:
			r.feed(msg)
		case <-deadline:
			r.t.Fatalf("never saw %s:\n%s", what, plain(frame(r.a)))
		}
	}
}

func (r *pairRig) press(names ...string) {
	for _, name := range names {
		r.feed(key(name))
	}
}

// slash types a command and runs what it starts.
func (r *pairRig) slash(line string) { r.run(r.a.slash(line)) }

func pairApp(t *testing.T, door Pairing, width int) *app {
	a := newTestApp(&fakeAgent{model: "test/model"})
	a.pairing = door
	a.width, a.height = width, 30
	// A run left open would leave its wait parked for the rest of the package's
	// tests, which the package's goroutine check refuses.
	t.Cleanup(a.pair.close)
	return a
}

func showsCode(t *testing.T, ui pair.OfferUI) *pair.Code {
	t.Helper()
	code, err := pair.NewCode(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	code = code.WithPlate("42")
	ui.Show(code, pair.ChatLines(code, ""))
	return code
}

func TestPairRowsAndAliasesResolve(t *testing.T) {
	for _, word := range []string{"pair", "sync", "link", "laptop"} {
		if got := canonicalCommand(word); got != "pair" {
			t.Errorf("/%s resolves to %q, want pair", word, got)
		}
	}
	var bare, coded bool
	for _, c := range commands {
		if c.name != "pair" {
			continue
		}
		bare = bare || c.args == ""
		coded = coded || c.args == "<code>"
	}
	if !bare || !coded {
		t.Fatalf("the table wants a bare row and a <code> row, has bare=%t coded=%t", bare, coded)
	}
}

func TestPairWithNoDoorSaysOneSentence(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "test/model"})
	if cmd := a.slash("/pair"); cmd != nil {
		t.Fatal("a connection that cannot pair started something")
	}
	if a.pair.open || !strings.Contains(updateNotes(a), pairUnavailableWord) {
		t.Fatalf("want the one sentence and no panel:\n%s", updateNotes(a))
	}
}

func TestPairShareShowsTheCodeThenAsksThenPairs(t *testing.T) {
	release := make(chan struct{})
	answered := make(chan bool, 1)
	door := &fakePairing{offer: func(ctx context.Context, ui pair.OfferUI) (string, error) {
		showsCode(t, ui)
		<-release
		yes := ui.Ask(ctx, "laptop", "amber cedar moon")
		answered <- yes
		if !yes {
			return "", pair.ErrRefused
		}
		return "laptop", nil
	}}
	a := pairApp(t, door, 80)
	r := newPairRig(t, a)
	r.slash("/pair")
	r.until("the code", func() bool { return a.pair.code != nil })
	shown := plain(frame(a))
	for _, want := range []string{"this shares your chats with the device you pair", "codeaf pair " + a.pair.code.Shown(), "10 min left"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("the panel lacks %q:\n%s", want, shown)
		}
	}
	close(release)
	r.until("the question", func() bool { return a.pair.ask != nil })
	asked := plain(frame(a))
	for _, want := range []string{pair.AskChatsLine("laptop", "amber cedar moon")} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the panel lacks %q:\n%s", want, asked)
		}
	}
	if hint := a.hintWord(); hint != pairAskKeys {
		t.Fatalf("hint = %q, want %q", hint, pairAskKeys)
	}
	r.press("enter", "x", "tab")
	if a.pair.ask == nil {
		t.Fatal("a key that is not y, n or esc answered the question")
	}
	r.press("y")
	r.until("the result", func() bool { return a.pair.done })
	if yes := <-answered; !yes {
		t.Fatal("y was heard as no")
	}
	if got := plain(frame(a)); !strings.Contains(got, pair.PairedChatsLine("laptop")) {
		t.Fatalf("no result line:\n%s", got)
	}
}

func TestPairShareRefusesOnNAndOnEsc(t *testing.T) {
	for _, refusal := range []string{"n", "esc"} {
		t.Run(refusal, func(t *testing.T) {
			door := &fakePairing{offer: func(ctx context.Context, ui pair.OfferUI) (string, error) {
				showsCode(t, ui)
				if ui.Ask(ctx, "laptop", "amber cedar moon") {
					return "laptop", nil
				}
				return "", pair.ErrRefused
			}}
			a := pairApp(t, door, 80)
			r := newPairRig(t, a)
			r.slash("/pair")
			r.until("the question", func() bool { return a.pair.ask != nil })
			r.press(refusal)
			r.until("the ending", func() bool { return a.pair.done })
			if a.pair.ok || !strings.Contains(plain(frame(a)), pair.ErrRefused.Error()) {
				t.Fatalf("a refusal must say so:\n%s", plain(frame(a)))
			}
			if !a.pair.open {
				t.Fatal("esc that answered the question also closed the panel")
			}
		})
	}
}

func TestPairBurnedCodeShowsTheBurnLineAboveTheNextCode(t *testing.T) {
	door := &fakePairing{offer: func(ctx context.Context, ui pair.OfferUI) (string, error) {
		showsCode(t, ui)
		ui.Burned()
		showsCode(t, ui)
		<-ctx.Done()
		return "", ctx.Err()
	}}
	a := pairApp(t, door, 80)
	r := newPairRig(t, a)
	r.slash("/pair")
	r.until("the second code", func() bool { return a.pair.burned && a.pair.code != nil })
	got := plain(frame(a))
	burn, next := strings.Index(got, pair.BurnLine), strings.Index(got, "codeaf pair "+a.pair.code.Shown())
	if burn < 0 || next < 0 || burn > next {
		t.Fatalf("the burn line must sit above the new code:\n%s", got)
	}
}

func TestPairClosingCancelsTheDoorsContext(t *testing.T) {
	stopped := make(chan struct{})
	door := &fakePairing{offer: func(ctx context.Context, ui pair.OfferUI) (string, error) {
		showsCode(t, ui)
		<-ctx.Done()
		close(stopped)
		return "", ctx.Err()
	}}
	a := pairApp(t, door, 80)
	r := newPairRig(t, a)
	r.slash("/pair")
	r.until("the code", func() bool { return a.pair.code != nil })
	r.press("esc")
	if a.pair.open {
		t.Fatal("esc with nothing asked left the panel open")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("closing the panel never cancelled the door")
	}
}

func TestPairAgainWhileACodeShowsReopensTheSamePanel(t *testing.T) {
	var (
		mu     sync.Mutex
		offers int
	)
	door := &fakePairing{offer: func(ctx context.Context, ui pair.OfferUI) (string, error) {
		mu.Lock()
		offers++
		mu.Unlock()
		showsCode(t, ui)
		<-ctx.Done()
		return "", ctx.Err()
	}}
	a := pairApp(t, door, 80)
	r := newPairRig(t, a)
	r.slash("/pair")
	r.until("the code", func() bool { return a.pair.code != nil })
	code := a.pair.code
	a.pair.hide()
	if cmd := a.slash("/pair"); cmd != nil {
		t.Fatal("a second /pair started a second offer")
	}
	mu.Lock()
	defer mu.Unlock()
	if !a.pair.open || a.pair.code != code || offers != 1 {
		t.Fatalf("open=%t same code=%t offers=%d", a.pair.open, a.pair.code == code, offers)
	}
}

func TestPairJoinSaysWaitingThenItsEnding(t *testing.T) {
	cases := []struct {
		name   string
		joined pair.Joined
		err    error
		want   string
	}{
		{"joined", pair.Joined{}, nil, pair.JoinedLine},
		{"already", pair.Joined{Already: true}, nil, pair.ErrAlreadyPaired.Error()},
		{"error", pair.Joined{}, pair.ErrCodeDidNotWork, pair.ErrCodeDidNotWork.Error()},
		{"shape", pair.Joined{}, pair.ErrCodeShape, pair.ErrCodeShape.Error()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			release := make(chan struct{})
			var typed string
			door := &fakePairing{join: func(_ context.Context, code string, ui pair.JoinUI) (pair.Joined, error) {
				typed = code
				ui.Waiting("amber cedar moon")
				<-release
				return c.joined, c.err
			}}
			a := pairApp(t, door, 80)
			r := newPairRig(t, a)
			r.slash("/sync 42-715-302")
			r.until("the waiting line", func() bool { return a.pair.waiting == pair.WaitingChatsLine("amber cedar moon") })
			if got := plain(frame(a)); !strings.Contains(got, pair.WaitingChatsLine("amber cedar moon")) {
				t.Fatalf("no waiting line:\n%s", got)
			}
			close(release)
			r.until("the ending", func() bool { return a.pair.done })
			if got := plain(frame(a)); !strings.Contains(got, c.want) {
				t.Fatalf("want %q:\n%s", c.want, got)
			}
			if typed != "42-715-302" {
				t.Fatalf("the door was given %q", typed)
			}
			r.press("enter")
			if a.pair.open {
				t.Fatal("enter did not close a finished panel")
			}
		})
	}
}

// THE PANEL FITS THE FRAME AT BOTH WIDTHS: no line is wider than the window and
// every sentence is still on screen, wrapped and not cut.
func TestPairPanelFitsEightyAndFortyColumns(t *testing.T) {
	for _, width := range []int{80, 40} {
		door := &fakePairing{offer: func(ctx context.Context, ui pair.OfferUI) (string, error) {
			showsCode(t, ui)
			ui.Ask(ctx, "the laptop in the study", "amber cedar moon")
			return "", pair.ErrRefused
		}}
		a := pairApp(t, door, width)
		r := newPairRig(t, a)
		r.slash("/pair")
		r.until("the question", func() bool { return a.pair.ask != nil })
		shown := plain(frame(a))
		for i, line := range strings.Split(shown, "\n") {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("width %d: line %d is %d wide: %q", width, i, got, line)
			}
		}
		flat := strings.Join(strings.Fields(shown), " ")
		for _, want := range []string{pair.AskChatsLine("the laptop in the study", "amber cedar moon"), "shares your chats"} {
			if !strings.Contains(flat, strings.Join(strings.Fields(want), " ")) {
				t.Fatalf("width %d lacks %q:\n%s", width, want, shown)
			}
		}
		r.press("n")
	}
}

// ── the first-run screen's field ─────────────────────────────────────────────

const fieldLabel = "have a code from another device?"

func TestFirstRunFieldIsThereWithADoorAndAbsentWithout(t *testing.T) {
	a, _, _ := setupApp(t, nil)
	if got := plain(frame(a)); strings.Contains(got, fieldLabel) || strings.Contains(got, setupRelayWord) {
		t.Fatalf("a connection that cannot pair drew the field:\n%s", got)
	}
	a.pairing = &fakePairing{}
	got := plain(frame(a))
	for _, want := range []string{fieldLabel, setupRelayWord} {
		if !strings.Contains(got, want) {
			t.Fatalf("the field lacks %q:\n%s", want, got)
		}
	}
}

func TestFirstRunFieldStealsNoKeystroke(t *testing.T) {
	a, _, _ := setupApp(t, nil)
	a.pairing = &fakePairing{}
	pressSetup(a, key("s"), key("k"), key("1"))
	if a.setup.text != "sk1" || a.setup.codeText != "" || a.setup.onCode {
		t.Fatalf("typing went to key=%q code=%q onCode=%t", a.setup.text, a.setup.codeText, a.setup.onCode)
	}
	pressSetup(a, key("tab"), key("4"), key("2"))
	if a.setup.text != "sk1" || a.setup.codeText != "42" {
		t.Fatalf("tab did not move the keyboard: key=%q code=%q", a.setup.text, a.setup.codeText)
	}
	pressSetup(a, key("esc"))
	if !a.setup.open || a.setup.onCode {
		t.Fatal("esc in the field must hand the keyboard back and not skip setup")
	}
}

func TestFirstRunFieldJoinsThroughTheSameDoor(t *testing.T) {
	var typed string
	door := &fakePairing{join: func(_ context.Context, code string, _ pair.JoinUI) (pair.Joined, error) {
		typed = code
		return pair.Joined{}, nil
	}}
	a, _, _ := setupApp(t, nil)
	a.pairing = door
	t.Cleanup(a.pair.close)
	r := newPairRig(t, a)
	r.press("tab")
	for _, c := range "42-715-302" {
		r.press(string(c))
	}
	r.press("enter")
	r.until("the join to end", func() bool { return a.pair.done })
	got := plain(frame(a))
	if !strings.Contains(got, pair.JoinedLine) || typed != "42-715-302" {
		t.Fatalf("typed=%q\n%s", typed, got)
	}
	if !a.setup.open || a.pair.open {
		t.Fatal("the join must report on the setup screen and leave the flow where it was")
	}
}

func TestFirstRunFieldShowsAJoinFailureAndEmptyEnterChangesNothing(t *testing.T) {
	door := &fakePairing{join: func(context.Context, string, pair.JoinUI) (pair.Joined, error) {
		return pair.Joined{}, pair.ErrCodeDidNotWork
	}}
	a, _, _ := setupApp(t, nil)
	a.pairing = door
	t.Cleanup(a.pair.close)
	r := newPairRig(t, a)
	r.press("tab", "enter")
	if a.setup.onCode || a.pair.run != nil {
		t.Fatal("enter on an empty field started something")
	}
	r.press("tab", "9", "9", "9", "9", "9", "9", "9", "enter")
	r.until("the join to end", func() bool { return a.pair.done })
	if got := plain(frame(a)); !strings.Contains(got, pair.ErrCodeDidNotWork.Error()) {
		t.Fatalf("no failure sentence:\n%s", got)
	}
}
