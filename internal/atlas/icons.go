package atlas

import (
	"os"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// SetGlyphSet lets an enclosing chat supply its already chosen repertoire.
func (m *Model) SetGlyphSet(set tokens.GlyphSet) { m.icons = set }

// icon is the map's single door to the shared vocabulary.
func (m *Model) icon(id tokens.GlyphID) string { return m.icons.Glyph(id) }

// mark resolves the same slot for the canvas, whose cells hold one rune.
func (m *Model) mark(id tokens.GlyphID) rune {
	r, _ := utf8.DecodeRuneInString(m.icon(id))
	return r
}

// terminalGlyphs folds the Display choice over the shared terminal detection.
func terminalGlyphs() tokens.GlyphSet {
	switch config.IconsAt(config.ProfileDir()) {
	case config.IconsRich:
		return tokens.NerdFont
	case config.IconsPlain:
		return tokens.Plain
	}
	set, _ := tokens.DetectGlyphSet(os.Getenv)
	return set
}
