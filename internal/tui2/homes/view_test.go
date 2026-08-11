package homes

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The shipping gate. At every width from 1 to 140, on every state this package
// can be handed, at every selection, with every styler including none at all, a
// render produces at most the rows it was given room for, never a row wider
// than the pane, never a newline inside a row, and never a panic.
//
// 140 is past anything a rail-and-transcript layout hands a detail pane; 1 is
// there because a compositor under pressure hands out a one-column rectangle
// before it hands out none.
func TestRenderNeverOverflowsAndNeverPanics(t *testing.T) {
	states := map[string]State{
		"rich":    rich(),
		"hostile": hostile(),
		"empty":   {},
		"wide":    wide(60),
	}
	heights := []int{1, 2, 3, 7, 24, 60}

	for name, state := range states {
		for _, sel := range selections(state) {
			for width := 1; width <= 140; width++ {
				for _, height := range heights {
					v := NewView(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
					lines := v.Render(state, sel, width, height)
					if len(lines) > height {
						t.Fatalf("%s %v w=%d h=%d: %d lines", name, sel, width, height, len(lines))
					}
					for i, line := range lines {
						if w := blocks.Width(line); w > width {
							t.Fatalf("%s %v w=%d h=%d: line %d is %d cells: %q",
								name, sel, width, height, i, w, line)
						}
						if strings.ContainsAny(line, "\n\r") {
							t.Fatalf("%s %v w=%d: line %d carries a newline: %q", name, sel, width, i, line)
						}
					}
				}
			}
		}
	}
}

// Every painting configuration, at the widths where something changes. A nil
// Styler is in the set on purpose: it is the honest degradation for a terminal
// that would not say what it can do, and a package whose renderer assumed one
// would crash exactly there.
func TestEveryStylerPaintsWithinTheFrame(t *testing.T) {
	state := rich()
	widths := []int{1, 2, 8, 13, 25, 26, 40, 80, 120}
	for name, st := range stylers() {
		v := NewView(st)
		for _, sel := range selections(state) {
			for _, width := range widths {
				lines := v.Render(state, sel, width, 40)
				for i, line := range lines {
					if w := blocks.Width(line); w > width {
						t.Fatalf("%s %v w=%d: line %d is %d cells: %q", name, sel, width, i, w, line)
					}
				}
			}
		}
	}
}

// A View with no Styler paints no escape at all. This is the property the nil
// case exists for: not "does not crash" but "produces bytes a dumb pipe can
// carry".
func TestNoStylerPaintsNoEscapes(t *testing.T) {
	v := NewView(nil)
	state := rich()
	for _, sel := range selections(state) {
		for _, line := range v.Render(state, sel, 80, 40) {
			if strings.ContainsRune(line, 0x1b) {
				t.Fatalf("%v: unstyled render carries an escape: %q", sel, line)
			}
		}
	}
}

// Foreign prose reaches this package from four directions — a model wrote the
// belief, a model wrote the charter, a foreign process wrote the log, and a
// craft name came off disk. None of it may move the cursor or clear the
// screen. The hostile fixture carries a CSI clear, an OSC title set, an OSC 8
// hyperlink and a BEL; none of them may survive to the frame.
func TestForeignProseCannotDriveTheTerminal(t *testing.T) {
	v := NewView(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
	state := hostile()
	for _, sel := range selections(state) {
		for _, line := range v.Render(state, sel, 80, 40) {
			for _, bad := range []string{"\x1b[2J", "\x1b[H", "\x1b]0;", "\x1b]8;", "\x07", "\x00"} {
				if strings.Contains(line, bad) {
					t.Fatalf("%v: %q survived sanitising: %q", sel, bad, line)
				}
			}
		}
	}
}

// The strike on a let-go belief must not cost a cell, or every column right of
// it lands one step off. It is asserted against the same ruler the renderer
// measures with rather than against a rune count, because the whole failure
// mode is a combining mark a ruler disagrees about.
func TestTheStrikeCostsNoCells(t *testing.T) {
	var l lineBuf
	var buf strings.Builder
	plain, struck := "held belief", ""
	l.reset(40)
	l.addStruck(plain, tokens.TextPrimary, true)
	struck = l.emit(&buf, tokens.TrueColor, tokens.FocusNormal, 40, false, tokens.Ground)
	if got, want := blocks.Width(struck), blocks.Width(plain); got != want {
		t.Fatalf("struck width %d, plain width %d", got, want)
	}
	if !strings.ContainsRune(struck, strikeOverlay) {
		t.Fatal("no strike applied")
	}
	// A profile with no colour gets the words and no combining marks, because a
	// terminal that would not admit to colour is the one most likely to draw
	// them as garbage.
	l.reset(40)
	l.addStruck(plain, tokens.TextPrimary, false)
	if out := l.emit(&buf, tokens.NoColor, tokens.FocusNormal, 40, false, tokens.Ground); strings.ContainsRune(out, strikeOverlay) {
		t.Fatalf("uncoloured profile got a strike: %q", out)
	}
}

// A row that is gone says so. A pane that went blank when the list moved under
// the cursor would look like a bug in the surface, and it is not one.
func TestAMissingRowSaysSoRatherThanGoingBlank(t *testing.T) {
	v := NewView(nil)
	for _, h := range All() {
		lines := v.Render(rich(), Selection{Home: h, Row: "no-such-row"}, 60, 10)
		if len(lines) == 0 {
			t.Fatalf("%v: blank pane for a missing row", h)
		}
		if !strings.Contains(strings.Join(lines, "\n"), "gone") {
			t.Fatalf("%v: %q does not say the row is gone", h, lines)
		}
	}
}

// 12.9.2, as a regression at its own magnitude: a charter whose per-run cost is
// unmeasured must SAY it is unmeasured. The failure this guards is the real one
// — "$20.00 a run" against a measured $0.0017 — and its smaller sibling, a real
// $0.0017 printed as "$0.00", which reads as free.
func TestAnUnmeasuredRateSaysSoAndAMeasuredOneKeepsItsPrecision(t *testing.T) {
	if got := charterCost(Charter{}); !strings.Contains(got, "not measured") {
		t.Fatalf("unmeasured rate rendered %q", got)
	}
	got := charterCost(Charter{CostPerRun: 0.0017, HasCost: true})
	if strings.Contains(got, "$0.00 ") || got == "$0.00 a run" {
		t.Fatalf("a real fraction of a cent rendered as free: %q", got)
	}
	if !strings.Contains(got, "a run") {
		t.Fatalf("rate lost its unit: %q", got)
	}
}

// The truncation law at the log tail (12.5.2): a window that dropped lines says
// how many, with the cut mark and not an ellipsis. The two glyphs mean
// different things — "this stopped and should not have" against "there is more,
// ask for it" — and a log tail is the second.
func TestATruncatedLogTailSaysWhatItDropped(t *testing.T) {
	v := NewView(nil)
	lines := v.Render(rich(), Selection{Home: HomeServices, Row: ServiceRowPrefix + "sv1"}, 80, 40)
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, "4812 earlier lines") {
		t.Fatalf("dropped count missing:\n%s", out)
	}
	if !strings.Contains(out, tokens.GlyphCut) {
		t.Fatalf("cut mark missing:\n%s", out)
	}
}

// The verb strip is the registry's, and a disabled verb keeps its place. The
// reason is shown once, because the common case is one reason disabling every
// verb at the same moment — a visitor window — and printing it four times would
// spend the whole line saying it.
func TestDisabledVerbsKeepTheirPlaceAndStateOneReason(t *testing.T) {
	v := NewView(nil)
	lines := v.Render(rich(), Selection{Home: HomeStanding, Row: CharterRowPrefix + "ch2"}, 80, 40)
	out := strings.Join(lines, "\n")
	for _, want := range []string{"pause", "retire", "visitor window"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q missing from:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "visitor window"); n != 1 {
		t.Fatalf("reason repeated %d times:\n%s", n, out)
	}
}

// A row id is never rendered (5.14). The ids in the fixture are deliberately
// unmistakable strings so the assertion is exact rather than a heuristic.
func TestRowIdsNeverReachTheFrame(t *testing.T) {
	state := rich()
	state.Notebook.Beliefs[0].ID = "ZZBELIEFIDZZ"
	state.Standing.Charters[0].ID = "ZZCHARTERIDZZ"
	state.Services.Services[0].ID = "ZZSERVICEIDZZ"
	v := NewView(nil)
	for _, sel := range selections(state) {
		out := strings.Join(v.Render(state, sel, 120, 60), "\n")
		for _, id := range []string{"ZZBELIEFIDZZ", "ZZCHARTERIDZZ", "ZZSERVICEIDZZ"} {
			if strings.Contains(out, id) {
				t.Fatalf("%v: id %q rendered:\n%s", sel, id, out)
			}
		}
	}
}

// The zero state renders honestly: four rooms with nothing in them, each
// teaching what would put something there (5.22 rule 6), and never a blank
// pane.
func TestTheZeroStateTeachesRatherThanGoingBlank(t *testing.T) {
	v := NewView(nil)
	for _, h := range All() {
		out := strings.Join(v.Render(State{}, Selection{Home: h}, 70, 20), "\n")
		if strings.TrimSpace(out) == "" {
			t.Fatalf("%v: empty room renders nothing", h)
		}
		if !strings.Contains(out, h.Word()) {
			t.Fatalf("%v: room does not name itself:\n%s", h, out)
		}
	}
}

// A missing clock drops relative times rather than dating the frame from the
// Unix epoch (10.2.8: a number that has not arrived and a number that is zero
// are different facts).
func TestAMissingClockDropsAgesRatherThanInventingThem(t *testing.T) {
	state := rich()
	state.Now = time.Time{}
	v := NewView(nil)
	out := strings.Join(v.Render(state, Selection{Home: HomeNotebook, Row: BeliefRowPrefix + "41"}, 80, 30), "\n")
	if strings.Contains(out, "learned") {
		t.Fatalf("an age was rendered with no clock:\n%s", out)
	}
}
