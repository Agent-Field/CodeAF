package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// A BLOCKQUOTE'S GUTTER IS NOT A RULE, BECAUSE THIS SURFACE ALREADY HAS ONE.
//
// The quote's bar was `│` — the box-drawing rule the margin column's divider is
// made of — and it sat at the LEFT margin of the feed while that divider ran
// down the RIGHT of the very same rows. On a screen with no other vertical
// rules, two `│` columns meaning unrelated things read as one broken frame. The
// fence beside it already owned the mark for "a block set apart": an eighth of a
// cell, which is a margin and cannot be mistaken for a border.
func TestAQuotedPassageIsMarkedWithAMarginAndNotWithARule(t *testing.T) {
	const src = "> a quoted sentence long enough that it has to wrap somewhere\n"
	rows := mdPlainRows(t, src, 60)
	joined := strings.Join(rows, "\n")

	// The bar is on the frame at all — so a quote that lost its gutter
	// altogether cannot pass this by drawing neither mark.
	if !strings.Contains(joined, tokens.GlyphCodeGutter) {
		t.Fatalf("the quote drew no gutter at all, want %q down its left:\n%s", tokens.GlyphCodeGutter, joined)
	}
	// AND IT IS NOT THE RULE. `railCont` is this surface's own vertical rule and
	// carries the same byte the margin divider does, so naming it here says what
	// the quote must not collide with rather than repeating a byte.
	rule := strings.TrimSpace(railCont)
	if strings.Contains(joined, rule) {
		t.Fatalf("the quote drew %q, which is the byte this surface's vertical rules are made of; want the hairline %q:\n%s",
			rule, tokens.GlyphCodeGutter, joined)
	}
	for _, row := range rows {
		if strings.HasPrefix(strings.TrimSpace(row), rule) {
			t.Fatalf("a quoted row hangs off %q, the frame's own rule:\n%s", rule, joined)
		}
	}
}

// AND THE SLOT IS STILL A SLOT. The prose glyphs are named meanings over bytes
// the vocabulary already ships, and the quote's byte moved from the spawn tree's
// trunk to the code fence's gutter — a slot with no twin at all would be a mark
// that escapes the width sweep in tokens' own glyph table.
func TestTheQuoteBarSharesTheFencesGutterAndNotTheTreesTrunk(t *testing.T) {
	if tokens.GlyphProseQuote == tokens.GlyphTreeVert {
		t.Fatalf("the blockquote gutter is %q, the spawn tree's trunk and every vertical rule's byte; want the fence's %q",
			tokens.GlyphProseQuote, tokens.GlyphCodeGutter)
	}
	if tokens.GlyphProseQuote != tokens.GlyphCodeGutter {
		t.Fatalf("the blockquote gutter is %q and the fence's is %q; one meaning, one byte, or the width sweep loses one of them",
			tokens.GlyphProseQuote, tokens.GlyphCodeGutter)
	}
}
