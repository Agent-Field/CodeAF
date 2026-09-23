package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// THE SELECTION IS CELLS, NOT ROWS (#222). A press on one character and a
// release on another takes exactly the characters between them, on one row or
// across several; a double-click takes the word and a triple the row; and a
// wide glyph is never split. These tests drive the real surface with the same
// synthetic mouse the row tests use, and read what landed off the OSC 52 write.

// colOf is the screen column the text `sub` starts on, on the body row that
// carries `line`.
func colOf(t *testing.T, a *app, line, sub string) (int, int) {
	t.Helper()
	y := screenRowWith(t, a, line)
	body, _ := a.bodyRows(a.bodyWidth(), a.viewHeight())
	text := plain(body[y-a.bodyTop()].text)
	at := strings.Index(text, sub)
	if at < 0 {
		t.Fatalf("%q is not on the row %q", sub, text)
	}
	starts, _ := glyphStarts(text)
	return starts[len([]rune(text[:at]))], y
}

func sweep(t *testing.T, a *app, x1, y1, x2, y2 int) string {
	t.Helper()
	drive(t, a, tea.MouseClickMsg{X: x1, Y: y1, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: x2, Y: y2, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: x2, Y: y2, Button: tea.MouseLeft})
	return rawPayload(t, runCmd(cmd))
}

// A press on the "d" of "do" and a release on the "t" of "print" copies
// exactly "do I print" — not the row, and not the prompt glyph before it.
func TestASweepWithinARowCopiesExactlyTheCellsBetweenThePointers(t *testing.T) {
	a := dragApp(t)
	x1, y := colOf(t, a, "how do I print?", "do I print")
	x2 := x1 + len("do I print") - 1
	if got := sweep(t, a, x1, y, x2, y); got != "do I print" {
		t.Fatalf("copied %q, want %q", got, "do I print")
	}
	if word := a.dragWord(); word != "copied · 10 chars" {
		t.Fatalf("the status line says %q", word)
	}
}

// A sweep that starts mid-row and ends mid-row two rows down takes the tail of
// the first, the middle rows whole, and the head of the last, joined by
// newlines — with the middle row's drawn indent lifted as copy mode lifts it.
func TestASweepAcrossRowsTakesTailWholeAndHead(t *testing.T) {
	a := dragApp(t)
	x1, y1 := colOf(t, a, "how do I print?", "I print?")
	x2, y2 := colOf(t, a, "Use fmt.Println.", "fmt")
	x2 += len("fmt") - 1
	got := sweep(t, a, x1, y1, x2, y2)
	if !strings.HasPrefix(got, "I print?\n") || !strings.HasSuffix(got, "\nUse fmt") {
		t.Fatalf("the ends are wrong:\n%q", got)
	}
	// The middle row keeps the indent it is drawn with, as copy mode keeps it:
	// an inset is the block's own and pastes as such (copymode.go's copyClean).
	if !strings.Contains(got, "the person wants fmt\n") {
		t.Fatalf("the middle row is not taken whole:\n%q", got)
	}
	if word := a.dragWord(); !strings.HasSuffix(word, " lines") {
		t.Fatalf("a multi-row copy is counted in lines, got %q", word)
	}
}

// A release BEHIND the anchor is the same selection read the other way.
func TestASweepBackwardsIsTheSameSelection(t *testing.T) {
	a := dragApp(t)
	x1, y := colOf(t, a, "how do I print?", "do I print")
	x2 := x1 + len("do I print") - 1
	if got := sweep(t, a, x2, y, x1, y); got != "do I print" {
		t.Fatalf("copied %q, want %q", got, "do I print")
	}
}

// A sweep that lands inside a wide glyph snaps to the glyph: the second cell of
// 本 selects 本, never half of it, and the highlight covers both its cells.
func TestASweepNeverSplitsAWideGlyph(t *testing.T) {
	a := dragApp(t)
	a.entries = append(a.entries, entry{kind: entryAssistant, settled: true, text: "日本語 end"})
	a.touch()
	x, y := colOf(t, a, "日本語 end", "本")
	// The pointer on the glyph's SECOND cell at both ends — swept out past
	// the slop and back, since a release in place is a click.
	drive(t, a, tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: x + 6, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
	if got := rawPayload(t, runCmd(cmd)); got != "本" {
		t.Fatalf("copied %q, want the whole glyph", got)
	}
	from, to, on := a.dragCells(a.bodyContentRow(y), "日本語 end")
	if !on || from != x || to != x+2 {
		t.Fatalf("the lit cells are %d..%d (on=%v), want %d..%d", from, to, on, x, x+2)
	}
}

// Double-click takes the word under the pointer; the sentence's full stop is
// left behind, the dotted name is kept whole.
func TestADoubleClickTakesTheWordUnderThePointer(t *testing.T) {
	a := dragApp(t)
	x, y := colOf(t, a, "Use fmt.Println.", "Println")
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if got := rawPayload(t, runCmd(cmd)); got != "fmt.Println" {
		t.Fatalf("copied %q, want %q", got, "fmt.Println")
	}
	if word := a.dragWord(); word != "copied · 1 word" {
		t.Fatalf("the status line says %q", word)
	}
}

// Triple-click takes the row — which is what the old sweep did for every row.
func TestATripleClickTakesTheRow(t *testing.T) {
	a := dragApp(t)
	x, y := colOf(t, a, "Use fmt.Println.", "fmt")
	for i := 0; i < 2; i++ {
		drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	}
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if got := rawPayload(t, runCmd(cmd)); got != "Use fmt.Println." {
		t.Fatalf("copied %q, want the row", got)
	}
	if word := a.dragWord(); word != "copied · 1 line" {
		t.Fatalf("the status line says %q", word)
	}
}

// The word under a double-click stops at a bracket and a quote, and a path is
// one word.
func TestWordAtStopsWhereAPersonWouldExpect(t *testing.T) {
	for _, tc := range []struct{ line, at, want string }{
		{"see (internal/standing/store.go:352) now", "standing", "internal/standing/store.go:352"},
		{`run "go test ./..." first`, "test", "test"},
		{"commit 2a7c8d77 landed.", "2a7c", "2a7c8d77"},
		{"the end.", "end", "end"},
		{"read go.mod", "mod", "go.mod"},
	} {
		at := strings.Index(tc.line, tc.at)
		from, to, ok := wordAt(tc.line, at)
		if !ok || tc.line[from:to] != tc.want {
			t.Fatalf("%q at %q: got %q (ok=%v), want %q", tc.line, tc.at, tc.line[from:min(to, len(tc.line))], ok, tc.want)
		}
	}
}

// Two quick clicks on a tool call are open-then-shut, not a word selection: a
// button acts on every click, and only text is taken by a double-click.
func TestADoubleClickOnAButtonRowIsTwoClicks(t *testing.T) {
	a := dragApp(t)
	y := screenRowWith(t, a, "thought for")
	for i := 0; i < 2; i++ {
		drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: 4, Y: y, Button: tea.MouseLeft})
	}
	// It opened the block on the first click (dragApp starts it open, so the
	// first shuts it) and shut it on the second — two toggles, back where it
	// began — rather than spending the second on a word.
	if !a.entries[1].open {
		t.Fatal("the second click selected a word instead of toggling the block again")
	}
	if a.dragCopied != 0 {
		t.Fatal("a double-click on a button copied something")
	}
}
