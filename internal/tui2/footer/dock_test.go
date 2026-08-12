package footer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// §6's dock: the sidebar, hidden, compressed to one line — and, since this wave
// makes hidden the default, the only thing on the frame that says live work
// exists at all. These tests read the PICTURE, because every promise the dock
// makes is about what a reader sees at the end of one row.

// dockCtx is a bar row with the day's money on it, so the dock's tail is the
// figure §6 writes after it rather than a second copy of one.
func dockCtx(d Dock) FocusContext {
	return FocusContext{Places: places(), Spend: 0.31, HaveSpend: true, Dock: d}
}

// TestTheDockReadsAsSixSevenWritesIt is the line from §6, rendered: the glyph,
// the counts that are nonzero, and the day's money behind them.
func TestTheDockReadsAsSixSevenWritesIt(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	row := ansi.Strip(m.Render(dockCtx(Dock{Shown: true, Working: 2, Questions: 1}), 120))
	want := tokens.GlyphWorking + " 2 working " + tokens.GlyphSeparator +
		" 1 question " + tokens.GlyphSeparator + " $0.31 today"
	if !strings.Contains(row, want) {
		t.Fatalf("the dock reads %q, want it to contain %q", row, want)
	}
	// It ends the row: §16's right edge is a column, and the money is the last
	// thing on it.
	if !strings.HasSuffix(strings.TrimRight(row, " "), "$0.31 today") {
		t.Fatalf("the dock is not against the right edge: %q", row)
	}
}

// TestTheDockCountsOnlyWhatIsThere: a zero is not news. §16's EMPTINESS at the
// one place a `0 working` would read as a statement about the work rather than
// about the absence of any.
func TestTheDockCountsOnlyWhatIsThere(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	for _, tc := range []struct {
		name string
		dock Dock
		want string
		gone []string
	}{
		{"working only", Dock{Shown: true, Working: 2},
			tokens.GlyphWorking + " 2 working", []string{"question"}},
		{"one question only", Dock{Shown: true, Questions: 1},
			tokens.GlyphWorking + " 1 question", []string{"working"}},
		{"two questions", Dock{Shown: true, Questions: 2},
			tokens.GlyphWorking + " 2 questions", []string{"working"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := ansi.Strip(m.Render(dockCtx(tc.dock), 120))
			if !strings.Contains(row, tc.want) {
				t.Fatalf("the dock reads %q, want %q", row, tc.want)
			}
			for _, banned := range tc.gone {
				if strings.Contains(row, banned) {
					t.Fatalf("the dock said %q with a count of zero: %q", banned, row)
				}
			}
			if strings.Contains(row, " 0 ") {
				t.Fatalf("the dock drew a zero: %q", row)
			}
		})
	}
}

// TestAShutDrawerWithNothingInItSaysNothing: the working glyph on a window
// where nothing is working is §18.2's lie about liveness, and this is the one
// assertion that keeps the door from becoming one.
func TestAShutDrawerWithNothingInItSaysNothing(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	for _, tc := range []struct {
		name string
		dock Dock
	}{
		{"rail on the frame", Dock{Working: 3, Questions: 1}},
		{"shut and empty", Dock{Shown: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := ansi.Strip(m.Render(dockCtx(tc.dock), 120))
			if strings.Contains(row, tokens.GlyphWorking) {
				t.Fatalf("the row drew a dock it has nothing to say with: %q", row)
			}
			if dockWidth(tc.dock) != 0 {
				t.Fatalf("a silent dock still claims %d cells", dockWidth(tc.dock))
			}
			// The day's money is untouched either way: the dock rides beside it,
			// it does not replace it.
			if !strings.Contains(row, "$0.31 today") {
				t.Fatalf("the row lost the day's total: %q", row)
			}
		})
	}
}

// TestTheMoneyIsSaidOnce: §19 forbids a screen saying a thing twice because two
// elements each wanted it, and §6's dock line ends with the very figure this row
// has carried since §13. There is exactly one `today` on the row.
func TestTheMoneyIsSaidOnce(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	row := ansi.Strip(m.Render(dockCtx(Dock{Shown: true, Working: 2, Questions: 1}), 120))
	if n := strings.Count(row, "today"); n != 1 {
		t.Fatalf("the row says `today` %d times: %q", n, row)
	}
	if n := strings.Count(row, "$0.31"); n != 1 {
		t.Fatalf("the row draws the day's money %d times: %q", n, row)
	}
	// And an absent day total leaves the dock ending at its last count rather
	// than inventing a figure (§16: absent is absent, never `$0.00`).
	ctx := dockCtx(Dock{Shown: true, Working: 2})
	ctx.HaveSpend = false
	row = ansi.Strip(m.Render(ctx, 120))
	if strings.Contains(row, "$") {
		t.Fatalf("an unknown day total was drawn anyway: %q", row)
	}
	if !strings.Contains(row, tokens.GlyphWorking+" 2 working") {
		t.Fatalf("the dock left with the money it does not own: %q", row)
	}
}

// TestAnOpenQuestionTintsTheDockAmber: §18.3 gives amber exactly one meaning —
// a human is needed — and a hidden rail is precisely the case where nothing
// else on the frame is going to say so.
func TestAnOpenQuestionTintsTheDockAmber(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	m := New(Options{Styler: sty})

	quiet, ok := dockFact(Dock{Shown: true, Working: 2})
	if !ok || quiet.tok != tokens.TextTertiary {
		t.Fatalf("a dock with no question is painted %v, want the chrome tier", quiet.tok)
	}
	asked, ok := dockFact(Dock{Shown: true, Working: 2, Questions: 1})
	if !ok || asked.tok != tokens.Amber {
		t.Fatalf("a dock with an open question is painted %v, want amber", asked.tok)
	}
	// And the frame carries the amber, not just the struct.
	row := m.Render(dockCtx(Dock{Shown: true, Working: 2, Questions: 1}), 120)
	if !strings.Contains(row, sty.PaintToken(asked.text, tokens.Amber)) {
		t.Fatalf("the painted row does not carry the amber dock: %q", row)
	}
}

// TestTheDockIsTheOneDoorInTheRightZone: clicking it opens the drawer, and
// nothing else out there answers a pointer — hit.go's rule, kept with its one
// stated exception.
func TestTheDockIsTheOneDoorInTheRightZone(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	dock := Dock{Shown: true, Working: 2, Questions: 1}
	ctx := dockCtx(dock)
	ctx.Dir = "~/aforge-v2"
	const width = 120
	row := ansi.Strip(m.Render(ctx, width))

	var found bool
	for _, target := range m.Targets(ctx, width) {
		if target.ID != DockTarget {
			continue
		}
		found = true
		if got := cells(row, target.From, target.To); got != dockWidthText(dock) {
			t.Fatalf("the dock target covers %q, want %q", got, dockWidthText(dock))
		}
		// A click anywhere on the counts answers, including its last cell.
		for x := target.From; x < target.To; x++ {
			if id, ok := m.TargetAt(ctx, width, x); !ok || id != DockTarget {
				t.Fatalf("column %d of the dock answers %q (%v)", x, id, ok)
			}
		}
	}
	if !found {
		t.Fatalf("the dock is not a target: %+v", m.Targets(ctx, width))
	}
	// The money behind it is still a statement and still answers nothing.
	moneyAt := strings.Index(row, "$0.31")
	if moneyAt < 0 {
		t.Fatalf("the row lost the day's total: %q", row)
	}
	if id, ok := m.TargetAt(ctx, width, ansi.StringWidth(row[:moneyAt])); ok {
		t.Fatalf("the day's money became a door onto %q", id)
	}
}

// dockWidthText is the dock's own text, so the target test can name what it
// expects without spelling the words a second time.
func dockWidthText(d Dock) string {
	f, _ := dockFact(d)
	return f.text
}

// TestTheDockOutlivesTheTelemetryOnANarrowRow: 10.3.15 forbids a live lane
// going silent. A row too narrow for everything drops the directory and the
// gauge before it drops the only statement that work is running at all.
func TestTheDockOutlivesTheTelemetryOnANarrowRow(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	ctx := dockCtx(Dock{Shown: true, Working: 2})
	ctx.Dir = "~/a/very/long/place/on/disk"
	ctx.CtxUsed, ctx.CtxWindow, ctx.HaveCtx = 40_000, 262_144, true

	wide := ansi.Strip(m.Render(ctx, 140))
	if !strings.Contains(wide, ctx.Dir) || !strings.Contains(wide, "of ") {
		t.Fatalf("the wide row is missing the telemetry it is supposed to shed: %q", wide)
	}
	for width := 100; width >= 40; width -= 10 {
		row := ansi.Strip(m.Render(ctx, width))
		if !strings.Contains(row, tokens.GlyphWorking) {
			continue
		}
		// While the dock is on the row, nothing below it in the ladder may be
		// keeping cells the dock needed.
		if strings.Contains(row, ctx.Dir) && !strings.Contains(row, "2 working") {
			t.Fatalf("width %d kept the directory and cut the dock: %q", width, row)
		}
	}
	// At a width that fits the money, the tabs and one more thing, that thing is
	// the dock rather than the path.
	row := ansi.Strip(m.Render(ctx, 52))
	if strings.Contains(row, ctx.Dir) {
		t.Fatalf("a 52-cell row spent its cells on the directory: %q", row)
	}
}

// TestTheDockKeepsTheHintsGrammar: §20's hints rule — dim, `·`-separated, no
// bare keys — read on the one run this wave added to the row.
func TestTheDockKeepsTheHintsGrammar(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	row := ansi.Strip(m.Render(dockCtx(Dock{Shown: true, Working: 2, Questions: 1}), 120))
	text := dockWidthText(Dock{Shown: true, Working: 2, Questions: 1})
	if strings.Contains(text, ",") || strings.Contains(text, "|") {
		t.Fatalf("the dock joins its counts with something other than the separator: %q", text)
	}
	if n := strings.Count(text, tokens.GlyphSeparator); n != 1 {
		t.Fatalf("the dock uses %d separators for two counts: %q", n, text)
	}
	if text != strings.ToLower(text) {
		t.Fatalf("the dock wears Title Case chrome (§16): %q", text)
	}
	// And it is joined to the money by the same separator, so the whole tail
	// reads as one run rather than as two zones that collided.
	if !strings.Contains(row, text+" "+tokens.GlyphSeparator+" $0.31 today") {
		t.Fatalf("the dock and the day's money do not read as one run: %q", row)
	}
}
