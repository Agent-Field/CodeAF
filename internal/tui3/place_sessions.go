package tui3

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// THE TASKS PLACE, app side: one thin state struct and the handful of things a
// place has to be able to do.
//
// The READING is next door in tasksplace.go and is pure — data, a window, a
// width and a palette in, rows out. This file is everything that needs the
// surface: taking the snapshot, holding the cursor and the scroll, answering the
// keyboard and the pointer, and handing the frame its rows. taskview.go keeps
// what is NOT the place — the record card's mode, the other-windows reading, and
// the one door the roster's column has onto here.
//
// The methods on [tasksPlace] are shaped for the `place` contract the switcher is
// settling into (docs/design/home-rethink/ARCHITECTURE.md): close, body, stops,
// enter, window, note, hint, changed. Where the rest of the surface already
// spells one of them under an older name, that name survives beside it as a
// one-line call — the seam the refactor lane deletes when the interface lands,
// and never a second copy of the logic.

// tasksPlace is the whole of the place's state. The zero value is closed.
//
// THE CURSOR AND THE OFFSET ARE BOTH LINE INDEXES into the layout the reading
// lays out ([tasksReading.lay]), which is what makes the paint and the pointer
// agree: one walk decides which screen line carries which piece of work, and the
// cursor is a line of that walk rather than a count of items the frame would
// have to re-derive.
type tasksPlace struct {
	// actionNote reports the last explicit row action, including write failures.
	actionNote string
	cursor     int
	top        int
	// opened is what a person has SET about this page's folds, keyed by the row's
	// own identity — a piece of work's (SessionID, ID) pair, or a conversation's
	// ([tasksChatKey], which cannot collide with the other).
	//
	// A KEY THAT IS NOT HERE IS THE ROW'S OWN DEFAULT AND NOT `SHUT`
	// ([tasksReading.opens]). Nil opens every conversation and nested family.
	// Shutting a conversation is REMEMBERED here as false
	// rather than deleted, which is the whole reason this map is read as
	// presence-and-value instead of as a set.
	opened map[tasksKey]bool
	// closed holds tasks put away during this search. A close must remove its
	// row immediately even though searches can recover older archived tasks.
	// Editing the query starts a new search and makes them discoverable again.
	closed map[tasksKey]bool
	// closedQuery is the search text [tasksPlace.closed] was put away under.
	// AN EDIT THAT CHANGES NOTHING IS NOT A NEW SEARCH: ctrl+k at the end of the
	// box, or backspace with the caret at its start, takes no rune, and a task
	// that came back on a keystroke that left the text as it was would read as a
	// close that did not hold.
	closedQuery string
	// query is the type-to-filter box, and it is the [editor] every other box on
	// this surface is rather than a string of its own: backspace, ctrl+u and
	// ctrl+w are edits a person's hands already know, and a second implementation
	// of them would be a second set of bugs in them.
	//
	// IT IS ON SCREEN NOW, as the first row of the list ([tasksControlRow]). It
	// used to be invisible, with a note line UNDER the rows saying back what had
	// been typed — a correction printed below the thing it was correcting.
	query editor
	// order is which column the list is sorted by and which way. It is the
	// PLACE's, beside the folds and for the same reason: the reading is replaced
	// whole every time a node lands, and an order kept there would reset itself
	// under somebody who had just chosen one.
	order tasksSort

	// detail is the row of the record this place is standing INSIDE, and
	// detailOn is what says it is (taskrecord.go). Together they are the place's
	// second MODE rather than a second page: the list is still underneath, esc
	// backs out to it, and the chord still closes the lot.
	//
	// IT IS A COPY AND NOT A POINTER. A card is a reading of one finished piece
	// of work rather than a live view of a row, and it outlives the reload that
	// replaces the snapshot whole whenever a node lands.
	detail   session.TaskIndexEntry
	detailOn bool
	// awayOwner is set while the card is standing over work ANOTHER WINDOW on
	// this project is running, and it is the whole of what that card's recovery
	// band is drawn from (taskrecord.go's [app.taskCardAwayRows]).
	//
	// IT IS HELD BESIDE THE ROW AND NOT INSIDE IT. [session.TaskIndexEntry] is
	// the RECORD's shape and no record row exists for work that has not landed —
	// the row the card was opened from was minted out of the other window's
	// presence file ([readTasks]), and the one fact that mints it, WHICH window,
	// has nowhere to live on a record entry.
	awayOwner tasksAwayOwner
	// detailTop is the card's own scroll. The report under it is as long as the
	// node made it, and the card is read rather than walked, so an offset is the
	// only thing that moves ([clampTop], expand.go).
	detailTop int
	// plan is the run's plan page standing over the list — the description, the
	// notes and the trajectory one plan task carries — and planOn says it is up.
	// It is the SAME latch the record card uses ([tasksPlace.detailOn]) and draws
	// in its place: enter over a plan row opens it and esc backs out one layer to
	// the list, exactly as the card does, because a second key for one door is a
	// second thing to learn ([app.taskSheetPlan]).
	plan          session.PlanTaskPage
	planOn        bool
	planBriefFull bool
	// planCalls says a program's page shows its raw calls instead of its
	// actions ([programCallsKey]); every page opens on the actions.
	planCalls bool
	planAt    int
	planBack  []session.PlanTaskPage
	// planNote is the note a person types on a plan task's page, and it is the
	// [editor] every other box on this surface is rather than a string of its own
	// (the filter is one, and so is the conversation's composer). Typing on the
	// page lands here and `enter` sends it through the store's note verb
	// ([app.taskPlanNoteSend]); the row's own keys are read over an EMPTY box, so
	// a note that starts with `p` or `x` is a letter the moment it has one.
	planNote editor
	// planStick is whether the page is pinned to its live edge — the bottom of
	// the trajectory, where the newest step arrives. It is the SAME mechanism the
	// room follows its own live edge with ([app.roomOffsetFor] resolves
	// [tasksPlace.detailTop] here, [app.taskPlanTopFor] is its twin at this
	// offset): a scroll up releases the pin and a scroll back to the bottom takes
	// it again. It is beside the offset and not inside it because the bottom moves
	// as the body grows, and a pinned page resolves to wherever the bottom now is
	// rather than to the number it was last drawn at.
	planStick bool
	// planFollowing is a follow read that has been asked and has not come back.
	// THE PAINT CLOCK ASKS AT MOST ONE AT A TIME: over a slow link a read per
	// frame would stand in the door line in front of the key a person presses
	// next, and every one of them would answer the same page.
	planFollowing bool
	// planPageAt is when the open page was last read, for any reason: the read
	// that opened it, a follow, the re-read after a note. The follow's beat is
	// counted from it ([app.taskPlanFollow]).
	planPageAt time.Time
	// planGen is the read of the run's rows this reading was filed from
	// ([app.planRowsGen]); a newer one re-files it ([tasksPlace.regroup]).
	planGen uint64
	// tail is the last thing the node said, read off its journal once when the
	// card opened, and tailRead says the read has happened — an empty tail with
	// tailRead false is a read still in flight, and one with tailRead true is a
	// journal that had nothing in it.
	//
	// tailKept says the journal is still on the disk of THE MACHINE THAT RAN THE
	// WORK, and tailUnread that the machine could not be asked at all. They are
	// three different sentences on the card and no two of them may be guessed
	// from the others: a journal somebody deleted, a journal that never held a
	// report, and a connection that did not answer look identical from an empty
	// string ([session.TaskRecord] says the same from the other end).
	tail       string
	tailRead   bool
	tailKept   bool
	tailUnread bool

	// paneTail is one report per row of the record, read off the loop when the
	// cursor settles and kept for as long as the place is open (taskpane.go).
	// A key that is PRESENT with an empty value is a row that was read and had
	// nothing to say, which is why this is read as presence-and-value.
	paneTail map[tasksKey]taskPaneTail
	// paneGen counts the cursor's moves, so a settle armed by an earlier one can
	// be told from the settle armed by the move that stopped
	// ([app.taskPaneFollow]).
	paneGen uint64

	// reading is the whole page: every authority's answer to "what has this
	// machine run", grouped once (tasksplace.go). EVERYTHING ON THE FRAME IS
	// DRAWN FROM IT — the body, the note line, the tally, the cursor, the
	// pointer and every key — so the place can never disagree with itself about
	// what it is holding.
	reading tasksReading
	// world and mine are the two halves the reading was taken from, held so a
	// time-window key can re-group WITHOUT a second walk of the disk.
	world session.World
	mine  tasksMine
	// awayAt stamps the reading of the OTHER WINDOWS that this grouping was
	// built from ([session.Elsewhere.Read]), and mineAt stamps THIS window's own
	// row-space at the same moment ([app.railStamp]). They are two comparisons,
	// and they are what [tasksPlace.regroup] hangs on.
	awayAt time.Time
	mineAt uint64
}

// tasksAwayOwner is the window a piece of work belongs to, when that window is
// not this one. The zero value is "this card is over an ordinary record row",
// which is every card but one.
type tasksAwayOwner struct {
	on bool
	// window is what the other window CALLS itself, and it is "" for a window
	// that has not settled on a name — which is a place with no name rather than
	// no place ([taskAwayCardWhere] keeps that half).
	window string
}

// taskSheetRows is the place's own page size: what pgup and pgdown move by, and
// nothing else. It is not a cap on anything — the list is as long as the machine
// has run, and the window scrolls it.
const taskSheetRows = 12

// ── opening and closing ─────────────────────────────────────────────────────

// showTaskPlace is THE ONE DOOR ONTO THIS PLACE, and it opens the place EMPTY.
//
// THERE USED TO BE TWO. `ctrl+.` and /history went through an `openTaskSheet`
// that took the reading, found nothing in it and refused — the chord fell
// through in silence, the command wrote one line — while `tab` and `alt+2` came
// through here and got the teaching prose. The argument for the split was that a
// command typed on purpose which answers with nothing reads as a command that
// broke; what the split actually produced is a machine on which the FIRST thing
// a new person does with the task page is the thing that does nothing, because
// on a fresh machine every door onto it was the refusing one.
//
// SCREEN 1f'S PREAMBLE SETTLES IT: an almost-empty place is the best teacher on
// the machine, and three sentences saying what tasks ARE ([tasksTeach]) is a
// better answer to /history-with-no-history than a line in a transcript. So
// every door opens the place, and the reading is taken ONCE on the way in,
// never per frame.
func (a *app) showTaskPlace() tea.Cmd {
	// THE OTHER WINDOWS ARE RE-READ ON THE WAY IN. A directory whose only live
	// work is in the window next door is a directory this page has something to
	// say about, and drawing out of a reading taken while the column was stowed
	// would open over work that is happening right now with no sign of it.
	a.refreshElsewhere()
	a.raiseTaskPlace(a.takeTaskReading())
	return a.loadTasks()
}

// raiseTaskPlace puts one taken reading on the frame.
//
// WHATEVER WAS STANDING IS ALREADY DOWN. The router closes the last place before
// it asks the next one to open (pages.go's [app.showPage]), so there is no
// exclusion to keep here — the one field that says which room is up cannot hold
// two answers.
func (a *app) raiseTaskPlace(sheet tasksPlace) {
	a.taskSheet = sheet
	a.taskSheet.cursor = a.tasksSettle(0)
	a.noticeEvent(eventTaskPageOpened)
	a.touch()
}

// takeTaskReading is THE ONE PLACE THE SNAPSHOT IS TAKEN. It walks the disk
// once, asks this window what it knows that no file does, and hands back a place
// that is open but not yet raised.
//
// It is a function rather than four lines inside [app.showTaskPlace] because
// three doors reach this page — the key, the command, and a card opened from
// home ([app.openTaskRecord]) — and a door that built the reading differently
// would be a second answer to what this machine has run.
func (a *app) takeTaskReading() tasksPlace {
	now := a.now()
	world := a.readWorld()
	mine := a.taskSheetMine()
	return tasksPlace{
		world:   world,
		mine:    mine,
		awayAt:  a.elsewhere().Read,
		mineAt:  a.railStamp,
		planGen: a.planRowsGen,
		reading: readTasks(world, mine, session.LastDays(now, taskSheetDays), tasksSort{},
			session.LastLookAt(a.looksRoot(), pageTasks.lookKey()), now),
	}
}

// regroup re-files the reading when the OTHER WINDOWS have been read again.
//
// THE DISK IS WALKED ONCE AND THE WINDOWS ARE A CACHE, and the difference is the
// whole reason this exists. What every project's file says is a snapshot: it is
// taken on the way in and a frame may never go back for it. What the windows
// next door have out is read on the paint clock every [elsewhereEvery] whether
// this place is up or not ([app.refreshElsewhere]), and it is the ONLY authority
// for work that has not landed — so a place that ignored a fresher one would go
// on drawing a task as `running` minutes after the window holding it closed, and
// would go on withholding the word [taskRecordStoppedWord] from the row that
// deserves it.
//
// AND THIS WINDOW'S OWN GRAPH IS THE SECOND AUTHORITY, for the same reason and
// with more force away from home. A task started while the page is up — by the
// model, or by somebody typing `/task` — has no row in any file yet; it exists
// only as a node on this surface's rail, which [app.taskSheetMine] folds in. Over
// a CONNECTION that is the only authority there is: nothing on the far end
// answers [session.Agent.Elsewhere], so the stamp above never moves and a page
// opened before the work started would go on drawing a roster without it until
// somebody closed and reopened the page.
//
// The common frame compares the held stamps and the main chat's own state.
// A main turn can finish without any worker or other-window notice.
//
// IT NEVER ASKS THE ENGINE. The run's rows are read off the loop and held on the
// surface ([app.refreshPlanRows]); this re-files what is held, and a read that
// has come back since the last filing is one more stamp to compare.
// planCanMove reports whether any row of a plan can change without the person
// touching it: work that is queued or running. A row that is done, incomplete
// or waiting on the person moves only by a verb, and a verb moves the stamp.
func planCanMove(rows []session.PlanTaskRow) bool {
	for _, row := range rows {
		switch planStateWord(row) {
		case "queued", "running":
			return true
		}
	}
	return false
}

func (p *tasksPlace) regroup(a *app) {
	at, stamp := a.elsewhere().Read, a.railStamp
	selfChanged := p.mine.row.ID != "" && (p.mine.row.Presence.State != a.taskSheetSelfState() || p.mine.row.Title != strings.TrimSpace(a.title))
	// A READING NOBODY EVER TOOK IS NOT A FRESH ONE. The chat's rail draws the
	// run's tree out of this same reading ([app.railRows]), in a conversation
	// whose person may never walk into the tasks place: the zero place holds the
	// zero stamps, the stamps of a quiet window are zero too, and the two read as
	// equal, so the rail stood empty until somebody opened the page once. So the
	// first ask files this window's own work under the present clock. It is the
	// light half of [app.takeTaskReading], the half with no walk of the record
	// in it: the record is the page's, and the page takes it on the way in.
	if p.reading.now.IsZero() {
		p.reading.now = a.now()
		p.reading.win = session.LastDays(p.reading.now, taskSheetDays)
	} else if at.Equal(p.awayAt) && stamp == p.mineAt && !selfChanged && p.planGen == a.planRowsGen {
		return
	}
	// THE CURSOR IS REMEMBERED BY WHAT IT IS ON, ACROSS THE REBUILD.
	//
	// [tasksPlace.cursor] is a LINE of a layout this replaces whole, and the
	// layout moves for reasons that have nothing to do with the person: a task
	// finishing can move its whole conversation into `completed`, and every
	// row below where it was shifts by one. The cursor stayed on the number and so
	// changed which piece of work it was on — silently, on a three-second beat,
	// between somebody reading a row and pressing enter on it. That is the
	// wrong-task failure this whole lane exists to end, arriving through the clock
	// instead of through a bad match.
	//
	// The name is the pair the record identifies a row by, which is the same name
	// the verb strip already binds to ([placeTasks.rowID]) and the same discipline
	// the roster (task.go's [railSpot]) and home (home.go's restores) have always
	// kept. A row that is genuinely gone falls back to the settle every other
	// rebuild uses, which parks on the nearest row rather than nowhere.
	was, held := p.rowAt(a, p.cursor)
	p.awayAt, p.mineAt, p.planGen = at, stamp, a.planRowsGen
	p.mine = a.taskSheetMine()
	p.reading = readTasks(p.world, p.mine, p.reading.win, p.order, p.reading.seen, p.reading.now)
	if !held {
		return
	}
	if line, found := p.lineOf(a, was); found {
		p.cursor = line
	}
}

// rowAt is the piece of work one line of THIS PLACE'S CURRENT reading is about,
// named by the pair that identifies it.
//
// IT LAYS THE HELD READING OUT DIRECTLY and never goes through
// [app.tasksFiltered], because that is the door [tasksPlace.regroup] is called
// FROM — asking it again from inside would be the freshness check calling itself.
func (p *tasksPlace) rowAt(a *app, line int) (tasksKey, bool) {
	r := p.filtered(a)
	return r.nameAt(r.lay(a.taskSheetListWidth()), line)
}

// lineOf is the line the work named by one key is on, in the reading this place
// is holding now.
func (p *tasksPlace) lineOf(a *app, want tasksKey) (int, bool) {
	r := p.filtered(a)
	lines := r.lay(a.taskSheetListWidth())
	for at := range lines {
		if name, ok := r.nameAt(lines, at); ok && name == want {
			return at, true
		}
	}
	return 0, false
}

// filtered is [app.tasksFiltered] WITHOUT the freshness check on the front of
// it: the same reading, the same folds, the same query, taken from what this
// place is holding at this instant.
func (p *tasksPlace) filtered(a *app) tasksReading {
	r := p.reading
	r.chatViews = make(map[string]tasksChatView)
	for _, tab := range a.tabList() {
		if tab.work {
			continue
		}
		working, unread := a.homeChatState(&homeCell{chatKey: tab.key})
		r.chatViews[tab.file] = tasksChatView{title: tab.word, working: working, unread: unread}
	}
	r.open = p.opened
	// Age stays the sort key; the header chooses its direction.
	r.order = tasksSort{back: p.order.back}
	needle := a.taskSheetFilter()
	// AND SO IS WHAT IS IN THE BOX, because the box is a ROW of the list now
	// ([tasksControlRow]) and a row cannot ask the surface anything. It is the
	// untrimmed text, so a person who has typed a space sees the caret move.
	r.query = p.query.String()
	if needle == "" || len(p.closed) > 0 {
		var kept []tasksItem
		for i, item := range r.items {
			if p.closed[tasksKeyOf(item.entry)] || (needle == "" && item.row.ArchivedTasks[item.entry.ID]) {
				if kept == nil {
					kept = make([]tasksItem, 0, len(r.items))
					kept = append(kept, r.items[:i]...)
				}
				continue
			}
			if kept != nil {
				kept = append(kept, item)
			}
		}
		if kept != nil {
			r.items = kept
			tree := tasksTreeOf(kept, r.now, r.order, r.chats...)
			tree.keepConversationStates(p.reading.tree())
			r.shape = &tree
		}
	}
	if needle == "" {
		return r
	}
	// A QUERY OPENS EVERY FOLD ON THE PAGE. A row that matched and is sitting
	// behind a shut fold is a row the query appears not to have found, and the
	// fold somebody left shut is not a decision they made about a list they had
	// not yet asked for.
	r.unfolded = true
	// AND A MATCH IS SHOWN WHERE IT SITS. The work ABOVE a hit — the piece of work
	// it was cut out of, and the conversation that asked for that — is kept even
	// though it matches nothing, because the row above a hit is the one thing on
	// the page that explains it. It used to be dropped, which promoted the hit to
	// a root and left a person reading a worker with no idea whose it was.
	hit := make(map[tasksKey]bool, len(r.items))
	found := make([]tasksKey, 0, len(r.items))
	for _, item := range r.items {
		if tasksMatches(item, needle) || session.TaskWordsMatch(r.chatViews[item.row.Transcript].title, needle) {
			key := tasksKeyOf(item.entry)
			hit[key] = true
			found = append(found, key)
		}
	}
	tree := r.tree()
	for _, key := range found {
		// The hit set ends the walk when a path has already been visited.
		for at := key; ; {
			up, ok := tree.up[at]
			if !ok || hit[up] {
				break
			}
			hit[up] = true
			at = up
		}
	}
	kept := make([]tasksItem, 0, len(hit))
	for _, item := range r.items {
		if hit[tasksKeyOf(item.entry)] {
			kept = append(kept, item)
		}
	}
	r.items = kept
	chats := make([]session.SessionRow, 0, len(r.chats))
	owners := make(map[string]bool)
	for _, item := range kept {
		owners[tasksChatOf(item)] = true
	}
	for _, row := range r.chats {
		if owners[row.ID] || session.TaskWordsMatch(row.Title+" "+row.Project+" "+r.chatViews[row.Transcript].title, needle) {
			chats = append(chats, row)
		}
	}
	r.chats = chats
	tree = tasksTreeOf(kept, r.now, r.order, chats...)
	tree.keepConversationStates(p.reading.tree())
	r.shape = &tree
	return r
}

// taskSheetDays is how far back the place opens on, and the four time keys walk
// from there ([tasksReading.step]).
const taskSheetDays = 14

// taskSheetMine is everything THIS WINDOW knows that the world scan cannot: the
// conversation it is sitting in, this project's index with the live graph merged
// over it, and what the other windows have out.
//
// EVERY ROW ARRIVES WITH ITS LIVENESS ALREADY SETTLED, by [app.recordRuns] —
// the one ladder this surface has. The reading is handed facts and never a
// callback, because a reading that could ask the surface a question is a reading
// that could ask it at paint time.
func (a *app) taskSheetMine() tasksMine {
	rows := a.taskSheetOwnRows()
	mine := tasksMine{row: a.taskSheetSelfRow(), tilde: a.tilde, rows: make([]tasksMineRow, 0, len(rows))}
	for _, entry := range rows {
		row := tasksMineRow{entry: entry, runs: a.recordRuns(&entry)}
		if node := a.taskSheetNodeFor(&entry); node != nil {
			status := a.taskStatus(node)
			row.live = &status
		}
		mine.rows = append(mine.rows, row)
	}
	// AND THE RUN'S PLAN, when this conversation seeded one. It is the store's
	// own read, narrowed to this chat, and it is the second authority for work
	// that has not landed: a task the run has added but not yet dispatched is on
	// this page and nowhere in the index (taskplan.go).
	//
	// THE ROWS ARE THE ONES THE SURFACE HOLDS, NEVER A READ MADE HERE. This runs
	// inside the frame, and over a connection the read is a call to another
	// process that can take the wire's whole deadline; it is asked for from
	// Update and folded in when it answers ([app.refreshPlanRows]).
	if rows, ok := a.heldPlanRows(); ok {
		mine.plan = rows
		mine.now = a.runSummaryNow
	}
	mine.away = a.taskSheetAwayRows()
	// AND WHICH OF THOSE WINDOWS ARE THIS ONE'S OWN CONVERSATIONS. The presence
	// files cannot say — a stowed conversation of this terminal writes the same
	// file as a terminal across the desk — and the difference is the difference
	// between `tab` and walking to another machine.
	mine.here = a.heldSessions()
	return mine
}

// taskSheetSelfRow is THIS conversation as a row of the world: what a piece of
// work this window ran is labelled with. It is the surface's own knowledge of
// itself rather than a lookup, because the scan cannot have read a journal this
// session has not finished writing.
func (a *app) taskSheetSelfRow() session.SessionRow {
	row := session.SessionRow{
		Title:      strings.TrimSpace(a.title),
		Transcript: strings.TrimSpace(a.file),
		Model:      a.model,
		Workspace:  a.workspace,
	}
	if file := row.Transcript; file != "" {
		row.ID = filepath.Base(filepath.Dir(file))
	}
	row.Open, row.Live = true, true
	row.Presence.State = a.taskSheetSelfState()
	return row
}

// A main chat changes state even when none of its workers sends a notice.
func (a *app) taskSheetSelfState() session.PresenceState {
	if needsPerson(a.agent) {
		return session.PresenceWaiting
	}
	if a.state == stateWorking {
		return session.PresenceWorking
	}
	return session.PresenceIdle
}

// taskSheetOwnRows is this project's record as THIS window holds it: the index
// snapshot the "@" list reads, with this session's live graph merged over the
// top.
//
// THE GRAPH IS CONVERTED HERE AND NOT LEFT TO THE INDEX. [session.Agent.TaskIndex]
// does merge the live rows in, but [app.comp].tasks is loaded asynchronously
// ([app.loadTasks]) and is EMPTY until that read lands — and an ordinary task
// writes NO row into the project's file until it finishes. So a page that read
// the index alone would show none of this window's own running work, which is
// the bug this whole reading exists to stop telling.
//
// A NODE THE INDEX ALREADY CARRIES IS LEFT TO THE INDEX. The file's row is the
// richer of the two — it has the outcome, the branch, the transcript and the
// files, none of which the graph keeps — and [session.Agent.TaskIndex] has
// already merged this session's live state over it, so it is no staler either.
// What the graph contributes is the work no row anywhere names yet.
// AND A WINDOW THAT CANNOT NAME ITSELF LEAVES ITS OWN NODES TO THE GRAPH. The
// pair is what stops one piece of work being drawn twice, and the pair needs a
// name on both halves: a window with no journal folder ([app.taskSheetSelfID] is
// "" for one) files its graph rows under no conversation while the index files
// the same nodes under the conversation that wrote them, and the two keys do not
// meet. What that drew was the worst possible shape — the same task under
// `running` AND a second copy under `earlier` reading `incomplete`, because the
// index copy could not be attributed and so could not be judged live. So an
// index row that this window cannot attribute and that one of its OWN nodes
// answers to by id and title is dropped in favour of the node: the graph is the
// one authority that is certainly ours, its row opens the right room, and its
// state is the live one. The cost is that a genuinely foreign row wearing the
// same number and the same words is not drawn while this window is nameless,
// which is a row missing from a history rather than a wrong page.
func (a *app) taskSheetOwnRows() []session.TaskIndexEntry {
	self := a.taskSheetSelfRow().ID
	rows := make([]session.TaskIndexEntry, 0, len(a.comp.tasks)+len(a.taskOrder))
	for i := range a.comp.tasks {
		if self == "" && a.taskNodeAnswersTo(&a.comp.tasks[i]) {
			continue
		}
		entry := a.currentTaskEntry(a.comp.tasks[i])
		if node := a.taskSheetNodeFor(&entry); node != nil && node.parent != "" {
			entry.Parent = node.parent
		}
		rows = append(rows, entry)
	}
	for _, id := range a.taskOrder {
		node := a.tasks[id]
		if node == nil || a.taskIndexHolds(node) {
			continue
		}
		label := strings.TrimSpace(node.label)
		if label == "" {
			label = node.title
		}
		rows = append(rows, session.TaskIndexEntry{
			ID:        strconv.FormatUint(node.id, 10),
			Parent:    node.parent,
			Label:     label,
			Title:     node.title,
			Status:    string(node.state),
			Kind:      taskNodeKind(node),
			Where:     node.where,
			Cost:      node.cost,
			Model:     node.model,
			SessionID: self,
			StartedAt: node.started,
			EndedAt:   taskNodeEnded(node),
		})
	}
	return rows
}

// taskIndexHolds reports whether the project's index already carries this node.
//
// IT ASKS THE PAGE'S OWN MEMBERSHIP RULE ([app.taskSheetNodeFor]) rather than
// the (SessionID, ID) pair the reading deduplicates on, and it has to: a session
// that has not written its journal yet has no folder to be named after, so its
// graph rows carry no conversation id for that pair to match on.
func (a *app) taskIndexHolds(node *taskNode) bool {
	for i := range a.comp.tasks {
		if a.taskSheetNodeFor(&a.comp.tasks[i]) == node {
			return true
		}
	}
	return false
}

// taskNodeAnswersTo reports that one of THIS session's own nodes wears the id
// and the words a record row does, WITHOUT asking who owns the row.
//
// IT IS DELIBERATELY THE UNATTRIBUTED MATCH, and it has exactly one caller
// ([app.taskSheetOwnRows]) for exactly the case where attribution is impossible.
// Everywhere else the owner vetoes first ([app.taskSheetNodeFor] says why, at
// length): the id alone names a different task in every conversation, so this is
// never a reason to OPEN anything. It is only ever a reason not to draw the same
// work twice.
func (a *app) taskNodeAnswersTo(entry *session.TaskIndexEntry) bool {
	node := a.tasks[taskSheetEntryID(entry.ID)]
	return node != nil && taskSheetSameWork(node.title, entry)
}

// taskNodeEnded is when a node of this session's graph LANDED, and the zero time
// while it is still going — which is what the index writes for a live row, and
// what the emptiness law asks for over a node whose record carried no clock.
// FOR A SETTLED NODE, THE RECORD'S LANDING TIME IS THE FIRST ANSWER. The live
// guard comes before that fact because a row that says it is running must never
// also claim it has landed. The fallback is computed from [taskNode.began],
// anchored off the age a running node's own update reported, only for a node
// this surface watched. Dating an older checkpoint that carried no stamp by when
// this WINDOW met it stamped work that finished twenty minutes ago at twelve
// minutes from now, which the page's date filter then read as tomorrow and
// dropped: the one task waiting on a person went missing from the tasks place
// while its shorter siblings sat on it saying `now`.
func taskNodeEnded(node *taskNode) time.Time {
	if node.state == session.TaskRunning || node.state == session.TaskQueued {
		return time.Time{}
	}
	if !node.ended.IsZero() {
		return node.ended
	}
	at := node.began
	if at.IsZero() && !node.restored {
		at = node.met
	}
	if at.IsZero() {
		return time.Time{}
	}
	return at.Add(node.elapsed)
}

// taskNodeKind is the shape of work a node is, in the index's own vocabulary. A
// node with an adaptive run behind it is one; everything else is left unsaid
// rather than named with a word the row would only repeat.
func taskNodeKind(node *taskNode) session.TaskKind {
	if strings.TrimSpace(node.run) != "" {
		return session.TaskKindAdaptive
	}
	return ""
}

// openTaskPage is what a COMMAND does with this place, and it is the router's
// own door and nothing else.
//
// IT IS ONE FUNCTION BECAUSE THERE ARE TWO COMMANDS. /history is the page's own
// name and a bare /task reaches it as well (taskcommand.go says why), and two
// copies of this line are two ways for one place to differ from itself.
//
// THE COMMAND AND THE TAB BAR NOW WALK INTO THE SAME ROOM. This used to read the
// machine first and refuse when nothing had run, on the argument that a command
// typed on purpose must answer rather than draw a page with a title and nothing
// under it. What that page draws with nothing under it is three sentences saying
// what tasks are ([tasksTeach]), which is an answer — and the extra directory
// walk it cost per keystroke goes with the refusal.
func (a *app) openTaskPage() tea.Cmd {
	return a.showPage(pageTasks)
}

// close writes the look stamp and drops the state. What a person saw is a fact
// about the moment they left, so it is written on the way out
// (placecounts.go's [app.leavePage]).
func (p *tasksPlace) close(a *app) {
	a.leavePage(pageTasks)
	*p = tasksPlace{}
}

// closeTaskSheet is the DOOR out of this place, and it goes through the router.
func (a *app) closeTaskSheet() {
	if a.at(pageTasks) {
		a.leavePlace()
	}
}

// ── the reading, as the place walks it ──────────────────────────────────────

// taskSheetFilter is what has been typed, trimmed. Empty is no filter, which is
// the same bargain [session.SearchTaskIndex] makes with an empty query.
func (a *app) taskSheetFilter() string {
	return strings.TrimSpace(a.taskSheet.query.String())
}

// taskSheetFiltering reports whether the place is being typed at.
func (a *app) taskSheetFiltering() bool { return a.taskSheetFilter() != "" }

// tasksFiltered is the reading with the query applied, and it never touches the
// snapshot the next keystroke starts from.
//
// EVERY SECTION IS FILTERED AT ONCE, because a person typing a word they half
// remember is asking about the whole of the machine's work — and a section a
// query empties is not drawn at all ([tasksReading.lay] states that half).
func (a *app) tasksFiltered() tasksReading {
	// IT IS THE ONE DOOR ONTO THE READING, so the freshness check hangs here:
	// every path that draws, counts, moves the cursor or answers a key comes
	// through this function, and a check on one of them would be the page fresh
	// in its body and stale in its foot.
	a.taskSheet.regroup(a)
	// THE FOLDS ARE THE PLACE'S AND THE ROWS ARE THE READING'S, joined in
	// [tasksPlace.filtered] at the one door onto both. A snapshot is replaced
	// whole every time a node lands ([tasksPlace.regroup]), so a fold kept on the
	// reading would shut itself under somebody who had just opened it.
	//
	// THE JOIN IS A METHOD ON THE PLACE so that the rebuild above can use it too:
	// it has to know which row the cursor was on BEFORE it replaces the reading,
	// and it cannot ask this function for it without asking the freshness check to
	// run inside itself.
	return a.taskSheet.filtered(a)
}

// tasksMatches asks the query of one row.
//
// ANOTHER WINDOW'S ROW IS ASKED ITS TITLE AND NOTHING ELSE. Its id is
// deliberately not matched: ids restart with every conversation (session's
// task_index.go says so on TaskIndexEntry.ID), so "7" typed here is somebody
// quoting a number they read in THIS window, and answering it with another
// window's seventh node would hand them the wrong task under the right number.
func tasksMatches(item tasksItem, needle string) bool {
	if item.away {
		return session.TaskWordsMatch(item.entry.Title+" "+item.row.Title+" "+item.row.Project, needle)
	}
	if session.TaskMatches(item.entry, needle) {
		return true
	}
	// The conversation and the project a row came out of are drawn ON the row,
	// so they are part of what a person can see and therefore part of what they
	// can search for.
	return session.TaskWordsMatch(item.row.Title+" "+item.row.Project, needle)
}

// stops is every line of the layout the cursor may stand on, in order. It is
// what ↑↓, pgup/pgdown, home and end all walk, so there is exactly one answer to
// "which rows answer to the keyboard" and the four keys cannot disagree.
func (p *tasksPlace) stops(a *app) []int {
	r := a.tasksFiltered()
	lines := r.lay(a.taskSheetListWidth())
	out := make([]int, 0, len(lines))
	for i := range lines {
		// A CONVERSATION IS A STOP LIKE ANY ROW OF WORK ([tasksReading.picks]).
		// It has a door of its own — the chat the work came out of — and a row a
		// person can see, can fold and cannot stand on is a list that reads as
		// broken (the argument [tasksItem.pick] makes at length).
		if r.picks(lines, i) {
			out = append(out, i)
		}
	}
	return out
}

// tasksSettle parks the cursor on the first stop at or after a line, and on the
// last stop of the place when there is none. A page whose every row is another
// window's has no stops at all and leaves the cursor at the top, where
// [app.taskSheetCurrent] answers false for it.
func (a *app) tasksSettle(from int) int {
	stops := a.taskSheet.stops(a)
	if len(stops) == 0 {
		return 0
	}
	for _, at := range stops {
		if at >= from {
			return at
		}
	}
	return stops[len(stops)-1]
}

// taskSheetCurrent is the work under the cursor, or false on a page with nothing
// the cursor may stand on.
//
// A CONVERSATION UNDER THE CURSOR ANSWERS FALSE HERE, and that is the whole
// reason the two questions are asked separately: every caller of this acts on a
// PIECE OF WORK — the room, the card, the mention, `stop it`, the strip's name
// for the row — and a conversation has none of those. It is asked for by name
// ([app.taskSheetChat]) or not at all.
func (a *app) taskSheetCurrent() (tasksItem, bool) {
	r := a.tasksFiltered()
	return r.at(r.lay(a.taskSheetListWidth()), a.taskSheet.cursor)
}

// taskSheetChat is the conversation under the cursor, and false over a row of
// work or a page with nothing under it.
func (a *app) taskSheetChat() (tasksChat, bool) {
	r := a.tasksFiltered()
	return r.chatAt(r.lay(a.taskSheetListWidth()), a.taskSheet.cursor)
}

// taskSheetFold opens or shuts the family under the cursor, and reports whether
// there was one to act on.
//
// `→` OPENS AND `←` SHUTS, which is the roster's own bargain with the same two
// keys (task.go), and it is asked BEFORE the verb strip on a row that heads a
// family (placekeys.go's `right` arm says why). A second `→` on a family already
// open falls through to the verbs, so a running root keeps its `stop it` — one
// key, two rungs, both of them drawn on the row.
func (a *app) taskSheetFold(open bool) bool {
	r := a.tasksFiltered()
	lines := r.lay(a.taskSheetListWidth())
	at := a.taskSheet.cursor
	if at < 0 || at >= len(lines) {
		return false
	}
	line := lines[at]
	if !line.folds || line.open == open {
		return false
	}
	if a.taskSheet.opened == nil {
		a.taskSheet.opened = map[tasksKey]bool{}
	}
	// WHAT WAS SET IS KEPT, INCLUDING `SHUT`. Deleting the key would put the row
	// back on its own default, which for a conversation is OPEN — so `←` on a
	// conversation would have redrawn it open on the next frame, a key that
	// visibly does nothing ([tasksPlace.opened] states the law).
	a.taskSheet.opened[line.family] = open
	// THE CURSOR STAYS ON THE ROW IT WAS ON. Shutting a fold above it would
	// otherwise slide the whole list up under a person's finger; the root is the
	// line the cursor is on and the line index of that root does not move, so
	// there is nothing to correct — but the window under it might now be past the
	// end, which [app.tasksSettle] is the one answer to.
	a.taskSheet.cursor = a.tasksSettle(a.taskSheet.cursor)
	a.touch()
	return true
}

// Reversing age preserves the selected row even when its line number changes.
func (a *app) taskSheetReverseAge() {
	was, held := a.taskSheet.rowAt(a, a.taskSheet.cursor)
	a.taskSheet.order = tasksSort{back: !a.taskSheet.order.back}
	if held {
		if line, found := a.taskSheet.lineOf(a, was); found {
			a.taskSheet.cursor = a.tasksSettle(line)
		}
	}
	a.taskSheet.top = 0
	a.touch()
}

// taskSheetTyped is what every edit of the filter ends with: the list has
// changed under the cursor, so the cursor goes back to the first row of it and
// the window with it. A cursor left at row forty of a list that now has three is
// a page a person types one letter into and finds empty.
func (a *app) taskSheetTyped() {
	if a.taskSheet.query.String() != a.taskSheet.closedQuery {
		a.taskSheet.closed = nil
	}
	a.taskSheet.top = 0
	a.taskSheet.cursor = a.tasksSettle(0)
}

// ── the keyboard ────────────────────────────────────────────────────────────

// taskSheetKeyPress is this place's whole claim on the keyboard: the one chord
// that OPENS it while it is closed, and every key while it is up.
//
// The guard while it is closed is the precedence law input.go states, restated
// rather than relied on because those keys are that file's: the door, the
// question the SESSION is blocked on, the modal overlays and the typed lists all
// outrank a page of work. ctrl+c is read above this and stays the door.
func (a *app) taskSheetKeyPress(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if !a.at(pageTasks) {
		if key != taskSheetKey {
			return nil, false
		}
		switch {
		case a.asking(), a.awaitingTask(), a.copy.on, a.rew.on, a.rewSheet.open, a.welcome.open,
			a.menu.open, a.comp.open, a.guarding(), a.stopping():
			return nil, false
		}
		// THE CHORD RAISES THE PLACE WHETHER OR NOT THERE IS ANYTHING IN IT
		// ([app.showTaskPlace] states the law and the reason). A chord that
		// answered with nothing was a chord a person could not tell they had
		// pressed, which is the defect this used to be written to avoid and is in
		// fact the one it caused: on a fresh machine there is never anything in
		// it. The record is re-read on the way in through the router, so a page
		// opened an hour into a session is not showing an hour-old file
		// (taskmention.go's [app.refreshTasks] keeps it fresh from there on).
		return a.showPage(pageTasks), true
	}

	defer a.touch()
	// THE ROUTER IS READ FIRST, AND IT IS ONE FUNCTION FOR EVERY PLACE
	// (placekeys.go). It claims the chords that mean the same thing wherever you
	// are standing and hands everything else straight back, so what follows keeps
	// its right of first refusal over its own keys.
	if cmd, took := a.placeKey(msg); took {
		return cmd, true
	}
	// THE CARD IS A MODE OF THIS PLACE AND IT TAKES THE KEYS FIRST. It is drawn
	// over the list, so every key while it is up belongs to it — including esc,
	// which backs out one layer to the list rather than closing the page
	// (taskrecord.go).
	if a.taskSheet.detailOn {
		if a.taskSheet.planOn {
			return a.taskPlanKey(msg), true
		}
		return a.taskCardKey(key), true
	}
	// THE CARET'S OWN CHORDS BEFORE THE PAGE'S KEYS (editkeys.go). `home` and
	// `end` walk this roster's rows rather than the filter's caret, which is why
	// they stay below with the rest of the walk; the word jumps and `ctrl+a`
	// mean nothing else on this page and belong to the box a person is typing
	// into.
	if editorMotion(&a.taskSheet.query, key) {
		return nil, true
	}
	// AND ctrl+z TAKES BACK WHAT WAS TYPED, in every box on this surface and not
	// only in the message one (editundo.go).
	if editorUndo(&a.taskSheet.query, key) {
		a.taskSheetTyped()
		return nil, true
	}
	if editorWordKill(&a.taskSheet.query, key) {
		a.taskSheetTyped()
		return nil, true
	}
	// A PLAN ROW'S OWN KEYS, the cancel and the hold, read over an EMPTY box the
	// way the roster reads its bare letters (stop.go's own law). It is ahead of
	// the switch because a plan row's `x` and `p` are verbs rather than filter
	// letters until the box has something in it ([app.taskSheetPlanKey]).
	if cmd, took := a.taskSheetPlanKey(key); took {
		return cmd, true
	}
	switch key {
	case "esc":
		// esc BACKS OUT ONE LAYER AT A TIME, which is the settings panel's own
		// layering ([app.sheetKey]): the filter first, the page second. A key that
		// closed the whole page from inside a filter would throw away the only
		// thing on screen the person typed, and leave them looking for the row they
		// had just narrowed to.
		if a.taskSheetFiltering() {
			a.taskSheet.query.reset()
			a.taskSheetTyped()
			return nil, true
		}
		a.leavePlace()
		return nil, true
	case taskSheetKey:
		// The chord that opened this is the chord that closes it — the roster's own
		// bargain with alt+t — and it closes it from inside a filter as well,
		// because a chord is not a layer a person is standing in.
		a.leavePlace()
		return nil, true
	case "up", "ctrl+p":
		a.taskSheetMove(-1)
	case "down", "ctrl+n":
		a.taskSheetMove(1)
	case "pgup":
		a.taskSheetMove(-taskSheetRows)
	case "pgdown":
		a.taskSheetMove(taskSheetRows)
	case "home":
		a.taskSheetMove(-len(a.taskSheet.stops(a)))
	case "end":
		a.taskSheetMove(len(a.taskSheet.stops(a)))
	case "enter":
		return a.taskSheetEnter(), true

	// ── the filter's own edits, in the settings panel's spelling ──────────────
	case "backspace":
		a.taskSheet.query.deleteBackward()
		a.taskSheetTyped()
	case "ctrl+u":
		a.taskSheet.query.killToStart()
		a.taskSheetTyped()
	case "ctrl+k":
		a.taskSheet.query.killToEnd()
		a.taskSheetTyped()
	case "ctrl+w":
		a.taskSheet.query.deleteWord()
		a.taskSheetTyped()

	default:
		// A DIGIT ANSWERS THE ROW UNDER THE CURSOR where the pane is drawing that
		// answer beside it, and is a character everywhere else. The pane's own verb
		// line is what decides, so no key is taken that nothing on the frame names
		// (taskpane.go's [app.taskPaneKey]).
		if cmd, took := a.taskPaneKey(key); took {
			return cmd, true
		}
		// EVERY PRINTABLE KEY IS THE FILTER, which is the one thing this page can
		// do with a letter: the frame is the page, so there is no draft underneath
		// for a keystroke to reach, and a record of four hundred tasks is found by
		// remembering a word of a title and by nothing else.
		//
		// THE SPACE IS TYPED HERE AND NOT ON THE SETTINGS PANEL, and the difference
		// is what the key already means: space ACTIVATES a row over there, and
		// nothing on this page answers it. "port the parser" is a thing a person
		// half-remembers as three words.
		if text := msg.Key().Text; text != "" {
			a.taskSheet.query.insert(text)
			a.taskSheetTyped()
		}
	}
	// EVERY OTHER KEY IS SWALLOWED. The page is the whole frame, so there is
	// nothing underneath for a key to mean anything to, and a chord that fell
	// through would act on a surface that is not on screen.
	//
	// THE PANE'S OWN FOLLOW IS NOT ARMED HERE. Half the arms above return before
	// this line and the router's chords never reach it at all, so the arming lives
	// one layer out, at the single door every key to this place comes through
	// ([placeTasks.key]).
	return nil, true
}

// taskSheetMove walks the stops, which is what steps the cursor over the head
// sentence, the blank air, the section words and another window's work in one
// rule rather than four. It clamps at both ends rather than wrapping, the way
// every other list on this surface walks ([moveCursor]).
func (a *app) taskSheetMove(delta int) {
	stops := a.taskSheet.stops(a)
	if len(stops) == 0 || delta == 0 {
		return
	}
	at := 0
	for i, stop := range stops {
		if stop <= a.taskSheet.cursor {
			at = i
		}
	}
	a.taskSheet.cursor = stops[min(max(at+delta, 0), len(stops)-1)]
}

// enter is the one activating key, and it opens the door that EXISTS for the row
// under it.
//
// A NODE THIS SESSION HOLDS HAS A ROOM, and the room is what every other list of
// work on this surface opens: the roster's enter, a strip chip, a spawn card and
// a `task 7` link all land in the same place ([app.railEnter]), and a second way
// to look at one task would be a second thing to learn.
//
// WORK ANOTHER CONVERSATION RAN HAS NO ROOM, and it never will: a room is a live
// lane onto a node this session's graph is holding, and that conversation closed.
// What it has instead is the CARD (taskrecord.go) — everything the record wrote
// down about that piece of work and the last thing the node itself said, drawn
// over this place with the list still underneath.
//
// WORK ANOTHER WINDOW IS RUNNING OPENS THE ONE PAGE THIS WINDOW CAN HONESTLY
// DRAW ABOUT IT: the card, standing over the same row, saying which window has
// the work, why there is no room here, and that the row lands in this project's
// record when that window finishes ([app.taskSheetAwayCard]). It used to answer
// nothing at all, because the cursor could not reach the row — and a person who
// aimed at a row that appeared as pressable as its nine neighbours and got
// silence had no way of telling a refusal from a surface that had broken.
func (p *tasksPlace) enter(a *app) tea.Cmd {
	// A CONVERSATION OPENS THE CONVERSATION, through the one door this surface
	// has onto a chat from a place that is not home ([app.openConversationRow],
	// place_search.go) — which is where the checks live that decide whether it is
	// the window you are sitting in, one this terminal is already holding, or a
	// folder that is not there any more. A second ladder here would be a second
	// answer to whether a conversation may be opened.
	if chat, ok := a.taskSheetChat(); ok {
		if strings.TrimSpace(chat.row.Transcript) == "" {
			// NOTHING IS INVENTED FOR A CONVERSATION WITH NO JOURNAL BEHIND IT. The
			// row is real — the record says this work came out of it — and the way in
			// is not, so the page says exactly that in the sentence the search results
			// already use for it.
			a.pageMsg = searchNoDoorWord
			return nil
		}
		return a.openConversationRow(chat.row)
	}
	item, ok := a.taskSheetCurrent()
	if !ok {
		return nil
	}
	// A PLAN ROW OPENS ITS OWN PAGE — the description, the notes and the
	// trajectory the store carries — through the sheet's own machinery and the
	// same key a record row opens its card with ([app.taskSheetPlan]).
	if item.plan != nil {
		return a.taskSheetPlan(item.plan.ID)
	}
	if item.away {
		// THE LADDER IS WALKED BEFORE THE CARD IS DRAWN, which is the whole of
		// what changed here. Work in another conversation is not one situation but
		// three — a conversation this terminal is holding, a conversation the
		// engine will hand this window a second view onto, and a window this
		// surface has no road to at all — and only the third of them has nothing
		// behind the key ([app.taskOwnerOf] ranks them).
		//
		// A RUNG THAT REFUSES FALLS THROUGH TO THE NEXT AND FINALLY TO THE CARD.
		// The person pressed a row and is owed a page either way; why it is the
		// card rather than the work is said on [app.pageMsg] beside it.
		owner := a.taskOwnerOf(item)
		switch owner.reach {
		case reachOpen:
			if cmd, went := a.openTaskInOwner(owner, item); went {
				return cmd
			}
		case reachAttach:
			if cmd, went := a.openOwnerRoom(owner, item); went {
				return cmd
			}
		}
		return a.taskSheetAwayCard(item)
	}
	entry := item.entry
	node := a.taskSheetNodeFor(&entry)
	if node == nil {
		return a.taskSheetInside(&entry)
	}
	a.closeTaskSheet()
	if node.run != "" {
		a.openOrchRoom(node.run, node.node)
	} else {
		a.openRoomFor(node.id, node.title)
	}
	return a.takeRoomPump()
}

func (a *app) taskSheetEnter() tea.Cmd { return a.taskSheet.enter(a) }

// taskSheetInside opens the card over one row of the record: the place stays up
// and the list stays underneath, which is the whole of what makes this a MODE
// rather than a fourth fullscreen surface (taskrecord.go says why).
//
// It reads the journal off the loop, which is the command it hands back.
func (a *app) taskSheetInside(entry *session.TaskIndexEntry) tea.Cmd {
	if entry == nil {
		return nil
	}
	a.taskSheet.detail, a.taskSheet.detailOn = *entry, true
	a.taskSheet.detailTop = 0
	a.taskSheet.tail, a.taskSheet.tailRead = "", false
	a.taskSheet.tailKept, a.taskSheet.tailUnread = false, false
	// AND THE OWNER IS CLEARED HERE, at the one door that opens a card, so a card
	// over an ordinary record row can never inherit the recovery band of the away
	// row somebody opened before it ([app.taskSheetAwayCard] sets it back after).
	a.taskSheet.awayOwner = tasksAwayOwner{}
	// AND THE PLAN LATCH IS CLEARED WITH IT, so the card can never be drawn in the
	// plan page's place ([app.taskSheetPlan] sets it for the one row that opens a
	// plan task's page).
	a.taskSheet.plan, a.taskSheet.planOn = session.PlanTaskPage{}, false
	a.taskSheet.planNote.reset()
	return a.readTaskTail(*entry)
}

// taskSheetAwayCard opens the card over work ANOTHER WINDOW on this project is
// running: the same card, over the same row, with the band that says where the
// work is and what to do about it.
//
// IT IS THE CARD AND NOT A FOURTH SURFACE. Every other row of this page that has
// no room behind it opens exactly this page (taskrecord.go's whole argument),
// and a separate refusal screen for one kind of row would be a second thing to
// learn for the case a person understands least.
//
// WHAT IT DOES NOT DO IS INVENT A RECORD. The row was minted out of the other
// window's presence file and carries a title, a state and its owner and nothing
// else, so the card draws exactly those and says why the rest is missing —
// rather than a page of blanks that reads as a card which failed to load.
func (a *app) taskSheetAwayCard(item tasksItem) tea.Cmd {
	entry := item.entry
	cmd := a.taskSheetInside(&entry)
	a.taskSheet.awayOwner = tasksAwayOwner{on: true, window: item.window}
	return cmd
}

// window is the four time keys, and it re-groups the CACHED world rather than
// starting a second walk of the disk — which is the law this place is built on
// (tasksplace.go's header) restated where it would be easiest to break.
func (p *tasksPlace) window(a *app, key string) bool {
	// A KEY IS BOUND ONLY WHERE THE HALF OF THE CONTROL NAMING IT IS DRAWN, which
	// is the one predicate standing and spend ask as well (placeprose.go's
	// [placeWindowFits]).
	width, _ := a.size()
	arrows, grain := placeWindowFits(width, p.reading.head(width, false), p.reading.win)
	if !arrows {
		return false
	}
	if (key == "shift+up" || key == "shift+down") && !grain {
		return false
	}
	before := p.reading.win
	next := p.reading.step(before, key)
	if next == before {
		return false
	}
	p.reading = readTasks(p.world, p.mine, next, p.order, p.reading.seen, a.now())
	p.top = 0
	p.cursor = a.tasksSettle(0)
	return true
}

// ── the pointer ─────────────────────────────────────────────────────────────

// taskSheetPress is a click inside the place: a row opens, and anything else
// does nothing.
//
// ONE PRESS AND NOT TWO, which is where this parts company with the settings
// panel ([app.sheetPress] selects first and answers second). That panel's rows
// CHANGE something, so a pointer passing over one must not be able to flip it;
// these rows open a page onto work, which is the gesture the roster's column has
// always answered on the first press.
func (a *app) taskSheetPress(x, y int) tea.Cmd {
	if a.taskSheet.detailOn {
		a.taskCardPress(x, y)
		return nil
	}
	// THE FRAME IS BUILT ONCE AND BOTH HALVES OF THE HIT MAP COME OUT OF IT. A
	// press used to lay the whole place out twice — once for the pane's verbs and
	// once for the list's rows — which on a filtered record is two tree builds for
	// one click (taskpane.go's [taskSheetHitsOf] and [taskPaneHitsOf] read the one
	// map from either side).
	width, height := a.size()
	painted, raw, _, _ := a.placeDraw(placeTasks{}, width, height)
	// THE PANE'S OWN VERB LINE ANSWERS FIRST, because it is the one thing on this
	// frame to the RIGHT of the seam that a press acts on and a click there must
	// never fall through to the row it is drawn beside (taskpane.go).
	if cmd, took := a.taskPanePress(x, y, raw); took {
		return cmd
	}
	hits := taskSheetHitsOf(raw)
	if y < 0 || y >= len(hits) {
		return nil
	}
	// On a compact frame the foot is an `esc close` band, so a press
	// on it is the way out (taskphone.go).
	if hits[y].kind == taskSheetHitBar {
		return a.taskSheetBarPress(x)
	}
	// The age header toggles direction using the same cells as the painter.
	if hits[y].kind == taskSheetHitControl {
		if tasksAgeHeaderHit(x, a.taskSheetListWidth()) {
			a.taskSheetReverseAge()
		}
		return nil
	}
	if hits[y].kind != taskSheetHitRow {
		return nil
	}
	// THE CURSOR MOVES FIRST, AND WHETHER THAT IS THE WHOLE GESTURE DEPENDS ON
	// WHETHER THERE IS A PREVIEW TO MOVE IT INTO. Where the frame splits, the
	// pane is what the first click buys — the row's record, without leaving the
	// list — and the second click on the SAME row is what opens it, which is the
	// pointer grammar home already keeps (THE POINTER PREVIEWS AND THE CURSOR
	// SELECTS, pages.go's [app.placeBodyHover]). Where there is no pane there is
	// nothing for a first click to show, so one click opens as it always did.
	moved := a.taskSheet.cursor != hits[y].index
	a.taskSheet.cursor = hits[y].index
	r := a.tasksFiltered()
	lines := r.lay(taskPaneList(width))
	if at := a.taskSheet.cursor; at >= 0 && at < len(lines) && lines[at].folds {
		line := lines[at]
		if y < len(painted) {
			// Read the last fold mark in the name cell so truncation and wide
			// title characters cannot move the click target away from its glyph.
			listWidth := a.taskSheetListWidth()
			_, _, nameCells := tasksColumns(listWidth, r.order.key)
			if line.kind == tasksLineTask && layoutTier(listWidth) == tierPhone {
				nameCells = listWidth
			}
			name := ansi.Cut(ansi.Strip(painted[y]), 0, nameCells)
			mark := tasksFoldMark(line, a.pal)
			if at := strings.LastIndex(name, mark); at >= 0 {
				foldX := ansi.StringWidth(name[:at])
				if x >= foldX && x < foldX+ansi.StringWidth(mark) {
					a.taskSheetFold(!line.open)
					return nil
				}
			}
		}
	}
	if moved && taskPaneOpen(width) {
		return a.taskPaneFollow()
	}
	return a.taskSheetEnter()
}

// taskSheetHover records which row the pointer is over, repainting only when the
// answer changed (hover.go's rule, applied to this place).
func (a *app) taskSheetHover(y int) tea.Cmd {
	if a.taskSheet.detailOn {
		// THE CARD LIGHTS ITS EDGES AND NOTHING ELSE. They are the way back and its
		// body is read, so a hover step over a paragraph would be the surface
		// offering a door that is not there — and an edge that stayed dark under the
		// hand was the other half of the same lie (hover.go's own law, and
		// taskrecord.go's [app.taskCardHitAt]).
		next := hoverAt{}
		if hit, ok := a.taskCardHitAt(y); ok {
			next = hoverAt{kind: hoverTaskCard, index: int(hit)}
		}
		if next != a.hot {
			a.hot = next
			a.touch()
		}
		return nil
	}
	width, height := a.size()
	// phone lane: no hover on glass, the rule home keeps at this tier
	// (homephone.go). A finger has no pointer to light a card with, and a tap
	// opens it in one gesture — a lit row would promise a hover a thumb cannot do.
	if layoutTier(width) == tierPhone {
		if a.hot != (hoverAt{}) {
			a.hot = hoverAt{}
			a.touch()
		}
		return nil
	}
	_, hits, _, _ := a.taskSheetFrame(width, height)
	next := hoverAt{}
	if y >= 0 && y < len(hits) && hits[y].kind == taskSheetHitRow {
		next = hoverAt{kind: hoverTaskSheet, index: hits[y].index}
	}
	moved := next.kind == hoverTaskSheet && a.selectPlaceRow(&a.taskSheet.cursor, next.index)
	if next == a.hot && !moved {
		return nil
	}
	a.hot = next
	a.touch()
	if moved {
		return a.taskPaneFollow()
	}
	return nil
}

// taskSheetScroll is the wheel: it walks the cursor rather than an offset of its
// own, which is the status sheet's bargain ([app.deckMove]) and the roster's
// ([app.railView] follows the focus). One place decides where the window is.
func (a *app) taskSheetScroll(delta int) {
	// INSIDE THE CARD THE WHEEL IS THE CARD'S. There is no cursor in there to
	// walk — the report is read down — so it moves the offset, which is the tool
	// detail's own bargain ([app.expandScroll]).
	if a.taskSheet.detailOn {
		a.taskCardScroll(delta)
		return
	}
	a.taskSheetMove(delta)
}

// ── the frame ───────────────────────────────────────────────────────────────

// taskSheetFrame is the whole screen while the place is open: exactly height
// rows, what each of them answers to the pointer, and where the caret sits.
//
// It is ONE function for [app.sheetFrame]'s reason: the frame draws these rows
// and the pointer resolves against them, and two answers to "where is the
// running section" is how a click opens the wrong task.
//
// The caret is handed back from the frame the place was drawn in, and while
// this place is up it belongs to THE FILTER'S CONTROL ROW rather than the
// foot's composer ([placeTasks.caretRow] — the box moved into the list when
// [placeTasks.body] did, and the caret moved with it).
func (a *app) taskSheetFrame(width, height int) ([]string, []taskSheetHit, int, int) {
	// THE CARD IS DRAWN INSTEAD OF THE LIST, not over the top of it. It is a mode
	// of this place and it takes the whole of the frame, so the rows below are not
	// built at all while it is up — and the hits it returns are its own, mapped
	// through here so that view.go plugs into one function either way
	// (taskrecord.go).
	//
	// It answers NO HITS OF ITS OWN. The card's rows are resolved against the
	// card's own frame ([app.taskCardPress]), and a list hit reported for a row
	// the list did not draw is exactly how a click opens the wrong task.
	lines, hits, caretX, caretY := a.placeDraw(placeTasks{}, width, height)
	// AND A SPLIT FRAME'S ROWS ANSWER FOR BOTH HALVES, so the list's half is
	// taken out here rather than by every reader of a hit (taskpane.go's
	// [taskSheetHitsOf]). A row of a split body wears one hit carrying two, and a
	// reader that asked for a bare list hit would have got nothing on every row —
	// which is a list where no click and no hover lands.
	return lines, taskSheetHitsOf(hits), caretX, caretY
}

// body is the place's own rows and the hit map the frame stores beside them.
//
// THE TAIL OF A CUT-OFF LIST FADES WITH DEPTH — NEVER STRIPES (depthfade.go).
// The fade is applied to the drawn rows and not to the blank padding under them:
// a list that stopped short of the window has nothing below it to point at. And
// it is never applied to the row the cursor or the pointer is on, which is why
// each row reports whether it came back BARE rather than being asked afterwards.
func (p *tasksPlace) body(a *app, width, room int) []placeRow {
	r := a.tasksFiltered()
	lines := r.lay(width)
	if len(lines) == 0 {
		// AN EMPTY PLACE DRAWS ITS HEADING AND ITS WHISPER, and no count beside
		// them — the emptiness law forbids the pair on one frame
		// ([tasksPlace.note] keeps the other half, placeprose.go's [placeWhisper]
		// the words).
		//
		// A QUERY THAT MATCHED NOTHING IS NOT AN EMPTY PLACE. There IS work here;
		// the words a person typed are hiding it, and teaching them what tasks are
		// would be answering a question nobody asked. What that frame says is on
		// the note line — `filter · zzz · nothing matches` — and the body stays
		// blank under it.
		if p.reading.held == 0 {
			return placeWhisperRows(pageTasks, width, room, a.pal)
		}
		rows := make([]placeRow, 0, room)
		for len(rows) < room {
			rows = append(rows, placeRow{})
		}
		return rows
	}
	p.cursor = a.tasksSettle(p.cursor)
	// THE CURSOR'S ROW GROWS A LINE WHERE THERE IS NO PANE, so the window has one
	// row less to put the list in ([tasksReasonShowing] says why it asks the whole
	// frame). It is reserved BEFORE the window is placed rather than squeezed in
	// after: a line added afterwards would push the last row of the list off the
	// frame, and the row it pushes off is sometimes the cursor's own.
	grown, grownIndent := "", ""
	if item, ok := r.at(lines, p.cursor); ok && tasksReasonShowing(a) {
		// AND IT STARTS WHERE THE ROW'S NAME STARTS. It is the row's own second
		// line, not a line of the page: drawn flush left under a worker five
		// levels down a family it reads as a peer of the section heading, which
		// is the one thing about it a person has to get right at a glance.
		grownIndent = tasksBareLead + strings.Repeat(" ", ansi.StringWidth(lines[p.cursor].kin)+taskSheetPhoneIndent)
		grown = tasksReasonLine(item, width-ansi.StringWidth(grownIndent), a.pal)
	}
	// The grown line costs the LIST a row and costs the FRAME nothing: the window
	// holds one line less of the record, and the row it gives up is spent on the
	// line under the cursor. Counting it against one budget twice returned a
	// short body and left a blank line under the foot.
	listRoom := room
	if grown != "" && listRoom > 1 {
		listRoom--
	}
	p.top = tasksTop(lines, p.cursor, p.top, listRoom)

	drawn := 0
	rows := make([]placeRow, 0, room)
	// bare records, per drawn line, whether that line is wearing neither the
	// cursor's band nor the pointer's — which is the one thing the depth fade
	// needs to know and the one thing it cannot ask a finished string. It is
	// reported by the row builder rather than recomputed here, because a second
	// answer to "is this row the cursor's" is how a list ends up fading the row a
	// person is standing on.
	var bare []bool
	more := false
	for at := p.top; at < len(lines); at++ {
		if drawn >= listRoom {
			more = true
			break
		}
		drawn++
		hit, lit := taskSheetHit{}, false
		if lines[at].kind == tasksLineControl {
			hit = taskSheetHit{kind: taskSheetHitControl}
		}
		if owner := lines[at].owner; owner >= 0 {
			if r.picks(lines, owner) {
				hit = taskSheetHit{kind: taskSheetHitRow, index: owner}
				// THE KEYBOARD AND THE POINTER ARE ONE FACT ARRIVED AT BY TWO HANDS,
				// and the row says it the one way every place does: the band, and
				// the subject bold inside it — no accent mark in the lead, which was
				// a second accent on a screen whose one accent is the live thing
				// (placeprose.go's THE FIVE-LEVEL SCALE).
				lit = owner == p.cursor
			}
		}
		text := r.paint(lines, at, width, a.pal, lit)
		if lit {
			text = placeBand(text, width, a.pal)
		}
		rows = append(rows, placeRow{text: text, hit: hit})
		bare = append(bare, !lit)
		if grown == "" || at != p.cursor {
			continue
		}
		// AND THE GROWN LINE BELONGS TO THE ROW ABOVE IT. It answers to the same
		// press and it is never faded, because it is part of the row the cursor is
		// standing on rather than a row of its own.
		rows = append(rows, placeRow{text: grownIndent + grown, hit: hit})
		bare = append(bare, false)
	}
	for i := range bare {
		if !bare[i] {
			continue
		}
		if stop := tailStop(i, len(bare), more); stop >= 0 {
			rows[i].text = a.pal.fadeRow(rows[i].text, stop)
		}
	}
	for len(rows) < room {
		rows = append(rows, placeRow{})
	}
	return rows
}

// tasksTop follows the cursor with the window.
//
// IT SCROLLS BY LINES AND NOT BY ROWS, which is what keeps a phone card whole: a
// card is two lines, so a window that counted rows would believe six cards fit
// in six lines and leave the cursor's own card clipped at the fold. The whole of
// the cursor's block — its line and any continuation under it — is what has to
// be on screen.
//
// AND A SECTION'S WORD SCROLLS IN WITH THE FIRST ROW OF ITS SECTION. A cursor
// that has just stepped onto that row would otherwise sit under nothing, which
// is a row a person cannot tell the section of.
func tasksTop(lines []tasksLine, cursor, top, room int) int {
	if room < 1 || len(lines) == 0 {
		return 0
	}
	end := cursor
	for end+1 < len(lines) && (lines[end+1].kind == tasksLineTail || lines[end+1].kind == tasksLinePlanUnder) {
		end++
	}
	if top > cursor {
		top = cursor
	}
	if end-top+1 > room {
		top = end - room + 1
	}
	if top > cursor {
		top = cursor
	}
	if cursor > 0 && top == cursor && lines[cursor-1].kind == tasksLineWord {
		top = cursor - 1
	}
	if top < 0 {
		top = 0
	}
	return top
}

// note is the one line the place says on its rule, and it says ONE THING: that
// the query has emptied the page.
//
// THE COUNT IS OFF THE RULE. It used to read `9 finished today · 191 earlier`
// there, and the body already says every one of those numbers on its section
// headings — the same partition, a few rows up, on a page a person is looking
// at (the owner's ruling, 2026-09-17). A figure said twice on one frame is the
// defect the model's colon suffix made once (effortchip.go).
//
// THE FILTER IS NOT SAID BACK HERE EITHER. This line used to carry `filter ·
// zzz` because the box a person was typing into was invisible, so the only
// place their own words could appear was UNDER the rows those words had just
// removed. The words are on the control row at the top of the list now
// ([tasksControlRow]). What survives is the half the row cannot say: that
// the query matched nothing, over a body that is blank rather than teaching.
func (p *tasksPlace) note(a *app, width int) []string {
	if p.actionNote != "" {
		return []string{" " + a.pal.dim(fit(p.actionNote, width-2))}
	}
	if p.detailOn || p.reading.held == 0 {
		return nil
	}
	r := a.tasksFiltered()
	if a.taskSheetFiltering() && len(r.items)+len(r.chats) == 0 {
		return []string{" " + a.pal.dim(fit(taskSheetFilterNone, width-2))}
	}
	return nil
}

// hint is SCREEN 1e's foot, assembled from the clauses that are TRUE of the row
// under the cursor (taskview.go spells every word of it and says why the design's
// one static line is built rather than quoted).
//
// The three clauses, in the design's order: which door enter has here, the verbs
// this row can actually be asked for, and the filter — replaced, while one is
// on, by the one key whose meaning just moved.
func (p *tasksPlace) hint(a *app) string {
	// A TEACHING PAGE PROMISES NO ROW KEYS. On a machine that has run nothing the
	// body spends the whole frame saying what tasks ARE ([tasksTeach], gated on
	// this same `held == 0`), and the foot under it went on offering `type to
	// filter` over a page with no rows to filter, no fold to open and no verb to
	// press. What is true there is the way out, and [placeTailed] puts `tab next
	// place` in front of it.
	if !p.detailOn && a.tasksFiltered().held == 0 {
		return mapCloseWords
	}
	var parts []string
	// THE CONVERSATION'S OWN CLAUSE, and it is the word this surface already uses
	// for going to a chat that is not this one ([tasksEnterOpenWord]) rather than
	// a second spelling of the same journey.
	if _, ok := a.taskSheetChat(); ok {
		parts = append(parts, tasksEnterOpenWord)
		if word := a.taskSheetFoldWord(); word != "" {
			parts = append(parts, word)
		}
		return strings.Join(a.tasksPageKeys(parts), railSep)
	}
	item, ok := a.taskSheetCurrent()
	switch {
	case !ok:
		// A PAGE WITH NO ROW UNDER THE CURSOR PROMISES NOTHING ABOUT enter,
		// because enter does nothing there. That is a page a filter has emptied
		// now: work another window is running takes the cursor like any other row
		// and has its own clause below.
	case item.away:
		// AND THAT CLAUSE SAYS WHAT IS REALLY BEHIND THE KEY, which is the same
		// ladder enter walks and not a guess at it: the foot asks [app.taskOwnerOf]
		// the question enter will ask, so the word under the cursor is the door the
		// next keystroke actually opens.
		switch a.taskOwnerOf(item).reach {
		case reachOpen:
			parts = append(parts, tasksEnterOpenWord)
		case reachAttach:
			parts = append(parts, tasksEnterJoinWord)
		default:
			parts = append(parts, tasksEnterAwayWord)
		}
	case a.taskSheetNodeFor(&item.entry) != nil:
		parts = append(parts, tasksEnterRoomWord)
	default:
		parts = append(parts, tasksEnterInsideWord)
	}
	// AND THE FOLD, WHERE THE CURSOR IS ON A FAMILY. It is named before the verbs
	// because on a shut family `→` is the fold and not the verb, and a foot that
	// said otherwise would be naming the second rung of a key whose first rung it
	// had not mentioned ([app.taskSheetFold]).
	if word := a.taskSheetFoldWord(); word != "" {
		parts = append(parts, word)
	}
	// AND A PLAN ROW'S OWN KEYS, beside the door its enter takes: the cancel and
	// the one key that holds the task, named where a person reads what a row can
	// do ([app.tasksPlanKeyWords] says why they are `x` and `p`).
	if ok && item.plan != nil {
		parts = append(parts, a.tasksPlanKeyWords(*item.plan)...)
	}
	if verbs := p.verbs(a); len(verbs) > 0 {
		words := make([]string, 0, len(verbs))
		for _, v := range verbs {
			words = append(words, v.word)
		}
		parts = append(parts, tasksVerbsWord+strings.Join(words, tasksVerbGap))
	}
	return strings.Join(a.tasksPageKeys(parts), railSep)
}

// tasksPageKeys names the filter and the way back after the selected row's actions.
func (a *app) tasksPageKeys(parts []string) []string {
	if a.taskSheetFiltering() {
		return append(parts, tasksClearFilterWord)
	}
	return append(parts, tasksFilterHint, mapCloseWords)
}

func (a *app) taskSheetKeysLine() string { return a.taskSheet.hint(a) }

// verbs is the `→` strip over the row under the cursor, and it is the other half
// of the foot's second clause: the strip draws exactly what the foot named, and
// the foot names exactly what the strip will do.
//
// ONLY ONE OF SCREEN 1e's TWO VERBS EXISTS, and the other is therefore ABSENT
// rather than drawn dead. The design spells `run it again, stop it`:
//
//   - `stop it` is real. A node THIS window's graph is holding, still queued or
//     running, is exactly what [app.stopTaskTarget] offers the roster's own `x`,
//     and the engine door behind it is [app.stopDoors]. Work another conversation
//     ran has no such node — the id in a cancel address is this session's — and
//     work that has settled has nothing left to stop, so neither is offered one.
//   - `run it again` has NO SEAM. Nothing on this machine re-runs a finished
//     task: a record row is an account of work that happened, and starting the
//     same brief again is `/task <brief>`, which is a new piece of work with a
//     new id rather than a repeat of an old one. A capability that cannot work is
//     absent, not broken — so the verb is not named here, and the foot does not
//     promise it.
func (p *tasksPlace) verbs(a *app) []verb {
	item, ok := a.taskSheetCurrent()
	if !ok {
		return nil
	}
	verbs := a.taskRowVerbs(item.row, item.entry)
	entry := item.entry
	node := a.taskSheetNodeFor(&entry)
	if node == nil {
		return verbs
	}
	target := a.stopTaskTarget(node)
	if target.empty() {
		return verbs
	}
	// THE BUILD GUARD, ASKED BEFORE THE VERB IS NAMED. A surface driven by an
	// agent with no door onto cancelling says so when `x` is pressed
	// ([stopUnavailableWord]); a NAMED verb that could only ever answer with that
	// sentence would be this place advertising a key it has not got.
	if _, ok := a.stopDoors(); !ok {
		return verbs
	}
	return append(verbs, verb{key: 's', word: stopActWord, do: func() tea.Cmd { return a.tasksStop(target) }})
}

// tasksStop ends one piece of work from the strip, and says what the engine
// said.
//
// IT ASKS NO CONFIRMATION, AND THAT IS DELIBERATE RATHER THAN MISSING. The
// confirmation card guards a BARE key: `x` is one keystroke over a list, and a
// person walking the roster with it under their finger can end an hour of work
// by accident (stop.go). The strip is not bare — `→` draws the word `stop it`
// and only then does `s` mean anything at all, which is two deliberate presses
// with the verb on screen for the second of them. And the card is UNAVAILABLE
// here by stop.go's own guard: [app.stopKey] refuses while this place is up,
// because a question raised over a frame that is not drawing it is a question
// nobody can see, answered by the next key they press. So the choice was this or
// a foot naming a verb nothing does.
//
// The words, the door and the receipt are all the card's own — [stopActWord],
// [app.stopDoors] and [app.stopSay] — so the two ways to end work can differ in
// how they are reached and in nothing else.
func (a *app) tasksStop(target stopTarget) tea.Cmd {
	doors, ok := a.stopDoors()
	if !ok {
		a.note(stopUnavailableWord)
		return nil
	}
	line, err := doors.Cancel(target.id)
	if err != nil {
		// The engine's own sentence, kept: a stop that could not be given is work
		// still running, and a surface that swallowed the reason would leave a
		// person pressing the same key again.
		a.stopSay(err.Error())
		return nil
	}
	a.stopSay(line)
	return nil
}

// changed is the tab's count: how much work has landed since the last look.
//
// IT ANSWERS FROM THE LATEST CACHED WORLD READING, because the tab bar is a
// paint path and must never turn into a directory walk.
func (p *tasksPlace) changed(a *app, since time.Time) int {
	// THE PLACE'S OWN WORLD WHILE IT IS STANDING, AND HOME'S OTHERWISE. The count
	// is asked of a place that is usually closed — that is the whole point of a
	// tab bar — so what it reads then is the scan home keeps on its own beat.
	world := a.home.world
	if a.at(pageTasks) {
		world = p.world
	}
	count := 0
	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			for _, entry := range row.Tasks.Rows {
				if !row.ArchivedTasks[entry.ID] && !entry.EndedAt.IsZero() && entry.EndedAt.After(since) {
					count++
				}
			}
		}
	}
	return count
}

func (a *app) tasksChangedSince(seen time.Time) int { return a.taskSheet.changed(a, seen) }

// ── the place ───────────────────────────────────────────────────────────────

// placeTasks is this place's handle on the registry. Every method is
// [tasksPlace]'s own, one line each: the state struct at the top of this file
// has carried this shape since before the interface existed (pages.go's [place]
// states the contract and why the handle holds no state itself).
type placeTasks struct{ placeBase }

func init() { registerPlace(placeTasks{}) }

func (placeTasks) id() page      { return pageTasks }
func (placeTasks) word() string  { return sessionsWord }
func (placeTasks) counted() bool { return true }

// open takes the reading and arms the beat. The reading is this place's own
// walk of the record; the beat is the tab bar's ([placeTasks.tick] says which
// of the two it is for).
// open primes the place AND the pane beside it: a record drawn with no report
// under it, waiting for a keystroke that may never come, is a preview that looks
// broken on the one frame everybody sees first.
func (placeTasks) open(a *app) tea.Cmd {
	cmd := a.showTaskPlace()
	return tea.Batch(cmd, a.armPlaceClock(), a.taskPaneFollow())
}
func (placeTasks) close(a *app) { a.taskSheet.close(a) }

// tick keeps the record current while somebody stands on it: the other windows'
// readings are re-filed without a second walk of the disk.
//
// IT RE-FILES AND DOES NOT RE-READ. The reading is taken on the keystroke that
// walks in ([app.showTaskPlace]); a second walk of the same record on the beat
// would be a third reader of it, and this place's own argument against that
// stands.
//
// IT ANSWERS TRUE ANYWAY, because the answer is about the BEAT and not about
// the reading. It used to answer false and that stopped the clock dead: the tab
// bar's numbers are recomputed on this beat, so standing on this place froze
// every tab's count — including the counts of the six rooms this one has
// nothing to do with (placecounts.go's [app.placeBeat]).
func (placeTasks) tick(a *app, now time.Time) (bool, tea.Cmd) {
	a.taskSheet.regroup(a)
	return true, nil
}

func (placeTasks) body(a *app, width, room int) []placeRow {
	return a.taskSheetBody(width, room)
}
func (placeTasks) stops(a *app) []int { return a.taskSheet.stops(a) }

// cursorAt is the line of the layout the cursor is on, in [placeTasks.stops]'s
// own numbers (pages.go's [place.cursorAt]).
func (placeTasks) cursorAt(a *app) int { return a.taskSheet.cursor }

// cursorRow is which drawn row the tasks cursor landed on. A task's card can be
// several lines and every one of them carries the same hit, so this answers the
// LAST of them — the strip belongs under the whole row it is about, not inside
// it (pages.go's [place.cursorRow]).
func (placeTasks) cursorRow(a *app, rows []placeRow) int {
	found := -1
	for i, row := range rows {
		if hit, ok := row.hit.(taskSheetHit); ok &&
			hit.kind == taskSheetHitRow && hit.index == a.taskSheet.cursor {
			found = i
		}
	}
	return found
}

func (placeTasks) enter(a *app) tea.Cmd { return a.taskSheet.enter(a) }
func (placeTasks) verbs(a *app) []verb  { return a.taskSheet.verbs(a) }

// rowID is the piece of work under the cursor, named by the pair that
// identifies a row of the project's record — the session that ran it and the
// node's id inside that session, which is what [session.TaskIndexEntry.ID]'s
// own header says is needed, ids being unique only within one conversation.
//
// The filter, the window and the beat all rebuild this list under the cursor,
// so the row at index four is not the row that was there when `→` was pressed.
func (placeTasks) rowID(a *app) string {
	if chat, ok := a.taskSheetChat(); ok {
		// A CONVERSATION IS NAMED SO IT CANNOT BE MISTAKEN FOR THE WORK UNDER IT
		// ([tasksChatKey] holds the mark that makes that true). It carries no verbs
		// of its own, and a strip captured over a task must not survive the cursor
		// stepping onto the chat above it.
		return chat.key.session + "\x00" + chat.key.id
	}
	item, ok := a.taskSheetCurrent()
	if !ok {
		return ""
	}
	return item.entry.SessionID + "\x00" + item.entry.ID
}
func (placeTasks) window(a *app, key string) (bool, tea.Cmd) {
	return a.taskSheet.window(a, key), nil
}
func (placeTasks) note(a *app, width int) []string     { return a.taskSheet.note(a, width) }
func (placeTasks) hint(a *app) string                  { return a.taskSheet.hint(a) }
func (placeTasks) changed(a *app, since time.Time) int { return a.taskSheet.changed(a, since) }

// box is the filter, exactly as it has always been: every printable key on this
// place goes into it and the list narrows as it fills. Its letters are drawn on
// the first row of the list, over the rows they changed ([tasksControlRow]);
// the foot draws no box here ([place.box]). The editor stays the place's box
// so shared editing and filtering controls can act on the same value.
func (placeTasks) box(a *app) *editor { return &a.taskSheet.query }

// Age direction changes through the header; no alternate sort keys are bound.
func (placeTasks) alt(a *app, letter rune) bool { return false }

// caretRow is the filter: [placeTasks.body] moved the box into the list's
// control row, and the caret that was parked in the foot's silhouette stayed
// behind — blinking under a dim invitation at the bottom of the frame while
// the letters land rows above it. The control row is painted by the reading's
// own layout (taskstable.go's [tasksControlRow]), so the hook asks the same
// painter with the same inputs the layout painted with — the place's untrimmed
// query and its sort order, at the width the list half is drawn in
// ([taskPaneList] splits the list from the pane at this width) — and finds the
// line among the rows the frame just built. The split body pads the line out
// to the seam (taskpane.go), so the line is a PREFIX of the drawn row.
func (placeTasks) caretRow(a *app, width int, rows []placeRow) (int, int, bool) {
	// AN EMPTY PLACE TEACHES RATHER THAN FILTERS ([app.taskSheetBody]): the
	// teach prose draws no control row, and the foot's invitation is the only
	// box on the frame.
	if a.taskSheet.reading.held == 0 {
		return 0, 0, false
	}
	line, column := tasksControlRow(a.taskSheet.query.String(), a.taskSheet.order, taskPaneList(width), a.pal)
	for j, row := range rows {
		if strings.HasPrefix(row.text, line) {
			return j, column, true
		}
	}
	// The window scrolled the control row off this frame — a short frame over a
	// long list. The caret is hidden rather than parked in the foot's resting
	// sentence, which is not the box the person is typing into.
	return -1, 0, true
}

// tasksFilterHint is the short spelling of that invitation. It is the control
// row's own placeholder ([tasksControlRow]) and it is repeated on the FOOT'S KEY
// LINE, which the ruling of 2026-09-11 asks for beside the sort chord: a chord
// nobody can find is a chord that does not exist, and the filter is the other
// half of what the keyboard does here.
const tasksFilterHint = "type to filter"

// bar is the phone lane's foot: the key legend becomes a `‹ back` band a thumb
// leaves by (taskphone.go). The count above it stays — a bar is the way out, and
// the tally is what the place is holding.
func (placeTasks) bar(a *app, width int) (string, placeHit, bool) {
	if layoutTier(width) != tierPhone {
		return "", nil, false
	}
	line, _ := a.taskSheetBar(width)
	return line, taskSheetHit{kind: taskSheetHitBar}, true
}

// ownFrame is the record CARD, drawn INSTEAD of the list and not over the top of
// it. It is a mode of this place and it takes the whole of the frame, so the
// rows below are not built at all while it is up (taskrecord.go).
//
// It answers NO HITS OF ITS OWN. The card's rows are resolved against the card's
// own frame ([app.taskCardPress]), and a list hit reported for a row the list did
// not draw is exactly how a click opens the wrong task.
func (placeTasks) ownFrame(a *app, width, height int) ([]string, []placeHit, int, int, bool) {
	if !a.taskSheet.detailOn {
		return nil, nil, 0, 0, false
	}
	// THE PLAN PAGE DRAWS IN THE CARD'S PLACE, through the same latch and the
	// same frame slot ([app.taskPlanFrame]).
	if a.taskSheet.planOn {
		lines, caretX, caretY := a.taskPlanFrame(width, height)
		return lines, nil, caretX, caretY, true
	}
	// NOTHING ON THE CARD IS TYPED INTO, so the caret is hidden rather than
	// parked at the frame's origin over the title — the same law the job page
	// and home at rest follow (view.go states it in [app.frameBody]), and the
	// one this card's own header has claimed all along. The card is a record a
	// person reads: a blinking bar with no box behind it is a cursor pointing at
	// a key that does not exist.
	a.caret = false
	lines, _, caretX, caretY := a.taskCardFrame(width, height)
	return lines, nil, caretX, caretY, true
}

// press, hover and wheel are this place's own: it resolves the pointer against
// the hit map its frame wrote ([app.taskSheetPress]) rather than against a body
// line, because a row of this list can be two screen lines tall at [tierPhone].
func (placeTasks) press(a *app, y int) (tea.Cmd, bool) {
	return a.taskSheetPress(0, y), true
}

func (placeTasks) hover(a *app, y int) bool {
	a.taskSheetHover(y)
	return true
}

func (placeTasks) wheel(a *app, delta int) (tea.Cmd, bool) {
	a.taskSheetScroll(delta)
	a.touch()
	// THE WHEEL MOVES THIS PLACE'S CURSOR, so the pane beside the list follows it
	// exactly as it follows an arrow key (taskpane.go's [app.taskPaneFollow]).
	return a.taskPaneFollow(), true
}

// owns is the task room — the card drawn over the roster — and the door home is
// the reason it is written here rather than left where it was. SPACE PAGES A
// CARD IN THE TASK ROOM, SO THE HOME DOOR YIELDS THERE. The card's own map
// spells the key `pgdown`, `ctrl+f`, `space` ([app.taskCardKey]), and that arm
// lived inside [app.taskSheetKeyPress] — which the router reaches through
// [placeTasks.key], BELOW the door. So a filter left holding one space, which
// is the exact state the door's own first press creates and which draws nothing
// a person could see, armed the door on a box the card types nothing into: the
// space meant to page the record took the person to home instead. Memory's card
// editor was the same hole in the same shape ([placeMemory.owns]), and this is
// the same claim the settings panel makes for its nested boxes
// ([placeSettings.owns]).
//
// THE ROUTER KEEPS ITS CHORDS WHILE THE CARD IS UP, which is why this reads
// [app.placeKey] before the card rather than swallowing the keyboard whole. That
// is the order [app.taskSheetKeyPress] has always used, restated here because
// the whole point of moving the card above the door is that nothing else about
// it moves: `tab`, the place chords and the map answer over an open card exactly
// as they did.
func (placeTasks) owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.taskSheet.detailOn {
		return nil, false
	}
	defer a.touch()
	if cmd, took := a.placeKey(msg); took {
		return cmd, true
	}
	if a.taskSheet.planOn {
		return a.taskPlanKey(msg), true
	}
	return a.taskCardKey(msg.String()), true
}

// key is this place's own reading of a key the router did not take
// (pages.go's [place] states the split).
func (placeTasks) key(a *app, msg tea.KeyPressMsg) tea.Cmd {
	a.taskSheet.actionNote = ""
	cmd, _ := a.taskSheetKeyPress(msg)
	// AND THE PANE FOLLOWS THE CURSOR WHATEVER MOVED IT. This is the one door
	// every key to this place comes through, which is the only place the arming
	// can be complete: the walk is only one of the ways the cursor moves, and the
	// fold arrows, the window arrows, `esc` on a filter and the editor's own
	// chords all answer inside [app.taskSheetKeyPress] and return before its tail
	// (taskpane.go's [app.taskPaneFollow] drops the ones with nothing to do).
	return tea.Batch(cmd, a.taskPaneFollow())
}

// placeFold is the grammar's one door onto a place's tree: `→` opens the thing
// under the cursor and `←` shuts it, on whichever place has one.
//
// TODAY EXACTLY ONE PLACE ANSWERS IT. That is a fact about the places and not a
// shortcut — home's fold is a single line at the foot of a list rather than a
// tree, and the other five have flat bodies — and the router asks through this
// one name so a second place growing a tree is one arm here rather than another
// claim on an arrow key in placekeys.go.
func (a *app) placeFold(open bool) bool {
	if !a.at(pageTasks) {
		return false
	}
	return a.taskSheetFold(open)
}

// taskSheetFoldWord is what the foot says about the family under the cursor, and
// "" where the cursor is not on one — the emptiness law said about a key.
func (a *app) taskSheetFoldWord() string {
	r := a.tasksFiltered()
	lines := r.lay(a.taskSheetListWidth())
	at := a.taskSheet.cursor
	if at < 0 || at >= len(lines) || !lines[at].folds {
		return ""
	}
	if lines[at].open {
		return tasksShutWord
	}
	return tasksOpenWord
}

// The two words, spelled once, and quoted in the manual exactly as they are here.
const (
	tasksOpenWord = "→ what ran under it"
	tasksShutWord = "← fold it back up"
)
