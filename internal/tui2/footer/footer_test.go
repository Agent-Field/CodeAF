package footer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The contextual line, tested the way a reader meets it: as a FRAME. Every test
// here asserts a picture — which words are on the row, at which tier, in which
// zone, at a width somebody actually runs a terminal at — rather than the
// intermediate strings the layout passes through on its way there.

func styler() *tokens.Styler { return tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal) }

func plainStyler() *tokens.Styler { return tokens.NewStyler(tokens.NoColor, tokens.FocusNormal) }

// places is the left zone the product ships: three homes, `chat` current.
func places() []Place {
	return []Place{
		{ID: "place:chat", Word: "chat", Current: true},
		{ID: "place:work", Word: "work"},
		{ID: "place:notebook", Word: "notebook"},
	}
}

// streamingContext is the frame the row spends most of its live seconds in: at
// home, a turn in flight, the standing facts on the right.
func streamingContext() FocusContext {
	return FocusContext{
		Places:        places(),
		EscInterrupts: true,
		Spend:         1.42,
		HaveSpend:     true,
	}
}

func sampleVerbs() []registry.Entry {
	return []registry.Entry{
		{ID: "key.node.cancel", Verb: "cancel", Key: "c"},
		{ID: "key.node.restart", Verb: "restart", Key: "r"},
		{ID: "slash.tasks", Verb: "focus tasks", Slash: "tasks"},
		{ID: "belt.revise", Verb: "revise"}, // no key, no slash: talk-only
	}
}

func fullContext() FocusContext {
	ctx := streamingContext()
	ctx.Verbs = sampleVerbs()
	ctx.KeyMode, ctx.KeyModeCount, ctx.Attention = KeyModeAnswer, 3, 1
	ctx.Health = []string{"visitor"}
	return ctx
}

func row(t *testing.T, m *Model, ctx FocusContext, width int) string {
	t.Helper()
	return ansi.Strip(m.Render(ctx, width))
}

// TestTheThreeZonesAtEveryTerminal is the anatomy, at the three widths §16's
// degradation ladder is written against: everything at 120, the same three zones
// at 80, and at 40 the row down to where you are and what the day cost.
func TestTheThreeZonesAtEveryTerminal(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	ctx := streamingContext()

	at120 := row(t, m, ctx, 120)
	for _, want := range []string{"chat", "work", "notebook", "interrupt esc", "$1.42 today"} {
		if !strings.Contains(at120, want) {
			t.Fatalf("120 cols is missing %q: %q", want, at120)
		}
	}
	// Zone order left to right, and the right zone hard against the edge.
	if got := strings.Index(at120, "chat"); got != tokens.LensIndent {
		t.Fatalf("places do not open the row at the lens edge (col %d): %q", got, at120)
	}
	if !strings.HasSuffix(at120, "$1.42 today") {
		t.Fatalf("the day total is not on the right edge: %q", at120)
	}
	if ansi.StringWidth(at120) != 120 {
		t.Fatalf("the right zone does not reach the edge: %d cells of 120: %q",
			ansi.StringWidth(at120), at120)
	}

	at80 := row(t, m, ctx, 80)
	for _, want := range []string{"chat", "interrupt esc", "$1.42 today"} {
		if !strings.Contains(at80, want) {
			t.Fatalf("80 cols is missing %q: %q", want, at80)
		}
	}

	at40 := row(t, m, ctx, 40)
	if !strings.Contains(at40, "chat") || !strings.Contains(at40, "$1.42 today") {
		t.Fatalf("40 cols dropped where-you-are or the day total: %q", at40)
	}
	// The tab row survives further than it used to now that the model word has
	// left the right zone; the collapse still happens, it just happens later.
	if at28 := row(t, m, ctx, 28); strings.Contains(at28, "notebook") {
		t.Fatalf("28 cols still draws the whole tab row: %q", at28)
	}
}

// TestSilenceIsTheDefault: with nothing live and nothing to stand on, the row is
// empty. Every standing legend this line used to carry moved to the `?` sheet,
// and a row that filled itself back up with them would be the bug returning.
func TestSilenceIsTheDefault(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	if out := m.Render(FocusContext{}, 80); out != "" {
		t.Fatalf("an empty context drew something: %q", out)
	}
	// Places and standing facts, and nothing between them.
	ctx := FocusContext{Places: places()}
	out := row(t, m, ctx, 100)
	if strings.Contains(out, "help") || strings.Contains(out, "ctrl+") {
		t.Fatalf("a standing legend came back to the middle: %q", out)
	}
	middle := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(out, lensPad+"chat"+tabGap+"work"+tabGap+"notebook"), "sonnet"))
	if middle != "" {
		t.Fatalf("the middle zone is not empty at rest: %q", middle)
	}
}

// TestTheMiddleSaysOnlyWhatIsLiveRightNow walks the three states §7 names.
func TestTheMiddleSaysOnlyWhatIsLiveRightNow(t *testing.T) {
	m := New(Options{Styler: plainStyler()})

	streaming := row(t, m, FocusContext{Places: places(), EscInterrupts: true}, 100)
	if !strings.Contains(streaming, "interrupt esc") {
		t.Fatalf("streaming does not offer the interrupt: %q", streaming)
	}

	asking := row(t, m, FocusContext{Places: places(), KeyMode: KeyModeAnswer, KeyModeCount: 3}, 100)
	if want := "answer 1" + enDash + "3"; !strings.Contains(asking, want) {
		t.Fatalf("an open question does not offer %q: %q", want, asking)
	}

	// Digits that jump to rail rows are NOT a live key in this sense: the rail
	// numbers its own rows, so a chip repeating the range is a standing legend.
	rooms := row(t, m, FocusContext{Places: places(), KeyMode: KeyModeRooms, KeyModeCount: 9}, 100)
	if strings.Contains(rooms, "1"+enDash+"9") || strings.Contains(rooms, "rooms") {
		t.Fatalf("the rail's digit range crept onto the row: %q", rooms)
	}
}

// TestChipsAreVerbFirstAndTwoTiers is §16's chip grammar on this surface: the
// verb first at the brighter tier, the key after it one tier down, and the verb
// never in the dimmest tier.
func TestChipsAreVerbFirstAndTwoTiers(t *testing.T) {
	sty := styler()
	m := New(Options{Styler: sty})
	painted := m.Render(FocusContext{Places: places(), EscInterrupts: true}, 100)
	plain := ansi.Strip(painted)

	verbAt, keyAt := strings.Index(plain, interruptVerb), strings.Index(plain, interruptKey)
	if verbAt < 0 || keyAt < 0 {
		t.Fatalf("the interrupt chip is not on the row: %q", plain)
	}
	if verbAt > keyAt {
		t.Fatalf("the chip is key-first: %q", plain)
	}
	if want := interruptVerb + registry.ChipGap + interruptKey; !strings.Contains(plain, want) {
		t.Fatalf("the chip is not one gap of one space: %q", plain)
	}
	if want := sty.PaintToken(interruptVerb, tokens.TextSecondary); !strings.Contains(painted, want) {
		t.Fatalf("the verb is not at the secondary tier:\n got %q\nwant substring %q", painted, want)
	}
	if want := sty.PaintToken(registry.ChipGap+interruptKey, tokens.TextTertiary); !strings.Contains(painted, want) {
		t.Fatalf("the key is not one tier down:\n got %q\nwant substring %q", painted, want)
	}
	if bad := sty.PaintToken(interruptVerb, tokens.TextTertiary); strings.Contains(painted, bad) {
		t.Fatal("an interactive chip is living in the dimmest tier")
	}
}

// TestAnAnsweredQuestionTintsItsChipAmber: the amber badge this row used to
// carry is gone, and the chip carries the meaning instead — amber only ever
// means a human is actually needed (§12).
func TestAnAnsweredQuestionTintsItsChipAmber(t *testing.T) {
	sty := styler()
	m := New(Options{Styler: sty})
	blocked := m.Render(FocusContext{KeyMode: KeyModeAnswer, KeyModeCount: 3, Attention: 2}, 100)
	if want := sty.PaintToken(answerVerb, tokens.Amber); !strings.Contains(blocked, want) {
		t.Fatalf("a blocked human is not amber:\n got %q\nwant substring %q", blocked, want)
	}
	if strings.Contains(ansi.Strip(blocked), tokens.GlyphNeedsHuman+"2") {
		t.Fatalf("the old attention badge is still drawn: %q", ansi.Strip(blocked))
	}
	quiet := m.Render(FocusContext{KeyMode: KeyModeAnswer, KeyModeCount: 3}, 100)
	if want := sty.PaintToken(answerVerb, tokens.Amber); strings.Contains(quiet, want) {
		t.Fatal("amber was spent with nobody blocked on it")
	}
}

// TestTheCurrentPlaceWearsTheFilledPill: the tab you are standing in is a chip
// with a ground, not a word one tier up. The caps are half-block cells painted
// with the chip's own colour as INK, so each end of the pill is half filled and
// half floor — which is why the row can carry a rounded chip without a border.
func TestTheCurrentPlaceWearsTheFilledPill(t *testing.T) {
	sty := styler()
	m := New(Options{Styler: sty})
	painted := m.Render(FocusContext{Places: places()}, 100)

	if want := sty.PaintOn(pillPad+"chat"+pillPad, pillWord, pillGround); !strings.Contains(painted, want) {
		t.Fatalf("the current place is not a filled pill:\n got %q\nwant substring %q", painted, want)
	}
	for _, cap := range []string{tokens.GlyphChipCapLeft, tokens.GlyphChipCapRight} {
		if want := sty.PaintToken(cap, pillGround); !strings.Contains(painted, want) {
			t.Fatalf("the pill has no %q cap:\n got %q\nwant substring %q", cap, painted, want)
		}
	}
	// The other two carry no ground at all — a second pill would say the reader
	// was in two places.
	for _, dim := range []string{"work", "notebook"} {
		if want := sty.PaintOn(pillPad+dim+pillPad, pillWord, pillGround); strings.Contains(painted, want) {
			t.Fatalf("%q is also filled: %q", dim, painted)
		}
	}
	if want := tokens.GlyphChipCapLeft + pillPad + "chat" + pillPad + tokens.GlyphChipCapRight +
		tabGap + "work"; !strings.HasPrefix(ansi.Strip(painted), lensPad+want) {
		t.Fatalf("the pill reads %q, want it to open with %q", ansi.Strip(painted), want)
	}
}

// TestWithoutARaisedGroundTheCurrentPlaceIsJustBrighter is the pill's honest
// degradation: at 16 colours and none there is no trustworthy raised
// background, so the chip is not drawn in reverse video (which would be a black
// slab louder than the answer it gives) — the row falls back to §7's floor, the
// current word one tier up and the others dim.
func TestWithoutARaisedGroundTheCurrentPlaceIsJustBrighter(t *testing.T) {
	sty := plainStyler()
	m := New(Options{Styler: sty})
	painted := m.Render(FocusContext{Places: places()}, 100)
	for _, cap := range []string{tokens.GlyphChipCapLeft, tokens.GlyphChipCapRight} {
		if strings.Contains(painted, cap) {
			t.Fatalf("a profile with no raised ground still drew a pill cap: %q", painted)
		}
	}
	if got := ansi.Strip(painted); !strings.HasPrefix(got, lensPad+"chat"+tabGap+"work") {
		t.Fatalf("the fallback row is %q", got)
	}
}

// TestTheCurrentPlaceIsBrightAndTheRestAreDim, at the tier level, on the
// profile that draws no pill.
func TestTheCurrentPlaceIsBrightAndTheRestAreDim(t *testing.T) {
	sty := tokens.NewStyler(tokens.ANSI16, tokens.FocusNormal)
	m := New(Options{Styler: sty})
	painted := m.Render(FocusContext{Places: places()}, 100)

	if want := sty.PaintToken("chat", tokens.TextSecondary); !strings.Contains(painted, want) {
		t.Fatalf("the current place is not bright:\n got %q\nwant substring %q", painted, want)
	}
	for _, dim := range []string{"work", "notebook"} {
		if want := sty.PaintToken(dim, tokens.TextTertiary); !strings.Contains(painted, want) {
			t.Fatalf("%q is not dim:\n got %q\nwant substring %q", dim, painted, want)
		}
	}
	// And moving does not move the words: the tabs are a fixed row, only the
	// brightness travels.
	moved := places()
	moved[0].Current, moved[1].Current = false, true
	if got, want := ansi.Strip(m.Render(FocusContext{Places: moved}, 100)), ansi.Strip(painted); got != want {
		t.Fatalf("changing the current place moved the row:\n got %q\nwant %q", got, want)
	}
}

// TestTheBreadcrumbStandsBesideTheTabs is the cohabitation call, DECIDED THE
// OTHER WAY by a reader on the live build.
//
// The trail used to replace the tabs, on the reasoning that its first segment
// did the tabs' job. It does not: a trail is a way UP and the tabs are a way
// ACROSS, and a reader who had descended into a task reported that there was
// "no way to go back to work" — which was literally true, because the only door
// to that page had been taken off the screen to make room for the name of the
// room they were standing in.
//
// So the three page words are permanent, and the trail says how deep inside one
// of them the reader is, from the middle zone.
func TestTheBreadcrumbStandsBesideTheTabs(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	ctx := FocusContext{
		Places:    places(),
		ScopeTail: tokens.GlyphScopeUp + " a named room " + tokens.GlyphScopeUp + " Higher-order investment angles",
	}
	out := row(t, m, ctx, 120)
	if !strings.Contains(out, "Higher-order investment angles") {
		t.Fatalf("the trail is not on the row: %q", out)
	}
	for _, tab := range []string{"chat", "work", "notebook"} {
		if !strings.Contains(out, tab) {
			t.Fatalf("entering a room took the %q tab off the row: %q", tab, out)
		}
	}
	// The tabs come first and the trail after them: where you CAN go, then how
	// deep in you are.
	if strings.Index(out, "notebook") > strings.Index(out, "Higher-order") {
		t.Fatalf("the trail is drawn before the tabs: %q", out)
	}
	// And every tab is still a door from inside the room.
	for _, p := range places() {
		found := false
		for _, target := range m.Targets(ctx, 120) {
			if target.ID == p.ID {
				found = true
			}
		}
		if !found {
			t.Fatalf("the %q tab is not clickable from inside a room", p.Word)
		}
	}
}

// TestTheCurrentNameSurvivesLongest: the trail elides ancestors first and keeps
// the reader's own place, at its own tier.
func TestTheCurrentNameSurvivesLongest(t *testing.T) {
	sty := styler()
	m := New(Options{Styler: sty})
	ctx := FocusContext{
		ScopeTail: tokens.GlyphScopeUp + " untitled room " + tokens.GlyphScopeUp + " Higher-order investment angles",
		Spend:     1.42,
		HaveSpend: true,
	}
	if want := sty.PaintToken("Higher-order investment angles", tokens.TextSecondary); !strings.Contains(m.Render(ctx, 140), want) {
		t.Fatalf("the reader's own place is not the bright segment: %q", m.Render(ctx, 140))
	}
	narrow := row(t, m, ctx, 60)
	if strings.Contains(narrow, "untitled room") {
		t.Fatalf("an ancestor outlived the current name: %q", narrow)
	}
	if !strings.Contains(narrow, "…") {
		t.Fatalf("the elision is not marked: %q", narrow)
	}
}

// TestDegradationOrder is §7's ladder, walked one cell at a time: the middle
// empties first, then the places collapse to the current word, then the right
// drops, and where-you-are is the last thing standing.
func TestDegradationOrder(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	ctx := streamingContext()

	full := row(t, m, ctx, 200)
	for _, want := range []string{"notebook", "interrupt", "$1.42 today"} {
		if !strings.Contains(full, want) {
			t.Fatalf("the widest row is missing %q: %q", want, full)
		}
	}

	stage := func(pred func(string) bool) int {
		for w := 200; w > 0; w-- {
			if !pred(row(t, m, ctx, w)) {
				return w
			}
		}
		return 0
	}
	middleGone := stage(func(out string) bool { return strings.Contains(out, "interrupt") })
	tabsGone := stage(func(out string) bool { return strings.Contains(out, "notebook") })
	rightGone := stage(func(out string) bool { return strings.Contains(out, "today") })

	if !(middleGone > tabsGone && tabsGone > rightGone) {
		t.Fatalf("degradation out of order: middle left at %d, tabs at %d, right at %d",
			middleGone, tabsGone, rightGone)
	}
	// At the width the right zone gives up, the place is still there.
	if out := row(t, m, ctx, rightGone); !strings.Contains(out, "chat") {
		t.Fatalf("where-you-are left before the standing facts did: %q", out)
	}
}

// TestASurvivingZoneSpendsWhatTheDroppedOnesGaveUp pins the hand-back, so
// nobody "fixes" the ladder into blank cells: a fact that leaves gives its cells
// to the zones still standing, and the row it leaves behind is the row this
// context would have drawn if the fact had never existed.
//
// Stating it as an IDENTITY rather than as a width coincidence is what makes it
// survive a re-priced column: whatever the fitter drops at whatever width, the
// surviving row may not carry a reserved hole where the dropped words were.
func TestASurvivingZoneSpendsWhatTheDroppedOnesGaveUp(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	ctx := streamingContext()
	ctx.Dir = "~/a/aforge-v2"

	if wide := row(t, m, ctx, 80); !strings.Contains(wide, "~/a/aforge-v2") ||
		!strings.Contains(wide, "notebook") {
		t.Fatalf("at 80 the directory and the whole tab row should both fit: %q", wide)
	}
	// At 60 the directory has gone. Its cells are not held in reserve: the tab
	// row is still whole, which it could not be if the fitter were still
	// counting sixteen columns for a fact it had already dropped.
	narrow := row(t, m, ctx, 48)
	if strings.Contains(narrow, "aforge-v2") {
		t.Fatalf("at 48 the directory should have gone: %q", narrow)
	}
	if !strings.Contains(narrow, "notebook") {
		t.Fatalf("at 48 the tabs collapsed into cells nobody is using: %q", narrow)
	}
	// And what is left is still flush against the right edge — a zone that gave
	// cells back does not leave the ones it kept floating in the middle.
	if !strings.HasSuffix(narrow, "$1.42 today") || ansi.StringWidth(narrow) != 48 {
		t.Fatalf("the surviving facts are not on the edge: %q (%d cells)", narrow, ansi.StringWidth(narrow))
	}
}

// TestSpendIsAbsentNotZero: §16's emptiness rule, on the one number the product
// is never allowed to guess.
func TestSpendIsAbsentNotZero(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	out := row(t, m, FocusContext{Places: places()}, 100)
	if strings.Contains(out, "$") {
		t.Fatalf("an unknown day total drew a figure: %q", out)
	}
	zero := row(t, m, FocusContext{Places: places(), HaveSpend: true}, 100)
	if !strings.Contains(zero, "$0.00 today") {
		t.Fatalf("a known zero is a fact and should be drawn: %q", zero)
	}
}

// TestStandingFactsRideTheRightEdge: health and the day total are one
// right-aligned column (§16), in that order. There is no model word among them
// — see [FocusContext] for why an identifier is not allowed on this row.
func TestStandingFactsRideTheRightEdge(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	out := row(t, m, FocusContext{
		Places: places(), Health: []string{"visitor " + tokens.GlyphSeparator + " pid 4242"}, Spend: 0.31, HaveSpend: true,
	}, 120)
	want := "visitor " + tokens.GlyphSeparator + " pid 4242" + sep + "$0.31 today"
	if !strings.HasSuffix(out, want) {
		t.Fatalf("the standing facts do not end the row as %q: %q", want, out)
	}
}

// TestAFailedSendIsTheOneColouredSentence: §12 spends coral on broken things,
// and a send that failed is broken. Every other input state belongs to the
// composer's ghost text and says nothing here.
func TestAFailedSendIsTheOneColouredSentence(t *testing.T) {
	sty := styler()
	m := New(Options{Styler: sty})
	if want := sty.PaintToken("send failed", tokens.Coral); !strings.Contains(
		m.Render(FocusContext{Input: InputFailed, Hint: "send failed"}, 80), want) {
		t.Fatal("a failed send is not coral")
	}
	for _, state := range []InputState{InputEmpty, InputTyped, InputQueued} {
		out := row(t, m, FocusContext{Input: state, Hint: "↵ send"}, 80)
		if strings.Contains(out, "send") {
			t.Fatalf("input state %v put its hint on the row: %q", state, out)
		}
	}
}

// TestVerbsCapAtThree: however many live verbs the host hands in, at most three
// render. A middle that grew without bound would stop being scannable, which is
// the whole reason silence is its resting state.
func TestVerbsCapAtThree(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	entries := []registry.Entry{
		{ID: "a", Verb: "one", Key: "1"}, {ID: "b", Verb: "two", Key: "2"},
		{ID: "c", Verb: "three", Key: "3"}, {ID: "d", Verb: "four", Key: "4"},
	}
	out := row(t, m, FocusContext{Verbs: entries}, 200)
	for _, want := range []string{"one 1", "two 2", "three 3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing expected chip %q: %q", want, out)
		}
	}
	if strings.Contains(out, "four") {
		t.Fatalf("a fourth verb was not capped away: %q", out)
	}
}

// TestAVerbWithoutAKeyIsStillAVerb pins the chip's fallback ladder, which lives
// in the registry and is not re-spelled here: the key, else the slash alias,
// else the verb alone.
func TestAVerbWithoutAKeyIsStillAVerb(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	out := row(t, m, FocusContext{Verbs: sampleVerbs()[2:]}, 200)
	for _, want := range []string{"focus tasks /tasks", "revise"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %q", want, out)
		}
	}
}

// TestWidthSweep sweeps width -1..200 across every frame shape this row has.
// Nothing may panic, nothing may exceed its given width, and the result is
// always exactly one row.
func TestWidthSweep(t *testing.T) {
	ctxs := []FocusContext{
		{},
		fullContext(),
		streamingContext(),
		{Places: places()},
		{ScopeTail: tokens.GlyphScopeUp + " untitled room " + tokens.GlyphScopeUp + " a very long name indeed"},
		{Input: InputFailed, Hint: "send failed"},
		{KeyMode: KeyModeAnswer, KeyModeCount: 3, Attention: 1},
		{Spend: 12.5, HaveSpend: true},
		{Places: []Place{{ID: "p", Word: "notebook"}}, Health: []string{"visitor"}},
	}
	for ci, ctx := range ctxs {
		for _, sty := range []*tokens.Styler{nil, styler()} {
			m := New(Options{Styler: sty})
			for width := -1; width <= 200; width++ {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("ctx %d width %d panicked: %v", ci, width, r)
						}
					}()
					out := m.Render(ctx, width)
					if strings.Contains(out, "\n") {
						t.Fatalf("ctx %d width %d produced more than one row: %q", ci, width, out)
					}
					if w := ansi.StringWidth(out); width > 0 && w > width {
						t.Fatalf("ctx %d width %d rendered %d cells: %q", ci, width, w, out)
					}
					if width <= 0 && out != "" {
						t.Fatalf("ctx %d width %d rendered non-empty: %q", ci, width, out)
					}
				}()
			}
		}
	}
}

// TestTheRowOpensAtTheLensEdge: this row is the bottom of a room whose other
// surfaces all begin one depth in, and a footer flush against the frame was the
// room disagreeing with itself about where it started.
func TestTheRowOpensAtTheLensEdge(t *testing.T) {
	m := New(Options{Styler: plainStyler()})
	ctx := streamingContext()
	for width := tokens.LensIndent + 1; width <= 140; width++ {
		out := row(t, m, ctx, width)
		if out == "" {
			continue
		}
		if !strings.HasPrefix(out, lensPad) {
			t.Fatalf("at width %d the row began at column 0: %q", width, out)
		}
		if strings.HasPrefix(out[tokens.LensIndent:], " ") {
			t.Fatalf("at width %d the row began past the edge: %q", width, out)
		}
		if got := ansi.StringWidth(out); got > width {
			t.Fatalf("at width %d the indented row ran %d cells: %q", width, got, out)
		}
	}
	// A terminal too narrow to hold the gutter and a word gives the gutter up.
	narrow := row(t, m, FocusContext{Places: places()}, tokens.LensIndent)
	if strings.HasPrefix(narrow, " ") {
		t.Fatalf("the gutter survived a %d-cell terminal: %q", tokens.LensIndent, narrow)
	}
}

// TestNeverPanicsWithNilStyler mirrors the composer package's own posture for a
// missing Styler.
func TestNeverPanicsWithNilStyler(t *testing.T) {
	if out := New(Options{}).Render(fullContext(), 120); out == "" {
		t.Fatal("nil-styler render produced nothing")
	}
}
