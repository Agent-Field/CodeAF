package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── ONE QUIET MARK BESIDE EACH STEP ──────────────────────────────────────────
//
// The compact block draws three step titles (livesteps.go). This file draws the
// one cell in front of each of them: a still, monochrome mark saying what FAMILY
// of work that step is — searching, editing, running a command — keyed off the
// closed vocabulary the engine already carries ([session.ActionCategory]).
//
// WHAT IT IS FOR is the glance. Three sentences in a fading ramp are read left
// to right and word by word; a shape is read before any of them, so a person
// coming back to the terminal knows the work is EDITING before they have read
// which file. That is the only claim the mark makes, and it is why there is
// exactly one per step.
//
// ── FOUR RULES, AND EACH ONE IS A REFUSAL ──
//
//  1. THE MARK NEVER MOVES. The newest step's TEXT shimmers (captionmotion.go);
//     its icon does not. Liveness is already said by the shimmer under it and by
//     the existing activity state, and a second moving thing in the same
//     three rows is two answers to "what is happening now". The gutter is a
//     label, and a label that animates is a label nobody can stop looking at.
//  2. IT NEVER SAYS HOW IT WENT. There is no tick, no cross, no warning mark and
//     no colour of its own in this table — a step that failed is not covered by
//     this block at all ([liveWorkKeepsRow]), so a success mark here could only
//     ever mean "still true so far", which is a mark that says nothing. `test`
//     draws a flask (a target in plain mode) and never a checkmark for exactly this reason: the family
//     is the ACTION of checking, not its verdict.
//  3. IT COSTS A FIXED GUTTER. Every step spends [actionGutter] cells whatever
//     its family, and a caption that wraps spends the same cells as blanks on
//     its later lines, so the sentences all begin in one column and the block
//     does not ripple as steps arrive.
//  4. IT FADES WITH ITS OWN SENTENCE. The icon takes the step's fade stop, not a
//     stop of its own — the oldest step is faint icon AND faint words. A gutter
//     held at full strength over fading text would make the marks the loudest
//     thing in the block, which is the opposite of what they are for.
//
// ── WHERE THE CHARACTERS LIVE ──
//
// NOT HERE. Every mark is a SLOT in internal/tui2/tokens' vocabulary, and the
// three spellings of each — the Font Awesome 4 icon a patched font draws, the
// geometric floor every terminal draws, and the one ASCII character a screen
// reader can name — are that table's (docs/design/icons/DESIGN.md). This file
// holds the map from a family of work to a slot, and nothing else.
//
// FALLBACK IS A CAPABILITY DECISION, NOT THE DESIGN BASELINE. The existing
// tokens.DetectGlyphSet vetoes terminals and locales that need plain symbols;
// colour remains independent. A terminal cannot report its configured font,
// so Display's step icons row offers plain for missing glyphs and rich for a
// patched font on a conservatively detected terminal. Linear and ASCII modes
// keep their accessible spelling even when rich is selected.

// iconSet is WHERE THE TIER IS DECIDED, and it is decided once: the Display
// setting when a person has said something, and [tokens.DetectGlyphSet]'s
// answer when they have not. Nothing else on this surface detects — every
// drawing site asks [palette.glyph] or [app.icon] for a slot and is handed the
// character this terminal draws it as.
func (a *app) iconSet() tokens.GlyphSet {
	switch a.iconMode {
	case config.IconsRich:
		return tokens.NerdFont
	case config.IconsPlain:
		return tokens.Plain
	}
	return a.actionAuto
}

// adoptIcons settles the repertoire onto the palette. It is called at boot,
// at every turn end and the moment the Display row changes, because the two
// halves of one fact — the setting and the palette that draws by it — may not
// be changed apart.
func (a *app) adoptIcons() {
	a.iconMode = config.IconsAt(a.profileDir)
	a.pal.icons = a.iconSet()
}

// icon is [palette.glyph] with the linear tier folded in, for the app methods
// that hold the screen-reader flag themselves.
func (a *app) icon(id tokens.GlyphID) string {
	if a.linear {
		return tokens.ASCII.Glyph(id)
	}
	return a.pal.glyph(id)
}

// actionGutter is the fixed cost of the mark: the cell it stands in, and the
// space after it. It is a constant rather than a measurement because the whole
// point is that it does not depend on which family a step turned out to be.
const actionGutter = 2

// actionMarks is the map from a family to its SLOT in the shared vocabulary,
// and it is all this file holds: the characters — rich, plain and ASCII — are
// internal/tui2/tokens' (nerdfont.go's action-family block), where they are
// measured by the width gate, checked against the pinned Nerd Fonts release and
// held to the one-meaning law like every other mark on the surface.
//
// THIS TABLE USED TO BE THE CHARACTERS THEMSELVES, and that is the bug it was
// changed for: ten private-use codepoints and ten plain glyphs spelled inline
// here were a second vocabulary nothing could gate, and the task states one
// file over had a third. Four families reuse a slot the table already owned —
// searching is the search mark, editing the pencil, a command the shell prompt,
// and a thing that was not there the plus.
//
// It is exhaustive over [session.ActionCategories] — a test walks the engine's
// list and fails on a family with no mark, so the vocabulary and the gutter
// cannot drift apart.
var actionMarks = map[session.ActionCategory]tokens.GlyphID{
	session.ActionSearch:      tokens.GSearch,
	session.ActionRead:        tokens.GActionRead,
	session.ActionEdit:        tokens.GWrite,
	session.ActionCreate:      tokens.GActionCreate,
	session.ActionRun:         tokens.GShell,
	session.ActionTest:        tokens.GActionTest,
	session.ActionBrowse:      tokens.GActionBrowse,
	session.ActionTransfer:    tokens.GActionTransfer,
	session.ActionCommunicate: tokens.GActionCommunicate,
	session.ActionCoordinate:  tokens.GActionCoordinate,
	session.ActionPlan:        tokens.GActionPlan,
	session.ActionWait:        tokens.GActionWait,
	session.ActionWork:        tokens.GActionWork,
}

// actionMarkFor is the mark for one family, in this terminal's tier.
//
// AN UNKNOWN FAMILY DRAWS THE BUCKET rather than a blank. A gutter that
// sometimes vanished would move the sentences beside it, and this whole file
// exists so they do not move; a family this build has never heard of is a step,
// and a step is [session.ActionWork].
func (a *app) actionMarkFor(category session.ActionCategory) string {
	slot, known := actionMarks[category]
	if !known {
		slot = actionMarks[session.ActionWork]
	}
	return a.icon(slot)
}

// actionLead is the whole gutter for one line of a step: the mark and its space
// on the FIRST line, and [actionGutter] blanks on every line the caption wrapped
// onto.
//
// The blanks are why a wrapped caption stays in its column. They are spelled here
// rather than at the call site so the two halves of one invariant — how wide the
// gutter is, and what fills it — cannot be changed apart.
func (a *app) actionLead(category session.ActionCategory, first bool) string {
	if !first {
		return "  "
	}
	return a.actionMarkFor(category) + " "
}

// stepCategory is the family of one caption: what the narrator said if it said
// anything, and otherwise what the calls underneath it were.
//
// THE FLOOR IS FIRST AND THE MODEL ONLY REFINES IT. A batch has tool names from
// the instant its first call begins, so the mark is right from the first frame
// and the narrator's answer — which arrives half a second later at the earliest
// — replaces it in the same repaint that replaces the sentence. There is no
// moment with no mark, and no second mark drawn for the same step.
//
// AND IT IS PURE OVER THE ENTRY LIST, which is what makes a repaint and a
// reopening draw the same thing. Nothing here reads a clock, a random source or
// anything the surface has cached: the same entries answer the same family
// forever, so scrolling a finished turn back into view cannot change its icons,
// and a conversation read out of a file that predates the narrator's prefix
// derives the mark its tools always implied.
func stepCategory(c caption, es []entry) session.ActionCategory {
	if c.category != "" {
		return c.category
	}
	from, to := captionTools(c, es)
	tools := make([]string, 0, to-from)
	for i := from; i < to && i < len(es); i++ {
		if es[i].kind == entryTool {
			tools = append(tools, es[i].tool)
		}
	}
	return session.ActionCategoryForTools(tools)
}
