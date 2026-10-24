package atlas

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// TestAtlasUsesTheChosenRepertoire proves C16: map chrome and arrowheads
// follow the same terminal tier, including the screen reader's ASCII marks.
func TestAtlasUsesTheChosenRepertoire(t *testing.T) {
	for _, set := range []tokens.GlyphSet{tokens.Plain, tokens.NerdFont, tokens.ASCII} {
		m := newTestModel(t, 120, 35)
		m.SetGlyphSet(set)
		m.flow = 0
		frame := m.Frame()
		for _, slot := range []tokens.GlyphID{tokens.GActionPlan, tokens.GMapLeft, tokens.GQueuePill} {
			if !strings.Contains(frame, set.Glyph(slot)) {
				t.Errorf("%s omits slot %d", set, slot)
			}
		}
	}
}
