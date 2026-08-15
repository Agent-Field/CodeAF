package footer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The footer's targets. Every test here compares the map against the PICTURE —
// the columns a word occupies in the painted row — because that is the only
// comparison that catches the failure the map exists to prevent: a click that
// lands one cell away from the word the reader aimed at.

func hitContext() FocusContext {
	return FocusContext{
		Places:        places(),
		EscInterrupts: true,
		Verbs:         []registry.Entry{{ID: "key.thread.receipts", Verb: "toggle receipts", Key: "ctrl+r"}},
		Spend:         1.42,
		HaveSpend:     true,
	}
}

func hitModel() *Model { return New(Options{Styler: plainStyler()}) }

// Every target names the columns the word it stands for actually occupies —
// including the chips, which are two tiers and one button.
func TestEveryTargetSitsUnderItsOwnWord(t *testing.T) {
	m := hitModel()
	ctx := hitContext()
	const width = 120
	row := ansi.Strip(m.Render(ctx, width))

	labels := map[string]string{
		"place:chat":          "chat",
		"place:work":          "work",
		"place:notebook":      "notebook",
		InterruptTarget:       interruptVerb + registry.ChipGap + interruptKey,
		"key.thread.receipts": "toggle receipts" + registry.ChipGap + "ctrl+r",
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
		if target.From < 0 || target.To > ansi.StringWidth(row) || target.From >= target.To {
			t.Fatalf("%s spans %d..%d of a %d-cell row", target.ID, target.From, target.To, ansi.StringWidth(row))
		}
		if got := cells(row, target.From, target.To); got != want {
			t.Fatalf("%s covers %q, want %q", target.ID, got, want)
		}
	}
}

// The breadcrumb is one target over the whole trail: clicking anywhere on it is
// one step out, the same step esc and the rail's ‹ take.
func TestTheWholeTrailIsOneWayOut(t *testing.T) {
	m := hitModel()
	ctx := FocusContext{ScopeTail: tokens.GlyphScopeUp + " untitled room " + tokens.GlyphScopeUp + " angles"}
	const width = 100
	row := ansi.Strip(m.Render(ctx, width))
	targets := m.Targets(ctx, width)
	if len(targets) != 1 || targets[0].ID != ScopeTarget {
		t.Fatalf("the trail is not one target: %+v", targets)
	}
	if got := cells(row, targets[0].From, targets[0].To); got != strings.TrimSpace(row) {
		t.Fatalf("the way out covers %q, want the whole trail %q", got, strings.TrimSpace(row))
	}
}

// TargetAt agrees with Targets at every column, and answers nothing where the
// row says nothing actionable — the standing facts on the right are statements
// about this window, not verbs on it.
func TestTargetAtAnswersOnlyWhereADoorIs(t *testing.T) {
	m := hitModel()
	ctx := hitContext()
	const width = 120

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
	// The right zone is statements, not doors: there is nothing to open on the
	// day's total, and the row does not ask the reader to find out which words
	// are which.
	row := ansi.Strip(m.Render(ctx, width))
	if id, ok := m.TargetAt(ctx, width, strings.Index(row, "$1.42")); ok {
		t.Fatalf("the day total answered as %q", id)
	}
}

// The answer chip is deliberately NOT a door: `answer 1—3` names three acts and
// a click cannot say which.
func TestTheAnswerChipIsNotADoor(t *testing.T) {
	m := hitModel()
	ctx := FocusContext{KeyMode: KeyModeAnswer, KeyModeCount: 3, Attention: 1}
	if targets := m.Targets(ctx, 100); len(targets) != 0 {
		t.Fatalf("the answer chip advertised a click: %+v", targets)
	}
}

// A zone the fit dropped has no targets. This is the property the shared layout
// exists for: a click cannot land on a word the paint left off.
func TestADroppedZoneHasNoTargets(t *testing.T) {
	m := hitModel()
	ctx := hitContext()
	wide := m.Targets(ctx, 120)
	narrow := m.Targets(ctx, 34)
	if len(narrow) >= len(wide) {
		t.Fatalf("narrow kept %d targets and wide had %d", len(narrow), len(wide))
	}
	row := ansi.StringWidth(ansi.Strip(m.Render(ctx, 34)))
	for _, target := range narrow {
		if target.To > row {
			t.Fatalf("%s spans past the %d-cell row it was drawn on", target.ID, row)
		}
	}
}

// No target ever names a column past the end of the row, at any width.
func TestNoTargetOverrunsTheRowAtAnyWidth(t *testing.T) {
	m := hitModel()
	ctx := hitContext()
	for width := 1; width <= 160; width++ {
		row := ansi.StringWidth(ansi.Strip(m.Render(ctx, width)))
		for _, target := range m.Targets(ctx, width) {
			if target.From < 0 || target.To > row || target.To > width {
				t.Fatalf("width %d: %s spans %d..%d of a %d-cell row",
					width, target.ID, target.From, target.To, row)
			}
		}
	}
}

// Hover brightens exactly one run and moves nothing. Width stability is the hard
// part: nothing on this row may dance, and a hover that re-fitted the row would
// move every cell to its right.
func TestHoverBrightensOneRunAndMovesNothing(t *testing.T) {
	m := New(Options{Styler: styler()})
	ctx := hitContext()
	const width = 120

	plain := m.Render(ctx, width)
	for _, id := range []string{"place:work", InterruptTarget, "key.thread.receipts"} {
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
	none := ctx
	none.Hover = ""
	if m.Render(none, width) != plain {
		t.Fatal("an empty hover repainted the row")
	}
}

// A hover naming a target that is not on the row changes nothing. It happens on
// every resize that drops a zone.
func TestAHoverOnAMissingTargetIsANoOp(t *testing.T) {
	m := New(Options{Styler: styler()})
	ctx := hitContext()
	ghost := ctx
	ghost.Hover = "place:nowhere"
	if m.Render(ghost, 120) != m.Render(ctx, 120) {
		t.Fatal("a hover over a word that is not on the row repainted it")
	}
}

// Hovering a chip promotes BOTH halves: the whole chip is one button, so it
// lights as one.
func TestHoverPromotesTheWholeChip(t *testing.T) {
	sty := styler()
	m := New(Options{Styler: sty})
	ctx := hitContext()
	ctx.Hover = InterruptTarget
	painted := m.Render(ctx, 120)
	if want := sty.PaintToken(interruptVerb, tokens.TextPrimary); !strings.Contains(painted, want) {
		t.Fatalf("the hovered verb did not promote: %q", painted)
	}
	if want := sty.PaintToken(registry.ChipGap+interruptKey, tokens.TextSecondary); !strings.Contains(painted, want) {
		t.Fatalf("the hovered key did not promote with its verb: %q", painted)
	}
}

// cells slices a row by printable COLUMN rather than by byte. The two diverge
// the moment a row carries a multi-byte glyph, and every footer does — the
// separator alone is two bytes and one cell.
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
