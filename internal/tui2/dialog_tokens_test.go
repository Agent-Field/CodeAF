package tui2_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// filler is a pane that fills its whole rectangle with one harmless character,
// so the only structure left in a frame is the structure the SHELL drew. The
// skeleton's placeholders no longer draw anything a rule could be confused with
// (§16 BORDERS; see placeholder.go), but the panes are filled anyway: a test
// that measured the shell's own hairline through whatever a placeholder happened
// to be drawing that wave would pass, or fail, for the wrong reason.
type filler struct{}

func (filler) Render(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	rows := make([]string, h)
	for i := range rows {
		rows[i] = strings.Repeat("x", w)
	}
	return strings.Join(rows, "\n")
}

func fillEveryPane(s *tui2.Shell) {
	for _, id := range []tui2.LayerID{
		tui2.LayerTranscript, tui2.LayerRail, tui2.LayerComposer,
		tui2.LayerStatus, tui2.LayerOverlay,
	} {
		s.SetPane(id, filler{})
	}
}

// hairlineRuns returns the length of the longest run of the hairline character
// on every line that carries one.
func hairlineRuns(frame string) []int {
	var runs []int
	for _, line := range strings.Split(frame, "\n") {
		longest, run := 0, 0
		for _, r := range line {
			if string(r) == tokens.GlyphTreeDash {
				run++
				if run > longest {
					longest = run
				}
				continue
			}
			run = 0
		}
		if longest > 0 {
			runs = append(runs, longest)
		}
	}
	return runs
}

// The dialog's boundary is drawn from the glyph vocabulary (5.17), and the root
// tui2 package cannot import tokens to read the constant without inverting the
// tokens → tui2 dependency the SEAM note in metrics.go documents — the same bind
// DefaultMetrics is in with the two fullscreen breakpoints, and the same answer:
// restate the literal, and pin it from outside.
//
// The test drives the real shell rather than reaching for the constant, so what
// it pins is what a terminal receives: a floating dialog arrives with a rule
// above and below it, drawn in the character 5.13 writes a room boundary in, and
// with nothing down its sides — two hairlines at a boundary, not the
// box-drawing frame 5.21's anti-catalog refuses.
func TestDialogChromeDrawsTheTokenHairline(t *testing.T) {
	s := tui2.NewShell(tui2.Options{Terminal: &tui2.TerminalOptions{}})
	fillEveryPane(s)
	s.SetOverlay(true)
	frame := s.Frame(120, 32)

	runs := hairlineRuns(frame)
	if len(runs) != 2 {
		t.Fatalf("a floating dialog should be ruled on exactly two rows, got %d (%v) — reconcile dialogHairline in dialog.go with tokens.GlyphTreeDash %q:\n%s",
			len(runs), runs, tokens.GlyphTreeDash, frame)
	}
	if runs[0] != runs[1] {
		t.Fatalf("the two rules disagree about width: %v", runs)
	}
	if runs[0] < 8 {
		t.Fatalf("rule of %d cells is not a boundary: %v", runs[0], runs)
	}
}

// Under the fullscreen doors the frame's own edge is the boundary, so there is
// no margin and nothing to rule (10.4.17).
func TestFullscreenDialogDrawsNoMargin(t *testing.T) {
	s := tui2.NewShell(tui2.Options{Terminal: &tui2.TerminalOptions{}})
	fillEveryPane(s)
	s.SetOverlay(true)
	frame := s.Frame(tokens.DialogFullscreenBelowWidth-1, 40)
	if runs := hairlineRuns(frame); len(runs) != 0 {
		t.Fatalf("a fullscreen dialog drew a margin rule it has no room for %v:\n%s", runs, frame)
	}
}
