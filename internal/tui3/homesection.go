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
// So the answer is given TWICE, at two scales, IN TWO CHANNELS: the row keeps
// its ground, and the HEADING of the section that holds it steps up from dim to
// the body ink ([homeView.sectionInk]). One band and one brightened heading in
// one column is a shape the eye reads before it reads a word — this block, and
// this row inside it — and it cannot be misread as two selections, because only
// one of the two is the selection's shape.
//
// ── THE HEADING BRIGHTENS AND DELIBERATELY DOES NOT TAKE A GROUND ───────────
//
// The first build of this law gave the heading the cursor's own band ("one
// fact, one rung, said twice"), and it read wrong in exactly the way a person
// said it did: two identical bands in one column are two candidate selections,
// and the frame is asking them which one enter would act on. A ground on this
// screen now means one thing — where a person's hands are — so the heading
// answers in the ladder's other channel, lightness. A heading on this surface
// has never been anything but dim, so a heading in the body ink is entirely new
// information; and ink is not the accent, so THE ACCENT BUDGET (a heading is
// never LIT) is kept to the letter.
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

// sectionInk is the paint the marked heading's WORD takes — ink while the
// cursor is somewhere in its section, the ordinary heading dim otherwise.
//
// IT IS LIGHTNESS AND DELIBERATELY NOT A GROUND. The first build of this law
// put the cursor's own band under the heading too ("said twice"), and the frame
// then held two identical bands in one column — a shape a person reads as two
// selections, asking which of them enter would act on. A band is the hand's
// mark and there is exactly one hand; the heading answers the COLUMN question
// in the other channel the ladder has, one lightness step up from every other
// heading on the frame. A heading has never been anything but dim on this
// surface, so a heading in the body ink is entirely new information — and it
// cannot be mistaken for a selection, because it is not the selection's shape.
func (h *homeView) sectionInk(at int, pal palette) func(string) string {
	if h.marksSection(at) {
		return pal.ink
	}
	return pal.dim
}
