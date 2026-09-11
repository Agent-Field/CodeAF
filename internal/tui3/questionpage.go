package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE PAGE, DRAWN ─────────────────────────────────────────────────────────
//
// `o` OPENS THE QUESTION AS TWO PANES (owner ruling 2026-09-11, page pick A):
// the question and its reason across the top, the answers in a list on the left
// with what the asker showed for the whole decision under them, and the evidence
// of the answer the pointer is on in a pane on the right — its word, what it
// means, what taking it leaves true, why the asker would take it, what would
// change its mind, how sure it is, and everything it drew.
//
//	  ? Which storage for the session index?          model asks · the turn waits on it
//	  The index needs somewhere to live between launches.
//
//	────────────────────────────────────────┬───────────────────────────────────────────
//	  ▸ 1  SQLite         ◆ recommended     │ SQLite
//	    2  JSONL                            │ one file beside the conversation
//	    3  BoltDB                           │
//	    4  something else…                  │ then · the index survives a crash mid-write
//	                                        │ why this one · already a dependency
//	  what it showed you                    │ would switch if you need it greppable by hand
//	  store.go:88-120  the current index    │ confidence · fairly sure
//	                                        │
//	                                        │ schema
//	                                        │ sessions(id, title, updated)
//	────────────────────────────────────────┴─── answering 1 SQLite ─────────────────────
//	  ↑↓ choose · enter take it · esc later · → detail          c change · x compare · ? ask back
//
// IT REPLACED A PAGE OF FOLDING SECTIONS, and the reason is the thing a person
// does on this page: WEIGH one answer against the next. The old page stacked
// every open answer's paragraph, asides, diagrams and notes one under another
// in a single column, so the second answer's evidence was below the first's
// and comparing them meant scrolling between two places that could not both be
// on screen. With the list fixed on the left and the evidence following the
// pointer on the right, `↓` IS the comparison: the pane changes under the eye
// and nothing else moves.
//
// NARROWER THAN TWO PANES ([besideFits]) IT IS ONE COLUMN, the panel's own
// preview-narrow shape: every answer a row, and the one the pointer is on
// unfolded under its row. The compare table (`x`) and an answer that is a SHAPE
// — holes, a checklist, a run of pairs, a dial — take the column at any width:
// the table lays every answer against every other and needs the whole of it,
// and a shape has no evidence per row to put beside it.
//
// ── TWO WINDOWS, AND THE POINTER IS ALWAYS IN ONE OF THEM ──
//
// The head is pinned; under the rule, the list (or the one column) and the
// evidence pane each keep an offset of their own. The list's window follows the
// pointer ([app.questionRoomShowFocus]) — the answer a person walked to is on
// screen, always — and the evidence pane starts at its top every time the
// pointer moves, because what it shows has just changed. `→` hands the arrows to
// the pane so a long diagram can be read to its end, `←` hands them back, and the
// wheel scrolls whichever pane it is over.

// questionPageOtherWord is what the evidence pane says while the pointer is on
// `something else…`: the page's box is the message box below it, so the row is
// not a box of its own the way the panel's is ([app.questionPanelOther]), and the
// pane says where the words go.
const questionPageOtherWord = "type your own answer in the box below, then enter"

// questionSpot is what a press on one row of the page reaches: which answer,
// and the columns it answers in. A row that reaches nothing is at -1.
type questionSpot struct {
	at       int
	from, to int
}

// questionPageLayout is the page laid out at one size, before either window is
// applied: the pinned head, where the seam falls, the left pane (or the one
// column) with the answer each of its rows presses, and the evidence pane.
type questionPageLayout struct {
	head []string
	// seam is the column the two panes meet at, or -1 for one column.
	seam   int
	list   []string
	listAt []int
	pane   []string
	// region is how many rows the two windows have under the rule.
	region int
}

// questionRoomBeside reports whether the page lays its answers beside the
// evidence of the one the pointer is on, or in one column.
func (a *app) questionRoomBeside(width int) bool {
	room := a.qroom
	return room != nil && besideFits(width) && !room.compare &&
		room.input.kind == session.InputNone && len(room.head.question.Options) > 0
}

// questionRoomTakesOther reports whether the page's list ends in `something
// else…`: exactly where the panel's does ([app.questionTakesOther]), so the list
// a person opened is the list they were looking at.
func (a *app) questionRoomTakesOther() bool {
	room := a.qroom
	return room != nil && room.input.kind == session.InputNone && a.questionTakesOther(room.head)
}

// questionRoomWalk is how many rows the page's pointer walks.
func (a *app) questionRoomWalk() int {
	room := a.qroom
	if room == nil {
		return 0
	}
	if room.input.kind != session.InputNone {
		return room.input.count()
	}
	n := len(room.head.question.Options)
	if a.questionRoomTakesOther() {
		n++
	}
	return n
}

// questionRoomShown is the question as the page's pointer sees it: the object
// the block holds, with the pointer standing where the page's does, so one
// answer row is painted by one painter wherever it is drawn
// ([app.questionPanelOption]).
func (a *app) questionRoomShown() questionShown {
	shown := a.qroom.head
	shown.pick = a.qroom.focus
	return shown
}

// questionRoomLay lays the page out at one size.
func (a *app) questionRoomLay(width, height int) questionPageLayout {
	lay := questionPageLayout{seam: -1}
	lay.head = a.questionPageHead(width)
	lay.region = max(height-len(lay.head)-1, 0)
	if !a.questionRoomBeside(width) {
		lay.list, lay.listAt = a.questionPageColumn(width)
		return lay
	}
	left, right := besideSplit(width, a.questionPageListWant(width))
	lay.seam = left
	lay.list, lay.listAt = a.questionPageList(left)
	lay.pane = a.questionPagePane(right)
	return lay
}

// questionRoomView is the body region at one size: exactly the rows it draws,
// and for each of them what a press on it reaches.
func (a *app) questionRoomView(width, height int) []row {
	room := a.qroom
	if room == nil || width <= 0 || height <= 0 {
		return nil
	}
	if a.questionDrainReplies() {
		room.dirty = true
	}
	if !room.dirty && room.width == width && room.height == height && room.rows != nil {
		return room.rows
	}
	lay := a.questionRoomLay(width, height)
	out := make([]row, 0, height)
	spots := make([]questionSpot, 0, height)
	add := func(text string, spot questionSpot) {
		if len(out) < height {
			out = append(out, row{text: text, entry: -1})
			spots = append(spots, spot)
		}
	}
	none := questionSpot{at: -1}
	for _, line := range lay.head {
		add(line, none)
	}
	add(besideRule(a.pal, width, lay.seam, tokens.GFrameTeeDown, ""), none)
	from := questionWindowStart(room.offset, len(lay.list), lay.region)
	list := lay.list[from:min(from+lay.region, len(lay.list))]
	listAt := lay.listAt[from:min(from+lay.region, len(lay.listAt))]
	if lay.seam < 0 {
		for i, line := range list {
			add(line, questionSpot{at: listAt[i], from: 0, to: width})
		}
	} else {
		// BOTH PANES FILL THE REGION, so the seam runs unbroken from the rule
		// above to the rule in the foot below, and the page is one object split
		// in two rather than two lists that happen to be near each other.
		detail := questionWindowStart(room.detail, len(lay.pane), lay.region)
		pane := lay.pane[detail:min(detail+lay.region, len(lay.pane))]
		for i, line := range besides(a.pal, questionPadTo(list, lay.region), pane, lay.seam, width) {
			spot := none
			if i < len(listAt) {
				spot = questionSpot{at: listAt[i], from: 0, to: lay.seam}
			}
			add(line, spot)
		}
	}
	room.rows, room.spots, room.width, room.height, room.dirty = out, spots, width, height, false
	room.seam = lay.seam
	return out
}

// questionWindowStart clamps a window's offset to the rows it is over, so a
// window never opens past its own end.
func questionWindowStart(offset, total, height int) int {
	return questionClamp(offset, 0, max(total-height, 0))
}

// questionPadTo pads a column with blank rows to `n`.
func questionPadTo(rows []string, n int) []string {
	for len(rows) < n {
		rows = append(rows, "")
	}
	return rows
}

// questionPageHead is what stands above the rule: the question with its mark
// and — at the right — who is asking and what is waiting on it, the asker's
// reason under it, and one blank row. The row of air above the head is the
// frame's own, under the tabs.
//
// THE FOUR FACTS DESIGN.md WRITES FOR THIS PAGE ARE ALL HERE: asked by · why now ·
// what is paused on it · what goes on without it ([questionBlockingWord] carries
// the last two). The emptiness law governs each of them.
func (a *app) questionPageHead(width int) []string {
	q := a.qroom.head.question
	inner := max(width-2*len(questionIndent), 1)
	aside := questionPageAside(q)
	asideW := ansi.StringWidth(aside)
	out := make([]string, 0, 6)
	lines := wrap(strings.TrimSpace(q.Head), max(inner-2, 1))
	// The aside stands beside the head where the head is one line with room
	// left for it, and on a dim row of its own under the reason where it is not
	// — the head is never cut to make room for its own attribution.
	beside := aside != "" && len(lines) == 1 && ansi.StringWidth(lines[0])+2+2+asideW <= inner
	for i, line := range lines {
		lead := "  "
		if i == 0 {
			lead = a.pal.warnBold(a.icon(tokens.GNeedsHuman)) + " "
		}
		text := questionIndent + lead + a.pal.ink(line)
		if i == 0 && beside {
			text = questionRightAlign(text, a.pal.dim(aside), width-len(questionIndent))
		}
		out = append(out, text)
	}
	if reason := strings.TrimSpace(q.Reason); reason != "" {
		for _, line := range wrap(reason, inner) {
			out = append(out, questionIndent+a.pal.dim(line))
		}
	}
	if aside != "" && !beside {
		for _, line := range wrap(aside, inner) {
			out = append(out, questionIndent+a.pal.dim(line))
		}
	}
	return append(out, "")
}

// questionPageAside is the head's attribution: who is asking, as a verb, and
// what is stopped on the answer and what is not.
func questionPageAside(q session.Question) string {
	parts := make([]string, 0, 2)
	if asker := questionAskerWord(q.Asker); asker != "" {
		parts = append(parts, asker+questionAsksWord)
	}
	if blocking := questionBlockingWord(q.Blocking); blocking != "" {
		parts = append(parts, blocking)
	}
	return strings.Join(parts, questionSep)
}

// questionRightAlign puts an aside at the right edge of a row, or leaves the row
// alone where there is no room for it. It measures the PAINTED strings, because
// that is what the terminal draws.
func questionRightAlign(left, right string, width int) string {
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// questionPageListWant is how wide the left pane would like to be: its widest
// answer row, or the widest thing the asker showed for the whole decision.
// [besideSplit] holds it to half the page.
func (a *app) questionPageListWant(width int) int {
	q := a.qroom.head.question
	room := max(width/2, 1)
	lead := questionPanelLead(q, room)
	gutter := len(questionPanelGap) + len(questionPanelGap)
	// Every answer's word stands in one column as wide as the widest of them
	// ([app.questionPanelRow]'s pad), and the pick's mark with its word stands
	// at the right edge of the list — so the list asks for both together.
	words := 0
	for _, option := range q.Options {
		words = max(words, ansi.StringWidth(strings.TrimSpace(option.Label)))
	}
	want := gutter + lead + words + len(questionPanelGap)
	if q.Pick != nil {
		want += 2 + ansi.StringWidth(a.icon(tokens.GRecommended)+" "+questionRecommendedWord) + 2
	}
	if a.questionRoomTakesOther() {
		want = max(want, gutter+lead+ansi.StringWidth(questionPanelOtherWord)+len(questionPanelGap))
	}
	for _, block := range q.Attach {
		want = max(want, len(questionIndent)+questionBlockWant(block)+1)
	}
	return want
}

// questionBlockWant is how wide a block would like to be drawn: its widest
// line, for the kinds whose lines are the drawing. Prose wraps to whatever it
// is given, so it asks for nothing.
func questionBlockWant(block session.Block) int {
	widest := ansi.StringWidth(strings.TrimSpace(block.Title))
	switch block.Kind {
	case session.BlockDiagram, session.BlockDiff:
		for _, line := range strings.Split(block.Body, "\n") {
			widest = max(widest, ansi.StringWidth(expandTabs(line)))
		}
	case session.BlockTable:
		for _, row := range block.Rows {
			w := 0
			for _, cell := range row {
				w += ansi.StringWidth(strings.TrimSpace(cell)) + 2
			}
			widest = max(widest, w)
		}
	case session.BlockLayout:
		w := 0
		for _, pane := range block.Rows {
			w += questionPaneWidest(pane) + 1
		}
		widest = max(widest, w)
	}
	return widest
}

// questionPageList is the left pane: the answers, then what the asker showed for
// the whole decision under its one dim title.
//
// THE ROWS ARE THE PANEL'S OWN ([app.questionPanelOption]), so the list a person
// opened the page from and the list on the page are one list drawn twice: the
// same pointer, the same key, the same word, the same `◆ recommended`.
func (a *app) questionPageList(left int) ([]string, []int) {
	room := a.qroom
	q := a.questionRoomShown()
	inner := max(left-len(questionPanelGap), 1)
	listRoom := max(inner-2*len(questionPanelGap), 1)
	pad := 0
	for _, option := range q.question.Options {
		if w := ansi.StringWidth(strings.TrimSpace(option.Label)); w <= listRoom/2 {
			pad = max(pad, w)
		}
	}
	out := make([]string, 0, len(q.question.Options)+8)
	at := make([]int, 0, cap(out))
	for i, option := range q.question.Options {
		for _, line := range a.questionPanelOption(q, i, option, pad, listRoom, questionBeside) {
			out, at = append(out, questionPanelGap+line), append(at, i)
		}
	}
	if a.questionRoomTakesOther() {
		other := questionOtherAt(q.question)
		focused := room.focus == other
		for _, line := range a.questionPanelRow(q, itoa(other+1), "", questionPanelOtherWord,
			"", "", pad, listRoom, focused, a.questionHovering(other)) {
			out, at = append(out, questionPanelGap+line), append(at, other)
		}
	}
	for _, line := range a.questionPageAttach(left) {
		out, at = append(out, line), append(at, -1)
	}
	return out, at
}

// questionPageAttach is what the asker showed for the WHOLE decision, as opposed
// to what hangs off one answer: one dim title, and the blocks under it.
//
// IT IS UNDER THE LIST AND NOT IN THE PANE, because the pane changes with the
// pointer and this does not — it is the same evidence whichever answer is being
// weighed, so it stands with the thing that stays still.
func (a *app) questionPageAttach(width int) []string {
	room := a.qroom
	if room == nil || len(room.head.question.Attach) == 0 {
		return nil
	}
	inner := max(width-len(questionIndent), 1)
	out := []string{"", questionIndent + a.pal.dim(fit(questionAttachWord, inner))}
	for i, block := range room.head.question.Attach {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, a.questionBlockRows(block, len(questionIndent), width)...)
	}
	return out
}

// questionPagePane is the right pane: the evidence of the answer the pointer is
// on, and the person's own notes on it — the comment `c` wrote and the one
// thing `?` asked.
func (a *app) questionPagePane(width int) []string {
	room := a.qroom
	inner := max(width-2*len(questionPanelGap), 1)
	q := room.head.question
	var rows []string
	if room.focus >= len(q.Options) {
		rows = []string{a.pal.dim(fit(questionPageOtherWord, inner))}
	} else {
		rows = a.questionEvidenceRows(q, room.focus, inner, true)
		if notes := a.questionNoteRows(questionOptionKeyAt(q, room.focus), 0, inner); len(notes) > 0 {
			rows = append(append(rows, ""), notes...)
		}
	}
	out := make([]string, 0, len(rows))
	for _, line := range rows {
		out = append(out, questionPanelGap+line)
	}
	return out
}

// questionPageColumn is the page as one column: the compare table, a shape, or
// the answers with the one the pointer is on unfolded under its row, and what
// the asker showed for the whole decision after them.
func (a *app) questionPageColumn(width int) ([]string, []int) {
	room := a.qroom
	out := make([]string, 0, 32)
	at := make([]int, 0, 32)
	prose := func(lines []string) {
		for _, line := range lines {
			out, at = append(out, line), append(at, -1)
		}
	}
	switch {
	case room.compare:
		prose(a.questionCompareRows(width))
		return out, at
	case room.input.kind != session.InputNone:
		// A STRUCTURED INPUT REPLACES THE ANSWERS AND NEVER SITS UNDER THEM.
		// DESIGN.md's ladder puts structured input one rung BELOW the question —
		// blanks, a checklist, this-or-this, a dial are the answer itself, not a
		// garnish on a list of answers.
		prose(a.questionInputRows(width))
		prose(a.questionPageAttach(width))
		return out, at
	}
	q := a.questionRoomShown()
	inner := max(width-len(questionPanelGap), 1)
	listRoom := max(inner-2*len(questionPanelGap), 1)
	pad := 0
	for _, option := range q.question.Options {
		if w := ansi.StringWidth(strings.TrimSpace(option.Label)); w <= listRoom/2 {
			pad = max(pad, w)
		}
	}
	lead := len(questionPanelGap) + questionPanelLead(q.question, listRoom)
	indent := strings.Repeat(" ", lead)
	for i, option := range q.question.Options {
		for _, line := range a.questionPanelOption(q, i, option, pad, listRoom, questionRowOnly) {
			out, at = append(out, questionPanelGap+line), append(at, i)
		}
		if i != room.focus {
			continue
		}
		// UNDER THE POINTER, THE WHOLE OF IT: the evidence the pane would show,
		// less the word its own row is carrying — and, here only, the person's
		// own notes on it. The evidence is prose and pictures, so its rows press
		// nothing.
		under := a.questionEvidenceRows(q.question, i, max(width-lead-1, 8), false)
		under = append(under, a.questionNoteRows(questionOptionKeyAt(q.question, i), 0, max(width-lead-1, 8))...)
		for _, line := range under {
			out, at = append(out, indent+line), append(at, -1)
		}
		if len(under) > 0 {
			out, at = append(out, ""), append(at, -1)
		}
	}
	if a.questionRoomTakesOther() {
		other := questionOtherAt(q.question)
		for _, line := range a.questionPanelRow(q, itoa(other+1), "", questionPanelOtherWord,
			"", "", pad, listRoom, room.focus == other, a.questionHovering(other)) {
			out, at = append(out, questionPanelGap+line), append(at, other)
		}
		if room.focus == other {
			out, at = append(out, indent+a.pal.dim(fit(questionPageOtherWord, max(width-lead, 1)))), append(at, -1)
		}
	}
	prose(a.questionPageAttach(width))
	return out, at
}

// ─────────────────────────────────────────────────────────────────────────────
// The geometry, on room.go's own terms.

// questionRoomWindow is the page's body region. It is exactly `height` rows
// where the page has two panes, whose seam runs to the foot; one column hangs
// from the top with the slack below it, exactly as a short conversation does.
func (a *app) questionRoomWindow(width, height int) ([]row, int) {
	if height <= 0 {
		return nil, 0
	}
	rows := a.questionRoomView(width, height)
	return rows, max(height-len(rows), 0)
}

// questionRoomRows is the body region at the frame's own height.
func (a *app) questionRoomRows(width int) []row {
	return a.questionRoomView(width, a.viewHeight())
}

// questionRoomShowFocus KEEPS THE ANSWER THE POINTER IS ON IN VIEW, and it is the
// one place the list's window is moved for anything other than the wheel.
//
// An arrow that walks the pointer below the fold with nothing moving on screen
// is a key that reads as doing nothing (lane F measured it on the old page, where
// the window was moved only by the wheel). It scrolls the LEAST it can: a row
// already on screen does not move, and one off it is brought just inside the
// edge it left by — the reading position a person built is kept, not
// re-anchored under them.
//
// AND THE EVIDENCE PANE STARTS AT ITS TOP, because the pointer moving is the
// pane's content changing: an offset kept from the last answer's diagram would
// open the next answer's evidence halfway down.
func (a *app) questionRoomShowFocus() {
	room := a.qroom
	height := a.viewHeight()
	if room == nil || height <= 0 {
		return
	}
	room.detail, room.dirty = 0, true
	lay := a.questionRoomLay(a.bodyWidth(), height)
	first, last := -1, -1
	for i, of := range lay.listAt {
		if of != room.focus {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first < 0 {
		return
	}
	// In one column the answer's unfolded evidence is part of its reading: it
	// is kept in view with its row where it fits, and the row wins where it
	// does not.
	if lay.seam < 0 {
		for i := last + 1; i < len(lay.listAt) && lay.listAt[i] < 0 && i-first < lay.region; i++ {
			if strings.TrimSpace(ansi.Strip(lay.list[i])) == "" && i+1 < len(lay.listAt) && lay.listAt[i+1] >= 0 {
				break
			}
			last = i
		}
	}
	offset := questionWindowStart(room.offset, len(lay.list), lay.region)
	switch {
	case first < offset:
		offset = first
	case last >= offset+lay.region:
		offset = min(last-lay.region+1, first)
	}
	room.offset = questionWindowStart(offset, len(lay.list), lay.region)
}

// questionRoomSpotAt is which answer a pointer at (x, y) reaches, and false
// where it reaches none.
//
// IT IS ASKED ABOUT THE COLUMN AS WELL AS THE ROW, because the page has two
// panes on one row: a press on the left of the seam is an answer's row, and a
// press on the right is the evidence beside it — prose and pictures, which have
// no gesture on this surface (app.go's [app.press]). A click aimed at a diagram
// that answered the question would be the worst press on the page.
func (a *app) questionRoomSpotAt(x, y int) (int, bool) {
	room := a.qroom
	top := a.bodyTop()
	if room == nil || top < 0 {
		return 0, false
	}
	a.questionRoomRows(a.bodyWidth())
	at := y - top
	if at < 0 || at >= len(room.spots) {
		return 0, false
	}
	spot := room.spots[at]
	if spot.at < 0 || x < spot.from || x >= spot.to {
		return 0, false
	}
	return spot.at, true
}

// questionRoomPress resolves a click on the page and reports whether it took
// one.
//
// ONE PRESS MOVES ONTO AN ANSWER AND THE NEXT ONE TAKES IT, which is the only
// shape that can be both discoverable and safe. The first press puts the pointer
// on that answer — the same thing `↓` does, and the pane beside it shows that
// answer's evidence — and a press on the answer the pointer is already standing
// on is `enter`. A page where the first click answered would answer a question
// with evidence somebody had not read yet.
//
// A PRESS ANYWHERE ELSE FALLS THROUGH AND DOES NOTHING ([app.questionRoomSpotAt]).
func (a *app) questionRoomPress(x, y int) (tea.Cmd, bool) {
	room := a.qroom
	if room == nil {
		return nil, false
	}
	// THE SETTLE GUARD IS THE POINTER'S TOO. A click that arrived less than
	// [questionSettle] after the page was drawn was aimed at whatever was on
	// screen before it, and a mouse is no better at stopping than a hand is.
	if a.now().Sub(room.shown) < questionSettle {
		return nil, true
	}
	at, ok := a.questionRoomSpotAt(x, y)
	if !ok {
		return nil, false
	}
	if at != room.focus {
		room.focus, room.reading = at, false
		a.questionRoomShowFocus()
		a.questionRoomTouched()
		return nil, true
	}
	a.questionRoomTouched()
	return a.questionRoomEnter(), true
}

// questionRoomScroll moves one of the page's two windows: the evidence pane
// where the pointer is over it, and the list (or the one column) everywhere
// else.
func (a *app) questionRoomScroll(x, delta int) {
	room := a.qroom
	if room == nil {
		return
	}
	width, height := a.bodyWidth(), a.viewHeight()
	lay := a.questionRoomLay(width, height)
	if lay.seam >= 0 && x > lay.seam {
		room.detail = questionWindowStart(room.detail+delta, len(lay.pane), lay.region)
	} else {
		room.offset = questionWindowStart(room.offset+delta, len(lay.list), lay.region)
	}
	a.questionRoomTouched()
}
