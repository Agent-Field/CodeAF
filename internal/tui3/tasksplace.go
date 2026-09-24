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

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
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
	// looking for the window they were in. This page now files both cases under
	// `waiting`; the column one keypress away makes the finer distinction between
	// `queued` for a slot and `waiting` behind work ([railGroupWords]).
	tasksParked
	tasksToday
	tasksEarlier
	tasksCompleted
	tasksSectionCount
)

// tasksSectionOrder names the two conversation categories in display order.
// Individual tasks retain their own state for their marks and descriptions.
var tasksSectionOrder = [...]tasksSection{tasksRunning, tasksCompleted}

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
	// plan is the STORE ROW this piece of work is, when it is one of the run's
	// plan tasks rather than a row of the record (taskplan.go). It is nil on
	// every other row, and the row's own steps and dollars — the figures the
	// record has no room for while the work is still turning — come off it.
	plan *session.PlanTaskRow
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
	// plan and now are one store reading carried to the frame as plain data.
	plan []session.PlanTaskRow
	now  string
	// tilde is this machine's home directory, which is what tells a folder
	// somebody WORKS in from the one they stand in ([chatProjectWord]). It is
	// read once at boot and handed in like every other fact, because the reading
	// may not ask the surface a question.
	tilde string
}

// tasksMineRow is one of those rows: the work, and whether it is happening.
type tasksMineRow struct {
	entry session.TaskIndexEntry
	runs  bool
	live  *session.TaskStatus
}

// tasksChatView carries the same current title and activity flags Home reads.
type tasksChatView struct {
	title           string
	working, unread bool
}

// tasksReading is everything drawing and routing need from one world reading.
// The seen stamp travels with the snapshot across place adapters.
type tasksReading struct {
	chatViews  map[string]tasksChatView
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
	// empty — `work codeaf ran on its own. nothing.` across the top of a machine
	// that had run ten pieces of work. The news that nothing matches already has
	// its own home on the note line ([taskSheetFilterLine]).
	whole      int
	wholeCost  float64
	win        session.UsageWindow
	seen       time.Time
	now        time.Time
	summaryNow string
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
	// kinFloor is the least number of indent levels the layout draws whatever
	// the width says ([tasksKinRoom]). The page leaves it zero. The chat's rail
	// sets it ([tasksReading.planRows]): a run's tree is what the rail is for,
	// and at the rail's width the page's budget is one level, which drew a task
	// and the task under it at one indent, two siblings to the eye. One more
	// level costs a grandchild's row two cells and no other row anything.
	kinFloor int
	// folder is the project THIS WINDOW is standing in and tilde this machine's
	// home directory. They are the two facts [chatProjectWord] needs to decide
	// whether a conversation root also names its folder — a tag on every row of
	// the folder a person is already in tells one row from no other.
	folder string
	tilde  string
	// query is what has been typed into the filter box, for the control row to
	// draw. The FILTERING is done before the reading is handed over
	// ([tasksPlace.filtered]); what is kept here is the text itself, because the
	// box is a row of the list now and a row cannot ask the surface anything.
	query string
	// order is the column this page is sorted by and which way, handed in by the
	// place the way the folds are ([tasksReading.open] states the same law): a
	// snapshot is replaced whole every time a node lands, and an order that lived
	// on the reading would reset itself under somebody who had just chosen one.
	order tasksSort
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
func readTasks(world session.World, mine tasksMine, win session.UsageWindow, by tasksSort, seen, now time.Time) tasksReading {
	by = tasksSort{back: by.back}
	r := tasksReading{win: win.Normalized(), seen: seen, now: now, summaryNow: strings.TrimSpace(mine.now), tilde: mine.tilde, order: by}
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
	// THE RUN'S PLAN COMES AFTER THIS WINDOW'S OWN WORK, which is the order a
	// person reads them in: the work of this chat, then the store the run is
	// coordinating through. A row a node already carries is left out — a
	// plan-born node and its store task are one piece of work (taskplan.go's
	// [planRowShown]).
	if len(mine.plan) > 0 {
		names := planNamesOf(mine.rows, mine.row.ID)
		// THE PAGE IS READ ONCE INTO NAMES, and every row off it is handed the
		// reading: a task the store holds `pending` is held behind named work, and
		// the name is another row's title (taskplan.go's [planWaits]).
		kin := planKinOf(mine.plan)
		for _, task := range mine.plan {
			if planRowShown(names, task.Title) {
				continue
			}
			item := planItem(task, mine.row.ID, kin)
			item.row = tasksRowFor(world, mine, item.entry)
			put(tasksKeyOf(item.entry), item)
		}
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
			Status: task.Task.State, SessionID: task.SessionID, StartedAt: task.Task.StartedAt,
		}
		key := tasksKeyOf(entry)
		// THE FAMILY AND THE PROGRAM COME OFF THE INDEX ROW OF THE SAME WORK, where
		// there is one: presence carries neither, and a row that dropped the
		// program would open a page with no badge over a program's work
		// ([taskGuestNode]).
		entry.Parent, entry.Program = held[key].entry.Parent, held[key].entry.Program
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
	// AND THE WINDOW'S OWN FOLDER IS READ OFF ITS OWN CONVERSATION ROW, which is
	// where the two authorities have already met: this window knows what it is
	// called and the world scan knows which bucket that conversation belongs to,
	// and [tasksConversationRows] merges the pair. Asking [tasksMine.row]
	// directly gets the half that has no folder on it, which is how every row of
	// the folder a person is sitting in went on wearing its name.
	//
	// AND A WINDOW THE SCAN HAS NOT MET YET HAS NO FOLDER, which leaves every row
	// wearing its project tag until it has. That is the right way for this to
	// fail — a tag too many is a fact that is true, and a tag too few on the one
	// frame a person is looking at is a row they cannot place.
	for _, row := range r.chats {
		if row.ID != "" && row.ID == strings.TrimSpace(mine.row.ID) {
			r.folder = strings.TrimSpace(row.ProjectDir)
			break
		}
	}
	r.held = len(order) + len(r.chats)
	// The time window chooses conversations, never fragments of their trees.
	included := make(map[string]bool)
	for _, row := range r.chats {
		included[row.ID] = true
	}
	for _, key := range order {
		item := held[key]
		if r.win.Holds(tasksEntryAt(item.entry, now)) {
			included[tasksChatOf(item)] = true
		}
	}
	for _, key := range order {
		item := held[key]
		if !included[tasksChatOf(item)] {
			continue
		}
		item.section = tasksSectionOf(item, now)
		visible = append(visible, item)
	}
	// AND THE ORDER THE ROWS ARRIVE IN IS THE ORDER THE PAGE DRAWS THEM, settled
	// once here so that every reader of [tasksReading.items] — the tally, the
	// filter, the layout — walks one list in one order.
	tree := tasksTreeOf(visible, now, by, r.chats...)
	r.items = tree.order()
	r.shape = &tree
	r.wholeChats = len(tree.groups)
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
// reader of this already drops an empty title ([tasksChatRow],
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
	// tasksLineControl is the one line under the head: the filter box on the left
	// and the column labels on the right ([tasksControlRow]). It is a line of the
	// LAYOUT rather than chrome around it, because its labels stand over the
	// columns the rows under it draw — two answers to where the `cost` column is
	// is a click that sorts by the wrong thing.
	tasksLineControl
	// tasksLineChat is a MAIN CONVERSATION standing over the work it asked for.
	// It is deliberately its own kind and not a task line wearing a made-up
	// entry: a conversation has no id in the record, nothing can stop it and
	// nothing can be replayed from it, and a synthetic row would have arrived at
	// [tasksPlace.verbs] and at the card offering both.
	tasksLineChat
	// tasksLinePlanUnder is one of the rows a plan task spends under its own
	// title while a step is in flight (task.go's [planUnderRows]): the live
	// step's command, and the task's figures beneath it. It answers to the row
	// above it ([tasksLine.owner]) and is never a stop of its own, the way a
	// phone card's second line is not.
	tasksLinePlanUnder
)

type tasksLine struct {
	branches  []bool
	treeChild bool
	lastChild bool
	kind      tasksLineKind
	// text is the words on a prose line.
	text string
	// item is the work a task line and its tail are about.
	item tasksItem
	// chat is the conversation a [tasksLineChat] is about, and the zero value
	// everywhere else.
	chat tasksChat
	// under says THE CONVERSATION THIS ROW CAME OUT OF IS ALREADY NAMED ABOVE IT,
	// by the chat row it hangs under or by the piece of work it was cut out of.
	// The row then spends those cells on its own name instead ([tasksChatRow]).
	under bool
	// owner is the line index of the task this line belongs to, and -1 on prose
	// and air. It is what the cursor stands on, what a press resolves to, and
	// what stops a card's second line reading as a second row.
	owner int
	// planUnder is which row of a plan task's under-block this line is: 0 for
	// the live step's command and 1 for the telemetry under it. It is meaningful
	// only on a [tasksLinePlanUnder] and is what [tasksReading.paint] picks out
	// of the block [planUnderRows] builds.
	planUnder int
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
	// underKin is the family column of the lines that stand UNDER this row: the
	// same indent, with the stroke carried through where a sibling is to come.
	underKin string
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
	// rank is the row's answer to every sort key, FOLDED WITH EVERYTHING UNDER IT
	// ([tasksRank.fold]). A row with work beneath it shows the family's total,
	// because a shut fold stands in for what it is hiding and an open one is the
	// sum of the rows under it — which is what `$0.49` over `$0.47` and `$0.02`
	// reads as. It is carried on the LINE and not re-derived in the paint, for the
	// same reason the fold mark is: it is a fact about the layout.
	rank tasksRank
}

// tasksBareLead is the cell in front of every row of work: the place's one left
// edge ([placeLead]), where the row's state glyph stands. It carries no mark —
// the band says which row a hand is on.
const tasksBareLead = placeLead

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
	// THE RAIL ALWAYS AFFORDS A GRANDCHILD ITS STEP ([tasksReading.kinFloor]).
	if r.kinFloor > levels {
		levels = r.kinFloor
	}
	add := func(kind tasksLineKind, text string) {
		lines = append(lines, tasksLine{kind: kind, text: text, owner: -1})
	}
	add(tasksLineWord, r.headLine(width))
	phone := layoutTier(width) == tierPhone
	// THE CONTROL ROW IS THE FIRST ROW OF THE LIST AND NOT A SECOND HEAD. It
	// stands where the rows stand, because its labels are the rows' own columns
	// named — and it is absent on a phone, where there are no columns to label
	// and the box at the foot is the way in to the filter (taskphone.go).
	if !phone {
		add(tasksLineControl, "")
	}

	// work draws one piece of work and, while its fold is open, everything under
	// it — to whatever depth the record goes.
	//
	// A ROW WITH WORK UNDER IT WEARS THE FOLD AND NOT THE CONNECTOR. There are two
	// cells and three things they could say; the fold is a key a person can press,
	// the connector is furniture, and the indent in front of both has already said
	// where the row sits.
	var familyDone func(tasksItem) bool
	familyDone = func(item tasksItem) bool {
		if item.plan == nil || (item.plan.Status != "done" && item.plan.Status != "failed" && item.plan.Status != "cancelled") {
			return false
		}
		for _, kid := range tree.kids[tasksKeyOf(item.entry)] {
			if !familyDone(kid) {
				return false
			}
		}
		return true
	}
	// rails says, for every step of indent in front of a row, whether the
	// family line runs through it ([tasksKinTree]). A row's descendants and the
	// lines under it carry the rule of every ancestor that still has a sibling
	// to come, so a family's line is one unbroken stroke from its first row to
	// its last, however many live lines and grandchildren stand between them.
	var work func(item tasksItem, depth int, last, named, nested bool, rails, branches []bool)
	work = func(item tasksItem, depth int, last, named, nested bool, rails, branches []bool) {
		key := tasksKeyOf(item.entry)
		kids := tree.kids[key]
		if r.kinFloor > 0 && item.plan != nil && len(kids) > 0 {
			kept := make([]tasksItem, 0, len(kids))
			var folded *tasksItem
			done := 0
			for _, kid := range kids {
				if kid.plan != nil && kid.plan.Status == "done" {
					done++
					if folded == nil {
						copy := kid
						folded = &copy
					}
					continue
				}
				kept = append(kept, kid)
			}
			if folded != nil {
				folded.entry.Title, folded.entry.Label = "done", "done"
				folded.entry.Activity = itoa(done) + " done"
				kept = append(kept, *folded)
			}
			kids = kept
		}
		own := len(lines)
		line := tasksLine{kind: tasksLineTask, item: item, owner: own, under: named || nested,
			rank: tree.rank[key], branches: append([]bool(nil), branches...), treeChild: named || nested, lastChild: last}
		mark := ""
		switch {
		case r.kinFloor > 0 && len(kids) > 0 && item.plan != nil:
			line.folds, line.family, line.kids = familyDone(item), key, len(kids)
			line.open = !line.folds
			if line.folds {
				ending := "done"
				if item.plan.Status != "done" {
					ending = "failed"
				}
				line.item.entry.Activity = itoa(len(kids)) + " " + ending
			}
			mark = tasksKinPad
			if line.folds {
				mark = tasksFoldShut
			} else if nested {
				mark = tasksKinCont
				if last {
					mark = tasksKinLast
				}
			}
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
		line.kin = tasksKinTree(rails, depth, levels, mark)
		// THE ROW'S OWN COLUMN CARRIES ON UNDER IT while a sibling is still to
		// come: the lines under a row, and the rows under those, stand inside
		// the family's stroke and never break it.
		through := tasksKinPad
		if nested && !last {
			through = tasksKinRule
		}
		line.underKin = tasksKinTree(rails, depth, levels, through)
		lines = append(lines, line)
		// phone lane: a row becomes a two-line card a thumb goes into
		// (taskphone.go), and the second line belongs to the first.
		if phone && tasksCardTail(item, r.now) != "" {
			lines = append(lines, tasksLine{
				kind: tasksLineTail, item: item, owner: own,
				kin:      line.underKin,
				branches: line.branches, treeChild: line.treeChild, lastChild: line.lastChild,
			})
		}
		// A PLAN ROW WITH A STEP IN FLIGHT SPENDS ITS UNDER-BLOCK on the live step
		// and the task's own figures (task.go's [planUnderRows]), for every tier:
		// the rows hang under the TITLE, answer to the same press as the row, and
		// are capped at [railUnderRows]. Nothing is added for a plan row between
		// steps or one that has landed, which is the emptiness law on the block.
		for under := 0; under < planUnderCount(item.plan); under++ {
			lines = append(lines, tasksLine{
				kind: tasksLinePlanUnder, item: item, owner: own,
				kin:      line.underKin,
				branches: line.branches, treeChild: line.treeChild, lastChild: line.lastChild,
				planUnder: under,
			})
		}
		if !line.open {
			return
		}
		below := append(append([]bool(nil), rails...), nested && !last)
		for at, kid := range kids {
			work(kid, depth+1, at == len(kids)-1, named, true, below, append(append([]bool(nil), branches...), !last))
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
			// THE RAIL IS ALREADY INSIDE THE CONVERSATION, so its projection draws
			// no conversation row and spends no indent on one: the run's root
			// stands at the rail's own edge ([tasksReading.kinFloor]).
			named := g.named && r.kinFloor == 0
			if named {
				line := tasksLine{
					kind: tasksLineChat, chat: g.chat, owner: len(lines),
					folds: len(g.roots) > 0, family: g.chat.key, kids: g.chat.kids,
					open: r.opens(g.chat.key), rank: g.chat.rank,
				}
				if view, ok := r.chatViews[line.chat.row.Transcript]; ok {
					line.chat.title = view.title
					line.chat.working, line.chat.unread = view.working, view.unread
				}
				line.kin = ""
				lines = append(lines, line)
				if !line.open {
					continue
				}
				depth = 1
			}
			for at, root := range g.roots {
				work(root, depth, at == len(g.roots)-1, named, false, make([]bool, depth), nil)
			}
		}
	}
	return lines
}

// headLine is the page's opening sentence at one width: what is held, and the
// window control where the frame has room to draw it.
//
// THE WINDOW EDGE IS NAMED ONCE PER FRAME, and which half of the line names it
// depends on whether the control fits. A frame with room for `shift+← aug 12 –
// aug 25 →` has the span between the arrows, where SCREEN 3d puts it — the
// control and the reading at once — so the sentence drops its `since` clause; a
// frame too narrow for the control keeps the clause, because a head line that
// named neither would leave the four arrow keys moving something nothing on the
// frame reports.
//
// IT IS A FUNCTION BECAUSE THE HEAD IS NOT ALWAYS DRAWN IN THE LIST'S OWN
// WIDTH. Where the frame splits, the list is laid out in the cells left of the
// seam and this one sentence keeps the whole frame (taskpane.go says why), so
// the layout and the split have to spell it the one way.
func (r tasksReading) headLine(width int) string {
	head := r.head(width, false)
	if arrows, _ := placeWindowFits(width, head, r.win); !arrows {
		head = r.head(width, true)
	}
	return head
}

// headRow is that same sentence PAINTED, at a width the layout may not be using.
//
// It goes through [tasksReading.paint] rather than spelling the head arm a second
// time, so the split frame's head and the list's own head are one row built one
// way — the head carries the window control and a second painter would be a
// second chance for the control to be drawn where it is not bound.
func (r tasksReading) headRow(width int, pal palette) string {
	return r.paint([]tasksLine{{kind: tasksLineWord, text: r.headLine(width), owner: -1}}, 0, width, pal, false)
}

// Tree strokes retain the continuation of every ancestor that has later siblings.
func tasksTreeLead(line tasksLine, width int, pal palette) string {
	if !line.treeChild {
		return line.kin
	}
	var out strings.Builder
	out.WriteString(tasksKinStep)
	branches := line.branches
	_, _, nameCells := tasksColumns(width, tasksByAge)
	levels := max(0, min(tasksKinRoom(width), (nameCells-tasksNameFloor-8)/2))
	if len(branches) > levels {
		branches = branches[len(branches)-levels:]
	}
	for _, continues := range branches {
		if continues {
			out.WriteString(pal.glyph(tokens.GTreeVert) + " ")
		} else {
			out.WriteString(tasksKinStep)
		}
	}
	if line.kind == tasksLineTail || line.kind == tasksLinePlanUnder {
		if line.lastChild {
			out.WriteString("   ")
		} else {
			out.WriteString(pal.glyph(tokens.GTreeVert) + "  ")
		}
		return out.String()
	}
	id := tokens.GTreeBranch
	if line.lastChild {
		id = tokens.GTreeLast
	}
	out.WriteString(pal.glyph(id) + pal.glyph(tokens.GTreeDash) + " ")
	return out.String()
}

func tasksFoldMark(line tasksLine, pal palette) string {
	if !line.folds {
		return ""
	}
	if line.open {
		return pal.glyph(tokens.GExpanded)
	}
	return pal.glyph(tokens.GCollapsed)
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

// tasksKinTree is [tasksKin] with the family's stroke drawn through it: one
// step for every ancestor, a rule where that ancestor still has a sibling to
// come and a blank where it was the last of its family.
func tasksKinTree(rails []bool, depth, levels int, mark string) string {
	if depth > levels {
		depth = levels
	}
	if depth <= 0 {
		return mark
	}
	var lead strings.Builder
	for step := 0; step < depth; step++ {
		if step < len(rails) && rails[step] {
			lead.WriteString(tasksKinRule)
			continue
		}
		lead.WriteString(tasksKinStep)
	}
	return lead.String() + mark
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
	// tasksKinRule is the family's stroke passing a line that is not a row of
	// it: the live line under a task, or the rows of the task under that.
	tasksKinRule = "│ "
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
	// fold arithmetic is about: the rows immediately under it, each of which may
	// be holding a family behind its own fold.
	kids int
	// whole is how many pieces of work are under it ALTOGETHER, at every depth,
	// and it is what the row SAYS ([tasksChatStateField]).
	//
	// IT IS THE COUNT AND NOT THE KIDS BECAUSE IT IS A CLAIM ABOUT WHAT IS
	// HIDDEN, not about what one key press reveals. A root over one task with
	// four workers under it saying `1 done` beside a heading that says `5 folded
	// away` is two numbers about the same rows, and the smaller one is the one a
	// person reads as the size of the thing they are deciding whether to open.
	whole int
	// at is the newest thing in it THAT ANYBODY CAN DATE, and the zero time where
	// nothing can be. It is deliberately not the stamp the conversation SORTS by:
	// live work is dated `now` for ranking and says nothing at all about when
	// ([tasksEntryAt] and [tasksEntryStamp] hold the two halves apart).
	at time.Time
	// rank is every sort key's answer for this conversation AT ONCE, folded from
	// the work under it ([tasksRank.fold]): the newest age, the total spend, the
	// total files, the most urgent state. It is what the row is ORDERED by and
	// what its own key cell SAYS, which are one question.
	rank tasksRank
	// word is the state word the most urgent piece of work under it wears, and it
	// is what the conversation's `state` cell says after the count: `5 your call`,
	// `9 done`. A conversation has no state of its own and this is not one.
	word string
	// state is where that puts it, kept beside the word so the cell and the
	// section cannot be worked out two ways.
	state                     tasksSection
	working, unread, question bool
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
	// rank is every row's answer to every sort key, folded with everything under
	// it. It is computed once here because the comparison is asked at every level
	// and a walk per comparison is a walk per log n.
	rank map[tasksKey]tasksRank
	// sort is the order this shape was BUILT in, and it is kept so the reading can
	// tell a cached shape from one that has to be built again ([tasksReading.tree]).
	// A tree is a sorted thing; two sorts are two trees.
	sort tasksSort
}

// tasksSort is the whole of what a person has chosen about the order: which key,
// and which way. It is one value so that the place, the reading and the tree
// cannot hold two thirds of an answer between them.
type tasksSort struct {
	key  tasksSortKey
	back bool
}

// on returns this sort with a key pressed: the same key again REVERSES, and a
// different one starts that key the way round it is naturally read
// ([tasksSortKey.ahead]).
//
// IT IS ONE RULE FOR THE KEYBOARD AND THE POINTER ALIKE. `s` cycles to the next
// key and a click names one directly, and both land here, so a label clicked
// twice and a key cycled round to itself behave the same way.
func (s tasksSort) on(key tasksSortKey) tasksSort {
	if s.key == key {
		return tasksSort{key: key, back: !s.back}
	}
	return tasksSort{key: key}
}

// tasksTreeOf builds that shape, and it is the ONE place the page's structure is
// decided — the layout and the section headings both read this rather than each
// walking the rows their own way. The tally deliberately reads each piece of
// work's own state instead ([tasksReading.tally]).
func tasksTreeOf(items []tasksItem, now time.Time, order tasksSort, chats ...session.SessionRow) tasksTree {
	order = tasksSort{back: order.back}
	t := tasksTree{
		kids:  map[tasksKey][]tasksItem{},
		up:    make(map[tasksKey]tasksKey, len(items)),
		at:    make(map[tasksKey]tasksItem, len(items)),
		filed: make(map[tasksKey]tasksSection, len(items)),
		rank:  make(map[tasksKey]tasksRank, len(items)),
		sort:  order,
	}
	for i := range items {
		// A tree may be built directly by the rail as well as through readTasks.
		// File every row here so both entrances use the same family ordering.
		if items[i].plan != nil {
			items[i].section = tasksSectionOf(items[i], now)
		}
		t.at[tasksKeyOf(items[i].entry)] = items[i]
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
	// EVERY ROW'S RANK IS FOLDED BEFORE ANYTHING IS ORDERED, because a parent is
	// compared by what is under it and a walk per comparison would be a walk per
	// log n. The roots are done after the groups are formed, since a root's fold
	// reaches every generation below it.
	for _, item := range items {
		key := tasksKeyOf(item.entry)
		t.rank[key] = tasksRankOf(item, now)
	}
	var folded func(item tasksItem) tasksRank
	folded = func(item tasksItem) tasksRank {
		key := tasksKeyOf(item.entry)
		rank := tasksRankOf(item, now)
		for _, kid := range t.kids[key] {
			rank = rank.fold(folded(kid))
		}
		t.rank[key] = rank
		return rank
	}
	for _, root := range roots {
		folded(root)
	}
	// Every level stays newest first among siblings, without flattening the tree.
	for key, kids := range t.kids {

		sort.SliceStable(kids, func(i, j int) bool {
			return t.sort.key.less(t.rank[tasksKeyOf(kids[i].entry)], t.rank[tasksKeyOf(kids[j].entry)], t.sort.back)
		})
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
	// THE TITLELESS ROW IS NAMED BY HOME'S OWN LADDER, never by the raw id its
	// Title carries: the stem is the session's id, and an id is a machine's
	// word in the one column whose whole job is matching names. Home solved this
	// for its own lists with one spelling of the word ([listName]'s defect
	// note), and this place draws the same row, so it reads the name from there
	// rather than spelling it a second time here.
	//
	// THE TEST IS EQUALITY WITH THE ROW'S OWN ID, FOLDED, because a titleless
	// row's Title is never empty — [tasksConversationRows] fills it with
	// [homeName], whose title case has already raised the id's first letter, so
	// the composer sees `De9ea39e6f4c18c3` where the id is `de9ea39e6f4c18c3` and
	// a byte-exact check never fires. EqualFold is the one comparison that does.
	//
	// ONLY THE NAME CHANGES. The row keeps its place, its age stays empty
	// (lastUserAt is the zero time and the emptiness law draws nothing), and
	// [chatProjectWord] still sees the row, so a project that is not this name
	// does not echo the word back as a project tag beside it.
	tasksName := func(row session.SessionRow) string {
		if strings.EqualFold(strings.TrimSpace(row.Title), row.ID) || (row.Title == "" && row.Transcript != "") {
			return unnamedConversationWord
		}
		return homeName(row)
	}
	for _, root := range roots {
		id := tasksChatOf(root)
		row, named := names[id]
		if !named {
			row = root.row
			row.ID = id
			if row.Title == "" && row.Transcript == "" {
				row.Title = "unknown conversation"
				if id != "" {
					row.Title = "conversation " + id
				}
			}
		}
		if at, found := group[id]; found {
			t.groups[at].roots = append(t.groups[at].roots, root)
			continue
		}
		group[id] = len(t.groups)
		t.groups = append(t.groups, tasksGroup{
			named: true,
			chat:  tasksChat{key: tasksChatKey(id), row: row, title: tasksName(row)},
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
			chat: tasksChat{key: tasksChatKey(row.ID), row: row, title: tasksName(row)}})
	}

	for i := range t.groups {
		g := &t.groups[i]
		g.section = tasksCompleted
		g.chat.rank = tasksRankZero()
		if g.named {
			g.order, g.chat.at = g.chat.row.At, g.chat.row.At
			g.chat.rank.name = g.chat.title
			g.chat.rank.at = g.chat.row.At
			if g.chat.row.NeedsPerson() {
				g.section = tasksRunning
			} else if g.chat.row.Live && g.chat.row.Presence.State == session.PresenceWorking {
				g.section = tasksRunning
			}
		}
		// urgent is the piece of work whose state the conversation's own cell says
		// after its count. It is the SAME row that decides which section the whole
		// conversation stands under, so the heading a person reads and the word on
		// the row under it cannot disagree.
		var urgent *tasksItem
		for _, root := range g.roots {
			t.under(root, func(item tasksItem, _ int) {
				g.held++
				if item.runs || item.status().Attention || item.status().Presence == session.TaskPresenceWaiting {
					g.section = tasksRunning
				}
				if urgent == nil || item.section < urgent.section {
					held := item
					urgent = &held
				}
				// A PLAN ROW'S ACTIVITY IS ITS OWN LAST MOVE; every other row
				// keeps the stamp the record's list has always sorted by, so the
				// plan's order never re-files the shipped engine's rows.
				stamp := tasksEntryAt(item.entry, now)
				if item.plan != nil {
					stamp = item.entry.StartedAt
					if item.entry.EndedAt.After(stamp) {
						stamp = item.entry.EndedAt
					}
				}
				if stamp.After(g.order) {
					g.order = stamp
				}
				if stamp := tasksEntryStamp(item, now); stamp.After(g.chat.at) {
					g.chat.at = stamp
				}
			})
		}
		for _, root := range g.roots {
			g.chat.rank = g.chat.rank.fold(t.rank[tasksKeyOf(root.entry)])
		}
		g.chat.state = g.section
		g.chat.working = g.chat.row.Live && g.chat.row.Presence.State == session.PresenceWorking
		g.chat.question = g.chat.row.NeedsPerson()
		if urgent != nil && urgent.status().Attention {
			g.chat.question = true
		}
		if urgent != nil {
			g.chat.word = taskStateWord(urgent.entry, urgent.runs)
			if urgent.live != nil {
				g.chat.word = urgent.live.Word
			}
		}
		sort.SliceStable(g.roots, func(a, b int) bool {
			return t.sort.key.less(t.rank[tasksKeyOf(g.roots[a].entry)], t.rank[tasksKeyOf(g.roots[b].entry)], t.sort.back)
		})
		g.chat.kids, g.chat.whole = len(g.roots), g.held
		for _, root := range g.roots {
			t.under(root, func(item tasksItem, _ int) { t.filed[tasksKeyOf(item.entry)] = g.section })
		}
	}
	// THE MOST URGENT CONVERSATION IS AT THE TOP, AND INSIDE ONE SECTION THE
	// CONVERSATIONS ARE ORDERED BY THE SAME KEY THEIR OWN ROWS ARE — by their
	// AGGREGATE, which is the question a person sorting by cost is asking about a
	// conversation. The section outranks the key because the sections are what the
	// page is FOR: a heading a sort could reorder under would be a page whose own
	// argument moved when somebody clicked a column.
	sort.SliceStable(t.groups, func(a, b int) bool {
		if t.groups[a].section != t.groups[b].section {
			return t.groups[a].section < t.groups[b].section
		}
		// The rail default is activity, newest first. Explicit column sorts
		// retain the table comparator below.
		if t.sort == (tasksSort{}) && tasksPlanGroup(t.groups[a]) && tasksPlanGroup(t.groups[b]) {
			return t.groups[a].order.After(t.groups[b].order)
		}
		return t.sort.key.less(t.groups[a].chat.rank, t.groups[b].chat.rank, t.sort.back)
	})
	return t
}

// tasksPlanGroup answers whether a group is a belt run's family: its roots are
// plan rows. THE ACTIVITY ORDER IS THE PLAN'S AND NOT THE RECORD'S, so only two
// plan families are compared by it; a group of the shipped engine's rows keeps
// the comparator its own list has always used.
func tasksPlanGroup(g tasksGroup) bool {
	return len(g.roots) > 0 && g.roots[0].plan != nil
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

// held is how much WORK is FILED in one section, at every depth and behind every
// fold. The heading compares this with the same tree's drawn rows to say what a
// fold hides ([tasksSectionHead]); the foot counts a different partition.
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
// AND A SHAPE BUILT IN ANOTHER ORDER IS NOT THIS READING'S SHAPE. A tree is a
// sorted thing — the order decides which of two siblings comes first at every
// level — so the cached one is used only while it was built the way this reading
// is asking for. Pressing a sort key does not rebuild anything: it changes the
// answer this comparison gives, and the next frame notices.
func (r tasksReading) tree() tasksTree {
	if r.shape != nil && r.shape.sort == r.order {
		return *r.shape
	}
	return tasksTreeOf(r.items, r.now, r.order, r.chats...)
}

// opens reports whether one foldable row is open: what a person set, and the
// row's own default where they have set nothing.
//
// Every conversation and nested task opens by default so the full tree is
// visible on arrival. Explicit folds remain in force until the person opens them.
//
// AND A QUERY OPENS EVERYTHING ([tasksReading.unfolded]). A row that matched and
// is sitting behind a shut fold is a row the query appears not to have found, and
// the person's own folds come back when the query clears.
func (r tasksReading) opens(key tasksKey) bool {
	if r.unfolded {
		return true
	}
	if open, set := r.open[key]; set {
		return open
	}
	return true
}

// rows is the whole page painted with no cursor anywhere on it, which is what a
// reader of the reading alone sees.
func (r tasksReading) rows(width int, pal palette) []string {
	lines := r.lay(width)
	out := make([]string, len(lines))
	for i := range lines {
		out[i] = r.paint(lines, i, width, pal, false)
	}
	return out
}

// planRows draws only this reading's store-backed plan rows — the rail's own
// projection of the tree, through the same layout pass that owns it on the
// tasks page. ONE LINE PER TASK: the rail is a narrow column beside a
// conversation somebody is reading, and the page's own row at this width is a
// two-line card with the steps and the money under the title, so a run of four
// tasks would spend eleven of the rail's rows saying what four lines say
// ([planRailRow]). Page chrome is not part of the projection: the rail already
// owns its section label and controls, while the task rows remain one tree.
func (r tasksReading) planRows(width int, pal palette) []string {
	rows := r.planRailRows(width, pal)
	if len(rows) == 0 {
		return nil
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.text
	}
	return out
}

// planRailLine is one drawn line of a run on the rail and the stored task it
// belongs to, which is what makes the line a door ([app.openRailPlan]). Every
// line a task draws carries its id, the dots and the live line under the title
// included, so the whole of a task's block opens that task.
type planRailLine struct {
	text  string
	id    string
	title string
}

// railTree keeps the rail's running-first order without changing the Sessions page.
func (r tasksReading) railTree() tasksTree {
	tree := r.tree()
	// The page caches its age order, so the rail sorts copies of its slices.
	tree.groups = append([]tasksGroup(nil), tree.groups...)
	children := make(map[tasksKey][]tasksItem, len(tree.kids))
	for key, kids := range tree.kids {
		children[key] = append([]tasksItem(nil), kids...)
	}
	tree.kids = children
	for i := range tree.groups {
		tree.groups[i].roots = append([]tasksItem(nil), tree.groups[i].roots...)
	}
	position := make(map[tasksKey]int, len(r.items))
	for i, item := range r.items {
		position[tasksKeyOf(item.entry)] = i
	}
	priority := func(item tasksItem) int {
		if item.plan != nil && (item.plan.Status == "claimed" || item.plan.Status == "running") {
			return 0
		}
		if item.runs {
			return 1
		}
		return 2
	}
	for i := range tree.groups {
		roots := tree.groups[i].roots
		sort.SliceStable(roots, func(i, j int) bool {
			if a, b := priority(roots[i]), priority(roots[j]); a != b {
				return a < b
			}
			return tasksEntryStamp(roots[i], r.now).After(tasksEntryStamp(roots[j], r.now))
		})
	}
	for _, kids := range tree.kids {
		sort.SliceStable(kids, func(i, j int) bool {
			if a, b := priority(kids[i]) == 0, priority(kids[j]) == 0; a != b {
				return a
			}
			return position[tasksKeyOf(kids[i].entry)] < position[tasksKeyOf(kids[j].entry)]
		})
	}
	return tree
}

// planRailRows is [tasksReading.planRows] with each line's task beside it.
func (r tasksReading) planRailRows(width int, pal palette) []planRailLine {
	if width <= 0 {
		return nil
	}
	items := make([]tasksItem, 0, len(r.items))
	for _, item := range r.items {
		if item.plan != nil {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil
	}
	plan := r
	plan.items, plan.held, plan.whole = items, len(items), len(items)
	plan.chats, plan.shape = nil, nil
	// THE RAIL HAS NO FOLDS OF ITS OWN TO OPEN. On the page everything opens shut
	// ([tasksReading.opens]) and a person opens the conversation they want; the
	// rail is already inside that conversation, so its run is drawn open. A
	// family's finished rows still fold to their one line, which is the tree's
	// own rule and not a fold a person sets.
	plan.unfolded = true
	plan.kinFloor = planRailLevels
	tree := plan.railTree()
	plan.shape = &tree
	lines := plan.lay(width)
	out := make([]planRailLine, 0, len(lines))
	for i := range lines {
		if lines[i].kind != tasksLineTask || lines[i].item.plan == nil {
			continue
		}
		id, title := lines[i].item.plan.ID, strings.TrimSpace(lines[i].item.plan.Title)
		out = append(out, planRailLine{planRailRow(lines[i], width, pal, r.now), id, title})
		if dots := planRailDots(lines[i], width, pal); dots != "" {
			out = append(out, planRailLine{dots, id, title})
			if lines[i].item.plan.Parent == "" {
				for _, text := range planRailNow(lines[i], width, pal, r.summaryNow) {
					out = append(out, planRailLine{text, id, title})
				}
			}
		}
		if live := planRailLive(lines[i], width, pal); live != "" {
			out = append(out, planRailLine{live, id, title})
		}
	}
	return out
}

// paint draws ONE line of the layout, lit where the cursor or the pointer is on
// it. Every line it returns is at most width cells, at every width.
func (r tasksReading) paint(lines []tasksLine, i, width int, pal palette, lit bool) string {
	if i < 0 || i >= len(lines) || width <= 0 {
		return ""
	}
	line, lead := lines[i], tasksBareLead
	room := width - ansi.StringWidth(lead)
	if room < 1 {
		room = 1
	}
	switch line.kind {
	case tasksLineAir:
		return ""
	case tasksLineControl:
		line, _ := tasksControlRow(r.query, r.order, width, pal)
		return line
	case tasksLineWord:
		if i == 0 {
			// THE HEAD LINE IS THE WINDOW'S CONTROL TOO (SCREEN 3d), drawn by the
			// one head row standing and spend also draw — `shift+← aug 12 – aug 25
			// →`, the control and the reading at once. Before it, this place bound
			// all four arrow keys and drew nothing that named them, which is the
			// exact defect verbstrip.go's law was written against.
			return placeHeadRow(width, line.text, placeHeading(line.text, pal), r.win, pal)
		}
		return placeLead + placeHeading(fit(line.text, width-len(placeLead)), pal)
	case tasksLineChat:
		return tasksChatRow(line, width, r.now, r.folder, r.tilde, r.order, pal, lit)
	case tasksLineTail:
		indent := strings.Repeat(" ", taskSheetPhoneIndent)
		kin := tasksTreeLead(line, width, pal)
		tail := room - taskSheetPhoneIndent - ansi.StringWidth(kin)
		if tail < 1 {
			tail = 1
		}
		return lead + pal.dim(kin) + indent + placeFactInk(lit, pal)(fit(tasksCardTail(line.item, r.now), tail))
	case tasksLinePlanUnder:
		// THE LIVE STEP LINE AND THE FIGURES UNDER IT, hung where the phone
		// card's second line hangs so a plan row's block reads at the same
		// indent whichever tier is drawing it (task.go's [planUnderRows]).
		indent := strings.Repeat(" ", taskSheetPhoneIndent)
		block := lead + pal.dim(tasksTreeLead(line, width, pal)) + indent
		under := room - ansi.StringWidth(block)
		if under < 1 {
			under = 1
		}
		rows := planUnderRows(line.item, under, pal)
		if line.planUnder >= len(rows) {
			return ""
		}
		return block + rows[line.planUnder]
	}
	// THE FAMILY COLUMN IS CHOSEN IN THE LAYOUT AND PAINTED BY THE ROW. It is dim
	// everywhere — a connector is the surface's own furniture, not the row's
	// words. The TABLE spends it out of the name so its columns stand still
	// ([tasksTableRow]); the phone card has no columns and takes what is left.
	if layoutTier(width) == tierPhone {
		kin := tasksTreeLead(line, width, pal)
		card := room - ansi.StringWidth(kin) - tasksFoldCells - tasksColumnAir
		if card < 1 {
			card = 1
		}
		name := tasksCardHead(line.item, card, pal, lit)
		fold := tasksFoldMark(line, pal)
		return lead + pal.dim(kin) + name + " " + pal.dim(fold) + pad(card-ansi.StringWidth(name)) + pad(1-ansi.StringWidth(fold)) + pad(tasksColumnAir)
	}
	return tasksRow(line, width, r.now, r.order, pal, lit)
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
		return "nothing" + since
	}
	// IT IS A HEADING AND NO LONGER A PARAGRAPH. It read `work codeaf ran on its
	// own. 14 pieces of work since aug 2, $34.10 between them.` — three clauses,
	// the widest thing on the page, and the first thing every reader met. Two of
	// them were wrong to lead with: the sentence taught the machinery's own idea
	// of itself (`ran on its own`) instead of naming the place, and the SPEND is
	// not what a person opens this page to find out. What they want is what is
	// happening, which is four rows below and was being pushed down by prose.
	//
	// So: the place, the count, the window edge, in the punctuation every other
	// heading on this surface uses.
	said := itoa(r.whole) + " " + plural("piece", r.whole) + " of work" + since
	if r.wholeChats > 0 {
		said = itoa(r.wholeChats) + " " + plural("chat", r.wholeChats)
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
const sessionsWord = "sessions"
const tasksHeadWord = sessionsWord

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

// tasksTally is what each piece of work on the place IS, said in the same words
// that may also head its sections.
//
// THE EMPTINESS LAW HOLDS HERE TOO. A section with nothing in it is not counted
// as zero — it is not mentioned — and the line is empty when the page is.
//
// IT IS NOT DRAWN ANY MORE. It was the rule's legend on the tasks place —
// `9 finished today · 191 earlier` — until 2026-09-17, when it came off
// because the section headings a few rows up say the same numbers
// (place_tasks.go's [tasksPlace.note]). It is kept as the reading's own count
// of the work by state, which the headings are held against in the tests, so
// a heading and the partition it draws from cannot be made to disagree.
//
// AND IT COUNTS THE WORK AND NEVER THE ROWS. It read `7 done today` over a
// section drawing four rows once, and the four were right — three of the seven
// were workers folded under a root — but the SEVEN is the number that belongs
// here: it is the per-section split of the head's own `10 pieces of work`,
// and 7 + 3 is that ten. A tally counting drawn rows would trade this
// disagreement for a larger one with the head, and would change under somebody
// opening a fold, which is a fact about the screen and not about the work.
//
// THE SECTIONS AND THIS LINE ARE TWO DIFFERENT PARTITIONS OF THE SAME ROWS. The
// sections file a conversation and all its work under where the CONVERSATION
// stands ([tasksTreeOf], [tasksTree.filed]); this line counts each piece of work
// by its own state. A finished sibling can therefore stand under `waiting`, and
// this line can name a state for which the page draws no heading. The manual is
// where a person is told why, and this line keeps the states rather than taking
// the sections' filing because conversation grouping must never turn a
// conversation's finished siblings into more decisions for a person to make.
func (r tasksReading) tally() string {
	counts := [tasksSectionCount]int{}
	for _, item := range r.items {
		counts[item.section]++
	}
	var segs []string
	for _, section := range [...]tasksSection{tasksNeeds, tasksRunning, tasksParked, tasksToday, tasksEarlier} {
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
// clause it feeds. [TestTheSectionHeadCountsTheRowsItActuallyWithholds] pins it
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
// fold is holding rows back — how much of what is filed here is behind a fold.
//
// THE HEADING SAYS ONE THING ONLY: how much of the work filed in this section is
// behind a fold. The foot counts what each piece of work is, while the section
// files a whole conversation under where that conversation stands; neither
// number is a total for the other partition ([tasksReading.tally],
// [tasksTree.filed]).
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
// NOTHING IS SAID WHERE NOTHING IS HELD BACK. Open the fold and the clause goes:
// the emptiness law applied to a fact that has stopped being one.
func tasksSectionHead(section tasksSection, held, shown int) string {
	word := tasksSectionWord(section)
	if held-shown <= 0 {
		return word
	}
	return word + railSep + itoa(held-shown) + " folded away"
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
	case tasksCompleted:
		return "completed"
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

// tasksChatRow is one MAIN CONVERSATION on the page: what it is called, what
// opening it would put on the page, and its aggregate under the sorted column.
//
// It shares Home's conversation-state bullet, with its independent fold control
// beside it so activity and tree navigation do not compete for one mark.
//
// Project identity has its own column, leaving the title identical to Home.
func tasksChatRow(line tasksLine, width int, now time.Time, folder, tilde string, by tasksSort, pal palette, lit bool) string {
	chat := line.chat
	name := chat.title
	project := strings.TrimSpace(chat.row.Project)
	if project == "" {
		project = chat.row.ProjectDir
	}
	bullet := conversationBullet(pal, chat.working, chat.unread, chat.question, pal.glyph(tokens.GWorking))
	lead := tasksBareLead + pal.dim(tasksTreeLead(line, width, pal)) + bullet + " "
	return tasksTableRow(lead, ansi.StringWidth(lead), name, "",
		tasksChatStateField(chat), tasksKeyField(by.key, line.rank, now),
		tasksKeyInk(by.key, lit, pal), width, by.key, pal, lit, project, tasksFoldMark(line, pal))
}

// tasksRow is one piece of work on a wide frame, and it is A ROW OF A TABLE: the
// mark, the name in whatever the columns leave, then the two columns.
// taskstable.go holds the arithmetic and says why this is a table at all.
//
// IT TAKES THE LAID-OUT LINE AND NOT THE WORK ALONE, because three of the things
// on the row are facts about where it SITS rather than about what it did — the
// fold it is holding shut, the count behind that fold, and the aggregate it
// stands for — and the layout is where all three were decided
// ([tasksReading.lay]); the paint may not re-derive any of them.
//
// IT IS HANDED THE LIST'S WHOLE WIDTH AND SPENDS ITS OWN LEAD — the place's left
// edge, the family column and the mark — out of the name, so the columns are
// asked of one width for every row of the frame and the 90-cell floor means the
// list is ninety cells wide ([tasksTableRow]). The two-line phone lane is
// [tasksCardHead]'s ([tasksReading.paint] routes to it before this is called),
// so the tier question is settled before we are here.
func tasksRow(line tasksLine, width int, now time.Time, by tasksSort, pal palette, lit bool) string {
	item := line.item
	glyph, glyphInk := tasksGlyph(item, pal)
	lead := tasksBareLead + pal.dim(tasksTreeLead(line, width, pal)) + glyphInk(glyph) + " "
	cells := ansi.StringWidth(lead)
	state, second := tasksStateField(line), tasksKeyField(by.key, line.rank, now)
	// A PLAN ROW SHOWS ITS OWN TELEMETRY AND NOT THE SORT KEY'S COLUMN. The
	// store carries two figures the record has no room for while the work is
	// still turning — the steps a worker has taken and the dollars the task has
	// cost — so they take the two cells, each omitted when it is nothing
	// (taskplan.go).
	if item.plan != nil {
		state, second = planStateField(item), planSpendField(item)
	}
	label := tasksLabel(item.entry)
	if item.plan != nil && item.plan.Total > 0 {
		label += "  " + planProgress(*item.plan, width, pal)
	}
	return tasksTableRow(lead, cells, label, item.entry.Program,
		state, second,
		tasksKeyInk(by.key, lit, pal), width, by.key, pal, lit, "", tasksFoldMark(line, pal))
}

// tasksAgeField reads recorded activity rather than dating every live task now.
// Undated legacy records remain blank, as required by the emptiness law.
func tasksAgeField(item tasksItem, now time.Time) rowField {
	return rowSay(sinceAt(tasksEntryStamp(item, now), now))
}

// tasksEntryStamp is the latest recorded start or completion. Unlike
// tasksEntryAt, which keeps undated work inside the time window, it never
// invents an activity time from the current clock.
func tasksEntryStamp(item tasksItem, now time.Time) time.Time {
	if item.entry.EndedAt.After(item.entry.StartedAt) {
		return item.entry.EndedAt
	}
	return item.entry.StartedAt
}

// tasksCardHead is the first line of a phone card: the state and the name, in
// the same hue the wide row paints them.
func tasksCardHead(item tasksItem, width int, pal palette, lit bool) string {
	glyph, glyphInk := tasksGlyph(item, pal)
	lead := glyphInk(glyph) + " "
	room := width - ansi.StringWidth(lead)
	if room < 1 {
		room = 1
	}
	label := tasksLabel(item.entry)
	if item.plan != nil && item.plan.Total > 0 {
		label += "  " + planProgress(*item.plan, width, pal)
	}
	// A PROGRAM'S WORK WEARS ITS BADGE AFTER THE NAME, as its wide row does
	// ([tasksTableRow]); an ordinary row is fitted exactly as it always was.
	if program := strings.TrimSpace(item.entry.Program); program != "" {
		return lead + pal.programTitled(label, program, room, func(s string) string { return placeSubject(s, lit, pal) })
	}
	return lead + placeSubject(fit(label, room), lit, pal)
}

// tasksCardTail is the second line of a phone card: what the work came to, where
// it is happening, and how long ago — each dropped when it has nothing behind
// it, so a card with nothing to add stays one line.
func tasksCardTail(item tasksItem, now time.Time) string {
	var segs []string
	if middle := tasksMiddle(item); middle != "" {
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

// tasksNote is what one row says about WHERE the work is, and NOTHING ABOUT
// WHAT STATE IT IS IN.
//
// IT USED TO SAY BOTH, AND THAT IS HOW ONE ROW CAME TO SAY ITS STATE TWICE. A
// row that claims to be running with nobody behind it took this page's own copy
// of the word ([taskRecordStoppedWord], which is still what the room's orchestrate
// line spends and is no longer where a ROW gets it), while [tasksMiddle] put the
// row's own state word in front of the outcome
// a few cells later — two facts, one reading, and a page drawing `stopped ·
// stopped`. THE STATE WORD IS SAID ONCE, BY THE ONE FACT THAT IS ABOUT THE
// STATE ([tasksMiddle] reads [session.TaskStatus.RowWord], which is the engine's
// own join of the word and its reason). What is left here is the fact nothing
// else on the row can say: which window the work is happening in.
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

// tasksMiddle is WHAT CAME OF THE WORK: what it touched, and the row's state
// with the reason behind it — said ONCE, by the one mechanism that punctuates
// that pair.
//
// THE WORD AND ITS REASON ARE JOINED BY THE ENGINE AND NEVER HERE. This page
// used to compose the pair itself, three different ways: the raw outcome for a
// refused landing, [session.TaskReasonOf] for one that carried no account of
// itself, and [taskStateWord] with the outcome hung off it for everything else.
// The third was the defect on the screenshot. A stopped node's own record spells
// its outcome `stopped` (internal/session's taskGradeOutcome), so the row read
// `stopped · stopped` — a page saying one fact twice because two writers each
// thought they were the one saying it.
//
// [session.TaskStatus.RowWord] is that join, written down once: the word, the
// reason, and the rule that a reason merely restating the word is not said
// again. It is what the column beside a conversation already reads (tasktier.go
// and its [tierRowBody]), so the two surfaces say the same sentence about one
// row by construction rather than by agreement.
//
// AND THE ROW'S RAW OUTCOME IS NOT ON IT. A landing's own sentence is the
// record's, and it is read on the card one keypress away; hung on the row it was
// ninety cells of detail eating the name beside it, and on every ROW THE ENGINE
// GRADES it was the state word again in different clothes.
func tasksMiddle(item tasksItem) string {
	entry := item.entry
	if activity := strings.TrimSpace(entry.Activity); activity != "" {
		return activity
	}
	var parts []string
	// A PLAN ROW CARRIES WHAT THE STORE KNOWS: its steps and its money
	// (taskplan.go), the two figures a phone card otherwise has no room for.
	if item.plan != nil {
		if steps := planStepWords(item.plan.Steps); steps != "" {
			parts = append(parts, steps)
		}
		if usd := planSpendWord(item.plan.USD); usd != "" {
			parts = append(parts, usd)
		}
	}
	if entry.FilesChanged > 0 {
		parts = append(parts, itoa(entry.FilesChanged)+plural(" file", entry.FilesChanged))
	}
	// THE READING IS THE ROW'S OWN ([tasksItem.status]), which is the same one
	// the mark in front of it is drawn from — a row whose glyph and whose words
	// came from two readings is a row that can contradict itself.
	if word := item.status().RowWord(); word != "" {
		parts = append(parts, word)
	}
	return strings.Join(parts, rowSep)
}

// tasksGlyph is one roster row's cell and the hue it is said in, and IT IS THE
// COLUMN'S OWN TABLE ASKED, not a second one (tasktier.go).
//
// This page used to keep a vocabulary of its own — one circle for queued where
// the rail drew another, one cross where the rail drew a different one — so a
// person who had learned the marks in the column beside their conversation had
// to learn them again one keypress away. There is one vocabulary now
// (internal/tui2/tokens) and this page draws out of it.
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
	// AND THE FOLD MARK IS NO LONGER THE WORKING MARK. This page shuts a family
	// with `▸` ([tasksFoldShut], tokens.GlyphCollapsed), and while the tier drew
	// work in flight with the same character a person had to tell "there is more
	// under this" from "this is working" by position alone. The vocabulary's
	// working mark is the half-filled circle, which means nothing else on this
	// surface, and the rail draws it too — the two pages agree now.
	return tierGlyph(pal, status), tierInk(pal, status)
}

// step keeps the four time keys in one grammar shared with spend.
func (r tasksReading) step(win session.UsageWindow, key string) session.UsageWindow {
	return placeWindowStep(win, key)
}
