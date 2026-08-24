package tui3

// THE SECTION THAT HOLDS THE CURSOR SAYS SO ON ITS OWN HEADING.
//
// Home is several regions at once — two zones of triage, a list of projects, a
// card — and at [homeTierColumns] they stand side by side as three columns. The
// row under the cursor wears THE GROUND LADDER's cursor step, which is a very
// quiet tint on purpose (1.17:1 against an ordinary terminal ground), and that
// tint answers "which ROW am I on" perfectly while answering "which REGION is
// my keyboard in" not at all. A person looking at the frame from a foot away
// sees three columns equally at rest and one faint band somewhere in them.
//
// So the answer is given TWICE, at two scales, in the one channel the ladder
// has: the row keeps its ground, and the HEADING of the section that holds it
// takes the same ground. Two lifted bands in one column and none in the others
// is a shape the eye reads before it reads a word — this block, and this row
// inside it.
//
// ── THE STEP IS THE CURSOR STEP, AND SELECTED WAS THE CANDIDATE ─────────────
//
// The obvious reading of the ladder puts the heading on the SELECTED step: the
// heading is the container of the current thing, selected is "the chosen thing",
// and it would leave the heading a rung senior to the row it stands over. It is
// the wrong answer here for one concrete reason — THE SELECTED STEP IS ALREADY
// SPENT ON THIS COLUMN. A conversation this terminal is holding on screen wears
// it ([markFront] in palette.go's [overlayRowTinted]), it is persistent, it is
// about a door rather than about where a person's hands are, and it can sit
// three rows under a project heading. A heading on that same ground in that same
// column would be one step carrying two meanings, which is the exact harm the
// ladder's refusal of a fifth step exists to prevent.
//
// And the cursor step is not a compromise: what a marked heading says is "the
// cursor is in here", which is the cursor step's own sentence read at the scale
// of a section rather than of a row. ONE FACT, ONE RUNG, SAID TWICE. What buys
// the heading its seniority is not a louder tint but NOVELTY — a heading on this
// surface has never worn a ground at all, so a heading wearing one is entirely
// new information, where a lifted row is one of a hundred a person sees in a
// session.
//
// ── AND THE TEXT DOES NOT MOVE ──────────────────────────────────────────────
//
// THE ACCENT BUDGET FORBIDS LIGHTING A HEADING. Headings are furniture: they sit
// in the same place every time, and a column of lit headings is a column with no
// answer to "where am I" — which is the defect this whole file is about, arriving
// from the other direction. So the heading's word stays `dim` exactly as it was,
// and the ground alone carries the fact.
//
// ── EXACTLY ONE, AND NEVER AT REST ──────────────────────────────────────────
//
// A frame marks ONE heading or none. None at rest, because rest is the morning
// glance and nothing on the column is chosen; none under a search, because the
// column is then a drop-up of matches and the headings over them are a filter's
// grouping rather than a place a person is standing in.
//
// ── THE POINTER NEVER MOVES IT ──────────────────────────────────────────────
//
// The pointer previews a card without moving the selection ([homeView.previewLine]),
// and it must not move this either: the marked heading answers "where is my
// keyboard", and a hover is the other hand. So the walk below starts at
// [homeView.cursor] and never looks at [homeView.hover].
//
// ── IT IS A WALK AND NOT A TABLE OF SECTIONS ────────────────────────────────
//
// The rule is "the nearest heading that owns the cursor's row", and it is one
// backward walk over the built lines that names no section kind of its own. A
// section this file had to learn about would be a section that stopped working
// the day somebody added one — the zones, the projects, the folded `elsewhere`
// block and everything after them are found by the same two predicates
// ([headingKind] and [sectionEnd]), which is how the archive fold correctly marks
// nothing: it is a section of one row with no heading over it, and the walk
// meets the blank above it and stops.

// sectionEnd reports that a line CLOSES whatever section stood above it, so the
// walk out of a row's block stops rather than reaching back into the previous
// one.
//
// It is the blank rows and only the blank rows, which is not a coincidence:
// [homeView.blank] is the one thing this column puts between two sections, THE
// SPACING LADDER's block step doing the job a border would do elsewhere. The
// zones' own gap is a kind of its own ([homeAttentionGap]) for a reason that has
// nothing to do with this walk, and it separates two sections just the same.
//
// IT FAILS TOWARD MARKING NOTHING, which is the right direction. A project
// somebody opened inside the folded block gets air on both sides
// ([homeView.buildElsewhere]), so a cursor down among its conversations meets a
// blank before it meets any heading and the frame marks none — that line is a
// DOOR rather than a heading and cannot be one without a row that answers enter
// wearing a ground it did not earn. An honest silence beats a confident mark on
// the section above.
func sectionEnd(kind homeRowKind) bool {
	return kind == homeBlank || kind == homeAttentionGap
}

// markedSection is the line number of THE ONE HEADING THIS FRAME MARKS, and
// [homeRest] when it marks none.
func (h *homeView) markedSection() int {
	if !h.open || h.searching() || h.cursor < 0 || h.cursor >= len(h.lines) {
		return homeRest
	}
	for at := h.cursor - 1; at >= 0; at-- {
		kind := h.lines[at].kind
		if headingKind(kind) {
			return at
		}
		if sectionEnd(kind) {
			return homeRest
		}
	}
	return homeRest
}

// marksSection reports that the line at `at` is that one heading.
func (h *homeView) marksSection(at int) bool {
	return at >= 0 && h.markedSection() == at
}

// sectionGround puts the cursor step under a heading that owns the cursor, and
// hands every other heading back exactly as it was drawn.
//
// THE GROUND IS THE FULL ROW because that is the shape every ground on this
// surface has: [palette.cursor] pads to the width it is given and the rows
// beside this one are painted the same way, so a heading with a tint behind its
// word alone would be a fifth shape rather than a fourth step. Below the ANSI256
// rung and in linear mode there is no ground to draw, which is the same trade
// the cursor's own row already makes one call site over.
func (h *homeView) sectionGround(text string, at, width int, pal palette) string {
	if !h.marksSection(at) {
		return text
	}
	return pal.cursor(text, width)
}
