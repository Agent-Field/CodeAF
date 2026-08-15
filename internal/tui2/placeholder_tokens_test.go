package tui2_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The skeleton's own marks, pinned from outside.
//
// placeholder.go restates two vocabulary bytes as literals for the reason
// dialog.go's SEAM note gives: this root package cannot import tokens without
// inverting the tokens → tui2 dependency. The restatements are therefore pinned
// here, the same way TestDialogChromeDrawsTheTokenHairline pins the hairline —
// through the real shell rather than through the constant, so what the test
// holds is what a terminal receives.

// The status line's separator is the vocabulary's one separator (5.17's `·`),
// not a second dot that happens to look like it.
func TestStatusLineIsPunctuatedFromTheVocabulary(t *testing.T) {
	s := tui2.NewShell(tui2.Options{Terminal: &tui2.TerminalOptions{}})
	frame := s.Frame(120, 30)
	want := " " + tokens.GlyphSeparator + " "
	if !strings.Contains(frame, "aforge v2"+want) {
		t.Fatalf("the status line does not separate its columns with tokens.GlyphSeparator (%q) — reconcile separatorMark in placeholder.go:\n%s",
			tokens.GlyphSeparator, frame)
	}
}

// Absent plumbing renders as the vocabulary's missing mark and never as an
// invented default (§16 EMPTINESS, 10.2.8).
func TestAbsentPlumbingRendersTheTokenMissingMark(t *testing.T) {
	s := tui2.NewShell(tui2.Options{Terminal: &tui2.TerminalOptions{}})
	frame := s.Frame(120, 30)
	if !strings.Contains(frame, "session "+tokens.GlyphMissing) {
		t.Fatalf("an unset session is not drawn with tokens.GlyphMissing (%q) — reconcile missingMark in placeholder.go:\n%s",
			tokens.GlyphMissing, frame)
	}
}

// §16 BORDERS, enforced on the skeleton rather than promised for the real
// panes: the delivery card's ground and the dialog's ring are the only
// boundaries in the product, and a placeholder that framed itself would be
// teaching the anti-catalog's grammar to every surface that lands after it.
//
// The dialog's own rule is a hairline, so a run of one dash character is legal
// — a RECTANGLE is what is banned, and the vertical strokes and the four
// corners are what make one. None of them may appear in any mode, at any size.
func TestNoRegionDrawsABox(t *testing.T) {
	for _, linear := range []bool{false, true} {
		for _, size := range [][2]int{{120, 30}, {80, 24}, {40, 10}, {6, 4}, {1, 1}} {
			s := tui2.NewShell(tui2.Options{
				Linear:   linear,
				Terminal: &tui2.TerminalOptions{},
			})
			frame := s.Frame(size[0], size[1])
			for _, glyph := range []string{
				tokens.GlyphTreeBranch, tokens.GlyphTreeLast, tokens.GlyphTreeVert,
				"┌", "┐", "┘",
			} {
				if strings.Contains(frame, glyph) {
					t.Fatalf("the skeleton drew box art (%q) at %dx%d (linear=%v):\n%s",
						glyph, size[0], size[1], linear, frame)
				}
			}
		}
	}
}
