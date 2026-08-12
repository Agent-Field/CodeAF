package footer

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The bar row of §7's hug, tested as the picture a reader meets: the tabs on
// the left, what is live in the middle, and the standing facts hard against the
// right edge.

// hugContext is the row at its fullest: at home, a turn in flight with its own
// receipts, and every standing fact the right zone can carry.
func hugContext() FocusContext {
	return FocusContext{
		Places:       places(),
		Dir:          "~/a/aforge-v2",
		Spend:        0.31,
		HaveSpend:    true,
		CtxUsed:      8000,
		CtxWindow:    262144,
		HaveCtx:      true,
		Live:         true,
		Elapsed:      12 * time.Second,
		TurnCost:     0.03,
		HaveTurnCost: true,

		EscInterrupts: true,
	}
}

// TestTheRightZoneReadsInOneSentence is the anatomy §7 words: the ground, the
// gauge with the window it is a gauge OF, and the day's money last.
func TestTheRightZoneReadsInOneSentence(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	out := row(t, m, hugContext(), 140)
	want := "~/a/aforge-v2" + sep + tokens.Gauge(8000.0/262144) + " 3.1% of 262K" + sep + "$0.31 today"
	if !strings.HasSuffix(out, want) {
		t.Fatalf("the right zone is not %q: %q", want, out)
	}
	if ansi.StringWidth(out) != 140 {
		t.Fatalf("the right zone does not reach the edge: %d cells: %q", ansi.StringWidth(out), out)
	}
}

// TestTheTurnsReceiptsLiveInTheMiddleAndLeaveWithIt: elapsed and this turn's
// cost are things that are true RIGHT NOW, so they stand beside the interrupt
// chip and take their cells back when the turn lands.
func TestTheTurnsReceiptsLiveInTheMiddleAndLeaveWithIt(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	ctx := hugContext()
	live := row(t, m, ctx, 140)
	for _, want := range []string{"interrupt esc", "12s", "$0.03"} {
		if !strings.Contains(live, want) {
			t.Fatalf("a live turn does not carry %q: %q", want, live)
		}
	}
	// They are BEFORE the standing facts and after the tabs.
	if strings.Index(live, "12s") > strings.Index(live, "~/a/aforge-v2") {
		t.Fatalf("the turn's receipts are drawn among the standing facts: %q", live)
	}

	ctx.Live, ctx.EscInterrupts = false, false
	settled := row(t, m, ctx, 140)
	for _, gone := range []string{"interrupt esc", "12s", "$0.03"} {
		if strings.Contains(settled, gone) {
			t.Fatalf("a settled room still carries %q: %q", gone, settled)
		}
	}
	// And the day's money is untouched by any of it.
	if !strings.HasSuffix(settled, "$0.31 today") {
		t.Fatalf("the day total did not survive the turn: %q", settled)
	}
}

// TestTheContextGaugeClimbsItsLadderAndTurnsAmberOnce walks the whole reading:
// the one-cell block fills with usage, the percentage rides beside it, and the
// pair goes amber exactly at the warn point and not a token before.
func TestTheContextGaugeClimbsItsLadderAndTurnsAmberOnce(t *testing.T) {
	sty := styler()
	m := New(Options{Styler: sty})
	const window = int64(100_000)
	var lastCell string
	for _, used := range []int64{0, 10_000, 30_000, 50_000, 70_000, 74_000, 76_000, 100_000} {
		ctx := FocusContext{Places: places(), CtxUsed: used, CtxWindow: window, HaveCtx: true}
		plain := row(t, m, ctx, 120)
		cell := tokens.Gauge(float64(used) / float64(window))
		if !strings.Contains(plain, cell) {
			t.Fatalf("used %d: the gauge cell %q is not on the row: %q", used, cell, plain)
		}
		// The ladder never goes backwards as the window fills.
		if lastCell != "" && gaugeRung(cell) < gaugeRung(lastCell) {
			t.Fatalf("used %d: the gauge fell from %q to %q", used, lastCell, cell)
		}
		lastCell = cell

		painted := m.Render(ctx, 120)
		amber := used >= int64(float64(window)*tokens.ContextWarnFraction)
		wantTok := tokens.TextTertiary
		if amber {
			wantTok = tokens.Amber
		}
		if !strings.Contains(painted, sty.PaintToken(cell+" "+tokens.Percent(float64(used)/float64(window)), wantTok)) {
			t.Fatalf("used %d of %d: the gauge is not %v", used, window, wantTok)
		}
	}
}

// gaugeRung is where a cell sits on [tokens.GaugeCells].
func gaugeRung(cell string) int {
	for i, c := range tokens.GaugeCells {
		if c == cell {
			return i
		}
	}
	return -1
}

// TestAnUnknownWindowDrawsNoGaugeAtAll is §16's EMPTINESS at the one cell the
// meta strip broke it: it drew `— ctx` on every frame the engine had not
// journalled a window, spending three cells to announce its own ignorance on a
// row where every cell is contested.
func TestAnUnknownWindowDrawsNoGaugeAtAll(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	for _, ctx := range []FocusContext{
		{Places: places(), Spend: 0.31, HaveSpend: true},
		{Places: places(), Spend: 0.31, HaveSpend: true, CtxUsed: 8000},
		{Places: places(), Spend: 0.31, HaveSpend: true, HaveCtx: true, CtxWindow: 0},
	} {
		out := row(t, m, ctx, 120)
		for _, cell := range tokens.GaugeCells {
			if strings.Contains(out, cell) {
				t.Fatalf("an unknown window drew the gauge cell %q: %q", cell, out)
			}
		}
		for _, refused := range []string{tokens.GlyphMissing, "ctx", "%"} {
			if strings.Contains(out, refused) {
				t.Fatalf("an unknown window announced itself with %q: %q", refused, out)
			}
		}
	}
}

// TestTheWindowWordLeavesBeforeTheReading: `of 262K` is the gauge's tail, not a
// fact of its own, so a narrowing row sheds it first and keeps `▁ 3.1%` — which
// still says whether there is room. The tail can never outlive the reading.
func TestTheWindowWordLeavesBeforeTheReading(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	ctx := hugContext()
	sawReadingAlone := false
	for width := 140; width >= 1; width-- {
		out := row(t, m, ctx, width)
		reading, tail := strings.Contains(out, "3.1%"), strings.Contains(out, "of 262K")
		if tail && !reading {
			t.Fatalf("at width %d the window word outlived its reading: %q", width, out)
		}
		if reading && !tail {
			sawReadingAlone = true
		}
	}
	if !sawReadingAlone {
		t.Fatal("the window word never left on its own; the split buys nothing")
	}
}

// TestTheDegradationOrderIsTheOneTheLawStates sweeps the row narrower one cell
// at a time and records the width each thing left at. §7 and §16 write the
// ladder as one sentence and this is that sentence as numbers.
func TestTheDegradationOrderIsTheOneTheLawStates(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	ctx := hugContext()
	ctx.Health = []string{"visitor"}

	// leftAt is the widest width at which a thing is NOT on the row.
	leftAt := func(has func(string) bool) int {
		for width := 1; width <= 200; width++ {
			if !has(row(t, m, ctx, width)) {
				continue
			}
			// Found the narrowest width that still has it; the width it left at
			// is one below.
			return width - 1
		}
		return 0
	}
	contains := func(s string) func(string) bool {
		return func(out string) bool { return strings.Contains(out, s) }
	}

	middle := leftAt(contains("interrupt esc"))
	health := leftAt(contains("visitor"))
	window := leftAt(contains("of 262K"))
	dir := leftAt(contains("~/a/aforge-v2"))
	tabs := leftAt(contains("notebook"))
	gauge := leftAt(contains("3.1%"))
	spend := leftAt(contains("$0.31 today"))
	place := leftAt(contains("chat"))

	ladder := []struct {
		name string
		at   int
	}{
		{"middle", middle}, {"health", health}, {"window word", window},
		{"dir", dir}, {"tabs", tabs}, {"gauge", gauge},
		{"day total", spend}, {"where you are", place},
	}
	for i := 1; i < len(ladder); i++ {
		if ladder[i-1].at <= ladder[i].at {
			t.Fatalf("%s left at %d and %s left at %d — the ladder is out of order:\n%+v",
				ladder[i-1].name, ladder[i-1].at, ladder[i].name, ladder[i].at, ladder)
		}
	}
	t.Logf("degradation ladder: %+v", ladder)
}

// TestTheStandingFactsAreDimAndTheMoneyIsNot: the right zone is receipts, which
// §12 puts in the dim tier, with the one exception the palette names — money is
// green because money is a thing a reader is looking FOR.
func TestTheStandingFactsAreDim(t *testing.T) {
	sty := styler()
	m := New(Options{Styler: sty})
	painted := m.Render(hugContext(), 140)
	for _, dim := range []string{"~/a/aforge-v2", "$0.31 today"} {
		if !strings.Contains(painted, sty.PaintToken(dim, tokens.TextTertiary)) {
			t.Fatalf("%q is not at the chrome tier: %q", dim, painted)
		}
	}
	// The TURN's money is green, because it is the live figure a reader watches.
	if !strings.Contains(painted, sty.PaintToken("$0.03", tokens.Green)) {
		t.Fatalf("the turn's cost is not green: %q", painted)
	}
}

// TestTheHugRowSurvivesEveryWidth is the sweep: nothing panics, nothing wraps,
// and no row is wider than the rectangle it was handed — at every width from
// one cell to a very wide terminal, in every state this row can be in.
func TestTheHugRowSurvivesEveryWidth(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI16, tokens.ANSI256, tokens.TrueColor} {
		m := New(Options{Styler: tokens.NewStyler(profile, tokens.FocusNormal)})
		states := map[string]FocusContext{
			"empty":    {},
			"at home":  {Places: places()},
			"full":     hugContext(),
			"inside":   {Places: places(), ScopeTail: crumbTail, Dir: "~/a/aforge-v2", Spend: 1.42, HaveSpend: true},
			"blocked":  {Places: places(), KeyMode: KeyModeAnswer, KeyModeCount: 3, Attention: 2},
			"failed":   {Places: places(), Input: InputFailed, Hint: "could not reach the store"},
			"a health": {Places: places(), Health: []string{"visitor " + tokens.GlyphSeparator + " pid 4242"}},
		}
		for name, ctx := range states {
			for width := 1; width <= 200; width++ {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%v/%s at %d panicked: %v", profile, name, width, r)
						}
					}()
					out := m.Render(ctx, width)
					if strings.Contains(out, "\n") {
						t.Fatalf("%v/%s at %d wrapped: %q", profile, name, width, out)
					}
					if n := ansi.StringWidth(out); n > width {
						t.Fatalf("%v/%s at %d is %d cells: %q", profile, name, width, n, out)
					}
					// Every target the row advertises lies inside the row it drew.
					for _, target := range m.Targets(ctx, width) {
						if target.From < 0 || target.To > width || target.From >= target.To {
							t.Fatalf("%v/%s at %d: %s spans %d..%d",
								profile, name, width, target.ID, target.From, target.To)
						}
					}
				}()
			}
		}
	}
}

// TestThePillIsDrawnWhereThereIsAGroundAndNowhereElse walks the whole profile
// ladder in one place: the filled chip at truecolor and 256, the plain
// bright-versus-dim fallback at 16 and none, and identical PLAIN text at every
// rung — the tier changes what a cell is painted with, never how many cells the
// row occupies (12.7's governing invariant).
func TestThePillIsDrawnWhereThereIsAGroundAndNowhereElse(t *testing.T) {
	ctx := FocusContext{Places: places()}
	var plainRow string
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI16, tokens.ANSI256, tokens.TrueColor} {
		sty := tokens.NewStyler(profile, tokens.FocusNormal)
		m := New(Options{Styler: sty})
		painted := m.Render(ctx, 120)
		stripped := ansi.Strip(painted)

		banded := profile.SelectionStyle() == tokens.SelectionBand
		hasCaps := strings.Contains(stripped, tokens.GlyphChipCapLeft)
		if hasCaps != banded {
			t.Fatalf("%v: pill drawn=%v, want %v: %q", profile, hasCaps, banded, stripped)
		}
		if banded {
			if !strings.Contains(painted, sty.PaintOn(pillPad+"chat"+pillPad, pillWord, pillGround)) {
				t.Fatalf("%v: the current tab is not filled: %q", profile, painted)
			}
			// Only the current one.
			for _, other := range []string{"work", "notebook"} {
				if strings.Contains(painted, sty.PaintOn(pillPad+other+pillPad, pillWord, pillGround)) {
					t.Fatalf("%v: %q is filled too: %q", profile, other, painted)
				}
			}
		} else {
			if !strings.Contains(painted, sty.PaintToken("chat", tokens.TextSecondary)) &&
				profile != tokens.NoColor {
				t.Fatalf("%v: the current tab is not brighter than the rest: %q", profile, painted)
			}
			if want := lensPad + "chat" + tabGap + "work" + tabGap + "notebook"; !strings.HasPrefix(stripped, want) {
				t.Fatalf("%v: the fallback tabs are %q", profile, stripped)
			}
		}
		// The plain text of the pilled rungs matches, and the plain text of the
		// unpilled rungs matches — the caps are two real cells, so the two
		// families differ by exactly those cells and by nothing else.
		if plainRow == "" || banded == strings.Contains(plainRow, tokens.GlyphChipCapLeft) {
			if plainRow != "" && plainRow != stripped {
				t.Fatalf("%v: the row's plain text moved within one family:\n got %q\nwant %q",
					profile, stripped, plainRow)
			}
		}
		plainRow = stripped
	}
}

// TestTheWholePillIsOneTarget: a reader points at the tab, not at the geometry
// of its left edge, so the caps and the padding answer with the word's own id.
func TestTheWholePillIsOneTarget(t *testing.T) {
	m := New(Options{Styler: styler()})
	ctx := FocusContext{Places: places()}
	const width = 120
	row := ansi.Strip(m.Render(ctx, width))
	from := strings.Index(row, tokens.GlyphChipCapLeft)
	if from < 0 {
		t.Fatalf("no pill on the row: %q", row)
	}
	// Every cell of `▐ chat ▌` answers with the chat tab's id.
	for x := tokens.LensIndent; x < tokens.LensIndent+ansi.StringWidth(
		tokens.GlyphChipCapLeft+pillPad+"chat"+pillPad+tokens.GlyphChipCapRight); x++ {
		id, ok := m.TargetAt(ctx, width, x)
		if !ok || id != "place:chat" {
			t.Fatalf("column %d of the pill answered (%q,%v)", x, id, ok)
		}
	}
	// And exactly one target covers it.
	seen := 0
	for _, target := range m.Targets(ctx, width) {
		if target.ID == "place:chat" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("the pill is %d targets, want one chip", seen)
	}
}
