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
// AND WHAT IT DRAWS IS A TREE AND NOT A LIST: the conversation that asked for
// the work, the work, and the work that work asked for, to whatever depth it
// goes ([tasksTreeOf] states the shape and why it is the honest one).
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
	// live keeps task-specific facts that the historical index cannot carry.
	live *session.TaskStatus
	// row is the conversation the work came out of, and it is a LABEL: it says
	// where to file the row on screen and it is never asked whether the work is
	// running. That answer travels on [tasksItem.runs] instead, from whichever
	// authority produced this row.
	row session.SessionRow
	// section is what THIS PIECE OF WORK needs next, and it is never anybody
	// else's answer: the mark on the row and the word beside it are read off it
	// ([tasksGlyph]). Where the row is DRAWN is its conversation's answer and
	// lives on the tree instead ([tasksTree.filed]).
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
	// here says the window named above is ANOTHER CONVERSATION OF THIS PROCESS.
	// One terminal holds any number of conversations, all of them running
	// (keeper.go), and every one of them writes the same presence file every
	// other terminal on the project reads — so its work arrives on this page
	// through the away authority like a stranger's, and the way to it is `tab`
	// rather than another terminal. It is a fact about THE WINDOW, not about the
	// door: what the door is is decided once, in [app.taskOwnerOf].
	here bool
}

// pick reports whether the CURSOR may stand on this row, and EVERY ROW OF WORK
// THIS PAGE DRAWS ANSWERS YES.
//
// A VISIBLE ROW IS NEVER INERT. This used to answer false for work another
// window is holding, on the argument that the two doors onto a piece of work —
// a ROOM, which is a live lane onto a node in THIS session's graph, and the
// record CARD, which is minted out of a landed row — are both missing for it.
// The argument was sound about the doors and wrong about the row: a person
// reading a list where nine rows take the cursor and the tenth silently refuses
// it has been handed a screen that appears broken, and the answer they needed —
// WHICH window is running this, and that they have to go there — was the one
// thing pressing it could not tell them. So the row is pressed, and what it
// opens is the card that says exactly that ([app.taskSheetAwayCard]).
//
// The door it opens is still not a room and still not a mention. Nothing here
// invents a lane into another process's graph; what changed is that the refusal
// is now a page a person can read rather than a keystroke that does nothing.
func (i tasksItem) pick() bool { return true }

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
	// here is which of those windows are conversations THIS PROCESS is holding,
	// by conversation id ([app.heldSessions]). The presence reading cannot tell —
	// a stowed conversation of this terminal writes the same file as a terminal
	// across the desk — and the difference decides whether the row's door is a
	// switch or a second view onto the engine.
	here map[string]bool
}

// tasksMineRow is one of those rows: the work, and whether it is happening.
type tasksMineRow struct {
	entry session.TaskIndexEntry
	runs  bool
	live  *session.TaskStatus
}

// tasksReading is everything drawing and routing need from one world reading.
//
// seen is deliberately retained even though state grouping does not use it: it
// is the look stamp paired with this snapshot, and a later place adapter must
// not need to reach back to disk to preserve that boundary.
type tasksReading struct {
	items      []tasksItem
	shape      *tasksTree
	chats      []session.SessionRow
	wholeChats int
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
	// open is what a person has SET about the folds on this page, and it is the
	// PLACE'S state handed in rather than the reading's own: a snapshot is
	// replaced whole every time a node lands (place_tasks.go), and a fold that
	// lived here would shut itself every time the page reloaded under somebody
	// reading it.
	//
	// PRESENCE MEANS SET AND ABSENCE MEANS THE ROW'S OWN DEFAULT, which is what
	// lets a conversation open by default and still be shut by hand
	// ([tasksReading.opens] holds both halves).
	open map[tasksKey]bool
	// unfolded opens every fold on the page at once, and exactly one thing turns
	// it on: a query. A row that matched and is behind a fold is a row the query
	// appears not to have found ([tasksPlace.filtered]).
	unfolded bool
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
		if row.entry.Parent == "" {
			row.entry.Parent = held[tasksKeyOf(row.entry)].entry.Parent
		}
		put(tasksKeyOf(row.entry), tasksItem{
			entry: row.entry, row: tasksRowFor(world, mine, row.entry), runs: row.runs, live: row.live,
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
		key := tasksKeyOf(entry)
		entry.Parent = held[key].entry.Parent
		put(key, tasksItem{
			entry: entry, row: tasksRowFor(world, mine, entry), runs: true, away: true, window: task.Session,
			here: mine.here[strings.TrimSpace(task.SessionID)],
		})
	}

	// EVERY ROW IS JUDGED BY ITS OWN STATE AND NOTHING ELSE RE-FILES IT. What one
	// piece of work needs next is a fact about that piece of work: the mark it
	// wears and the word beside it are read straight off this
	// ([tasksGlyph], [tasksSectionOf]), and a row that took its section from a
	// relative would wear a relative's mark.
	//
	// WHERE IT IS DRAWN IS A DIFFERENT QUESTION, and the tree answers it: a whole
	// conversation stands under its most urgent member's heading ([tasksTreeOf]),
	// which is what keeps a refused child visibly under its still-running parent
	// and both of them under the chat that asked for the work.
	visible := make([]tasksItem, 0, len(order))
	r.chats = tasksConversationRows(world, mine, r.win, now)
	r.wholeChats = len(r.chats)
	r.held = len(order) + len(r.chats)
	for _, key := range order {
		item := held[key]
		if !r.win.Holds(tasksEntryAt(item.entry, now)) {
			continue
		}
		item.section = tasksSectionOf(item, now)
		visible = append(visible, item)
	}
	// AND THE ORDER THE ROWS ARRIVE IN IS THE ORDER THE PAGE DRAWS THEM, settled
	// once here so that every reader of [tasksReading.items] — the tally, the
	// filter, the layout — walks one list in one order.
	tree := tasksTreeOf(visible, now, r.chats...)
	r.items = tree.order()
	r.shape = &tree
	// WHAT THE PLACE IS HOLDING IS COUNTED HERE, ONCE, off the rows before any
	// query has touched them ([tasksReading.whole] states why).
	r.whole = len(r.items)
	for _, item := range r.items {
		r.wholeCost += item.entry.Cost
	}
	return r
}

// tasksNewer is the one order this page ranks anything in: newest first.
//
// THE SECTIONS ARE NAMED BY TIME AND MUST THEREFORE BE ORDERED BY IT. Under
// `finished today` the ages used to read `2h, 50m, 5h, 5h`, so the one question
// the heading answers — what happened most recently — could not be answered by
// reading down. The order was inherited from the world scan, which sorts
// PROJECTS by when somebody was last in one of their CONVERSATIONS
// (internal/session's world.go): a fact about conversations and not about work,
// and one that reshuffled this page every time a conversation was opened, on a
// page that is supposed to be a record.
func tasksNewer(a, b tasksItem, now time.Time) bool {
	return tasksEntryAt(a.entry, now).After(tasksEntryAt(b.entry, now))
}

// tasksRowFor is the conversation to LABEL one of this project's rows with: the
// world's own row for the session that ran it where the scan found one, and this
// conversation otherwise — which is the honest answer for work this window
// started and no file has heard about yet.
//
// AND THE FALLBACK IS ONLY EVER OFFERED FOR OUR OWN WORK. A row NAMES its owner
// ([session.TaskIndexEntry.SessionID]), and where that name is somebody else's
// and the scan has not met them, the honest answer is that this surface does not
// know the conversation — not this one. Falling through was a quiet
// misattribution with a person-visible face: the card over a task another window
// is running opened `out of <the conversation you are sitting in>`, which is the
// one sentence on that page a person would act on, and it named the wrong owner.
// A row with nothing behind it is the emptiness law's own answer, and every
// reader of this already drops an empty title ([tasksFacts],
// [app.taskCardSourceLine]).
func tasksRowFor(world session.World, mine tasksMine, entry session.TaskIndexEntry) session.SessionRow {
	id := strings.TrimSpace(entry.SessionID)
	if id != "" {
		for _, project := range world.Projects {
			for _, row := range project.Sessions {
				if row.ID == id {
					return row
				}
			}
		}
	}
	// A NAME THIS SURFACE CANNOT PLACE IS STILL A NAME. The scan did not find the
	// conversation, so nothing here knows what it is called — but it is not this
	// one unless it says so, and no substitute for an owner is better than the
	// wrong owner ([app.taskSheetOwnsEntry] refuses on the same rule, and states
	// what it costs).
	if id != "" && id != strings.TrimSpace(mine.row.ID) {
		return session.SessionRow{}
	}
	return mine.row
}

// status uses the live node's complete reading when this window owns it.
// A conversation's unrelated question says nothing about an individual task.
func (item tasksItem) status() session.TaskStatus {
	if item.live != nil {
		return *item.live
	}
	return taskEntryStatus(item.entry, item.runs)
}

// tasksSectionOf files one piece of work under the question a person acts on
// next, which is the whole ordering of this place.
func tasksSectionOf(item tasksItem, now time.Time) tasksSection {
	switch {
	case item.status().Attention:
		return tasksNeeds
	case tasksWorking(item):
		return tasksRunning
	case item.runs || item.status().Presence == session.TaskPresenceWaiting:
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
	if !item.runs {
		return false
	}
	switch item.status().Presence {
	case session.TaskPresenceWorking, session.TaskPresenceFinishing:
		return true
	}
	return false
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
	// tasksLineChat is a MAIN CONVERSATION standing over the work it asked for.
	// It is deliberately its own kind and not a task line wearing a made-up
	// entry: a conversation has no id in the record, nothing can stop it and
	// nothing can be replayed from it, and a synthetic row would have arrived at
	// [tasksPlace.verbs] and at the card offering both.
	tasksLineChat
)

type tasksLine struct {
	kind tasksLineKind
	// text is the words on a prose line.
	text string
	// item is the work a task line and its tail are about.
	item tasksItem
	// chat is the conversation a [tasksLineChat] is about, and the zero value
	// everywhere else.
	chat tasksChat
	// under says THE CONVERSATION THIS ROW CAME OUT OF IS ALREADY NAMED ABOVE IT,
	// by the chat row it hangs under or by the piece of work it was cut out of.
	// The row then spends those cells on its own name instead ([tasksFacts]).
	under bool
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
	// whose work this surface cannot place in any conversation, and that has never
	// split a task up, every row is a root with nothing under it — and two cells
	// of empty gutter in front of all of them would be a column saying "there is
	// structure here" about a page that has none.
	tree := r.tree()
	column := tree.column()
	levels := tasksKinRoom(width)
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

	// work draws one piece of work and, while its fold is open, everything under
	// it — to whatever depth the record goes.
	//
	// A ROW WITH WORK UNDER IT WEARS THE FOLD AND NOT THE CONNECTOR. There are two
	// cells and three things they could say; the fold is a key a person can press,
	// the connector is furniture, and the indent in front of both has already said
	// where the row sits.
	var work func(item tasksItem, depth int, last, named, nested bool)
	work = func(item tasksItem, depth int, last, named, nested bool) {
		key := tasksKeyOf(item.entry)
		kids := tree.kids[key]
		own := len(lines)
		line := tasksLine{kind: tasksLineTask, item: item, owner: own, under: named || nested}
		mark := ""
		switch {
		case len(kids) > 0:
			line.folds, line.family, line.kids = true, key, len(kids)
			line.open = r.opens(key)
			mark = tasksFoldShut
			if line.open {
				mark = tasksFoldOpen
			}
		case nested:
			mark = tasksKinCont
			if last {
				mark = tasksKinLast
			}
		case column:
			mark = tasksKinPad
		}
		line.kin = tasksKin(depth, levels, mark)
		lines = append(lines, line)
		// phone lane: a row becomes a two-line card a thumb goes into
		// (taskphone.go), and the second line belongs to the first.
		if phone && tasksCardTail(item, r.now) != "" {
			lines = append(lines, tasksLine{
				kind: tasksLineTail, item: item, owner: own,
				kin: tasksKin(depth, levels, tasksKinPad),
			})
		}
		if !line.open {
			return
		}
		for at, kid := range kids {
			work(kid, depth+1, at == len(kids)-1, named, true)
		}
	}

	for _, section := range tasksSectionOrder {
		groups := tree.in(section)
		// A SECTION WITH NOTHING IN IT IS NOT DRAWN AT ALL, filter or no filter.
		// It is the emptiness law: a `running` word with a blank under it says
		// the query found something and lost it.
		if len(groups) == 0 {
			continue
		}
		add(tasksLineAir, "")
		add(tasksLineWord, tasksSectionHead(section, tree.held(section), tree.shown(r, section)))
		// THE SECTION IS DRAWN AS CONVERSATIONS AND NOT AS A FLAT LIST. A chat that
		// split one ask into eight workers used to arrive as eight peers of
		// everything else on the page; now the conversation is the row, the work it
		// asked for hangs under it, and the work THAT asked for hangs under that —
		// each fold shut or open by its own default ([tasksReading.opens]).
		for _, g := range groups {
			depth := 0
			if g.named {
				line := tasksLine{
					kind: tasksLineChat, chat: g.chat, owner: len(lines),
					folds: len(g.roots) > 0, family: g.chat.key, kids: g.chat.kids,
					open: r.opens(g.chat.key),
				}
				mark := tasksKinPad
				if line.folds {
					mark = tasksFoldShut
				}
				if line.folds && line.open {
					mark = tasksFoldOpen
				}
				line.kin = tasksKin(0, levels, mark)
				lines = append(lines, line)
				if !line.open {
					continue
				}
				depth = 1
			}
			for at, root := range g.roots {
				work(root, depth, at == len(g.roots)-1, g.named, false)
			}
		}
	}
	return lines
}

// tasksKin is the family column in front of one row: one step of indent for
// every ancestor the frame has room to draw, and the two cells that say what
// this row IS.
func tasksKin(depth, levels int, mark string) string {
	if depth > levels {
		depth = levels
	}
	if depth <= 0 {
		return mark
	}
	return strings.Repeat(tasksKinStep, depth) + mark
}

// tasksKinRoom is how many levels of indent this frame can afford.
//
// THE COLUMN MAY NEVER TAKE THE CELLS THE NAME NEEDS (rowfit.go's law 1). Work
// six deep on a sixty-column frame would spend a fifth of every row saying where
// the row sits and then cut the words saying what it is — so past the level this
// answers, deeper work shares the deepest indent and is told apart by its
// connector and by the row above it.
func tasksKinRoom(width int) int {
	levels := width / 16
	if levels > tasksKinLevels {
		levels = tasksKinLevels
	}
	if levels < 1 {
		levels = 1
	}
	return levels
}

// ── the family column ───────────────────────────────────────────────────────

// The four things the two cells in front of a row of work can say. They are the
// roster's own marks (task.go's [app.railGrow] draws the same tree in the
// column beside a conversation), so a family reads the same way in both places.
const (
	// tasksKinPad is a row with nothing under it, on a page that has a shape
	// somewhere. It holds the column open so nothing is ragged.
	tasksKinPad = "  "
	// tasksFoldShut and tasksFoldOpen are a row that HAS something under it — a
	// conversation over its work, or a piece of work over the work it was cut
	// into. One mark for one act: both open with `→` and shut with `←`.
	tasksFoldShut = "▸ "
	tasksFoldOpen = "▾ "
	// tasksKinCont and tasksKinLast are the connectors under an open piece of
	// work, on the rows that hold nothing themselves.
	tasksKinCont = "├ "
	tasksKinLast = "└ "
)

// tasksFamilyOf is the FOLD one piece of work hangs under: its parent's key,
// and its own where it has no parent on this page.
//
// IT IS THE (SessionID, ID) PAIR internal/session states is a row's identity,
// spelled here once. Node ids restart with every conversation, so a fold
// remembered under the id alone would open a family in another project.
//
// A ROW'S OWN FOLD IS [tasksKeyOf], AND THE TWO ARE THE SAME THING ONLY FOR A
// ROOT. Work is nested to whatever depth the record goes ([tasksTreeOf]), so the
// fold that holds a row and the fold a row holds are two different keys on every
// worker that has workers of its own.
func tasksFamilyOf(entry session.TaskIndexEntry) tasksKey {
	id := strings.TrimSpace(entry.Parent)
	if id == "" {
		id = strings.TrimSpace(entry.ID)
	}
	return tasksKey{session: strings.TrimSpace(entry.SessionID), id: id}
}

// ── the tree: a conversation, its work, and the work under that ─────────────

// A MAIN CONVERSATION IS THE ROOT OF ITS OWN WORK, AND THIS PAGE DRAWS IT THAT
// WAY.
//
// Nothing on this machine happens outside a conversation: somebody asks for
// something in a chat, the chat cuts it into tasks, and a task cuts itself into
// more. The page used to draw the middle of that sentence and throw both ends
// away — every task on the machine as a peer of every other, whichever chat
// commissioned it, one level of family and no more — so eight workers of one
// conversation and one question asked in another arrived as nine equal things a
// person had to re-sort in their head. It also cost the page the one door it
// most obviously owed: from a piece of work back to the chat that asked for it.
//
// So the page is the tree that was always in the record: the conversation, the
// work it asked for, and the work that work asked for, to whatever depth it
// goes ([session.TaskIndexEntry.Parent] is an IMMEDIATE parent and always was).
//
// A CONVERSATION NOTHING NAMED IS NOT A ROW. The tree is built out of what the
// authorities already know, and a row invented for a conversation this surface
// cannot name would be a fold with a blank on it standing over real work. That
// work keeps the place it has always had, at the top of its section — the same
// refusal [readTasks] makes about a task with no words on it, and the same one
// [tasksRowFor] makes about naming an owner it cannot place.
const (
	// tasksKinStep is one level of the family column and tasksKinLevels the most
	// levels it will ever draw — deeper work shares the deepest indent rather
	// than marching off the row ([tasksKinRoom] cuts it further on a narrow
	// frame).
	tasksKinStep   = "  "
	tasksKinLevels = 4
)

// tasksChatMark is what makes a conversation's key UNMISTAKABLE for a piece of
// work's. Node ids are decimal ([session.TaskIndexEntry.ID]) and this is not a
// number at all, so the fold map, the cursor's memory across a rebuild and the
// verb strip can all hold both kinds of row in one key space — and a collision
// there would be a fold opening the wrong row, or worse, a conversation offered
// a task's verbs.
const tasksChatMark = "\x00conversation"

func tasksChatKey(id string) tasksKey {
	return tasksKey{session: strings.TrimSpace(id), id: tasksChatMark}
}

// chat reports that this key names a conversation rather than a piece of work.
func (k tasksKey) chat() bool { return k.id == tasksChatMark }

// tasksChatOf names the conversation one row belongs to: the owner the record
// carries, and the conversation the reading LABELLED it with where the record
// carries none — which is the honest answer for rows written before the index
// named their session, and for this window's own unlanded work.
func tasksChatOf(item tasksItem) string {
	if id := strings.TrimSpace(item.entry.SessionID); id != "" {
		return id
	}
	return strings.TrimSpace(item.row.ID)
}

// tasksChat is one conversation as this place draws it: the row a person opens,
// what it is called, and what it is holding.
type tasksChat struct {
	// key is the fold's name and the cursor's name for this row.
	key tasksKey
	// row is the conversation as the world knows it, and it is the whole of what
	// the door needs ([app.openConversationRow] takes exactly this).
	row   session.SessionRow
	title string
	// kids is how many rows OPENING THIS ONE puts on the page, which is what the
	// shut fold says out loud. Work nested under those is behind their own folds
	// and is counted by the section's heading instead ([tasksSectionHead]).
	kids int
	// runs says a worker is in one of them, which is the hue the row wears — the
	// same claim a live piece of work's own title makes ([tasksLabelInk]).
	runs bool
	// at is the newest thing in it THAT ANYBODY CAN DATE, and the zero time where
	// nothing can be. It is deliberately not the stamp the conversation SORTS by:
	// live work is dated `now` for ranking and says nothing at all about when
	// ([tasksEntryAt] and [tasksEntryStamp] hold the two halves apart).
	at time.Time
}

// tasksGroup is what one section holds: a conversation and the work under it,
// or ONE piece of work whose conversation nothing on this page could name.
//
// AN UNNAMED CONVERSATION IS NOT A GROUP. Filing several rows together under a
// heading, without drawing the row that says why they are together, would move
// work between sections for a reason nothing on screen states.
type tasksGroup struct {
	chat  tasksChat
	named bool
	roots []tasksItem
	held  int
	// section is where the whole group stands: its most urgent member's answer,
	// at every depth.
	section tasksSection
	order   time.Time
}

// tasksTree is one reading as a shape rather than a list: the groups in the
// order the page draws them, the work under each piece of work, the way back up,
// and the section every row is filed under.
type tasksTree struct {
	groups []tasksGroup
	kids   map[tasksKey][]tasksItem
	up     map[tasksKey]tasksKey
	at     map[tasksKey]tasksItem
	filed  map[tasksKey]tasksSection
}

// tasksTreeOf builds that shape, and it is the ONE place the page's structure is
// decided — the layout, the tally and the section headings all read this rather
// than each walking the rows their own way.
func tasksTreeOf(items []tasksItem, now time.Time, chats ...session.SessionRow) tasksTree {
	t := tasksTree{
		kids:  map[tasksKey][]tasksItem{},
		up:    make(map[tasksKey]tasksKey, len(items)),
		at:    make(map[tasksKey]tasksItem, len(items)),
		filed: make(map[tasksKey]tasksSection, len(items)),
	}
	for _, item := range items {
		t.at[tasksKeyOf(item.entry)] = item
	}
	// parentOf is one row's parent WHERE THE PAGE IS DRAWING THAT PARENT TOO. A
	// child whose parent is outside the time window, or filtered off the page,
	// stands on its own rather than vanishing behind a row that is not there.
	parentOf := func(key tasksKey) (tasksKey, bool) {
		item, found := t.at[key]
		if !found {
			return tasksKey{}, false
		}
		id := strings.TrimSpace(item.entry.Parent)
		if id == "" {
			return tasksKey{}, false
		}
		// THE PARENT IS NAMED INSIDE THE SAME CONVERSATION. Ids restart with every
		// one of them, so a parent id read against the whole machine would hang this
		// row under a stranger's work that happens to wear the same number.
		parent := tasksKey{session: key.session, id: id}
		if parent == key {
			return tasksKey{}, false
		}
		if _, found := t.at[parent]; !found {
			return tasksKey{}, false
		}
		return parent, true
	}
	// grounded reports that walking up from a row ENDS. A record claiming a row is
	// its own grandparent is a record and not a tree; the row is then drawn where a
	// row with no parent is drawn, which loses the claimed nesting and keeps the
	// work on the page rather than the other way round — and, more to the point,
	// keeps this walk from being the thing that never returns.
	grounded := func(key tasksKey) bool {
		seen := map[tasksKey]bool{key: true}
		at := key
		for {
			parent, ok := parentOf(at)
			if !ok {
				return true
			}
			if seen[parent] {
				return false
			}
			seen[parent] = true
			at = parent
		}
	}
	roots := make([]tasksItem, 0, len(items))
	for _, item := range items {
		key := tasksKeyOf(item.entry)
		if parent, ok := parentOf(key); ok && grounded(key) {
			t.up[key] = parent
			t.kids[parent] = append(t.kids[parent], item)
			continue
		}
		roots = append(roots, item)
	}
	for key, kids := range t.kids {
		sort.SliceStable(kids, func(i, j int) bool { return tasksNewer(kids[i], kids[j], now) })
		t.kids[key] = kids
	}

	// THE CONVERSATION IS NAMED ONCE, out of whichever authority knew it — and a
	// conversation with no name is left unnamed rather than called after its
	// project or its folder, because two untitled chats of one project would then
	// draw two identical rows opening two different places.
	names := map[string]session.SessionRow{}
	for _, row := range chats {
		names[row.ID] = row
	}
	for _, item := range items {
		id := tasksChatOf(item)
		if id == "" || strings.TrimSpace(item.row.Title) == "" {
			continue
		}
		if _, found := names[id]; !found {
			names[id] = item.row
		}
	}
	group := map[string]int{}
	for _, root := range roots {
		id := tasksChatOf(root)
		row, named := names[id]
		if !named {
			t.groups = append(t.groups, tasksGroup{roots: []tasksItem{root}})
			continue
		}
		if at, found := group[id]; found {
			t.groups[at].roots = append(t.groups[at].roots, root)
			continue
		}
		group[id] = len(t.groups)
		t.groups = append(t.groups, tasksGroup{
			named: true,
			chat:  tasksChat{key: tasksChatKey(id), row: row, title: strings.TrimSpace(row.Title)},
			roots: []tasksItem{root},
		})
	}

	// A main chat remains a task even when it has delegated no work yet.
	for _, row := range chats {
		if _, found := group[row.ID]; found {
			continue
		}
		group[row.ID] = len(t.groups)
		t.groups = append(t.groups, tasksGroup{named: true,
			chat: tasksChat{key: tasksChatKey(row.ID), row: row, title: row.Title}})
	}

	for i := range t.groups {
		g := &t.groups[i]
		g.section = tasksSectionCount
		if g.named {
			g.order, g.chat.at = g.chat.row.At, g.chat.row.At
			if g.chat.row.NeedsPerson() {
				g.section = tasksNeeds
			} else if g.chat.row.Live && g.chat.row.Presence.State == session.PresenceWorking {
				g.section, g.chat.runs = tasksRunning, true
			}
		}
		for _, root := range g.roots {
			t.under(root, func(item tasksItem, _ int) {
				g.held++
				if item.section < g.section {
					g.section = item.section
				}
				if stamp := tasksEntryAt(item.entry, now); stamp.After(g.order) {
					g.order = stamp
				}
				if stamp := tasksEntryStamp(item, now); stamp.After(g.chat.at) {
					g.chat.at = stamp
				}
				if item.runs {
					g.chat.runs = true
				}
			})
		}
		if g.section == tasksSectionCount {
			g.section = tasksEarlier
		}
		sort.SliceStable(g.roots, func(a, b int) bool { return tasksNewer(g.roots[a], g.roots[b], now) })
		g.chat.kids = len(g.roots)
		for _, root := range g.roots {
			t.under(root, func(item tasksItem, _ int) { t.filed[tasksKeyOf(item.entry)] = g.section })
		}
	}
	// THE MOST URGENT CONVERSATION IS AT THE TOP AND THE REST READ NEWEST FIRST,
	// which is the same ranking the rows themselves have always had, applied one
	// level up.
	sort.SliceStable(t.groups, func(a, b int) bool {
		if t.groups[a].section != t.groups[b].section {
			return t.groups[a].section < t.groups[b].section
		}
		return t.groups[a].order.After(t.groups[b].order)
	})
	return t
}

// under walks one piece of work and everything beneath it, in draw order, with
// the depth each was found at.
//
// The tree has already had cycles removed by tasksTreeOf. Valid depth is not
// a reason to discard work; only the drawn indentation is constrained by width.
func (t tasksTree) under(item tasksItem, fn func(tasksItem, int)) { t.step(item, 0, fn) }

func (t tasksTree) step(item tasksItem, depth int, fn func(tasksItem, int)) {
	fn(item, depth)
	for _, kid := range t.kids[tasksKeyOf(item.entry)] {
		t.step(kid, depth+1, fn)
	}
}

// order is every row of the tree in the order the page draws it.
func (t tasksTree) order() []tasksItem {
	out := make([]tasksItem, 0, len(t.at))
	for _, g := range t.groups {
		for _, root := range g.roots {
			t.under(root, func(item tasksItem, _ int) { out = append(out, item) })
		}
	}
	return out
}

// in is the groups one section holds, in draw order.
func (t tasksTree) in(section tasksSection) []tasksGroup {
	out := make([]tasksGroup, 0, len(t.groups))
	for _, g := range t.groups {
		if g.section == section {
			out = append(out, g)
		}
	}
	return out
}

// held is how much WORK one section is holding, at every depth and behind every
// fold. It is the number the foot counts and the number the heading reconciles
// the drawn rows against ([tasksSectionHead]).
func (t tasksTree) held(section tasksSection) int {
	n := 0
	for _, g := range t.in(section) {
		n += g.held
	}
	return n
}

// shown is how many rows of WORK one section actually draws: what is not behind
// a shut conversation, and what is not behind a shut piece of work.
//
// IT COUNTS THE WORK AND NOT THE ROWS ON SCREEN — a conversation's own row is
// not a piece of work and is not counted — because the number it is compared
// against is the section's own hold ([tasksTree.held]).
func (t tasksTree) shown(r tasksReading, section tasksSection) int {
	n := 0
	for _, g := range t.in(section) {
		if g.named && !r.opens(g.chat.key) {
			continue
		}
		for _, root := range g.roots {
			n += t.rows(r, root)
		}
	}
	return n
}

func (t tasksTree) rows(r tasksReading, item tasksItem) int {
	n, key := 1, tasksKeyOf(item.entry)
	if !r.opens(key) {
		return n
	}
	for _, kid := range t.kids[key] {
		n += t.rows(r, kid)
	}
	return n
}

// column reports whether anything on this page has a shape to draw — a
// conversation over its work, or work under work — which is what decides that
// the family column is drawn at all.
func (t tasksTree) column() bool {
	if len(t.kids) > 0 {
		return true
	}
	for _, g := range t.groups {
		if g.named {
			return true
		}
	}
	return false
}

// tree is the shape of THIS reading, built from the rows it is holding at this
// instant — which is what makes a filtered page a tree of what survived rather
// than a tree with holes in it.
func (r tasksReading) tree() tasksTree {
	if r.shape != nil {
		return *r.shape
	}
	return tasksTreeOf(r.items, r.now, r.chats...)
}

// opens reports whether one foldable row is open: what a person set, and the
// row's own default where they have set nothing.
//
// A CONVERSATION OPENS AND A FAMILY DOES NOT, and the two defaults are opposite
// on purpose. The fold under a task exists to keep eight workers from burying
// six other things this machine did, and it opens on demand; the fold under a
// CONVERSATION is the page's own structure, and a page that opened with every
// conversation shut would be a list of chat titles with the work — the thing
// this place is for — hidden one keypress behind each of them.
//
// AND A QUERY OPENS EVERYTHING ([tasksReading.unfolded]).
func (r tasksReading) opens(key tasksKey) bool {
	if r.unfolded {
		return true
	}
	if open, set := r.open[key]; set {
		return open
	}
	return key.chat()
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
	case tasksLineChat:
		kin := pal.dim(line.kin)
		room -= ansi.StringWidth(line.kin)
		if room < 1 {
			room = 1
		}
		return lead + kin + tasksChatRow(line, room, r.now, pal)
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
// there. Prose and air are deliberately holes; every row of WORK is a stop,
// whoever owns it ([tasksItem.pick] states why).
func (r tasksReading) at(lines []tasksLine, i int) (tasksItem, bool) {
	if i < 0 || i >= len(lines) || lines[i].kind != tasksLineTask {
		return tasksItem{}, false
	}
	if !lines[i].item.pick() {
		return tasksItem{}, false
	}
	return lines[i].item, true
}

// chatAt is the CONVERSATION drawn on one painted line, and whether the cursor
// may stand there.
//
// EVERY CONVERSATION ROW IS A STOP, and what it opens is the chat itself — the
// door this page has always owed and never had. It is deliberately not
// [tasksReading.at]: everything that acts on a piece of work asks that one, and a
// conversation answering it would be a conversation offered `stop it`.
func (r tasksReading) chatAt(lines []tasksLine, i int) (tasksChat, bool) {
	if i < 0 || i >= len(lines) || lines[i].kind != tasksLineChat {
		return tasksChat{}, false
	}
	return lines[i].chat, true
}

// picks is whether the cursor may stand on one painted line AT ALL — a row of
// work, or the conversation standing over it. One answer for both kinds, so the
// walk, the hit map and the paint cannot disagree about which lines answer to a
// person.
func (r tasksReading) picks(lines []tasksLine, i int) bool {
	if _, ok := r.at(lines, i); ok {
		return true
	}
	_, ok := r.chatAt(lines, i)
	return ok
}

// nameAt is what the row on one line is CALLED, which is what the cursor is
// remembered by across a rebuild and what the verb strip is bound to. A
// conversation's name can never be a task's ([tasksChatKey] says how).
func (r tasksReading) nameAt(lines []tasksLine, i int) (tasksKey, bool) {
	if item, ok := r.at(lines, i); ok {
		return tasksKeyOf(item.entry), true
	}
	if chat, ok := r.chatAt(lines, i); ok {
		return chat.key, true
	}
	return tasksKey{}, false
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
	if r.whole == 0 && r.wholeChats == 0 {
		return tasksHeadWord + " · nothing" + since
	}
	// IT IS A HEADING AND NO LONGER A PARAGRAPH. It read `work aforge ran on its
	// own. 14 pieces of work since aug 2, $34.10 between them.` — three clauses,
	// the widest thing on the page, and the first thing every reader met. Two of
	// them were wrong to lead with: the sentence taught the machinery's own idea
	// of itself (`ran on its own`) instead of naming the place, and the SPEND is
	// not what a person opens this page to find out. What they want is what is
	// happening, which is four rows below and was being pushed down by prose.
	//
	// So: the place, the count, the window edge, in the punctuation every other
	// heading on this surface uses.
	said := tasksHeadWord + railSep + itoa(r.whole) + " " + plural("piece", r.whole) + " of work" + since
	if r.wholeChats > 0 {
		said = tasksHeadWord + railSep + itoa(r.wholeChats) + " " + plural("chat", r.wholeChats)
		if r.whole > 0 {
			said += railSep + itoa(r.whole) + " " + plural("subtask", r.whole)
		}
		said += since
	}
	// THE SPEND IS LAST AND IS THE FIRST THING A NARROW FRAME GIVES UP, because
	// the alternative is [placeHeadRow] cutting the line — and a figure with its
	// end cut off is a wrong number, which is the one thing rowfit.go's law
	// forbids anywhere on this surface. It has a whole place of its own.
	if r.wholeCost > 0 {
		if whole := said + railSep + dollars(r.wholeCost); width <= 0 || ansi.StringWidth(whole) <= width {
			return whole
		}
	}
	return said
}

// tasksHeadWord names the place, in the word the switcher's own tab spells
// (pages.go's [pageTasks]). One name for one place.
const tasksHeadWord = "tasks"

func tasksWindowStart(win session.UsageWindow) string {
	win = win.Normalized()
	if win.From.IsZero() {
		return ""
	}
	return strings.ToLower(win.From.Format("Jan 2"))
}

// section is the work one heading stands over, in draw order.
//
// IT IS WHERE THE ROW IS DRAWN AND NOT WHAT THE ROW IS. The two used to be one
// field and cannot be: a piece of work that finished this morning inside a
// conversation still waiting on a person is drawn under `your call`, with
// the conversation, because that is where a person will look for it — and it is
// still a finished piece of work, which is what its own mark and its own words
// say ([tasksItem.section] is that answer, and [tasksTree.filed] this one).
func (r tasksReading) section(want tasksSection) []tasksItem {
	tree := r.tree()
	items := make([]tasksItem, 0)
	for _, item := range r.items {
		if tree.filed[tasksKeyOf(item.entry)] == want {
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
//
// AND IT COUNTS THE WORK AND NEVER THE ROWS. It read `7 done today` over a
// section drawing four rows once, and the four were right — three of the seven
// were workers folded under a root — but the SEVEN is the number that belongs
// here: this line is what the PLACE is holding (place_tasks.go's
// [tasksPlace.note] says so), it is the per-section split of the head's own
// `10 pieces of work`, and 7 + 3 is that ten. A tally counting drawn rows would
// trade this disagreement for a larger one with the head, and would change under
// somebody opening a fold, which is a fact about the screen and not about the
// work. What reconciles the two is [tasksSectionHead], on the heading standing
// between them.
// The footer counts actual states. Conversation grouping never turns finished
// siblings into additional decisions for the person.
func (r tasksReading) tally() string {
	counts := [tasksSectionCount]int{}
	for _, item := range r.items {
		counts[item.section]++
	}
	var segs []string
	for _, section := range tasksSectionOrder {
		if n := counts[section]; n > 0 {
			segs = append(segs, itoa(n)+" "+tasksSectionWord(section))
		}
	}
	return strings.Join(segs, railSep)
}

// shown is how many rows of work the section these items were taken from
// actually draws.
//
// IT ASKS THE SAME TREE THE PAGE IS BUILT FROM ([tasksReading.tree]) AND READS
// THE SAME FOLD STATE ([tasksReading.opens]), so the count and the rows cannot
// be made to disagree by a change to either — which is the whole point of the
// clause it feeds. [TestTheSectionHeadCountsTheRowsItActuallyDraws] pins it
// against the rows [tasksReading.lay] really produces rather than against this
// arithmetic said twice.
func (r tasksReading) shown(items []tasksItem) int {
	if len(items) == 0 {
		return 0
	}
	tree := r.tree()
	return tree.shown(r, tree.filed[tasksKeyOf(items[0].entry)])
}

// tasksSectionHead is the heading over one section: its word, and — only where a
// fold is holding rows back — how much of what the foot counts is on the page.
//
// THE COUNT AND THE ROWS MEET HERE, which is the one place between them. The foot
// says `5 done today` because five pieces of work landed today
// ([tasksReading.tally]); the section draws two rows because three of the five
// are workers folded under a root. A person reads the foot, counts the rows and
// finds a defect — and the only thing on the frame reconciling the two used to be
// a clause in the middle of one row's tail.
//
// IT IS ON THE HEADING AND NOT ON THE FOOT, and that is a decision about width.
// The foot is one dim line holding every section at once, already 67 cells with
// five sections on it, and it is FITTED rather than wrapped — a clause added
// there came out as `… · 5 done today, 2 s…` at sixty columns, which is a figure
// with its end cut off, the one thing rowfit.go's law forbids anywhere on this
// surface. The heading has a whole row to itself at every width, and it stands
// directly over the rows it is counting, which is where the eye is when it
// counts them.
//
// AND IT IS ON THE HEADING AND NOT IN THE FAMILY COLUMN. That column is exactly
// two cells on every row of the page; widening it on the rows that fold would
// leave every glyph ragged, and widening it everywhere would take two cells off
// every name — which is exactly what the name-first fix was made to stop.
//
// NOTHING IS SAID WHERE NOTHING IS HELD BACK. Open the fold and the numbers agree
// by themselves, and the clause goes: the emptiness law applied to a fact that
// has stopped being one.
func tasksSectionHead(section tasksSection, held, shown int) string {
	word := tasksSectionWord(section)
	if shown >= held {
		return word
	}
	return word + railSep + itoa(shown) + " of " + itoa(held) + " shown"
}

func tasksSectionWord(section tasksSection) string {
	switch section {
	case tasksNeeds:
		// THE WORD IS THE READING'S. `needs your look` was this page's own name
		// for the tier internal/session calls `your call`, and a heading that
		// spells a state differently from the rows under it is two states
		// (tasktier.go's [tierYourCallWord]).
		return tierYourCallWord
	case tasksRunning:
		return taskSheetNowHead
	case tasksParked:
		// ONE WORD FOR ONE FACT, AND IT IS THE COLUMN'S. The word is read out of
		// the rail's own vocabulary rather than spelled a second time here — a
		// second spelling is how the two surfaces came to disagree.
		//
		// THE COLUMN SPLITS WHAT THIS PAGE DOES NOT, and `waiting` is the honest
		// word for both halves. The rail says `queued` for work with nothing in its
		// way but a slot and `waiting` for work blocked behind other work; this
		// section holds both, and every row in it is waiting for something.
		return railGroupWords[railParked]
	case tasksToday:
		// `done today` HELD FAILURES. Three rows under it, one of them `× install
		// the render toolchain · failed` — and `done` is the word this surface uses
		// for work that came off. What is actually true of every row here is that
		// it ENDED today, whatever it ended as, and each row still says which.
		return "finished today"
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

// tasksChatRow is one MAIN CONVERSATION on the page: what it is called, and —
// while its fold is shut — how much work is under it. It obeys the same law
// every row here does: the name keeps every cell it asks for before a fact gets
// one (rowfit.go, law 1).
//
// IT WEARS NO EXTRA GLYPH. The fold mark already identifies a conversation
// root, while the section and its children describe the work's state.
//
// AND IT NAMES ITS PROJECT ONCE. That fact used to be repeated on every row of
// work the conversation ran, twenty-odd cells a row, on the rows whose names
// were being cut to make room for it ([tasksFacts] drops it under here).
func tasksChatRow(line tasksLine, width int, now time.Time, pal palette) string {
	chat := line.chat
	facts := make([]rowField, 0, 3)
	if !line.open && chat.kids > 0 {
		// A MARK WITH NO COUNT is a mark a person has to open to find out whether
		// it was worth opening — the same sentence a shut family says.
		facts = append(facts, rowSay(tasksUnderWord(chat.kids)))
	}
	facts = append(facts, rowSay(sinceAt(chat.at, now)))
	project := strings.TrimSpace(chat.row.Project)
	if project != "" && project != chat.title {
		facts = append(facts, rowSay(project))
	}
	plan := rowPlan{primary: chat.title, fields: facts}
	label, tail := plan.fit(width)
	// THE HUE IS THE CLAIM, exactly as it is on a row of work ([tasksLabelInk]):
	// a conversation with a worker in it is live, and the record is dulled.
	ink := pal.muted
	if chat.runs {
		ink = pal.ink
	}
	if tail == "" {
		return fit(ink(label), width)
	}
	pad := width - ansi.StringWidth(label) - ansi.StringWidth(tail)
	if pad < 1 {
		pad = 1
	}
	return fit(ink(label)+strings.Repeat(" ", pad)+pal.dim(tail), width)
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
	room := width - ansi.StringWidth(lead)
	// THE SPEND ONLY EVER FILLS SPACE THE OTHER FACTS DID NOT WANT.
	//
	// Being LAST in the rank is not enough, and this row is where that showed.
	// The fitter degrades a fact before it drops it ([rowTail], law 2), so a
	// hundred-and-twenty-column frame cut `Annual is the default and the monthly
	// price stays visible beside it.` down to `2 files` — and then spent the cells
	// that bought on `$0.27`. The person lost the sentence saying what the work
	// came to and kept the one figure they did not open this page for, which is
	// the same inversion the reorder was meant to end, arriving through the
	// spelling ladder instead of through the order.
	//
	// So money is asked a question no other fact is asked: is every fact ahead of
	// it being said WHOLE? If any of them had to be shortened, the row has already
	// run out of room for what it is about, and the figure is not drawn at all.
	if !tasksSpendEarnsCells(tasksLabel(item.entry), facts, room) {
		facts = facts[:len(facts)-1]
	}
	plan := rowPlan{primary: tasksLabel(item.entry), fields: tasksFields(facts)}
	label, tail := plan.fit(room)
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
// THE ORDER IS THE ARGUMENT, AND THE TAIL IS SPENT FROM THE END, so this is also
// what a narrow frame gives up and in which order.
//
// A SHUT FOLD'S COUNT comes first because it is not a fact about the work at all
// — it is the row saying that three more rows are behind it, which is the
// difference between a page a person believes they have read and one they have
// not. The note is next because it is the only thing that can CORRECT the glyph —
// a claim of running with nobody behind it, or work happening in another window —
// and a row whose mark and whose words disagree is worse than a row missing a
// fact. The age follows, because every section here is named by time and the age
// is what a person scans down. Then WHERE THE WORK CAME FROM and WHAT IT IS
// DOING, which are the two facts a person acts on: whose it is, and whether it
// needs them.
//
// AND MONEY IS LAST. It used to sit fourth, in front of the owner and the state,
// and it was also the only fact on the row with an ink of its own — so `$3.10`
// was the loudest thing on a row whose state word had been cut off to make room
// for it. Nobody opens this page to find out what work cost; the spend has a
// place of its own, and this row's job is to say what the work is and what it
// needs.
//
// THE KIND IS GONE ENTIRELY. It spelled `adaptive` next to `18 of 40` on a
// running row — an implementation word competing with the progress a person was
// actually reading, describing a setting somebody chose before the work started
// that changes nothing they can do now. A capability that changes no action is
// not a fact worth a cell.
func tasksFacts(line tasksLine, now time.Time, pal palette) []tasksFact {
	item := line.item
	entry := item.entry
	source := strings.TrimSpace(item.row.Title)
	if source == "" {
		source = strings.TrimSpace(item.row.Project)
	}
	// A ROW DOES NOT REPEAT A CONVERSATION THAT IS ALREADY NAMED ABOVE IT. The
	// page drew the same title four times down a family, and once per row down a
	// whole conversation, spending twenty-odd cells a row to restate a fact the
	// row above had already stated — on the rows whose names were being cut to
	// make room for it. What is left saying it is a row this page could not place
	// under any conversation, which is the one row where it is news.
	if line.under {
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
		{field: rowSay(source)},
		{field: tasksMiddleField(entry)},
		{field: money, ink: placeMoneyInk(pal)},
	}
}

// tasksSpendEarnsCells reports whether the money fact ([tasksFacts] puts it
// last) has earned the cells it would take: every fact ahead of it fits at its
// LONGEST spelling in the room the name leaves behind.
//
// IT MEASURES AND DOES NOT DRAW. The answer is handed back to [tasksRow], which
// drops the fact before fitting, so there is still exactly one fitter deciding
// what a row says — this only decides what is offered to it.
func tasksSpendEarnsCells(name string, facts []tasksFact, room int) bool {
	if len(facts) == 0 {
		return true
	}
	money := facts[len(facts)-1].field
	if !money.known() {
		// Nothing to weigh: a row that cost nothing says nothing about cost.
		return true
	}
	plan := rowPlan{primary: name}
	left := room - ansi.StringWidth(plan.label(room)) - rowGutter
	if left <= 0 {
		return false
	}
	whole := rowAll(tasksFields(facts[:len(facts)-1]))
	return ansi.StringWidth(whole) <= left
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
// node nothing has started, a row whose window died, and work replayed out of an
// older checkpoint whose record genuinely never carried its landing time. A
// current record carries that fact and the surface reads it directly.
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
		// A CONVERSATION OF THIS TERMINAL IS NOT `another window`. It arrives
		// through the same presence reading, because that is the only authority for
		// work nothing has landed a row for yet — but the place it is in is this
		// one, and a row that said otherwise sent a person looking for a terminal
		// they are already sitting at ([taskOpenHereWord]).
		if item.here {
			return taskOpenHereWord + taskAwayNoteName(item.window)
		}
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
		// THE REASON IS THE ENGINE'S SENTENCE AND NOT THIS PAGE'S. A row whose
		// record carried no account of itself still says why it ended, in the one
		// spelling of that table there is ([session.TaskReasonOf]).
		return session.TaskReasonOf(entry.Ending, "")
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

// tasksGlyph is one roster row's cell and the hue it is said in, and IT IS THE
// COLUMN'S OWN TABLE ASKED, not a second one (tasktier.go).
//
// This page used to keep a vocabulary of its own — ○ for queued where the rail
// drew ◌, ◐ for working where the rail drew a spinner, ✕ where the rail drew ✗ —
// so a person who had learned the marks in the column beside their conversation
// had to learn them again one keypress away. There are five cells on this
// surface and this page draws the same five.
//
// A ROW NOTHING IS RUNNING is the one thing the reading cannot see and this page
// can: the record is a file and the file cannot correct itself, so a row that
// claims to be running with no window behind it wears [glyphIdle] rather than a
// state it never reached.
func tasksGlyph(item tasksItem, pal palette) (string, func(string) string) {
	status := item.status()
	if status.Presence == session.TaskPresenceIncomplete && status.Liveness == session.TaskLivenessUnclaimed {
		if pal.ascii {
			return glyphIdleASCII, pal.dim
		}
		return glyphIdle, pal.dim
	}
	glyph, ascii := tierGlyph(status)
	// AND `▸` IS ALREADY SPENT ON THIS PAGE. The tier's cell for work in flight is
	// the same character the family column shuts a fold with ([tasksFoldShut],
	// tokens.GlyphCollapsed), and a page that drew it in both columns would be
	// asking a person to tell "there is more under this" from "this is working" by
	// position alone. The rail escapes it because a live row there ANIMATES — the
	// spinner is `▸` moving (tasktier.go's [app.tierMark]) — and this page is
	// redrawn only when something changes, so it has no spinner to spend. It keeps
	// the half-filled circle, which is the one cell on this surface that means
	// nothing else.
	if glyph == glyphRunning {
		return tokens.GlyphWorking, tierInk(pal, status)
	}
	if pal.ascii {
		glyph = ascii
	}
	return glyph, tierInk(pal, status)
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
