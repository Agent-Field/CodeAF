package rail

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The pointer table. Every test here asks the same question the shell asks —
// "what is at this cell?" — and checks the answer against the PICTURE, never
// against a second computation of the layout. A test that recomputed the row
// heights would pass at exactly the moments the table was wrong.

// The table has to have one entry per line drawn, at every width, or a
// coordinate past the last row reads a row that is not there.
func TestTheHitTableIsExactlyAsLongAsTheFrame(t *testing.T) {
	m := New(scene())
	v := plainView()
	for width := 8; width <= 110; width += 7 {
		for _, height := range []int{1, 3, 8, 24} {
			lines := v.Render(m, ModeRail, width, height)
			if v.Lines() != len(lines) {
				t.Fatalf("%dx%d: Lines()=%d, frame=%d", width, height, v.Lines(), len(lines))
			}
			if got := len(v.marks); got != len(lines) {
				t.Fatalf("%dx%d: %d marks for %d lines", width, height, got, len(lines))
			}
			if _, ok := v.RowAt(len(lines)); ok {
				t.Fatalf("%dx%d: a coordinate past the frame named a row", width, height)
			}
		}
	}
}

// The row a cell names is the row whose NAME is on that line. This is the
// property that makes a click land where the finger points.
func TestEveryRowNameSitsOnItsOwnRow(t *testing.T) {
	m := New(scene())
	v := plainView()
	lines := copyOf(v.Rail(m, 40, 40))
	rows := m.Scope().Rows
	seen := 0
	for y, line := range lines {
		index, ok := v.RowAt(y)
		if !ok {
			continue
		}
		if index < 0 || index >= len(rows) {
			t.Fatalf("line %d named row %d of %d", y, index, len(rows))
		}
		if strings.Contains(line, rows[index].Name) {
			seen++
			continue
		}
		// A continuation line (status, meta, telemetry) belongs to the row above
		// it and must name the same row.
		if before, _ := v.RowAt(y - 1); before != index {
			t.Fatalf("line %d (%q) named row %d but the line above named %d",
				y, line, index, before)
		}
	}
	if seen < len(rows) {
		t.Fatalf("only %d of %d rows had their name on a line the table claims", seen, len(rows))
	}
}

// Chrome is not a row. The hairline between the surface and the members is the
// case that matters: a click there must do nothing, not select row 0.
func TestChromeLinesNameNoRow(t *testing.T) {
	m := New(scene())
	v := plainView()
	lines := copyOf(v.Rail(m, 40, 40))
	found := false
	for y, line := range lines {
		if strings.TrimSpace(line) == "" || !strings.Contains(line, "───") {
			continue
		}
		found = true
		if _, ok := v.RowAt(y); ok {
			t.Fatalf("the hairline at line %d named a row", y)
		}
	}
	if !found {
		t.Fatal("no hairline in the frame; the test asserted nothing")
	}
}

// A rail with room to spare must not let the empty space below it stand in for
// the last row.
func TestEmptySpaceBelowTheMapNamesNothing(t *testing.T) {
	m := New(scene())
	v := plainView()
	lines := copyOf(v.Rail(m, 40, 40))
	if _, ok := v.RowAt(len(lines) + 3); ok {
		t.Fatal("a coordinate below the map named a row")
	}
}

// 5.15: the scope header is the breadcrumb tail and is clickable to go up. It
// is the WHOLE line where the header has one of its own.
func TestTheScopeHeaderIsTheWayOutAcrossItsWholeLine(t *testing.T) {
	m := entered(t)
	v := plainView()
	lines := copyOf(v.Rail(m, 28, 24))
	if !strings.Contains(lines[0], tokens.GlyphScopeUp) {
		t.Fatalf("the first line is not the scope header: %q", lines[0])
	}
	for x := 0; x < 28; x++ {
		if !v.ScopeUpAt(x, 0) {
			t.Fatalf("column %d of the scope header was not the way out", x)
		}
	}
	if v.ScopeUpAt(0, 1) {
		t.Fatal("the surface row under the header was read as the way out")
	}
}

// Where the header has been merged into row 0 (renderMap), only the ‹ pops.
// The rest of that line is the room's own name, and a click that left the room
// from there would fire whenever the reader aimed at where they are.
func TestTheMergedLeadPopsAndTheRestOfTheRowDoesNot(t *testing.T) {
	m := enteredAtomic(t)
	v := plainView()
	lines := copyOf(v.Rail(m, 40, 24))
	at, ok := columnOf(lines[0], tokens.GlyphScopeUp)
	if !ok {
		t.Fatalf("the merged row lost its way out: %q", lines[0])
	}
	if !v.ScopeUpAt(at, 0) {
		t.Fatalf("the ‹ at column %d was not the way out", at)
	}
	if row, ok := v.RowAt(0); !ok || row != 0 {
		t.Fatalf("the merged line named row %d (ok=%v), want row 0", row, ok)
	}
	// The room's own name, several cells to the right, stays row 0.
	name, ok := columnOf(lines[0], "Permanent")
	if !ok {
		t.Fatalf("the merged row lost the room name: %q", lines[0])
	}
	if v.ScopeUpAt(name, 0) {
		t.Fatal("clicking the room's own name would have left the room")
	}
}

// A frame at home has no way out, and must not claim one.
func TestHomeHasNoWayOut(t *testing.T) {
	v := plainView()
	v.Rail(New(scene()), 40, 24)
	for y := 0; y < 24; y++ {
		for x := 0; x < 40; x += 7 {
			if v.ScopeUpAt(x, y) {
				t.Fatalf("home offered a way out at %d,%d", x, y)
			}
		}
	}
}

// Hover is a preview and nothing else: it changes bytes and changes no state
// the keyboard can see.
func TestHoverLightsARowAndMovesNoCursor(t *testing.T) {
	m := New(scene())
	v := NewView(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
	before := copyOf(v.Rail(m, 40, 24))
	cursor := m.Cursor()

	row, ok := v.RowAt(len(before) - 1)
	if !ok || row == cursor {
		t.Fatalf("no unselected row to hover (row=%d ok=%v cursor=%d)", row, ok, cursor)
	}
	if !v.SetHover(int32(row)) {
		t.Fatal("SetHover reported no change on a fresh hover")
	}
	after := copyOf(v.Rail(m, 40, 24))
	if m.Cursor() != cursor {
		t.Fatalf("hovering moved the cursor to %d", m.Cursor())
	}
	changed := 0
	for i := range before {
		if before[i] != after[i] {
			changed++
		}
	}
	if changed == 0 {
		t.Fatal("hovering changed no bytes; the affordance is invisible")
	}
	// And it changed only the hovered row's lines.
	for i := range before {
		at, _ := v.RowAt(i)
		if before[i] != after[i] && at != row {
			t.Fatalf("hovering row %d repainted line %d, which belongs to row %d", row, i, at)
		}
	}
}

// The bandwidth half of the all-motion decision: a pointer that stays inside
// one target costs nothing at all.
func TestHoveringTheSameRowTwiceCostsNothing(t *testing.T) {
	m := New(scene())
	v := plainView()
	v.Rail(m, 40, 24)
	if !v.SetHover(1) {
		t.Fatal("the first hover reported no change")
	}
	if v.SetHover(1) {
		t.Fatal("re-hovering the same row reported a change")
	}
	if !v.SetHover(markNoHover) {
		t.Fatal("clearing the hover reported no change")
	}
	if v.SetHover(markNoHover) {
		t.Fatal("clearing an already-clear hover reported a change")
	}
}

// The selected row is never also drawn as hovered: one target, one statement
// (5.16 gives selection the background; hover only ever has the foreground).
func TestTheSelectedRowIsNeverDrawnAsHovered(t *testing.T) {
	m := New(scene())
	m.Select(1)
	v := NewView(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
	plain := copyOf(v.Rail(m, 40, 24))
	v.SetHover(1)
	hovered := copyOf(v.Rail(m, 40, 24))
	assertLines(t, hovered, plain)
}

// HoverAt is the whole resolution rule in one call, and it must agree with the
// two lookups it is built from.
func TestHoverAtAgreesWithTheTable(t *testing.T) {
	m := enteredAtomic(t)
	v := plainView()
	lines := copyOf(v.Rail(m, 40, 24))
	at, _ := columnOf(lines[0], tokens.GlyphScopeUp)
	if got := v.HoverAt(at, 0); got != markScopeUp {
		t.Fatalf("the ‹ resolved to %d, want the way out", got)
	}
	if got := v.HoverAt(30, 0); got != 0 {
		t.Fatalf("the merged row's name resolved to %d, want row 0", got)
	}
	if got := v.HoverAt(0, len(lines)+2); got != markNoHover {
		t.Fatalf("a cell below the map resolved to %d, want nothing", got)
	}
}

// The Pane forwards, including the "the pointer left" case nothing inside the
// rail could observe on its own.
func TestThePaneForwardsTheWholePointerVocabulary(t *testing.T) {
	m := New(scene())
	p := Pane{Model: m, View: plainView(), Mode: ModeRail}
	p.Render(40, 24)

	if _, ok := p.RowAt(0); !ok {
		t.Fatal("the pane found no row on its own first line")
	}
	if !p.Hover(2, 0, true) {
		t.Fatal("the pane reported no change on a first hover")
	}
	if !p.Hover(2, 0, false) {
		t.Fatal("the pane reported no change when the pointer left")
	}
	if p.View.HoverRow() != markNoHover {
		t.Fatalf("the pointer left and %d stayed lit", p.View.HoverRow())
	}

	var nilPane *Pane
	if _, ok := nilPane.RowAt(0); ok {
		t.Fatal("a nil pane found a row")
	}
	if nilPane.ScopeUpAt(0, 0) || nilPane.Hover(0, 0, true) {
		t.Fatal("a nil pane answered a pointer")
	}
}

// The HUD is a summary, not a map: it has no cursor, so it must offer no
// targets either (8.2.8).
func TestTheHUDOffersNoTargets(t *testing.T) {
	v := plainView()
	lines := v.HUD(New(scene()), 60, 8)
	if len(lines) == 0 {
		t.Fatal("the HUD drew nothing; the test asserted nothing")
	}
	for y := range lines {
		if _, ok := v.RowAt(y); ok {
			t.Fatalf("HUD line %d named a row", y)
		}
		if v.ScopeUpAt(0, y) {
			t.Fatalf("HUD line %d claimed to be a way out", y)
		}
	}
}

// columnOf converts a substring's byte offset into the printable COLUMN a
// pointer would have to be at. The two differ the moment a line carries a
// multi-byte glyph, which every rail line does, and conflating them is how a
// hit test passes on ASCII and fails on the shipped frame.
func columnOf(line, want string) (int, bool) {
	at := strings.Index(line, want)
	if at < 0 {
		return 0, false
	}
	return blocks.Width(line[:at]), true
}
