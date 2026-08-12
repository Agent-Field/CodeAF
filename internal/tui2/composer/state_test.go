package composer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The hug's two-cell gutter: the state edge at column 0 and the prompt beside
// it, sharing one colour (§7's state-reactive prompt, §12's affordance law read
// at one cell).
//
// Every test here asserts the FRAME — the exact cells at the head of the row a
// reader looks at — rather than the enum that produced them, because the whole
// point of the channel is that the two cells say the same thing without being
// read, and only a frame can show that.

// gutter is the painted head of a rendered row: its first two cells, with the
// paint kept, so a test can compare colour as well as shape.
func gutter(out string) string { return strings.SplitN(out, "\n", 2)[0] }

// padded prefixes a row's expected head with §19's inner pad at this width. The
// chrome rows (candidates, chips) carry no edge of their own, so the pad is all
// that stands in front of them.
func padded(width int, head string) string {
	return strings.Repeat(" ", padAt(width)) + head
}

// edged is a DRAFT row's expected head: the field's own edge at column 0, the
// pad inside it, then whatever marker the row carries. Every assertion about
// the head of a composer row goes through this rather than concatenating the
// edge and a glyph, because the cell between them is exactly the defect §19
// closed — a test that spelled them adjacent would pin the bug back in place.
func edged(width int, glyph string) string {
	return tokens.GlyphHugEdge + strings.Repeat(" ", padAt(width)) + glyph
}

// stateModel is a focused composer with a draft in it, so the ghost line is not
// what is being measured.
func stateModel(t *testing.T, sty *tokens.Styler) *Model {
	t.Helper()
	m := New(Options{Styler: sty})
	m.Focus(true)
	typeString(m, "a sentence")
	return m
}

// TestTheEdgeAndThePromptShareOneStateColour walks every state the approved
// spec names and asserts BOTH cells, in one comparison each: the edge glyph and
// the state's own glyph, painted with the state's own token.
//
// A test that checked the glyph alone would pass on a frame where the mark
// changed and the colour did not, which is the half-signal §12 forbids — the
// edge is one cell wide and unlabelled, so colour is the only thing it has.
func TestTheEdgeAndThePromptShareOneStateColour(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	for _, tc := range []struct {
		name  string
		set   func(*Model)
		glyph string
		tok   tokens.Token
	}{
		{"idle", func(m *Model) { m.SetState(StateIdle) }, tokens.GlyphPromptChat, tokens.Cyan},
		{"question", func(m *Model) { m.SetState(StateQuestion) }, tokens.GlyphNeedsHuman, tokens.Amber},
		{"failed", func(m *Model) { m.SetState(StateFailed) }, tokens.GlyphFailed, tokens.Coral},
		{"streaming", func(m *Model) { m.Streaming(true, 3) }, tokens.Spinner(3), tokens.Cyan},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := stateModel(t, sty)
			tc.set(m)
			got := gutter(m.Render(40, 1))
			// The whole gutter is ONE painted span — edge, glyph and the space
			// after them — so the two cells cannot be given different colours by
			// a later edit without this comparison failing.
			want := sty.PaintToken(edged(40, tc.glyph)+" ", tc.tok)
			if !strings.HasPrefix(got, want) {
				t.Fatalf("the gutter is %q, want it to open with %q", got, want)
			}
		})
	}
}

// TestStreamingOutranksEveryState is the precedence the ladder promises: a
// reply arriving is the most live thing that can be true of this surface, so it
// takes the cell even while a question is open behind it.
func TestStreamingOutranksEveryState(t *testing.T) {
	sty := tokens.NewStylerIn(tokens.NoColor, tokens.FocusNormal, tokens.Plain)
	m := stateModel(t, sty)
	m.SetState(StateQuestion)
	m.Streaming(true, 2)
	if got, want := gutter(m.Render(40, 1)), edged(40, tokens.Spinner(2)); !strings.HasPrefix(got, want) {
		t.Fatalf("a streaming turn drew %q, want it to open with %q", got, want)
	}
	// And the state comes back the instant the reply lands, because the
	// question did not go anywhere.
	m.Streaming(false, 0)
	if got, want := gutter(m.Render(40, 1)), edged(40, tokens.GlyphNeedsHuman); !strings.HasPrefix(got, want) {
		t.Fatalf("after the reply the gutter is %q, want it to open with %q", got, want)
	}
}

// TestTheEdgeRunsDownEveryDraftedRowAndThePromptDoesNot: a draft that wrapped
// to four lines is one thing being typed. The edge says so; a prompt repeated
// on each row would say the opposite.
func TestTheEdgeRunsDownEveryDraftedRowAndThePromptDoesNot(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal),
		NewlineKeys: []string{"alt+enter"}})
	m.Focus(true)
	for i := range 3 {
		if i > 0 {
			m.Key(altEnterKey())
		}
		typeString(m, "line")
	}
	rows := strings.Split(m.Render(40, 1+m.GrowRows(40)), "\n")
	if len(rows) != 3 {
		t.Fatalf("a 3-line draft drew %d rows", len(rows))
	}
	for i, row := range rows {
		if !strings.HasPrefix(row, tokens.GlyphHugEdge) {
			t.Fatalf("row %d carries no edge: %q", i, row)
		}
		hasPrompt := strings.HasPrefix(row, edged(40, tokens.GlyphPromptChat))
		if want := i == 0; hasPrompt != want {
			t.Fatalf("row %d prompt=%v, want %v: %q", i, hasPrompt, want, row)
		}
	}
}

// TestAnOutOfRangeStateReadsAsIdle: a render must not go quiet or die because a
// caller handed it a number.
func TestAnOutOfRangeStateReadsAsIdle(t *testing.T) {
	m := stateModel(t, tokens.NewStylerIn(tokens.NoColor, tokens.FocusNormal, tokens.Plain))
	m.SetState(State(200))
	if got, want := gutter(m.Render(40, 1)), edged(40, tokens.GlyphPromptChat); !strings.HasPrefix(got, want) {
		t.Fatalf("an out-of-range state drew %q", got)
	}
}

// TestTheGutterShedsFromTheRightAndNeverLosesItsEdge: a terminal too narrow for
// three cells gives up the space first and the prompt second. The edge is never
// dropped — a row with no gutter has stopped being a composer.
func TestTheGutterShedsFromTheRightAndNeverLosesItsEdge(t *testing.T) {
	// An EMPTY draft, so the one visible row is the draft's first row rather
	// than whichever row the tail-anchored cursor is sitting on: what is under
	// test is the gutter, not the scroll.
	m := New(Options{Styler: tokens.NewStylerIn(tokens.NoColor, tokens.FocusNormal, tokens.Plain)})
	m.Focus(true)
	for width := 1; width <= 6; width++ {
		row := gutter(m.Render(width, 1))
		if !strings.HasPrefix(row, tokens.GlyphHugEdge) {
			t.Fatalf("width %d drew no edge: %q", width, row)
		}
		if n := ansi.StringWidth(row); n > width {
			t.Fatalf("width %d overran to %d cells: %q", width, n, row)
		}
		if want := gutterAt(innerWidth(width)); want >= 2 &&
			!strings.HasPrefix(row, edged(width, tokens.GlyphPromptChat)) {
			t.Fatalf("width %d (gutter %d) dropped the prompt before the space: %q", width, want, row)
		}
	}
}

// TestRestorePutsAFailedSendBackAndRefusesToOverwrite is the other half of the
// failed state: the sentence that did not go is the one thing the reader needs,
// and the one thing they must not lose to a failure arriving late.
func TestRestorePutsAFailedSendBackAndRefusesToOverwrite(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	if !m.Restore("the words that did not go") {
		t.Fatal("an empty draft refused a restore")
	}
	if got := m.Value(); got != "the words that did not go" {
		t.Fatalf("the draft holds %q", got)
	}
	// The ghost hints stay gone: this is a sentence waiting for a second
	// attempt, not an opening.
	if strings.Contains(m.Render(60, 1), hintCells[HintIdle][0]) {
		t.Fatal("a restored draft is teaching the opening move")
	}
	// A reader who started typing in the half-second the post was in flight
	// keeps every character.
	if m.Restore("something else") {
		t.Fatal("a restore overwrote a draft that had words in it")
	}
	if got := m.Value(); got != "the words that did not go" {
		t.Fatalf("the draft was overwritten: %q", got)
	}
	if m.Restore("") {
		t.Fatal("an empty restore claimed to have done something")
	}
}
