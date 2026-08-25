package tui3

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// WHAT THE TASKS PLACE IS MADE OF, minus the place itself.
//
// The place is three files. tasksplace.go is the READING — pure, and the only
// thing that decides what a row says. place_tasks.go is the PLACE — the cursor,
// the scroll, the keyboard, the pointer and the frame. This file is what is left
// over and belongs to neither:
//
//   - THE WORDS. The page's key, its command's name, the two section words the
//     column's tally already spends, and the sentences its foot offers each hand.
//   - WHAT THE OTHER WINDOWS HAVE OUT, and the ONE ladder that judges a claim of
//     running ([app.recordRuns]) — asked by the column and by the place alike.
//   - THE ROSTER'S SIDE of the join: which node of this session's graph a row of
//     the record names ([app.taskSheetNodeFor]), and the one dim door the column
//     carries onto this place ([app.railHasRecord]).
//
// It exists because the roster answers one question and a person keeps asking
// two. The column beside the conversation is THIS SESSION'S record of its own
// work ([app.railEntries] walks [app.taskOrder] and nothing else), which is the
// right answer to "what is happening" and no answer at all to "what did we do
// about this last week". The machine has that answer on disk — internal/session
// keeps a tasks.jsonl beside the conversations in every project — and until this
// place the only door onto it was the "@" drop-up, which is a completion
// somebody has to already be typing a message to reach.
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
	// taskSheetWord is what the COMMAND is called, in the command list
	// alike, and it is the word the command spells: /history.
	//
	// IT IS NOT SPELLED "tasks", AND THAT IS THE WHOLE OF WHY THE COMMAND IS
	// /history. "/task <brief>" means GIVE AFORGE WORK, and it has three rows in
	// the command list; a "/tasks" beside them narrowed to both on the four
	// characters they share, so the muscle memory for starting work led to a page
	// that starts none. What a person calls this thing is the record of everything
	// the project has run, and "history" is that word.
	taskSheetWord = "history"
	// taskSheetNowHead heads the section of work that is happening. It is
	// [railGroupWords]'s own word rather than a second one, because a person who
	// reads "3 running" at the bottom of the column must not have to learn that
	// this place calls the same thing something else.
	taskSheetNowHead = "running"
	// taskSheetPastHead heads the last section, the one everything that landed
	// before today falls into. "earlier" and not "past": the rows under it are
	// work, in the order it happened, and the word a person uses for the thing
	// that came before this one is the word that goes on it.
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
	taskSheetMoreHint = taskSheetKey + " view more"
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
	taskSheetPastHint = taskSheetKey + " " + taskSheetPastHead
)

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
//   - THE TASKS PLACE'S `running` SECTION gains a row per piece of work another
//     window is holding, beside this session's own, each with the window it
//     belongs to on the right.
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
// IT IS THE ONLY LADDER ON THIS SURFACE. The tasks place settles every row of
// its reading through here (place_tasks.go's [app.taskSheetMine]) and so does
// the column, because a screen that said `running` on a row it had filed under
// `earlier` would be the surface arguing with itself.
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
// ([tasksItem.pick]) and they say what they are for — knowing that the
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

// ── naming one row of the record ────────────────────────────────────────────

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

// ── what one row of the place answers to a hand ─────────────────────────────

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

// ── the words one row of the record wears ───────────────────────────────────

// THE TITLE ROW IS GONE. This page drew `history … esc close` across its own
// head; under the router the tab bar above the rule says which place this is and
// the shared hint line says how to leave, so a title here would be the frame
// naming itself twice (pages.go). [taskSheetWord] survives because it is still
// what the COMMAND is called — `/history` — and the manual quotes it.

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
// [taskSheetPastHint] — `ctrl+. earlier`, dim, one line — whenever the project
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
