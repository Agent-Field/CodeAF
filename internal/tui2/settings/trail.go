package settings

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The drill trail, arriving on the settings sheet.
//
// internal/tui2/palette/trail.go states the grammar and the argument for it;
// this is the third surface to join it (the model picker was the second), and
// it joined because of the same report that produced the first one: a reader
// who reached settings THROUGH the palette had no way back. Esc threw the whole
// flow away, clicking did nothing, and there was no key and no target that
// returned to the list they had come from. A surface you can enter and not
// leave is a trap regardless of how good the surface is.
//
// THE GRAMMAR, unchanged from the palette's, because the whole point is that it
// is one grammar:
//
//   - THE TRAIL is where you are and the way back, on the line that already
//     names the surface. Every ancestor word is a click target; the current step
//     — `settings` — is a label and is not.
//   - BACKSPACE on an empty query ascends one rung. With text in the field it
//     erases a letter, unchanged, and while a row's own editor is open it
//     belongs to that editor: a key that sometimes deleted a character and
//     sometimes threw away the screen would make typing feel dangerous.
//   - ESC closes the whole flow from any depth. It does NOT unwind a rung —
//     8.2.21 is that esc acts on what you are watching, and what a reader
//     watching a drilled sheet wants gone is the sheet. The sheet's own esc
//     ladder (an open editor, then a running search) is unchanged and sits
//     UNDER this: every rung of it is about unsubmitted state on this screen,
//     and none of them is navigation.
//
// A sheet opened on its own — alt+, , the slash row — has no trail, draws the
// header it always drew, and answers backspace exactly as it did. The back
// grammar is a property of how you GOT here, which is a fact only the wiring
// has, so it is installed by the wiring and never inferred.

// trailLead opens each ancestor step. It is internal/tui2/palette's own mark,
// respelled here rather than imported — the same trade internal/tui2/modelui
// made and for the same reason: importing that package would drag its rail and
// registry reads behind a surface that wanted one glyph. trail_test.go pins the
// two spellings equal so the path cannot change its mark halfway.
const trailLead = tokens.GlyphPromptChat + " "

// TrailWord is this surface's word on the trail — the same word the standalone
// header has always drawn, so the sheet is called one thing whichever door
// opened it.
//
// It is exported because the drill does not stop here: a model picker raised
// from one of this sheet's model rows is one rung deeper and wears this word as
// its own ancestor. Written down once, so the word a reader clicks to come back
// cannot drift from the word they see at the top of the sheet.
const TrailWord = "settings"

const selfStep = TrailWord

// trailStep is one drawn ancestor: which rung a click on it returns to, and the
// cells it occupied in the last render. The columns are RECORDED at paint time
// rather than recomputed, because a hit test that measured the string again
// would be a second opinion about where the words are.
type trailStep struct {
	depth int
	at    int
	w     int
}

// headerLine is the row the trail is drawn on. It is named because the pointer
// has to find it and [Model.Render] has to draw it, and a 0 written twice is a
// 0 that can drift.
const headerLine = 0

// SetTrail installs the path this sheet was reached THROUGH, and the door back
// out of it. The wiring calls it on every raise, with a nil trail for a sheet
// opened on its own.
//
// The trail is COPIED: a caller reusing its own slice between raises would
// otherwise be rewriting a path this surface is still drawing.
func (m *Model) SetTrail(trail []string, onBack func(depth int) tea.Cmd) {
	m.trail = append(m.trail[:0], trail...)
	m.onBack = onBack
}

// Trail is the path above this sheet, for a caller that wants to read back what
// it installed. It never includes [selfStep] — that is where the reader is, not
// a rung anybody can return to.
func (m *Model) Trail() []string { return m.trail }

// addTrail writes the path at the head of the header and records where each
// ancestor landed, so a click can be turned back into a rung.
//
// With nothing above it the sheet draws the header it always drew: `‹ settings`,
// the scope line, no targets — a one-step path is a label pretending to be
// navigation.
//
// The TIERS split the steps by what they are (5.22's checklist: an interactive
// chip may never live permanently in the dimmest tier). Every ancestor is a door
// and sits at the secondary tier; `settings` is where you already are, opens
// nothing, and keeps the primary tier it has always had, because it is also the
// title of the screen.
func (m *Model) addTrail(line *lineBuf) {
	m.steps = m.steps[:0]
	if len(m.trail) == 0 {
		line.add(tokens.GlyphScopeUp+" ", tokens.TextTertiary)
		line.add(selfStep, tokens.TextPrimary)
		return
	}
	for depth, word := range m.trail {
		if word == "" {
			continue
		}
		line.add(trailLead, tokens.TextTertiary)
		at := line.used
		line.add(word, tokens.TextSecondary)
		// A step clipped by a narrow header is still the cells it drew, and
		// still a door on them. A step that drew nothing at all is not
		// recorded: a zero-width target is one a pointer can only hit by
		// arithmetic accident.
		if w := line.used - at; w > 0 {
			m.steps = append(m.steps, trailStep{depth: depth, at: at, w: w})
		}
		line.add(" ", tokens.TextTertiary)
	}
	line.add(trailLead, tokens.TextTertiary)
	line.add(selfStep, tokens.TextPrimary)
}

// stepAt maps a column on the header row back to the rung a click there returns
// to. The current step is deliberately not a target — clicking where you already
// are must do nothing rather than rebuild the screen under the pointer.
func stepAt(steps []trailStep, x int) (int, bool) {
	for i := range steps {
		if s := steps[i]; x >= s.at && x < s.at+s.w {
			return s.depth, true
		}
	}
	return 0, false
}

// back leaves the sheet for the rung directly above it. With nothing above, it
// does nothing and says so by returning no command — which is what lets
// backspace fall through to erasing a letter without a second test.
func (m *Model) back() tea.Cmd { return m.backTo(len(m.trail) - 1) }

// backTo leaves for a stated rung, which is what a click on an ancestor word
// means. The rung is the wiring's to raise: this package holds words and never
// learns what they open.
//
// The pending write is flushed first, for [Model.Close]'s reason: a value typed
// and then walked away from is a change the reader has already made, and the
// moment the sheet stops being on screen is the last one where it can be
// honoured.
func (m *Model) backTo(depth int) tea.Cmd {
	if m.onBack == nil || depth < 0 || depth >= len(m.trail) {
		return nil
	}
	m.Flush()
	return m.onBack(depth)
}

// Select puts the band on one registry row by key, and reports whether the sheet
// had it.
//
// It closes the REQUESTED SEAM the palette's settings rows carried — "the sheet
// has no select-this-key door, so a chosen row opens the sheet rather than the
// row". The palette's own promise is that picking a setting takes you TO it, and
// a sheet that opened at the top after a reader searched for one row was making
// them search twice.
//
// A key the sheet cannot show — filtered away by a gate, or simply not a row —
// leaves the selection where it was rather than moving it somewhere arbitrary.
func (m *Model) Select(key string) bool {
	if key == "" {
		return false
	}
	for position, index := range m.visible {
		if m.rows[index].setting.Key != key {
			continue
		}
		m.selected = position
		m.cancel()
		return true
	}
	return false
}
