package tui3

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// GOING INSIDE A TASK THAT IS OVER.
//
// A LIVE node has a room: a page that is the node's own transcript, its present
// arriving live, and a box that talks to it (room.go). A row of the project's
// RECORD has no live node behind it — the conversation that ran it closed — so
// the room machinery cannot host it, and for a long time this surface answered
// the gesture with the only other door it had: enter on an `earlier` row wrote
// "@its-name" into the message box and left it there.
//
// THAT WAS THE WRONG ANSWER TO THE GESTURE PEOPLE WERE MAKING. Pressing a row of
// finished work means "show me what this did". The mention is a way of pointing
// the MODEL at the work, which is a different errand and a slower one — a person
// who wanted to read the outcome had to write a sentence around the token and
// send it, and pay a turn to be told something the record already knew.
//
// So a record row opens a CARD: everything the project wrote down about that
// piece of work, and under it the last thing the node itself said, read off the
// journal the row points at. It is a MODE OF THE HISTORY PAGE rather than a
// fourth fullscreen surface, and that is the whole design decision:
//
//   - THE PAGE IS ALREADY THE RECORD'S HOME. It is where every row of the file
//     is drawn, it is already fullscreen, and it already owns the frame, the
//     keyboard and the pointer (taskview.go). A separate surface would be a
//     fourth thing for [app.standDownFullscreen] to keep exclusive and a fourth
//     place for esc to mean something new.
//   - esc IS ONE LAYER AT A TIME, as it is everywhere else here: the card backs
//     out to the list, and the list's own esc closes the page. A person who
//     opened three tasks in a row never loses the list they were reading down.
//   - THE CHORD IS NOT A LAYER. ctrl+. closes the whole page from inside the
//     card, exactly as it closes it from inside a filter.
//
// AND THE MENTION SURVIVES, on `m`, in the card's foot. It could not stay on
// enter and it could not move to a letter on the LIST — every printable key
// there is the filter — so it lives in the one place on this page that is not
// typed into, named on the line under it.

// The words the card says. Each is quoted in the manual exactly as it is
// spelled here.
const (
	// taskCardBackWord is the way out, in the head's right corner. It is `back`
	// and not `close` because that is what the key does from here: the list is
	// underneath, and a card that promised to close would be lying about the
	// next keystroke — the same honesty [tasksClearFilterWord] keeps.
	taskCardBackWord = "esc back"
	// taskCardKeys is the foot: the scroll, the one gesture this card carries
	// that the row it came from used to, and the way out.
	//
	// THE WAY OUT IS LAST BECAUSE IT IS SPENT LAST. This line is fitted by
	// [hintFit], which keeps its final clause to the last cell there is and
	// drops the clauses in front of it working backwards — so a foot with `esc
	// back` at the head was a foot that gave the way out away FIRST, and a
	// sixty-cell card ended up offering `m puts it in your message` and no way
	// off the page. Every other key sheet on this surface already ends on its
	// esc (`↑↓ pick · enter use it · esc back`, `filter · ↑↓ · enter open · esc
	// cancel`); this one now does too.
	taskCardKeys = "↑↓ scroll · m puts it in your message · " + taskCardBackWord
	// taskCardKeysHeld is the same keys WITHOUT the way out, and it is the sheet
	// the foot draws on almost every frame. The head's right corner already says
	// `esc back` at every width it has a corner to say it in, and a page that
	// named the same instruction twice on a six-line card spent two of its
	// sixteen words repeating itself (docs/design/polish/audit-tasks.md row 15).
	// So THE FOOT CARRIES THE WAY OUT ONLY WHERE THE HEAD DOES NOT, which is
	// [app.taskCardTitleLine]'s own answer and not a second guess at it.
	//
	// AND IT ENDS ON THE SCROLL RATHER THAN ON THE MENTION, which is the one way
	// its clauses are ordered differently from the sheet above. [hintFit] keeps a
	// key row's FINAL clause to the last cell there is and spends the ones in
	// front of it first — that is why the way out is last up there — so the final
	// clause of a sheet that has no way out on it has to be the one both worth
	// keeping and cheap enough to keep. `m puts it in your message` is
	// twenty-five cells; left at the end it would be the clause a narrow foot
	// SLICED, which is the one thing no hint on this surface does.
	taskCardKeysHeld = "m puts it in your message · ↑↓ scroll"
	// taskCardTailHead heads the report. "what it said at the end" and not
	// "final assistant message": the node is a thing that did some work and then
	// said how it went, and that is the sentence a person came here to read.
	taskCardTailHead = "what it said at the end"
	// taskCardTailGone is what the card says when the row NAMES a transcript and
	// the file is not on this disk any more — a session folder somebody deleted.
	// It is said rather than left blank because the row's own transcript line is
	// still printed above it, and a path with nothing under it reads as a card
	// that failed to load.
	taskCardTailGone = "its transcript is not on this disk any more"
	// taskCardTailGoneOn is that same fact about ANOTHER MACHINE'S disk, and the
	// machine's name is what makes it true. Over a connection the journal is on
	// the machine that ran the work, and "not on this disk" said about a file
	// sitting perfectly well on the server was the exact sentence this card drew
	// before it learned to ask (host.go's own honesty table).
	taskCardTailGoneOn  = "its transcript is not on "
	taskCardTailGoneEnd = " any more"
	// taskCardTailReading is what stands where the report will be while the far
	// machine is still being asked for it.
	//
	// IT IS NEVER SILENCE, which is where this parts company with the emptiness
	// law next door. A blank slot on a local card is a read that takes a
	// millisecond; over a connection it is a round trip down an ssh pipe, and a
	// card that said nothing for a second and then grew a paragraph reads as a
	// card that was wrong and then changed its mind. So it says which machine is
	// being asked, in the same plain register every other line here is in — and
	// it says nothing about progress, because nothing here knows any.
	taskCardTailReading = "reading it on "
	// taskCardTailUnread is the far machine refusing or the link not answering.
	// The card keeps every fact the ROW carried — they came over on the walk and
	// are still true — and says only that this one thing could not be had.
	taskCardTailUnread = "it could not be read on "
	// The labels on the places a piece of work left something behind. They are
	// words and not glyphs because they are the only lines on the card whose
	// meaning is not obvious from what follows them.
	//
	// THE WORKING COPY HAS NO CONSTANT HERE ANY MORE, and that is the point. It
	// had one — `worktree` — and the word was wrong twice over: it is the
	// machinery's own vocabulary, which this house bans in anything a person
	// reads, and it named a mechanism this surface had not checked was in use. A
	// repository task is grounded in a fork whenever furrow can make one
	// (internal/session's groundladder.go), and that card told the person their
	// work had been in a git worktree it never went near. What the directory was
	// is the LADDER's to say, one line per rung, and this card asks
	// ([app.taskCardGroundWord]).
	taskCardBranchWord = "branch"
	// taskCardPlaceWord labels a place this card can say nothing else about: a
	// task folder, or a directory whose rung nobody wrote down. It claims no
	// mechanism at all, which is why it is also what the ground word falls back
	// to when the record has none.
	taskCardPlaceWord      = "where"
	taskCardTranscriptWord = "transcript"
	// taskCardFilesWord is the count of what the work wrote, singular and plural.
	// The list is not here — the record keeps a count and not forty paths
	// (session's task_index.go) — and the transcript below is where they are.
	taskCardFilesOne  = " file changed"
	taskCardFilesMany = " files changed"
)

// taskCardFoot is what the card spends on its own foot: a rule and the keys.
const taskCardFoot = 2

// taskCardHit is what one row of the card answers to a click.
type taskCardHit uint8

const (
	taskCardHitNone taskCardHit = iota
	// taskCardHitHead is the title and the blank under it, and taskCardHitFoot
	// the keys line at the bottom. Both are the way back to the list — the tool
	// detail's own bargain ([expandHitClose], expand.go), because a card with no
	// close button to aim at has to make its edges mean something.
	//
	// THEY ARE TWO VALUES AND NOT ONE BECAUSE THE POINTER LIGHTS THEM. A hover is
	// about the thing under the hand, and the head and the foot are at opposite
	// ends of the screen: one value would brighten both edges of the card whichever
	// of them a person was reaching for (hover.go's [hoverTaskCard]).
	taskCardHitHead
	taskCardHitFoot
	// phone lane: taskCardHitMention is the foot's second band, where the keys
	// line becomes two targets a thumb can hit (taskphone.go). It takes the
	// foot's place at that width, so it is NOT one of the two edges [back]
	// answers for — a thumb on that band is aiming at a chip, not at the door.
	taskCardHitMention
)

// back reports whether a row of the card is the way back to the list.
func (h taskCardHit) back() bool { return h == taskCardHitHead || h == taskCardHitFoot }

// ── opening, and reading the journal ────────────────────────────────────────

// taskTailMsg carries one journal read back to the loop: the transcript that
// was read, and the last thing the node said in it.
//
// IT NAMES THE PATH IT ANSWERS ABOUT. A person walking down the record opens one
// card, backs out and opens the next faster than a file read completes, and a
// reply that did not say which task it was about would print the previous
// node's report under this one's title.
type taskTailMsg struct {
	path string
	tail string
	// kept says the journal is still on the disk of the machine that ran the
	// work, and unread that the machine could not be asked at all
	// ([tasksPlace.tail] holds the three sentences these pick between).
	kept   bool
	unread bool
}

// openTaskRecord raises the history page standing INSIDE one row of the record.
//
// It is the door from the column ([app.railEnter]) as well as from the page's
// own list, which is why it opens the page rather than assuming it is already
// up: the two lists draw the same rows, so pressing one has to arrive in the
// same place as pressing the other.
func (a *app) openTaskRecord(entry *session.TaskIndexEntry) tea.Cmd {
	if entry == nil {
		return nil
	}
	// IT GOES THROUGH THE ROUTER, exactly as every other door onto a place does
	// (pages.go's [app.showPage]): what was standing is closed, its look stamp is
	// written, and the tasks place opens on the same snapshot every other road in
	// takes. It used to stand the other pages down and build the reading itself,
	// which is a second answer to what this place is holding — and before that it
	// built NO reading at all, so a card opened from home left an EMPTY place
	// behind it and esc dropped the person onto a page with nothing on it.
	raised := a.showPage(pageTasks)
	a.taskSheet.detail, a.taskSheet.detailOn = *entry, true
	// The list underneath is parked on the row that was pressed, so esc comes
	// back to it rather than to the top of a list somebody scrolled a long way
	// down. It is done on the way IN because the list is rebuilt every frame and
	// the row's position is only knowable while the entry is in hand.
	a.taskSheetPointAt(*entry)
	a.touch()
	return tea.Batch(raised, a.readTaskTail(*entry))
}

// taskSheetPointAt puts the LIST's cursor on the row naming this piece of work,
// leaving it where it is when the record has no such row.
//
// It matches on the pair that identifies a row of the file — the conversation
// that ran it and the id inside that conversation — because an id alone is not
// unique across the record (session's task_index.go says so on
// [session.TaskIndexEntry.ID]).
func (a *app) taskSheetPointAt(want session.TaskIndexEntry) {
	r := a.tasksFiltered()
	width, _ := a.size()
	lines := r.lay(width)
	for at := range lines {
		if item, ok := r.at(lines, at); ok && taskSameRecord(item.entry, want) {
			a.taskSheet.cursor = at
			return
		}
	}
}

// taskSameRecord reports whether two rows of the project's record are the same
// piece of work.
func taskSameRecord(a, b session.TaskIndexEntry) bool {
	return a.SessionID == b.SessionID && a.ID == b.ID
}

// readTaskTail reads the node's own journal off the loop, once per card.
//
// IT NEVER RUNS ON THE RENDER PATH. The journal is a whole session file — a
// forward scan of every line it holds ([session.PeekReport]) — and a frame
// arrives thirty times a second while anything is running. A card that read it
// while laying itself out would stutter the surface for as long as it was up.
//
// A row that names no transcript, or names something that is not a file on this
// machine, is not read at all: there is nothing to open, and the card says so
// where the report would have gone.
func (a *app) readTaskTail(entry session.TaskIndexEntry) tea.Cmd {
	path := taskURIPath(entry.TranscriptURI)
	if path == "" {
		a.taskSheet.tailRead = true
		return nil
	}
	// OVER A CONNECTION THE JOURNAL IS ON THE OTHER MACHINE, so the reading is
	// asked of the machine that owns it (internal/remote's Places.Task) and never
	// of this disk. Reading here was the fault: the path is the engine's, almost
	// certainly names nothing on this laptop, and the miss came back as
	// `its transcript is not on this disk any more` — a refusal invented by
	// opening a file on a machine it was never on, which is exactly what home's
	// [homeView.readGone] refuses to do with a stat.
	if read := a.farRecord; read != nil {
		return func() tea.Msg {
			record, err := read(entry.TranscriptURI, 0)
			if err != nil {
				return taskTailMsg{path: path, unread: true}
			}
			return taskTailMsg{path: path, tail: record.Report, kept: record.Kept}
		}
	}
	// AND A HOSTED SURFACE WITH NO SEAM ASKS NOBODY. It is the safety net rather
	// than a state any door produces — the --host door wires the seam — and a
	// build that forgot to would otherwise fall through to the line below, which
	// is a read of THIS laptop's disk at a path on somebody else's. It is the
	// same net [app.worldOf] keeps over the walk, for the same reason.
	if a.hosted() {
		return func() tea.Msg { return taskTailMsg{path: path, unread: true} }
	}
	return func() tea.Msg {
		record := session.ReadTaskRecord(entry.TranscriptURI, 0)
		return taskTailMsg{path: path, tail: record.Report, kept: record.Kept}
	}
}

// taskTailRead folds one journal read into the card, and drops a read that is
// about a task the person has already walked away from.
func (a *app) taskTailRead(msg taskTailMsg) {
	if !a.taskSheet.detailOn || taskURIPath(a.taskSheet.detail.TranscriptURI) != msg.path {
		return
	}
	a.taskSheet.tail, a.taskSheet.tailRead = msg.tail, true
	a.taskSheet.tailKept, a.taskSheet.tailUnread = msg.kept, msg.unread
	a.touch()
}

// closeTaskRecord backs out of the card and leaves the list up, with the cursor
// where the card was opened from.
func (a *app) closeTaskRecord() {
	a.taskSheet.detail, a.taskSheet.detailOn = session.TaskIndexEntry{}, false
	a.taskSheet.detailTop, a.taskSheet.tail, a.taskSheet.tailRead = 0, "", false
	a.taskSheet.tailKept, a.taskSheet.tailUnread = false, false
	a.touch()
}

// taskURIPath is the FILE a row's URI names, or "" for a URI that names anything
// else — [session.TaskRecordPath], which is where the rule lives now that both
// halves of a connection have to agree about it (session's taskrecord.go says
// what a `git:task/…` URI is and why a URI naming a host is not a path).
//
// OVER --host IT IS THE OTHER MACHINE'S PATH. Nothing here stats it, and nothing
// here may: the card asks the machine that owns the journal instead
// ([app.readTaskTail]).
func taskURIPath(uri string) string { return session.TaskRecordPath(uri) }

// ── the keyboard ────────────────────────────────────────────────────────────

// taskCardKey routes one keypress while the card is up. It is reached from
// [app.taskSheetKeyPress], which is where this page's whole claim on the
// keyboard lives.
func (a *app) taskCardKey(key string) tea.Cmd {
	switch key {
	case "esc", "left":
		// ONE LAYER AT A TIME. The list is underneath and it is where this came
		// from; a key that closed the whole page would throw away a list somebody
		// may have scrolled a long way down to find this row.
		a.closeTaskRecord()
	case taskSheetKey:
		// The chord that opens the page closes it from anywhere inside, because a
		// chord is not a layer a person is standing in ([app.taskSheetKeyPress]
		// says the same about the filter).
		a.closeTaskSheet()
	case "m":
		// THE MENTION, WHICH HAD TO GO SOMEWHERE. It was enter on the row this
		// card was opened from; enter now goes inside, and the LIST cannot carry a
		// letter because every printable key there is the filter. So it is here,
		// on the one page of this surface that is read rather than typed at, and
		// the foot names it ([taskCardKeys]).
		entry := a.taskSheet.detail
		a.closeTaskSheet()
		a.mentionTask(&entry)
	case "up", "k", "ctrl+p":
		a.taskCardScroll(-1)
	case "down", "j", "ctrl+n":
		a.taskCardScroll(1)
	case "pgup", "ctrl+b":
		a.taskCardScroll(-a.taskCardPage())
	case "pgdown", "ctrl+f", " ", "space":
		a.taskCardScroll(a.taskCardPage())
	case "home", "g":
		a.taskSheet.detailTop = 0
	case "end", "G":
		// A very large offset is clamped by the frame, which is the one place
		// that knows how long the card came out (expand.go's own bargain).
		a.taskSheet.detailTop = 1 << 20
	}
	return nil
}

// taskCardPage is a screenful of the card, one row shy so a page turn keeps a
// line of context — the same courtesy [app.expandPage] pays the tool detail.
func (a *app) taskCardPage() int {
	_, height := a.size()
	if page := height - taskCardFoot - 4; page > 1 {
		return page
	}
	return 1
}

func (a *app) taskCardScroll(delta int) {
	a.taskSheet.detailTop += delta
	if a.taskSheet.detailTop < 0 {
		a.taskSheet.detailTop = 0
	}
	a.touch()
}

// taskCardPress resolves a click on the card. Its edges are the way back and
// its body is read, which is [app.expandPress]'s own shape.
func (a *app) taskCardPress(x, y int) {
	width, height := a.size()
	_, hits, _, _ := a.taskCardFrame(width, height)
	if y < 0 || y >= len(hits) {
		return
	}
	// phone lane: the foot is two bands rather than one way out (taskphone.go).
	if hits[y] == taskCardHitMention {
		a.taskCardBarPress(x)
		return
	}
	if !hits[y].back() {
		return
	}
	a.closeTaskRecord()
}

// taskCardHitAt is that hit-test with nothing done about it: which of the card's
// two edges the pointer is over, and false where it is over the body. The
// pointer asks it so the edge under the hand can light on exactly the rows a
// click would act on (hover.go's law).
func (a *app) taskCardHitAt(y int) (taskCardHit, bool) {
	width, height := a.size()
	_, hits, _, _ := a.taskCardFrame(width, height)
	if y < 0 || y >= len(hits) || !hits[y].back() {
		return taskCardHitNone, false
	}
	return hits[y], true
}

// ── the frame ───────────────────────────────────────────────────────────────

// taskCardFrame is the whole screen while the card is up: exactly height rows,
// what each of them answers to the pointer, and where the caret sits.
//
// It is ONE function for [app.taskSheetFrame]'s reason: the frame draws these
// rows and the pointer resolves against them, and two answers to "which row is
// the foot" is a click that closes a card somebody meant to scroll.
//
// The caret is reported as (0, 0) and never moves, because nothing on this card
// is typed into.
func (a *app) taskCardFrame(width, height int) ([]string, []taskCardHit, int, int) {
	pal := a.pal
	lines := make([]string, 0, height)
	hits := make([]taskCardHit, 0, height)
	add := func(text string, hit taskCardHit) {
		// THE EDGE UNDER THE POINTER LIGHTS, AND ONLY THE EDGE. The card's two ends
		// are the way back and everything between them is read, so a hover step over
		// a paragraph would be the surface offering a door that is not there
		// (hover.go's law) — and both rows of the head light together, because the
		// blank under the title is part of the same target and a person aiming at it
		// deserves to see how far it reaches.
		if hit.back() && a.hoveringTaskCard(int(hit)) {
			if text == "" {
				// An empty row has nothing for [palette.background] to paint, so it is
				// handed the one cell the padding grows out from.
				text = " "
			}
			text = a.hoverRow(text, width)
		}
		lines = append(lines, text)
		hits = append(hits, hit)
	}

	entry := a.taskSheet.detail
	title, wayOut := a.taskCardTitleLine(width, entry)
	add(title, taskCardHitHead)
	add("", taskCardHitHead)
	add(pal.dim(rule(width)), taskCardHitNone)

	head := len(lines)
	room := height - head - taskCardFoot
	if room < 1 {
		room = 1
	}

	body := a.taskCardBody(entry, width-2)
	a.taskSheet.detailTop = clampTop(a.taskSheet.detailTop, len(body), room)
	// THE FOOT RIDES UNDER THE LAST DRAWN ROW, AND THE FRAME ENDS THERE.
	//
	// This page has no composer under it. A rule pinned to the bottom of a
	// fifty-row terminal, with seventeen blank rows between it and a six-line
	// card, is a foot pinned for nobody: the reader's eye travels the whole
	// frame to find `esc back` for a page that ended at line eleven, and the
	// emptiness the law forbids in a figure is drawn here as rows of it
	// (docs/design/polish/audit-tasks.md row 5).
	//
	// A PAGE THAT SCROLLS KEEPS ITS PINNED FOOT, because there the bottom of the
	// frame IS where the content ends — so the pad is gone and nothing else is:
	// `drawn` is the room whenever the body fills it. And this is a page and not
	// a PLACE. A place has a composer and a place strip at fixed rows, and blank
	// space between a short list and the rule there is honest emptiness rather
	// than a defect (composerlayer.go's law); nothing below moves here because
	// there is nothing below.
	drawn := room
	if len(body) < drawn {
		drawn = len(body)
	}
	for i := 0; i < drawn; i++ {
		add(" "+body[a.taskSheet.detailTop+i], taskCardHitNone)
	}

	add(pal.dim(rule(width)), taskCardHitNone)
	// phone lane: the keys line becomes bands a thumb can hit (taskphone.go).
	if taskCardPhone(width) {
		line, _ := a.taskCardBar(width)
		add(line, taskCardHitMention)
	} else {
		add(" "+paintHint(hintFit(taskCardFootKeys(wayOut), width-2), pal, pal.dim), taskCardHitFoot)
	}

	// A terminal too short for the whole card keeps its head and its foot: what
	// this is, and how to leave. It is [app.taskSheetFrame]'s own trim.
	if len(lines) > height && height > 1 {
		lines = append(lines[:1], lines[len(lines)-(height-1):]...)
		hits = append(hits[:1], hits[len(hits)-(height-1):]...)
	}
	return lines, hits, 0, 0
}

// taskCardFootKeys is the foot's sheet: everything the card can do, and the way
// out only where the head has not already said it.
//
// IT IS THE ONE PLACE THAT DECIDES, and it is handed the head's own answer
// rather than re-deriving it from the width — the head keeps or drops its corner
// on the TITLE's length as well as on the width, so a second guess here would be
// a card that says `esc back` twice on one frame and, on the frame after, not at
// all.
func taskCardFootKeys(headSaysTheWayOut bool) string {
	if headSaysTheWayOut {
		return taskCardKeysHeld
	}
	return taskCardKeys
}

// taskCardTitleLine is the head — what this task was called on the left, and how
// to get back to the list on the right — AND whether it had room for that right
// corner at all. The frame asks for both in one call so the head and the foot
// cannot disagree about who is naming `esc back` on this frame
// ([taskCardFootKeys]).
func (a *app) taskCardTitleLine(width int, entry session.TaskIndexEntry) (string, bool) {
	words := strings.TrimSpace(entry.Title)
	if words == "" {
		words = strings.TrimSpace(entry.Label)
	}
	right := taskCardBackWord + " "
	room := width - ansi.StringWidth(right) - 1
	if room < 1 {
		return fit(" "+a.pal.bold(a.pal.ink(words)), width), false
	}
	words = fit(words, room)
	left := " " + a.pal.bold(a.pal.ink(words))
	gap := width - ansi.StringWidth(" "+words) - ansi.StringWidth(right)
	if gap < 1 {
		return fit(left, width), false
	}
	return left + strings.Repeat(" ", gap) + a.pal.dim(right), true
}

// taskCardBody is everything under the rule, in one fixed order: what state the
// work came home in and when, what came of it, what it cost, what it wrote,
// where it left it, and the last thing it said.
//
// THE EMPTINESS LAW REACHES EVERY LINE OF IT. A record row is written from
// whatever the run could say, and half of them are silent about half of these:
// a node that spent nothing has no money line, a node that wrote nothing has no
// count, a node whose working copy is gone has a branch instead of a path, and a
// node this conversation is still running has no age at all. Nothing here is
// drawn as a zero, and a band with nothing in it takes no blank line either.
func (a *app) taskCardBody(entry session.TaskIndexEntry, width int) []string {
	if width < 1 {
		width = 1
	}
	pal := a.pal
	// The bands, in order. Each is joined with ONE blank between the ones that
	// survive, which is the detail column's own assembly (home.go's [homeBands]):
	// whitespace is how this surface separates blocks.
	var bands [][]string

	if line := a.taskCardWhenLine(entry); line != "" {
		bands = append(bands, []string{pal.ink(fit(line, width))})
	}
	// THE OUTCOME IS THE SENTENCE THE PERSON CAME FOR, so it is the first thing
	// under the state and it is drawn in ink rather than in the dim every other
	// fact here wears.
	if outcome := strings.TrimSpace(entry.Outcome); outcome != "" {
		var said []string
		for _, wrapped := range wrap(outcome, width) {
			said = append(said, pal.ink(wrapped))
		}
		bands = append(bands, said)
	}

	var facts []string
	// WHERE IT CAME OUT OF, because the page a person opens to learn MORE may
	// not know less than the row they opened it from. The row carries the
	// conversation (tasksplace.go's [tasksFacts]); the card dropped it, and on a
	// machine with three projects and twelve conversations `Port the picker onto
	// the new list` alone does not say which codebase it touched.
	if line := a.taskCardSourceLine(entry); line != "" {
		facts = append(facts, pal.dim(fit(line, width)))
	}
	if line := taskCardSpendLine(entry); line != "" {
		facts = append(facts, pal.dim(fit(line, width)))
	}
	if entry.FilesChanged > 0 {
		word := taskCardFilesMany
		if entry.FilesChanged == 1 {
			word = taskCardFilesOne
		}
		facts = append(facts, pal.dim(fit(itoa(entry.FilesChanged)+word, width)))
	}
	bands = append(bands, facts)

	bands = append(bands, a.taskCardWhereRows(entry, width))
	bands = append(bands, a.taskCardTailRows(entry, width))

	var out []string
	for _, band := range bands {
		if len(band) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, band...)
	}
	return out
}

// taskCardWhenLine is the state the work came home in, when it landed, and how
// long it ran — one line, with every clause that has nothing behind it dropped.
func (a *app) taskCardWhenLine(entry session.TaskIndexEntry) string {
	segs := []string{taskStateWord(entry, a.recordRuns(&entry))}
	if !entry.EndedAt.IsZero() {
		segs = append(segs, taskCardEndWord(entry)+" "+session.TaskAgeWord(a.now().Sub(entry.EndedAt))+" ago")
	}
	// THE CLOCK IS WRITTEN WHEN THE WORK LANDS and is zero on every row that has
	// not, so a row still claiming to be running says nothing about how long —
	// the same reason the record's own right margin says `running` instead of an
	// age on a row that has not landed.
	if !entry.Live() {
		if ran := countUpWord(entry.Duration()); ran != "" {
			segs = append(segs, "ran "+ran)
		}
	}
	return strings.Join(segs, railSep)
}

// taskCardSourceLine is the conversation this work came out of, spelled the way
// the row that opened the card spells it: the conversation's own title, and the
// project when nothing has titled the conversation yet.
//
// IT ASKS THE READING'S OWN LADDER ([tasksRowFor]) rather than a second one, so
// the card and the row cannot name two different conversations for one piece of
// work. Where nothing knows the conversation the line is not drawn — the
// emptiness law, and `out of ` with nothing after it is a preposition standing
// in for a fact.
func (a *app) taskCardSourceLine(entry session.TaskIndexEntry) string {
	row := tasksRowFor(a.taskSheet.world, a.taskSheet.mine, entry)
	source := strings.TrimSpace(row.Title)
	if source == "" {
		source = strings.TrimSpace(row.Project)
	}
	if source == "" {
		return ""
	}
	return "out of " + source
}

// taskCardEndWord is what the clock clause CALLS the end, and it follows the
// state the segment in front of it has just named.
//
// WORK THAT FAILED DID NOT LAND. `landed` is this codebase's own word for work
// that ARRIVED, and the refused task's page opened `failed · landed 8d ago ·
// ran 4m 0s` — six words telling a person both that nothing came of the run and
// that it came home. The stamp is the same either way; only the verb over it
// was wrong.
func taskCardEndWord(entry session.TaskIndexEntry) string {
	if entry.Status == string(session.TaskFailed) {
		return "stopped"
	}
	return "landed"
}

// taskCardSpendLine is what the work ran on and what it cost. The model is
// first because it is the rate the other two figures are at, which is the
// reason the record carries it at all (session's task_index.go).
func taskCardSpendLine(entry session.TaskIndexEntry) string {
	var segs []string
	if model := strings.TrimSpace(entry.Model); model != "" {
		segs = append(segs, model)
	}
	if entry.Cost > 0 {
		segs = append(segs, dollars(entry.Cost))
	}
	if entry.Tokens > 0 {
		segs = append(segs, tokenWord(entry.Tokens)+" tok")
	}
	return strings.Join(segs, railSep)
}

// taskCardWhereRows is where the work and the story were left: the working copy
// or the branch, and the journal.
//
// BOTH ARE DOORS WHERE THEY ARE STILL THERE. The path linker stats before it
// links, so a working copy that was merged and swept is drawn as the plain text
// it is and a journal still on the disk opens on a click — which is the honesty
// rule that file is written under, arrived at from here (pathlink.go). A BRANCH
// IS NEVER A DOOR: it is a name inside a repository and not a place on the
// disk, so it is printed and nothing more.
func (a *app) taskCardWhereRows(entry session.TaskIndexEntry, width int) []string {
	var out []string
	label := func(word, shown, path string) {
		room := width - len(word) - len(railSep)
		if room < 8 {
			// Too narrow to say both. The label is what makes the value legible,
			// so the value goes and the row is left off entirely rather than
			// printed under a word that no longer explains it.
			return
		}
		shown = fit(shown, room)
		if path != "" {
			shown = a.pathLink(path, shown)
		}
		out = append(out, a.pal.dim(word+railSep)+a.pal.muted(shown))
	}
	// OVER A CONNECTION A PATH ON THIS CARD IS THE OTHER MACHINE'S, SO IT IS SAID
	// WITH THAT MACHINE'S NAME AND NEVER OFFERED AS A DOOR ([app.hostedPath]).
	// Two things go wrong otherwise, and both were on the frame: `~` collapses
	// against THIS home directory, so a far path under a home with the same shape
	// came out claiming to be one here; and a click would ask this laptop for a
	// file that is on the server — which the linker would in fact refuse (a task's
	// journal is outside the two roots internal/remote hands anything over under),
	// so the anchor would be a door onto nothing. The BRANCH row is unaffected: a
	// branch is a name inside a repository rather than a place on a disk, and it
	// was never a door on either machine.
	where := func(word, path string) {
		if a.hosted() {
			label(word, a.hostedPath(path), "")
			return
		}
		label(word, taskCardShown(path, a.tilde), path)
	}
	var artifactPath string
	if uri := strings.TrimSpace(entry.ArtifactURI); uri != "" {
		if path := taskURIPath(uri); path != "" {
			artifactPath = filepath.Clean(path)
			where(taskCardGroundWord(entry), path)
		} else {
			label(taskCardBranchWord, strings.TrimPrefix(uri, "git:"), "")
		}
	}
	if path := strings.TrimSpace(entry.Where); path != "" && (artifactPath == "" || filepath.Clean(path) != artifactPath) {
		if path == "task folder" {
			label(taskCardPlaceWord, path, "")
		} else {
			where(taskCardPlaceWord, path)
		}
	}
	if path := taskURIPath(entry.TranscriptURI); path != "" {
		where(taskCardTranscriptWord, path)
	}
	return out
}

// taskCardGroundWord labels the directory a piece of work was done in, in the
// words the ground ladder keeps for the rung that made it (internal/session's
// groundladder.go).
//
// THE WORDS ARE THE ENGINE'S AND NEVER THIS PACKAGE'S. The rung is the only
// thing that knows whether the directory was a branch of the person's repository
// or a copy of their folder, and a surface that spelled its own answer would be
// a second source for one fact — which is exactly the drift this row was already
// in, saying `worktree` about a task that had worked in a fork.
//
// A ROW WITH NO ANSWER FALLS BACK TO THE PLACE WORD. The record is append-only
// and holds rows from before the rung was written down, and a node given a
// folder of its own on the reference promise climbed no rung at all — so
// [session.GroundWord] hands back "" and this says `where`, which names the
// place and claims nothing about what made it.
func taskCardGroundWord(entry session.TaskIndexEntry) string {
	if word := session.GroundWord(entry.Rung, entry.Mode); word != "" {
		return word
	}
	return taskCardPlaceWord
}

// taskCardGoneWord is "the journal this row names is not there any more", said
// about the disk it was actually looked for on.
func (a *app) taskCardGoneWord() string {
	if a.hosted() {
		return taskCardTailGoneOn + a.host + taskCardTailGoneEnd
	}
	return taskCardTailGone
}

// taskCardShown is a path as this card prints it: the home directory
// abbreviated to "~" and NOTHING ELSE abbreviated at all.
//
// It is deliberately not [shortPath]'s fish-style initials, which the status
// sheet spends because a sheet row is one line at forty-four columns. This card is the
// whole frame, and the question it is answering is WHERE the work went — an
// answer of `~/.a/v3/p/-U-s/t/…jsonl` is a path nobody can retype, on a screen
// with room for the real one. What it does not fit in is cut by [fit], and the
// anchor over it opens the whole file regardless (pathlink.go).
func taskCardShown(path, home string) string {
	if home = strings.TrimRight(home, "/"); home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// taskCardTailRows is the last thing the node said, under its own dim heading.
//
// IT IS THE WHOLE REPORT AND NOT THE FIRST SENTENCE. [session.TaskIndexEntry]'s
// outcome is that sentence, cut, and it is already drawn above; this is what the
// row's TranscriptURI was carried for. The card scrolls, so a long report is
// read down rather than clipped, and the transcript's own path is linked two
// bands up for anybody who wants the rest of the file.
//
// THE READ IS NOT DRAWN UNTIL IT HAS HAPPENED. Until then this is empty rather
// than a heading with a blank under it, which is the emptiness law applied to a
// fact that is merely late.
func (a *app) taskCardTailRows(entry session.TaskIndexEntry, width int) []string {
	named := taskURIPath(entry.TranscriptURI) != ""
	if !a.taskSheet.tailRead {
		// OVER A CONNECTION A READ IN FLIGHT IS SAID, NOT LEFT BLANK. At home the
		// journal opens in a millisecond and the emptiness law is the right answer
		// to a fact that is merely late; down an ssh pipe it is a round trip, and a
		// card that stood empty and then grew a paragraph reads as a card that was
		// wrong first. So the far one says which machine it is waiting on.
		if a.hosted() && named {
			return []string{a.pal.dim(fit(taskCardTailReading+a.host, width))}
		}
		return nil
	}
	if a.taskSheet.tailUnread {
		// THE MACHINE COULD NOT BE ASKED, which is not the same claim as the file
		// being gone — every other fact on this card came over on the walk and is
		// still true, and only this one thing is missing.
		if !named {
			return nil
		}
		return []string{a.pal.dim(fit(taskCardTailUnread+a.host, width))}
	}
	if strings.TrimSpace(a.taskSheet.tail) == "" {
		// A ROW THAT NAMES A TRANSCRIPT AND HAS NO REPORT SAYS WHY. The file is
		// named on the band above, so silence here would read as a card that gave
		// up half way. A row that named no transcript at all says nothing: there
		// was never anything to open.
		if !named {
			return nil
		}
		// AND A JOURNAL THAT IS STILL THERE SAYS NOTHING AT ALL. `Kept` is the
		// machine that owns the file answering the question this line used to
		// guess at from an empty string — a node that landed without a closing
		// word is not a node whose transcript was deleted, and over --host the
		// guess was wrong about every row on the far machine.
		if a.taskSheet.tailKept {
			return nil
		}
		return []string{a.pal.dim(fit(a.taskCardGoneWord(), width))}
	}
	out := []string{a.pal.dim(fit(taskCardTailHead, width))}
	for _, para := range strings.Split(a.taskSheet.tail, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		for _, wrapped := range wrap(para, width) {
			out = append(out, a.pal.muted(wrapped))
		}
	}
	return out
}
