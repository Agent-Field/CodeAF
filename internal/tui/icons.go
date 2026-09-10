package tui

import (
	"os"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── WHERE V1 GETS ITS MARKS ──────────────────────────────────────────────────
//
// THE SAME PLACE EVERY OTHER SURFACE DOES. Every state, action and attention
// mark this window draws is a SLOT in internal/tui2/tokens' vocabulary, and the
// three spellings of each — the Font Awesome 4 icon a patched font draws, the
// geometric floor every terminal draws, and the one ASCII character a screen
// reader can name — are that table's (docs/design/icons/DESIGN.md). This file
// holds the map from this window's meanings to slots, and nothing else.
//
// The law is product-wide rather than v3's: one terminal shows ONE tier, so the
// person who patched a font sees icons in the chat, in this window and on the
// resident's pages, and the person who did not sees the designed geometric floor
// in all three. A mark spelled as a literal draws that floor forever whatever
// the terminal can do, which is exactly the bug the law was written after
// finding — icons beside the tool calls and bare shapes beside the tasks, on one
// screen. [TestNoV1SurfaceSpellsAnIconItself] fails the build on one.
//
// V1 IS THE VISUAL NORTH STAR AND STAYS ONE. Nothing here adds a mark, moves a
// mark or paints one a new colour: the hue axis is untouched (the mint, rose,
// peach and butter styles below every call site are exactly what they were), and
// a shape changes only where the shared vocabulary already had a different
// character for the meaning this window was drawing.

// iconSet is WHERE THE TIER IS DECIDED, and it is decided once, from the same
// two inputs internal/tui3 folds together in app.iconSet: the person's Display
// row (`step icons`: auto · rich · plain) when they have said something, and
// [tokens.DetectGlyphSet]'s answer when they have not.
//
// [Model.iconAuto] is deliberately the zero value — [tokens.Plain] — until the
// door that opens a real terminal settles it ([RunWithCommander]). A window
// built by an embedder or a test has no terminal to detect and gets the designed
// floor, which is the tier this surface has drawn since it was written.
func (m *Model) iconSet() tokens.GlyphSet {
	switch m.iconMode {
	case config.IconsRich:
		return tokens.NerdFont
	case config.IconsPlain:
		return tokens.Plain
	}
	return m.iconAuto
}

// settleIcons puts the tier onto the model, and it is the ONE assignment.
// [Model.icon] reads it there, so two rows of one frame cannot come out of two
// different repertoires.
//
// AND IT DROPS EVERY CACHE OF ALREADY-DRAWN LINES, because the repertoire is the
// one thing a cached block depends on that its key does not say. A settled
// message, a card, a brief and the help modal are all kept as the bytes they
// rendered to, so a person who changes the Display row would otherwise watch the
// new tier arrive on the live tail while the conversation above it stayed in the
// old one — a screen showing both answers at once, which is precisely the
// mixed-repertoire frame this law exists to prevent. [Model.renderMessages]
// already drops the thread's caches whole when the pane's width no longer
// matches, so the tier borrows that door rather than opening a second one.
func (m *Model) settleIcons() {
	m.icons = m.iconSet()
	m.helpLines = nil
	m.blockWidth = -1
	m.chatBlocksValid = false
	m.invalidateDock()
	m.graphPaneStale = true
	m.selfPaneStale = true
}

// adoptIcons re-reads the Display row and settles the tier. It runs when the
// window takes a commander (which is where a profile directory arrives) and
// again the moment the row is changed, because the two halves of one fact — the
// setting and the surface that draws by it — may not be changed apart.
func (m *Model) adoptIcons() {
	m.iconMode = config.IconsAuto
	if m.settingsRegistry != nil {
		if row, ok := m.settingsRegistry.Row(config.KeyIcons); ok {
			m.iconMode = row.Value()
		}
	}
	m.settleIcons()
}

// detectIcons is the door's half: what the terminal itself can be trusted with.
// It is called once, by [RunWithCommander], because that is the only place in
// this package that knows a real terminal is on the other end.
func (m *Model) detectIcons(env tokens.Env) {
	m.iconAuto, _ = tokens.DetectGlyphSet(env)
	m.settleIcons()
}

// icon resolves a slot under this window's tier. It is the ONLY door: no drawing
// site in this package spells a mark for itself.
func (m *Model) icon(id tokens.GlyphID) string { return m.icons.Glyph(id) }

// osEnv is [os.Getenv] as a [tokens.Env], so the detector's pure-function shape
// survives all the way to the door.
func osEnv(name string) string { return os.Getenv(name) }
