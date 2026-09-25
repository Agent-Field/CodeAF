package tui3

import (
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
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
// message box answers without looking, the roster's alt+t, the column's ctrl+g,
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
	// /history. "/task <brief>" means GIVE codeaf WORK, and it has three rows in
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
func taskAwayNote(name string) string { return taskAwayWord + taskAwayNoteName(name) }

// taskAwayNoteName is the second half of that tail on its own — the separator
// and the name, or nothing at all — because two words now open it: `another
// window`, and [taskOpenHereWord] for a conversation this same terminal holds.
// One rule about the empty case, spelled once.
func taskAwayNoteName(name string) string {
	if name = strings.TrimSpace(name); name == "" {
		return ""
	}
	return railSep + name
}

// ── THE CARD OVER WORK THIS WINDOW CANNOT OPEN ──────────────────────────────
//
// A person who presses one of those rows is asking one question — what do I do
// about this — and the answer is a LABEL AND AN ACTION, not an essay. It was
// four sentences for a while, two of which were claims this surface is in no
// position to make: that the work is running perfectly well, and that it will
// land in this project's history. The first is a guess about another process and
// the second is a promise about a run that may be stopped, may fail, and may be
// in a window that is about to close. Both are gone.
//
// WHAT IS LEFT IS WHAT IS KNOWN: where the work is, and the one thing to do
// about it.
//
// AND THIS CARD IS THE LAST RUNG AND NOT THE FIRST. A row whose conversation
// this terminal is holding, or whose conversation the engine will hand this
// window a second view onto, opens the WORK — taskowner.go's ladder, which
// [tasksPlace.enter] walks before it comes here. The card is what a row gets
// when every one of those rungs is genuinely absent.
const (
	// taskAwayCardNoRoom is the refusal in one clause, said only where there is
	// no door at all — a row another PROCESS is running that this window has no
	// capability to join. A room is a live lane onto a task in a conversation
	// this surface is connected to ([taskRoomAgent], and [taskGuest] for a
	// borrowed one); with neither, going to that window is the way.
	taskAwayCardNoRoom = "go to that window to read it, steer it or stop it."
)

// taskAwayCardWhere is the card's first line: which window is running this.
//
// IT NAMES THE WINDOW IN THE ROW'S OWN WORD ([taskAwayWord]), so the row a
// person pressed and the page it opened say the same thing about the same place.
// A window nothing has named is still a place, and the sentence stops rather
// than trailing off after a colon — [taskAwayNote]'s law said in prose.
//
// IT CLAIMS NOTHING ABOUT HOW THE WORK IS GOING. `running` here is the presence
// file's own word for what that window says it has out, which is the same claim
// the ROW makes; anything further would be this window reporting on a process it
// cannot see.
func taskAwayCardWhere(window string) string {
	if window = strings.TrimSpace(window); window == "" {
		return "this is running in " + taskAwayWord + " on this project."
	}
	return "this is running in " + taskAwayWord + " on this project: " + window + "."
}

// taskOpenHereWord is the note on a row whose conversation THIS TERMINAL is
// holding — one codeaf, several conversations, all of them alive (keeper.go).
//
// IT IS `open` AND NOT `another window`, and the difference is the whole of what
// the row is for. Those conversations write the same presence file every other
// terminal reads, so without this they arrived wearing `another window` and the
// card under them said `go to that window` — about a window that is this one,
// reached with `tab`. The keeper's own person-facing word for the fact is
// `open`, which is what the status line and home already spell.
const taskOpenHereWord = "open in this terminal"

// THE FOOT, AS SCREEN 1e SPELLS IT.
//
// The design draws one line under this place:
//
//	enter open its room · → verbs: run it again, stop it · type to filter · tab next place
//
// Three of its four clauses are built here and the fourth is the router's — it
// appends `alt+. map · tab next place` to every place's sentence ([placeTailed]),
// so a foot that wrote `tab next place` itself would be the frame naming a key
// twice. What is left is the imperative grammar of the design — `open`, `go`,
// `type` — in place of the older third-person one this page used to spell.
//
// AND THE `type to filter` CLAUSE HAS SINCE LEFT THIS LINE FOR THE BOX. The
// design put it here because there was nowhere else for it: the box below said
// `say what you want done` on every place, which on this one was an invitation to
// send a message into a slot that only narrows the list, and a foot clause was the
// only correction available. The box says the true sentence itself now
// ([tasksTypeWord], place_tasks.go's [placeTasks.resting]), so this line stopped
// repeating it — one screen may not name one thing twice. What the foot keeps
// that the box cannot say is `esc clear the filter`, which is only true while a
// filter is on.
//
// EVERY CLAUSE IS TRUE OF THE ROW UNDER THE CURSOR OR IT IS NOT DRAWN. That is
// this surface's own law — no key does anything that is not drawn on screen
// right now, and nothing is named that is not bound (verbstrip.go states it) —
// and it is why the design's one static line is assembled rather than quoted:
// `enter` opens a ROOM only over a node this window is holding, a CARD over work
// another conversation ran, and over work another WINDOW is running the card
// that says where that window is — three doors, three clauses, and the row under
// the cursor decides which of them is said.
const (
	// tasksEnterRoomWord is enter over a node THIS session's graph is holding:
	// the room, which is what every other list of work on this surface opens.
	tasksEnterRoomWord = "enter open its room"
	// tasksEnterInsideWord is enter over work another conversation ran. There is
	// no room to open — a room is a live lane onto a node this session holds, and
	// that conversation closed — so what it opens is the card (taskrecord.go).
	//
	// The design does not spell this clause, because 1e's cursor is on a row this
	// window is running. It is 1e's grammar said about the other door, which is
	// the only honest thing a foot can do over a row that has no room.
	tasksEnterInsideWord = "enter go inside it"
	// tasksEnterAwayWord is enter over work ANOTHER WINDOW is running. There is
	// no room — the node is in another process's graph — and nothing has landed
	// for a mention to point at, so what the key opens is the card that says
	// which window has it and what to do about it (taskrecord.go's away band).
	//
	// THE CLAUSE PROMISES THE ANSWER AND NOT THE WORK, because that is what is on
	// the other side of the key. `enter go inside it` here would be the foot
	// promising a transcript this window cannot reach, which is the same lie as
	// the inert row this clause replaced.
	tasksEnterAwayWord = "enter where it is running"
	// tasksEnterOpenWord is enter over work one of THIS TERMINAL'S OTHER
	// CONVERSATIONS is running. One codeaf holds any number of them and every one
	// is alive (keeper.go), so the row arrived through the same presence reading a
	// stranger's would — and the door is a switch: the conversation on screen is
	// stowed still running, and the one that owns the work comes forward standing
	// in that task's own room.
	//
	// IT NAMES THE CONVERSATION AND NOT THE ROOM, because that is the surprising
	// half. A person who presses this ends up somewhere else, and a foot that said
	// `open its room` would have promised them the page without the journey.
	tasksEnterOpenWord = "enter go to that conversation"
	// tasksEnterJoinWord is enter over work ANOTHER conversation on the engine is
	// running, which this window can join as a second view (taskowner.go's
	// [app.openOwnerRoom]). The page is that task's own transcript, live.
	//
	// IT PROMISES READING AND NOT STEERING. The keyboard for that task belongs to
	// the window that owns it (internal/remote's driver.go), and this view is
	// given it only if the engine says it is going spare — so the clause claims
	// the half that is always true and the box on the page says the rest.
	tasksEnterJoinWord = "enter read it as it runs"
	// tasksVerbsWord opens the design's second clause. What follows it is the
	// verbs that EXIST for the row under the cursor, joined by the design's own
	// comma — and the clause is absent entirely when the row has none.
	tasksVerbsWord = "→ verbs: "
	tasksVerbGap   = ", "
	// tasksClearFilterWord takes that clause's place while a filter is on. It is
	// the one fact the keyboard has that just MOVED — esc clears the filter first
	// and closes the place second ([app.taskSheetKeyPress]) — and the clause it
	// replaces would be teaching a person to do the thing they are already doing.
	tasksClearFilterWord = "esc clear the filter"
	// taskSheetFilterNone is what the note line says when the query has taken
	// every row off the page. A filtered page with nothing on it and nothing said
	// is a page a person reads as broken.
	//
	// IT IS ALL THAT LINE SAYS NOW. It used to open `filter · <what was typed>`,
	// because the box a person typed into was two rows below it and said nothing
	// about narrowing anything — so their own words were echoed back UNDER the
	// rows their keystrokes had just changed. The words are on the control row at
	// the top of the list now ([tasksControlRow]), where the typing lands, and an
	// echo under the list would be the frame saying one thing twice.
	taskSheetFilterNone = "nothing matches"
	// taskSheetPastHint is the line at the bottom of the ROSTER'S COLUMN that
	// reaches this page (task.go's [app.railFootRows]), drawn when what is
	// behind it is the project's own record: any directory that has been worked
	// in before has one.
	//
	// IT WEARS THE NAME OF WHAT IT OPENS. It used to say `view more` as well,
	// when the only thing the column held back was a family it folded; the
	// column folds its own groups now and a press on the heading opens them, so
	// "earlier" is the one thing this line can promise. A permanent "view more"
	// over a month of finished work never told anybody the work existed, which is
	// the whole reason the record was ever footnoted onto the column.
	//
	// It is [taskSheetPastHead]'s own word rather than a second one, because it is
	// the section it lands you in.
	taskSheetPastHint = taskSheetKey + " " + taskSheetPastHead
)

// ── what the other windows have out ─────────────────────────────────────────

// THE PROJECT IS BIGGER THAN THIS WINDOW, and until now this surface could not
// say so.
//
// A person with two codeaf windows open on one directory would start a task in
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

// A SECOND CONVERSATION OF THIS PROCESS USED TO BE LEFT OUT OF THE READING, and
// it is not any more. It writes the same presence file every other terminal
// reads, so it arrived on our own rows as `another window` and the card under it
// said `go to that window` — about a window that is this one, reached with
// `tab`. The reading was therefore narrowed by [app.heldSessions], which removed
// the wrong sentence by removing the ROW, and with it the only sign that a
// conversation open in this terminal had work running in it.
//
// The same list now CLASSIFIES instead of excluding ([tasksMine.here]): the row
// is drawn, it says [taskOpenHereWord], and pressing it switches to that
// conversation and opens the task's room (taskowner.go). One list, the honest
// job.

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
// [app.showTaskPlace] on the way in — a page raised after ten minutes of a
// stowed column must not answer out of a ten-minute-old reading.
func (a *app) refreshElsewhere() {
	if a.away.read && a.now().Sub(a.away.at) < elsewhereEvery {
		return
	}
	a.away = elsewhereCache{at: a.now(), read: true}
	agent, ok := a.agent.(elsewhereAgent)
	if !ok {
		return
	}
	// THE WHOLE READING, MINUS THIS CONVERSATION. The engine leaves the session
	// it is asked from out of its own answer and nothing else
	// (internal/session's [Agent.ElsewhereExcept]), which is exactly right: a
	// surface knows its own graph better than any file, and every other live
	// conversation on this project is something this window has to be able to
	// draw — including the ones this same process is holding.
	a.away.held = agent.Elsewhere()
}

// heldSessions is the session id of every conversation this process holds and is
// not drawing, as a set.
//
// THE ID IS THE SESSION FOLDER'S NAME, which is what [session.Place.ID] answers
// and what the presence file carries — so it is arithmetic on the transcript
// path rather than a question for the agent ([taskSessionOf] spells it once for
// every caller). A legacy flat journal has no folder to name and contributes
// nothing, which costs at most one row saying `another window` about a window
// that is this one, on a session shape that predates presence entirely.
//
// IT NAMES WHAT THIS TERMINAL CAN REACH, which is what makes it a classifier
// rather than a filter: a row in this set is a row whose owner is one `tab`
// away, and taskowner.go's ladder turns that into a door.
func (a *app) heldSessions() map[string]bool {
	if len(a.behind) == 0 {
		return nil
	}
	out := make(map[string]bool, len(a.behind))
	for _, held := range a.behind {
		if id := taskSessionOf(held.conv.SessionFile); id != "" {
			out[id] = true
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
// A ROW HERE HAS NEITHER OF THIS SURFACE'S TWO DOORS ONTO WORK, AND IS PRESSED
// ANYWAY. The doors are a ROOM, which is a live lane onto a node in THIS
// session's graph, and a MENTION, which mints a pointer block out of a landed
// row's outcome, branch and transcript ([app.mentionTask]); work running in
// another window has no node here to open and nothing landed to point at.
//
// What changed is what the row does about that. It used to take no cursor at all
// ([tasksItem.pick] tells the whole story), which left a person aiming at a row
// as visible as its neighbours and getting silence. It now opens the card that
// says which window has the work and what to do about it
// ([app.taskSheetAwayCard]) — the refusal as a page rather than as a keystroke
// that does nothing.
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
// says so on [session.TaskIndexEntry.ID]).
//
// AND THE TITLE IS A SECOND CHECK RATHER THAN THE OWNER CHECK. It was read as
// one for a while — two conversations that ran a task numbered 7 with the same
// words were taken for one errand run twice — and that is exactly the pair a
// family handing the same work out twice produces, so the reading resolved
// another conversation's row to this session's node. The owner is asked first
// now ([app.taskSheetOwnsEntry]) and this is what settles a row whose owner
// nothing can name.
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

// taskSheetOwnsEntry reports whether a row of the record belongs to THE
// CONVERSATION THIS WINDOW IS SITTING IN. [app.taskSheetSelfID] is next door in
// taskowner.go with the rest of the identity arithmetic.
//
// A NAMED OWNER IS BELIEVED, AND AN UNKNOWN SELF IS NOT A LICENCE. The rule is
// two lines and the second one is the whole of the fix:
//
//   - A row that NAMES NO CONVERSATION is this window's by provenance. Nothing
//     else can have written it: the only rows that reach this surface without an
//     owner are the ones [app.taskSheetOwnRows] mints out of THIS session's own
//     graph, before its journal has a folder to be named after
//     ([app.taskIndexHolds] says why a graph row can carry no conversation id).
//   - A row that names one is ours only if it names US. An earlier reading let a
//     window whose own id was unknown claim ANY row, on the argument that a
//     comparison needs two names — and the conclusion it drew from not knowing
//     was the dangerous one. A record that says `this belongs to session X` is a
//     fact; a window that cannot say whether it is X has not contradicted it,
//     and must not act as though it had.
//
// WHAT THAT COSTS, HONESTLY. A conversation whose journal id and whose folder
// name genuinely disagree loses the ROOM on its own landed rows and gets the
// card instead — one door narrower, and never somebody else's task. That is the
// trade, and it is the right way round: the failure this replaced opened a live
// room drawing a different task's transcript.
func (a *app) taskSheetOwnsEntry(entry *session.TaskIndexEntry) bool {
	owner := strings.TrimSpace(entry.SessionID)
	if owner == "" {
		return true
	}
	return owner == a.taskSheetSelfID()
}

// taskSheetNodeFor is the live node a record row names, or nil for work another
// conversation ran. It is what decides whether enter opens a room or writes a
// mention ([app.taskSheetEnter]).
//
// THE OWNER IS ASKED BEFORE THE ID, AND THAT IS THE WHOLE OF THE FIX. Node ids
// restart with every conversation, so `7` names a different piece of work in
// every one of them — and the id-and-title rule below reads a row FROM ANOTHER
// CONVERSATION numbered 7 with the same words as this session's own node 7. The
// title was supposed to be the guard, and it is not one: a family that hands the
// same errand out twice, a task somebody proposed again in a second terminal,
// and a run replayed out of a checkpoint all produce exactly that pair. What
// happened then was the worst shape a navigation defect can take — the row
// opened a room, the room drew a transcript, and the transcript belonged to a
// DIFFERENT task with the same number. A wrong page that looks right is worse
// than no page at all, so the owner vetoes first.
func (a *app) taskSheetNodeFor(entry *session.TaskIndexEntry) *taskNode {
	if entry == nil {
		return nil
	}
	if !a.taskSheetOwnsEntry(entry) {
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
	// taskSheetHitControl is the list's first row — the filter box and the two
	// column labels ([tasksControlRow]). It is its own kind because a press on it
	// is answered by WHICH CELL it landed in and not by a row index, which is the
	// one gesture on this page that needs the column as well as the line.
	taskSheetHitControl
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

// taskStateWord is WHAT ONE ROW OF THE RECORD IS, in a person's words: the
// reading of the row ([taskEntryStatus]) spelled by the one table that spells
// readings ([taskPresenceWord], taskstatus.go).
//
// IT IS ONE FUNCTION BECAUSE TWO SURFACES SAY IT. The record card's first line
// ([app.taskCardWhenLine], taskrecord.go) and this file's own tail answer the
// same question about the same row, and two spellings of "your call" is two
// chances for two screens to disagree about one finished task.
//
// THE READING IS WHERE THE JUDGEMENTS LIVE NOW, and three of them changed when
// it arrived: a row a person stopped says `stopped` rather than `failed`, a run
// the wire or a threshold ended says `incomplete` rather than `failed`, and a
// QUEUED row in a live session says `queued` rather than `running` — acceptance
// and execution are different receipts, and the old word claimed a worker that
// had not started.
func taskStateWord(entry session.TaskIndexEntry, runs bool) string {
	return taskPresenceWord(taskEntryStatus(entry, runs))
}

// ── the one door the column has onto this page ──────────────────────────────

// THE COLUMN IS THIS CONVERSATION'S WORK AND THE PROJECT'S RECORD IS THIS PAGE'S,
// and the two are joined by exactly one dim line.
//
// It was not always so. The column carried a footnote of the record under its
// live rows — at most six flat rows, dulled, walkable, each a door into a card —
// and it was put there to solve a real problem: a person opening codeaf in a
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
