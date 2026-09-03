package tui3

// THE TASKS PLACE IS ONE READING, AND EVERYTHING ON THE FRAME IS DRAWN FROM IT.
//
// The place answers "what has this machine run" — every project, every
// conversation, this window's own work included. Four authorities know part of
// that answer and no single one of them knows all of it:
//
//   - THE WORLD SCAN knows every project's index file, and it has already
//     settled whether a live-looking row is really still running.
//   - THIS PROJECT'S INDEX, held in memory and merged with this session's live
//     graph, is fresher than the file for the project the window is sitting in.
//   - THIS SESSION'S GRAPH is the only authority for work this window started
//     and has not landed: an ordinary task writes NO row into the project's
//     index until it finishes (internal/session's taskelsewhere.go says why).
//   - THE OTHER WINDOWS on this project are the only authority for THEIR
//     unlanded work, for the same reason.
//
// So the reading takes all four and DEDUPLICATES ON (SessionID, ID) — the pair
// internal/session states is what identifies one row — with the freshest
// authority winning. One piece of work is drawn once, however many of the four
// know about it.
//
// Everything below the reading is pure: it never touches disk and never reads a
// clock. The snapshot is taken when the place opens (and re-grouped, from the
// CACHED world, on a time-window key), so a resize, a cursor move or a frame can
// never walk a directory or find a different `now` halfway through one screen.

import (
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

type tasksSection int

const (
	tasksNeeds tasksSection = iota
	tasksRunning
	// tasksParked is work that has been admitted and that NOTHING IS DOING: a
	// node waiting behind the piece that needs a person, or behind a slot.
	//
	// IT USED TO BE FILED UNDER `running` and counted with it, because the one
	// liveness answer this place carries ([tasksItem.runs]) is true for a queued
	// node as well as a working one. So a machine with nothing executing on it
	// drew two rows under `running` and a foot that said `2 running`, and a
	// developer read that as two workers burning tokens somewhere and went
	// looking for the window they were in. The column one keypress away called
	// the same two nodes `2 parked` the whole time — two surfaces, one fact, two
	// words — and this is the word both of them say now ([railGroupWords]).
	tasksParked
	tasksToday
	tasksEarlier
	tasksSectionCount
)

// tasksSectionOrder is the one order the page is drawn in and counted in, and
// there is exactly one of it: a heading list and a tally list that were spelled
// out separately are two places a new section can be forgotten, and the foot
// would then count a page it does not describe.
var tasksSectionOrder = [...]tasksSection{tasksNeeds, tasksRunning, tasksParked, tasksToday, tasksEarlier}

// tasksItem is one piece of work as this place files it: the row itself, the
// conversation it came out of, and the two judgements the reading is not allowed
// to make twice — which section it belongs under, and whether it is happening.
type tasksItem struct {
	entry session.TaskIndexEntry
	// row is the conversation the work came out of, and it is a LABEL: it says
	// where to file the row on screen and it is never asked whether the work is
	// running. That answer travels on [tasksItem.runs] instead, from whichever
	// authority produced this row.
	row     session.SessionRow
	section tasksSection
	// runs is the ONE liveness answer for this row. It is decided once, by the
	// authority the row came from, and never recomputed — a screen that asked
	// twice is a screen that can say `running` on a row it filed under `earlier`.
	runs bool
	// away marks one piece of work ANOTHER window on this project has out at
	// this instant. It has no row in any index file — nothing lands one until
	// the work finishes — so the other window's own presence is the only place
	// it can be read from, and window is what that window is CALLED.
	away   bool
	window string
}

// pick reports whether the CURSOR may stand on this row.
//
// ANOTHER WINDOW'S WORK IS READ AND NOT PRESSED. The two doors this surface has
// onto a piece of work are a ROOM, which is a live lane onto a node in THIS
// session's graph, and the record CARD, which is minted out of a landed row.
// Work running in another window has neither — no node here to open, and nothing
// landed to point at — so the cursor steps over it rather than promising a door
// that does not exist ([app.taskSheetAwayRows] states the same law).
func (i tasksItem) pick() bool { return !i.away }

// tasksMine is what the window drawing this page knows that no file does yet.
//
// IT IS PLAIN DATA AND NOT A DOOR BACK TO THE SURFACE. Every row arrives with
// its liveness already settled by the one ladder that owns that judgement
// ([app.recordRuns], asked once in place_tasks.go), because a reading that could
// ask the app a question would be a reading that could ask it at paint time.
type tasksMine struct {
	// row is this conversation, as a row of the world — what a row of this
	// project's index is labelled with when the world scan has never seen the
	// conversation that ran it.
	row session.SessionRow
	// rows is this project's whole index with this session's live graph merged
	// over the top, each already judged.
	rows []tasksMineRow
	// away is what the other windows on this project have out right now.
	away []session.ElsewhereTask
}

// tasksMineRow is one of those rows: the work, and whether it is happening.
type tasksMineRow struct {
	entry session.TaskIndexEntry
	runs  bool
}

// tasksReading is everything drawing and routing need from one world reading.
//
// seen is deliberately retained even though state grouping does not use it: it
// is the look stamp paired with this snapshot, and a later place adapter must
// not need to reach back to disk to preserve that boundary.
type tasksReading struct {
	items []tasksItem
	// held is how much work the machine has run IN ANY WINDOW, and it is what
	// tells the two empty pages apart.
	//
	// A LIST EMPTIED BY THE WINDOW IS NOT AN EMPTY PLACE. Both draw no rows, and
	// the right answer to each is the opposite of the other's: a machine that has
	// run nothing wants the whole frame spent saying what tasks ARE
	// ([tasksTeach]), while a window paged back past the oldest task wants the
	// count line — which is the only thing on the frame naming the window the
	// four shift-arrows are moving. Teaching in the second case swallowed the way
	// back, so `shift+←` on a real machine looked like the page had been wiped.
	held int
	// whole is how much work THE WINDOW holds and what that came to, counted
	// before any query narrowed the page.
	//
	// THE HEAD SENTENCE IS A CLAIM ABOUT THE PLACE AND THE FILTER IS A PROPERTY
	// OF THE QUERY. Counting the sentence off the rows that survived a filter
	// told a person who typed a word they half-remembered that their history was
	// empty — `work aforge ran on its own. nothing.` across the top of a machine
	// that had run ten pieces of work. The news that nothing matches already has
	// its own home on the note line ([taskSheetFilterLine]).
	whole     int
	wholeCost float64
	win       session.UsageWindow
	seen      time.Time
	now       time.Time
	// open is which families are unfolded, and it is the PLACE'S state handed in
	// rather than the reading's own: a snapshot is replaced whole every time a
	// node lands (place_tasks.go), and a fold that lived here would shut itself
	// every time the page reloaded under somebody reading it.
	open map[tasksKey]bool
}

// tasksKey is what identifies ONE piece of work across every authority: the
// conversation that ran it and the id inside that conversation. internal/session
// states the pair on [session.TaskIndexEntry.ID] — ids restart with every
// conversation, so an id alone names a different task in every one of them.
type tasksKey struct{ session, id string }

func tasksKeyOf(entry session.TaskIndexEntry) tasksKey {
	return tasksKey{session: strings.TrimSpace(entry.SessionID), id: strings.TrimSpace(entry.ID)}
}

// readTasks walks every authority once and files what it finds under the
// question a person acts on next.
//
// THE AUTHORITIES ARE APPLIED WEAKEST FIRST and each overwrites what the one
// before it said about the same work, because that is what "freshest wins"
// means in one pass: the file, then this project's in-memory index and live
// graph, then the other windows — which are reading a presence file written
// seconds ago and are the only authority for work that has not landed.
func readTasks(world session.World, mine tasksMine, win session.UsageWindow, seen, now time.Time) tasksReading {
	r := tasksReading{win: win.Normalized(), seen: seen, now: now}
	// order keeps the pass stable: a map alone would re-order the page on every
	// frame it was rebuilt, and the sections below are drawn in the order the
	// rows arrived within each one.
	var order []tasksKey
	held := map[tasksKey]tasksItem{}
	put := func(key tasksKey, item tasksItem) {
		if _, seen := held[key]; !seen {
			order = append(order, key)
		}
		held[key] = item
	}

	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			for _, entry := range row.Tasks.Rows {
				// The world scan has already judged this row against the
				// conversation that wrote it (world.go's first law), so its
				// answer travels with it.
				put(tasksKeyOf(entry), tasksItem{entry: entry, row: row, runs: row.Runs(entry)})
			}
		}
	}
	for _, row := range mine.rows {
		put(tasksKeyOf(row.entry), tasksItem{
			entry: row.entry, row: tasksRowFor(world, mine, row.entry), runs: row.runs,
		})
	}
	for _, task := range mine.away {
		// A TASK NOTHING NAMED IS LEFT OFF. A row with no words on it says
		// nothing a person can act on, which is the same refusal
		// [session.recordTaskIndexEntry] makes about writing one.
		title := strings.TrimSpace(task.Task.Title)
		if title == "" {
			continue
		}
		entry := session.TaskIndexEntry{
			ID: task.Task.ID, Label: title, Title: title,
			Status: task.Task.State, SessionID: task.SessionID,
		}
		put(tasksKeyOf(entry), tasksItem{entry: entry, runs: true, away: true, window: task.Session})
	}

	// A FAMILY STANDS TOGETHER under its most urgent member's section. Splitting
	// a refused child away from its still-running parent makes one piece of work
	// look like two unrelated tasks and hides the reason under the wrong row.
	// The roster already applies this same family judgement (task.go's
	// railForest); the project record keeps the tree intact here too.
	visible := make([]tasksItem, 0, len(order))
	r.held = len(order)
	for _, key := range order {
		item := held[key]
		if !r.win.Holds(tasksEntryAt(item.entry, now)) {
			continue
		}
		item.section = tasksSectionOf(item, now)
		visible = append(visible, item)
	}
	visibleRoots := make(map[tasksKey]bool, len(visible))
	for _, item := range visible {
		if strings.TrimSpace(item.entry.Parent) == "" {
			visibleRoots[tasksFamilyOf(item.entry)] = true
		}
	}
	familySection := make(map[tasksKey]tasksSection, len(visibleRoots))
	for _, item := range visible {
		key := tasksFamilyOf(item.entry)
		if !visibleRoots[key] {
			continue
		}
		section, found := familySection[key]
		if !found || item.section < section {
			familySection[key] = item.section
		}
	}
	sections := [tasksSectionCount][]tasksItem{}
	for _, item := range visible {
		if section, found := familySection[tasksFamilyOf(item.entry)]; found {
			item.section = section
		}
		sections[item.section] = append(sections[item.section], item)
	}
	for _, section := range sections {
		r.items = append(r.items, tasksInTimeOrder(section, now)...)
	}
	// WHAT THE PLACE IS HOLDING IS COUNTED HERE, ONCE, off the rows before any
	// query has touched them ([tasksReading.whole] states why).
	r.whole = len(r.items)
	for _, item := range r.items {
		r.wholeCost += item.entry.Cost
	}
	return r
}

// tasksInTimeOrder puts one section's rows in the order the heading over them
// promises: newest first.
//
// THE SECTION IS NAMED BY TIME AND MUST THEREFORE BE ORDERED BY IT. Under
// `done today` the ages used to read `2h, 50m, 5h, 5h`, so the one question the
// heading answers — what happened most recently — could not be answered by
// reading down. The order was inherited from the world scan, which sorts
// PROJECTS by when somebody was last in one of their CONVERSATIONS
// (internal/session's world.go): a fact about conversations and not about work,
// and one that reshuffled this page every time a conversation was opened, on a
// page that is supposed to be a record.
//
// A FAMILY IS ONE PIECE OF WORK AND MOVES AS ONE. The root's own stamp places
// the whole family and the workers are ordered among themselves, because a
// child sorted on its own would drift out from under the row that explains it.
// A child whose root is outside the window is a root here ([tasksFamilies]
// states that), and is placed by its own stamp like any other.
func tasksInTimeOrder(items []tasksItem, now time.Time) []tasksItem {
	if len(items) < 2 {
		return items
	}
	newest := func(a, b tasksItem) bool {
		return tasksEntryAt(a.entry, now).After(tasksEntryAt(b.entry, now))
	}
	roots, kids := tasksFamilies(items)
	sort.SliceStable(roots, func(i, j int) bool { return newest(roots[i], roots[j]) })
	out := make([]tasksItem, 0, len(items))
	for _, root := range roots {
		out = append(out, root)
		under := kids[tasksFamilyOf(root.entry)]
		sort.SliceStable(under, func(i, j int) bool { return newest(under[i], under[j]) })
		out = append(out, under...)
	}
	return out
}

// tasksRowFor is the conversation to LABEL one of this project's rows with: the
// world's own row for the session that ran it where the scan found one, and this
// conversation otherwise — which is the honest answer for work this window
// started and no file has heard about yet.
func tasksRowFor(world session.World, mine tasksMine, entry session.TaskIndexEntry) session.SessionRow {
	if id := strings.TrimSpace(entry.SessionID); id != "" {
		for _, project := range world.Projects {
			for _, row := range project.Sessions {
				if row.ID == id {
					return row
				}
			}
		}
	}
	return mine.row
}

// tasksSectionOf files one piece of work under the question a person acts on
// next, which is the whole ordering of this place.
func tasksSectionOf(item tasksItem, now time.Time) tasksSection {
	switch {
	case item.entry.Status == string(session.TaskUnverified) || (item.row.NeedsPerson() && item.runs):
		return tasksNeeds
	case tasksWorking(item):
		return tasksRunning
	case item.runs:
		return tasksParked
	case tasksLandedToday(item.entry, now):
		return tasksToday
	}
	return tasksEarlier
}

// tasksWorking reports whether a WORKER IS IN THIS ROW right now, which is the
// narrower of the two questions [tasksItem.runs] used to be asked.
//
// runs answers "is this admitted and held by a live conversation", and it is
// true of a queued node as much as of a working one — the whole of row 3's
// defect. This asks the other half: the engine's own state word says a child
// agent is in the worktree. Both are needed and neither is the other, so the
// place asks each by name.
func tasksWorking(item tasksItem) bool {
	return item.runs && item.entry.Status == string(session.TaskRunning)
}

func tasksLandedToday(entry session.TaskIndexEntry, now time.Time) bool {
	if entry.Status != string(session.TaskDone) && entry.Status != string(session.TaskFailed) {
		return false
	}
	if entry.EndedAt.IsZero() {
		return false
	}
	y, m, d := now.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	return !entry.EndedAt.Before(start) && entry.EndedAt.Before(start.AddDate(0, 0, 1))
}

// ── the layout ──────────────────────────────────────────────────────────────

// THE PAINT AND THE POINTER READ ONE LAYOUT. They used to be two walks of the
// same rows counting the same headings, and two answers to "which line is the
// third earlier row on" is exactly how a click opens the wrong task.

type tasksLineKind uint8

const (
	// tasksLineWord is a line of the page's own prose: the header sentence, or
	// a section's word. It answers to nothing.
	tasksLineWord tasksLineKind = iota
	// tasksLineAir is the blank line between two sections. GROUPS ARE SEPARATED
	// BY WHITESPACE AND NEVER BY A DIVIDER on this surface.
	tasksLineAir
	// tasksLineTask is one piece of work.
	tasksLineTask
	// tasksLineTail is the second line of a phone card — the same row continued
	// (taskphone.go), never a row of its own.
	tasksLineTail
)

type tasksLine struct {
	kind tasksLineKind
	// text is the words on a prose line.
	text string
	// item is the work a task line and its tail are about.
	item tasksItem
	// owner is the line index of the task this line belongs to, and -1 on prose
	// and air. It is what the cursor stands on, what a press resolves to, and
	// what stops a card's second line reading as a second row.
	owner int
	// kin is the family column: the two cells in front of a row of work that say
	// where it sits in a tree. It is "" on a page with no families in it at all,
	// which is most pages — the column APPEARS when there is a tree to draw, so
	// nothing moves sideways on a machine that has never split work up.
	//
	// A ROOT CARRIES THE FOLD MARK AND A CHILD CARRIES THE CONNECTOR. The marks
	// are chosen in [tasksReading.lay] rather than in the paint, because which
	// of them a row wears is a fact about the layout — how many rows are under
	// it and whether they are drawn — and the paint may not re-derive it.
	kin string
	// folds says this line is a family root that can be opened and shut, and
	// open says it is open. They are what `→` and `←` act on, and what the row's
	// own clause reports (place_tasks.go's [app.taskSheetFold]).
	folds bool
	open  bool
	// family is the key the fold is remembered under — the pair internal/session
	// states is what identifies one row ([tasksKey]).
	family tasksKey
	// kids is how many pieces of work are under this root, which the row says
	// out loud while the fold is shut. Zero everywhere else.
	kids int
}

// tasksBareLead is the two cells in front of every row of work. On the row a
// person is on they carry the mark in the accent — `›` where the keyboard is and
// `·` where the pointer is — which is what every other list on this surface
// leads with ([overlayLead]); the lead is chosen by the frame and passed in,
// because the reading does not know where anybody is standing.
const tasksBareLead = "  "

// lay is the one walk of the reading: what line the page draws, in order, and
// which of them a person can act on.
func (r tasksReading) lay(width int) []tasksLine {
	// A MACHINE THAT HAS RUN NOTHING LAYS NOTHING OUT, and the place spends the
	// frame on [tasksTeach] instead. A machine that HAS and whose window holds
	// none of it still lays out its head line, because that line is the only
	// thing on the frame naming the window the shift-arrows move
	// ([tasksReading.held] states the law).
	if width <= 0 || r.held == 0 {
		return nil
	}
	lines := make([]tasksLine, 0, len(r.items)+8)
	// THE FAMILY COLUMN APPEARS ONLY WHERE THERE IS A TREE TO DRAW. On a machine
	// that has never split work up every row is a root with nothing under it, and
	// two cells of empty gutter in front of all of them would be a column that
	// says "there is structure here" about a page that has none.
	tree := r.families()
	add := func(kind tasksLineKind, text string) {
		lines = append(lines, tasksLine{kind: kind, text: text, owner: -1})
	}
	// THE WINDOW EDGE IS NAMED ONCE PER FRAME, and which half of the line names
	// it depends on whether the control fits. A frame with room for
	// `shift+← aug 12 – aug 25 →` has the span between the arrows, where SCREEN
	// 3d puts it — the control and the reading at once — so the sentence drops
	// its `since` clause; a frame too narrow for the control keeps the clause,
	// because a head line that named neither would leave the four arrow keys
	// moving something nothing on the frame reports.
	head := r.head(width, false)
	if arrows, _ := placeWindowFits(width, head, r.win); !arrows {
		head = r.head(width, true)
	}
	add(tasksLineWord, head)
	phone := layoutTier(width) == tierPhone
	for _, section := range tasksSectionOrder {
		items := r.section(section)
		// A SECTION WITH NOTHING IN IT IS NOT DRAWN AT ALL, filter or no filter.
		// It is the emptiness law: a `running` word with a blank under it says
		// the query found something and lost it.
		if len(items) == 0 {
			continue
		}
		add(tasksLineAir, "")
		add(tasksLineWord, tasksSectionWord(section))
		// THE SECTION IS DRAWN AS FAMILIES AND NOT AS A FLAT LIST. A run that
		// split into eight workers used to arrive as eight peers of everything
		// else on the page, which buried the six other things this machine did
		// today under one piece of work. Now the root is the row and the workers
		// fold under it — shut unless somebody opened it (see [tasksFamilies]).
		roots, kids := tasksFamilies(items)
		for _, item := range roots {
			at := len(lines)
			line := tasksLine{kind: tasksLineTask, item: item, owner: at}
			under := kids[tasksFamilyOf(item.entry)]
			if tree {
				line.kin = tasksKinPad
			}
			if len(under) > 0 {
				line.folds, line.family, line.kids = true, tasksFamilyOf(item.entry), len(under)
				line.open = r.open[line.family]
				line.kin = tasksFoldShut
				if line.open {
					line.kin = tasksFoldOpen
				}
			}
			lines = append(lines, line)
			if phone && tasksCardTail(item, r.now) != "" {
				lines = append(lines, tasksLine{kind: tasksLineTail, item: item, owner: at, kin: line.kin})
			}
			if !line.open {
				continue
			}
			for at, kid := range under {
				own := len(lines)
				kin := tasksKinCont
				if at == len(under)-1 {
					kin = tasksKinLast
				}
				lines = append(lines, tasksLine{kind: tasksLineTask, item: kid, owner: own, kin: kin})
				// phone lane: a row becomes a two-line card a thumb goes into
				// (taskphone.go), and the second line belongs to the first.
				if phone && tasksCardTail(kid, r.now) != "" {
					lines = append(lines, tasksLine{kind: tasksLineTail, item: kid, owner: own, kin: tasksKinPad})
				}
			}
		}
	}
	return lines
}

// ── the family column ───────────────────────────────────────────────────────

// The four things the two cells in front of a row of work can say. They are the
// roster's own marks (task.go's [app.railGrow] draws the same tree in the
// column beside a conversation), so a family reads the same way in both places.
const (
	// tasksKinPad is a row with no family at all, on a page that has one
	// somewhere. It holds the column open so nothing is ragged.
	tasksKinPad = "  "
	// tasksFoldShut and tasksFoldOpen are a root that has work under it.
	tasksFoldShut = "▸ "
	tasksFoldOpen = "▾ "
	// tasksKinCont and tasksKinLast are the connectors under an open root.
	tasksKinCont = "├ "
	tasksKinLast = "└ "
)

// tasksFamilyOf is the key one piece of work's FAMILY is remembered under: the
// root's id inside the session that ran it.
//
// IT IS THE (SessionID, ID) PAIR internal/session states is a row's identity,
// spelled here once. Node ids restart with every conversation, so a fold
// remembered under the id alone would open a family in another project.
func tasksFamilyOf(entry session.TaskIndexEntry) tasksKey {
	id := strings.TrimSpace(entry.Parent)
	if id == "" {
		id = strings.TrimSpace(entry.ID)
	}
	return tasksKey{session: strings.TrimSpace(entry.SessionID), id: id}
}

// tasksFamilies splits one section's rows into the roots it draws and the work
// that hangs under each of them, keeping the order the reading already ranked
// them in.
//
// Every visible member was assigned its family's most urgent section in
// [readTasks], so a visible root and its visible children are always here
// together. A child whose root is outside the selected time window still stands
// alone rather than vanishing behind a row the page cannot draw.
func tasksFamilies(items []tasksItem) ([]tasksItem, map[tasksKey][]tasksItem) {
	here := make(map[tasksKey]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.entry.Parent) == "" {
			here[tasksFamilyOf(item.entry)] = true
		}
	}
	roots := make([]tasksItem, 0, len(items))
	kids := map[tasksKey][]tasksItem{}
	for _, item := range items {
		key := tasksFamilyOf(item.entry)
		if strings.TrimSpace(item.entry.Parent) != "" && here[key] {
			kids[key] = append(kids[key], item)
			continue
		}
		roots = append(roots, item)
	}
	return roots, kids
}

// families reports whether anything on this page has work under it, which is
// what decides that the column is drawn at all.
func (r tasksReading) families() bool {
	for _, item := range r.items {
		if strings.TrimSpace(item.entry.Parent) != "" {
			return true
		}
	}
	return false
}

// rows is the whole page painted with no cursor anywhere on it, which is what a
// reader of the reading alone sees.
func (r tasksReading) rows(width int, pal palette) []string {
	lines := r.lay(width)
	out := make([]string, len(lines))
	for i := range lines {
		out[i] = r.paint(lines, i, width, pal, tasksBareLead)
	}
	return out
}

// paint draws ONE line of the layout behind the lead the frame chose. Every line
// it returns is at most width cells, at every width.
func (r tasksReading) paint(lines []tasksLine, i, width int, pal palette, lead string) string {
	if i < 0 || i >= len(lines) || width <= 0 {
		return ""
	}
	line := lines[i]
	room := width - ansi.StringWidth(lead)
	if room < 1 {
		room = 1
	}
	switch line.kind {
	case tasksLineAir:
		return ""
	case tasksLineWord:
		if i == 0 {
			// THE HEAD LINE IS THE WINDOW'S CONTROL TOO (SCREEN 3d), drawn by the
			// one head row standing and spend also draw — `shift+← aug 12 – aug 25
			// →`, the control and the reading at once. Before it, this place bound
			// all four arrow keys and drew nothing that named them, which is the
			// exact defect verbstrip.go's law was written against.
			return placeHeadRow(width, line.text, pal.muted(line.text), r.win, pal)
		}
		return pal.dim(fit(line.text, width))
	case tasksLineTail:
		indent := strings.Repeat(" ", taskSheetPhoneIndent)
		tail := room - taskSheetPhoneIndent - ansi.StringWidth(line.kin)
		if tail < 1 {
			tail = 1
		}
		return lead + pal.dim(line.kin) + indent + pal.dim(fit(tasksCardTail(line.item, r.now), tail))
	}
	// THE FAMILY COLUMN IS PAINTED HERE AND CHOSEN IN THE LAYOUT. It is dim
	// everywhere — a connector is the surface's own furniture, not the row's
	// words — and the room the row gets is what is left after it.
	kin := pal.dim(line.kin)
	room -= ansi.StringWidth(line.kin)
	if room < 1 {
		room = 1
	}
	if layoutTier(width) == tierPhone {
		return lead + kin + tasksCardHead(line.item, room, pal)
	}
	return lead + kin + tasksRow(line, room, r.now, pal)
}

// tasksUnderWord is what a shut fold says about the work it is holding.
//
// IT IS A SENTENCE AND NOT A NOTATION. `+3 under` is arithmetic with a
// preposition for a noun — the sort of shorthand a person has to be taught —
// and this surface says what things are in words: the fold HOLDS three more
// pieces of work, and one of them reads `holds 1 more`.
func tasksUnderWord(kids int) string {
	return "holds " + itoa(kids) + " more"
}

// at is the work drawn on one painted line, and whether the cursor may stand
// there. Prose, air and another window's rows are deliberately holes.
func (r tasksReading) at(lines []tasksLine, i int) (tasksItem, bool) {
	if i < 0 || i >= len(lines) || lines[i].kind != tasksLineTask {
		return tasksItem{}, false
	}
	if !lines[i].item.pick() {
		return tasksItem{}, false
	}
	return lines[i].item, true
}

// head is the sentence the page opens on: what this place is, how much of it
// there is, how far back it reaches and what it cost.
//
// THE WINDOW EDGE IS SAID HERE AND NOWHERE ELSE. It used to be repeated on a
// fold at the foot of every section, which is one number in two places and the
// drift the one-source-of-truth law exists to stop.
func (r tasksReading) head(width int, edge bool) string {
	// A WINDOW HOLDING NONE OF IT SAYS SO IN WORDS AND NOT AS A ZERO. The
	// emptiness law reaches this sentence: `0 since aug 12` is the figure the law
	// exists to forbid, and `nothing since aug 12` is the same fact a person can
	// read — with the date still on the line, which is what says the window is
	// the reason.
	//
	// `edge` IS WHETHER THIS SENTENCE HAS TO CARRY THE WINDOW'S OWN DATE. It does
	// on a frame with no room for the control; where the control is drawn, the
	// span sits between its arrows and a `since` clause here would be the same
	// date in two places on one line ([tasksReading.lay] decides which).
	//
	// IT COUNTS THE PLACE AND NOT THE QUERY ([tasksReading.whole]).
	since := ""
	if start := tasksWindowStart(r.win); edge && start != "" {
		since = " since " + start
	}
	if r.whole == 0 {
		return "work aforge ran on its own. nothing" + since + "."
	}
	// THE FIGURE GETS ITS NOUN. `10,` is a number a person has to guess the unit
	// of, and the comma after it spliced two clauses that were not a sentence —
	// worse at sixty cells, where `10 since aug 20` read as if 10 were a sum of
	// money. The count and the spend are one clause each, and the noun comes
	// from the same helper the rest of this file counts with.
	said := "work aforge ran on its own. " + itoa(r.whole) + " " + plural("piece", r.whole) + " of work" + since
	// AND THE SPEND CLAUSE IS THE FIRST THING A NARROW FRAME GIVES UP, because
	// the alternative is [placeHeadRow] cutting the sentence — and a figure with
	// its end cut off is a wrong number, which is the one thing rowfit.go's law
	// forbids anywhere on this surface. The count and the window edge are what a
	// person reads this line for; the spend has a whole place of its own.
	full := said + "."
	if r.wholeCost > 0 {
		between := " between them"
		if r.whole == 1 {
			between = " of it"
		}
		if whole := said + ", " + dollars(r.wholeCost) + between + "."; width <= 0 || ansi.StringWidth(whole) <= width {
			return whole
		}
	}
	return full
}

func tasksWindowStart(win session.UsageWindow) string {
	win = win.Normalized()
	if win.From.IsZero() {
		return ""
	}
	return strings.ToLower(win.From.Format("Jan 2"))
}

func (r tasksReading) section(want tasksSection) []tasksItem {
	items := make([]tasksItem, 0)
	for _, item := range r.items {
		if item.section == want {
			items = append(items, item)
		}
	}
	return items
}

// tasksTally is what the place says it is holding, in the same words its
// sections are headed with.
//
// THE EMPTINESS LAW HOLDS HERE TOO. A section with nothing in it is not counted
// as zero — it is not mentioned — and the line is empty when the page is.
func (r tasksReading) tally() string {
	var segs []string
	for _, section := range tasksSectionOrder {
		if n := len(r.section(section)); n > 0 {
			segs = append(segs, itoa(n)+" "+tasksSectionWord(section))
		}
	}
	return strings.Join(segs, railSep)
}

func tasksSectionWord(section tasksSection) string {
	switch section {
	case tasksNeeds:
		return "needs your look"
	case tasksRunning:
		return taskSheetNowHead
	case tasksParked:
		// ONE WORD FOR ONE FACT, AND IT IS THE COLUMN'S. The rail already calls
		// these nodes `parked` in its heading and in its footer, so the word is
		// read out of the rail's own vocabulary rather than spelled a second time
		// here — a second spelling is how the two surfaces came to disagree.
		return railGroupWords[railParked]
	case tasksToday:
		return "done today"
	default:
		return taskSheetPastHead
	}
}

// ── one row of work ─────────────────────────────────────────────────────────

// tasksLabel is what a row is CALLED, which is the label the index carries and
// the title behind it when nothing shortened one.
func tasksLabel(entry session.TaskIndexEntry) string {
	if label := strings.TrimSpace(entry.Label); label != "" {
		return label
	}
	return strings.TrimSpace(entry.Title)
}

// tasksRow is one piece of work on a wide frame, laid out by the law rowfit.go
// holds and every other list on this surface already obeys.
//
// WHAT IT USED TO DO, AND WHY IT WAS THE LAW INVERTED. The facts were measured
// first and the NAME was handed whatever remained, with a drop loop that only
// fired once the facts had taken more than the row had — so the name was
// guaranteed eight cells and the facts were guaranteed everything. At a hundred
// and sixty columns, the most room anybody has, three rows in four still drew
// `✓ Put the…` beside a ninety-character sentence of detail at its full width.
// A list exists to let a person MATCH NAMES; a name they cannot match is a row
// they have to open to read, and every scan of the page became a sequence of
// opens.
//
// So: the name keeps every cell it asks for before a fact gets one (law 1), the
// facts behind it are a RANKED PREFIX that degrades by spelling rather than
// vanishing (laws 2 and 3), and they are joined by ` · ` — the separator every
// other list here joins facts with, including this task's own page one keypress
// away. `The Certificate Rotation incomplete now` was three facts a reader had
// to re-parse into three.
//
// IT FITS AGAINST THE ROOM AND NOT THE FRAME, which is why it reaches
// [rowPlan.fit] rather than [rowHalves]: the caller has already spent the
// cursor's lead and the family column, and the two-line phone lane is
// [tasksCardHead]'s ([tasksReading.paint] routes to it before this is called),
// so the tier question is settled before we are here.
//
// It takes the LAID-OUT LINE and not the work alone, because two of the things
// on the row are facts about where it sits rather than about what it did — the
// fold it is holding shut, and the root above it that has already named their
// conversation — and the layout is where both were decided
// ([tasksReading.lay]); the paint may not re-derive either.
func tasksRow(line tasksLine, width int, now time.Time, pal palette) string {
	item := line.item
	glyph, glyphInk := tasksGlyph(item, pal)
	lead := glyph + " "
	facts := tasksFacts(line, now, pal)
	plan := rowPlan{primary: tasksLabel(item.entry), fields: tasksFields(facts)}
	label, tail := plan.fit(width - ansi.StringWidth(lead))
	left := glyphInk(glyph) + " " + tasksLabelInk(item, pal)(label)
	if tail == "" {
		return fit(left, width)
	}
	pad := width - ansi.StringWidth(lead+label) - ansi.StringWidth(tail)
	if pad < 1 {
		pad = 1
	}
	return fit(left+strings.Repeat(" ", pad)+tasksPaintTail(tail, facts, pal), width)
}

// tasksFact is one fact on a row: what it can say, in the spellings the fitter
// chooses between, and the ink it is painted in when it survives.
type tasksFact struct {
	field rowField
	// ink is nil for everything the surface draws dim, which is nearly all of
	// it. A fact with an ink of its own has it because the ink is part of the
	// fact — money is the only one on this row.
	ink func(string) string
}

// tasksFacts is the ranked prefix: what a person scanning this list reads, in
// the order they read it.
//
// THE ORDER IS THE ARGUMENT. A SHUT FOLD'S COUNT comes first because it is not
// a fact about the work at all — it is the row saying that three more rows are
// behind it, which is the difference between a page a person believes they have
// read and one they have not. The note is next because it is the only thing
// that can CORRECT the glyph — a claim of running with nobody behind it, or work
// happening in another window — and a row whose mark and whose words disagree is
// worse than a row missing a fact. The age follows, because every section here
// is named by time and the age is what a person scans down; then money, the
// figure nobody can recover by looking; then where the work came from, then what
// came of it, and last the kind — a setting somebody chose before the work
// started, which is news to nobody afterwards.
func tasksFacts(line tasksLine, now time.Time, pal palette) []tasksFact {
	item := line.item
	entry := item.entry
	source := strings.TrimSpace(item.row.Title)
	if source == "" {
		source = strings.TrimSpace(item.row.Project)
	}
	// A CHILD DOES NOT REPEAT ITS PARENT'S CONVERSATION. Opening a family drew
	// the root's title four times down the page, spending twenty-odd cells a row
	// to restate a fact the row two lines up had already stated — on the rows
	// whose names were being cut to make room for it.
	if line.kin == tasksKinCont || line.kin == tasksKinLast {
		source = ""
	}
	fold := rowSay()
	if line.folds && !line.open && line.kids > 0 {
		// A MARK WITH NO COUNT is a mark a person has to open to find out
		// whether it was worth opening.
		fold = rowSay(tasksUnderWord(line.kids))
	}
	money := rowSay()
	if entry.Cost > 0 {
		money = rowSay(dollars(entry.Cost))
	}
	return []tasksFact{
		{field: fold},
		{field: rowSay(tasksNote(item))},
		{field: tasksAgeField(item, now)},
		{field: money, ink: placeMoneyInk(pal)},
		{field: rowSay(source)},
		{field: tasksMiddleField(entry)},
		{field: rowSay(session.TaskKindWord(entry.Kind))},
	}
}

// tasksFields is the facts as the fitter takes them.
func tasksFields(facts []tasksFact) []rowField {
	fields := make([]rowField, 0, len(facts))
	for _, fact := range facts {
		fields = append(fields, fact.field)
	}
	return fields
}

// tasksPaintTail paints a fitted tail fact by fact.
//
// IT PAINTS THE SEPARATORS ITSELF because a hue nested inside a hue ends at the
// inner one's reset (room.go's [app.roomHeadWord] states the same rule), so a
// tail carrying a money figure may not be painted whole. Anything the fitter
// spelled that no fact answers to — the halves of a detail sentence that had a
// ` · ` of its own inside it — is the dim every other fact wears.
func tasksPaintTail(tail string, facts []tasksFact, pal palette) string {
	if tail == "" {
		return ""
	}
	ink := func(said string) string {
		for _, fact := range facts {
			if fact.ink == nil || said == "" {
				continue
			}
			if said == fact.field.full || said == fact.field.short || said == fact.field.tiny {
				return fact.ink(said)
			}
		}
		return pal.dim(said)
	}
	said := strings.Split(tail, rowSep)
	for i := range said {
		said[i] = ink(said[i])
	}
	return strings.Join(said, pal.dim(rowSep))
}

// tasksAgeField is HOW LONG AGO, and it is the fact this page's own headings
// promise.
//
// A STOPPED ROW IS NOT DATED `now`. [tasksEntryAt] files an undated row at the
// moment of the reading, which is the right answer for a WINDOW — a window
// cannot judge a row with no stamp on it — and the wrong one for an AGE: a row
// reading `The Certificate Rotation incomplete now` says in one half that the
// window running this is gone and in the other that it is happening this
// second, and the age is the half a person believes.
//
// AND NOTHING DATES IT. The index holds one stamp, [session.TaskIndexEntry]'s
// EndedAt, and it is zero on exactly the rows this case is about — a row that
// claims to be running has not landed, and no start time is written anywhere a
// window that died could have left one. So the row says NOTHING about when,
// which is the emptiness law's own answer, and the note beside it carries the
// whole of what is known: `incomplete`.
//
// NOR IS A PARKED ROW DATED `now`. A node admitted and waiting behind the piece
// that needs a person has not started, so there is no age to count from either
// end of it, and `now` on that row said the work was happening this second.
func tasksAgeField(item tasksItem, now time.Time) rowField {
	return rowSay(sinceAt(tasksEntryStamp(item, now), now))
}

// tasksEntryStamp is the moment a row's work IS, for DRAWING AN AGE FROM, and
// the zero time whenever nobody knows one — which [sinceAt] then draws as
// nothing at all.
//
// IT IS NOT [tasksEntryAt], AND THE DIFFERENCE IS THE WHOLE POINT. That one
// answers the time WINDOW, which cannot judge a row with no stamp on it and so
// files an undated row at the moment of the reading rather than dropping it off
// the page. This one answers a person, and a person told `now` about work that
// nothing is doing has been told something false. The three silences here are a
// node nothing has started, a row whose window died, and work replayed out of a
// checkpoint that kept how long it ran and never when it began.
func tasksEntryStamp(item tasksItem, now time.Time) time.Time {
	if item.entry.Live() {
		if tasksWorking(item) {
			return now
		}
		return time.Time{}
	}
	return item.entry.EndedAt
}

// tasksMiddleField is what came of the work, in two spellings: everything the
// record can say about it, and the count of files alone.
//
// LAW 2: A FACT DEGRADES BEFORE IT DISAPPEARS. `2 files · Annual is the default
// and the monthly price stays visible beside it.` is ninety cells of detail
// that used to end the tail and eat the name beside it; `2 files` is the same
// fact at seven, and it survives three widths further down.
func tasksMiddleField(entry session.TaskIndexEntry) rowField {
	full := tasksMiddle(entry)
	if full == "" {
		return rowSay()
	}
	if entry.FilesChanged > 0 {
		if short := itoa(entry.FilesChanged) + plural(" file", entry.FilesChanged); short != full {
			return rowSay(full, short)
		}
	}
	// A FAILED ROW'S SHORT SPELLING IS ITS STATE WORD, which is the word its own
	// page says ([tasksMiddle] joins the two).
	if word := taskStateWord(entry, false); strings.HasPrefix(full, word+rowSep) {
		return rowSay(full, word)
	}
	return rowSay(full)
}

// tasksCardHead is the first line of a phone card: the state and the name, in
// the same hue the wide row paints them.
func tasksCardHead(item tasksItem, width int, pal palette) string {
	glyph, glyphInk := tasksGlyph(item, pal)
	lead := glyphInk(glyph) + " "
	room := width - ansi.StringWidth(lead)
	if room < 1 {
		room = 1
	}
	return lead + tasksLabelInk(item, pal)(fit(tasksLabel(item.entry), room))
}

// tasksCardTail is the second line of a phone card: what the work came to, where
// it is happening, and how long ago — each dropped when it has nothing behind
// it, so a card with nothing to add stays one line.
func tasksCardTail(item tasksItem, now time.Time) string {
	var segs []string
	if middle := tasksMiddle(item.entry); middle != "" {
		segs = append(segs, middle)
	}
	if note := tasksNote(item); note != "" {
		segs = append(segs, note)
	}
	// The card dates a row the way the wide row does, and says nothing where
	// nothing dated it ([tasksAgeField]).
	if age := tasksAgeField(item, now).full; age != "" {
		segs = append(segs, age)
	}
	return strings.Join(segs, railSep)
}

// tasksLabelInk is THE HUE AS THE CLAIM: dulled means this is the record, and a
// row that is genuinely running is not the record. The ink is the one a running
// node's title wears in the column ([app.railTitle]), so a person who has
// watched work run recognizes it here without learning a second signal.
func tasksLabelInk(item tasksItem, pal palette) func(string) string {
	if item.runs {
		return pal.ink
	}
	return pal.muted
}

// tasksNote is what one row says about WHERE the work is, or about a claim of
// running with nobody behind it.
//
// THE RECORD IS A FILE AND THE FILE CANNOT CORRECT ITSELF (taskview.go states
// the law). A row takes the word `running` when the work starts and nothing
// rewrites it, so a window that was killed leaves rows claiming a present that
// ended hours ago. What is drawn is [taskRecordStoppedWord] — not a judgement
// about the work, only the fact that the window went.
func tasksNote(item tasksItem) string {
	if item.away {
		return taskAwayNote(item.window)
	}
	if item.entry.Live() && !item.runs {
		return taskRecordStoppedWord
	}
	return ""
}

// tasksEntryAt is WHEN a row is, for the time window and for the age on it: the
// moment it landed, and NOW for work that is still going or that nothing dated.
// An undated row is filed at the moment of the reading rather than dropped — a
// window cannot judge a row with no stamp on it, and dropping it would take work
// that stopped off every surface this program has.
func tasksEntryAt(entry session.TaskIndexEntry, now time.Time) time.Time {
	if entry.Live() || entry.EndedAt.IsZero() {
		return now
	}
	return entry.EndedAt
}

func tasksMiddle(entry session.TaskIndexEntry) string {
	if activity := strings.TrimSpace(entry.Activity); activity != "" {
		return activity
	}
	if entry.Status == string(session.TaskFailed) && refused(entry.Ending) {
		if outcome := strings.TrimSpace(entry.Outcome); outcome != "" {
			return outcome
		}
		return endingWordRefused
	}
	if entry.Status == string(session.TaskFailed) && strings.TrimSpace(entry.Outcome) != "" {
		// ONE STATE WORD ON BOTH SURFACES. The row used to read `gave up, said
		// why` where its own page, one keypress away, opened `failed · landed 8d
		// ago`, and a person who filed the row under one of those words could not
		// find it under the other. The word is [taskStateWord]'s, which is where
		// the page takes it from too, and the reason follows it rather than being
		// promised — a promise of a reason is a row you have to open to read.
		return taskStateWord(entry, false) + rowSep + strings.TrimSpace(entry.Outcome)
	}
	var parts []string
	if entry.FilesChanged > 0 {
		parts = append(parts, itoa(entry.FilesChanged)+plural(" file", entry.FilesChanged))
	}
	if outcome := strings.TrimSpace(entry.Outcome); outcome != "" {
		parts = append(parts, outcome)
	}
	return strings.Join(parts, " · ")
}

func tasksGlyph(item tasksItem, pal palette) (string, func(string) string) {
	if item.section == tasksNeeds {
		return tokens.GlyphNeedsHuman, pal.warn
	}
	// A ROW NOTHING IS RUNNING DOES NOT WEAR THE RUNNING GLYPH. [glyphIdle] takes
	// its place — the dot this surface already spends on a call that was still
	// going when its turn ended — because a frozen spinner would claim the work
	// is alive and a dot claims nothing.
	if item.entry.Live() && !item.runs {
		if pal.ascii {
			return glyphIdleASCII, pal.dim
		}
		return glyphIdle, pal.dim
	}
	switch item.entry.Status {
	case string(session.TaskRunning):
		return tokens.GlyphWorking, pal.live
	case string(session.TaskQueued):
		return tokens.GlyphQueued, pal.dim
	case string(session.TaskDone):
		return tokens.GlyphSettled, pal.muted
	case string(session.TaskFailed):
		if refused(item.entry.Ending) {
			return glyphHalted, pal.warn
		}
		return tokens.GlyphFailed, pal.bad
	default:
		return tokens.GlyphQueued, pal.dim
	}
}

// step keeps the four time keys in one grammar shared with spend.
func (r tasksReading) step(win session.UsageWindow, key string) session.UsageWindow {
	return placeWindowStep(win, key)
}

// tasksTeach spends an empty page on explaining the place rather than drawing
// headings for lists that do not exist.
//
// EVERY DOOR ONTO THIS PLACE REACHES IT NOW. `ctrl+.` and /history used to
// refuse to raise a page with nothing on it, so the teaching was only what a
// person who walked in with `tab` found; they go through the router with every
// other door ([app.showTaskPlace] tells the story), and this is what all of them
// answer with on a machine that has run nothing.
//
// THE LAST LINE IS WHAT /history USED TO SAY INSTEAD OF OPENING
// ([taskSheetEmpty]), spelled once and moved rather than written again: what a
// person does about an empty page belongs on the empty page.
func tasksTeach(pal palette) []string {
	return []string{
		pal.dim("tasks is the history of work this machine has run."),
		pal.dim("it lists work aforge ran on its own, across every project."),
		pal.dim("enter opens a task's room when there is one here."),
		pal.dim(taskSheetEmpty),
	}
}
