package tui3

import (
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// memoryPlaceRows is what `pgup` and `pgdown` step by inside this place's boxes.
const memoryPlaceRows = 12

// THE FOOT SAYS WHAT THE ROW UNDER THE CURSOR CAN BE ASKED FOR, and only keys
// that really do it: a letter on this place is a verb only while the `→` strip
// is drawn (verbstrip.go), so the foot names the arrow and never a bare letter.
// A GROUP NAMES ITS ARROW AND NOT `enter`: `→` opens it and `←` folds it back
// up, which is the Activity tab's bargain with the same two keys
// ([tasksShutWord] is that tab's word, said here unchanged).
const (
	memoryLineHint      = "enter open · → edit, forget, move · type to filter"
	memoryGoneHint      = "enter open · → " + memoryUndoWord + " · type to filter"
	memoryShelfOpenWord = "→ show its lines"
	memoryShelfOpenHint = memoryShelfOpenWord + " · type to filter"
	memoryShelfShutHint = tasksShutWord + " · type to filter"
	memoryAddHint       = "enter " + memoryAddWord + " · type to filter"
	// memoryNoMatchHint is the foot over a filter that matched nothing: the
	// add row is not drawn under a filter, so `enter` has nothing to do, and
	// the one key that does something is the one that clears it.
	memoryNoMatchHint = "esc clear the filter"
	memoryCardHint    = "→ edit, forget, move · esc back to the list"
	memoryEditHint    = "enter save · esc cancel"
	// A PAGE WITH NOTHING ON IT HAS ONLY A WAY OUT: its whisper already says
	// /remember adds a line, and the add row arrives with the first one.
	memoryBareHint = "esc"
)

// The words this place's own doors are spelled in, once.
const (
	// memoryAskOpening is the sentence `a` on a line opens a conversation with.
	// The line's own words are the message, and the clause in front says what
	// they are: a conversation opened with a bare belief reads as somebody
	// asserting it, which is the opposite of asking about it.
	memoryAskOpening = "about something you remember: "
	// memoryUnreadableWord is a store that is there and will not answer.
	memoryUnreadableWord = "what is remembered could not be read just now"
	// memoryUnchangedWord is a write the store would not take. Its own text is
	// not carried onto the screen: a SQLite message is machinery vocabulary.
	memoryUnchangedWord = "that line could not be changed just now"
	// memoryNoProjectWord is the engine not answering which project this
	// conversation is in: the tab then draws every project's lines by name.
	memoryNoProjectWord = "couldn't ask which project this is · every project's lines are shown by name"
)

// MemoryStore is the exact durable seam the memory place needs: one reading,
// one count, and the writes that act on a row the page is showing. The writes
// that need to know which project this conversation is in — adding a line and
// moving one — go through the conversation instead ([memoryAgent]).
type MemoryStore interface {
	// Snapshot is everything remembered, shelved by owner and counted.
	Snapshot(limit int) (store.MemoryShelves, error)
	// ChangedSince is how many lines were learned after t and how many were let
	// go of after it — the two figures a tab's count is made of.
	ChangedSince(t time.Time) (learned, letGo int, err error)

	UpdateMemory(id, text string) error
	ForgetMemory(id string) error
	RestoreMemory(id string) error
	MemoryProvenance(id string) (string, string, time.Time, error)
}

type memoryStore = MemoryStore

// memoryOrigin is where and when one memory was learned — the provenance read,
// kept per id so that opening a line twice asks the store once.
type memoryOrigin struct {
	session string
	title   string
	at      time.Time
}

// memoryAddID is the edit target that means "a new line" rather than an id.
const memoryAddID = "\x00add"

// memoryPlace is the whole Memories tab: the snapshot it is drawing, which
// groups are open, the filter, and the one line a card or an editor is about.
//
// IT HOLDS A SNAPSHOT AND FILTERS THE SNAPSHOT. The whole shape is read in a
// fixed number of statements ([MemoryStore.Snapshot]) on the clock and on the
// keystroke that walks in, and typing filters what is already held.
type memoryPlace struct {
	shelves store.MemoryShelves
	limit   int
	read    time.Time
	// here is the owner this conversation keeps project lines under, which is
	// what makes one shelf "this project". hereUnknown is the engine failing to
	// say: here is then "" and no shelf is called this project.
	here        string
	hereUnknown bool
	// open is what `→` opened and `←` folded back up. closed is what `←`
	// folded while a filter was typed — a filter opens every group with a match
	// — and closedQuery is the filter it was folded under: a new filter is a
	// new question, and starts with every match open again.
	open        map[string]bool
	closed      map[string]bool
	closedQuery string
	reading     memoryReading
	filter      editor
	// cursor is a line of the reading; top and shown are the window the last
	// draw put over it, and hover the line the pointer is over (-1 for none).
	cursor     int
	top, shown int
	hover      int
	// expanded is the one line whose card is up, and origins is where each such
	// line was learned, read for that one id when its card opens.
	expanded string
	origins  map[string]memoryOrigin
	// asking is the ids whose provenance has been asked for and has not come
	// back, so a cursor resting on a line asks the store once.
	asking map[string]bool
	// edit is the wording being written, and editID the line it belongs to —
	// [memoryAddID] for a new one.
	edit   *editor
	editID string
	// footer is the receipt line: what just happened, in the place's words.
	footer string
}

func (p *memoryPlace) close() { *p = memoryPlace{} }

// start takes one snapshot and makes it the page, with the shelves a
// conversation here is told already open.
func (p *memoryPlace) start(shelves store.MemoryShelves, here string, hereUnknown bool, now time.Time) {
	*p = memoryPlace{
		shelves: shelves, limit: memorySnapshotRows, read: now, here: here, hereUnknown: hereUnknown, hover: -1,
		open: memoryOpenByDefault(shelves, here), origins: map[string]memoryOrigin{},
		asking: map[string]bool{},
	}
	p.rank()
	// THE CURSOR STARTS ON THE FIRST LINE KEPT, not on the head row, the add
	// row or a group: the first thing a person can do here is read what is
	// kept, and the pane beside the list reads it out.
	p.cursor = p.nearestStop(0)
	for i := range p.reading.lines {
		if stop, ok := p.reading.at(i); ok && stop.line != nil {
			p.cursor = i
			break
		}
	}
}

// refresh replaces the snapshot under a page that is already up, keeping the
// filter, the open shelves and — where it can — the line the cursor was on.
func (p *memoryPlace) refresh(shelves store.MemoryShelves, now time.Time) {
	was, _ := p.reading.at(p.cursor)
	p.shelves, p.read = shelves, now
	p.rank()
	p.followStop(was)
}

// rank rebuilds the reading from the snapshot and the filter.
func (p *memoryPlace) rank() {
	if p.open == nil {
		p.open = map[string]bool{}
	}
	if query := p.filter.String(); query != p.closedQuery {
		p.closed, p.closedQuery = nil, query
	}
	p.reading = readMemory(p.shelves, p.here, p.open, p.closed, p.filter.String(), p.read)
	if p.hereUnknown && p.shelves.Selection == nil {
		// WHICH LINES RIDE HERE IS WHAT THE ENGINE COULD NOT SAY, so how many do
		// not fit is unknown too — and unknown is drawn as nothing, never as a
		// count measured without this project's lines.
		p.reading.outside = 0
		for i := range p.reading.lines {
			p.reading.lines[i].selection = ""
		}
	}
	p.cursor = p.nearestStop(p.cursor)
}

// followStop puts the cursor back on the thing it was standing on rather than
// on the line number it was standing at: a list that reorders under a cursor
// has moved the cursor, and a pointer that stayed put could land somebody on a
// line they never chose, one key from forgetting it.
func (p *memoryPlace) followStop(was memoryStop) {
	for i := range p.reading.lines {
		stop, ok := p.reading.at(i)
		if !ok {
			continue
		}
		switch {
		case was.line != nil && stop.line != nil && stop.line.ID == was.line.ID:
			p.cursor = i
			return
		case was.add && stop.add:
			p.cursor = i
			return
		case was.line == nil && !was.add && was.shelf != "" && stop.line == nil && stop.shelf == was.shelf:
			p.cursor = i
			return
		}
	}
	p.cursor = p.nearestStop(p.cursor)
}

// nearestStop is the first line at or after `from` that can be stood on, and
// the last one before it when there is none.
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

// move walks the cursor by whole STOPS rather than by rows.
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

// choice is the line this place's verbs act on: the card's, when one is up,
// and otherwise the one under the cursor.
func (p *memoryPlace) choice() (store.Memory, bool) {
	if p.expanded != "" {
		return p.byID(p.expanded)
	}
	stop, ok := p.reading.at(p.cursor)
	if !ok || stop.line == nil {
		return store.Memory{}, false
	}
	return *stop.line, true
}

// shelfUnder is the group the cursor is standing ON.
func (p *memoryPlace) shelfUnder() (string, bool) {
	stop, ok := p.reading.at(p.cursor)
	if !ok || stop.line != nil || stop.add || stop.shelf == memoryLoadKey {
		return "", false
	}
	return stop.shelf, true
}

// onAdd is whether the cursor is on the add row.
func (p *memoryPlace) onAdd() bool {
	stop, ok := p.reading.at(p.cursor)
	return ok && stop.add
}

// fold opens or shuts the group under the cursor, and reports whether there
// was one to act on — `→` and `←` (placekeys.go). A group already the way it
// was asked to be answers false, so `→` on an open group falls through to the
// row's verbs, and a group has none.
//
// A CARD OR AN EDITOR STANDING OPEN IS NOT THE LIST, so neither key folds
// anything under one: `→` on a card opens the card's line's verbs.
func (p *memoryPlace) fold(open bool) bool {
	if p.expanded != "" || p.edit != nil {
		return false
	}
	key, ok := p.shelfUnder()
	if !ok {
		return false
	}
	for _, line := range p.reading.lines {
		if line.kind == memoryReadingShelf && line.shelf == key && line.open == open {
			return false
		}
	}
	was, _ := p.reading.at(p.cursor)
	if strings.TrimSpace(p.filter.String()) != "" {
		if p.closed == nil {
			p.closed = map[string]bool{}
		}
		p.closed[key] = !open
	} else {
		p.open[key] = open
	}
	p.rank()
	p.followStop(was)
	return true
}

// shelfOpen is whether the group under the cursor is open, and false over
// anything that is not a group.
func (p *memoryPlace) shelfOpen() bool {
	if line := p.cursor; line >= 0 && line < len(p.reading.lines) && p.reading.lines[line].kind == memoryReadingShelf {
		return p.reading.lines[line].open
	}
	return false
}

// startEdit opens the editor on a line's words, or on nothing for a new one.
func (p *memoryPlace) startEdit(id, text string) {
	box := editor{}
	box.setText(text)
	p.edit, p.editID = &box, id
}

func (p *memoryPlace) byID(id string) (store.Memory, bool) {
	for _, shelf := range p.shelves.Shelves {
		for _, memory := range shelf.Memories {
			if memory.ID == id {
				return memory, true
			}
		}
	}
	for _, memory := range p.shelves.LetGo {
		if memory.ID == id {
			return memory, true
		}
	}
	return store.Memory{}, false
}

// memoryWhere is where a line holds, in the words a card says it.
func (p *memoryPlace) memoryWhere(memory store.Memory) string {
	switch {
	case memory.Owner == store.OwnerUser:
		return "for you everywhere"
	case memory.Owner == store.OwnerMachine:
		return "for this machine"
	case memory.Owner == store.OwnerLegacyProject:
		return "in a project an earlier build could not name · → move it here"
	case p.here != "" && memory.Owner == p.here:
		return "for this project"
	case memory.ProjectName != "":
		return "in " + memory.ProjectName
	}
	return "in another project"
}

// card is the one line's own page: all of its words, where it holds, who kept
// it and when, and the conversation it came from. It is drawn INSTEAD of the
// shelves rather than under them, for the reason every fullscreen page here is.
func (p *memoryPlace) card(width int, pal palette) []string {
	memory, ok := p.byID(p.expanded)
	if !ok {
		return []string{pal.dim(fit("that line is not here any more", width))}
	}
	var rows []string
	for _, line := range wrapText(memory.Text, width) {
		rows = append(rows, pal.bold(fit(line, width)))
	}
	rows = append(rows, "")
	for _, fact := range p.memoryFacts(memory) {
		rows = append(rows, pal.dim(fit(fact, width)))
	}
	return rows
}

// wrapText is the card's own wrap: whole words, measured in runes.
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

// memoryFrame is this place, drawn.
func (a *app) memoryFrame(width, height int) ([]string, []int, int, int) {
	lines, hits, caretX, caretY := a.placeDraw(placeMemory{}, width, height)
	return lines, placeLineHits(hits), caretX, caretY
}

// memoryReady is whether the memory place has a store behind it.
func (a *app) memoryReady() bool { return a.memory != nil }

// memorySnapshotRows is how many memories one reading carries.
const memorySnapshotRows = 500

// openMemory walks into the memory place: one reading of the store, and the
// clock that keeps it current. The place opens whether or not there is a store
// behind it, and says once on its note when there is not.
func (a *app) openMemory() tea.Cmd {
	shelves, why := a.memorySnapshot()
	here, unknown := "", false
	if agent, ok := a.brain(); ok {
		project, err := agent.MemoryProject()
		switch {
		case err != nil:
			// "COULD NOT ASK" IS NOT "NO PROJECT": nothing the failed answer
			// carried is taken, and the note says why no shelf is this one.
			unknown = true
			if why == "" {
				why = memoryNoProjectWord
			}
		default:
			here = project
		}
	}
	a.standDownFullscreen()
	a.page = pageMemory
	a.mem.start(shelves, here, unknown, a.now())
	a.mem.footer = why
	a.touch()
	return tea.Batch(a.armPlaceClock(), a.memoryFollow())
}

// memorySnapshot is what the store holds, and — when there is no store — the
// one dim sentence saying so.
func (a *app) memorySnapshot() (store.MemoryShelves, string) {
	if !a.memoryReady() {
		return store.MemoryShelves{}, memoryUnavailableNote
	}
	shelves, err := a.memory.Snapshot(memorySnapshotRows)
	if err != nil {
		return store.MemoryShelves{}, memoryUnreadableWord
	}
	a.attachMemorySelection(&shelves)
	return shelves, ""
}

// refreshMemory is the place clock's beat on this page. A beat that cannot
// read keeps what it had: the page was true three seconds ago.
func (a *app) refreshMemory() {
	if !a.at(pageMemory) || !a.memoryReady() {
		return
	}
	shelves, err := a.memory.Snapshot(max(memorySnapshotRows, a.mem.limit))
	if err != nil {
		return
	}
	a.attachMemorySelection(&shelves)
	a.mem.refresh(shelves, a.now())
}

// memoryPlaceDoneMsg is one write the place started, finished: its receipt for
// the note line. The page re-reads the store when it lands rather than patching
// the held snapshot, because the store owns the counts and the shelves.
type memoryPlaceDoneMsg struct {
	text    string
	shelves *store.MemoryShelves
}

// memoryPlaceDo runs one write OFF the update loop — over --host it is a wire
// call — and answers its receipt as a message, so a slow engine never freezes
// the keyboard.
func (a *app) memoryPlaceDo(write func() string) tea.Cmd {
	return func() tea.Msg { return memoryPlaceDoneMsg{text: write()} }
}

// adoptMemoryPlaceDone lands one write's receipt and redraws from the store.
func (a *app) adoptMemoryPlaceDone(msg memoryPlaceDoneMsg) {
	if !a.at(pageMemory) {
		return
	}
	a.mem.footer = msg.text
	if msg.shelves != nil {
		a.attachMemorySelection(msg.shelves)
		a.mem.refresh(*msg.shelves, a.now())
	} else {
		a.refreshMemory()
	}
	a.touch()
}

func (a *app) memoryKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.mem
	if p.edit != nil {
		switch msg.String() {
		case "esc":
			p.edit, p.editID = nil, ""
		case "enter":
			text, id := strings.TrimSpace(p.edit.String()), p.editID
			p.edit, p.editID = nil, ""
			if text == "" {
				p.footer = "nothing was kept · a line needs words"
				a.touch()
				return nil
			}
			a.touch()
			return a.memorySave(id, text)
		default:
			listNavigate(msg, p.edit, func(int) {}, func() {}, memoryPlaceRows)
		}
		a.touch()
		return nil
	}
	if p.expanded != "" {
		if msg.String() == "esc" {
			p.expanded = ""
		}
		a.touch()
		return nil
	}
	switch msg.String() {
	case "esc":
		// ONE LAYER AT A TIME: a filter with something in it is cleared first,
		// and the second esc leaves.
		if !p.filter.empty() {
			p.filter.reset()
			p.rank()
			a.touch()
			return nil
		}
		a.leavePlace()
		return nil
	case "enter":
		cmd := placeMemory{}.enter(a)
		a.touch()
		return cmd
	case "delete", "ctrl+d":
		if memory, ok := p.choice(); ok && memory.Status == store.MemoryActive {
			a.touch()
			return a.memoryForgetRow(memory)
		}
	default:
		listNavigate(msg, &p.filter, p.move, p.rank, memoryPlaceRows)
	}
	a.touch()
	return nil
}

// memorySave writes what the editor held: a new line through the conversation
// (which knows where a line kept here holds), or a fixed wording through the
// store.
func (a *app) memorySave(id, text string) tea.Cmd {
	if id == memoryAddID {
		agent, ok := a.brain()
		if !ok {
			a.mem.footer = memoryUnavailableNote
			return nil
		}
		return a.memoryPlaceDo(func() string {
			receipt, err := agent.Remember(text)
			if err != nil {
				return "could not keep that · " + err.Error()
			}
			return receipt
		})
	}
	if a.memory == nil {
		return nil
	}
	mem, here := a.memory, a.mem.where()
	return a.memoryPlaceDo(func() string {
		if err := mem.UpdateMemory(id, text); err != nil {
			return memoryWriteRefusal(err, here, text)
		}
		return "fixed · " + text
	})
}

// memoryWriteRefusal is the receipt for a write of text the store would not
// take. The two refusals the person can act on are said: a full block, in the
// engine's own words and from this conversation ([session.MemoryFullWords]:
// here, which project, or another one), and words that are already kept
// ([store.ErrMemoryKept]). Anything else is [memoryUnchangedWord].
//
// THE MATCH IS ON THE WORDS AS WELL AS THE SENTINEL, because over --host the
// store's error crosses the wire as text and is a different value at this end.
//
// It runs off the update loop, inside the write's command, so where "here" is
// was read on the loop beforehand ([memoryPlace.where]).
func memoryWriteRefusal(err error, here memoryWhere, text string) string {
	switch {
	case errors.Is(err, store.ErrMemoryFull) || strings.Contains(err.Error(), store.ErrMemoryFull.Error()):
		return session.MemoryFullWords(err, here.owner, here.name)
	case errors.Is(err, store.ErrMemoryKept) || strings.Contains(err.Error(), store.ErrMemoryKept.Error()):
		return "already kept · " + text + " · nothing was changed"
	}
	return memoryUnchangedWord
}

// memoryWhere is this conversation's project as the page knows it: the owner
// its lines are kept under and the name that shelf carries.
type memoryWhere struct{ owner, name string }

// where reads [memoryWhere] off the page, on the loop.
func (p *memoryPlace) where() memoryWhere {
	here := memoryWhere{owner: p.here}
	for _, shelf := range p.shelves.Shelves {
		if p.here != "" && shelf.Owner == p.here {
			here.name = shelf.ProjectName
		}
	}
	return here
}

// memoryForgetRow forgets one line from the page.
func (a *app) memoryForgetRow(memory store.Memory) tea.Cmd {
	if a.memory == nil {
		return nil
	}
	mem := a.memory
	a.mem.expanded = ""
	return a.memoryPlaceDo(func() string {
		if err := mem.ForgetMemory(memory.ID); err != nil {
			return memoryUnchangedWord
		}
		return "forgot · " + memory.Text + " · it is under let go, where → then u puts it back"
	})
}

// ── the place ───────────────────────────────────────────────────────────────

// placeMemory is this place's handle on the registry.
type placeMemory struct{ placeBase }

func init() { registerPlace(placeMemory{}) }

func (placeMemory) id() page        { return pageMemory }
func (placeMemory) word() string    { return "memories" }
func (placeMemory) navWord() string { return "Memory" }
func (placeMemory) counted() bool   { return true }

func (placeMemory) open(a *app) tea.Cmd {
	cmd := a.openMemory()
	if a.kb.open {
		return tea.Batch(cmd, a.knowledgeRead(false))
	}
	return cmd
}

func (placeMemory) close(a *app) {
	a.leavePage(pageMemory)
	a.mem.close()
}

func (placeMemory) tick(a *app, now time.Time) (bool, tea.Cmd) {
	a.refreshMemory()
	if a.kb.open {
		return true, a.knowledgeRead(false)
	}
	return true, nil
}

// body is the groups and their lines — with the cursor row's pane beside them
// on a frame wide enough for one — or the one line whose card is open.
func (placeMemory) body(a *app, width, room int) []placeRow {
	if a.kb.open {
		return a.knowledgeBody(width, room)
	}
	p := &a.mem
	if p.expanded != "" {
		p.top = 0
		rows := make([]placeRow, 0, room)
		for _, line := range p.card(width-len(placeLead), a.pal) {
			if len(rows) < room {
				rows = append(rows, placeRow{text: placeLead + line, hit: -1})
			}
		}
		p.shown = len(rows)
		for len(rows) < room {
			rows = append(rows, placeRow{text: "", hit: -1})
		}
		return rows
	}
	if p.reading.bare() {
		// THE EMPTY PAGE IS ITS HEADING AND ITS WHISPER, which names the door.
		p.shown = 0
		return placeWhisperRows(pageMemory, width, room, a.pal)
	}
	// A WIDE FRAME SPLITS THE WAY THE ACTIVITY TAB'S DOES (taskpane.go): the
	// list keeps the left at the width it is laid out at, the seam divides it,
	// and the right is the cursor row read out — its whole words, where it
	// holds, who kept it and where it was learned — following the cursor.
	// [taskPaneCols] is the one function that decides whether there is a pane,
	// so the two tabs split at the same width.
	left := taskPaneList(width)
	list := a.memoryListRows(left, room)
	if left == width {
		return list
	}
	head := 0
	if p.top == 0 && len(list) > 0 {
		head = 1
	}
	pane := a.memoryPaneRows(width-left-ansi.StringWidth(taskPaneSeam), room-head)
	seam := a.pal.dim(taskPaneSeam)
	out := make([]placeRow, 0, len(list))
	next := 0
	for at, row := range list {
		if head == 1 && at == 0 {
			// THE HEAD ROW IS ABOUT THE TAB AND KEEPS THE WHOLE FRAME; the seam
			// starts under it, as it does on the Activity tab.
			out = append(out, placeRow{text: placeLead + placeHeading(fit(p.reading.counts(), width-len(placeLead)), a.pal), hit: row.hit})
			continue
		}
		text := ""
		if next < len(pane) {
			text = pane[next]
		}
		next++
		out = append(out, placeRow{text: taskPanePad(row.text, left) + seam + text, hit: row.hit})
	}
	return out
}

// memoryListRows is the list half: the reading painted at the list's width,
// windowed over the cursor, the cursor row banded.
func (a *app) memoryListRows(width, room int) []placeRow {
	p := &a.mem
	rows := make([]placeRow, 0, room)
	body := p.reading.paint(width, a.pal, memoryPaint{
		lit:    func(i int) bool { return i == p.cursor },
		editID: p.editID, edit: p.edit,
	})
	p.top = placeTop(p.top, p.cursor, len(body), room)
	for i := p.top; i < len(body); i++ {
		if len(rows) >= room {
			break
		}
		text := body[i]
		if _, stop := p.reading.at(i); stop && i == p.cursor {
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

// caretRow parks the terminal's caret in the box a person is typing into when
// that box is one of the rows just built: the filter row, the new line being
// written on the add row, or a line being reworded in its own row.
func (placeMemory) caretRow(a *app, width int, rows []placeRow) (int, int, bool) {
	p := &a.mem
	if p.expanded != "" || p.reading.bare() {
		return 0, 0, false
	}
	listWidth := taskPaneList(width)
	want, column := -1, 0
	for i, line := range p.reading.lines {
		switch {
		case p.edit == nil && line.kind == memoryReadingControl:
			_, column = memoryControlRow(p.reading.typed, listWidth, a.pal)
			want = i
		case p.edit != nil && p.editID == memoryAddID && line.kind == memoryReadingAdd:
			_, column = memoryAddRow(listWidth, a.pal, true, p.edit)
			want = i
		case p.edit != nil && line.kind == memoryReadingMemory && line.memory != nil && line.memory.ID == p.editID:
			_, column = memoryLineRow(line, listWidth, a.pal, true, p.edit)
			want = i
		}
		if want >= 0 {
			break
		}
	}
	if want < 0 {
		return 0, 0, false
	}
	for j, row := range rows {
		if hit, ok := row.hit.(int); ok && hit == want {
			return j, column, true
		}
	}
	// The window scrolled the box off this frame; the caret is hidden rather
	// than parked in a box that is not the one the words are landing in.
	return -1, 0, true
}

// memoryPaneRows is the pane: the row under the cursor read out, in exactly the
// rows it was given.
func (a *app) memoryPaneRows(width, room int) []string {
	if room < 1 {
		return nil
	}
	p, pal := &a.mem, a.pal
	var rows []string
	add := func(text string) { rows = append(rows, fit(text, width)) }
	gap := func() {
		if len(rows) > 0 && rows[len(rows)-1] != "" {
			rows = append(rows, "")
		}
	}
	stop, ok := p.reading.at(p.cursor)
	switch {
	case width < 1 || !ok:
	case stop.add:
		add(pal.bold(pal.ink(memoryAddWord)))
		gap()
		for _, line := range wrapText("one line codeaf keeps in front of every conversation "+p.addReach(), width) {
			add(pal.dim(line))
		}
		gap()
		add(pal.dim("enter starts it"))
	case stop.line != nil:
		memory := *stop.line
		for _, line := range wrapText(memory.Text, width) {
			add(pal.bold(pal.ink(line)))
		}
		gap()
		for _, fact := range p.memoryFacts(memory) {
			add(pal.dim(fact))
		}
		gap()
		if memory.Status != store.MemoryActive {
			add(pal.dim("→ " + memoryUndoWord))
		} else {
			add(pal.dim("→ edit, forget, move"))
		}
	default:
		for _, line := range p.reading.lines {
			if line.kind != memoryReadingShelf || line.shelf != stop.shelf {
				continue
			}
			add(placeHeading(line.label, pal))
			gap()
			for _, said := range wrapText(memoryShelfAbout(stop.shelf, p.here), width) {
				add(pal.dim(said))
			}
			add(pal.dim(memoryLinesWord(line.count)))
			gap()
			if line.open {
				add(pal.dim(tasksShutWord))
			} else {
				add(pal.dim(memoryShelfOpenWord))
			}
			break
		}
	}
	if len(rows) > room {
		rows = rows[:room]
	}
	for len(rows) < room {
		rows = append(rows, "")
	}
	return rows
}

// memoryFacts is what the pane and the card say about one line beyond its
// words: where it holds, who kept it or what happened to it, and when and
// where it was learned when the store has said.
func (p *memoryPlace) memoryFacts(memory store.Memory) []string {
	about := []string{p.memoryWhere(memory)}
	if word := memoryStatusWord(memory.Status); word != "" {
		about = append(about, word)
	}
	if word := memoryOriginWord(memory.Origin); word != "" {
		about = append(about, "kept by "+word)
	}
	facts := []string{strings.Join(about, " · ")}
	kind := memory.SemanticType
	if kind == "" {
		kind = memory.Type
	}
	if kind != "" {
		label := "kind · " + memoryKindWords(kind)
		if memory.SemanticType != "" && memory.Type != "" && memory.SemanticType != memory.Type {
			label += " · originally saved as " + memoryKindWords(memory.Type)
		}
		facts = append(facts, label)
	}
	if !p.hereUnknown || p.shelves.Selection != nil {
		prefix := ""
		if p.shelves.Selection == nil {
			prefix = "project preview · "
		}
		facts = append(facts, prefix+memorySelectionWords(memory, p.reading.decisions))
	}
	// WHERE IT WAS LEARNED IS SAID ONLY WHEN THE STORE SAID. An origin nobody
	// could name is absent rather than "somewhere", and one still on its way is
	// absent until it lands.
	if origin, held := p.origins[memory.ID]; held {
		learned := ""
		if age := sinceAt(origin.at, p.read); age != "" {
			learned = age + " ago"
		}
		if origin.title != "" {
			if learned != "" {
				learned += " · "
			}
			learned += "in '" + origin.title + "'"
		}
		if learned != "" {
			facts = append(facts, learned)
		}
	}
	return facts
}

// addReach is where a line kept from the add row holds, in the pane's words.
func (p *memoryPlace) addReach() string {
	if p.here == "" {
		return "here, for you in every project"
	}
	return "in this project"
}

// memoryShelfAbout is what a group means, in the pane's words: which
// conversations its lines are put in front of.
func memoryShelfAbout(key, here string) string {
	switch {
	case key == store.OwnerUser:
		return "available in every conversation, in every project"
	case key == store.OwnerMachine:
		return "available in every conversation on this machine"
	case key == memoryLetGoKey:
		return "forgotten or replaced, and in front of nothing · → on a line puts it back"
	case key == store.OwnerLegacyProject:
		return "kept by an earlier build that could not tell which project · → on a line moves it"
	case here != "" && key == here:
		return "available in every conversation in this project"
	}
	return "in front of conversations in that project, and not here"
}

// memoryLinesWord is a group's count in words.
func memoryLinesWord(n int) string {
	if n == 1 {
		return "1 line"
	}
	return groupedInt(n) + " lines"
}

// memoryOriginMsg is one line's provenance, read off the loop.
type memoryOriginMsg struct {
	id     string
	origin memoryOrigin
	ok     bool
}

// memoryFollow asks the store where the line under the cursor was learned,
// once per line, OFF the update loop — over --host it is a wire call — so the
// pane can say it when it lands. Until then that one fact is absent, which is
// the emptiness law applied to a fact that is merely late.
func (a *app) memoryFollow() tea.Cmd {
	p := &a.mem
	if a.memory == nil || !a.at(pageMemory) {
		return nil
	}
	memory, ok := p.choice()
	if !ok {
		return nil
	}
	if _, held := p.origins[memory.ID]; held || p.asking[memory.ID] {
		return nil
	}
	if p.asking == nil {
		p.asking = map[string]bool{}
	}
	p.asking[memory.ID] = true
	mem, id := a.memory, memory.ID
	return func() tea.Msg {
		where, title, at, err := mem.MemoryProvenance(id)
		return memoryOriginMsg{id: id, origin: memoryOrigin{session: where, title: title, at: at}, ok: err == nil}
	}
}

// adoptMemoryOrigin lands one provenance read. A read that failed is not
// retried on this visit: the pane is complete without it.
func (a *app) adoptMemoryOrigin(msg memoryOriginMsg) {
	p := &a.mem
	delete(p.asking, msg.id)
	if !msg.ok || p.origins == nil {
		return
	}
	p.origins[msg.id] = msg.origin
	a.touch()
}

func (placeMemory) stops(a *app) []int {
	if a.kb.open {
		out := make([]int, len(a.kb.targets))
		for i := range out {
			out[i] = i
		}
		return out
	}
	return a.mem.stops()
}

func (placeMemory) cursorAt(a *app) int {
	if a.kb.open {
		return a.kb.cursor
	}
	return a.mem.cursor
}
func (placeMemory) cursorRow(a *app, rows []placeRow) int {
	return placeRowAtLine(rows, a.mem.cursor)
}

// rowID is the line, the shelf or the add row under the cursor.
func (placeMemory) rowID(a *app) string {
	if a.kb.open {
		return "knowledge\x00" + a.kb.selected
	}
	p := &a.mem
	if p.onAdd() {
		return "add"
	}
	if key, ok := p.shelfUnder(); ok {
		return "shelf\x00" + key
	}
	if memory, ok := p.choice(); ok {
		return "line\x00" + memory.ID
	}
	return ""
}

// enter opens the editor on the add row and a line's card — the whole view of
// one line. IT DOES NOTHING ON A GROUP: groups open on `→` and fold on `←`,
// and the foot over a group names those and not `enter`.
func (placeMemory) enter(a *app) tea.Cmd {
	p := &a.mem
	if stop, ok := p.reading.at(p.cursor); ok && stop.shelf == memoryLoadKey {
		if a.memory == nil {
			return nil
		}
		p.limit = max(memorySnapshotRows, p.limit) + memorySnapshotRows
		limit, memory := p.limit, a.memory
		return func() tea.Msg {
			shelves, err := memory.Snapshot(limit)
			if err != nil {
				return memoryPlaceDoneMsg{text: memoryUnreadableWord}
			}
			return memoryPlaceDoneMsg{text: "more memories loaded", shelves: &shelves}
		}
	}
	if p.onAdd() {
		p.startEdit(memoryAddID, "")
		return nil
	}
	if memory, ok := p.choice(); ok {
		p.openMemoryCard(a, memory)
	}
	return nil
}

// fold is `→` and `←` on a group (placekeys.go's [app.placeFold]).
func (placeMemory) fold(a *app, open bool) bool {
	if !a.mem.fold(open) {
		return false
	}
	a.touch()
	return true
}

// openMemoryCard is one line's own page: its provenance is read for THAT ONE id,
// on the keystroke that asked for it.
func (p *memoryPlace) openMemoryCard(a *app, memory store.Memory) {
	p.expanded = memory.ID
	if _, held := p.origins[memory.ID]; !held && a.memory != nil {
		if where, title, at, err := a.memory.MemoryProvenance(memory.ID); err == nil {
			p.origins[memory.ID] = memoryOrigin{session: where, title: title, at: at}
		}
	}
}

// memorySourceRow is the conversation a line came from, when this window knows
// it — so its card can offer to open it.
func (a *app) memorySourceRow(memory store.Memory) (string, bool) {
	origin, held := a.mem.origins[memory.ID]
	if !held || origin.session == "" {
		return "", false
	}
	for _, row := range a.home.world.Sessions() {
		if row.ID == origin.session && row.Transcript != "" {
			return row.ID, true
		}
	}
	return "", false
}

// verbs is this place's `→` strip: what can be done to the line under the
// cursor, or to the one whose card is open. Every letter on it is a verb only
// while the strip is drawn, so the filter keeps every letter of the alphabet.
func (placeMemory) verbs(a *app) []verb {
	if a.kb.open {
		return nil
	}
	p := &a.mem
	memory, ok := p.choice()
	if !ok {
		return nil
	}
	if memory.Status != store.MemoryActive {
		return []verb{{key: 'u', word: memoryUndoWord, do: func() tea.Cmd {
			if a.memory == nil {
				return nil
			}
			mem, here := a.memory, p.where()
			p.expanded = ""
			return a.memoryPlaceDo(func() string {
				if err := mem.RestoreMemory(memory.ID); err != nil {
					return memoryWriteRefusal(err, here, memory.Text)
				}
				return "put back · " + memory.Text
			})
		}}}
	}
	verbs := []verb{
		{key: 'e', word: memoryFixWord, do: func() tea.Cmd {
			p.startEdit(memory.ID, memory.Text)
			return nil
		}},
		{key: 'f', word: memoryForgetWord, do: func() tea.Cmd { return a.memoryForgetRow(memory) }},
	}
	// THE MOVES ARE OFFERED TO WHERE THE LINE IS NOT, and "this project" only
	// where this conversation has one.
	move := func(key rune, word, scope string) verb {
		return verb{key: key, word: word, do: func() tea.Cmd {
			agent, ok := a.brain()
			if !ok {
				p.footer = memoryUnavailableNote
				return nil
			}
			p.expanded = ""
			here := p.where()
			return a.memoryPlaceDo(func() string {
				receipt, err := agent.MoveMemory(memory.ID, scope)
				if err != nil {
					return memoryWriteRefusal(err, here, memory.Text)
				}
				return memory.Text + " · " + receipt
			})
		}}
	}
	if memory.Owner != store.OwnerUser {
		verbs = append(verbs, move('y', memoryMoveYouWord, store.MemoryScopeUser))
	}
	if p.here != "" && memory.Owner != p.here {
		verbs = append(verbs, move('p', memoryMoveHereWord, store.MemoryScopeProject))
	}
	if memory.Owner != store.OwnerMachine {
		verbs = append(verbs, move('m', memoryMoveMachineWord, store.MemoryScopeEnv))
	}
	verbs = append(verbs, verb{key: 'a', word: memoryAskWord, do: func() tea.Cmd {
		cmd, _ := a.placeTalkAbout(memoryAskOpening + strings.TrimSpace(memory.Text))
		return cmd
	}})
	if id, known := a.memorySourceRow(memory); known && p.expanded != "" {
		verbs = append(verbs, verb{key: 'o', word: memorySourceWord, do: func() tea.Cmd {
			for _, row := range a.home.world.Sessions() {
				if row.ID == id {
					return a.openConversationRow(row)
				}
			}
			return nil
		}})
	}
	return verbs
}

// alt answers no view key: the groups open on `→` and fold on `←`.
func (placeMemory) alt(a *app, letter rune) bool { return false }

// box is the filter, or the words being written while the editor is open.
func (placeMemory) box(a *app) *editor {
	if a.kb.open {
		return &a.kb.filter
	}
	if a.mem.edit != nil {
		return a.mem.edit
	}
	return &a.mem.filter
}

// note is the receipt line.
func (placeMemory) note(a *app, width int) []string {
	if a.kb.open {
		return []string{" " + a.pal.dim(fit(a.kb.footer, width-2))}
	}
	if a.mem.footer == "" {
		return []string{" " + a.pal.dim(fit(a.chords.say("Saved memory · alt+u Knowledge"), width-2))}
	}
	return []string{" " + a.pal.dim(fit(a.chords.say("alt+u Knowledge · "+a.mem.footer), width-2))}
}

func (placeMemory) about() string { return "what codeaf remembers" }

// hint is WHAT THE ROW UNDER THE CURSOR CAN BE ASKED FOR.
func (placeMemory) hint(a *app) string {
	if a.kb.open {
		return "↑↓ select · enter open · ctrl+l links · pgup/pgdn read · esc back · alt+u saved memory"
	}
	p := &a.mem
	switch {
	case p.edit != nil && p.editID == memoryAddID:
		return "new line · " + memoryEditHint
	case p.edit != nil:
		return "edit · " + memoryEditHint
	case p.expanded != "":
		if memory, ok := p.choice(); ok && memory.Status != store.MemoryActive {
			return "→ " + memoryUndoWord + " · esc back to the list"
		}
		return memoryCardHint
	case p.reading.bare():
		return memoryBareHint
	case p.onAdd():
		return memoryAddHint
	case p.cursor >= 0 && p.cursor < len(p.reading.lines) && p.reading.lines[p.cursor].kind == memoryReadingLoad:
		return "enter " + memoryLoadWord
	}
	if _, ok := p.shelfUnder(); ok {
		if p.shelfOpen() {
			return memoryShelfShutHint
		}
		return memoryShelfOpenHint
	}
	if memory, ok := p.choice(); ok {
		if memory.Status != store.MemoryActive {
			return memoryGoneHint
		}
		return memoryLineHint
	}
	if strings.TrimSpace(p.filter.String()) != "" {
		return memoryNoMatchHint
	}
	return memoryAddHint
}

func (placeMemory) changed(a *app, since time.Time) int { return a.memoryChangedSince(since) }

// press is enter on the row it lands on, and on a group it is the fold: a
// pointer has no arrows, and a press on a row that opens is how it opens. A
// card or an editor standing open is not the list, so a press over it lands on
// no row of it.
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
	if !stop.add && stop.line == nil && stop.shelf != memoryLoadKey {
		p.fold(!p.shelfOpen())
		return a.memoryFollow(), true
	}
	return tea.Batch(placeMemory{}.enter(a), a.memoryFollow()), true
}

func (placeMemory) hover(a *app, y int) bool {
	if a.kb.open {
		return false
	}
	next := -1
	if at, ok := placeBodyLine(y, a.mem.top, a.mem.shown); ok && a.mem.expanded == "" {
		if _, stop := a.mem.reading.at(at); stop {
			next = at
		}
	}
	return placeHoverMoved(&a.mem.hover, &a.mem.cursor, next, next, a)
}

func (placeMemory) wheel(a *app, delta int) (tea.Cmd, bool) {
	if a.kb.open {
		p := &a.kb
		switch {
		case p.links:
			p.linkCursor = max(0, min(p.linkCursor+delta, len(p.pageLinks())-1))
		case p.article:
			p.scroll = max(0, p.scroll+delta)
		default:
			p.cursor = max(0, min(p.cursor+delta, len(p.targets)-1))
		}
		a.touch()
		return nil, true
	}
	a.mem.move(delta)
	a.touch()
	return a.memoryFollow(), true
}

// key is this place's own reading of a key the router did not take. Every key
// may have moved the cursor onto a line whose provenance the pane has not
// read yet, so each one asks ([app.memoryFollow]).
func (placeMemory) key(a *app, msg tea.KeyPressMsg) tea.Cmd {
	if a.kb.open {
		return nil
	}
	return tea.Batch(a.memoryKey(msg), a.memoryFollow())
}

// owns is the editor: a whole-keyboard surface inside [placeMemory.key], so the
// place router never walks the place out from under somebody mid-sentence.
func (placeMemory) owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if cmd, took := a.knowledgeOwns(msg); took {
		return cmd, true
	}
	if a.mem.edit == nil {
		return nil, false
	}
	return a.memoryKey(msg), true
}

// attachMemorySelection asks the conversation rather than guessing from the
// snapshot: hosted rows and their admission must come from the same engine.
func (a *app) attachMemorySelection(shelves *store.MemoryShelves) {
	if reader, ok := a.agent.(interface {
		MemorySelection() (store.MemoryBlock, error)
	}); ok {
		if block, err := reader.MemorySelection(); err == nil {
			shelves.Selection = &block
		}
	}
}
