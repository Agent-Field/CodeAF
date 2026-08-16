package blocks_test

// The twin pins for the marks blocks has to spell itself.
//
// The vocabulary authority for every glyph in this tree is internal/tui2/tokens,
// and blocks cannot import it: the edge runs tokens → blocks so blocks stays a
// leaf. A mark blocks draws is therefore spelled twice — once as the
// vocabulary's authority there, once as a byte here — and only a test can hold
// the two spellings equal.
//
// tokens' own glyphvocab_test.go carries the older half of this seam
// (TestBlocksTwinsAreOneMark, for the accent edge and the body indent). This
// file is the blocks-side half, and it is a blocks_test package for the obvious
// reason: an external test binary may import tokens without blocks itself
// importing it, so the leaf stays a leaf and the pins still fail on drift.
//
// The rule for anything added here: if blocks writes a byte that the tokens
// table also names, it gets a line in this file the same day.

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// TestBlockMarksMatchTheVocabulary pins every mark blocks spells to the slot
// tokens declares for it. A drift here is two marks for one meaning, which is
// exactly the failure the glyph audit found and the twins exist to prevent.
func TestBlockMarksMatchTheVocabulary(t *testing.T) {
	for _, pin := range []struct {
		what   string
		token  string
		blocks string
	}{
		{"the overflow ellipsis (§16's ONE ELLIPSIS GRAMMAR)", tokens.GlyphEllipsis, blocks.OverflowMark},
		{"the rule stroke", tokens.GlyphTreeDash, blocks.RuleMark},
		{"the telemetry separator", tokens.GlyphSeparator, blocks.SeparatorMark},
		{"the expanded chevron", tokens.GlyphExpanded, blocks.ExpandedMark},
		{"the collapsed chevron", tokens.GlyphCollapsed, blocks.CollapsedMark},
		{"the cut mark", tokens.GlyphCut, blocks.CutMark},
		{"the accent edge", tokens.GlyphAccentRail, blocks.AccentEdge},
	} {
		if pin.token != pin.blocks {
			t.Errorf("two vocabularies for %s: tokens %q, blocks %q",
				pin.what, pin.token, pin.blocks)
		}
	}
}

// TestTheSpinnerIsTheHouseSpinner pins the whole cycle, frames and order and
// count alike.
//
// The count is pinned on purpose. 5.21's ◐◓◑◒ was four frames and the braille
// cycle is ten, so anything that latched onto "four" while the old set shipped
// — a cadence, a modulus, a golden — is a bug this catches rather than a
// mystery somebody debugs at a CJK terminal. blocks.Clock.Glyph derives its
// index from len(Spinner) for the same reason.
func TestTheSpinnerIsTheHouseSpinner(t *testing.T) {
	if len(blocks.Spinner) != len(tokens.SpinnerFrames) {
		t.Fatalf("two spinner cycles: tokens has %d frames, blocks has %d",
			len(tokens.SpinnerFrames), len(blocks.Spinner))
	}
	for i, frame := range tokens.SpinnerFrames {
		if blocks.Spinner[i] != frame {
			t.Errorf("spinner frame %d parted: tokens %q, blocks %q",
				i, frame, blocks.Spinner[i])
		}
	}
}

// TestTheSpinnerNeverChangesWidthMidSpin is the measured half of the amendment
// the spinner's comment states: the reason the cycle is braille and not 5.21's
// circles is that ◐ and ◑ are East_Asian_Width=Ambiguous and ◓ and ◒ are not,
// so a row drawn from that set is one cell wide on one frame and two on the
// next. Every frame here has to measure the same, through the same ruler the
// renderer lays rows out with.
func TestTheSpinnerNeverChangesWidthMidSpin(t *testing.T) {
	want := blocks.Width(blocks.Spinner[0])
	if want != 1 {
		t.Fatalf("spinner frame 0 (%q) is %d cells, not 1", blocks.Spinner[0], want)
	}
	for _, frame := range blocks.Spinner {
		if got := blocks.Width(frame); got != want {
			t.Errorf("spinner frame %q is %d cells where frame 0 is %d: "+
				"a spinning row would change width mid-spin and everything "+
				"right of it would dance", frame, got, want)
		}
	}
}
