package tui3

import (
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
//     the pulse at the transcript's foot, and a second moving thing in the same
//     three rows is two answers to "what is happening now". The gutter is a
//     label, and a label that animates is a label nobody can stop looking at.
//  2. IT NEVER SAYS HOW IT WENT. There is no tick, no cross, no warning mark and
//     no colour of its own in this table — a step that failed is not covered by
//     this block at all ([liveWorkKeepsRow]), so a success mark here could only
//     ever mean "still true so far", which is a mark that says nothing. `test`
//     draws a TARGET and never a checkmark for exactly this reason: the family
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
// ── WHY THESE CHARACTERS ──
//
// EVERY MARK HERE DRAWS IN AN ORDINARY MONOSPACE FONT. No patched font is
// required, nothing is installed, and there is no emoji: four of the thirteen are
// slots the shared vocabulary already ships and has measured
// (internal/tui2/tokens' [tokens.GlyphSearch], [tokens.GlyphWrite],
// [tokens.GlyphShell], [tokens.GlyphDiffAdd]), and the rest come from Geometric
// Shapes, Arrows and Mathematical Operators — the blocks every terminal font in
// use covers. actionicon_test.go measures all thirteen at one cell under BOTH
// rulers this tree ships against, so a tempting replacement cannot enter without
// passing the same gate the shared table uses.
//
// THE PATCHED-FONT TIER IS DELIBERATELY NOT WIRED HERE, and that is a finding
// rather than an omission. [tokens.DetectGlyphSet] defaults its answer to
// NerdFont and can only ever VETO — no terminal reports its font — so wiring it
// would draw tofu boxes in this gutter on every unpatched terminal, which is the
// commonest terminal there is. A gutter of empty rectangles is strictly worse
// than the shape it replaced. The rich icons the shared table already binds
// (nf-fa-search, nf-fa-pencil, nf-fa-terminal, nf-pl-branch) are one explicit
// opt-out door away from being usable here; until this surface HAS that door,
// the plain floor is what ships, and it is a designed floor rather than a
// degradation.

// actionGutter is the fixed cost of the mark: the cell it stands in, and the
// space after it. It is a constant rather than a measurement because the whole
// point is that it does not depend on which family a step turned out to be.
const actionGutter = 2

// actionMark is one family's two spellings.
type actionMark struct {
	// glyph is the ordinary-terminal mark, one cell.
	glyph string
	// ascii is the screen-reader and no-Unicode tier's mark, also one cell, so
	// flipping the tier moves no column. Each is a character a shell already
	// gives a meaning to where one exists — `$` a prompt, `?` a search, `+` a
	// new thing, `@` addressing somebody, `|` things running side by side —
	// because in the tier with no shapes left, familiarity is the only thing a
	// mark has.
	ascii string
}

// actionMarks is the table, and it is exhaustive over [session.ActionCategories]
// — a test walks the engine's list and fails on a family with no mark, so the
// vocabulary and the gutter cannot drift apart.
var actionMarks = map[session.ActionCategory]actionMark{
	// ⌕ U+2315: the vocabulary's own search slot.
	session.ActionSearch: {tokens.GlyphSearch, "?"},
	// ▤ U+25A4: a box with lines in it — a page of text, opened.
	session.ActionRead: {"▤", "<"},
	// ✎ U+270E: the vocabulary's own write slot, and the pencil the owner
	// picked out of Octicons.
	session.ActionEdit: {tokens.GlyphWrite, "*"},
	// + : something that was not there is. It is the vocabulary's diff-add byte
	// and it is ASCII, which the table has never claimed exclusively.
	session.ActionCreate: {tokens.GlyphDiffAdd, "+"},
	// $ : the vocabulary's shell slot — the prompt a person types a command at.
	session.ActionRun: {tokens.GlyphShell, "$"},
	// ◎ U+25CE: a target being aimed at. NEVER a checkmark: this is the act of
	// checking, and the block draws no verdicts.
	session.ActionTest: {"◎", "!"},
	// ↗ U+2197: out of here and onto a page somewhere else — the link, drawn as
	// the thing a link does.
	session.ActionBrowse: {"↗", "^"},
	// ⇄ U+21C4: bytes going the other way as well.
	session.ActionTransfer: {"⇄", "&"},
	// » U+00BB: the guillemet, which is a quotation mark in half of Europe —
	// something being SAID, to a person, and one cell in every font ever made.
	session.ActionCommunicate: {"»", "@"},
	// ⇉ U+21C9: two arrows travelling side by side — work handed out, or this
	// mind copied to run beside itself.
	session.ActionCoordinate: {"⇉", "|"},
	// ≡ U+2261: three level lines, an outline. It is the safe cousin of ☰,
	// which the shared table BANS for measuring two cells.
	session.ActionPlan: {"≡", "#"},
	// ◷ U+25F7: a quarter of a clock face, still. The hourglasses are banned —
	// two cells, and they lie about liveness on a row that is not moving.
	session.ActionWait: {"◷", ","},
	// ▪ U+25AA: a small square. The bucket's mark is the quietest one in the
	// table on purpose — it says "a step", which is all it knows.
	session.ActionWork: {"▪", "."},
}

// actionMarkFor is the mark for one family, in this terminal's tier.
//
// AN UNKNOWN FAMILY DRAWS THE BUCKET rather than a blank. A gutter that
// sometimes vanished would move the sentences beside it, and this whole file
// exists so they do not move; a family this build has never heard of is a step,
// and a step is [session.ActionWork].
func (a *app) actionMarkFor(category session.ActionCategory) string {
	mark, known := actionMarks[category]
	if !known {
		mark = actionMarks[session.ActionWork]
	}
	return a.linearMark(mark.glyph, mark.ascii)
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
