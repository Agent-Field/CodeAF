package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE REWIND TIMELINE: the whole conversation, as a list you pick a point out of.
//
//	 ⟲ rewind — pick where the conversation goes back to            esc close
//
//	 › and now write the tests
//
//	 ─────────────────────────────────────────────────────────────────────────
//	   › port the parser              4 tools · 2 files
//	     it is done — here is what changed
//	     [read: internal/parse/lex.go]
//	   › and now write the tests
//	 ─────────────────────────────────────────────────────────────────────────
//	 ⟲ drops 1 turn — everything below the pick is let go
//	 esc close · ↑↓ move · enter picks the point
//
// THERE ARE TWO REWINDS AND THEY ANSWER TWO DIFFERENT QUESTIONS. esc esc opens
// the INLINE mode (rewind.go), which is the quick take-back: the transcript on
// screen is the picker, the answer is almost always "the thing I just said", and
// the whole gesture is over in two keystrokes. This page is the DELIBERATE one —
// "take me back to before we started down this road" — and the question it
// answers cannot be answered by the inline mode at all, because the inline mode
// walks the DRAWN blocks and a resumed conversation draws only its last
// [replayTail] entries. Everything older than that was unreachable.
//
// SO THIS PAGE IS BUILT FROM THE SESSION AND NOT FROM THE SCREEN. Its rows come
// out of [session.Agent.Transcript] and its points out of
// [session.Agent.RewindPoints], both of which cover the WHOLE history — the same
// reading /export already takes for the same reason. Nothing here is matched
// against the drawn block list, so there is no window to fall off the top of.
//
// IT IS THE FOURTH CITIZEN OF THE FRAME-TAKEOVER PATTERN, after the settings
// panel, the task page and home (view.go's [app.frame] draws them in that
// order), and it follows the task page's shape down to the smallest part:
// a state struct, one key handler that owns every key while it is up, one
// function that is both the frame and the pointer's hit map, and a filter that
// every printable key types into.
//
// THE TWO SURFACES SHARE ONE VOCABULARY. The ⟲ glyph, the "drops N turns"
// arithmetic ([rewindDropCount]), the cut itself and everything that happens
// after it ([app.rewindLand] — the rebuild, the note, the message coming back to
// the box) are one implementation with two doors on it. A person who learns the
// quick gesture has already learned this page.
//
// ENTER IS TWO-STAGE, and that is the one place this page deliberately parts
// company with the inline mode. The inline mode is a gesture aimed at a row a
// person is already looking at; this is a list somebody has just scrolled a
// hundred rows through, and a single blind keystroke that drops an hour of
// conversation from a row you have only just arrived at is not a thing this
// surface is going to offer. So the first enter PLACES the pick — the foot
// changes to say what the next one does — and the second one on the same point
// cuts. Moving the cursor takes the arming away again.

// The page's words, written down once. Everything a person reads on it is here.
const (
	// rewindSheetTitleWord is the head, and it states the CONSEQUENCE rather than
	// naming the screen. "rewind" alone would be a label; what somebody arriving
	// here needs to know in one line is that they are choosing where the
	// conversation goes back to.
	rewindSheetTitleWord = "rewind — pick where the conversation goes back to"
	// rewindSheetCloseWord is the right end of the head, which is every fullscreen
	// page's own promise about the way out ([app.taskSheetTitle]).
	rewindSheetCloseWord = "esc close"
	// rewindSheetDropTail is what the foot adds to the count, and it is the whole
	// reason this page can be trusted with a destructive key: the number says how
	// much, and this says what "how much" means.
	rewindSheetDropTail = " — everything below the pick is let go"
	// rewindSheetKeys is the foot's legend at rest, rewindSheetPickedKeys the one
	// on a point the first enter has already placed, and rewindSheetSearchKeys the
	// one while something has been typed. Each names what the NEXT keystroke does,
	// which is the task page's own law about its foot ([app.taskSheetKeysLine]).
	rewindSheetKeys       = "esc close · ↑↓ move · enter picks the point"
	rewindSheetPickedKeys = "esc close · ↑↓ move · enter again rewinds here"
	rewindSheetSearchKeys = "esc clears the search · ↑↓ move · enter picks the point"
	// rewindSheetSearchWord opens the line that says what was typed, and
	// rewindSheetSearchNone is what that line adds when the search has taken every
	// row off the page. A filtered page with nothing on it and nothing said is a
	// page a person reads as broken.
	rewindSheetSearchWord = "search · "
	rewindSheetSearchNone = " · nothing matches"
	// rewindSheetLiftWord is the key that lifts the inline mode into this page,
	// spelled for the inline mode's own legend ([rewindKeysWord]).
	rewindSheetLiftKey  = "tab"
	rewindSheetLiftWord = rewindSheetLiftKey + " the whole conversation"
)

// rewindSheetPreviewRows is how many rows the preview above the list is given.
//
// IT IS A FIXED HEIGHT AND NOT A FITTED ONE. The preview is redrawn on every
// cursor step, and a block that grew and shrank with the length of each message
// would slide the list up and down under the very cursor that is walking it —
// which is how a person aiming for a row lands on its neighbour. Three rows is
// enough for a normal instruction; a longer one is cut and says so.
const rewindSheetPreviewRows = 3

// rewindSheetPage is what pgup and pgdown move by. It is not a cap on anything:
// the list is as long as the conversation is.
const rewindSheetPage = 12

// rewindSheetRowKind is what one row of the timeline IS. The kind decides how
// the row is painted and nothing else — every row belongs to a point the same
// way ([app.rewindSheetRows]).
type rewindSheetRowKind uint8

const (
	// rewindRowTurn is something the person said: the row a rewind is usually
	// aimed at, and the only kind that carries an annotation.
	rewindRowTurn rewindSheetRowKind = iota
	// rewindRowReply is one of the model's answers, first line only.
	rewindRowReply
	// rewindRowTool is one call, as the bracketed one-liner below.
	rewindRowTool
	// rewindRowNote is a line the SESSION wrote — a compaction mark, a task's
	// completion note — which is neither anybody's words nor a call.
	rewindRowNote
)

// rewindSheetRow is one line of the timeline.
type rewindSheetRow struct {
	kind rewindSheetRowKind
	// entry is this row's index into the session's transcript, and it is what
	// decides whether the row is above or below the cut: a cut at a point drops
	// every display entry from [session.RewindPoint.Entry] on.
	entry int
	// point is the rewind point this row BELONGS TO — the last point whose cut
	// starts at or above it, which is [app.rewindPointAtEntry]'s law restated over
	// this list. A click or an enter anywhere in a turn's rows chooses that turn,
	// so a pick never drops less than the row it was aimed at.
	point int
	// head marks the one row of each point's run that the cursor may stand on.
	// The rest are read rather than chosen, exactly as a section's word is on the
	// tasks place ([tasksItem.pick]).
	head bool
	// text is the row's words, unpainted, and note is the dim annotation a turn
	// row carries after them.
	text string
	note string
	// full is what the preview shows and what the search asks. It is the WHOLE
	// message rather than the first line of it, because a person searching for a
	// sentence they wrote is as likely to remember its middle as its opening.
	full string
}

func (r rewindSheetRow) pick() bool { return r.head && r.point >= 0 }

// rewindSheet is the page's whole state. The zero value is closed.
type rewindSheet struct {
	open bool
	// points and rows are captured on the way in, for the reason the inline
	// mode captures its own list: the thing being walked must not change under
	// the walk.
	points []session.RewindPoint
	rows   []rewindSheetRow
	// cursor and top index the FILTERED list ([app.rewindSheetItems]), which is
	// the whole list whenever nothing has been typed.
	cursor int
	top    int
	// at is the point the wash and the count are about. It follows the cursor,
	// so the page shows what a commit would let go while the hand is still
	// moving.
	at int
	// armed says the first enter has landed on this point and the next one cuts.
	// Any movement takes it away again — an arming that survived a scroll would
	// be a destructive key waiting under a cursor somewhere else.
	armed bool
	// query is the type-to-search box, and it is the [editor] every other box on
	// this surface is: backspace, ctrl+u and ctrl+w are edits a person's hands
	// already know.
	query editor
	// draft and caret are the person's own sentence, held for them exactly as the
	// inline mode holds it ([rewindMode.draft]).
	draft []rune
	caret int
	// said is the foot's feedback line: the engine's refusal, in the engine's own
	// words, until a key moves the pick.
	said string
}

// ── the door ────────────────────────────────────────────────────────────────

// openRewindSheet raises the page. It is what /rewind calls.
//
// THE REFUSALS ARE THE INLINE MODE'S ([app.enterRewind]), restated rather than
// shared because the two doors are not quite the same door: this one can also be
// reached from inside the inline mode, which is why that state is handled by the
// lift below instead of being refused here.
func (a *app) openRewindSheet() tea.Cmd {
	if a.rewSheet.open || a.rew.on || a.copy.on || a.roomOpen() || a.sheet.open || a.railFull() {
		return nil
	}
	agent, ok := a.rewinder()
	if !ok {
		return a.sayRewind(rewindEmptyWord)
	}
	points := agent.RewindPoints()
	if len(points) == 0 {
		return a.sayRewind(rewindEmptyWord)
	}
	a.disarmRewind()
	a.closeLists()
	a.dropHover()
	// THE OTHER FULLSCREEN PAGES STAND DOWN, which is the law settings.go states:
	// only one page may believe it owns the frame, because view.go can only draw
	// one and a page opened under another would take the keys of a screen nobody
	// can see.
	a.standDownFullscreen()
	a.rewSheet = rewindSheet{
		open:   true,
		points: points,
		rows:   a.rewindSheetRows(points),
		draft:  append([]rune(nil), a.input.value...),
		caret:  a.input.cursor,
	}
	// THE PICK STARTS AT THE LAST THING THE PERSON SAID, which is the inline
	// mode's own opening position ([lastTurnPoint]) and for its reason: it is what
	// a person reaching for a rewind means nine times out of ten, and everything
	// older is a walk away.
	a.rewindSheetGoTo(lastTurnPoint(points))
	a.input.reset()
	a.sel = -1
	a.touch()
	return a.wake()
}

// liftRewind is `tab` inside the inline mode: the same conversation, opened as
// the whole list, with the cut the person had already chosen carried over.
//
// THE DRAFT TRAVELS WITH IT. The inline mode is holding the person's half-written
// sentence and this page holds it in exactly the same way, so a lift is invisible
// to the box: esc out of here puts back the sentence esc out of there would have.
func (a *app) liftRewind() tea.Cmd {
	if !a.rew.on {
		return nil
	}
	points, at := a.rew.points, a.rew.at
	draft, caret := a.rew.draft, a.rew.cursor
	// The inline mode leaves WITHOUT restoring: this page is taking the stash on,
	// and a draft put back into a box that is about to be replaced by a page would
	// be a sentence the next esc restores twice.
	a.leaveRewind(false)
	a.rewSheet = rewindSheet{
		open:   true,
		points: points,
		rows:   a.rewindSheetRows(points),
		draft:  draft,
		caret:  caret,
	}
	a.rewindSheetGoTo(at)
	a.touch()
	return a.wake()
}

// closeRewindSheet lowers the page. restore puts the stashed sentence back,
// which is what esc does and what a step cut does; a turn cut replaces it with
// the message the cut took out of the conversation ([app.rewindLand]).
func (a *app) closeRewindSheet(restore bool) {
	if !a.rewSheet.open {
		return
	}
	if restore {
		a.input.value = append(a.input.value[:0], a.rewSheet.draft...)
		a.input.cursor = min(a.rewSheet.caret, len(a.input.value))
	}
	a.rewSheet = rewindSheet{}
	a.dropHover()
	a.touch()
}

// ── the rows ────────────────────────────────────────────────────────────────

// rewindSheetRows turns the session's whole transcript into the list this page
// walks, and hangs every row off the point it belongs to.
//
// IT READS [session.Agent.Transcript] AND NOTHING ELSE ABOUT THE SCREEN. The
// drawn blocks are a windowed rendering of this same conversation (replay.go
// caps them at [replayTail]) and matching against them is what confines the
// inline mode to the tail. There is no window here.
func (a *app) rewindSheetRows(points []session.RewindPoint) []rewindSheetRow {
	if a.agent == nil {
		return nil
	}
	transcript := a.agent.Transcript()
	out := make([]rewindSheetRow, 0, len(transcript))
	for i, e := range transcript {
		row, ok := rewindSheetRowFor(i, e)
		if !ok {
			continue
		}
		row.point = rewindSheetPointAt(points, i)
		out = append(out, row)
	}
	// The annotations are counted over the finished list rather than while it is
	// being built, because what a turn spent is a fact about the rows AFTER it and
	// a walk that had not reached them yet could not know it.
	rewindSheetAnnotate(out, transcript)
	// EXACTLY ONE ROW PER POINT ANSWERS TO THE CURSOR, and it is the first of that
	// point's run — the row where its cut begins. The rest are read: they belong
	// to the point above them, so a cursor that could stand on one would be
	// offering a choice that is not a different choice.
	seen := -1
	for i := range out {
		if out[i].point >= 0 && out[i].point != seen {
			out[i].head = true
			seen = out[i].point
		}
	}
	return out
}

// rewindSheetRowFor shapes one transcript entry, or reports that it draws
// nothing. It is [replayDraws]'s rule and the replay's own shaping, asked once:
// a row this page drew that the transcript does not hold would be a row with no
// point behind it.
func rewindSheetRowFor(index int, e session.DisplayEntry) (rewindSheetRow, bool) {
	text := strings.TrimSpace(e.Text)
	switch e.Role {
	case "user":
		if text == "" && len(e.ImageRefs) == 0 {
			return rewindSheetRow{}, false
		}
		words := text
		if words == "" {
			// A message that was only pictures is still a message, and the names of
			// what was attached are the whole of what it said (replay.go's
			// [replayUserLine] makes the same trade for the same reason).
			words = strings.Join(rewindSheetNames(e.ImageRefs), " ")
		}
		return rewindSheetRow{kind: rewindRowTurn, entry: index, text: firstLine(words), full: words}, true
	case "assistant":
		if text == "" {
			return rewindSheetRow{}, false // a step that only called tools; its calls follow
		}
		return rewindSheetRow{kind: rewindRowReply, entry: index, text: firstLine(text), full: text}, true
	case "tool":
		if strings.TrimSpace(e.Tool) == "" {
			return rewindSheetRow{}, false // a result message from the wire, not a call
		}
		line := rewindSheetToolLine(e)
		return rewindSheetRow{kind: rewindRowTool, entry: index, text: line, full: line}, true
	case "note", "aside":
		if text == "" {
			return rewindSheetRow{}, false
		}
		return rewindSheetRow{kind: rewindRowNote, entry: index, text: firstLine(text), full: text}, true
	}
	return rewindSheetRow{}, false
}

// rewindSheetNames is the base names of a message's pictures, which is what a
// terminal cell can honestly show of one.
func rewindSheetNames(refs []string) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref = strings.TrimSpace(ref); ref != "" {
			out = append(out, "["+baseName(ref)+"]")
		}
	}
	return out
}

// rewindSheetToolLine is one call as this page draws it: `[read: path]`,
// `[bash: git status]`.
//
// THE WORDS COME FROM THE GLOSS THE CLUSTER ALREADY DREW
// ([session.DisplayEntry.Hint]), with the tool's own name taken off the front
// because the bracket already carries it. The gloss is the engine's single
// answer to "what is this call about" — reading the raw arguments again here
// would be a second answer, and the two would drift. A call whose journal kept
// no gloss falls back to the arguments, and one with neither is its bare name:
// the emptiness law, applied to a row rather than to a number.
func rewindSheetToolLine(e session.DisplayEntry) string {
	tool := strings.TrimSpace(e.Tool)
	words := rewindSheetToolWords(e)
	if words == "" {
		return "[" + tool + "]"
	}
	return "[" + tool + ": " + words + "]"
}

// rewindSheetToolWords is the gloss with the tool's name stripped off it.
func rewindSheetToolWords(e session.DisplayEntry) string {
	tool := strings.TrimSpace(e.Tool)
	hint := strings.TrimSpace(e.Hint)
	if hint == "" {
		return firstLine(strings.TrimSpace(e.Args))
	}
	if rest := strings.TrimPrefix(hint, tool); rest != hint {
		return strings.TrimSpace(rest)
	}
	return hint
}

// rewindSheetPointAt is the point a row belongs to: the last one whose cut
// begins at or above it. A row above every point takes the oldest, which is the
// nearest legal cut in the direction the reader is looking — the same reading
// [app.rewindPointAtEntry] gives a click.
func rewindSheetPointAt(points []session.RewindPoint, entry int) int {
	at := -1
	for i, point := range points {
		if point.Entry > entry {
			break
		}
		at = i
	}
	if at < 0 && len(points) > 0 {
		return 0
	}
	return at
}

// rewindSheetAnnotate hangs a dim tally on each turn row: how many calls that
// turn made, and how many files it wrote.
//
// THE EMPTINESS LAW DECIDES THE WHOLE SHAPE OF IT. A turn that called nothing
// gets nothing — not `0 tools`, not a separator with a blank after it — and a
// turn that called things but wrote no file says only what it called. There is
// no timestamp and no cost on these rows either, and that is not an omission
// this page is waiting to fill: [session.DisplayEntry] carries neither, so any
// clock word here would be one this surface made up.
//
// A FILE IS A write OR AN edit, counted by the path the gloss names, distinct.
// Those two are the tools whose whole purpose is to change a file on disk, and
// the count is the answer to the question a person asks at exactly this moment —
// "how much did that turn actually touch?" — which the page then answers again,
// honestly, in its foot: none of it comes back.
func rewindSheetAnnotate(rows []rewindSheetRow, transcript []session.DisplayEntry) {
	turn := -1
	tools := 0
	var files map[string]bool
	flush := func() {
		if turn < 0 {
			return
		}
		var segs []string
		if tools > 0 {
			segs = append(segs, itoa(tools)+" "+plural("tool", tools))
		}
		if len(files) > 0 {
			segs = append(segs, itoa(len(files))+" "+plural("file", len(files)))
		}
		rows[turn].note = strings.Join(segs, railSep)
	}
	for i := range rows {
		if rows[i].kind == rewindRowTurn {
			flush()
			turn, tools, files = i, 0, nil
			continue
		}
		if rows[i].kind != rewindRowTool || turn < 0 {
			continue
		}
		tools++
		e := transcript[rows[i].entry]
		if !rewindSheetWrites(e.Tool) {
			continue
		}
		if path := rewindSheetToolWords(e); path != "" {
			if files == nil {
				files = make(map[string]bool, 4)
			}
			files[path] = true
		}
	}
	flush()
}

// rewindSheetWrites reports whether a tool's whole purpose is to change a file.
func rewindSheetWrites(tool string) bool {
	switch strings.TrimSpace(tool) {
	case "write", "edit":
		return true
	}
	return false
}

// ── the search ──────────────────────────────────────────────────────────────

// rewindSheetQuery is what has been typed, trimmed. Empty is no search.
func (a *app) rewindSheetQuery() string {
	return strings.TrimSpace(a.rewSheet.query.String())
}

// rewindSheetSearching reports whether the page is being typed at.
func (a *app) rewindSheetSearching() bool { return a.rewindSheetQuery() != "" }

// rewindSheetItems is the list as it is drawn: the whole timeline, or the rows a
// search kept.
//
// A KEPT ROW IS PICKABLE WHATEVER KIND IT IS, which is where the search parts
// company with the list underneath it. Unfiltered, the cursor stops only on the
// row where a point's cut begins, because the rows between are the same choice
// said again; filtered, a person is looking at four rows out of four hundred and
// every one of them is the answer they searched for — so every one of them takes
// the point it belongs to.
func (a *app) rewindSheetItems() []rewindSheetRow {
	needle := strings.ToLower(a.rewindSheetQuery())
	if needle == "" {
		return a.rewSheet.rows
	}
	out := make([]rewindSheetRow, 0, len(a.rewSheet.rows))
	for _, row := range a.rewSheet.rows {
		if row.point < 0 || !strings.Contains(strings.ToLower(row.full), needle) {
			continue
		}
		row.head = true
		out = append(out, row)
	}
	return out
}

// rewindSheetTyped is what every edit of the search ends with: the list has
// changed under the cursor, so the cursor goes back to the top of it and the
// window with it. It is the task page's own rule ([app.taskSheetTyped]) — a
// cursor left at row two hundred of a list that now has three is a page a person
// types one letter into and finds empty.
func (a *app) rewindSheetTyped() {
	a.rewSheet.cursor, a.rewSheet.top = 0, 0
	a.rewindSheetFollow()
}

// ── the cursor and the pick ─────────────────────────────────────────────────

// rewindSheetClamp is the cursor rule: only ever a row that answers to it, and
// never off the end. It walks forward first and then back, so a cursor that
// lands between two points steps onto the next one rather than off the top.
func rewindSheetClamp(rows []rewindSheetRow, at int) int {
	if len(rows) == 0 {
		return 0
	}
	at = min(max(at, 0), len(rows)-1)
	for i := at; i < len(rows); i++ {
		if rows[i].pick() {
			return i
		}
	}
	for i := at; i >= 0; i-- {
		if rows[i].pick() {
			return i
		}
	}
	return at
}

// rewindSheetFollow puts the cursor on a legal row and moves the pick under it.
func (a *app) rewindSheetFollow() {
	rows := a.rewindSheetItems()
	a.rewSheet.cursor = rewindSheetClamp(rows, a.rewSheet.cursor)
	if at := a.rewSheet.cursor; at >= 0 && at < len(rows) && rows[at].point >= 0 {
		a.rewSheet.at = rows[at].point
	}
}

// rewindSheetGoTo parks the cursor on the row that a point's cut begins at, and
// the pick with it. It is what opening does and what a lift out of the inline
// mode does.
func (a *app) rewindSheetGoTo(point int) {
	rows := a.rewSheet.rows
	a.rewSheet.at = clampInt(point, 0, max(len(a.rewSheet.points)-1, 0))
	for i, row := range rows {
		if row.pick() && row.point == a.rewSheet.at {
			a.rewSheet.cursor = i
			a.rewSheet.armed = false
			return
		}
	}
	a.rewSheet.cursor = rewindSheetClamp(rows, len(rows)-1)
	a.rewSheet.armed = false
	a.rewindSheetFollow()
}

// rewindSheetMove walks the rows, stepping over the ones the cursor may not
// stand on and clamping at both ends the way every other list here does.
//
// A MOVE DISARMS. The first enter's promise was about the point under the cursor
// at the moment it was pressed, and a cursor somewhere else is a different
// promise — one nobody made.
func (a *app) rewindSheetMove(delta int) {
	rows := a.rewindSheetItems()
	if len(rows) == 0 {
		return
	}
	at := rewindSheetClamp(rows, a.rewSheet.cursor)
	step := 1
	if delta < 0 {
		step = -1
	}
	for i := 0; i < abs(delta); i++ {
		next := at
		for {
			next += step
			if next < 0 || next >= len(rows) {
				next = at
				break
			}
			if rows[next].pick() {
				break
			}
		}
		if next == at {
			break
		}
		at = next
	}
	a.rewSheet.cursor = at
	a.rewindSheetDisarm()
	a.rewindSheetFollow()
}

// rewindSheetDisarm takes the arming away and, with it, the refusal that was
// about the cut it was armed on.
func (a *app) rewindSheetDisarm() {
	a.rewSheet.armed = false
	a.rewSheet.said = ""
}

// rewindSheetPoint is the point the wash and the count are about.
func (a *app) rewindSheetPoint() (session.RewindPoint, bool) {
	if a.rewSheet.at < 0 || a.rewSheet.at >= len(a.rewSheet.points) {
		return session.RewindPoint{}, false
	}
	return a.rewSheet.points[a.rewSheet.at], true
}

// rewindSheetDrops counts what the pick takes, over this page's own rows: how
// many of the person's messages go, and how many rows in total.
func (a *app) rewindSheetDrops() (turns, rows int) {
	point, ok := a.rewindSheetPoint()
	if !ok {
		return 0, 0
	}
	for _, row := range a.rewSheet.rows {
		if row.entry < point.Entry {
			continue
		}
		if row.kind == rewindRowTurn {
			turns++
		}
		rows++
	}
	return turns, rows
}

// rewindSheetDropWord spells that count, in the words the inline mode's bar
// spells it in ([rewindDropCount]).
func (a *app) rewindSheetDropWord() string {
	return rewindDropCount(a.rewindSheetDrops())
}

// ── the keyboard ────────────────────────────────────────────────────────────

// rewindSheetKey is this page's whole claim on the keyboard, and it TAKES EVERY
// KEY while the page is up: the page is the frame, so there is nothing under it
// for a key to mean anything to. ctrl+c is read above this and stays the door.
func (a *app) rewindSheetKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.rewSheet.open {
		return nil, false
	}
	defer a.touch()
	switch key := msg.String(); key {
	case "esc":
		// esc BACKS OUT ONE LAYER AT A TIME, which is the settings panel's own
		// layering and the task page's: the search first, the page second. A key
		// that closed the whole page from inside a search would throw away the one
		// thing on screen the person typed.
		if a.rewindSheetSearching() {
			a.rewSheet.query.reset()
			a.rewindSheetTyped()
			return nil, true
		}
		a.closeRewindSheet(true)
	case "enter":
		a.rewindSheetEnter()
	case "up", "ctrl+p":
		a.rewindSheetMove(-1)
	case "down", "ctrl+n":
		a.rewindSheetMove(1)
	case "pgup":
		a.rewindSheetMove(-rewindSheetPage)
	case "pgdown":
		a.rewindSheetMove(rewindSheetPage)
	case "home":
		a.rewSheet.cursor = 0
		a.rewindSheetDisarm()
		a.rewindSheetFollow()
	case "end":
		a.rewSheet.cursor = len(a.rewindSheetItems()) - 1
		a.rewindSheetDisarm()
		a.rewindSheetFollow()

	// ── the search's own edits, in the task page's spelling ───────────────────
	case "backspace":
		a.rewSheet.query.deleteBackward()
		a.rewindSheetTyped()
	case "ctrl+u":
		a.rewSheet.query.reset()
		a.rewindSheetTyped()
	case "ctrl+w":
		a.rewSheet.query.deleteWord()
		a.rewindSheetTyped()

	default:
		// EVERY PRINTABLE KEY IS THE SEARCH. There is no draft under this page for
		// a letter to reach, and a conversation four hundred rows long is found by
		// remembering a word of what was said and by nothing else.
		if text := msg.Key().Text; text != "" {
			a.rewSheet.query.insert(text)
			a.rewindSheetTyped()
		}
	}
	return nil, true
}

// rewindSheetEnter is the two-stage key: it PLACES the pick on the point under
// the cursor, and on a point it has already placed it CUTS.
func (a *app) rewindSheetEnter() {
	rows := a.rewindSheetItems()
	at := rewindSheetClamp(rows, a.rewSheet.cursor)
	if at < 0 || at >= len(rows) || !rows[at].pick() {
		return
	}
	if a.rewSheet.armed && rows[at].point == a.rewSheet.at {
		a.commitRewindSheet()
		return
	}
	a.rewSheet.cursor = at
	a.rewSheet.at = rows[at].point
	a.rewSheet.armed = true
	a.rewSheet.said = ""
}

// commitRewindSheet is the cut, and everything that follows one.
//
// A REFUSAL KEEPS THE PAGE UP and prints the engine's own sentence in the foot,
// which is the inline mode's bargain ([app.commitRewind]) for its reason: the one
// refusal the engine actually raises is a turn winding down, and the answer to it
// is to press the same key again a moment later — which is only possible if the
// page is still there to press it in.
func (a *app) commitRewindSheet() {
	point, ok := a.rewindSheetPoint()
	if !ok {
		a.closeRewindSheet(true)
		return
	}
	// The count is taken BEFORE the cut, because after it the rows it counted are
	// gone.
	word := a.rewindSheetDropWord()
	stash, caret := a.rewSheet.draft, a.rewSheet.caret
	err := a.rewindLand(point, word, stash, caret, func() { a.rewSheet = rewindSheet{} })
	if err != nil {
		// THE ARMING SURVIVES A REFUSAL. The one refusal the engine raises is a
		// turn still winding down, and the answer to it is the same key a moment
		// later — a page that made a person re-place the pick they had just placed
		// would be charging them two keystrokes for the engine's timing.
		a.rewSheet.said = errText(err)
		a.touch()
	}
}

// ── the pointer ─────────────────────────────────────────────────────────────

// rewindSheetHitKind is what one screen row of the page answers to a click.
type rewindSheetHitKind uint8

const (
	rewindSheetHitNone rewindSheetHitKind = iota
	rewindSheetHitRow
)

type rewindSheetHit struct {
	kind  rewindSheetHitKind
	index int
}

// rewindSheetPress is a click inside the page, and it MIRRORS enter: a press
// places the pick, and a press on the point already placed cuts. The pointer can
// do everything the keys can and nothing they cannot, which is this surface's own
// law about the mouse (rewind.go).
func (a *app) rewindSheetPress(y int) {
	width, height := a.size()
	_, hits, _, _ := a.rewindSheetFrame(width, height)
	if y < 0 || y >= len(hits) || hits[y].kind != rewindSheetHitRow {
		return
	}
	a.rewSheet.cursor = hits[y].index
	a.rewindSheetEnter()
	a.touch()
}

// rewindSheetHover records which row the pointer is over, repainting only when
// the answer changed (hover.go's rule, applied to this page).
func (a *app) rewindSheetHover(y int) {
	width, height := a.size()
	_, hits, _, _ := a.rewindSheetFrame(width, height)
	next := hoverAt{}
	if y >= 0 && y < len(hits) && hits[y].kind == rewindSheetHitRow {
		next = hoverAt{kind: hoverRewindSheet, index: hits[y].index}
	}
	if next == a.hot {
		return
	}
	a.hot = next
	a.touch()
}

// rewindSheetScroll is the wheel, and it walks the cursor rather than an offset
// of its own — the task page's bargain, so that one place decides where the
// window is.
func (a *app) rewindSheetScroll(delta int) { a.rewindSheetMove(delta) }

// ── the frame ───────────────────────────────────────────────────────────────

// rewindSheetFrame is the whole screen while the page is open: exactly height
// rows, and what each of them answers to the pointer.
//
// It is ONE function for [app.taskSheetFrame]'s reason: the frame draws these
// rows and the pointer resolves against them, and two answers to "where is the
// third turn" is how a click drops the wrong half of a conversation.
//
// The caret is reported as (0, 0) and never moves, because nothing on this page
// is typed INTO in the sense a box is — the search has no caret of its own, it
// has the line at the foot that says what it holds.
func (a *app) rewindSheetFrame(width, height int) ([]string, []rewindSheetHit, int, int) {
	pal := a.pal
	lines := make([]string, 0, height)
	hits := make([]rewindSheetHit, 0, height)
	add := func(text string, hit rewindSheetHit) {
		lines = append(lines, text)
		hits = append(hits, hit)
	}

	rows := a.rewindSheetItems()
	a.rewSheet.cursor = rewindSheetClamp(rows, a.rewSheet.cursor)

	add(a.rewindSheetTitle(width), rewindSheetHit{})
	add("", rewindSheetHit{})
	for _, text := range a.rewindSheetPreview(width) {
		add(text, rewindSheetHit{})
	}
	add(pal.dim(rule(width)), rewindSheetHit{})

	// The foot is three rows and it is spoken for before the list is: a rule, what
	// the cut costs, and the keys. A FOURTH JOINS THEM WHILE A SEARCH IS ON,
	// because what was typed has to be on screen — a list that has lost rows for a
	// reason a reader cannot see is a list that has lost them for no reason.
	foot := 3
	if a.rewindSheetSearching() {
		foot++
	}
	head := len(lines)
	room := height - head - foot
	if room < 1 {
		room = 1
	}

	a.rewSheet.top = listTop(a.rewSheet.cursor, a.rewSheet.top, len(rows), room)
	for at := a.rewSheet.top; at < len(rows) && len(lines)-head < room; at++ {
		hit := rewindSheetHit{}
		if rows[at].pick() {
			hit = rewindSheetHit{kind: rewindSheetHitRow, index: at}
		}
		add(a.rewindSheetRowText(rows[at], at, width), hit)
	}
	for len(lines)-head < room {
		add("", rewindSheetHit{})
	}

	add(pal.dim(rule(width)), rewindSheetHit{})
	add(" "+a.rewindSheetFootLine(width-2), rewindSheetHit{})
	if a.rewindSheetSearching() {
		add(" "+pal.dim(fit(a.rewindSheetSearchLine(rows), width-2)), rewindSheetHit{})
	}
	add(" "+pal.dim(fit(a.rewindSheetKeysLine(), width-2)), rewindSheetHit{})

	// A terminal too short for the whole page keeps its head and its foot: what
	// this is, and how to leave. It is [app.taskSheetFrame]'s own trim.
	if len(lines) > height && height > 1 {
		lines = append(lines[:1], lines[len(lines)-(height-1):]...)
		hits = append(hits[:1], hits[len(hits)-(height-1):]...)
	}
	return lines, hits, 0, 0
}

// rewindSheetTitle is the head: the consequence on the left, the way out on the
// right.
func (a *app) rewindSheetTitle(width int) string {
	word := rewindMark(a.pal) + " " + rewindSheetTitleWord
	left := " " + a.pal.bold(a.pal.ink(word))
	right := rewindSheetCloseWord + " "
	gap := width - ansi.StringWidth(" "+word) - ansi.StringWidth(right)
	if gap < 1 {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", gap) + a.pal.dim(right)
}

// rewindSheetPreview is the block above the list: what the pick actually says,
// in full, so that a person confirms CONTENT and not merely a position.
//
// It is drawn in the transcript's own shape — the "›" marker in the accent, the
// words in ink — because it is the same message they read there, and a second
// styling for one message would be a second thing to recognise. A pick that
// landed on a step point has no words of anybody's to show, so it shows the row
// the cut begins at, which is exactly what that cut is about.
func (a *app) rewindSheetPreview(width int) []string {
	out := make([]string, 0, rewindSheetPreviewRows)
	words, own := a.rewindSheetPreviewWords()
	room := width - 4
	if room < 1 {
		room = 1
	}
	var body []string
	if words = strings.TrimSpace(words); words != "" {
		body = wrap(words, room)
	}
	for i, line := range body {
		if i >= rewindSheetPreviewRows {
			// The tail is named rather than silently cut: a message that stops
			// mid-sentence with nothing said about it reads as a message that was
			// stored that way.
			more := len(body) - rewindSheetPreviewRows + 1
			out[len(out)-1] = "   " + a.pal.dim(fit("… "+itoa(more)+" more "+plural("line", more), room))
			break
		}
		text := a.pal.ink(fit(line, room))
		if i == 0 {
			if own {
				out = append(out, " "+a.pal.accent(a.pal.youGlyph())+text)
			} else {
				out = append(out, "   "+a.pal.dim(fit(line, room)))
			}
			continue
		}
		out = append(out, "   "+text)
	}
	for len(out) < rewindSheetPreviewRows {
		out = append(out, "")
	}
	return out
}

// rewindSheetPreviewWords is what the preview shows, and whether the words are
// the person's own.
func (a *app) rewindSheetPreviewWords() (string, bool) {
	point, ok := a.rewindSheetPoint()
	if !ok {
		return "", false
	}
	if point.Turn {
		return point.Said, true
	}
	for _, row := range a.rewSheet.rows {
		if row.point == a.rewSheet.at && row.entry >= point.Entry {
			return row.full, row.kind == rewindRowTurn
		}
	}
	return "", false
}

// rewindSheetRowText draws one row: its words, its annotation, the wash over
// everything the pick would let go, and the band under the cursor.
//
// THE WASH IS THE INLINE MODE'S, MOVED ONTO A LIST ([app.rewindPass]). Every row
// from the pick down is drawn dim, whatever it would otherwise have been, because
// the true statement about all of them is the same one: all of this goes. It
// follows the cursor as it walks, so the page is always showing what the next
// enter would cost.
func (a *app) rewindSheetRowText(row rewindSheetRow, at, width int) string {
	pal := a.pal
	room := width - 2
	if room < 1 {
		room = 1
	}
	dropped := a.rewindSheetDropped(row)
	indent := "  "
	body := row.text
	if row.kind == rewindRowTurn {
		// [palette.youGlyph] carries its own trailing space, which is what puts a
		// turn's words two cells in from the rows under it.
		body = pal.youGlyph() + row.text
	} else {
		indent = "    "
		room -= 2
		if room < 1 {
			room = 1
		}
	}
	text, used := fitWidth(body, room)
	switch {
	case dropped:
		text = pal.dim(text)
	case row.kind == rewindRowTurn:
		text = pal.ink(text)
	case row.kind == rewindRowReply:
		text = pal.muted(text)
	default:
		text = pal.dim(text)
	}
	if row.note != "" {
		if gap := room - used - ansi.StringWidth(row.note) - 2; gap >= 0 {
			text += strings.Repeat(" ", gap+2) + pal.dim(row.note)
		}
	}
	text = indent + text
	// NOTHING ON THIS PAGE IS CHOSEN UNTIL A CUT IS COMMITTED, so nothing on it
	// wears THE GROUND LADDER's selected step. [rewindSheet.at] looks like a
	// chosen point and is not — its own comment says it FOLLOWS the cursor — and
	// the one genuinely persistent state here, `armed`, is a warning said in
	// words rather than a rung.
	//
	// So the two facts left are the keyboard cursor and the pointer, and the
	// ladder gives them ONE rung between them. The cursor used to take the
	// selected step, which said a decision had been made when all that had
	// happened was that ↑/↓ got there — on the one page where the difference
	// between "looking at this cut" and "taking this cut" is the whole point.
	if at == a.rewSheet.cursor || (a.hot.kind == hoverRewindSheet && a.hot.index == at) {
		text = pal.cursor(text, width)
	}
	return text
}

// rewindSheetDropped reports whether a cut at the pick takes this row with it.
func (a *app) rewindSheetDropped(row rewindSheetRow) bool {
	point, ok := a.rewindSheetPoint()
	return ok && row.entry >= point.Entry
}

// rewindSheetFootLine is what the cut costs, said in the surface's own voice —
// or the engine's refusal, in the engine's own words, where the mode's feedback
// belongs.
func (a *app) rewindSheetFootLine(width int) string {
	if a.rewSheet.said != "" {
		return a.pal.warn(fit(a.rewSheet.said, width))
	}
	word := rewindMark(a.pal) + " drops " + a.rewindSheetDropWord()
	line := word + rewindSheetDropTail
	if ansi.StringWidth(line) > width {
		// On a frame too narrow for both, the CLAUSE goes and the count stays. It
		// is the mode bar's own trade ([app.rewindBar]): the number of turns about
		// to be dropped is not recoverable by anything else on the screen.
		line = word
	}
	return a.pal.dim(fit(line, width))
}

// rewindSheetSearchLine is what was typed, said back where a person is already
// reading the count — and, when the search has emptied the page, the one clause
// that stops a blank list reading as a page that broke.
func (a *app) rewindSheetSearchLine(rows []rewindSheetRow) string {
	line := rewindSheetSearchWord + a.rewindSheetQuery()
	if len(rows) == 0 {
		line += rewindSheetSearchNone
	}
	return line
}

// rewindSheetKeysLine names the keys, and it names what the NEXT enter does. A
// foot that went on promising to pick a point that is already picked would be
// the page lying about its own destructive key.
func (a *app) rewindSheetKeysLine() string {
	switch {
	case a.rewindSheetSearching():
		return rewindSheetSearchKeys
	case a.rewSheet.armed:
		return rewindSheetPickedKeys
	}
	return rewindSheetKeys
}
