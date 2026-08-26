package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// memoryPanelRows matches the model picker's twelve-row reading window. It is
// what `pgup` and `pgdown` step by inside this place's boxes.
const memoryPanelRows = 12

const (
	// SCREEN 2d's register: the verb first, the way out last. Its own sentence is
	// `enter open a shelf · → verbs: fix the wording, forget it, settle it · alt+t
	// only what was tidied · tab next place`, and the two clauses missing here are
	// missing because the keys are: the verbs' own names live on the row's `→` strip
	// (verbstrip.go) and `alt+t` has nothing behind it yet. A foot may not name a
	// key that does nothing — SCREEN 3a's whole law is that nothing is drawn that is
	// not real — so this line says what is bound and no more.
	memoryFilterHint = "enter open a shelf · → verbs · ↑↓ move · type to filter · alt+s walk the shelves · esc close"
	memoryEditHint   = "edit memory · enter save · esc cancel"
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

// memoryPanel is the whole memory place: the snapshot it is drawing, which
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
type memoryPanel struct {
	open bool
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
	// snapshot, the filter or a fold actually changed ([memoryPanel.rank]).
	reading memoryReading
	filter  editor
	// cursor is a LINE OF THE READING and not an index into the memories: the
	// body is shelves, lines and folds together, and the pointer stops only where
	// the reading says there is something to stand on ([memoryReading.at]).
	cursor int
	// top and shown are the WINDOW the last draw put over the reading, and hover
	// the line the pointer is over (-1 for none) — the same three fields every
	// promoted place keeps, meaning the same thing on each: the window follows
	// the cursor ([placeTop]), and the pointer previews where the cursor selects.
	top, shown int
	hover      int
	// expanded is the one line whose card is up, and origins is where each such
	// line was learned. The provenance is read for THAT ONE ID on the keystroke
	// that opens it, which is one query for one door rather than one per row.
	expanded string
	origins  map[string]memoryOrigin
	// edit is the wording being fixed, and editID the line it belongs to.
	edit   *editor
	editID string
	// undoID and undoName are the one forget that can be taken back, and footer
	// is the receipt that says so.
	undoID   string
	undoName string
	footer   string
}

func (p *memoryPanel) close() { *p = memoryPanel{} }

// start takes one snapshot and makes it the page.
func (p *memoryPanel) start(shelves store.MemoryShelves, now time.Time) {
	*p = memoryPanel{
		open: true, shelves: shelves, read: now, hover: -1,
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
func (p *memoryPanel) refresh(shelves store.MemoryShelves, now time.Time) {
	if !p.open {
		return
	}
	was, _ := p.reading.at(p.cursor)
	p.shelves, p.read = shelves, now
	p.rank()
	p.followStop(was)
}

// rank rebuilds the reading from the snapshot and the filter. It is called
// `rank` because that is what the overlay's own re-filter was called and what
// every paste path on this surface still asks for by name (app.go).
func (p *memoryPanel) rank() {
	p.reading = readMemory(p.shelves, p.shelfOpen, p.filter.String(), p.read)
	p.cursor = p.nearestStop(p.cursor)
}

// followStop puts the cursor back on the thing it was standing on rather than
// on the line number it was standing at.
//
// A LIST THAT REORDERS UNDER A CURSOR HAS MOVED THE CURSOR — home's own refresh
// says it in those words. A shelf that grew past another one between two beats
// genuinely re-sorts this page, and a pointer that stayed at line seven would
// land somebody on a memory they never chose, with `f forget it` one keypress
// away.
func (p *memoryPanel) followStop(was memoryStop) {
	if was.shelf == "" && was.line == nil {
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
		case was.line == nil && stop.line == nil && stop.shelf == was.shelf:
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
func (p *memoryPanel) nearestStop(from int) int {
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
func (p *memoryPanel) move(delta int) {
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
func (p *memoryPanel) stops() []int {
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
func (p *memoryPanel) choice() (store.Memory, bool) {
	stop, ok := p.reading.at(p.cursor)
	if !ok || stop.line == nil {
		return store.Memory{}, false
	}
	return *stop.line, true
}

// shelfUnder is the shelf the cursor is standing ON — the heading itself, and
// not the shelf a line happens to sit on. `enter` unrolls a heading; a line has
// its own door.
func (p *memoryPanel) shelfUnder() (string, bool) {
	stop, ok := p.reading.at(p.cursor)
	if !ok || stop.line != nil {
		return "", false
	}
	return stop.shelf, true
}

// toggleShelf is `enter` on a heading: unroll it, or roll it up again.
func (p *memoryPanel) toggleShelf(scope string) {
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
func (p *memoryPanel) cycleShelf() {
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
func (p *memoryPanel) forget(id string) {
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
func (p *memoryPanel) card(width int, pal palette) []string {
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

func (p *memoryPanel) byID(id string) (store.Memory, bool) {
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

// ── the frame ───────────────────────────────────────────────────────────────

// memoryFrame draws the memory place: the shelves, or the one line whose card
// is open.
//
// THE BODY IS A READING AND THE READING IS PURE (memoryplace.go's [readMemory]).
// Nothing here opens the store, and the reading itself was built when the
// snapshot, the filter or a fold last changed — so a resize is a re-measure of
// words already decided rather than five hundred rows re-ranked.
func (a *app) memoryFrame(width, height int) ([]string, []int, int, int) {
	lines, hits, caretX, caretY := placeFrame(a, width, height, func(width, room int) []placeRow {
		p := &a.memPanel
		var body []string
		switch {
		case p.expanded != "":
			body = p.card(width, a.pal)
		default:
			body = p.reading.rows(width, a.pal)
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
			if _, stop := p.reading.at(i); stop && p.expanded == "" && (i == p.cursor || i == p.hover) {
				text = a.pal.selected(text, width)
			}
			rows = append(rows, placeRow{text: text, hit: i})
		}
		p.shown = len(rows)
		for len(rows) < room {
			rows = append(rows, placeRow{text: "", hit: -1})
		}
		return rows
	})
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
	// nothing. SCREEN 1f'S PREAMBLE is the law: the place opens on its own three
	// sentences ([memoryTeaching]), which is exactly the reading somebody who has
	// never seen this page needs, and the one fact those sentences cannot carry —
	// that there is no store here to hold any of it — is said once on the note
	// line under them ([memoryOffNote], [memoryPanel.footer]).
	shelves, why := a.memorySnapshot()
	// AND IT JOINS THE EXCLUSION LAW, for the standing place's reason exactly
	// ([app.standDownFullscreen]).
	a.standDownFullscreen()
	a.page = pageMemory
	a.memPanel.start(shelves, a.now())
	a.memPanel.footer = why
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
	if !a.memPanel.open || !a.memoryReady() {
		return
	}
	shelves, err := a.memory.Snapshot(memorySnapshotRows)
	if err != nil {
		// A BEAT THAT CANNOT READ SAYS NOTHING AND KEEPS WHAT IT HAD. The page
		// was true three seconds ago, which is a better thing to be looking at
		// than an error line that arrives on its own every three seconds.
		return
	}
	a.memPanel.refresh(shelves, a.now())
}

func (a *app) memoryKey(msg tea.KeyPressMsg) tea.Cmd {
	// THE ROUTER IS READ FIRST, AND IT IS ONE FUNCTION FOR EVERY PLACE
	// (placekeys.go). It claims the chords that mean the same thing wherever you
	// are standing — alt+1…7, tab, alt+enter, alt+., the shift arrows, and `→`
	// when the row has verbs — and hands everything else straight back, so this
	// handler keeps its right of first refusal over its own keys.
	if cmd, took := a.placeKey(msg); took {
		return cmd
	}
	p := &a.memPanel
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
			listNavigate(msg, p.edit, func(int) {}, func() {}, memoryPanelRows)
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
		a.leavePage(pageMemory)
		p.close()
	case "enter":
		if scope, ok := p.shelfUnder(); ok {
			p.toggleShelf(scope)
			break
		}
		if memory, ok := p.choice(); ok {
			p.expanded = memory.ID
			// ONE QUERY, FOR ONE LINE, ON THE KEYSTROKE THAT ASKED FOR IT. The
			// overlay asked this of every memory it had just listed; a door asks it
			// of the one thing behind the door.
			if _, held := p.origins[memory.ID]; !held && a.memory != nil {
				if _, title, at, err := a.memory.MemoryProvenance(memory.ID); err == nil {
					p.origins[memory.ID] = memoryOrigin{title: title, at: at}
				}
			}
		}
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
	// back (verbstrip.go's [app.memoryRowVerbs]).
	//
	// `tab` cycled which shelf this place shows. `tab` is the way to the next
	// place now, so the view moved to `alt+s` — the class a view belongs to
	// (placekeys.go's [app.placeAlt]).
	default:
		listNavigate(msg, &p.filter, p.move, p.rank, memoryPanelRows)
	}
	a.touch()
	return nil
}

// setText corrects one line's wording in the held snapshot.
func (p *memoryPanel) setText(id, text string) {
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
