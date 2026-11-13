package tokens

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// L4-H8a, L4-H8d: Every platform slot has three single-cell spellings without depending on removed design documents.
func TestL4B2PlatformVocabulary(t *testing.T) {
	if NerdFont.Upgrade(GlyphPlatformMac) != GlyphPlatformMac {
		t.Fatal("the Command key label became a platform icon")
	}
	t.Run("complete vocabulary", TestVocabularyIsCompleteAndFallsBackToFiveSeventeen)
	t.Run("single cell", TestEveryBindingMeasuresOneCellInBothTiers)
	t.Run("distinct meanings", TestOneGlyphOneMeaning)
	t.Run("pinned release", TestProvenance)
	for _, id := range []GlyphID{GPlatformMac, GPlatformLinux, GPlatformWindows, GPlatformMobile, GPlatformOther} {
		binding, ok := bindingOf(id)
		if !ok || binding.NFName == "" || binding.ASCII == "" {
			t.Fatalf("incomplete binding: %+v", binding)
		}
		for _, tier := range []GlyphSet{Plain, NerdFont, ASCII} {
			mark := tier.Glyph(id)
			if mark == "$" || ansi.StringWidth(mark) != 1 || runewidth.StringWidth(mark) != 1 {
				t.Fatalf("invalid mark %q", mark)
			}
		}

	}
}
