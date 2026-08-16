package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/golden"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The composer seam (§16 SURFACE SEAMS ARE GROUNDS, NOT STROKES).
//
// What these hold is one sentence: the fixed bottom strip has a floor of its
// own, so the transcript slides UNDER it instead of stopping at nothing. The
// failure they exist to catch has two shapes, and they are opposite shapes — a
// strip that draws no ground where it could, and a strip that draws SOMETHING
// where it has no ground to draw. The second is the one that costs the most:
// the obvious repair for a missing boundary is a hairline, and §16's RULED
// LINES admits exactly two hairlines in the product, neither of them here.

// seamParams strips an SGR sequence down to its parameters, so a background can
// be found in a frame whether it was written on its own or merged with a
// foreground by the compositor. An empty sequence stays empty.
func seamParams(sgr string) string {
	return strings.TrimSuffix(strings.TrimPrefix(sgr, "\x1b["), "m")
}

// seamProfiles is the whole degradation ladder, because the answer differs at
// every rung and the rungs that draw nothing are as load-bearing as the ones
// that draw a plane.
var seamProfiles = []tokens.Profile{tokens.NoColor, tokens.ANSI16, tokens.ANSI256, tokens.TrueColor}

// TestTheSeamIsAGroundAtEveryProfile: where the profile has a trustworthy
// raised background the strip carries it on every cell of every row; where it
// has none the strip is returned exactly as it was composed.
func TestTheSeamIsAGroundAtEveryProfile(t *testing.T) {
	const width, height = 40, 3
	view := "one\ntwo"
	for _, profile := range seamProfiles {
		style := tokens.NewStyler(profile, tokens.FocusNormal)
		got := seamStrip(style, view, width, height, tokens.HugGroundBar)
		if !profile.SheetGround() {
			if got != view {
				t.Errorf("%v: the seam painted where it has no ground: %q", profile, got)
			}
			continue
		}
		ground := tokens.HugGroundBar.Bg(profile, tokens.FocusNormal)
		rows := strings.Split(got, "\n")
		if len(rows) != height {
			t.Errorf("%v: strip is %d rows, want %d", profile, len(rows), height)
			continue
		}
		for i, row := range rows {
			if !strings.Contains(row, ground) {
				t.Errorf("%v row %d carries no seam ground: %q", profile, i, row)
			}
			if n := blocks.Width(row); n != width {
				t.Errorf("%v row %d is %d cells, want %d", profile, i, n, width)
			}
		}
	}
}

// TestTheSeamDrawsNoRuleWhereItHasNoGround is the honest-degradation half, and
// it is the one worth stating out loud: at 16 colours and at none the strip
// draws NOTHING. A fallback hairline would be the school-notebook mark §16
// forbids, invented on exactly the profiles least able to afford another idiom
// — there the blank row the region already leaves above the draft is the whole
// seam, the same way the dialog's ring keeps its shape and drops its ground.
func TestTheSeamDrawsNoRuleWhereItHasNoGround(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI16} {
		style := tokens.NewStyler(profile, tokens.FocusNormal)
		got := seamStrip(style, "verbs", 40, 2, tokens.HugGroundBar)
		if got != "verbs" {
			t.Errorf("%v: the seam changed the strip to %q", profile, got)
		}
		if strings.Contains(got, tokens.GlyphTreeDash) || strings.Contains(got, "─") {
			t.Errorf("%v: the seam fell back to a rule line", profile)
		}
	}
	if got := seamStrip(nil, "verbs", 40, 1, tokens.HugGroundBar); got != "verbs" {
		t.Errorf("a seam with no styler painted: %q", got)
	}
}

// TestTheSeamKnowsEveryResetThatClearsIt is the same claim it always made, now
// asked of [tokens.GroundResets] instead of a restatement in this file. The
// list moved to the package that WRITES the bytes, which is the only place it
// can be right by construction rather than right by a guard.
func TestTheSeamKnowsEveryResetThatClearsIt(t *testing.T) {
	style := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	resets := tokens.GroundResets(tokens.TrueColor)
	if len(resets) == 0 {
		t.Fatal("a colour profile reports no ground-clearing resets at all")
	}

	// A banded run — the caret cell, a selected row, the current tab's pill —
	// resets both layers, so the seam has to put its floor back after it.
	banded := style.PaintOn("x", tokens.TextPrimary, tokens.Band)
	found := false
	for _, reset := range resets {
		if strings.Contains(banded, reset) {
			found = true
		}
	}
	if !found {
		t.Errorf("a banded run resets with a sequence the seam does not know: %q", banded)
	}

	// An ordinary tier run resets the foreground alone and keeps whatever ground
	// it was drawn over, which is what makes the common row free.
	plain := style.PaintToken("x", tokens.TextTertiary)
	for _, reset := range resets {
		if strings.Contains(plain, reset) {
			t.Errorf("a tier run carries %q, so the seam rewrites every row: %q", reset, plain)
		}
	}
	// A profile that writes no colour writes nothing to repair.
	if got := tokens.GroundResets(tokens.NoColor); len(got) != 0 {
		t.Errorf("the no-colour profile reports resets to repair: %q", got)
	}
}

// TestTheSeamKeepsItsFloorAcrossAnInnerReset is [blocks.CardBlock]'s own
// objection, answered: a ground wrapped around already-painted text is undone
// by the first inner reset, so the seam re-asserts it after every one. The
// composer's caret is the real row this protects — a strip that lost its floor
// at the caret would be a surface that changes plane where the cursor happens
// to be.
func TestTheSeamKeepsItsFloorAcrossAnInnerReset(t *testing.T) {
	style := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	ground := tokens.HugGroundInput.Bg(tokens.TrueColor, tokens.FocusNormal)
	view := style.PaintToken("say", tokens.TextTertiary) +
		style.PaintOn(" ", tokens.TextPrimary, tokens.Band) +
		style.PaintToken("something", tokens.TextTertiary)

	got := seamStrip(style, view, 40, 1, tokens.HugGroundInput)
	caret := strings.Index(got, "\x1b[39;49m")
	if caret < 0 {
		t.Fatalf("the banded cell lost its own reset: %q", got)
	}
	if !strings.Contains(got[caret:], ground) {
		t.Errorf("the seam does not resume after the caret: %q", got[caret:])
	}
	if n := blocks.Width(got); n != 40 {
		t.Errorf("the regrounded row is %d cells, want 40", n)
	}
}

// TestTheSeamFillsTheRectangleItWasGiven: a strip that returns fewer rows than
// it was handed shows the transcript through the hole (12.13's ghost, one plane
// up), and a row that stops at its last letter is a highlight rather than a
// plane.
func TestTheSeamFillsTheRectangleItWasGiven(t *testing.T) {
	style := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	for _, height := range []int{1, 2, 3, 5} {
		got := strings.Split(seamStrip(style, "one", 24, height, tokens.HugGroundBar), "\n")
		if len(got) != height {
			t.Errorf("height %d: strip is %d rows", height, len(got))
			continue
		}
		for i, row := range got {
			if n := blocks.Width(row); n != 24 {
				t.Errorf("height %d row %d is %d cells, want 24", height, i, n)
			}
		}
	}
	// Degenerate rectangles are ordinary inputs during a resize: the seam draws
	// less rather than failing.
	for _, size := range [][2]int{{0, 1}, {24, 0}, {-4, 2}, {24, -1}} {
		if got := seamStrip(style, "one", size[0], size[1], tokens.HugGroundBar); got != "one" {
			t.Errorf("%dx%d: the seam painted %q", size[0], size[1], got)
		}
	}
}

// TestTheContextualLineStandsOnTheSeam is the frame-shaped half: the surface is
// driven the way the golden harness drives it, and the bottom row of the real
// frame is asked what it is standing on.
//
// It asserts the ROW and not the strip, because the composer region above it
// joins the same floor through the same [seamStrip] call from its own Render —
// one law, two callers, and this is the one that lives in this file.
func TestTheContextualLineStandsOnTheSeam(t *testing.T) {
	const width, height = 80, 24
	for _, profile := range seamProfiles {
		backend := &fakeBackend{}
		dressedThread(backend)
		app := goldenApp(backend, profile, golden.Theme{Mode: golden.Dark})
		drivePoll(app)
		rows := strings.Split(app.Frame(width, height), "\n")
		if len(rows) != height {
			t.Fatalf("%v: frame is %d rows, want %d", profile, len(rows), height)
		}
		last := rows[height-1]
		if n := ansi.StringWidth(last); n != width {
			t.Errorf("%v: the contextual line is %d cells, want %d", profile, n, width)
		}
		// The ground is looked for by its PARAMETERS rather than as a whole
		// sequence: the compositor re-serializes cells, so a foreground and a
		// background that arrived as two escapes leave as one merged SGR. A test
		// that matched the standalone sequence would pass on the blank cells and
		// fail the day a painted word reached the edge of the row.
		ground := seamParams(tokens.HugGroundBar.Bg(profile, tokens.FocusNormal))
		switch {
		case profile.SheetGround():
			if !strings.Contains(last, ground) {
				t.Errorf("%v: the contextual line has no floor: %q", profile, last)
			}
		case profile == tokens.NoColor:
			if strings.Contains(last, "\x1b") {
				t.Errorf("%v: the contextual line emitted escapes: %q", profile, last)
			}
		default:
			// At 16 colours a foreground is 30-37 or 90-97, so any `\x1b[4…` on
			// the row is a background being set or given back — and the seam is
			// the only thing on this surface that would write one.
			if strings.Contains(last, "\x1b[4") {
				t.Errorf("%v: the contextual line painted a ground it cannot trust: %q", profile, last)
			}
		}
	}
}
