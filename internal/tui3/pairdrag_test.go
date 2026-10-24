package tui3

// The pairing panel takes the copy gestures (pairdrag.go): a drag over the
// code selects and copies it, a `c` copies the code on its own, and both say
// "copied · N chars" the way every copy on this surface does. Each test here
// failed before the panel's sweep existed — a press over the panel was
// swallowed and answered nothing.

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/pair"
)

// pairCodeScreen finds the screen cell the code is drawn at: the overlay line
// carrying it, and the column it starts on.
func pairCodeScreen(t *testing.T, a *app, code *pair.Code) (x, y int) {
	t.Helper()
	width, _ := a.size()
	shown := code.Shown()
	rows := a.overlayRows(width, a.overlayHeight())
	for i, line := range rows {
		plain := ansi.Strip(line)
		if at := strings.Index(plain, shown); at >= 0 {
			for yy := 0; yy < a.height; yy++ {
				if mark, ok := a.chromeAt(yy); ok && mark.kind == chromeOverlay && mark.index == i {
					return at, yy
				}
			}
		}
	}
	t.Fatalf("the code %q is not on the panel:\n%s", shown, plain(frame(a)))
	return 0, 0
}

func TestCInThePairPanelCopiesTheCode(t *testing.T) {
	door := &fakePairing{offer: func(ctx context.Context, ui pair.OfferUI) (string, error) {
		showsCode(t, ui)
		return "", pair.ErrRefused
	}}
	a := pairApp(t, door, 80)
	r := newPairRig(t, a)
	r.slash("/pair")
	r.until("the code", func() bool { return a.pair.code != nil })
	want := a.pair.code.Shown()
	r.press("c")
	if got := a.dragWord(); got != "copied · "+itoa(utf8.RuneCountInString(want))+" chars" {
		t.Fatalf("the status line says %q after c", got)
	}
	if got := strings.Join(strings.Fields(plain(frame(a))), " "); !strings.Contains(got, "c copy code") {
		t.Fatalf("the hint does not offer the key:\n%s", got)
	}
}

func TestADragOverThePairCodeCopiesIt(t *testing.T) {
	door := &fakePairing{offer: func(ctx context.Context, ui pair.OfferUI) (string, error) {
		showsCode(t, ui)
		return "", pair.ErrRefused
	}}
	a := pairApp(t, door, 80)
	r := newPairRig(t, a)
	r.slash("/pair")
	r.until("the code", func() bool { return a.pair.code != nil })
	want := a.pair.code.Shown()
	x, y := pairCodeScreen(t, a, a.pair.code)
	end := x + utf8.RuneCountInString(want) - 1

	r.feed(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	r.feed(tea.MouseMotionMsg{X: end, Y: y, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: end, Y: y, Button: tea.MouseLeft})
	copied := rawPayload(t, runCmd(cmd))
	if copied != want {
		t.Fatalf("the sweep copied %q, want %q", copied, want)
	}
	if got := a.dragWord(); got != "copied · "+itoa(utf8.RuneCountInString(want))+" chars" {
		t.Fatalf("the status line says %q after a sweep", got)
	}
}

// THE REPLACE IS TYPED, ASKED, AND ONLY THEN RUN. A no or an escape leaves
// everything untouched: the door's replacing errand is never called.

func replaceApp(t *testing.T, hasChats bool) (*app, *pairRig, *int) {
	var joins int
	door := &fakePairing{
		hasChats: hasChats,
		replace: func(ctx context.Context, code string, ui pair.JoinUI) (pair.Joined, error) {
			joins++
			return pair.Joined{}, nil
		},
	}
	a := pairApp(t, door, 80)
	return a, newPairRig(t, a), &joins
}

func TestPairReplaceAsksAndOnlyYesJoins(t *testing.T) {
	a, r, joins := replaceApp(t, true)
	r.slash("/pair 71-869-571 --replace")
	r.until("the question", func() bool { return a.pair.ask != nil })
	shown := strings.Join(strings.Fields(plain(frame(a))), " ")
	for _, want := range []string{"Replace them?", "cannot be read afterwards", "gone for good"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("no replace confirmation (lacks %q):\n%s", want, shown)
		}
	}
	r.press("n")
	r.until("the decline", func() bool { return a.pair.done })
	if got := plain(frame(a)); !strings.Contains(got, replaceDeclinedLine) {
		t.Fatalf("no decline line:\n%s", got)
	}
	if *joins != 0 {
		t.Fatal("a no still called the replacing errand")
	}
	// AND ESCAPE IS A NO TOO: the panel closed with nothing touched.
	r.press("enter")
	a2, r2, joins2 := replaceApp(t, true)
	r2.slash("/pair 71-869-571 --replace")
	r2.until("the question", func() bool { return a2.pair.ask != nil })
	r2.press("esc")
	if *joins2 != 0 {
		t.Fatal("escape called the replacing errand")
	}
}

func TestPairReplaceYesJoins(t *testing.T) {
	a, r, joins := replaceApp(t, true)
	r.slash("/pair 71-869-571 --replace")
	r.until("the question", func() bool { return a.pair.ask != nil })
	r.press("y")
	r.until("the ending", func() bool { return a.pair.done })
	if *joins != 1 {
		t.Fatalf("yes ran the replacing errand %d times", *joins)
	}
}

// A HOME WITH NOTHING TO LOSE IS JOINED WITHOUT THE QUESTION.
func TestPairReplaceWithoutOwnChatsSkipsTheAsk(t *testing.T) {
	a, r, joins := replaceApp(t, false)
	r.slash("/pair 71-869-571 --replace")
	r.until("the ending", func() bool { return a.pair.done })
	if *joins != 1 {
		t.Fatalf("the replacing errand ran %d times", *joins)
	}
	if a.pair.ask != nil {
		t.Fatal("a home with no own chats was asked anyway")
	}
}
