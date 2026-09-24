package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/store"
)

// memoryPlaceRows matches the model picker's twelve-row reading window. It is
// what `pgup` and `pgdown` step by inside this place's boxes.
const memoryPlaceRows = 12

const (
	// SCREEN 1f'S FOOT, WORD FOR WORD, over the row it is drawn over — a LINE.
	// The two letters on it are the row's `→` strip (verbstrip.go), which is where
	// they are bound; the foot names them because 1f names them, and because the
	// answer to "what can I do with this line" is a worse answer for leaving them
	// out.
	memoryLineHint = "enter " + memoryAskWord + " · e " + memoryFixWord + " · f " + memoryForgetWord
	// AND A SHELF HEADING IS A DIFFERENT ROW WITH A DIFFERENT enter. The foot used
	// to be one constant for every row, so on a line — where `enter` does not open
	// a shelf — it named a key and described something else. pages.go's contract
	// for a hint is "what the row under the cursor can be asked for".
	memoryShelfHint = "enter open a shelf · type to filter · alt+s walk the shelves"
	memoryEditHint  = "edit memory · enter save · esc cancel"
	// AND A PAGE WITH NOTHING ON IT HAS ONLY A WAY OUT. [placeTailed] adds `tab
	// next place` in front of the `esc`, so this is the whole of the foot on a
	// machine that has remembered nothing — three keys fewer than the shelf line
	// and none of them a promise the body cannot keep.
	memoryBareHint = "esc"
)

// The words this place's own doors are spelled in, once.
const (
	// memoryAskWord is `enter` on a line (SCREEN 1f): the line goes into a fresh
	// conversation and you talk about it there.
	memoryAskWord = "ask me about it"
	// memoryAskOpening is the sentence that carries it. THE LINE'S OWN WORDS ARE
	// THE MESSAGE and the clause in front of them says what they are: a
	// conversation opened with a bare belief in it reads as somebody asserting
	// that belief, which is the opposite of asking about it.
	memoryAskOpening = "about something you remember: "
)

// MemoryStore is the exact durable seam the memory place needs. Keeping it
// narrow makes the memory-off state structural and makes every mutation
// testable.
//
// ── THE TWO READERS AT THE TOP ARE THE WHOLE OF WHAT THE PLACE DRAWS ────────
//
// The panel this place grew out of read `ListMemories("", 500)` and then asked
// the store for one memory's PROVENANCE per row — up to five hundred and one
// round trips, on the keystroke, before a frame could return. That survives
// behind a slash command somebody opens once a week. It does not survive on a
// page in a tab bar with a three-second clock behind it, so the drawing reads
// [MemoryStore.Snapshot] instead: two statements in one read transaction,
// whatever a person has remembered (internal/store's memory_snapshot.go states
// the law and does the counting in SQLite).
//
// The three WRITERS below are unchanged, and [MemoryStore.ListMemories] stays
// because the undo needs to re-read after a restore — a snapshot is a reading
// and a restore is a write, and a place that redrew from a stale reading after
// putting a line back would be a screen arguing with the disk.
type MemoryStore interface {
	// Snapshot is everything remembered, shelved and counted, in a fixed number
	// of statements. limit caps the ROWS; the counts are exact whatever it is.
	Snapshot(limit int) (store.MemoryShelves, error)
	// ChangedSince is how many memories were learned after t and how many were
	// let go of after it — the two figures a tab's count is made of. A zero t
	// answers zeros, because a delta with no origin is not a delta.
	ChangedSince(t time.Time) (learned, letGo int, err error)

	ListMemories(scope string, limit int) ([]store.Memory, error)
	UpdateMemory(id, title, text string, tags []string) error
	ForgetMemory(id string) error
	RestoreMemory(id string) error
	MemoryProvenance(id string) (string, string, time.Time, error)
}

type memoryStore = MemoryStore

// memoryOrigin is where and when one memory was learned — the provenance read,
// kept per id so that opening a line twice asks the store once.
type memoryOrigin struct {
	title string
	at    time.Time
}

// memoryPlace is the whole memory place: the snapshot it is drawing, which
// shelves are unrolled, what is typed into the filter, and the one line an
// editor or an undo is about.
//
// ── IT HOLDS A SNAPSHOT AND FILTERS THE SNAPSHOT ────────────────────────────
//
// The overlay this grew out of held every memory as a flat slice and re-ranked
// it on every keystroke, which was survivable; what was not survivable is that
// it asked the store for a row's provenance ONCE PER MEMORY as it opened — up
// to five hundred and one queries before a frame returned. A place on a tab bar
// with a three-second clock behind it cannot pay that, so the whole shape is
// read in two statements ([MemoryStore.Snapshot]) on the clock and on the
// keystroke that walks in, and TYPING FILTERS WHAT IS ALREADY HELD. Nothing in
// this file opens the store on a draw, and nothing but `enter` on a line opens
// it on a keypress.
type memoryPlace struct {
	// shelves is the snapshot the body is drawn from, and read is the instant it
	// was taken. Every age on the page is measured from that instant rather than
	// from a fresh clock, so two rows drawn in one frame cannot disagree about
	// how old they are (the tasks place states the same law).
	shelves store.MemoryShelves
	read    time.Time
	// shelfOpen is which shelves are unrolled, by scope. It is a map rather than
	// an index because the reading re-sorts the shelves biggest-first as the
	// filter narrows them, and a shelf remembered by POSITION would unroll a
	// different shelf the moment a letter was typed.
	shelfOpen map[string]bool
	// reading is the last built body: pure, derived, and rebuilt only when the
	// snapshot, the filter or a fold actually changed ([memoryPlace.rank]).
	reading memoryReading
	filter  editor
	// cursor is a LINE OF THE READING and not an index into the memories: the
	// body is shelves, lines and folds together, and the pointer stops only where
	// the reading says there is something to stand on ([memoryReading.at]).
	cursor int
	// top and shown are the WINDOW the last draw put over the reading, and hover
	// the line the pointer is over (-1 for none) — the same three fields every
	// promoted place keeps, meaning the same thing on each: the window follows
	// cursor ([placeTop]), which mouse and keyboard navigation both move.
	top, shown int
	hover      int
	// expanded is the one line whose card is up, and origins is where each such
	// line was learned. The provenance is read for THAT ONE ID on the keystroke
	// that opens it, which is one query for one door rather than one per row.
	expanded string
	origins  map[string]memoryOrigin
	// width is the frame the reading was last laid out for. It is kept because
	// the teaching prose is WRAPPED into the reading rather than cut on the way
	// out ([memoryReading.wrapped]), so a resize is a re-lay and not only a
	// re-measure.
	width int
	// edit is the wording being fixed, and editID the line it belongs to.
	edit   *editor
	editID string
	// undoID and undoName are the one forget that can be taken back, and footer
	// is the receipt that says so.
	undoID   string
	undoName string
	footer   string
}

func (p *memoryPlace) close() { *p = memoryPlace{} }

// start takes one snapshot and makes it the page.
func (p *memoryPlace) start(shelves store.MemoryShelves, now time.Time) {
	*p = memoryPlace{
		shelves: shelves, read: now, hover: -1,
		shelfOpen: map[string]bool{}, origins: map[string]memoryOrigin{},
	}
	// THE BIGGEST SHELF OPENS ITSELF AND THE REST STAY ROLLED UP (SCREEN 2d).
	// A page of three closed headings teaches nothing about what is on them, and
	// a page with all of them open is the flat list this shape exists to replace.
	p.rank()
	for _, line := range p.reading.lines {
		if line.kind == memoryReadingShelf {
			p.shelfOpen[line.shelf] = true
			break
		}
	}
	p.rank()
}

// refresh replaces the snapshot under a page that is already up, keeping the
// filter, the folds and — where it can — the line the cursor was on.
func (p *memoryPlace) refresh(shelves store.MemoryShelves, now time.Time) {
	was, _ := p.reading.at(p.cursor)
	p.shelves, p.read = shelves, now
	p.rank()
	p.followStop(was)
}

// rank rebuilds the reading from the snapshot and the filter. It is called
// `rank` because that is what the overlay's own re-filter was called and what
// every paste path on this surface still asks for by name (app.go).
func (p *memoryPlace) rank() {
	p.reading = readMemory(p.shelves, p.shelfOpen, p.filter.String(), p.read).wrapped(p.width)
	p.cursor = p.nearestStop(p.cursor)
}

// remeasure re-lays the reading for a frame of this width, keeping the cursor on
// the THING it was standing on rather than on the line number it was standing at
// — [memoryPlace.followStop]'s law, for the same reason: wrapping the prose to a
// narrower frame moves every line under it down.
func (p *memoryPlace) remeasure(width int) {
	if width < 1 || p.width == width {
		return
	}
	was, _ := p.reading.at(p.cursor)
	p.width = width
	p.rank()
	p.followStop(was)
}

// followStop puts the cursor back on the thing it was standing on rather than
// on the line number it was standing at.
//
// A LIST THAT REORDERS UNDER A CURSOR HAS MOVED THE CURSOR — home's own refresh
// says it in those words. A shelf that grew past another one between two beats
// genuinely re-sorts this page, and a pointer that stayed at line seven would
// land somebody on a memory they never chose, with `f forget it` one keypress
// away.
func (p *memoryPlace) followStop(was memoryStop) {
	if was.shelf == "" && was.line == nil && was.fold == "" {
		return
	}
	for i := range p.reading.lines {
		stop, ok := p.reading.at(i)
		if !ok {
			continue
		}
		switch {
		case was.line != nil && stop.line != nil && stop.line.ID == was.line.ID:
			p.cursor = i
			return
		case was.fold != "" && stop.fold == was.fold:
			p.cursor = i
			return
		case was.line == nil && was.fold == "" && stop.line == nil && stop.fold == "" && stop.shelf == was.shelf:
			p.cursor = i
			return
		}
	}
	p.cursor = p.nearestStop(p.cursor)
}

// nearestStop is the first line at or after `from` that can be stood on, and
// the last one before it when there is none. A reading with no stops at all —
// the teaching page, a filter that matched nothing — answers zero, and the
// cursor then points at prose, which is exactly the state in which no verb is
// offered.
func (p *memoryPlace) nearestStop(from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(p.reading.lines); i++ {
		if _, ok := p.reading.at(i); ok {
			return i
		}
	}
	for i := from - 1; i >= 0; i-- {
		if _, ok := p.reading.at(i); ok {
			return i
		}
	}
	return 0
}

// move walks the cursor by whole STOPS rather than by rows, so ↓ never lands on
// a section heading or on a fold line that nothing can be done to.
func (p *memoryPlace) move(delta int) {
	stops := p.stops()
	if len(stops) == 0 {
		return
	}
	at := 0
	for i, line := range stops {
		if line == p.cursor {
			at = i
			break
		}
		if line < p.cursor {
			at = i
		}
	}
	p.cursor = stops[moveCursor(at, delta, len(stops))]
}

// stops is every line of the reading a cursor may stand on, in drawn order.
func (p *memoryPlace) stops() []int {
	var found []int
	for i := range p.reading.lines {
		if _, ok := p.reading.at(i); ok {
			found = append(found, i)
		}
	}
	return found
}

// choice is the memory under the cursor, and false where the cursor is on a
// shelf, on prose, or on nothing.
func (p *memoryPlace) choice() (store.Memory, bool) {
	stop, ok := p.reading.at(p.cursor)
	if !ok || stop.line == nil {
		return store.Memory{}, false
	}
	return *stop.line, true
}

// shelfUnder is the shelf the cursor is standing ON — the heading itself, and
// not the shelf a line happens to sit on. `enter` unrolls a heading; a line has
// its own door.
func (p *memoryPlace) shelfUnder() (string, bool) {
	stop, ok := p.reading.at(p.cursor)
	if !ok || stop.line != nil || stop.fold != "" {
		return "", false
	}
	return stop.shelf, true
}

// toggleShelf is `enter` on a heading: unroll it, or roll it up again.
func (p *memoryPlace) toggleShelf(scope string) {
	if p.shelfOpen == nil {
		p.shelfOpen = map[string]bool{}
	}
	was, _ := p.reading.at(p.cursor)
	p.shelfOpen[scope] = !p.shelfOpen[scope]
	p.rank()
	p.followStop(was)
}

// cycleShelf is `alt+s`: WHICH SHELF THIS PLACE IS SHOWING, one at a time.
//
// It was `tab` while memory was a modal overlay and it narrowed a flat list to
// one scope; `tab` is the way to the next place now, so the view moved into the
// class views belong to (placekeys.go's [app.placeAlt]). Against a body made of
// shelves the same meaning is "unroll the next one and roll the others up",
// which walks a person through the whole page on one key and ends with
// everything closed — a state `enter` cannot reach in one press.
func (p *memoryPlace) cycleShelf() {
	var scopes []string
	for _, line := range p.reading.lines {
		if line.kind == memoryReadingShelf {
			scopes = append(scopes, line.shelf)
		}
	}
	if len(scopes) == 0 {
		return
	}
	next := 0
	for i, scope := range scopes {
		if p.shelfOpen[scope] {
			next = i + 1
			break
		}
	}
	was, _ := p.reading.at(p.cursor)
	p.shelfOpen = map[string]bool{}
	if next < len(scopes) {
		p.shelfOpen[scopes[next]] = true
	}
	p.rank()
	p.followStop(was)
}

// forget takes one line off the shelves it is on, so the page redraws without
// it before the next snapshot lands. The store has already been told.
func (p *memoryPlace) forget(id string) {
	for i := range p.shelves.Shelves {
		shelf := &p.shelves.Shelves[i]
		for j := range shelf.Memories {
			if shelf.Memories[j].ID != id {
				continue
			}
			shelf.Memories = append(shelf.Memories[:j], shelf.Memories[j+1:]...)
			shelf.Held, shelf.LetGo = shelf.Held-1, shelf.LetGo+1
			p.shelves.Held, p.shelves.LetGo = p.shelves.Held-1, p.shelves.LetGo+1
			p.shelves.Shown--
			p.rank()
			return
		}
	}
}

// card is the one line's own page: what it says, what it is made of, how often
// it has helped, and where it was learned.
//
// It is drawn INSTEAD of the shelves rather than under them, for the reason
// every fullscreen page on this surface is drawn instead of the one before it:
// a card over a list is two things claiming the same rows.
func (p *memoryPlace) card(width int, pal palette) []string {
	memory, ok := p.byID(p.expanded)
	if !ok {
		return []string{pal.dim(fit("that line is not on a shelf any more", width))}
	}
	rows := []string{pal.bold(fit(memory.Title, width))}
	for _, line := range wrapText(memory.Text, width) {
		rows = append(rows, pal.ink(fit(line, width)))
	}
	rows = append(rows, "")
	var about []string
	if memory.Type != "" {
		about = append(about, memory.Type)
	}
	if word := store.MemoryShelfWord(memory.Scope); word != "" {
		about = append(about, word)
	}
	if len(memory.Tags) > 0 {
		about = append(about, "tags · "+strings.Join(memory.Tags, ", "))
	}
	if help := memoryHelp(memory, p.read); help != "" {
		about = append(about, help)
	}
	if len(about) > 0 {
		rows = append(rows, pal.dim(fit(strings.Join(about, " · "), width)))
	}
	// WHERE IT WAS LEARNED IS DRAWN ONLY WHEN THE STORE SAID. An origin nobody
	// could name is absent rather than "somewhere", which is the emptiness law
	// applied to a sentence instead of to a number.
	if origin, held := p.origins[memory.ID]; held {
		learned := ""
		if age := sinceAt(origin.at, p.read); age != "" {
			learned = "learned " + age
		}
		if origin.title != "" {
			if learned == "" {
				learned = "learned"
			}
			learned += " in '" + origin.title + "'"
		}
		if learned != "" {
			rows = append(rows, pal.dim(fit(learned, width)))
		}
	}
	return rows
}

func (p *memoryPlace) byID(id string) (store.Memory, bool) {
	for _, shelf := range p.shelves.Shelves {
		for _, memory := range shelf.Memories {
			if memory.ID == id {
				return memory, true
			}
		}
	}
	return store.Memory{}, false
}

// wrapText is the card's own wrap: whole words, measured in runes, which is
// what the card's one paragraph needs and all it needs.
func wrapText(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	lines := []string{words[0]}
	for _, word := range words[1:] {
		last := len(lines) - 1
		if len([]rune(lines[last]+" "+word)) <= width {
			lines[last] += " " + word
		} else {
			lines = append(lines, word)
		}
	}
	return lines
}

// memoryFrame is this place, drawn: the shared frame with this place's body in
// it, and the hit map cast back into the body lines this place answers with
// (pages.go's [app.placeDraw] and [placeLineHits]).
func (a *app) memoryFrame(width, height int) ([]string, []int, int, int) {
	lines, hits, caretX, caretY := a.placeDraw(placeMemory{}, width, height)
	return lines, placeLineHits(hits), caretX, caretY
}

// memoryReady is whether the memory place has a store behind it.
//
// IT IS NOT A GUARD ON OPENING ANY MORE — the place opens either way
// ([app.openMemory]) — and it is what tells the two empty pages apart: nothing
// remembered yet, or nothing that CAN be remembered here. It is also what
// /memories asks before choosing the printed list over the place, which is the
// one door that still has two honest answers.
func (a *app) memoryReady() bool {
	_, ok := a.brain()
	return ok && a.memory != nil
}

// memorySnapshotRows is how many memories one reading carries. The COUNTS on
// the page are exact whatever this is — the store counts in SQLite — so this
// bounds the rows a shelf can unroll and nothing a person reads as a total.
const memorySnapshotRows = 500

// openMemory walks into the memory place: ONE reading of the store, and the
// clock that keeps it current.
//
// It reads in TWO STATEMENTS where the overlay it replaces read up to five
// hundred and one ([MemoryStore] tells that story), and it returns the place
// clock for the reason [app.openHome] returns home's: a page that opened
// without starting one would be a photograph of a store other windows go on
// writing to.
func (a *app) openMemory() tea.Cmd {
	// THE PLACE OPENS WHETHER OR NOT THERE IS A STORE BEHIND IT. It used to
	// refuse twice — once when this build was not remembering anything, once when
	// the store would not answer — and both refusals put the person back on the
	// page they came from, so `alt+4` on a fresh machine was a key that did
	// nothing. SCREEN 1f'S PREAMBLE is the law: the place opens on its heading
	// and whisper ([placeWhisper]), and the one fact those cannot carry — that
	// there is no store here to hold any of it — is said once on the note
	// ([memoryOffNote], [memoryPlace.footer]).
	shelves, why := a.memorySnapshot()
	// AND IT JOINS THE EXCLUSION LAW, for the standing place's reason exactly
	// ([app.standDownFullscreen]).
	a.standDownFullscreen()
	a.page = pageMemory
	a.mem.start(shelves, a.now())
	a.mem.footer = why
	a.touch()
	return a.armPlaceClock()
}

// memorySnapshot is what the store holds, and — when it holds nothing because
// there is no store — the one dim sentence saying so.
//
// IT ANSWERS AN EMPTY SNAPSHOT RATHER THAN AN ERROR, because every caller draws
// a page either way now. The sentence is in the same register as the teaching
// prose above it: a fact about this machine, not a fault anybody committed
// (styles.go's THE EMPTINESS LAW covers the figures; this covers the reason).
func (a *app) memorySnapshot() (store.MemoryShelves, string) {
	if !a.memoryReady() {
		if a.hosted() {
			return store.MemoryShelves{}, memoryRemoteWord
		}
		return store.MemoryShelves{}, memoryOffNote
	}
	shelves, err := a.memory.Snapshot(memorySnapshotRows)
	if err != nil {
		return store.MemoryShelves{}, memoryUnreadableWord
	}
	return shelves, ""
}

// memoryUnreadableWord is a store that is there and will not answer. The error's
// own text is deliberately not carried onto the screen: a SQLite message is
// machinery vocabulary, and what a person can do about it is the same in every
// case. The OTHER sentence this place says about itself — that this build is not
// remembering anything at all — is [memoryOffNote], spelled once in memory.go
// and said here and in the transcript both.
const memoryUnreadableWord = "what is remembered could not be read just now"

// refreshMemory is the place clock's beat on this page: the same two statements
// again, with the filter, the folds and the line under the cursor kept.
func (a *app) refreshMemory() {
	if !a.at(pageMemory) || !a.memoryReady() {
		return
	}
	shelves, err := a.memory.Snapshot(memorySnapshotRows)
	if err != nil {
		// A BEAT THAT CANNOT READ SAYS NOTHING AND KEEPS WHAT IT HAD. The page
		// was true three seconds ago, which is a better thing to be looking at
		// than an error line that arrives on its own every three seconds.
		return
	}
	a.mem.refresh(shelves, a.now())
}

func (a *app) memoryKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.mem
	if p.edit != nil {
		switch msg.String() {
		case "esc":
			p.edit, p.editID = nil, ""
		case "enter":
			memory, ok := p.byID(p.editID)
			if ok && a.memory.UpdateMemory(memory.ID, memory.Title, p.edit.String(), memory.Tags) == nil {
				// THE HELD SNAPSHOT IS CORRECTED IN PLACE rather than re-read. The
				// write has landed; re-reading the whole store to learn one string
				// this process just wrote would be the page asking the disk what it
				// already knows, and the clock brings everything else along anyway.
				p.setText(memory.ID, p.edit.String())
			}
			p.edit, p.editID = nil, ""
		default:
			listNavigate(msg, p.edit, func(int) {}, func() {}, memoryPlaceRows)
		}
		a.touch()
		return nil
	}
	if p.expanded != "" {
		switch msg.String() {
		case "esc":
			p.expanded = ""
		case "enter":
			if memory, ok := p.byID(p.expanded); ok {
				box := editor{}
				box.setText(memory.Text)
				p.edit, p.editID = &box, memory.ID
			}
		}
		a.touch()
		return nil
	}
	switch msg.String() {
	case "esc":
		// ONE LAYER AT A TIME, the rule every place on this surface follows: a
		// filter with something in it is cleared first, and the second esc leaves.
		if !p.filter.empty() {
			p.filter.reset()
			p.rank()
			a.touch()
			return nil
		}
		a.leavePlace()
		return nil
	case "enter":
		// ONE SPELLING OF WHAT `enter` DOES HERE, and it is the interface's
		// ([placeMemory.enter]). This arm held a second copy of it, which is how
		// the two came to disagree about what the key opens.
		cmd := placeMemory{}.enter(a)
		a.touch()
		return cmd
	case "delete", "ctrl+d":
		if memory, ok := p.choice(); ok && a.memory.ForgetMemory(memory.ID) == nil {
			p.undoID, p.undoName = memory.ID, memory.Title
			// THE RECEIPT NAMES THE WAY BACK IN THE WORDS THE KEY IS ACTUALLY
			// SPELLED IN NOW. `u` alone would be a letter this place no longer
			// binds, and a receipt that names an unbound key is the exact defect
			// the strip exists to fix (verbstrip.go).
			p.footer = "forgot '" + memory.Title + "' · → " + memoryUndoWord
			p.forget(memory.ID)
		}
	// `u` AND `tab` USED TO BE HERE AND BOTH HAD TO GO.
	//
	// `u` put a forgotten line back, and it was matched ahead of the default arm
	// — so the letter could not be TYPED into the filter at all, and a search for
	// a word with a `u` in it silently restored something instead. It is a verb
	// on the row's `→` strip now, offered only while there is something to put
	// back ([placeMemory.verbs]).
	//
	// `tab` cycled which shelf this place shows. `tab` is the way to the next
	// place now, so the view moved to `alt+s` — the class a view belongs to
	// (placekeys.go's [app.placeAlt]).
	default:
		listNavigate(msg, &p.filter, p.move, p.rank, memoryPlaceRows)
	}
	a.touch()
	return nil
}

// setText corrects one line's wording in the held snapshot.
func (p *memoryPlace) setText(id, text string) {
	for i := range p.shelves.Shelves {
		for j := range p.shelves.Shelves[i].Memories {
			if p.shelves.Shelves[i].Memories[j].ID == id {
				p.shelves.Shelves[i].Memories[j].Text = text
				p.rank()
				return
			}
		}
	}
}

// ── the place ───────────────────────────────────────────────────────────────

// placeMemory is this place's handle on the registry (pages.go's [place] states
// the contract and why the handle holds no state of its own).
type placeMemory struct{ placeBase }

func init() { registerPlace(placeMemory{}) }

func (placeMemory) id() page      { return pageMemory }
func (placeMemory) word() string  { return "memory" }
func (placeMemory) counted() bool { return true }

func (placeMemory) open(a *app) tea.Cmd { return a.openMemory() }

func (placeMemory) close(a *app) {
	a.leavePage(pageMemory)
	a.mem.close()
}

func (placeMemory) tick(a *app, now time.Time) (bool, tea.Cmd) {
	a.refreshMemory()
	return true, nil
}

// body is the shelves, or the one line whose card is open.
//
// THE BODY IS A READING AND THE READING IS PURE (memoryplace.go's [readMemory]).
// Nothing here opens the store, and the reading itself was built when the
// snapshot, the filter or a fold last changed — so a resize is a re-measure of
// words already decided rather than five hundred rows re-ranked.
func (placeMemory) body(a *app, width, room int) []placeRow {
	p := &a.mem
	p.remeasure(width)
	var body []string
	switch {
	case p.expanded != "":
		// The card stands on the place's one left edge, as the list does.
		for _, line := range p.card(width-len(placeLead), a.pal) {
			body = append(body, placeLead+line)
		}
	case p.reading.bare():
		return placeWhisperRows(pageMemory, width, room, a.pal)
	default:
		body = p.reading.paint(width, a.pal, func(i int) bool { return i == p.cursor })
	}
	// THE WINDOW FOLLOWS THE CURSOR, and a card standing open is not a list:
	// it is one line's provenance, drawn from its top, so it has no cursor to
	// follow and starts where it starts.
	if p.expanded != "" {
		p.top = 0
	} else {
		p.top = placeTop(p.top, p.cursor, len(body), room)
	}
	rows := make([]placeRow, 0, room)
	for i := p.top; i < len(body); i++ {
		if len(rows) >= room {
			break
		}
		text := body[i]
		if _, stop := p.reading.at(i); stop && p.expanded == "" && i == p.cursor {
			text = placeBand(text, width, a.pal)
		}
		rows = append(rows, placeRow{text: text, hit: i})
	}
	p.shown = len(rows)
	for len(rows) < room {
		rows = append(rows, placeRow{text: "", hit: -1})
	}
	return rows
}

func (placeMemory) stops(a *app) []int { return a.mem.stops() }

// cursorAt is the line of the reading the cursor is on (pages.go's
// [place.cursorAt]).
func (placeMemory) cursorAt(a *app) int { return a.mem.cursor }
func (placeMemory) cursorRow(a *app, rows []placeRow) int {
	return placeRowAtLine(rows, a.mem.cursor)
}

// rowID is the line or the shelf under the cursor (pages.go's [place.rowID]).
// A shelf is named by its scope and a line by the memory's own id, and the two
// are kept apart by a word in front so that a shelf and a memory that happened
// to share a name are still two different rows.
func (placeMemory) rowID(a *app) string {
	p := &a.mem
	if scope, ok := p.shelfUnder(); ok {
		return "shelf\x00" + scope
	}
	if memory, ok := p.choice(); ok {
		return "line\x00" + memory.ID
	}
	return ""
}

// enter opens a shelf, or ASKS ME ABOUT A LINE (SCREEN 1f).
//
// The card used to be here, and it was the wrong thing behind this key. A line
// on this page is something codeaf believes about you; the useful thing to do
// with one is to talk about it, and the design puts that on `enter` and moves
// the card onto the row's strip, behind `c`. The conversation is started through
// the door every other place starts one through ([app.placeTalkAbout]), so a
// line asked about from here and a sentence typed into a place's box arrive the
// same way.
func (placeMemory) enter(a *app) tea.Cmd {
	p := &a.mem
	if scope, ok := p.shelfUnder(); ok {
		p.toggleShelf(scope)
		return nil
	}
	// A FOLD LINE OPENS WHERE IT STANDS and the cursor stays on it, now
	// reading `fewer`, so the next `enter` undoes it.
	if stop, ok := p.reading.at(p.cursor); ok && stop.fold != "" {
		p.toggleShelf(stop.fold)
		return nil
	}
	memory, ok := p.choice()
	if !ok {
		return nil
	}
	cmd, _ := a.placeTalkAbout(memoryAskOpening + strings.TrimSpace(memory.Text))
	return cmd
}

// openMemoryCard is the `c` verb: the line's own page — what it says, what it is
// made of, how often it has helped, and where it was learned.
func (p *memoryPlace) openMemoryCard(a *app, memory store.Memory) {
	p.expanded = memory.ID
	// ONE QUERY, FOR ONE LINE, ON THE KEYSTROKE THAT ASKED FOR IT. The overlay
	// asked this of every memory it had just listed; a door asks it of the one
	// thing behind the door.
	if _, held := p.origins[memory.ID]; !held && a.memory != nil {
		if _, title, at, err := a.memory.MemoryProvenance(memory.ID); err == nil {
			p.origins[memory.ID] = memoryOrigin{title: title, at: at}
		}
	}
}

// verbs is this place's `→` strip, and it closes a real bug: `u` (undo a forget)
// was matched ahead of the filter's default arm, so a person could not type a
// `u` into the filter box at all — a search for "must" lost its second letter
// and put a memory back instead. On the strip the letter is a verb only while
// the strip is drawn, and the filter gets every letter of the alphabet back.
func (placeMemory) verbs(a *app) []verb {
	p := &a.mem
	var verbs []verb
	// THE TWO THAT ACT ON A LINE ARE OFFERED ONLY WHILE THERE IS A LINE. A shelf
	// heading, a section line, the prose at the top of a nearly-empty page — the
	// cursor stands on all of them and none of them has wording to fix or
	// anything to forget ([memoryPlace.choice] answers only on a line).
	if memory, ok := p.choice(); ok {
		verbs = append(verbs,
			// THE CARD IS THE FIRST VERB BECAUSE IT IS THE ONE THAT ONLY LOOKS.
			// `enter` opens a conversation about this line now (SCREEN 1f), and the
			// card came here rather than being dropped: where a line was learned is
			// the fact somebody wants when they doubt it.
			verb{key: 'c', word: memoryCardWord, do: func() tea.Cmd {
				p.openMemoryCard(a, memory)
				return nil
			}},
			verb{key: 'e', word: memoryFixWord, do: func() tea.Cmd {
				box := editor{}
				box.setText(memory.Text)
				p.edit, p.editID = &box, memory.ID
				return nil
			}},
			verb{key: 'f', word: memoryForgetWord, do: func() tea.Cmd {
				if a.memory != nil && a.memory.ForgetMemory(memory.ID) == nil {
					p.undoID, p.undoName = memory.ID, memory.Title
					p.footer = "forgot '" + memory.Title + "' · → " + memoryUndoWord
					p.forget(memory.ID)
				}
				return nil
			}})
	}
	// AND THE UNDO WHENEVER THERE IS SOMETHING TO PUT BACK, WITH OR WITHOUT A ROW
	// UNDER THE CURSOR. It is the one verb here that is about the PLACE and not
	// about a line — the line it would put back is, by definition, not on the
	// screen — and forgetting the last thing on a shelf must not be the one
	// forget that cannot be taken back.
	if p.undoID != "" {
		verbs = append(verbs, verb{key: 'u', word: memoryUndoWord, do: func() tea.Cmd {
			if a.memory == nil || a.memory.RestoreMemory(p.undoID) != nil {
				return nil
			}
			// AND THE PAGE IS RE-READ RATHER THAN PATCHED. Every other change this
			// place makes is one field this process just wrote and can therefore
			// correct in the held snapshot; a restore puts back a row that was
			// REMOVED from it, with counts and a shelf and a status the store owns,
			// so the honest redraw is the store's own answer ([app.refreshMemory]
			// is the same two statements the clock runs).
			a.refreshMemory()
			p.footer = "put '" + p.undoName + "' back"
			p.undoID, p.undoName = "", ""
			return nil
		}})
	}
	return verbs
}

// alt is `alt+s`: WHICH SHELF THIS PLACE IS SHOWING. It was `tab` while memory
// was a modal overlay; `tab` is the way between places now, so the view key
// moved into the class views belong to ([memoryPlace.cycleShelf]).
func (placeMemory) alt(a *app, letter rune) bool {
	if letter != 's' {
		return false
	}
	a.mem.cycleShelf()
	return true
}

// box is the filter, or the wording being fixed when a card's editor is open.
func (placeMemory) box(a *app) *editor {
	if a.mem.edit != nil {
		return a.mem.edit
	}
	return &a.mem.filter
}

// note is the receipt line — what was just forgotten and how to put it back, or
// the one sentence saying this build remembers nothing at all.
func (placeMemory) note(a *app, width int) []string {
	if a.mem.footer == "" {
		return nil
	}
	return []string{" " + a.pal.dim(fit(a.mem.footer, width-2))}
}

// hint is WHAT THE ROW UNDER THE CURSOR CAN BE ASKED FOR (pages.go's
// [place.hint]), which on this place is two different sentences: a shelf heading
// unrolls and a line is talked about.
func (placeMemory) hint(a *app) string {
	if a.mem.edit != nil {
		return memoryEditHint
	}
	// A PAGE WITH NO SHELVES ON IT PROMISES NO SHELF KEYS. The fall-through at
	// the foot of this function is [memoryShelfHint], which is right whenever
	// there are shelves and was drawn over a bare page too — so a machine that
	// has remembered nothing read `enter open a shelf · type to filter · alt+s
	// walk the shelves` under a body with nothing to open, nothing to filter and
	// no shelves to walk. What is true there is the way out, under the whisper
	// the body already gave ([placeWhisper]).
	if a.mem.reading.bare() {
		return memoryBareHint
	}
	if _, ok := a.mem.shelfUnder(); ok {
		return memoryShelfHint
	}
	if stop, ok := a.mem.reading.at(a.mem.cursor); ok && stop.fold != "" {
		return foldEnterWord(a.mem.shelfOpen[stop.fold]) + " · type to filter · alt+s walk the shelves"
	}
	if _, ok := a.mem.choice(); ok {
		return memoryLineHint
	}
	return memoryShelfHint
}

func (placeMemory) changed(a *app, since time.Time) int { return a.memoryChangedSince(since) }

// press is enter on the row it lands on, with the one exception [place.press]
// names: a line's `enter` asks the model about it, so a press on a line opens
// its card, and the card's own `enter` and `esc` take it from there. A card
// standing open is not the list, so a press over it lands on no row of it.
func (placeMemory) press(a *app, y int) (tea.Cmd, bool) {
	p := &a.mem
	if p.expanded != "" || p.edit != nil {
		return nil, true
	}
	at, ok := placeBodyLine(y, p.top, p.shown)
	if !ok {
		return nil, true
	}
	stop, ok := p.reading.at(at)
	if !ok {
		return nil, true
	}
	p.cursor = at
	a.touch()
	if stop.line != nil {
		p.openMemoryCard(a, *stop.line)
		return nil, true
	}
	return placeMemory{}.enter(a), true
}

func (placeMemory) hover(a *app, y int) bool {
	next := -1
	if at, ok := placeBodyLine(y, a.mem.top, a.mem.shown); ok && a.mem.expanded == "" {
		if _, stop := a.mem.reading.at(at); stop {
			next = at
		}
	}
	return placeHoverMoved(&a.mem.hover, &a.mem.cursor, next, next, a)
}

func (placeMemory) wheel(a *app, delta int) (tea.Cmd, bool) {
	a.mem.move(delta)
	a.touch()
	return nil, true
}

// key is this place's own reading of a key the router did not take
// (pages.go's [place] states the split).
func (placeMemory) key(a *app, msg tea.KeyPressMsg) tea.Cmd { return a.memoryKey(msg) }

// owns is the card editor, and the door home is the reason it is here: the
// editor is a whole-keyboard surface inside [placeMemory.key], and without this
// method it lived BELOW the door in the place router — so a card cleared down to
// its last space armed the door on the editor's own box ([placeMemory.box] hands
// the editor over while it is open), and the second space wiped the wording and
// stood the person on home mid-edit. The settings panel already holds this exact
// shape for its own nested boxes ([placeSettings.owns]); this is the same claim,
// and [app.memoryKey]'s edit arm answers every key the way that one does — a
// chord it does not know is swallowed rather than walking the place and leaving
// the editor dangling open over a page the person is no longer reading.
func (placeMemory) owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.mem.edit == nil {
		return nil, false
	}
	return a.memoryKey(msg), true
}
