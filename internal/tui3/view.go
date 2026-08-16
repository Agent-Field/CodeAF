package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// THE FRAME, AS OF THE STATUS-DOWN WAVE.
//
//	conversation   everything that has happened, scrolled
//	───────────    a thin dim rule: below it is your business
//	(consent)      the approval question, when one is waiting
//	(follow)       after yield · N, when something is queued
//	(blank)
//	 › the draft   one row, or up to six of a pasted block
//	(overlay)      the open list, when one is open
//	status         title · model · $cost · N% ctx · state        /help · ctrl+o
//
// The status bar used to be the FIRST row, and this wave moved it to the last.
// The reason is where a person's eye already is: on this surface everything
// that changes — the reply arriving, the calls running, the question being
// asked, the sentence being typed — happens at the bottom, and a line about
// what is happening was the one fact parked two feet away from all of it. A
// status line at the bottom is read in the same glance as the box above it,
// which is what makes it worth drawing at all. The footer hints went with it
// (the same line's right end), so the frame lost a whole row of chrome rather
// than moving one.
//
// The rule above the input is the only HORIZONTAL line this surface draws, and
// it draws one thing: where the conversation stops and where the person's own
// business starts. There are still no borders and there are not going to be.
//
// THE RAIL ARRIVED WITH THE TASKER, and it is the frame's one vertical seam:
// thirty columns on the right, drawn only while there is a task to stand in
// them and only on a frame wide enough to lend them (task.go's railFloor). It
// takes its columns from the CONVERSATION and from nothing else — the status
// row and the legend span the whole window, because they are about the window.
//
// A terminal too short for all of it gives up its breathing room first — the
// rule and the blank above the draft — and its status line last: what is
// happening and what you are typing are the two facts a one-inch window still
// has to carry.

// inputPad is the one cell the draft block is inset by. It is what makes the
// box read as an object below the rule rather than as one more line of the
// transcript, and it is one cell because two would be a margin.
const inputPad = " "

// chromeKind says what one chrome row IS, for the pointer. The frame is the
// only thing that knows where these rows land on screen, so it is the only
// thing that answers the question — see [app.chrome].
type chromeKind uint8

const (
	chromeNone chromeKind = iota
	// chromeChoices is the consent block's offer line, which is interactive by
	// keyboard and hoverable by pointer.
	chromeChoices
	// chromeOverlay is one row of whichever list is open; index is its position
	// in that list's own rows.
	chromeOverlay
	// chromeWelcome is one row of the welcome box; index is its position within
	// the box, which [app.welcomeSlotAt] turns back into a recent session.
	chromeWelcome
	// chromeStatus is one row of the HUD's status line; index is its position
	// within that block (0 is the identity's row, 1 the telemetry's on a narrow
	// frame). It is hoverable by nothing and pressable in one place: the model
	// segment, whose columns the render records (render.go's [app.identityParts]).
	chromeStatus
)

// chromeRow is one row of the frame below the conversation.
type chromeRow struct {
	kind  chromeKind
	index int
}

// View declares the frame and the terminal state it wants. Alt screen, because
// the conversation scrolls under our own anchor; ALL motion, because a tool
// line is a thing you click and now a thing you hover — cell motion would
// deliver the wheel and the press and nothing in between.
func (a *app) View() tea.View {
	frame, caretX, caretY := a.frame()
	v := tea.NewView(frame)
	v.AltScreen = true
	// THE MOUSE IS OPT-IN. Reporting it — at any motion level — makes the app
	// the owner of every drag, and the terminal's native text selection is
	// dead from that moment; in an alt-screen app there is no scrollback to
	// fall back on. The setting (ui.mouse, default off) is the person's call
	// between hover/click here and select-to-copy everywhere, and off is the
	// default because copy is the more fundamental act: every key the mouse
	// would save already exists, and no key replaces a dead selection.
	if a.mouse {
		v.MouseMode = tea.MouseModeAllMotion
	}
	// FOCUS REPORTING IS ON, and it buys exactly one thing: a turn that ends on
	// a window nobody is looking at can say so (notify.go). It costs two escape
	// sequences at startup and a message per alt-tab, and a terminal that does
	// not speak it simply never sends one — which the notification treats as
	// "focused", i.e. as silence.
	v.ReportFocus = true
	// Bracketed paste stays ON — a pasted stack trace arrives as one
	// tea.PasteMsg with its newlines intact instead of as a stack of enters,
	// each of which would submit. v2 enables it unless this says otherwise, and
	// it says so out loud because the default is the thing being relied on.
	v.DisableBracketedPasteMode = false
	v.Cursor = &tea.Cursor{
		Position: tea.Position{X: caretX, Y: caretY},
		Shape:    tea.CursorBar,
		Blink:    true,
	}
	return v
}

// frame is the whole screen and where the caret sits in it.
func (a *app) frame() (string, int, int) {
	width, height := a.size()
	// The settings panel is the one thing on this surface that takes the whole
	// frame, and it takes it WHOLE: no conversation above it, no input line
	// under it, nothing of the frame below showing through at the edges
	// (settings.go). A sheet drawn into a viewport is a sheet you read past.
	if a.sheet.open {
		lines, _, caretX, caretY := a.sheetFrame(width, height)
		return strings.Join(lines, "\n"), caretX, caretY
	}
	chrome, _, caretX, caretRow := a.chrome(width)
	// THE FOCUS HEADER IS THE FRAME'S ONE PINNED ROW ABOVE the conversation, and
	// it spans the WHOLE window for the reason the status row does: it is about
	// the window — which page this is, and how to leave it — rather than about
	// the transcript, so it is not one of the columns the rail borrows from
	// (room.go).
	head := a.roomHead(width)
	// THE RAIL COSTS COLUMNS, AND IT COSTS THEM HERE. The conversation is laid
	// out at [app.bodyWidth] — everything below this line, the wheel and the
	// hit-testing included, resolves through the same number — and the chrome is
	// drawn at the FULL width, because the status line and the legend are about
	// the whole window rather than about the transcript (task.go).
	view := a.viewHeight()
	body, pad := a.bodyRows(a.bodyWidth(), view)
	rail := a.railRows(view)

	rows := make([]string, 0, height)
	if head != "" {
		rows = append(rows, head)
	}
	railAt := func(i int) string {
		if i < len(rail) {
			return rail[i]
		}
		return ""
	}
	for i := 0; i < pad; i++ {
		rows = append(rows, a.railJoin("", railAt(i)))
	}
	for i, r := range body {
		rows = append(rows, a.railJoin(r.text, railAt(pad+i)))
	}
	rows = append(rows, chrome...)
	// A frame taller than the terminal loses rows from the TOP: the chrome is
	// the tail, and everything the caret's row is counted back through is in it.
	if len(rows) > height {
		rows = rows[len(rows)-height:]
	}
	caretY := height - len(chrome) + caretRow
	if caretY < 0 {
		caretY = 0
	}
	if caretY >= height {
		caretY = height - 1
	}
	return strings.Join(rows, "\n"), caretX, caretY
}

// chrome is everything below the conversation: the rows, what each row is for
// the pointer, the caret's column and the caret's row within the block.
//
// It is ONE function because it is one geometry. The frame draws these rows,
// [app.viewHeight] subtracts their count from the conversation, and
// [app.chromeAt] resolves a pointer to one of them — three questions that must
// never be able to disagree about where the input line is.
func (a *app) chrome(width int) ([]string, []chromeRow, int, int) {
	_, height := a.size()
	roomy := height >= 6

	rows := make([]string, 0, 8)
	marks := make([]chromeRow, 0, 8)
	add := func(text string, mark chromeRow) {
		rows = append(rows, text)
		marks = append(marks, mark)
	}

	// The welcome box sits ABOVE the rule, which is where it belongs: the rule
	// is the seam between what happened and what you are about to say, and the
	// box is about neither — it is what there is instead of a conversation
	// (welcome.go).
	for i, line := range a.welcomeRows(width) {
		add(line, chromeRow{kind: chromeWelcome, index: i})
	}
	if roomy {
		// THE RULE IS A LEGEND NOW: the same one line, with where you are written
		// into it (render.go). It degrades back to the plain rule on a frame with
		// no room for a label.
		add(a.legend(width), chromeRow{})
	}
	for i, line := range a.consentRows(width) {
		// The offer is the second row of the block, and it is the only row of it
		// a pointer can be over — the call above it is a transcript row that
		// happens to be repeated here, and the rule and the count below it are
		// statements.
		mark := chromeRow{}
		if i == consentOfferRow {
			mark = chromeRow{kind: chromeChoices}
		}
		add(line, mark)
	}
	if line := a.followRow(width); line != "" {
		add(line, chromeRow{})
	}
	if roomy {
		add("", chromeRow{})
	}

	input, caretX, caretRow := a.inputBlock(width - len(inputPad))
	// THE BOX IS THE REDIRECT LANE while a proposal is open: the placeholder is
	// applied to the block the input already rendered, because the hint slot
	// inside it belongs to the picker's filter and the two are never up together
	// (task.go).
	input = a.redirectLane(input, width-len(inputPad))
	// AND THE BOX TALKS TO THE NODE while a room is open: same box, same rules,
	// a placeholder that says who is listening (room.go). The two lanes cannot be
	// up together — a proposal is a question about work that has not started, a
	// room is a page for work that has — and [app.roomSteerLaneRows] defers to
	// the one above it rather than assuming so.
	input = a.roomSteerLaneRows(input, width-len(inputPad))
	caretRow += len(rows)
	for _, line := range input {
		add(inputPad+line, chromeRow{})
	}
	for i, line := range a.overlayRows(width, a.overlayHeight()) {
		add(line, chromeRow{kind: chromeOverlay, index: i})
	}
	for i, line := range a.statusRow(width) {
		add(line, chromeRow{kind: chromeStatus, index: i})
	}

	return rows, marks, caretX + len(inputPad), caretRow
}

// statusRow is the HUD's status row — one row, or two on a narrow frame where
// the telemetry stops sharing with the identity (render.go's [app.statusRows])
// — with the reasoning level on the model segment:
// "anthropic/claude-sonnet-4.5:high" where a level has been dialled, and the
// bare model id — the line exactly as it was — where none has.
//
// The level belongs on that line because it is a fact about what the next
// request will cost and how long it will take, and the model segment is where a
// person already looks for both. It is spelled with a colon rather than a fourth
// segment for the same reason it is spelled that way on the picker row: it is
// not a thing beside the model, it is how this model is being run.
//
// The splice happens by LENDING the model field its suffixed form for the length
// of one call. [app.status] reads a.model directly (render.go), the frame is
// drawn on the model goroutine one row at a time, and the alternative is a
// second copy of the status line's segment layout — width budget, narrow-frame
// dropping and all — kept in step with the first by nothing but attention.
func (a *app) statusRow(width int) []string {
	level := a.reasoningFor(a.model)
	if level == "" || a.model == "" {
		return a.statusRows(width)
	}
	id := a.model
	a.model = id + ":" + level
	defer func() { a.model = id }()
	return a.statusRows(width)
}

// chromeAt resolves a screen row to the chrome row drawn on it. It is the
// pointer's half of [app.chrome] and it asks the same function the frame does,
// so a hover cannot land on a row the frame drew somewhere else.
func (a *app) chromeAt(y int) (chromeRow, bool) {
	width, height := a.size()
	_, marks, _, _ := a.chrome(width)
	at := y - (height - len(marks))
	if at < 0 || at >= len(marks) {
		return chromeRow{}, false
	}
	return marks[at], true
}

// chromeHeight is how many rows the frame spends below the conversation.
func (a *app) chromeHeight() int {
	width, height := a.size()
	// The status (one row, or two when the telemetry wraps), the input block,
	// and whatever the two optional blocks, the open list and the welcome box
	// are holding.
	n := a.statusHeight(width) + a.inputHeight() + a.overlayHeight() + a.consentHeight() +
		a.followHeight() + a.welcomeHeight()
	if height >= 6 {
		n += 2 // the rule, and the blank above the draft
	}
	return n
}

// rule is the one line this surface draws: the seam between what happened and
// what you are about to say.
func (a *app) rule(width int) string {
	if width < 1 {
		return ""
	}
	return a.pal.dim(strings.Repeat("─", width))
}

// size is the frame's working size: the terminal's, or the classic default
// when nobody has said. A headless boot and a terminal that answers zero are
// the same case, and drawing into a zero-by-zero frame would mean drawing
// nothing at all.
func (a *app) size() (int, int) {
	width, height := a.width, a.height
	if width < 8 {
		width = 8
	}
	if height < 1 {
		height = 1
	}
	return width, height
}

// window is the visible slice of the row list and the padding above it.
//
// Every geometric question on this surface goes through here — what the frame
// draws, where the wheel lands, which row a click hit — so a row's position on
// screen has exactly ONE definition. The padding is why a short conversation
// sits next to the input, the way a terminal session grows upward, instead of
// hanging under the top of the screen.
func (a *app) window(width, height int) ([]row, int) {
	if height <= 0 {
		return nil, 0
	}
	rows := a.visible(width)
	offset := a.offsetFor(len(rows), height)
	end := min(offset+height, len(rows))
	visible := rows[offset:end]
	if pad := height - len(visible); pad > 0 {
		return visible, pad
	}
	return visible, 0
}

// bodyRows is what the frame draws above the chrome: the live conversation, or
// the frozen snapshot while copy mode is up (copymode.go).
//
// It is the ONE place the two can be swapped, and the swap is deliberately not
// in [app.window]: window is what the wheel, the click hit-testing and the
// selection all resolve through, and every one of those questions is about the
// LIVE conversation whatever is on screen. A frozen view answers "what is
// drawn" and nothing else, which is why the pointer paths return early while it
// is up rather than being redirected here.
// THE ROOM IS THE THIRD ANSWER, and it is the whole of what "a task is a place"
// costs the frame: while one is open the body region draws that node's page
// instead of the conversation (room.go). Everything else about the frame is
// unchanged — the rail is still beside it, the chrome is still under it, the
// turn underneath is still streaming into a transcript nobody is looking at —
// which is what makes esc restore the conversation exactly.
func (a *app) bodyRows(width, height int) ([]row, int) {
	if a.copy.on {
		return a.copyRows(width, height)
	}
	if a.roomOpen() {
		return a.roomWindow(width, height)
	}
	return a.window(width, height)
}

// bodyTop is the screen row the conversation starts on, or -1 when the frame is
// too short to have one. The conversation now opens the frame — the status bar
// that used to sit above it moved to the bottom — so the answer is zero
// wherever there is a conversation at all, and one under the focus header a room
// pins above it (room.go).
func (a *app) bodyTop() int {
	if a.viewHeight() <= 0 {
		return -1
	}
	return a.headHeight()
}

// rowAt resolves a screen line to the row drawn on it.
//
// A ROOM ANSWERS NOTHING HERE. While one is open the transcript's rows are not
// on screen, and this is what both the click hit-testing and the hover resolve
// through — so a pointer over a room would otherwise brighten and expand tool
// calls from a conversation the person cannot see. The room's own rows are
// deliberately not offered in their place: a room row is a reading, and it has
// nothing to open (room.go).
func (a *app) rowAt(y int) (row, bool) {
	if a.roomOpen() {
		return row{}, false
	}
	top := a.bodyTop()
	if top < 0 {
		return row{}, false
	}
	body, pad := a.window(a.bodyWidth(), a.viewHeight())
	at := y - top - pad
	if at < 0 || at >= len(body) {
		return row{}, false
	}
	return body[at], true
}

// viewHeight is how many rows the conversation gets: everything the chrome did
// not take.
//
// The subtraction belongs here rather than at the call sites because this is
// the number every geometric question is answered from — what the frame draws,
// where the wheel lands, which row a click hit. An overlay the layout knew
// about but the hit-testing did not would deliver clicks to rows twelve lines
// from where they were drawn.
func (a *app) viewHeight() int {
	_, height := a.size()
	if body := height - a.chromeHeight() - a.headHeight(); body > 0 {
		return body
	}
	return 0
}

// headHeight is what the pinned focus header costs the body region: one row
// while a room is open on a frame with the height to spare, and nothing
// otherwise (room.go).
//
// It is subtracted HERE, in the number every geometric question resolves
// through, rather than at the frame — a header the frame drew and the scrolling
// did not know about would put the room's last row under the input box.
func (a *app) headHeight() int {
	_, height := a.size()
	// The same floor the rule and the blank above the draft stand on: a terminal
	// too short for breathing room is too short for a header, and what is
	// happening is still on the status line.
	if a.room == nil || height < 6 {
		return 0
	}
	return 1
}

func (a *app) page() int {
	if p := a.viewHeight() - 1; p > 1 {
		return p
	}
	return 1
}

// offsetFor resolves the scroll position for a rendered length. Sticking is
// resolved here rather than stored, so a turn that streams six lines while the
// reader is at the bottom keeps them at the bottom without anybody recomputing
// an offset per delta.
func (a *app) offsetFor(total, height int) int {
	bottom := total - height
	if bottom < 0 {
		bottom = 0
	}
	if a.stick || a.offset > bottom {
		return bottom
	}
	if a.offset < 0 {
		return 0
	}
	return a.offset
}

// scroll moves the window by delta rows and re-decides whether the reader is
// following the live edge. Reaching the bottom re-arms sticking: leaving it
// off would mean a reader who scrolled up once never sees a new reply again.
func (a *app) scroll(delta int) {
	height := a.viewHeight()
	total := len(a.visible(a.bodyWidth()))
	bottom := total - height
	if bottom < 0 {
		bottom = 0
	}
	at := a.offsetFor(total, height) + delta
	switch {
	case at >= bottom:
		a.offset, a.stick = bottom, true
	case at <= 0:
		a.offset, a.stick = 0, false
	default:
		a.offset, a.stick = at, false
	}
}

// reveal scrolls just enough to put an entry's first row on screen. It is what
// keeps ↑/↓ selection from walking off the top of the window.
func (a *app) reveal(entry int) {
	height := a.viewHeight()
	if height <= 0 {
		return
	}
	rows := a.visible(a.bodyWidth())
	at := -1
	for i, r := range rows {
		if r.entry == entry {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	offset := a.offsetFor(len(rows), height)
	switch {
	case at < offset:
		a.offset, a.stick = at, false
	case at >= offset+height:
		a.offset, a.stick = at-height+1, false
	}
}

// clampScroll keeps the offset legal after a resize.
func (a *app) clampScroll() {
	a.offset = a.offsetFor(len(a.visible(a.bodyWidth())), a.viewHeight())
}

// follow is what every append calls: content grew, and a reader at the live
// edge stays at the live edge.
func (a *app) follow() {
	if a.stick {
		a.offset = 0 // resolved from the bottom by offsetFor
	}
}
