package footer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The footer's targets. Every test here compares the map against the PICTURE —
// the column a word occupies in the painted row — because that is the only
// comparison that catches the failure the map exists to prevent: a click that
// lands one cell away from the word the reader aimed at.

func hitContext() FocusContext {
	return FocusContext{
		Verbs: []registry.Entry{
			{ID: "key.quit", Verb: "stop or quit", Key: "ctrl+c"},
			{ID: "key.thread.receipts", Verb: "toggle receipts", Key: "ctrl+r"},
		},
		ScopeTail: "‹ untitled room",
	}
}

func hitModel() *Model {
	return New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
}

// Every target names the columns the word it stands for actually occupies.
func TestEveryTargetSitsUnderItsOwnWord(t *testing.T) {
	m := hitModel()
	ctx := hitContext()
	const width = 100
	row := ansi.Strip(m.Render(ctx, width))

	labels := map[string]string{
		"key.quit":            "ctrl+c stop or quit",
		"key.thread.receipts": "ctrl+r toggle receipts",
		HelpTarget:            helpDoorText,
		ScopeTarget:           ctx.ScopeTail,
	}
	targets := m.Targets(ctx, width)
	if len(targets) != len(labels) {
		t.Fatalf("%d targets for %d words: %+v", len(targets), len(labels), targets)
	}
	for _, target := range targets {
		want, ok := labels[target.ID]
		if !ok {
			t.Fatalf("unexpected target %q", target.ID)
		}
		if target.From < 0 || target.To > len(row) || target.From >= target.To {
			t.Fatalf("%s spans %d..%d of a %d-cell row", target.ID, target.From, target.To, len(row))
		}
		if got := cells(row, target.From, target.To); got != want {
			t.Fatalf("%s covers %q, want %q", target.ID, got, want)
		}
	}
}

// TargetAt agrees with Targets at every column, and answers nothing where the
// row says nothing actionable — the attention badge and the health cell are
// statements, not verbs.
func TestTargetAtAnswersOnlyWhereAVerbIs(t *testing.T) {
	m := hitModel()
	ctx := hitContext()
	ctx.Attention = 2
	ctx.Health = []string{"visitor"}
	const width = 110

	targets := m.Targets(ctx, width)
	for x := range ansi.StringWidth(m.Render(ctx, width)) {
		id, ok := m.TargetAt(ctx, width, x)
		want, wantOK := "", false
		for _, target := range targets {
			if target.Contains(x) {
				want, wantOK = target.ID, true
			}
		}
		if ok != wantOK || id != want {
			t.Fatalf("column %d: TargetAt gave (%q,%v), Targets says (%q,%v)", x, id, ok, want, wantOK)
		}
	}
	// The attention badge is at the head of the row and is not a door.
	if id, ok := m.TargetAt(ctx, width, 0); ok {
		t.Fatalf("the attention badge answered as %q", id)
	}
}

// A column the fit dropped has no target. This is the property the shared
// [Model.fit] exists for: a click cannot land on a word the paint left off.
func TestADroppedColumnHasNoTarget(t *testing.T) {
	m := hitModel()
	ctx := hitContext()
	wide := m.Targets(ctx, 100)
	narrow := m.Targets(ctx, 30)
	if len(narrow) >= len(wide) {
		t.Fatalf("narrow kept %d targets and wide had %d", len(narrow), len(wide))
	}
	row := ansi.Strip(m.Render(ctx, 30))
	for _, target := range narrow {
		if target.To > ansi.StringWidth(row) {
			t.Fatalf("%s spans past the %d-cell row it was drawn on", target.ID, ansi.StringWidth(row))
		}
	}
}

// No target ever names a column past the end of the row, at any width. The
// fitting drops whole columns, so a partial target would be a click landing on
// a word that is only half there.
func TestNoTargetOverrunsTheRowAtAnyWidth(t *testing.T) {
	m := hitModel()
	ctx := hitContext()
	ctx.Attention = 1
	for width := 1; width <= 140; width++ {
		row := ansi.StringWidth(ansi.Strip(m.Render(ctx, width)))
		for _, target := range m.Targets(ctx, width) {
			if target.From < 0 || target.To > row || target.To > width {
				t.Fatalf("width %d: %s spans %d..%d of a %d-cell row",
					width, target.ID, target.From, target.To, row)
			}
		}
	}
}

// Hover brightens exactly one run and moves nothing. Width stability is the
// hard part: 5.21 bans anything on this row from dancing, and a hover that
// re-fitted the row would move every cell to its right.
func TestHoverBrightensOneRunAndMovesNothing(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	ctx := hitContext()
	const width = 100

	plain := m.Render(ctx, width)
	for _, id := range []string{"key.quit", "key.thread.receipts", HelpTarget, ScopeTarget} {
		hovered := ctx
		hovered.Hover = id
		painted := m.Render(hovered, width)
		if painted == plain {
			t.Fatalf("hovering %s changed no bytes", id)
		}
		if got, want := ansi.Strip(painted), ansi.Strip(plain); got != want {
			t.Fatalf("hovering %s moved the row:\n got %q\nwant %q", id, got, want)
		}
	}

	// And a hover over nothing is the plain row again, byte for byte.
	none := ctx
	none.Hover = ""
	if m.Render(none, width) != plain {
		t.Fatal("an empty hover repainted the row")
	}
}

// A hover naming a target that is not on the row changes nothing. It happens
// on every resize that drops a column, and a row that repainted for it would
// be spending bytes on a pointer that is no longer over anything.
func TestAHoverOnAMissingTargetIsANoOp(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	ctx := hitContext()
	ghost := ctx
	ghost.Hover = "key.nothing.at.all"
	if m.Render(ghost, 100) != m.Render(ctx, 100) {
		t.Fatal("a hover over a word that is not on the row repainted it")
	}
}

// The help door is the target the footer has been advertising since this
// surface existed. It is checked by name because the pane maps it to the
// registry's help row, and a rename on either side has to fail somewhere.
func TestTheHelpDoorIsATarget(t *testing.T) {
	m := hitModel()
	ctx := hitContext()
	row := ansi.Strip(m.Render(ctx, 100))
	at := ansi.StringWidth(row[:strings.Index(row, helpDoorText)])
	if !strings.Contains(row, helpDoorText) {
		t.Fatalf("the row does not advertise help: %q", row)
	}
	id, ok := m.TargetAt(ctx, 100, at)
	if !ok || id != HelpTarget {
		t.Fatalf("the help words answered (%q,%v)", id, ok)
	}
}

// cells slices a row by printable COLUMN rather than by byte. The two diverge
// the moment a row carries a multi-byte glyph, and every footer does — the
// separator alone is two bytes and one cell — so a test that sliced by byte
// would drift one cell further off with every `·` it passed.
func cells(row string, from, to int) string {
	out := strings.Builder{}
	x := 0
	for _, r := range row {
		w := ansi.StringWidth(string(r))
		if x >= from && x+w <= to {
			out.WriteRune(r)
		}
		x += w
	}
	return out.String()
}
