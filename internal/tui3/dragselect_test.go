package tui3

// SWEEP TO COPY, CLICK ON RELEASE. dragselect.go's two halves of one bargain:
// mouse reporting takes the terminal's own drag-select away, so the drag is
// answered in kind — sweep and the rows are copied — and the price is that the
// body's click moved from press to release, which is where every GUI has fired
// a click since there were GUIs.

import (
	"encoding/base64"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// dragApp is a small conversation with a thinking block in it — the block a
// sweep must never collapse, because it is the one people copy from most.
func dragApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	a.width, a.height = 80, 30
	a.entries = append(a.entries,
		entry{kind: entryUser, text: "how do I print?"},
		entry{kind: entryThinking, text: "the person wants fmt", open: true, settled: true},
		entry{kind: entryAssistant, settled: true, text: "Use fmt.Println."},
	)
	a.touch()
	return a
}

// screenRowWith is the screen row a body row carrying the text is drawn on.
func screenRowWith(t *testing.T, a *app, text string) int {
	t.Helper()
	body, _ := a.bodyRows(a.bodyWidth(), a.viewHeight())
	for i, r := range body {
		if strings.Contains(plain(r.text), text) {
			return a.bodyTop() + i
		}
	}
	t.Fatalf("no body row carries %q", text)
	return -1
}

// rawPayload digs the OSC 52 write out of whatever the release commanded and
// hands back the decoded clipboard text.
func rawPayload(t *testing.T, msgs []tea.Msg) string {
	t.Helper()
	for _, msg := range msgs {
		raw, ok := msg.(tea.RawMsg)
		if !ok {
			continue
		}
		seq, _ := raw.Msg.(string)
		body := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]52;c;"), "\a")
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			t.Fatalf("the payload is not base64: %q", seq)
		}
		return string(decoded)
	}
	t.Fatal("nothing the release commanded writes a clipboard")
	return ""
}

// A sweep from the question to the answer copies every row between them —
// thinking included, un-collapsed — and clicks nothing on the way.
func TestASweepOverTheBodyCopiesItsRowsAndClicksNothing(t *testing.T) {
	a := dragApp(t)
	from := screenRowWith(t, a, "how do I print?")
	to := screenRowWith(t, a, "Use fmt.Println.")

	drive(t, a, tea.MouseClickMsg{X: 4, Y: from, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: to, Button: tea.MouseLeft})
	model, cmd := a.Update(tea.MouseReleaseMsg{X: 4, Y: to, Button: tea.MouseLeft})
	a = model.(*app)

	copied := rawPayload(t, runCmd(cmd))
	for _, want := range []string{"how do I print?", "the person wants fmt", "Use fmt.Println."} {
		if !strings.Contains(copied, want) {
			t.Fatalf("the sweep missed %q; it copied:\n%s", want, copied)
		}
	}
	// The thinking block the sweep began over is exactly as it was: a press
	// that collapsed it would have shuffled the rows mid-sweep and copied text
	// nobody selected.
	if !a.entries[1].open {
		t.Fatal("the sweep collapsed the thinking block it crossed")
	}
	if word := a.dragWord(); !strings.Contains(word, "copied · ") {
		t.Fatalf("the status line says %q after a sweep", word)
	}
}

// Press and release in place is a click, and it lands where it always did: on
// the thinking block, toggling it.
func TestAPressAndReleaseInPlaceIsStillAClick(t *testing.T) {
	a := dragApp(t)
	y := screenRowWith(t, a, "the person wants fmt")

	drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	if !a.entries[1].open {
		t.Fatal("the press alone acted: the click no longer waits for the release")
	}
	drive(t, a, tea.MouseReleaseMsg{X: 4, Y: y, Button: tea.MouseLeft})
	if a.entries[1].open {
		t.Fatal("press and release in place did not click the thinking block")
	}
}

// A press that wanders inside the slop is still a click; one that changes rows
// is a sweep however small.
func TestTheSlopSeparatesAJitteryClickFromASweep(t *testing.T) {
	a := dragApp(t)
	y := screenRowWith(t, a, "the person wants fmt")

	drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: 5, Y: y, Button: tea.MouseLeft})
	if a.drag.on {
		t.Fatal("a two-cell wobble became a selection")
	}
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: y + 1, Button: tea.MouseLeft})
	if !a.drag.on {
		t.Fatal("a row change did not become a selection")
	}
	drive(t, a, tea.MouseReleaseMsg{X: 4, Y: y + 1, Button: tea.MouseLeft})
	if a.entries[1].open != true {
		t.Fatal("a sweep clicked the block it started on")
	}
}

// The swept rows wear the hover background while the button is down, so what
// is highlighted is exactly what the release will copy.
func TestTheSweptRowsWearTheSelectionWhileTheButtonIsDown(t *testing.T) {
	a := dragApp(t)
	from := screenRowWith(t, a, "how do I print?")
	to := screenRowWith(t, a, "Use fmt.Println.")

	drive(t, a, tea.MouseClickMsg{X: 4, Y: from, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: to, Button: tea.MouseLeft})
	low, high, on := a.dragSpan()
	if !on || low != from || high != to {
		t.Fatalf("the selection spans %d..%d (on=%v), want %d..%d", low, high, on, from, to)
	}
	drive(t, a, tea.MouseReleaseMsg{X: 4, Y: to, Button: tea.MouseLeft})
	if _, _, on := a.dragSpan(); on {
		t.Fatal("the selection survived its own release")
	}
}