package tui3

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE TASK PAGE: /history, ctrl+. , or the column's own one door line — the
// SECOND of the two fullscreen pages this surface draws at every width, and the
// settings panel (settings.go) is the first. The status deck and the tool detail
// take the frame as well, but only at [tierPhone].
//
// It exists because the roster answers one question and a person keeps asking
// two. The column beside the conversation is THIS SESSION'S record of its own
// work ([app.railEntries] walks [app.taskOrder] and nothing else), which is the
// right answer to "what is happening" and no answer at all to "what did we do
// about this last week". The project has that answer on disk — internal/session
// keeps a tasks.jsonl beside the conversations and [session.Agent.TaskIndex]
// reads it back with this session's live graph merged over the top — and until
// this page the only door onto it was the "@" drop-up, which is a completion
// somebody has to already be typing a message to reach.
//
// Three rules hold it together, and each is the settings panel's read across to
// a list of work:
//
//   - IT IS MODAL AND FULLSCREEN, and it is now one of exactly two things on
//     this surface that are. Opening either closes the other ([app.openTaskSheet]
//     and [app.openSettings] each say so), because two pages that both believe
//     they own the frame is a frame that draws one and takes keys for the other.
//   - IT HAS TWO SECTIONS AND THEY ANSWER DIFFERENT QUESTIONS. `running` is the
//     tree — families with anything still going, drawn whole, with what each node
//     is doing and what it is spending under its title, because a shape is what
//     makes a run legible. `earlier` is a FLAT list of everything the project has
//     finished, newest first, one line each: a person reading history is looking
//     for a name, and a tree of four hundred landed nodes is a shape nobody is
//     reading.
//   - `running` IS THE DIRECTORY'S AND NOT THIS WINDOW'S. Under the tree it
//     carries a row per piece of work every OTHER aforge window open on this
//     project has out, with the window named on the right — the one place in this
//     program a person can see that the directory is busy somewhere else. The
//     roster's column stays this session's own; see the block above
//     [elsewhereEvery] for why the split falls there.
//   - IT READS THE SAME SNAPSHOT THE "@" LIST READS ([app.comp].tasks, loaded by
//     [app.loadTasks]). One read, one cache, one answer to "what has this project
//     run" — a page that fetched its own copy would be a second answer with its
//     own staleness. The column asks the same snapshot ONE QUESTION
//     ([app.railHasRecord]) — is there any of this at all — because the answer
//     decides whether the foot of the column carries a door onto this page.
//   - IT IS TYPED AT. Every printable key builds a filter over both sections at
//     once ([app.taskSheetFilter]), because a record of four hundred tasks is
//     reached by remembering a word of the title and by nothing else. esc backs
//     out of the filter first and closes the page second, which is the settings
//     panel's own layering ([app.sheetKey]).
//
// WHAT IT DOES NOT DO IS SHARE THE COLUMN. The column carried a dulled footnote
// of this same record under its live rows for a while, and it was the wrong
// place for it: six rows out of two thousand is not a record, the rows pushed the
// column's own "no tasks yet" off the top, and a person walking the roster's
// cursor fell out of this conversation's work into another one's without the
// column ever saying they had. So the column is THIS CONVERSATION'S WORK AND
// NOTHING ELSE, and what stands at the foot of it is ONE DIM DOOR onto this page
// ([taskSheetPastHint], drawn by task.go's [app.railFootRows]) whenever the
// project has a record to open. Every row of old work — and every door into one —
// is here.

// The page's own key, and the words that name it.
//
// ctrl+. IS THE LAST OBVIOUS CHORD AND IT IS SPENT DELIBERATELY. Every
// ctrl+<letter> this surface could reach for is taken — the readline edits the
// message box answers without looking, the roster's ctrl+t, the column's ctrl+g,
// copy mode's ctrl+b — and the four letters that are free are documented as NOT
// BOUND, which is a promise a person has read. What is left is the punctuation
// pair, and the pair is the point: ctrl+, opens the settings panel and ctrl+.
// opens this one, two adjacent keys for the two fullscreen pages. A terminal
// that cannot send one cannot send the other either, which is why this page has
// two more doors — /history, and the line at the bottom of the column.
const (
	taskSheetKey = "ctrl+."
	// taskSheetWord is the page's name, in the title and in the command list
	// alike, and it is the word the command spells: /history.
	//
	// IT IS NOT SPELLED "tasks", AND THAT IS THE WHOLE OF WHY THE COMMAND IS
	// /history. "/task <brief>" means GIVE AFORGE WORK, and it has three rows in
	// the command list; a "/tasks" beside them narrowed to both on the four
	// characters they share, so the muscle memory for starting work led to a page
	// that starts none. What a person calls this thing is the record of everything
	// the project has run, and "history" is that word.
	taskSheetWord = "history"
	// taskSheetNowHead heads the tree. It is [railGroupWords]'s own word rather
	// than a second one, because a person who reads "3 running" at the bottom of
	// the column must not have to learn that this page calls the same thing
	// something else.
	taskSheetNowHead = "running"
	// taskSheetPastHead heads the flat list. "earlier" and not "past": the rows
	// under it are work, in the order it happened, and the word a person uses for
	// the thing that came before this one is the word that goes on it.
	taskSheetPastHead = "earlier"
	// taskSheetEmpty is what /history says instead of opening an empty page. The
	// emptiness law reaches modals too — a fullscreen page with nothing on it is
	// the loudest possible way of saying nothing.
	taskSheetEmpty = "no tasks yet — /task <brief> starts one"
)

// The two words a row of the project's RECORD says about a claim of running,
// and the one that names the window a piece of work belongs to.
//
// THE RECORD IS A FILE AND THE FILE CANNOT CORRECT ITSELF. A row takes the word
// `running` when the work starts and nothing rewrites it, so a window that was
// killed, or a laptop that shut, leaves rows claiming a present that ended hours
// ago (internal/session's world.go states the law and taskelsewhere.go applies
// it here). What settles the claim is the window that made it: while that window
// is open and still names the node among the work it has out, the row is
// running; the moment it is not, the row is a record of work nobody finished.
const (
	// taskRecordRunsWord goes on a row another window is still holding. It is the
	// same word the sections and the column's tally already spend
	// ([taskSheetNowHead]), because it is the same fact.
	taskRecordRunsWord = taskSheetNowHead
	// taskRecordStoppedWord goes on a row that claims to be running with nobody
	// running it. It is `incomplete` — the word the interrupted-task outcome
	// itself uses (session's taskInterruptedOutcome), the word the home page
	// puts on the same fact, and NOT a claim about the work: nobody looked at it
	// and nobody judged it, it simply stopped.
	taskRecordStoppedWord = "incomplete"
	// taskAwayWord names the place a piece of work is happening when the place
	// is not this window. It is what a row says when the other window never
	// settled on a title; a window that HAS one says both, because the name is
	// how a person tells two other windows apart.
	taskAwayWord = "another window"
)

// taskAwayNote is the dim tail on a row of another window's work: where it is
// happening, and — when that window has settled on a name — what it is called.
//
// THE EMPTINESS LAW DECIDES THE SHAPE. A window nothing has named has no name,
// and a tail reading "another window · " with nothing after it would be a
// separator standing in for a fact. So the name is added or it is not, and the
// row is honest either way.
func taskAwayNote(name string) string {
	if name = strings.TrimSpace(name); name == "" {
		return taskAwayWord
	}
	return taskAwayWord + railSep + name
}

// The two lines at the foot, which are what this page offers each hand.
const (
	taskSheetRoomKeys = "esc close · ↑↓ move · enter opens its room"
	// taskSheetInsideKeys is the foot over a row of the project's record, and it
	// promises what enter now does: it GOES INSIDE that task — the card carrying
	// what the work came to, what it cost, where it left its changes and the last
	// thing it said (taskrecord.go).
	//
	// It used to read "enter puts it in your message", which was the truth about a
	// key answering the wrong question. The mention is still one keystroke away
	// and the card's own foot names it.
	taskSheetInsideKeys = "esc close · ↑↓ move · enter goes inside it"
	// taskSheetFilterWord opens the line that says what was typed, and
	// taskSheetFilterNone is what that line adds when the query has taken every
	// row off the page. A filtered page with nothing on it and nothing said is a
	// page a person reads as broken.
	taskSheetFilterWord = "filter · "
	taskSheetFilterNone = " · nothing matches"
	// taskSheetFilterKeys replaces the enter line while a filter is being typed:
	// esc means the filter first and the page second, and a foot that went on
	// promising to close would be lying about the next keystroke.
	taskSheetFilterKeys = "esc clears the filter · ↑↓ move · enter opens the row"
	// taskSheetReadKeys is the foot for a page with nothing under the cursor to
	// act on, which is a page whose only rows are another window's work. It
	// PROMISES NOTHING ABOUT enter, because enter does nothing there — a foot
	// still offering a room over work this window cannot open would be the page
	// lying about its own door.
	taskSheetReadKeys = "esc close · ↑↓ move"
	// taskSheetMoreHint is the line at the bottom of the ROSTER'S COLUMN that
	// reaches this page (task.go's [app.railFootRows]). It is shaped like the two
	// lines under it — the key, then what it reaches — and it is drawn only when
	// there is genuinely more here than the column is showing (task.go's
	// [app.railFootRows] weighs it against [app.railFoldedAny] and
	// [app.railHasRecord]).
	taskSheetMoreHint = taskSheetKey + " — view more"
	// taskSheetPastHint is that SAME LINE when what is behind it is the project's
	// own record, and it is the commoner of the two by a long way: any directory
	// that has been worked in before has one.
	//
	// IT IS ONE DOOR WEARING THE NAME OF WHAT IT OPENS, not a second door. The
	// column has exactly one line onto this page and the words on it say which
	// question the page will answer — "earlier" when there is history down there,
	// "view more" when the only thing the column is holding back is a family it
	// folded. A permanent "view more" over a month of finished work never told
	// anybody the work existed, which is the whole reason the record was ever
	// footnoted onto the column in the first place.
	//
	// It is [taskSheetPastHead]'s own word rather than a second one, because it is
	// the section it lands you in.
	taskSheetPastHint = taskSheetKey + " — " + taskSheetPastHead
)

// taskSheetRows is the page's own page size: what pgup and pgdown move by, and
// nothing else. It is not a cap on anything — the list is as long as the project
// is, and the window scrolls it.
const taskSheetRows = 12

// taskSheet is the page's whole state. The zero value is closed.
//
// THE CURSOR IS AN ITEM INDEX AND THE OFFSET IS TOO, which is not the roster's
// bargain ([app.railView] windows LINES) and is deliberate: a running node is
// one to three lines tall and a landed one is always one, so a window measured
// in lines would have to re-render every row above the offset to find out where
// it starts. Windowing items renders exactly what is drawn.
type taskSheet struct {
	open   bool
	cursor int
	top    int
	// query is the type-to-filter box, and it is the [editor] every other box on
	// this surface is rather than a string of its own: backspace, ctrl+u and
	// ctrl+w are edits a person's hands already know, and a second implementation
	// of them would be a second set of bugs in them.
	query editor

	// detail is the row of the project's record this page is standing INSIDE,
	// and detailOn is what says it is (taskrecord.go). Together they are the
	// page's second MODE rather than a second page: the list is still underneath,
	// esc backs out to it, and the chord still closes the lot.
	//
	// IT IS A COPY AND NOT A POINTER, which is where it parts company with
	// [taskSheetItem.entry]. That one points into the snapshot because it is
	// rebuilt from it thirty times a second; this one outlives a reload — the
	// snapshot is replaced whole whenever a node lands (taskmention.go's
	// [app.tasksLoaded]) — and a card is a reading of one finished piece of work
	// rather than a live view of a row.
	detail   session.TaskIndexEntry
	detailOn bool
	// detailTop is the card's own scroll. The report under it is as long as the
	// node made it, and the card is read rather than walked, so an offset is the
	// only thing that moves ([clampTop], expand.go).
	detailTop int
	// tail is the last thing the node said, read off its journal once when the
	// card opened, and tailRead says the read has happened — an empty tail with
	// tailRead false is a read still in flight, and one with tailRead true is a
	// journal that had nothing in it.
	tail     string
	tailRead bool
}

// taskSheetItem is one row of the page: a heading, a node of the tree, or one
// row of the project's record.
//
// Headings are items rather than a separate list for the settings panel's
// reason ([sheetItem]): the cursor walks one list and steps over the rows that
// are not choices, so there is no second index to keep in agreement with the
// first.
type taskSheetItem struct {
	// head is the section's word, and it is what makes this row a heading.
	head string
	// node and stems are the tree half: the node, and its ancestry as the
	// connectors need it ([railEntry.stems]).
	node  *taskNode
	stems []bool
	root  bool
	// entry is the flat half: one row of the project's record.
	//
	// IT POINTS INTO THE SNAPSHOT RATHER THAN COPYING IT. The record runs to two
	// thousand rows, this list is rebuilt on every frame, and a frame arrives
	// thirty times a second while anything is running — a row carried by value
	// would be half a megabyte of copying per rebuild for a page that draws
	// twenty lines. The snapshot it points into is REPLACED whole rather than
	// edited (taskmention.go's [app.tasksLoaded]), so a pointer taken here is
	// still a pointer at something true.
	entry *session.TaskIndexEntry
	// away is the third kind of row: one piece of work ANOTHER window on this
	// project has out at this instant, which is neither a node of this session's
	// graph nor a row of the file ([app.taskSheetAwayRows] says why it is both
	// drawn and unpressable). It points into the slice the frame built for the
	// same reason [taskSheetItem.entry] does.
	away *session.ElsewhereTask
}

func (i taskSheetItem) heading() bool { return i.head != "" }

// pick reports whether the CURSOR may stand on this row.
//
// IT IS NOT THE OPPOSITE OF [taskSheetItem.heading], and that is the whole
// reason it exists. A section's word answers nothing because it is a rule; a row
// of another window's work answers nothing for a different reason — there is no
// door onto it from here ([app.taskSheetAwayRows] says why) — and a cursor that
// could stand on one would be a cursor promising enter something it cannot do.
func (i taskSheetItem) pick() bool { return i.head == "" && i.away == nil }

// ── what the other windows have out ─────────────────────────────────────────

// THE PROJECT IS BIGGER THAN THIS WINDOW, and until now this surface could not
// say so.
//
// A person with two aforge windows open on one directory would start a task in
// the first, look at the second, and find no trace of it anywhere: not in the
// column, not on this page, not in the "@" list. The reason is in
// internal/session's taskelsewhere.go — an ordinary task writes NO row into the
// project's index until it lands, so there is nothing on disk for a second
// window to read — and the fix is the presence file every live session already
// keeps, which says what that session has out at this instant.
//
// TWO THINGS COME OUT OF ONE READING, and they are drawn in two different places
// for one reason: the roster's tree is THIS SESSION'S work and stays that way
// ([app.railEntries] walks [app.taskOrder] and nothing else), because a tree
// with another window's nodes hanging off it would be a shape that claims a
// parentage nothing has. So:
//
//   - THE HISTORY PAGE'S `running` SECTION gains a flat row per piece of work
//     another window is holding, under this session's own tree, each with the
//     window it belongs to on the right.
//   - EVERY RECORD ROW, on this page and in the column alike, gets its claim of
//     running judged against the same reading ([app.recordRuns]).

// elsewhereEvery is how long ONE reading of the other windows is held before
// another is taken.
//
// THREE SECONDS, AND IT IS A CADENCE RATHER THAN A CACHE SIZE. The reading is a
// directory read plus two small files per window, which is nothing on a clock
// and thirty times a second on a frame — and the thing being read only changes
// every [session.presenceHeartbeat] anyway, so a shorter window would buy
// re-reads of a file nobody has rewritten. It is the ONE number: the paint clock
// asks for a refresh while the roster or this page is on the frame and this
// decides whether the ask reaches the disk.
const elsewhereEvery = 3 * time.Second

// elsewhereCache is one held reading and its stamp. The zero value has never
// read anything, which is what [app.elsewhere] takes as "go and look".
type elsewhereCache struct {
	held session.Elsewhere
	at   time.Time
	read bool
}

// elsewhereAgent is the slice of [session.Agent] this file needs, asserted
// rather than added to [Agent].
//
// It is optional on [taskMentionAgent]'s own terms: a surface driven by a
// scripted agent has no project bucket and no other windows, and the honest
// answer for one is an empty reading rather than a seam every test has to
// implement.
type elsewhereAgent interface {
	// Elsewhere is what the project's OTHER windows have out right now.
	Elsewhere() session.Elsewhere
}

// keeperAwareAgent is [elsewhereAgent] told which other windows are OURS
// (session's taskelsewhere.go).
//
// A second conversation of this process on the same project writes the same
// presence file every other terminal reads, so without this it would arrive on
// our own away rows as `another window` — and `go to that window to act on it`
// is the wrong answer when the window is this one and the way there is `tab`.
type keeperAwareAgent interface {
	ElsewhereExcept(others ...string) session.Elsewhere
}

// elsewhere is the reading this surface is currently drawing from. IT NEVER
// TOUCHES THE DISK except the very first time it is asked — every later refresh
// is the paint clock's ([app.refreshElsewhere]) — so it is safe to ask from
// inside a layout, which is where every caller is.
//
// The first reading is taken on demand rather than at startup because the
// alternative is worse than a lazy read: a surface that drew one frame before
// its first reading would spend that frame calling another window's live work
// `incomplete`, which is the exact lie this whole lane exists to stop telling.
func (a *app) elsewhere() session.Elsewhere {
	if !a.away.read {
		a.refreshElsewhere()
	}
	return a.away.held
}

// refreshElsewhere takes a new reading if the held one has aged out.
//
// IT IS CALLED FROM THE PAINT CLOCK AND ONLY WHILE SOMETHING DRAWS IT (app.go's
// [app.paint] gates on the roster standing or this page being open), and from
// [app.openTaskSheet] on the way in — a page raised after ten minutes of a
// stowed column must not answer out of a ten-minute-old reading.
func (a *app) refreshElsewhere() {
	if a.away.read && a.now().Sub(a.away.at) < elsewhereEvery {
		return
	}
	a.away = elsewhereCache{at: a.now(), read: true}
	if aware, ok := a.agent.(keeperAwareAgent); ok {
		a.away.held = aware.ElsewhereExcept(a.behindIDs()...)
		return
	}
	agent, ok := a.agent.(elsewhereAgent)
	if !ok {
		return
	}
	a.away.held = agent.Elsewhere()
}

// behindIDs is the session id of every conversation this process holds and is
// not drawing.
//
// THE ID IS THE SESSION FOLDER'S NAME, which is what [session.Place.ID] answers
// and what the presence file carries — so it is arithmetic on the transcript
// path rather than a question for the agent. A legacy flat journal has no
// folder to name and contributes nothing, which costs at most one stale
// `another window` row on a session shape that predates presence entirely.
func (a *app) behindIDs() []string {
	if len(a.behind) == 0 {
		return nil
	}
	out := make([]string, 0, len(a.behind))
	for _, held := range a.behind {
		if dir := homeBucketOf(held.conv.SessionFile); dir != "" {
			out = append(out, filepath.Base(filepath.Dir(held.conv.SessionFile)))
		}
	}
	return out
}

// recordRuns is THE judgement about one row of the project's record: is this
// work happening, or is it a file remembering that it started?
//
// IT IS ASKED IN EXACTLY ONE PLACE PER SURFACE — the page's row and the column's
// row are drawn by the same function ([app.taskSheetPastRow]) and both come
// through here — because a screen that said `running` on a row it filed under
// the record would be the surface arguing with itself.
//
// The ladder is internal/session's, in the order the better answer comes first:
// a node THIS session's graph is holding is running because this window is the
// authority on its own work, and any other row is running only while the window
// that wrote it is open and still names it ([session.Elsewhere.Runs]).
func (a *app) recordRuns(entry *session.TaskIndexEntry) bool {
	if entry == nil || !entry.Live() {
		return false
	}
	if a.taskSheetNodeFor(entry) != nil {
		return true
	}
	return a.elsewhere().Runs(*entry)
}

// taskSheetAwayRows is every piece of work another window on this project has
// out, as rows this page can draw.
//
// A ROW HERE IS READ AND NOT PRESSED, and that is deliberate rather than
// unfinished. The two doors this surface has onto a piece of work are a ROOM,
// which is a live lane onto a node in THIS session's graph, and a MENTION, which
// mints a pointer block out of a landed row's outcome, branch and transcript
// ([app.mentionTask]). Work running in another window has neither: no node here
// to open, and nothing landed to point at. So the cursor steps over these rows
// ([taskSheetItem.pick]) and they say what they are for — knowing that the
// directory is busy, and where.
//
// A TASK NOTHING NAMED IS LEFT OFF. A row with no words on it says nothing a
// person can act on, which is the same refusal [session.recordTaskIndexEntry]
// makes about writing one.
func (a *app) taskSheetAwayRows() []session.ElsewhereTask {
	tasks := a.elsewhere().Tasks()
	out := make([]session.ElsewhereTask, 0, len(tasks))
	for _, task := range tasks {
		if strings.TrimSpace(task.Task.Title) == "" {
			continue
		}
		out = append(out, task)
	}
	return out
}

// ── opening and closing ─────────────────────────────────────────────────────

// openTaskSheet raises the page, and reports whether it went up.
//
// IT REFUSES ON AN EMPTY PROJECT rather than drawing a page with a title and
// nothing under it. The refusal is the caller's to say — /history writes a line
// and the key falls through in silence — because a command typed on purpose that
// answers with nothing reads as a command that broke, and a chord that was never
// bound in the person's mind reads as a chord that was never bound.
func (a *app) openTaskSheet() bool {
	// THE OTHER WINDOWS ARE RE-READ ON THE WAY IN, before the page decides
	// whether it has anything to show — a directory whose only live work is in
	// the window next door is a directory this page has something to say about,
	// and answering out of a reading taken while the column was stowed would
	// refuse to open over work that is happening right now.
	a.refreshElsewhere()
	if !a.taskSheetHasAnything() {
		return false
	}
	// THE OTHER FULLSCREEN PAGES STAND DOWN — the settings panel and home both
	// ([app.standDownFullscreen] states the law). Only one of the three may
	// believe it owns the frame: view.go draws them in a fixed order, so a page
	// opened under another would take the keyboard and never be seen.
	a.standDownFullscreen()
	a.taskSheet = taskSheet{open: true}
	a.taskSheetFollow()
	a.touch()
	return true
}

// openTaskPage is the whole of what a COMMAND does with this page: open it, or
// say why there was nothing to open, and arm the read either way.
//
// IT IS ONE FUNCTION BECAUSE THERE ARE TWO DOORS. /history is the page's own
// name, and a bare /task reaches it as well (taskcommand.go says why), and two
// copies of these four lines are two ways for the same command to differ from
// itself — the refusal in particular, which is a sentence a person reads.
func (a *app) openTaskPage() tea.Cmd {
	if !a.openTaskSheet() {
		// It REFUSES rather than raising a page with a title and nothing under it —
		// the emptiness law reaches modals — and it says so, because a command typed
		// on purpose that answers with silence reads as a command that broke.
		a.note(taskSheetEmpty)
		return nil
	}
	return a.loadTasks()
}

func (a *app) closeTaskSheet() {
	a.taskSheet = taskSheet{}
	a.touch()
}

// taskSheetHasAnything reports whether there is a single row to draw.
//
// ANOTHER WINDOW'S WORK COUNTS. A person who opens a second aforge in a
// directory and asks for /history before running anything themselves is asking
// precisely because something is happening next door, and a refusal there would
// be the page denying the one fact it was opened to report.
func (a *app) taskSheetHasAnything() bool {
	if len(a.taskOrder) > 0 {
		return true
	}
	if len(a.taskSheetAwayRows()) > 0 {
		return true
	}
	return len(a.taskSheetPast(nil)) > 0
}

// taskSheetFollow parks the cursor on the first row that is not a heading. It is
// what opening does, and what a list that has shrunk under the cursor does.
func (a *app) taskSheetFollow() {
	items := a.taskSheetItems()
	a.taskSheet.cursor = taskSheetClamp(items, a.taskSheet.cursor)
}

// taskSheetClamp is the cursor rule: only ever a row that answers to it
// ([taskSheetItem.pick] — never a section's word, never another window's work),
// and never off the end. It walks forward first and then back, so a cursor that
// lands on a section's word steps onto the first row of that section rather than
// off the top of the page. A page whose every row is another window's leaves the
// cursor where it was, and [app.taskSheetCurrent] answers false for it.
func taskSheetClamp(items []taskSheetItem, at int) int {
	if len(items) == 0 {
		return 0
	}
	at = min(max(at, 0), len(items)-1)
	for i := at; i < len(items); i++ {
		if items[i].pick() {
			return i
		}
	}
	for i := at; i >= 0; i-- {
		if items[i].pick() {
			return i
		}
	}
	return at
}

// ── the rows ────────────────────────────────────────────────────────────────

// taskSheetItems is the whole page as a list: the tree, then the record, minus
// whatever the filter has taken out of both.
//
// THE RECORD IS SUBTRACTED FROM THE UNFILTERED TREE and only then filtered
// itself, which is the one ordering here that is not free: [app.taskSheetPast]
// drops the rows the tree above is already drawing, and asking it about a tree a
// query has just emptied would put every one of those rows back under `earlier`
// as though another conversation had run them.
func (a *app) taskSheetItems() []taskSheetItem {
	live := a.taskSheetForest()
	away := a.taskSheetAwayRows()
	past := a.taskSheetPast(live)
	if needle := a.taskSheetFilter(); needle != "" {
		live, past = taskSheetKeepLive(live, needle), taskSheetKeepPast(past, needle)
		away = taskSheetKeepAway(away, needle)
	}
	out := make([]taskSheetItem, 0, len(live)+len(away)+len(past)+2)
	// A SECTION WITH NOTHING IN IT IS NOT DRAWN AT ALL, filter or no filter. It is
	// the emptiness law: a `running` rule with a blank under it says the query
	// found something and lost it.
	//
	// ONE SECTION HOLDS BOTH KINDS OF RUNNING WORK, this session's tree and the
	// other windows' flat rows under it, because `running` is one question and a
	// person asking it is asking about the DIRECTORY. Which window a row belongs
	// to is said on the row itself ([app.taskSheetAwayRow]), which is where a
	// fact about one row belongs — a second heading would make the reader learn a
	// section to learn a word.
	if len(live) > 0 || len(away) > 0 {
		out = append(out, taskSheetItem{head: taskSheetNowHead})
		for _, e := range live {
			out = append(out, taskSheetItem{node: e.node, stems: e.stems, root: e.root})
		}
		for i := range away {
			out = append(out, taskSheetItem{away: &away[i]})
		}
	}
	if len(past) > 0 {
		out = append(out, taskSheetItem{head: taskSheetPastHead})
		for _, entry := range past {
			out = append(out, taskSheetItem{entry: entry})
		}
	}
	return out
}

// ── the filter ──────────────────────────────────────────────────────────────

// taskSheetFilter is what has been typed, trimmed. Empty is no filter, which is
// the same bargain [session.SearchTaskIndex] makes with an empty query.
func (a *app) taskSheetFilter() string {
	return strings.TrimSpace(a.taskSheet.query.String())
}

// taskSheetFiltering reports whether the page is being typed at.
func (a *app) taskSheetFiltering() bool { return a.taskSheetFilter() != "" }

// taskSheetKeepLive is the filter over the tree, and IT FLATTENS WHAT IT KEEPS.
//
// A FILTERED TREE IS A TREE WITH HOLES IN IT. The connectors are a claim about
// what hangs off what, and a query that takes a parent out from between two
// matching rows leaves stems pointing at a row that is no longer drawn. So while
// a filter is on, the top half of the page is a flat list of the live work that
// matches — which is the same answer this page already gives for the record, and
// for the same reason: a person searching is looking for a name, not a shape.
func taskSheetKeepLive(live []railEntry, needle string) []railEntry {
	out := make([]railEntry, 0, len(live))
	for _, e := range live {
		if e.node == nil || !taskSheetNodeMatches(e.node, needle) {
			continue
		}
		out = append(out, railEntry{node: e.node})
	}
	return out
}

// taskSheetNodeMatches asks the query of one live node.
//
// THE ID IS AN EXACT MATCH OR NOTHING, which is [session.TaskMatches]'s own rule
// restated over a node rather than over a row: "7" typed at this page is somebody
// quoting an id, and a fuzzy id answers the wrong task.
func taskSheetNodeMatches(node *taskNode, needle string) bool {
	if strings.TrimSpace(needle) == strconv.FormatUint(node.id, 10) {
		return true
	}
	return session.TaskWordsMatch(node.title, needle)
}

// taskSheetKeepPast is the filter over the record, in the record's own order.
// It RANKS NOTHING: the rows are newest first because that is what the file
// says, and a query is a question about which of them to draw rather than about
// which to draw first.
func taskSheetKeepPast(past []*session.TaskIndexEntry, needle string) []*session.TaskIndexEntry {
	out := make([]*session.TaskIndexEntry, 0, len(past))
	for _, entry := range past {
		if session.TaskMatches(*entry, needle) {
			out = append(out, entry)
		}
	}
	return out
}

// taskSheetKeepAway is the filter over the other windows' work, in the reading's
// own order.
//
// IT ASKS THE TITLE AND NOTHING ELSE. The id is deliberately not matched, which
// is where this parts company with [taskSheetNodeMatches]: ids restart with
// every conversation (session's task_index.go says so on TaskIndexEntry.ID), so
// "7" typed here is somebody quoting a number they read in THIS window, and
// answering it with another window's seventh node would hand them the wrong task
// under the right number. The window's own name is not matched either — a filter
// is a question about work.
func taskSheetKeepAway(away []session.ElsewhereTask, needle string) []session.ElsewhereTask {
	out := make([]session.ElsewhereTask, 0, len(away))
	for _, task := range away {
		if session.TaskWordsMatch(task.Task.Title, needle) {
			out = append(out, task)
		}
	}
	return out
}

// taskSheetTyped is what every edit of the filter ends with: the list has
// changed under the cursor, so the cursor goes back to the first row of it and
// the window with it. A cursor left at item forty of a list that now has three
// is a page a person types one letter into and finds empty.
func (a *app) taskSheetTyped() {
	a.taskSheet.cursor, a.taskSheet.top = 0, 0
	a.taskSheetFollow()
}

// taskSheetForest is the tree half: every family with anything still going,
// drawn WHOLE.
//
// IT NEVER FOLDS, and that is the whole difference between this and the column.
// The column folds because it is thirty cells wide beside a paragraph somebody
// is reading; this page is the frame, and a person who came here came to see the
// shape. A family's SETTLED members are drawn with it for the same reason — a
// tree with its finished branches taken out is a tree whose connectors point at
// nothing.
func (a *app) taskSheetForest() []railEntry {
	var out []railEntry
	for _, tree := range a.railForest() {
		if !a.railTwigLive(tree) {
			continue
		}
		out = a.taskSheetWalk(out, tree, nil)
	}
	return out
}

// taskSheetWalk lays one family out, depth first. It is [app.railWalk] with the
// fold taken out, and the stems are copied down for the same reason: one backing
// array shared between two siblings is the second sibling drawing the first
// one's stems.
func (a *app) taskSheetWalk(out []railEntry, t *railTwig, stems []bool) []railEntry {
	out = append(out, railEntry{node: t.node, stems: stems, root: len(t.kids) > 0})
	for i, kid := range t.kids {
		next := make([]bool, len(stems), len(stems)+1)
		copy(next, stems)
		out = a.taskSheetWalk(out, kid, append(next, i < len(t.kids)-1))
	}
	return out
}

// taskSheetPast is the flat half: the project's record, newest first, minus
// whatever the tree above is already showing.
//
// THE ROWS ARE THE INDEX'S OWN ORDER and nothing re-sorts them. internal/session
// sorts on when the work landed and files a running node as NOW (task_index.go's
// taskIndexAt), so newest-first is already true of the slice as it arrives.
func (a *app) taskSheetPast(live []railEntry) []*session.TaskIndexEntry {
	shown := make(map[uint64]string, len(live))
	for _, e := range live {
		if e.node != nil {
			shown[e.node.id] = e.node.title
		}
	}
	out := make([]*session.TaskIndexEntry, 0, len(a.comp.tasks))
	for i := range a.comp.tasks {
		entry := &a.comp.tasks[i]
		if entry.Live() && a.recordRuns(entry) {
			// EVERYTHING THAT IS ACTUALLY RUNNING IS IN THE SECTION ABOVE. This
			// session's own live rows are read off the very graph the tree is drawn
			// from ([session.Agent.TaskIndex] merges them in), and another window's
			// are drawn as its own rows beside that tree — either way a copy down
			// here would be the same work said twice.
			//
			// A LIVE-LOOKING ROW THAT NOTHING IS RUNNING FALLS THROUGH ON PURPOSE.
			// It used to be dropped here with the rest, which took a task some
			// window was killed in the middle of off every surface this program
			// has: the file went on saying `running`, nothing believed it, and
			// nobody was ever told the work had stopped. It belongs in the record,
			// which is what it is, and it says [taskRecordStoppedWord].
			continue
		}
		if title, drawn := shown[taskSheetEntryID(entry.ID)]; drawn && taskSheetSameWork(title, entry) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// taskSheetEntryID reads a record row's id back as the number this session's
// graph keys its nodes by, or 0 for a row whose id is not one.
func taskSheetEntryID(id string) uint64 {
	value, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// taskSheetSameWork reports whether a record row and a node of this session's
// graph are the same piece of work.
//
// IT ASKS THE TITLE AS WELL AS THE ID, because an id is not unique across the
// file: ids restart with every conversation, so "#7" names one node in this
// session and a different one in every conversation before it (task_index.go
// says so on [session.TaskIndexEntry.ID]). Two conversations that ran a task
// numbered 7 with the same words ran the same errand twice, and showing one row
// for it is an understatement rather than a claim about work that never
// happened.
func taskSheetSameWork(title string, entry *session.TaskIndexEntry) bool {
	want := strings.TrimSpace(strings.ToLower(title))
	if want == "" {
		return false
	}
	for _, words := range []string{entry.Title, entry.Label} {
		if strings.TrimSpace(strings.ToLower(words)) == want {
			return true
		}
	}
	return false
}

// taskSheetNodeFor is the live node a record row names, or nil for work another
// conversation ran. It is what decides whether enter opens a room or writes a
// mention ([app.taskSheetEnter]).
func (a *app) taskSheetNodeFor(entry *session.TaskIndexEntry) *taskNode {
	if entry == nil {
		return nil
	}
	node := a.tasks[taskSheetEntryID(entry.ID)]
	if node == nil || !taskSheetSameWork(node.title, entry) {
		return nil
	}
	return node
}

// ── the keyboard ────────────────────────────────────────────────────────────

// taskSheetKeyPress is this page's whole claim on the keyboard: the one chord
// that OPENS it while it is closed, and every key while it is up.
//
// The guard while it is closed is the precedence law input.go states, restated
// rather than relied on because those keys are that file's: the door, the
// question the SESSION is blocked on, the modal overlays and the typed lists all
// outrank a page of work. ctrl+c is read above this and stays the door.
func (a *app) taskSheetKeyPress(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if !a.taskSheet.open {
		if key != taskSheetKey {
			return nil, false
		}
		switch {
		case a.asking(), a.awaitingTask(), a.copy.on, a.rew.on, a.rewSheet.open, a.welcome.open,
			a.menu.open, a.comp.open, a.guarding(), a.stopping():
			return nil, false
		}
		// THE KEY FALLS THROUGH WHEN THERE IS NOTHING TO SHOW rather than raising
		// an empty page, which is ctrl+t's own rule for the same reason
		// ([app.railKey]): a chord that answers with nothing is a chord a person
		// cannot tell they pressed.
		if !a.openTaskSheet() {
			return nil, false
		}
		// The record is re-read on the way in, so a page opened an hour into a
		// session is not showing an hour-old file (taskmention.go's
		// [app.refreshTasks] keeps it fresh from there on).
		return a.loadTasks(), true
	}

	defer a.touch()
	// THE CARD IS A MODE OF THIS PAGE AND IT TAKES THE KEYS FIRST. It is drawn
	// over the list, so every key while it is up belongs to it — including esc,
	// which backs out one layer to the list rather than closing the page
	// (taskrecord.go).
	if a.taskSheet.detailOn {
		return a.taskCardKey(key), true
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
		a.closeTaskSheet()
		return nil, true
	case taskSheetKey:
		// The chord that opened this is the chord that closes it — the roster's own
		// bargain with ctrl+t — and it closes it from inside a filter as well,
		// because a chord is not a layer a person is standing in.
		a.closeTaskSheet()
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
		a.taskSheet.cursor = taskSheetClamp(a.taskSheetItems(), 0)
	case "end":
		items := a.taskSheetItems()
		a.taskSheet.cursor = taskSheetClamp(items, len(items)-1)
	case "enter":
		return a.taskSheetEnter(), true

	// ── the filter's own edits, in the settings panel's spelling ──────────────
	case "backspace":
		a.taskSheet.query.deleteBackward()
		a.taskSheetTyped()
	case "ctrl+u":
		a.taskSheet.query.reset()
		a.taskSheetTyped()
	case "ctrl+w":
		a.taskSheet.query.deleteWord()
		a.taskSheetTyped()

	default:
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
	return nil, true
}

// taskSheetMove walks the rows, stepping over the section words and clamping at
// both ends the way every other list on this surface does ([moveCursor]).
func (a *app) taskSheetMove(delta int) {
	items := a.taskSheetItems()
	if len(items) == 0 {
		return
	}
	at := taskSheetClamp(items, a.taskSheet.cursor)
	step := 1
	if delta < 0 {
		step = -1
	}
	for i := 0; i < abs(delta); i++ {
		next := at
		for {
			next += step
			if next < 0 || next >= len(items) {
				next = at
				break
			}
			if items[next].pick() {
				break
			}
		}
		if next == at {
			break
		}
		at = next
	}
	a.taskSheet.cursor = at
}

// taskSheetCurrent is the row under the cursor, or false on an empty page.
func (a *app) taskSheetCurrent() (taskSheetItem, bool) {
	items := a.taskSheetItems()
	at := taskSheetClamp(items, a.taskSheet.cursor)
	if at < 0 || at >= len(items) || !items[at].pick() {
		return taskSheetItem{}, false
	}
	return items[at], true
}

// taskSheetEnter is the one activating key, and it opens the door that EXISTS
// for the row under it.
//
// A NODE THIS SESSION HOLDS HAS A ROOM, and the room is what every other list of
// work on this surface opens: the roster's enter, a strip chip, a spawn card and
// a `task 7` link all land in the same place ([app.railEnter]), and a second way
// to look at one task would be a second thing to learn.
//
// WORK ANOTHER CONVERSATION RAN HAS NO ROOM, and it never will: a room is a live
// lane onto a node this session's graph is holding, and that conversation closed.
// What it has instead is the CARD (taskrecord.go) — everything the project wrote
// down about that piece of work and the last thing the node itself said, drawn
// over this page with the list still underneath.
//
// IT USED TO WRITE A MENTION HERE, and that was answering the wrong question.
// Pressing a row of finished work means "show me what this did"; the mention
// points the MODEL at it, which is a different errand and one a person had to
// write a sentence around and pay a turn for. The mention did not go away — it
// is `m` on the card, named in the card's own foot.
func (a *app) taskSheetEnter() tea.Cmd {
	item, ok := a.taskSheetCurrent()
	if !ok {
		return nil
	}
	node := item.node
	if node == nil {
		node = a.taskSheetNodeFor(item.entry)
	}
	if node == nil {
		return a.taskSheetInside(item.entry)
	}
	a.closeTaskSheet()
	if node.run != "" {
		a.openOrchRoom(node.run, node.node)
	} else {
		a.openRoomFor(node.id, node.title)
	}
	return a.takeRoomPump()
}

// taskSheetInside opens the card over one row of the record: the page stays up
// and the list stays underneath, which is the whole of what makes this a MODE
// rather than a fourth fullscreen surface (taskrecord.go says why).
//
// It reads the journal off the loop, which is the command it hands back.
func (a *app) taskSheetInside(entry *session.TaskIndexEntry) tea.Cmd {
	if entry == nil {
		return nil
	}
	a.taskSheet.detail, a.taskSheet.detailOn = *entry, true
	a.taskSheet.detailTop, a.taskSheet.tail, a.taskSheet.tailRead = 0, "", false
	return a.readTaskTail(*entry)
}

// ── the pointer ─────────────────────────────────────────────────────────────

// taskSheetHitKind is what one screen row of the page answers to a click.
type taskSheetHitKind uint8

const (
	taskSheetHitNone taskSheetHitKind = iota
	taskSheetHitRow
	// phone lane: taskSheetHitBar is the foot at [tierPhone], where the key
	// legend becomes a `‹ back` band a thumb leaves by (taskphone.go).
	taskSheetHitBar
)

type taskSheetHit struct {
	kind  taskSheetHitKind
	index int
}

// taskSheetPress is a click inside the page: a row opens, and anything else does
// nothing.
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
	width, height := a.size()
	_, hits, _, _ := a.taskSheetFrame(width, height)
	if y < 0 || y >= len(hits) {
		return nil
	}
	// phone lane: the foot is a `‹ back` band rather than a key legend, so a press
	// on it is the way out (taskphone.go).
	if hits[y].kind == taskSheetHitBar {
		a.taskSheetBarPress(x)
		return nil
	}
	if hits[y].kind != taskSheetHitRow {
		return nil
	}
	a.taskSheet.cursor = hits[y].index
	return a.taskSheetEnter()
}

// taskSheetHover records which row the pointer is over, repainting only when the
// answer changed (hover.go's rule, applied to this page).
func (a *app) taskSheetHover(y int) {
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
		return
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
		return
	}
	_, hits, _, _ := a.taskSheetFrame(width, height)
	next := hoverAt{}
	if y >= 0 && y < len(hits) && hits[y].kind == taskSheetHitRow {
		next = hoverAt{kind: hoverTaskSheet, index: hits[y].index}
	}
	if next == a.hot {
		return
	}
	a.hot = next
	a.touch()
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

// taskSheetFrame is the whole screen while the page is open: exactly height
// rows, what each of them answers to the pointer, and where the caret sits.
//
// It is ONE function for [app.sheetFrame]'s reason: the frame draws these rows
// and the pointer resolves against them, and two answers to "where is the
// running section" is how a click opens the wrong task.
//
// The caret is reported as (0, 0) and never moves, because nothing on this page
// is typed into. It is returned all the same so the page plugs into view.go's
// [app.frame] beside the two sheets that do.
func (a *app) taskSheetFrame(width, height int) ([]string, []taskSheetHit, int, int) {
	// THE CARD IS DRAWN INSTEAD OF THE LIST, not over the top of it. It is a mode
	// of this page and it takes the whole of the page's frame, so the rows below
	// are not built at all while it is up — and the hits it returns are its own,
	// mapped through here so that view.go plugs into one function either way
	// (taskrecord.go).
	// It answers NO HITS OF ITS OWN. The card's rows are resolved against the
	// card's own frame ([app.taskCardPress]), and a list hit reported for a row
	// the list did not draw is exactly how a click opens the wrong task.
	if a.taskSheet.detailOn {
		lines, _, caretX, caretY := a.taskCardFrame(width, height)
		return lines, nil, caretX, caretY
	}
	pal := a.pal
	lines := make([]string, 0, height)
	hits := make([]taskSheetHit, 0, height)
	add := func(text string, hit taskSheetHit) {
		lines = append(lines, text)
		hits = append(hits, hit)
	}

	items := a.taskSheetItems()
	a.taskSheet.cursor = taskSheetClamp(items, a.taskSheet.cursor)

	add(a.taskSheetTitle(width), taskSheetHit{})
	add("", taskSheetHit{})
	add(pal.dim(rule(width)), taskSheetHit{})

	// The foot is three rows and it is spoken for before the list is: a rule, the
	// tally, and the keys. A FOURTH JOINS THEM WHILE A FILTER IS ON, because what
	// was typed has to be on screen — a list that has lost rows for a reason a
	// reader cannot see is a list that has lost them for no reason at all
	// (settings.go's [sheetTitle] says the same thing about its own search).
	foot := 3
	if a.taskSheetFiltering() {
		foot++
	}
	head := len(lines)
	room := height - head - foot
	if room < 1 {
		room = 1
	}

	a.taskSheet.top = a.taskSheetTop(items, a.taskSheet.cursor, a.taskSheet.top, room, width)
	for at := a.taskSheet.top; at < len(items) && len(lines)-head < room; at++ {
		item := items[at]
		hit := taskSheetHit{}
		if item.pick() {
			hit = taskSheetHit{kind: taskSheetHitRow, index: at}
		}
		for _, text := range a.taskSheetItemRows(item, at, width) {
			if len(lines)-head >= room {
				break
			}
			add(text, hit)
		}
	}
	for len(lines)-head < room {
		add("", taskSheetHit{})
	}

	add(pal.dim(rule(width)), taskSheetHit{})
	add(" "+pal.dim(fit(a.taskSheetTally(items), width-2)), taskSheetHit{})
	if a.taskSheetFiltering() {
		add(" "+pal.dim(fit(a.taskSheetFilterLine(items), width-2)), taskSheetHit{})
	}
	// phone lane: the key legend becomes a `‹ back` band a thumb leaves by
	// (taskphone.go). The count above it stays — a bar is the way out, and the
	// tally is what the page is holding.
	if layoutTier(width) == tierPhone {
		line, _ := a.taskSheetBar(width)
		add(line, taskSheetHit{kind: taskSheetHitBar})
	} else {
		add(" "+pal.dim(fit(a.taskSheetKeysLine(), width-2)), taskSheetHit{})
	}

	// A terminal too short for the whole page keeps its head and its foot: what
	// this is, and how to leave. It is [app.sheetFrame]'s own trim, for the same
	// reason.
	if len(lines) > height && height > 1 {
		lines = append(lines[:1], lines[len(lines)-(height-1):]...)
		hits = append(hits[:1], hits[len(hits)-(height-1):]...)
	}
	return lines, hits, 0, 0
}

// taskSheetTop follows the cursor with the window. On a wide frame an item is a
// line and it is [listTop] with one correction: a section's word is drawn above
// the first row of its section, so a cursor that has just stepped onto that first
// row scrolls its heading in with it rather than leaving the row under a rule
// that says nothing.
//
// AT [tierPhone] AN ITEM IS TWO LINES, so counting items as lines undershoots —
// [listTop] would believe six two-line cards fit in six rows and leave the cursor
// off the bottom. So the phone window is found by walking BACK from the cursor,
// summing the lines each item actually draws ([app.taskSheetItemLines]), and
// stopping at the topmost item that still leaves the cursor's whole card on
// screen. That is the only arithmetic that keeps a card a thumb scrolled to from
// being clipped at the fold.
func (a *app) taskSheetTop(items []taskSheetItem, cursor, top, room, width int) int {
	if layoutTier(width) != tierPhone {
		top = listTop(cursor, top, len(items), room)
		if cursor > 0 && cursor == top && items[cursor-1].heading() {
			return cursor - 1
		}
		return top
	}
	if len(items) == 0 {
		return 0
	}
	// Scroll UP if the cursor has walked above the window.
	if cursor < top {
		top = cursor
	}
	// Scroll DOWN just enough that the cursor's whole card sits on screen: sum the
	// lines from the cursor upward and stop where the next item would spill.
	used, at := 0, cursor
	for at >= 0 {
		used += a.taskSheetItemLines(items[at], width)
		if used > room {
			at++
			break
		}
		at--
	}
	if at < 0 {
		at = 0
	}
	if at > top {
		top = at
	}
	if top > cursor {
		top = cursor
	}
	if cursor > 0 && top == cursor && items[cursor-1].heading() {
		top = cursor - 1
	}
	return top
}

// taskSheetItemLines is how many screen lines one item draws, which is what the
// phone window sums to keep the cursor's card whole. It asks the row builder with
// a cursor no row can match, so the count is the content's and not the
// selection's — the band never changes how many lines a row takes.
func (a *app) taskSheetItemLines(item taskSheetItem, width int) int {
	return len(a.taskSheetItemRows(item, -1, width))
}

// taskSheetTitle is the head: what this is on the left, and how to leave on the
// right.
func (a *app) taskSheetTitle(width int) string {
	left := " " + a.pal.bold(a.pal.ink(taskSheetWord))
	right := "esc close "
	gap := width - ansi.StringWidth(" "+taskSheetWord) - ansi.StringWidth(right)
	if gap < 1 {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", gap) + a.pal.dim(right)
}

// taskSheetTally is the one line of aggregate at the foot: how much is on this
// page, in the same two words the sections are headed with.
//
// THE EMPTINESS LAW HOLDS HERE TOO. A section with nothing in it is not counted
// as zero — it is not mentioned, and the line is empty when the page is.
func (a *app) taskSheetTally(items []taskSheetItem) string {
	now, past := 0, 0
	section := ""
	for _, item := range items {
		if item.heading() {
			section = item.head
			continue
		}
		if section == taskSheetNowHead {
			now++
			continue
		}
		past++
	}
	var segs []string
	if now > 0 {
		segs = append(segs, itoa(now)+" "+taskSheetNowHead)
	}
	if past > 0 {
		segs = append(segs, itoa(past)+" "+taskSheetPastHead)
	}
	return strings.Join(segs, railSep)
}

// taskSheetFilterLine is what was typed, said back where a person is already
// reading the tally — and, when the query has emptied the page, the one clause
// that stops a blank list reading as a page that broke.
func (a *app) taskSheetFilterLine(items []taskSheetItem) string {
	line := taskSheetFilterWord + a.taskSheetFilter()
	if len(items) == 0 {
		line += taskSheetFilterNone
	}
	return line
}

// taskSheetKeysLine names the keys, and it names the one enter actually has on
// the row under the cursor. A foot that promised a room over work that has none
// would be the page lying about its own door.
//
// WHILE A FILTER IS ON IT NAMES WHAT esc DOES, because that is the key whose
// meaning just moved: it clears the filter first and closes the page second
// ([app.taskSheetKeyPress]), and a foot still reading "esc close" would be the
// page lying about the next keystroke instead of about enter.
func (a *app) taskSheetKeysLine() string {
	if a.taskSheetFiltering() {
		return taskSheetFilterKeys
	}
	item, ok := a.taskSheetCurrent()
	switch {
	case !ok:
		return taskSheetReadKeys
	case item.node != nil, a.taskSheetNodeFor(item.entry) != nil:
		return taskSheetRoomKeys
	}
	return taskSheetInsideKeys
}

// taskSheetItemRows draws one row of the page, selection and hover included.
func (a *app) taskSheetItemRows(item taskSheetItem, at, width int) []string {
	if item.heading() {
		// A SECTION'S WORD IS A RULE AND NOT A ROW. It answers to nothing, so it
		// takes neither the band nor the hover step, and it is drawn in the same
		// dim the completion's own section rules are (taskmention.go).
		return []string{" " + a.pal.dim(item.head)}
	}
	room := width - 2
	if room < 1 {
		room = 1
	}
	// phone lane: the flat rows become two-line cards a thumb goes into
	// (taskphone.go). The running tree's own rows are already a card's shape — a
	// title with what it is doing under it — so they are drawn the same way at
	// every width.
	phone := layoutTier(width) == tierPhone
	var rows []string
	switch {
	case item.node != nil:
		rows = a.taskSheetNodeRows(item, room)
	case item.away != nil:
		if phone {
			rows = a.taskSheetPhoneAway(*item.away, room)
		} else {
			rows = []string{a.taskSheetAwayRow(*item.away, room)}
		}
	default:
		if phone {
			rows = a.taskSheetPhonePast(item.entry, room)
		} else {
			rows = []string{a.taskSheetPastRow(item.entry, room)}
		}
	}
	selected := at == a.taskSheet.cursor
	hovered := a.hot.kind == hoverTaskSheet && a.hot.index == at
	out := make([]string, 0, len(rows))
	for _, text := range rows {
		text = "  " + text
		// SELECTED OUTRANKS HOVERED, which is the law every list on this surface
		// states: the two backgrounds cannot nest, and of the two facts "this is
		// where you are" is the one still true when the pointer moves away.
		switch {
		case selected:
			text = a.pal.selected(text, width)
		case hovered:
			text = a.hoverRow(text, width)
		}
		out = append(out, text)
	}
	return out
}

// taskSheetNodeRows is one node of the tree, in FULL: its connectors, its state,
// its name and its handle, and under it what it is doing and what it has spent.
//
// THE BLOCK UNDER THE TITLE IS NOT EARNED HERE, and that is the difference from
// the column. The roster hands it only to the rows that are still going
// ([app.railSaysMore]), because thirty cells beside a paragraph cannot afford a
// second column of history; this page has the frame, and a person who opened it
// came for exactly those two lines.
func (a *app) taskSheetNodeRows(item taskSheetItem, width int) []string {
	node := item.node
	prefix, at := a.railPrefix(item.stems)
	glyph := a.railTreeGlyph(node)
	lead := glyph + " "
	if !item.root && len(item.stems) == 0 {
		// A ROOTLESS ROW OPENS WITH TWO GLYPHS, the column's own grammar
		// ([app.railEntryRows]): the state, which changes, and the node's own
		// identity mark, which never does.
		lead = glyph + " " + a.taskMark(node.ident) + " "
	}
	room := width - at - ansi.StringWidth(lead)
	meta := railMetaWord(node)
	if room-ansi.StringWidth(meta)-1 < railTitleFloor {
		meta = ""
	}
	if meta != "" {
		room -= ansi.StringWidth(meta) + 1
	}
	if room < 1 {
		room = 1
	}
	title := fit(node.title, room)
	line := prefix + lead + a.railTitle(node, title)
	if meta != "" {
		if pad := room - ansi.StringWidth(title) + 1; pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		line += a.pal.dim(meta)
	}
	rows := []string{line}
	// The stem is painted, so its cells are measured with [ansi.StringWidth],
	// which counts what is drawn and not the escape sequence in front of it.
	stem := a.taskSheetUnderStem(item)
	for _, under := range a.railUnder(node, width-ansi.StringWidth(stem)) {
		rows = append(rows, stem+under)
	}
	return rows
}

// taskSheetUnderStem is what a node's detail block is drawn behind: the same
// continued stems the column uses ([app.railUnderStem]), so the vertical line
// the eye follows down a family is not broken by the two lines hanging off one
// of its rows.
func (a *app) taskSheetUnderStem(item taskSheetItem) string {
	return a.railUnderStem(railEntry{node: item.node, stems: item.stems, root: item.root})
}

// taskSheetPastRow is one row of the project's record: its state, the mention
// mark, its name, and how long ago it landed.
//
// IT IS THE "@" LIST'S OWN ROW ([taskRowLabel] and [taskNoteWord],
// taskmention.go) and not a second rendering of the same facts. A person who has
// picked a task out of the drop-up has already learned these glyphs, and the two
// lists of the project's work must not disagree about what a finished task looks
// like.
//
// A ROW THAT CLAIMS TO BE RUNNING IS DRAWN AS RUNNING ONLY WHEN IT IS. The claim
// is the file's ([app.recordRuns] judges it), and the row says one of three
// things: the ordinary record grammar for work that landed, the LIVE grammar —
// undulled, in the ink a running row wears — for work another window is still
// holding, and [taskRecordStoppedWord] for a claim nothing is behind.
func (a *app) taskSheetPastRow(entry *session.TaskIndexEntry, width int) string {
	runs := a.recordRuns(entry)
	label := taskRecordLabel(*entry, a.pal.ascii, runs)
	note := taskRecordNote(*entry, runs)
	room := width
	if note != "" {
		room -= ansi.StringWidth(note) + 1
	}
	if room < 1 {
		room = 1
	}
	label = fit(label, room)
	// THE HUE IS THE CLAIM AND THE GLYPH IS THE CLAIM: dulled means this is the
	// record, and a row that is genuinely running is not the record. The ink is
	// the one a running node's title wears in the column ([app.railTitle]), so a
	// person who has watched work run recognizes it here without learning a
	// second signal.
	line := a.pal.muted(label)
	if runs {
		line = a.pal.ink(label)
	}
	if note != "" {
		if pad := room - ansi.StringWidth(label) + 1; pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		line += a.pal.dim(note)
	}
	return line
}

// taskRecordLabel is [taskRowLabel] with the liveness judgement folded in.
//
// A ROW NOTHING IS RUNNING DOES NOT WEAR THE RUNNING GLYPH. [glyphIdle] takes
// its place — the dot this surface already spends on a call that was still going
// when its turn ended, chosen there for exactly the reason it is right here: a
// frozen spinner would claim the work is alive, and a dot claims nothing.
func taskRecordLabel(entry session.TaskIndexEntry, ascii, runs bool) string {
	words := entry.Label
	if words == "" {
		words = entry.Title
	}
	glyph := taskStatusGlyph(entry, ascii)
	if entry.Live() && !runs {
		glyph = glyphIdle
		if ascii {
			glyph = glyphIdleASCII
		}
	}
	return glyph + " " + mentionMark(ascii) + " " + words
}

// taskRecordNote is the dim tail on a record row: one word about a live-looking
// claim, and the age for everything that landed.
//
// THE AGE IS NOT DRAWN FOR EITHER LIVE CASE, and that is the emptiness law
// rather than a shortage of room. [taskNoteWord] measures a live row by
// [session.TaskIndexEntry.DurationMS], which is written when the work LANDS —
// so it is zero on every row that has not, and a tail reading `0s` beside a task
// that has been going for an hour is a number worse than no number.
func taskRecordNote(entry session.TaskIndexEntry, runs bool) string {
	switch {
	case entry.Live() && runs:
		return taskRecordRunsWord
	case entry.Live():
		return taskRecordStoppedWord
	}
	return taskNoteWord(entry)
}

// taskStateWord is WHAT ONE ROW OF THE RECORD IS, in a person's words: the
// state the work came home in, or the judgement about a claim of running.
//
// IT IS ONE FUNCTION BECAUSE THREE SURFACES SAY IT. Home's task lines
// ([homeTaskWord]), the record card's first line ([app.taskCardWhenLine],
// taskrecord.go) and this file's own tail all answer the same question about the
// same row, and three spellings of "needs your look" is three chances for two
// screens to disagree about one finished task.
//
// The vocabulary is the surface's and not the engine's: `needs your look` where
// the code says TaskUnverified (task.go states that law at [taskUnverifiedWord]),
// and `incomplete` for a claim of running with nothing behind it — which is not
// a judgement about the work, only the fact that the window went.
func taskStateWord(entry session.TaskIndexEntry, runs bool) string {
	switch {
	case entry.Live() && runs:
		return taskRecordRunsWord
	case entry.Live():
		return taskRecordStoppedWord
	case entry.Status == string(session.TaskFailed):
		return doneFailWord
	case entry.Status == string(session.TaskUnverified):
		return taskUnverifiedWord
	}
	return doneWord
}

// taskSheetAwayRow is one piece of work another window has out: the state it is
// in, the words it was given, and the window it is happening in.
//
// IT WEARS NO MENTION MARK, which every other row on this page does. The mark is
// a promise that "@" reaches this task ([taskRowLabel] puts it on the record's
// rows because it does), and work that has not landed has no row in the project
// index for a mention to resolve against — so the mark would be an offer this
// page cannot keep.
func (a *app) taskSheetAwayRow(away session.ElsewhereTask, width int) string {
	glyph := taskStatusGlyph(session.TaskIndexEntry{Status: away.Task.State}, a.pal.ascii)
	note := taskAwayNote(away.Session)
	room := width
	if note != "" {
		room -= ansi.StringWidth(note) + 1
	}
	if room < 1 {
		room = 1
	}
	label := fit(glyph+" "+away.Task.Title, room)
	line := a.pal.ink(label)
	if note != "" {
		if pad := room - ansi.StringWidth(label) + 1; pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		line += a.pal.dim(note)
	}
	return line
}

// ── the one door the column has onto this page ──────────────────────────────

// THE COLUMN IS THIS CONVERSATION'S WORK AND THE PROJECT'S RECORD IS THIS PAGE'S,
// and the two are joined by exactly one dim line.
//
// It was not always so. The column carried a footnote of the record under its
// live rows — at most six flat rows, dulled, walkable, each a door into a card —
// and it was put there to solve a real problem: a person opening aforge in a
// directory they had worked in for a month saw a column saying "no tasks yet",
// and nothing on the frame suggested that a month of finished work was one chord
// away. The footnote made the work visible and cost more than it was worth. Six
// rows out of two thousand is not a record, it is a sample; the sample stood
// where the column's own empty label goes, so the column stopped being able to
// say what it was for; and the roster's cursor walked out of this conversation's
// work into somebody else's without the column ever saying it had.
//
// SO THE VISIBILITY IS KEPT AND THE ROWS ARE NOT. The foot of the column carries
// [taskSheetPastHint] — `ctrl+. — earlier`, dim, one line — whenever the project
// has a record behind it, and that line is the door: it names the page, it says
// what is on it, and it is pressed as readily as it is typed (task.go's
// [app.railFootRows] draws it, [app.railPress] answers it). Everything the
// footnote used to offer is on the other side of it, whole: every row, the
// filter, the cards, the mention.

// railHasRecord reports whether the project has finished work this session did
// not run — which is the ONLY question the column asks of the record, and the
// question that decides whether its foot carries a door (task.go's
// [app.railFootRows]).
//
// THE MEMBERSHIP RULE IS THE PAGE'S ([app.taskSheetNodeFor]): a row whose id and
// title name a node of this session's graph is that node, and the column is
// already drawing it — so a directory whose whole record is this conversation's
// own work has nothing behind a door and is offered none.
//
// IT SHORT-CIRCUITS ON THE FIRST ROW IT FINDS, which is what makes it cheap
// enough to ask on every frame: the record runs to two thousand rows and the
// common answer costs one comparison rather than a walk of all of them.
func (a *app) railHasRecord() bool {
	for i := range a.comp.tasks {
		if a.taskSheetNodeFor(&a.comp.tasks[i]) == nil {
			return true
		}
	}
	return false
}

// mentionTask writes "@<slug>" into the draft, which is the one door work
// another conversation ran has ever had (taskmention.go mints the pointer block
// carrying its outcome, its branch and its transcript when the message is sent).
//
// IT APPENDS RATHER THAN REPLACING, with one space in front of it when the
// sentence already has words: the box is where a person was part-way through
// saying something, and a list that emptied it to hand back a name would have
// thrown away the sentence the name was for.
//
// IT IS OFFERED FROM INSIDE THE CARD AND FROM NOWHERE ELSE — `m`, which the
// card's own foot names (taskrecord.go). The column used to offer it too, from
// the record rows it no longer draws; one spelling of "put this task in my
// message" is the point, because two would be two ways for the same gesture to
// differ.
func (a *app) mentionTask(entry *session.TaskIndexEntry) {
	if entry == nil {
		return
	}
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = strings.TrimSpace(entry.ID)
	}
	if name == "" {
		return
	}
	text := strings.TrimRight(string(a.input.value), " ")
	if text != "" {
		text += " "
	}
	a.input.setText(text + "@" + name + " ")
	a.touch()
}
