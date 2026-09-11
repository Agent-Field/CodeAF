package tui3

// THE RECORD BESIDE THE LIST.
//
// The tasks place is a column of names. A name is what a person scans, and it is
// almost never what they came for: the questions they arrive with are "what did
// this do", "what did it cost", "where did it leave my files" and, on a row that
// is their call, "do I accept it". Every one of those used to cost a keypress
// into the record card, a read, and an `esc` back to a list they then had to find
// their place in again — so walking ten rows of a morning's work was ten opens.
//
// SO A WIDE FRAME SPLITS: the list keeps the left, a dim seam divides it, and the
// right is the cursor row's record, following the cursor. Nothing is opened and
// nothing is lost — enter still opens the full card, esc still comes back — and a
// person reading down the list reads the answers as they go.
//
// ── THE LAWS ──
//
//   - ONE FUNCTION DECIDES WHETHER THERE IS A PANE AT ALL ([taskPaneCols]).
//     Under [taskPaneFloor] the pane is ABSENT rather than blank and the list's
//     cursor row grows its own line instead; a second opinion about that width is
//     how a frame comes to draw a seam with nothing behind it.
//
//   - IT IS NOT A SECOND RENDERER. Every row it draws is built by the record's own
//     builders (taskrecord.go's `taskRecord…` half), so the pane and the card
//     cannot say two different things about one piece of work.
//
//   - THE SEAM IS THE ONE THIS SURFACE ALREADY DRAWS. [railSeam] is the rule the
//     roster and the room split on; DESIGN-LANGUAGE bans borders, and a second
//     spelling of one stroke would be a second border to keep in step.
//
//   - NOTHING ON THE DRAW PATH TOUCHES A DISK. The report under the title is read
//     off the loop, once per row, after the cursor has SETTLED — a held `↓` moving
//     thirty times a second fires no reads at all ([app.taskPaneFollow]) — and
//     what has been read is kept per row for as long as the place is open.
//
//   - UNTIL THE REPORT ARRIVES ITS SLOT IS EMPTY. The emptiness law applied to a
//     fact that is merely late: no spinner, no `loading`, and no row that appears
//     and then changes its mind.

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── how wide, and whether at all ────────────────────────────────────────────

const (
	// taskPaneFloor is the frame at which the body splits. It is the width at
	// which BOTH halves are still worth having: the pane's own floor
	// ([taskPaneThin]) plus the seam, plus the sixty-odd cells a list of titles
	// with two fact columns needs before it starts cutting names — under that the
	// split buys a preview by making the thing being previewed unreadable.
	taskPaneFloor = 110
	// taskPaneShare is the share of the frame the pane takes, in hundredths. The
	// list is the subject and the pane is the answer about one row of it, so the
	// list keeps the larger half at every width.
	taskPaneShare = 40
	// taskPaneThin and taskPaneWide clamp that share. A pane narrower than the
	// floor cannot hold a facts line without wrapping it into nonsense; one wider
	// than the ceiling is a column of forty-character sentences with a hand's
	// width of air beside them, and the cells are worth more to the names.
	taskPaneThin = 44
	taskPaneWide = 64
)

// taskPaneCols is THE ONE FUNCTION THAT DECIDES how wide the pane is, and zero
// says there is none — which is what the list asks before it grows the cursor
// row's own line instead.
func taskPaneCols(width int) int {
	if width < taskPaneFloor {
		return 0
	}
	cols := width * taskPaneShare / 100
	if cols < taskPaneThin {
		cols = taskPaneThin
	}
	if cols > taskPaneWide {
		cols = taskPaneWide
	}
	return cols
}

// taskPaneOpen reports whether this frame splits. It is the same answer
// [taskPaneCols] gives, asked by the callers that only want the yes or no.
func taskPaneOpen(width int) bool { return taskPaneCols(width) > 0 }

// taskPaneList is the cells the LIST is drawn in on a split frame, and the whole
// width where there is no pane. One arithmetic, so the paint, the hit map and the
// pointer cannot disagree about where the seam is.
func taskPaneList(width int) int {
	cols := taskPaneCols(width)
	if cols == 0 {
		return width
	}
	return width - cols - ansi.StringWidth(railSeam)
}

// taskPaneShowing is THE ONE PREDICATE the list asks before it grows the cursor
// row's own line: is the record already beside it?
//
// IT ASKS THE FRAME AND NEVER THE WIDTH IT WAS HANDED. [tasksPlace.body] is
// given the cells the LIST is drawn in, which on a split frame is already short
// of the floor — so a list that asked [taskPaneOpen] of its own width would
// decide there is no pane on exactly the frames that have one, and draw the
// grown line beside it. One question, one answer, asked of the whole frame.
func (a *app) taskPaneShowing() bool {
	width, _ := a.size()
	return taskPaneOpen(width)
}

// taskSheetListWidth is that same answer asked of the frame this window is on,
// which is what every "which row is the cursor on" question has to lay the
// reading out at: a cursor that counted lines of a hundred-and-twenty-two-cell
// layout while the list was drawn in seventy-two would stand on a different row
// from the one under the band.
func (a *app) taskSheetListWidth() int {
	width, _ := a.size()
	return taskPaneList(width)
}

// ── one row of the split ────────────────────────────────────────────────────

// taskPaneVerb is one clause of the pane's verb line as it was DRAWN: the key it
// presses, the whole clause it spells, and the cells it occupies.
//
// THE CLAUSE OPENS WITH THE KEY, by construction at the one place these are made
// ([app.taskPaneVerbs]), which is what lets the paint find the key inside it
// without a second copy of the string to keep in step.
type taskPaneVerb struct {
	key      string
	text     string
	from, to int
}

// taskPaneHit is what one row of the pane answers to a press. Only the verb line
// answers anything; every other row of a preview is read.
type taskPaneHit struct{ verbs []taskPaneVerb }

// taskPaneRow is one row of the pane: the text and what it answers.
type taskPaneRow struct {
	text string
	hit  taskPaneHit
}

// taskSplitHit is what ONE ROW of a split body answers: the list's half on the
// left of the seam, and the pane's on the right.
//
// IT IS ONE HIT AND NOT TWO MAPS because the frame hands back one hit per row
// (pages.go's [placeRow]), and the column the pointer is in is what chooses
// between the halves ([app.taskSheetPress]).
type taskSplitHit struct {
	list taskSheetHit
	pane taskPaneHit
}

// taskSheetHitsOf is the list's half of a frame's hit map, whether or not that
// frame was split. It is what [app.taskSheetFrame] hands the pointer, so every
// reader of a list hit reads it the one way.
func taskSheetHitsOf(hits []placeHit) []taskSheetHit {
	out := make([]taskSheetHit, len(hits))
	for i, hit := range hits {
		switch got := hit.(type) {
		case taskSplitHit:
			out[i] = got.list
		case taskSheetHit:
			out[i] = got
		}
	}
	return out
}

// taskPaneHitsOf is the same map read from the pane's side.
func taskPaneHitsOf(hits []placeHit) []taskPaneHit {
	out := make([]taskPaneHit, len(hits))
	for i, hit := range hits {
		if got, ok := hit.(taskSplitHit); ok {
			out[i] = got.pane
		}
	}
	return out
}

// ── the body, split ─────────────────────────────────────────────────────────

// taskSheetBody is the place's body: the list alone, or the list and the record
// beside it with the seam between them.
//
// THE LIST IS BUILT AT THE WIDTH IT IS DRAWN IN and never at the frame's, which
// is the whole reason the split happens here rather than in the paint: the
// reading fits names, facts and the family column against the room it was given
// ([tasksReading.lay]), and a list told it had a hundred and twenty-two cells
// while it was drawn in seventy-two would cut every row in the wrong place.
//
// AND THE PAGE'S HEAD SENTENCE KEEPS THE WHOLE FRAME. It is the one line here
// that is about the PLACE rather than about a row — how much is held, how far
// back the window reaches, and the shift-arrows that move it — and that control
// is bound only where it is drawn ([placeWindowFits], [tasksPlace.window]). A
// head cut to the list's half would unbind four keys on a frame wide enough to
// offer them, which is why the seam starts under it and not beside it.
func (a *app) taskSheetBody(width, room int) []placeRow {
	left := taskPaneList(width)
	if left == width || a.taskSheet.reading.held == 0 {
		// A PLACE WITH NOTHING IN IT SPENDS THE FRAME TEACHING WHAT IT IS
		// ([tasksTeach]), and a seam drawn beside that prose would be a rule with
		// nothing on either side of it.
		return a.taskSheet.body(a, width, room)
	}
	rows := a.taskSheet.body(a, left, room)
	r := a.tasksFiltered()
	pane := a.taskPaneRows(width-left-ansi.StringWidth(railSeam), room)
	seam := a.pal.dim(railSeam)
	out := make([]placeRow, 0, len(rows))
	// THE PANE IS SPENT FROM ITS OWN TOP, not from the body's. The head takes a
	// row of the frame and none of the pane, so a pane indexed by the body's row
	// would have its title drawn under the head and lost.
	next := 0
	for at, row := range rows {
		list, _ := row.hit.(taskSheetHit)
		if a.taskSheet.top+at == 0 {
			out = append(out, placeRow{
				text: r.headRow(width, a.pal),
				hit:  taskSplitHit{list: list},
			})
			continue
		}
		text, hit := "", taskPaneHit{}
		if next < len(pane) {
			text, hit = pane[next].text, pane[next].hit
		}
		next++
		out = append(out, placeRow{
			text: taskPanePad(row.text, left) + seam + text,
			hit:  taskSplitHit{list: list, pane: hit},
		})
	}
	return out
}

// taskPanePad fills one drawn row out to the cells the seam starts at. A row
// that stopped short would put the seam under the end of its own text, which is
// a rule that wanders down the frame.
func taskPanePad(text string, width int) string {
	if gap := width - ansi.StringWidth(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return text
}

// taskPaneRows is the pane itself: the cursor row's record, or the cursor
// conversation's, padded out to the room the body was given.
func (a *app) taskPaneRows(width, room int) []taskPaneRow {
	var rows []taskPaneRow
	switch {
	case width < 1 || room < 1:
	default:
		if chat, ok := a.taskSheetChat(); ok {
			rows = a.taskPaneChat(chat, width)
		} else if item, ok := a.taskSheetCurrent(); ok {
			rows = a.taskPaneRecord(item, width)
		}
	}
	// A FRAME TOO SHORT FOR THE WHOLE PREVIEW KEEPS ITS HEAD AND ITS VERBS: what
	// this row is, and what can be done about it. It is [app.taskCardFrame]'s own
	// trim, and it matters more here — the verb line is the LAST band, so a pane
	// cut from the bottom would drop the two answers and keep the file paths.
	if len(rows) > room && room > 1 {
		rows = append(rows[:1], rows[len(rows)-(room-1):]...)
	}
	for len(rows) < room {
		rows = append(rows, taskPaneRow{})
	}
	return rows[:room]
}

// ── the record, at pane width ───────────────────────────────────────────────

// taskPaneRecord is one piece of work as the pane draws it, in the order the
// spec fixes: what it is called, what it needs, what it cost, where it left the
// work, what it said, what it wrote, and what can be done about it.
//
// EVERY BAND IS THE RECORD'S OWN ([taskRecordBands] joins them, taskrecord.go
// builds them), and every band with nothing behind it is absent along with the
// blank line that would have separated it — the emptiness law, which on a preview
// is the difference between a short honest card and a tall one full of holes.
func (a *app) taskPaneRecord(item tasksItem, width int) []taskPaneRow {
	pal, entry := a.pal, item.entry
	var bands [][]string

	head := []string{fit(pal.bold(pal.ink(taskRecordWords(entry))), width)}
	glyph, glyphInk := tasksGlyph(item, pal)
	if word := item.status().RowWord(); word != "" {
		head = append(head, fit(glyphInk(glyph)+" "+pal.dim(word), width))
	}
	bands = append(bands, head)

	// WORK ANOTHER WINDOW IS RUNNING HAS NO RECORD TO PREVIEW, and the sentence a
	// person came for is where the work is — the card's own answer at its own
	// width (taskrecord.go's [app.taskCardAwayRows] states it).
	if item.away {
		var away []string
		for _, wrapped := range wrap(taskAwayCardWhere(item.window), width) {
			away = append(away, pal.ink(wrapped))
		}
		bands = append(bands, away)
		return taskPaneDraw(taskRecordBands(bands), nil)
	}

	if line := a.taskPaneFacts(entry); line != "" {
		facts := []string{fit(pal.dim(line), width)}
		if where := a.taskPaneWhere(entry); where != "" {
			facts = append(facts, fit(pal.dim(where), width))
		}
		bands = append(bands, facts)
	} else if where := a.taskPaneWhere(entry); where != "" {
		bands = append(bands, []string{fit(pal.dim(where), width)})
	}

	if report := a.taskPaneReport(entry); report != "" {
		var said []string
		for _, wrapped := range wrap(report, width) {
			said = append(said, pal.ink(wrapped))
		}
		bands = append(bands, said)
	}
	bands = append(bands, a.taskPaneFiles(entry, width))

	verbs := a.taskPaneVerbs(item)
	return taskPaneDraw(taskRecordBands(bands), a.taskPaneVerbLine(verbs, width))
}

// taskPaneDraw turns the record's rows and its verb line into pane rows: the
// body answers nothing, and the verb line answers its own clauses.
func taskPaneDraw(body []string, verbs *taskPaneRow) []taskPaneRow {
	rows := make([]taskPaneRow, 0, len(body)+2)
	for _, text := range body {
		rows = append(rows, taskPaneRow{text: text})
	}
	if verbs == nil {
		return rows
	}
	if len(rows) > 0 {
		rows = append(rows, taskPaneRow{})
	}
	return append(rows, *verbs)
}

// taskPaneFacts is the one line of figures: what the work wrote, what it cost,
// what it ran on, and how long ago it landed.
//
// IT IS THE CARD'S OWN FIGURES IN ONE LINE INSTEAD OF THREE. A card a hundred
// cells wide can afford a line each; a pane cannot, and the four facts a person
// scans are the same four either way. Each clause with nothing behind it is
// dropped rather than drawn as a zero, so a row that spent nothing and wrote
// nothing draws no line at all.
func (a *app) taskPaneFacts(entry session.TaskIndexEntry) string {
	var segs []string
	if entry.FilesChanged > 0 {
		segs = append(segs, itoa(entry.FilesChanged)+plural(" file", entry.FilesChanged))
	}
	if entry.Cost > 0 {
		segs = append(segs, dollars(entry.Cost))
	}
	if model := strings.TrimSpace(entry.Model); model != "" {
		segs = append(segs, model)
	}
	if !entry.EndedAt.IsZero() {
		segs = append(segs, session.TaskAgeWord(a.now().Sub(entry.EndedAt))+" ago")
	}
	return strings.Join(segs, railSep)
}

// taskPaneWhere is where the work was left: the branch it is on, and the word
// the ground ladder keeps for the copy of the repository it happened in.
//
// THE WORD IS THE ENGINE'S ([taskCardGroundWord] asks it), because the rung is
// the only thing that knows whether the directory was a worktree or a whole
// fork — and a surface that spelled its own answer would be a second source for
// one fact, which is the drift that card was already in.
func (a *app) taskPaneWhere(entry session.TaskIndexEntry) string {
	branch := taskRecordBranch(entry)
	if branch == "" {
		return ""
	}
	line := taskCardBranchWord + " " + branch
	if word := taskCardGroundWord(entry); word != taskCardPlaceWord {
		line += railSep + "in a " + word
	}
	return line
}

// taskPaneReport is the first paragraph of what the node said at the end, or ""
// while the journal has not been read yet.
//
// THE SLOT IS EMPTY AND NOT WAITING. A read that has not come back is a fact
// that is merely late, and a preview that grew a spinner under a title would be
// this surface saying something about progress it does not know.
func (a *app) taskPaneReport(entry session.TaskIndexEntry) string {
	tail, read := a.taskSheet.paneTail[tasksKeyOf(entry)]
	if !read {
		return ""
	}
	return taskRecordParagraph(tail)
}

// taskPaneFiles is up to [taskPaneFileRows] of the paths the work wrote, and the
// count of what would not fit.
//
// THE COUNT IS THE RECORD'S HONEST TOTAL and the list is what it kept: a node
// that wrote more paths than [session.TaskIndexEntry.Files] holds has a count
// larger than its list, and the `N more` is measured against the count so it is
// never a lie in either direction.
func (a *app) taskPaneFiles(entry session.TaskIndexEntry, width int) []string {
	if len(entry.Files) == 0 {
		return nil
	}
	shown := entry.Files
	if len(shown) > taskPaneFileRows {
		shown = shown[:taskPaneFileRows]
	}
	out := make([]string, 0, len(shown)+1)
	for _, path := range shown {
		out = append(out, fit(a.pal.muted(path), width))
	}
	if more := entry.FilesChanged - len(shown); more > 0 {
		out = append(out, fit(a.pal.dim(itoa(more)+taskPaneMoreWord), width))
	}
	return out
}

// taskPaneFileRows is how many paths the pane names before it counts the rest.
// Five is what fits beside a paragraph and a verb line on the shortest frame the
// pane is drawn on at all, and the count below them is what keeps the band
// honest at every longer one.
const taskPaneFileRows = 5

// The words the pane says, quoted in the manual exactly as they are spelled
// here.
const (
	// taskPaneMoreWord ends the file band on a row that wrote more than the pane
	// names. It is " more" and not " more files" because the paths above it have
	// already said what is being counted.
	taskPaneMoreWord = " more"
	// taskPaneOpenWord is what `enter` does from the list: the whole record card,
	// with this row's report scrollable under it.
	taskPaneOpenWord = "enter open"
	// taskPaneChatWord is that same key over a CONVERSATION's row, where it opens
	// the conversation rather than a record.
	taskPaneChatWord = "enter open the chat"
	// taskPaneWorkWord counts what one conversation asked for, and the money
	// beside it is what the whole of that came to. It is the head line's own
	// count of the same thing, spelled the one way ([tasksReading.head]).
	taskPaneWorkWord = "piece"
	taskPaneWorkTail = " of work"
)

// ── the conversation's own preview ──────────────────────────────────────────

// taskPaneChat is what the pane shows over a CONVERSATION's row: what it is
// called, how much work came out of it and what that came to, and the first few
// of those rows.
//
// IT IS A SUMMARY AND NOT A SECOND LIST. The work itself is one `→` away in the
// list beside it, and a pane that drew twenty rows of it would be the page
// drawing the same tree twice.
func (a *app) taskPaneChat(chat tasksChat, width int) []taskPaneRow {
	pal := a.pal
	bands := [][]string{{fit(pal.bold(pal.ink(chat.title)), width)}}

	held, cost, first := 0, 0.0, make([]tasksItem, 0, taskPaneChatRows)
	r := a.tasksFiltered()
	tree := r.tree()
	for _, group := range tree.groups {
		if group.chat.key != chat.key {
			continue
		}
		for _, root := range group.roots {
			tree.under(root, func(item tasksItem, _ int) {
				held++
				cost += item.entry.Cost
				if len(first) < taskPaneChatRows {
					first = append(first, item)
				}
			})
		}
	}
	if held > 0 {
		line := itoa(held) + " " + plural(taskPaneWorkWord, held) + taskPaneWorkTail
		if cost > 0 {
			line += railSep + dollars(cost)
		}
		bands = append(bands, []string{fit(pal.dim(line), width)})
	}
	var rows []string
	for _, item := range first {
		glyph, glyphInk := tasksGlyph(item, pal)
		rows = append(rows, fit(glyphInk(glyph)+" "+pal.muted(tasksLabel(item.entry)), width))
	}
	bands = append(bands, rows)

	verbs := []taskPaneVerb{{key: questionEnterKey, text: taskPaneChatWord}}
	return taskPaneDraw(taskRecordBands(bands), a.taskPaneVerbLine(verbs, width))
}

// taskPaneChatRows is how many of a conversation's rows the preview names. Three
// is the tree's own order — the most urgent first ([tasksTreeOf] files them) —
// which is what a person is looking for when the cursor is on the conversation
// rather than on one of its rows.
const taskPaneChatRows = 3

// ── the verbs ───────────────────────────────────────────────────────────────

// taskPaneVerbs is what can be done about the row under the cursor, in the order
// the spec fixes: the two answers where the row is the person's call, and then
// the door.
//
// THE ANSWERS COME FROM THE RECORD'S OWN TABLE ([taskRecordVerbs]) and the keys
// they draw are the keys [app.taskRecordAnswer] takes, so nothing here can
// advertise a key the answer road would drop. A row this window holds no question
// for draws no chips at all — a capability that cannot work is absent, not
// broken — and its verb line is the door alone.
func (a *app) taskPaneVerbs(item tasksItem) []taskPaneVerb {
	var verbs []taskPaneVerb
	if q, ok := a.taskRecordLanding(item.entry); ok {
		for _, verb := range taskRecordVerbs(q, a.taskRecordAsk(item.entry)) {
			verbs = append(verbs, taskPaneVerb{key: verb.digit, text: verb.digit + " " + verb.word})
		}
	}
	return append(verbs, taskPaneVerb{key: questionEnterKey, text: taskPaneOpenWord})
}

// taskPaneVerbGap is what separates two clauses of the verb line. It is wider
// than the middot every other row of facts here uses, because these are TARGETS
// rather than facts: the gap is what a pointer aims between.
const taskPaneVerbGap = "   "

// taskPaneVerbLine paints the verb line and records where each clause landed, so
// a press resolves against what the frame actually drew.
//
// IT IS ONE WALK FOR BOTH ANSWERS. [taskCardPress]'s law said about a line
// instead of a page: two answers to "which cells are the accept chip" is a click
// that answers a question a person was reading past.
func (a *app) taskPaneVerbLine(verbs []taskPaneVerb, width int) *taskPaneRow {
	if len(verbs) == 0 {
		return nil
	}
	pal := a.pal
	line, at, spans := "", 0, make([]taskPaneVerb, 0, len(verbs))
	for i, verb := range verbs {
		if i > 0 {
			line += taskPaneVerbGap
			at += len(taskPaneVerbGap)
		}
		text := verb.text
		if at+ansi.StringWidth(text) > width {
			// A CLAUSE THAT WOULD NOT FIT IS NOT DRAWN AND IS NOT PRESSABLE. The
			// spans are what the pointer resolves against, so a clause cut by [fit]
			// with a span still recorded for it would be a target under a word that
			// is no longer there.
			break
		}
		verb.from, verb.to = at, at+ansi.StringWidth(text)
		spans = append(spans, verb)
		line += pal.warnBold(verb.key) + pal.warn(strings.TrimPrefix(text, verb.key))
		at = verb.to
	}
	if len(spans) == 0 {
		return nil
	}
	return &taskPaneRow{text: fit(line, width), hit: taskPaneHit{verbs: spans}}
}

// ── the keyboard and the pointer ────────────────────────────────────────────

// taskPaneKey is a key the LIST did not claim, offered to the pane's verb line.
// It reports whether the pane took it.
//
// A DIGIT ANSWERS THE ROW UNDER THE CURSOR, AND ONLY WHERE THE LINE THAT NAMES
// IT IS ON SCREEN. Every printable key on this page is the filter, and a page
// that quietly swallowed `2` out of a query somebody was typing would be a key
// that does one thing on a wide frame and another on a narrow one with nothing
// saying so. So the pane has to be drawn for its digits to mean anything, which
// is [taskPaneCols]' answer once more rather than a second guess at it.
func (a *app) taskPaneKey(key string) (tea.Cmd, bool) {
	width, _ := a.size()
	if !taskPaneOpen(width) || a.taskSheet.detailOn {
		return nil, false
	}
	item, ok := a.taskSheetCurrent()
	if !ok {
		return nil, false
	}
	for _, verb := range a.taskPaneVerbs(item) {
		if verb.key != key || key == questionEnterKey {
			continue
		}
		return a.taskRecordAnswer(item.entry, key)
	}
	return nil, false
}

// taskPanePress is a click to the right of the seam: the verb under it, pressed.
// It reports whether there was one.
func (a *app) taskPanePress(x, y int) (tea.Cmd, bool) {
	width, height := a.size()
	left := taskPaneList(width)
	if x < left+ansi.StringWidth(railSeam) {
		return nil, false
	}
	_, hits, _, _ := a.placeDraw(placeTasks{}, width, height)
	pane := taskPaneHitsOf(hits)
	if y < 0 || y >= len(pane) {
		return nil, false
	}
	at := x - left - ansi.StringWidth(railSeam)
	for _, verb := range pane[y].verbs {
		if at < verb.from || at >= verb.to {
			continue
		}
		if verb.key == questionEnterKey {
			return a.taskSheetEnter(), true
		}
		item, ok := a.taskSheetCurrent()
		if !ok {
			return nil, false
		}
		return a.taskRecordAnswer(item.entry, verb.key)
	}
	return nil, false
}

// ── reading the journal off the cursor ──────────────────────────────────────

// taskPaneSettle is how long the cursor has to stand still before the pane asks
// the disk for that row's report.
//
// IT IS THE KEY REPEAT AND NOT A GUESS. A held arrow on every terminal anybody
// runs this in repeats faster than this, so a person walking the list never pays
// for a row they are passing through, and a person who has STOPPED on a row has
// their report inside a beat of stopping. The frame itself never waits on it —
// the read is a command and the slot is empty until it lands.
const taskPaneSettle = 120 * time.Millisecond

// taskPaneSettleMsg is the cursor having stood still, carrying the move it was
// armed by.
//
// IT NAMES ITS GENERATION. A held `↓` arms one of these per row it passes, and a
// reply that did not say which move it belonged to would have every one of them
// firing a read — which is the stutter this whole mechanism exists to not have.
type taskPaneSettleMsg struct {
	gen uint64
	key tasksKey
}

// taskPaneFollow is what the pane does about a cursor that has just moved: it
// arms one settle, and the settle is what reads.
//
// IT IS ARMED BY THE GESTURE AND NEVER BY THE DRAW. A frame arrives thirty times
// a second and a journal is a whole session file read forward
// ([session.PeekReport]); the law that nothing on the draw path touches a disk is
// the reason this is a command hanging off a keypress, a click and a wheel tick
// rather than a lookup inside [app.taskPaneRecord].
func (a *app) taskPaneFollow() tea.Cmd {
	width, _ := a.size()
	if !taskPaneOpen(width) || a.taskSheet.detailOn {
		return nil
	}
	item, ok := a.taskSheetCurrent()
	if !ok {
		return nil
	}
	key := tasksKeyOf(item.entry)
	if _, read := a.taskSheet.paneTail[key]; read {
		return nil
	}
	a.taskSheet.paneGen++
	gen := a.taskSheet.paneGen
	return tea.Tick(taskPaneSettle, func(time.Time) tea.Msg {
		return taskPaneSettleMsg{gen: gen, key: key}
	})
}

// taskPaneSettled answers one settle: the read, where the cursor is still on the
// row it was armed for and that row has not been read already.
func (a *app) taskPaneSettled(msg taskPaneSettleMsg) tea.Cmd {
	if msg.gen != a.taskSheet.paneGen {
		// A LATER MOVE HAS ALREADY ARMED ANOTHER. This one is about a row the
		// cursor walked through, which is exactly the read this mechanism exists
		// to not pay for.
		return nil
	}
	if _, read := a.taskSheet.paneTail[msg.key]; read {
		return nil
	}
	item, ok := a.taskSheetCurrent()
	if !ok || tasksKeyOf(item.entry) != msg.key {
		return nil
	}
	return a.readTaskTail(item.entry)
}

// taskPaneKeep files one journal read under the row it was about, so walking
// back up a list costs nothing.
//
// IT IS KEYED BY THE ROW AND NOT BY THE PATH ([tasksKey] is the pair
// internal/session says identifies one piece of work), and an EMPTY report is
// kept as readily as a full one: "this row has nothing to say" is an answer, and
// a cache that only remembered the answers it liked would ask the disk about
// every silent row on every cursor move.
func (a *app) taskPaneKeep(key tasksKey, tail string) {
	if key == (tasksKey{}) {
		return
	}
	if a.taskSheet.paneTail == nil {
		a.taskSheet.paneTail = make(map[tasksKey]string)
	}
	a.taskSheet.paneTail[key] = tail
	a.touch()
}
