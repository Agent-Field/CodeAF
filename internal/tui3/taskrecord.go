package tui3

import (
	"net/url"
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
	// next keystroke — the same honesty [taskSheetFilterKeys] keeps.
	taskCardBackWord = "esc back"
	// taskCardKeys is the foot: the way out, the scroll, and the one gesture
	// this card carries that the row it came from used to.
	taskCardKeys = "esc back · ↑↓ scroll · m puts it in your message"
	// taskCardTailHead heads the report. "what it said at the end" and not
	// "final assistant message": the node is a thing that did some work and then
	// said how it went, and that is the sentence a person came here to read.
	taskCardTailHead = "what it said at the end"
	// taskCardTailGone is what the card says when the row NAMES a transcript and
	// the file is not on this disk any more — a session folder somebody deleted,
	// a machine the work happened on and this one is not. It is said rather than
	// left blank because the row's own transcript line is still printed above it,
	// and a path with nothing under it reads as a card that failed to load.
	taskCardTailGone = "its transcript is not on this disk any more"
	// The labels on the two places a piece of work left something behind. They
	// are words and not glyphs because they are the only lines on the card whose
	// meaning is not obvious from what follows them.
	taskCardTreeWord       = "worktree"
	taskCardBranchWord     = "branch"
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
	// THE OTHER FULLSCREEN PAGES STAND DOWN. This is an open path onto the page,
	// so it owes the same law every other one does ([app.standDownFullscreen],
	// settings.go) — and it is stated here rather than left to [app.openTaskSheet]
	// because this one does not go through it.
	a.standDownFullscreen()
	a.taskSheet = taskSheet{open: true, detail: *entry, detailOn: true}
	// The list underneath is parked on the row that was pressed, so esc comes
	// back to it rather than to the top of a list somebody scrolled a long way
	// down. It is done on the way IN because the list is rebuilt every frame and
	// the row's position is only knowable while the entry is in hand.
	a.taskSheetPointAt(*entry)
	a.touch()
	return a.readTaskTail(*entry)
}

// taskSheetPointAt puts the LIST's cursor on the row naming this piece of work,
// leaving it where it is when the record has no such row.
//
// It matches on the pair that identifies a row of the file — the conversation
// that ran it and the id inside that conversation — because an id alone is not
// unique across the record (session's task_index.go says so on
// [session.TaskIndexEntry.ID]).
func (a *app) taskSheetPointAt(want session.TaskIndexEntry) {
	for at, item := range a.taskSheetItems() {
		if item.entry != nil && taskSameRecord(*item.entry, want) {
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
	return func() tea.Msg {
		tail, _ := session.PeekReport(path)
		return taskTailMsg{path: path, tail: tail}
	}
}

// taskTailRead folds one journal read into the card, and drops a read that is
// about a task the person has already walked away from.
func (a *app) taskTailRead(msg taskTailMsg) {
	if !a.taskSheet.detailOn || taskURIPath(a.taskSheet.detail.TranscriptURI) != msg.path {
		return
	}
	a.taskSheet.tail, a.taskSheet.tailRead = msg.tail, true
	a.touch()
}

// closeTaskRecord backs out of the card and leaves the list up, with the cursor
// where the card was opened from.
func (a *app) closeTaskRecord() {
	a.taskSheet.detail, a.taskSheet.detailOn = session.TaskIndexEntry{}, false
	a.taskSheet.detailTop, a.taskSheet.tail, a.taskSheet.tailRead = 0, "", false
	a.touch()
}

// taskURIPath is the LOCAL FILE a row's URI names, or "" for a URI that names
// anything else.
//
// The record carries two URIs and one of them is sometimes not a file at all: a
// node whose worktree was pruned keeps its branch, spelled `git:task/…`
// (session's taskArtifactURI). A file URI naming a host names another machine's
// disk, which is the same refusal the path linker makes about one (pathlink.go).
// Everything this returns is a path this machine can be asked to stat.
func taskURIPath(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	parsed, err := url.Parse(uri)
	if err != nil || !strings.EqualFold(parsed.Scheme, "file") || parsed.Path == "" {
		return ""
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		return ""
	}
	return parsed.Path
}

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
func (a *app) taskCardPress(y int) {
	if _, ok := a.taskCardHitAt(y); !ok {
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
	add(a.taskCardTitle(width, entry), taskCardHitHead)
	add("", taskCardHitHead)
	add(pal.dim(rule(width)), taskCardHitNone)

	head := len(lines)
	room := height - head - taskCardFoot
	if room < 1 {
		room = 1
	}

	body := a.taskCardBody(entry, width-2)
	a.taskSheet.detailTop = clampTop(a.taskSheet.detailTop, len(body), room)
	for i := 0; i < room; i++ {
		at := a.taskSheet.detailTop + i
		if at >= len(body) {
			// Padding, and it answers to NOTHING — a gap that fell through to the
			// way out would be a click that did something a person could not see
			// coming ([app.expandFrame] holds the original of this).
			add("", taskCardHitNone)
			continue
		}
		add(" "+body[at], taskCardHitNone)
	}

	add(pal.dim(rule(width)), taskCardHitNone)
	add(" "+pal.dim(fit(taskCardKeys, width-2)), taskCardHitFoot)

	// A terminal too short for the whole card keeps its head and its foot: what
	// this is, and how to leave. It is [app.taskSheetFrame]'s own trim.
	if len(lines) > height && height > 1 {
		lines = append(lines[:1], lines[len(lines)-(height-1):]...)
		hits = append(hits[:1], hits[len(hits)-(height-1):]...)
	}
	return lines, hits, 0, 0
}

// taskCardTitle is the head: what this task was called on the left, and how to
// get back to the list on the right.
func (a *app) taskCardTitle(width int, entry session.TaskIndexEntry) string {
	words := strings.TrimSpace(entry.Title)
	if words == "" {
		words = strings.TrimSpace(entry.Label)
	}
	right := taskCardBackWord + " "
	room := width - ansi.StringWidth(right) - 1
	if room < 1 {
		return fit(" "+a.pal.bold(a.pal.ink(words)), width)
	}
	words = fit(words, room)
	left := " " + a.pal.bold(a.pal.ink(words))
	gap := width - ansi.StringWidth(" "+words) - ansi.StringWidth(right)
	if gap < 1 {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", gap) + a.pal.dim(right)
}

// taskCardBody is everything under the rule, in one fixed order: what state the
// work came home in and when, what came of it, what it cost, what it wrote,
// where it left it, and the last thing it said.
//
// THE EMPTINESS LAW REACHES EVERY LINE OF IT. A record row is written from
// whatever the run could say, and half of them are silent about half of these:
// a node that spent nothing has no money line, a node that wrote nothing has no
// count, a node whose worktree was pruned has a branch instead of a path, and a
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
		segs = append(segs, "landed "+session.TaskAgeWord(a.now().Sub(entry.EndedAt))+" ago")
	}
	// THE CLOCK IS WRITTEN WHEN THE WORK LANDS and is zero on every row that has
	// not, so a row still claiming to be running says nothing about how long —
	// the same reason [taskRecordNote] leaves the age off a live row.
	if !entry.Live() {
		if ran := countUpWord(entry.Duration()); ran != "" {
			segs = append(segs, "ran "+ran)
		}
	}
	return strings.Join(segs, railSep)
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

// taskCardWhereRows is where the work and the story were left: the worktree or
// the branch, and the journal.
//
// BOTH ARE DOORS WHERE THEY ARE STILL THERE. The path linker stats before it
// links, so a worktree that was merged and pruned is drawn as the plain text it
// is and a journal still on the disk opens on a click — which is the honesty
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
	if uri := strings.TrimSpace(entry.ArtifactURI); uri != "" {
		if path := taskURIPath(uri); path != "" {
			label(taskCardTreeWord, taskCardShown(path, a.tilde), path)
		} else {
			label(taskCardBranchWord, strings.TrimPrefix(uri, "git:"), "")
		}
	}
	if path := taskURIPath(entry.TranscriptURI); path != "" {
		label(taskCardTranscriptWord, taskCardShown(path, a.tilde), path)
	}
	return out
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
	if !a.taskSheet.tailRead {
		return nil
	}
	if strings.TrimSpace(a.taskSheet.tail) == "" {
		// A ROW THAT NAMES A TRANSCRIPT AND HAS NO REPORT SAYS WHY. The file is
		// named on the band above, so silence here would read as a card that gave
		// up half way. A row that named no transcript at all says nothing: there
		// was never anything to open.
		if taskURIPath(entry.TranscriptURI) == "" {
			return nil
		}
		return []string{a.pal.dim(fit(taskCardTailGone, width))}
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
