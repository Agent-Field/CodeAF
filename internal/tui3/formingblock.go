package tui3

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE FORMING BLOCK: what a task looks like in the seconds before it exists.
//
// A task does not start the moment somebody asks for it. The brief is written
// first — an auxiliary model call that reads what was typed and writes what a
// worker will actually be given (internal/session's task_shape.go) — and that
// call takes as long as careful writing takes. The block is what stands there
// meanwhile, and until this file existed the whole of it was three rows and a
// count-up:
//
//	▏ task
//	▏ "write the release notes"
//	▏ ⠙ shaping the brief… · 13s
//
// Thirteen seconds of a number going up, in front of a person who has no way to
// tell a careful model from a stuck one. THE WORDS WERE BEING WRITTEN THE WHOLE
// TIME; they simply had nowhere to go. So the block grew two things, and
// deliberately no more than two.
//
// ── ONE: THE TAIL, WHICH IS NOT A FEED ──
//
// One dim row under the status row, one shade under it, hanging in the status
// row's own indent: the newest part of the brief as the shaper writes it.
//
//	▏ ⠙ shaping the brief… · 13s
//	▏   the failure this kind of work has is a release note that lists comm
//
// A TAIL IS A CONSTANT-HEIGHT LINE. It is exactly one row whatever the model is
// writing, so nothing above it ever moves and no row on the screen jumps. That
// is the whole reason the preview is a tail rather than the brief being typed
// into the transcript: a block that grew a row every second would push the
// conversation up the screen for as long as it took, which is the surface
// writing over somebody's reading.
//
// The row is the LAST line the brief lays out to at this width
// ([formingTailLines]), not the first cut short. What a person wants from a
// preview is what is being written NOW; the opening of a paragraph, frozen while
// the model writes five hundred more characters after it, is a still photograph
// of a stream.
//
// ── TWO: THE WINDOW, BEHIND THE FOLD EVERY BLOCK ALREADY HAS ──
//
// The tail row is a door. Pointed at, it wears `▸` in the two cells its indent
// was already spending; pressed — by click, or by `→` with an empty box — it
// becomes `▾` and the one row becomes [formingWindowLines] of them, the last few
// lines instead of the last one.
//
//	▏ ⠙ shaping the brief… · 13s
//	▏ ▾ good, to somebody who was not in the room. The failure this kind of
//	▏   work has is a release note that lists commits: name what changed for
//	▏   the person using it, and say which release it lands in.
//
// NO NEW KEY IS SPENT ON IT. `▸`, the press, and `→` on the row a person is
// standing on are this surface's fold everywhere else it has one; ctrl+e's
// thought window is the same idea one block over (thinking.go), and this one
// borrows its shape rather than inventing a second one.
//
// ── AND SEVERAL AT ONCE ARE ONE BLOCK ──
//
// Two commands can be shaping at the same time, and three blocks of four rows
// stacked at the tail of a transcript is a wall. So more than one is ONE block
// with a head that counts them and one compact row each:
//
//	▏ tasks · 3 forming
//	▏ ⠙ release notes · 13s
//	▏ ⠙ nil-map crash · 8s
//	▏ ▸ Reproduce the crash from the stack trace in issue #94, then write
//	▏ ⠙ the docs for /task · 2s
//
// ONLY THE POINTED ROW SHOWS ANYTHING UNDER IT — the block is constant height in
// the number of tasks, not in the number of tasks times the size of a preview —
// and which row is pointed at is said the way this surface says it everywhere:
// the row lights (hover.go's one pass). ↑/↓ walk the rows, which are the keys
// that walk rows in the transcript already.
//
// WITH EXACTLY ONE TASK FORMING, NONE OF THAT DRAWS. No head that counts, no
// roster row, no walking: the block is what it always was, plus the tail. That
// is the emptiness law read at the shape of a block rather than at a number —
// machinery for the plural case does not appear in the singular case.

// previewing is what the tail should be showing, and whether it is the model's
// working rather than its answer.
//
// THE BRIEF WINS THE MOMENT THERE IS A BRIEF, AND NEVER GIVES THE ROW BACK. A
// shaper on the careful tier reasons first and writes second, so the row starts
// as the think and becomes the brief — and a row that flicked back to reasoning
// on a pause would be the surface un-saying something a person had begun to
// read.
func (p preflight) previewing() (string, bool) {
	if p.tail != "" {
		return p.tail, false
	}
	return p.think, true
}

// formingWindowLines is how much of the brief the opened window holds.
//
// SIX, WHICH IS TWICE THE THOUGHT WINDOW AND FOR A DIFFERENT REASON. The
// thinking window keeps three ([thoughtLive]) because it is drawn UNASKED beside
// a reply somebody is reading, and an uninvited block owes the page restraint. A
// person opened this one on purpose, and what they opened it to read is prose —
// three lines of a brief is a sentence cut in half, six is a paragraph you can
// judge. It is still small enough that the whole block fits under a turn without
// becoming the screen.
const formingWindowLines = 6

// formingHeadWord is the plural block's head: `tasks · 3 forming`. The singular
// head is [taskFormingName]'s own word and is spelled where it always was.
const formingHeadWord = "tasks"

// formingStateWord is the state the head counts in. It is the card's own word
// ([taskFormingWord] without its ellipsis), because a person reading `forming…`
// on a card and `3 forming` on a block is reading about the same thing. The
// spelling is longer than the word it holds because [formingWord] is already a
// tool row's whole sentence one file over (toolview.go), and one name for two
// things is the drift this codebase spends its comments preventing.
const formingStateWord = "forming"

// formingRail is the block's whole border: one hairline and one space, on every
// row, exactly as the three-row block wore it.
const formingRail = "▏ "

// formingIndent is the two cells the tail hangs in, under the status row's mark.
// It is also the tail row's GLYPH CELL — the fold mark is drawn into it rather
// than in front of it, so a row that gains a `▸` does not move sideways by two
// columns while somebody is reading it.
const formingIndent = "  "

// preflightRows draws the forming block at the transcript tail.
//
// It answers in ROWS rather than in strings because the block is pressable now:
// the tail is a door and, where several tasks are forming, each roster row is
// one. The hit carries the wait's place in the list, which is what
// [app.formingPress] and [app.isHot] resolve a press and a pointer against.
func (a *app) preflightRows(width int) []row {
	if !a.waiting() || width < 3 {
		return nil
	}
	a.clampWaitAt()
	if len(a.waits) == 1 {
		return a.formingOneRows(width)
	}
	return a.formingManyRows(width)
}

// formingOneRows is the block as it has always been — the title, the person's
// words, the phase — with the tail hung under the phase row.
func (a *app) formingOneRows(width int) []row {
	p := &a.waits[0]
	room := width - 2
	out := []row{{text: a.pal.dim(formingRail + taskFormingName), entry: -1}}
	for _, line := range a.formingIdentity(p, room) {
		out = append(out, row{text: a.pal.dim(formingRail + line), entry: -1})
	}
	out = append(out, row{text: a.pal.dim(formingRail + a.formingMark() + " " + fit(a.formingPhase(p), room-2)), entry: -1})
	return append(out, a.formingPreviewRows(p, 0, width)...)
}

// formingManyRows is the block when more than one command is in flight: one head
// that counts, one row each, and the pointed row's preview under it.
func (a *app) formingManyRows(width int) []row {
	room := width - 2
	head := formingHeadWord + railSep + itoa(len(a.waits)) + " " + formingStateWord
	out := []row{{text: a.pal.dim(formingRail + fit(head, room)), entry: -1}}
	mark := a.formingMark()
	for i := range a.waits {
		p := &a.waits[i]
		line := mark + " " + a.formingName(p, room-2)
		if word := countUpWord(a.now().Sub(p.at)); word != "" {
			line += railSep + word
		}
		out = append(out, row{
			text:  a.pal.dim(formingRail + fit(line, room)),
			entry: -1, hit: hitForming, turn: i,
		})
		if i == a.waitAt {
			out = append(out, a.formingPreviewRows(p, i, width)...)
		}
	}
	return out
}

// formingIdentity is the rows that say WHOSE work this is.
//
// The person's words when there are person's words — quoted, because they are
// verbatim — and the task's own name when the block was raised by an approved
// proposal, plain, because the name is the surface's word and wearing quotes
// would claim somebody typed it. The quotation is capped at two fitted rows so a
// long command cannot turn a transient wait into a transcript card.
func (a *app) formingIdentity(p *preflight, room int) []string {
	switch {
	case p.brief != "":
		lines := wrap(strconv.Quote(p.brief), room)
		if len(lines) > 2 {
			lines = lines[:2]
			lines[1] = fit(lines[1], room)
			if !strings.HasSuffix(lines[1], "…") {
				lines[1] = fit(lines[1]+"…", room)
			}
		}
		return lines
	case p.name != "":
		return []string{fit(p.name, room)}
	}
	return nil
}

// formingName is one forming task said in a few words, for the plural block's
// compact row: the proposal's own name where there is one, and otherwise the
// opening of what the person typed.
//
// IT WEARS NO QUOTES, and that is the one place this block departs from the
// singular above. A roster row is a list of things, not a quotation of one, and a
// cut quotation is a quotation with no closing mark — `"write the release nu…` —
// which reads as a rendering fault rather than as somebody's sentence. The words
// are still theirs and still in the block's own ink; they are simply listed.
func (a *app) formingName(p *preflight, room int) string {
	name := p.name
	if name == "" {
		name = strings.TrimSpace(strings.ReplaceAll(p.brief, "\n", " "))
	}
	return fit(name, room)
}

// formingPhase is the status row's sentence: what is happening, and for how
// long. The count-up is [countUpWord], which floors under a second by the
// emptiness law.
func (a *app) formingPhase(p *preflight) string {
	line := p.note
	if word := countUpWord(a.now().Sub(p.at)); word != "" {
		line += railSep + word
	}
	return line
}

// formingMark is the live mark every row of the block wears.
//
// The linear tier's objection to a spinner is the one it makes on a tool line: a
// claim repeated thirty times a second is heard thirty times a second by a
// surface being read aloud. A still mark makes it once.
func (a *app) formingMark() string {
	if a.linear {
		return glyphRunASCII
	}
	return tokens.Spinner(a.paints / spinnerStep)
}

// formingPreviewRows is the tail, or the window somebody opened in its place.
//
// It draws NOTHING at all until the shaper has written something, which is the
// emptiness law and also the only honest answer on the proposal road: that brief
// was written before the person was ever asked, so there is no stream behind the
// wait and a row promising one would be machinery pretending to be telemetry.
//
// The rows are painted one stop along the fade, which is one shade under the
// status row above them on a terminal that has the steps and plain dim on one
// that does not ([palette.fade]). The fold mark rides the FIRST of them, in the
// indent's own cells.
func (a *app) formingPreviewRows(p *preflight, at, width int) []row {
	text, thinking := p.previewing()
	lines := formingTailLines(text, width-4)
	if len(lines) == 0 {
		return nil
	}
	// THE TAIL IS THE WINDOW'S LAST ROW, and it is that row by construction
	// rather than by two functions agreeing: the lines are laid out once and the
	// closed block keeps the last of them. Laying the tail out on its own budget
	// was one line off the window's for a whole afternoon, which is exactly the
	// kind of disagreement one source of truth is for.
	if !p.open {
		lines = lines[len(lines)-1:]
	}
	out := make([]row, 0, len(lines))
	for i, line := range lines {
		lead := formingIndent
		if i == 0 {
			lead = bandFoldMark(a.pal, !p.open) + " "
		}
		// THE MODEL'S WORKING IS SAID IN THE INK THIS SURFACE SAYS IT IN. A live
		// think is italic here exactly as it is in the thought window one file
		// over (thinking.go), so a person reading the row can tell the shaper's
		// reasoning from the brief it has started writing without being told.
		if thinking {
			line = a.pal.italic(line)
		}
		out = append(out, row{
			text:  a.pal.dim(formingRail) + a.pal.fade(lead+line, 1),
			entry: -1, hit: hitForming, turn: at,
		})
	}
	return out
}

// formingTailLines is the last [formingWindowLines] lines the brief lays out to
// at this width — the window, of which the closed block keeps the final row.
//
// THE END IS WHAT A PREVIEW IS FOR. The text grows at its end, so the rows kept
// are the last ones and the last row is where the model's pen is. Everything
// above that row is settled: [wrap] breaks a paragraph greedily from its start,
// so a line that has been drawn once keeps the words it was drawn with and only
// the final row grows. That is what makes the preview calm to read rather than
// a paragraph reflowing under somebody's eyes.
//
// WHAT IT IS LAID OUT FROM IS ALREADY BOUNDED. The scanner behind it keeps the
// last eight kilobytes of the field and forgets the rest (session's
// PartialStringLimit), and the shaper is capped well under that
// (taskShapeBriefLimit), so there is no second cut here — and there must not be
// one, because a cut that moved as the text grew would move every line break
// after it and make the tail jitter.
func formingTailLines(text string, width int) []string {
	text = strings.TrimRight(text, " \t\n")
	if text == "" || width < 4 {
		return nil
	}
	lines := trimBlanks(wrap(text, width))
	if len(lines) == 0 {
		return nil
	}
	if len(lines) > formingWindowLines {
		lines = lines[len(lines)-formingWindowLines:]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, fit(line, width))
	}
	return out
}

// ── the pointer, the press and the keys ─────────────────────────────────────

// clampWaitAt keeps the pointed row inside the list. Waits settle in whatever
// order their doors answer in, so the row a person was standing on can leave
// under them.
func (a *app) clampWaitAt() {
	if a.waitAt >= len(a.waits) {
		a.waitAt = len(a.waits) - 1
	}
	if a.waitAt < 0 {
		a.waitAt = 0
	}
}

// formingPress is a click on the block.
//
// A row that is not the pointed one takes the point AND opens, because in a
// block of several the point is only visible as the preview it draws — pointing
// at a row and showing nothing under it would be a gesture with no answer. The
// pointed row's own rows toggle, which is what the mark on them promises.
func (a *app) formingPress(at int) {
	if at < 0 || at >= len(a.waits) {
		return
	}
	if at != a.waitAt {
		a.waitAt = at
		a.waits[at].open = true
		a.touch()
		return
	}
	a.waits[at].open = !a.waits[at].open
	a.touch()
}

// openForming opens the pointed wait's window, and reports whether there was one
// to open. It is `→`'s share of the fold, and it answers false wherever the key
// must keep the meaning it has everywhere else: no block up, nothing written
// yet, or a window already open (input.go).
func (a *app) openForming() bool {
	if !a.waiting() {
		return false
	}
	a.clampWaitAt()
	p := &a.waits[a.waitAt]
	if text, _ := p.previewing(); p.open || text == "" {
		return false
	}
	p.open = true
	a.touch()
	return true
}

// closeForming is `←`'s half of the same fold, on the same terms.
func (a *app) closeForming() bool {
	if !a.waiting() {
		return false
	}
	a.clampWaitAt()
	p := &a.waits[a.waitAt]
	if !p.open {
		return false
	}
	p.open = false
	a.touch()
	return true
}

// walkForming moves the point one row, and reports whether it moved.
//
// IT ANSWERS FALSE WITH ONE TASK FORMING, which is the emptiness law spent on
// the keyboard: there is no list to walk, so ↑ and ↓ keep every meaning they
// have (input.go's ladder) and nothing about a single command's wait changes
// what the arrows do. It answers false at either end too, so walking off the
// block falls through to selecting a call and then to scrolling, the way every
// other walk on this surface ends.
func (a *app) walkForming(delta int) bool {
	if len(a.waits) < 2 {
		return false
	}
	a.clampWaitAt()
	next := a.waitAt + delta
	if next < 0 || next >= len(a.waits) {
		return false
	}
	a.waitAt = next
	a.touch()
	return true
}

// formingHot reports whether a person is on this row of the block — under the
// pointer, or under the keyboard.
//
// THE WHOLE OF A WAIT'S ROWS LIGHT TOGETHER. Its compact row and the preview
// under it are one thing to press, and a preview that went dark when the pointer
// crossed onto it would be the surface withdrawing the door at the cell where it
// matters.
//
// AND THE KEYBOARD'S ROW LIGHTS EXACTLY AS THE POINTER'S DOES, which is
// hover.go's own law: the row a person is on does not change appearance
// depending on which hand they used. ↑/↓ move the point through a block of
// several, so the point has to be visible without them.
//
// IT ANSWERS NOTHING AT ALL FOR A BLOCK OF ONE. There is no list to be at a
// place in, so a lit ground would be the surface reporting a choice nobody made
// — the emptiness law, spent on the pointer.
func (a *app) formingHot(r row) bool {
	if r.hit != hitForming {
		return false
	}
	if a.hot.kind == hoverForming && r.turn == a.hot.index {
		return true
	}
	return len(a.waits) > 1 && r.turn == a.waitAt
}
