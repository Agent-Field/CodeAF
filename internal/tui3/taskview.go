package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE TASK PAGE: /history, ctrl+. , or the column's own "view more" line — the
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
//   - IT READS THE SAME SNAPSHOT THE "@" LIST READS ([app.comp].tasks, loaded by
//     [app.loadTasks]). One read, one cache, one answer to "what has this project
//     run" — a page that fetched its own copy would be a second answer with its
//     own staleness. The COLUMN reads it too now ([app.railRecord]), which is the
//     same one-read law with a third reader on it.
//   - IT IS TYPED AT. Every printable key builds a filter over both sections at
//     once ([app.taskSheetFilter]), because a record of four hundred tasks is
//     reached by remembering a word of the title and by nothing else. esc backs
//     out of the filter first and closes the page second, which is the settings
//     panel's own layering ([app.sheetKey]).
//
// WHAT IT DOES NOT DO is replace the column. The column carries the same record
// under its live rows — dulled, and capped at [railRecordMax] — and this is where
// a person acts on it: the column's record rows answer to nothing, and every door
// onto old work is on this page.

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

// The two lines at the foot, which are what this page offers each hand.
const (
	taskSheetRoomKeys    = "esc close · ↑↓ move · enter opens its room"
	taskSheetMentionKeys = "esc close · ↑↓ move · enter puts it in your message"
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
	// taskSheetMoreHint is the line at the bottom of the ROSTER'S COLUMN that
	// reaches this page (task.go's [app.railFootRows]). It is shaped like the two
	// lines under it — the key, then what it reaches — and it is drawn only when
	// there is genuinely more here than the column is showing
	// ([app.railOffersMore]).
	taskSheetMoreHint = taskSheetKey + " — view more"
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
}

func (i taskSheetItem) heading() bool { return i.head != "" }

// ── opening and closing ─────────────────────────────────────────────────────

// openTaskSheet raises the page, and reports whether it went up.
//
// IT REFUSES ON AN EMPTY PROJECT rather than drawing a page with a title and
// nothing under it. The refusal is the caller's to say — /history writes a line
// and the key falls through in silence — because a command typed on purpose that
// answers with nothing reads as a command that broke, and a chord that was never
// bound in the person's mind reads as a chord that was never bound.
func (a *app) openTaskSheet() bool {
	if !a.taskSheetHasAnything() {
		return false
	}
	// THE OTHER FULLSCREEN PAGE STANDS DOWN. Only one of the two may believe it
	// owns the frame: view.go draws the settings panel first, so a page opened
	// under it would take the keyboard and never be seen.
	if a.sheet.open {
		a.closeSettings()
	}
	a.taskSheet = taskSheet{open: true}
	a.taskSheetFollow()
	a.touch()
	return true
}

func (a *app) closeTaskSheet() {
	a.taskSheet = taskSheet{}
	a.touch()
}

// taskSheetHasAnything reports whether there is a single row to draw.
func (a *app) taskSheetHasAnything() bool {
	if len(a.taskOrder) > 0 {
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

// taskSheetClamp is the cursor rule: never a heading, never off the end. It
// walks forward first and then back, so a cursor that lands on a section's word
// steps onto the first row of that section rather than off the top of the page.
func taskSheetClamp(items []taskSheetItem, at int) int {
	if len(items) == 0 {
		return 0
	}
	at = min(max(at, 0), len(items)-1)
	for i := at; i < len(items); i++ {
		if !items[i].heading() {
			return i
		}
	}
	for i := at; i >= 0; i-- {
		if !items[i].heading() {
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
	past := a.taskSheetPast(live)
	if needle := a.taskSheetFilter(); needle != "" {
		live, past = taskSheetKeepLive(live, needle), taskSheetKeepPast(past, needle)
	}
	out := make([]taskSheetItem, 0, len(live)+len(past)+2)
	// A SECTION WITH NOTHING IN IT IS NOT DRAWN AT ALL, filter or no filter. It is
	// the emptiness law: a `running` rule with a blank under it says the query
	// found something and lost it.
	if len(live) > 0 {
		out = append(out, taskSheetItem{head: taskSheetNowHead})
		for _, e := range live {
			out = append(out, taskSheetItem{node: e.node, stems: e.stems, root: e.root})
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
		if entry.Live() {
			// EVERYTHING LIVE IS IN THE TREE. The index's live rows are read off
			// the very graph the tree above is drawn from
			// ([session.Agent.TaskIndex] merges them in), so a live row down here
			// would be the same node said twice.
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
		case a.asking(), a.awaitingTask(), a.copy.on, a.rew.on, a.welcome.open,
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
			if !items[next].heading() {
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
	if at < 0 || at >= len(items) || items[at].heading() {
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
// What it has instead is the door the "@" list already built — the task's name in
// your message, which mints a pointer block carrying its outcome, its branch and
// its transcript when you send (taskmention.go). So enter writes the mention,
// which is the same errand arriving through the same machinery.
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
		a.taskSheetMention(item.entry)
		return nil
	}
	a.closeTaskSheet()
	if node.run != "" {
		a.openOrchRoom(node.run, node.node)
	} else {
		a.openRoomFor(node.id, node.title)
	}
	return a.takeRoomPump()
}

// taskSheetMention writes the name into the draft and leaves. The writing is
// [app.mentionTask] — the column's record rows do the same thing from the same
// place — and what belongs to the page is the LEAVING: you came here to find a
// task, and the box you were sent back to is where the sentence is.
func (a *app) taskSheetMention(entry *session.TaskIndexEntry) {
	if entry == nil || strings.TrimSpace(entry.Name)+strings.TrimSpace(entry.ID) == "" {
		return
	}
	a.closeTaskSheet()
	a.mentionTask(entry)
}

// ── the pointer ─────────────────────────────────────────────────────────────

// taskSheetHitKind is what one screen row of the page answers to a click.
type taskSheetHitKind uint8

const (
	taskSheetHitNone taskSheetHitKind = iota
	taskSheetHitRow
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
func (a *app) taskSheetPress(y int) tea.Cmd {
	width, height := a.size()
	_, hits, _, _ := a.taskSheetFrame(width, height)
	if y < 0 || y >= len(hits) || hits[y].kind != taskSheetHitRow {
		return nil
	}
	a.taskSheet.cursor = hits[y].index
	return a.taskSheetEnter()
}

// taskSheetHover records which row the pointer is over, repainting only when the
// answer changed (hover.go's rule, applied to this page).
func (a *app) taskSheetHover(y int) {
	width, height := a.size()
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
func (a *app) taskSheetScroll(delta int) { a.taskSheetMove(delta) }

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

	a.taskSheet.top = taskSheetTop(items, a.taskSheet.cursor, a.taskSheet.top, room)
	for at := a.taskSheet.top; at < len(items) && len(lines)-head < room; at++ {
		item := items[at]
		hit := taskSheetHit{}
		if !item.heading() {
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
	add(" "+pal.dim(fit(a.taskSheetKeysLine(), width-2)), taskSheetHit{})

	// A terminal too short for the whole page keeps its head and its foot: what
	// this is, and how to leave. It is [app.sheetFrame]'s own trim, for the same
	// reason.
	if len(lines) > height && height > 1 {
		lines = append(lines[:1], lines[len(lines)-(height-1):]...)
		hits = append(hits[:1], hits[len(hits)-(height-1):]...)
	}
	return lines, hits, 0, 0
}

// taskSheetTop follows the cursor with the window, in ITEMS. It is [listTop]
// with one correction: a section's word is drawn above the first row of its
// section, so a cursor that has just stepped onto that first row scrolls its
// heading in with it rather than leaving the row under a rule that says nothing.
func taskSheetTop(items []taskSheetItem, cursor, top, room int) int {
	top = listTop(cursor, top, len(items), room)
	if cursor > 0 && cursor == top && items[cursor-1].heading() {
		return cursor - 1
	}
	return top
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
		return taskSheetRoomKeys
	case item.node != nil, a.taskSheetNodeFor(item.entry) != nil:
		return taskSheetRoomKeys
	}
	return taskSheetMentionKeys
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
	var rows []string
	if item.node != nil {
		rows = a.taskSheetNodeRows(item, room)
	} else {
		rows = []string{a.taskSheetPastRow(item.entry, room)}
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
			text = a.pal.band(text, width)
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
func (a *app) taskSheetPastRow(entry *session.TaskIndexEntry, width int) string {
	label := taskRowLabel(*entry, a.pal.ascii)
	note := taskNoteWord(*entry)
	room := width
	if note != "" {
		room -= ansi.StringWidth(note) + 1
	}
	if room < 1 {
		room = 1
	}
	label = fit(label, room)
	line := a.pal.muted(label)
	if note != "" {
		if pad := room - ansi.StringWidth(label) + 1; pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		line += a.pal.dim(note)
	}
	return line
}

// ── the column's own record rows ────────────────────────────────────────────

// THE COLUMN CARRIES THE PROJECT'S RECORD TOO, dulled, under everything this
// session is doing.
//
// It did not, and that was the whole feature reading as absent: a person opening
// aforge in a directory they had worked in for a month saw an empty frame, no
// column at all, and therefore no "view more" line and no reason to guess that a
// page existed behind it. The record was one chord away and the chord was
// undiscoverable.
//
// So the column is the project's roster as well as the session's, in two tiers
// that are drawn nothing alike:
//
//   - THIS SESSION'S WORK IS THE COLUMN, as it always was — the forest, the
//     folds, the cursor, the rooms behind enter.
//   - THE RECORD IS A FOOTNOTE UNDER IT: at most [railRecordMax] rows, flat, no
//     connectors, no cursor, no door, drawn in the same muted-and-dim the page's
//     own `earlier` rows are ([app.taskSheetPastRow] draws both, so the two
//     lists cannot disagree about what a finished task looks like).
//
// THE RECORD NEVER EVICTS LIVE WORK. It is filled into the rows the session's
// own list did not need and into no others ([app.railRecordLines]), so a column
// full of running work carries none of it — and the honest answer for a person
// who wants it anyway is the page, which the footer's line names.

// railRecordMax is how many record rows the column may carry.
//
// SIX, AND IT IS A CAP ON A FOOTNOTE RATHER THAN A WINDOW ONTO A FILE. The
// record runs to two thousand rows; a column that showed forty of them would be
// a file browser standing where a glance used to be, and the rows under the
// sixth are what the page is for. It is the most RECENT six, because the index
// arrives newest first and recency is the only order a footnote can carry.
const railRecordMax = 6

// railRecord is the project's record as the column shows it: the rows this
// session is not already holding, newest first, and never more than limit of
// them.
//
// THE MEMBERSHIP RULE IS THE PAGE'S ([app.taskSheetNodeFor]): a row whose id and
// title name a node of this session's graph is that node, and the column is
// already drawing it — folded or not — so repeating it down here would be the
// same work said twice in two different colours.
//
// IT STOPS AT limit, which is what makes it cheap enough to ask on every frame:
// the column asks whether there is ANY record before it decides whether to stand
// at all ([app.railContent]), and that question costs one row rather than a walk
// of two thousand.
func (a *app) railRecord(limit int) []*session.TaskIndexEntry {
	if limit < 1 {
		return nil
	}
	out := make([]*session.TaskIndexEntry, 0, min(limit, len(a.comp.tasks)))
	for i := range a.comp.tasks {
		entry := &a.comp.tasks[i]
		if entry.Live() {
			// EVERYTHING LIVE BELONGS TO THE FOREST ABOVE. The index's live rows are
			// merged in off this session's own graph ([session.Agent.TaskIndex]), and
			// a live row down here would be a running task drawn as history.
			continue
		}
		if a.taskSheetNodeFor(entry) != nil {
			continue
		}
		out = append(out, entry)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// railHasRecord reports whether the project has any work this session is not
// showing — the cheapest form of the question, and the one [app.railShowing]
// asks.
func (a *app) railHasRecord() bool { return len(a.railRecord(1)) > 0 }

// railRecordLines fills what is left of the column's body with the record: a
// blank, the same `earlier` word the page heads its flat list with, and the rows.
//
// IT TAKES ONLY WHAT THE SESSION'S OWN ROWS LEFT BEHIND, and that is the whole
// contract: it is handed the lines already laid out and the height they had to
// fit in, so a column whose running work fills the frame gets no record rows at
// all rather than a record row where a running one was. Nothing here can evict
// anything.
//
// THE ROWS ARE DOORS. They were a note once — dulled, and unreachable by cursor
// or pointer, with the page as the only place to act on them — and that was the
// wrong call twice over: a row a person can read and cannot press is a row they
// press anyway, and the thing they want from it is exactly the thing enter on
// the page already does. So the cursor walks into them ([app.railMove]), the
// pointer lights them, and enter or a click writes "@<slug>" into the message box
// ([app.mentionTask]) — which is the door that EXISTS for work another
// conversation ran, because a room is a live lane onto a node in this session's
// graph and that session is closed.
//
// WHAT DOES NOT CHANGE IS THE PAINT. They stay muted-and-dim under the selection
// band, because dulled is a claim about the WORK — this is the record, not what
// is happening — and not a claim about whether the row answers.
//
// The rows the frame drew are kept in [app.railPast], in drawn order, because
// how many of them fit is a fact only this layout has: the cursor and the
// pointer resolve against that list, which is the same bargain the glyph and
// badge spans make ([railLine]).
//
// AND THEY ARE WHAT AN EMPTY SESSION IN AN OLD PROJECT SHOWS INSTEAD OF THE
// LABEL. The permanent column says "no tasks yet" when it has nothing
// ([railEmptyWord], task.go) — but a directory with a record behind it has
// something, so [app.railView] stands the label down and these rows take the top
// of the column. Two lines saying "no tasks yet" and "earlier · Port the parser"
// one under the other would be the column contradicting itself.
func (a *app) railRecordLines(out []railLine, body, width int) []railLine {
	// THE DRAWN LIST IS CLEARED BEFORE IT IS FILLED, on every frame and however
	// early this returns. A cursor resolving against last frame's rows — after a
	// resize, after a landing that took the leftover space — is a cursor standing
	// on a row that is not on the screen.
	a.railPast = a.railPast[:0]
	left := body - len(out)
	// A RULE WITH NOTHING UNDER IT IS NOT A SECTION. Two rows is the least this
	// block can be: the word, and one task under it.
	if left < 2 {
		return out
	}
	rows := a.railRecord(min(railRecordMax, left-1))
	if len(rows) == 0 {
		return out
	}
	if len(out) > 0 && left > len(rows)+1 {
		// ONE BLANK ABOVE IT where the column can lend one — whitespace is how this
		// surface separates blocks, and it is what the footer already does. There is
		// nothing to separate at the top of the column, though: a conversation that
		// has run nothing in a project that has ([app.railView]'s empty branch stands
		// down for exactly this) opens with the word itself and not with a blank row.
		out = append(out, railLine{entry: -1})
	}
	out = append(out, railLine{text: a.pal.dim(taskSheetPastHead), entry: -1, past: true})
	for _, entry := range rows {
		out = append(out, railLine{
			text: a.taskSheetPastRow(entry, width), entry: -1, past: true, record: entry})
	}
	a.railPast = append(a.railPast, rows...)
	return out
}

// railPastAt is the record row a cursor position names, or nil. It is asked of
// the DRAWN list, so a cursor that a shrinking column has pushed off the bottom
// answers nothing rather than answering about a row nobody can see.
func (a *app) railPastAt(at int) *session.TaskIndexEntry {
	if at < 0 || at >= len(a.railPast) {
		return nil
	}
	return a.railPast[at]
}

// railPastIndex is where the roster's cursor is standing in the record block, or
// -1 when it is not standing there at all.
//
// IT IS RESOLVED BY KEY AND NOT BY POSITION, which is [railSpot]'s own law: the
// record shifts under the cursor whenever a node of this session's lands into it
// or the column's leftover space changes, and an index would follow the shift
// instead of following the work.
func (a *app) railPastIndex() int {
	if a.railWhere.past == "" {
		return -1
	}
	for i, entry := range a.railPast {
		if railPastKey(entry) == a.railWhere.past {
			return i
		}
	}
	return -1
}

// railPastFocus is the record row the cursor is on, or nil — and it is nil
// unless the roster actually HOLDS the keyboard, which is [app.railFocusIndex]'s
// own rule: a marker on a map that keys do not reach is a marker that lies about
// what enter will do.
func (a *app) railPastFocus() *session.TaskIndexEntry {
	if !a.railHold {
		return nil
	}
	return a.railPastAt(a.railPastIndex())
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
// It is ONE function because there are now two lists that offer it — the page's
// `earlier` rows and the column's ([app.railEnter]) — and two spellings of
// "put this task in my message" is two ways for the same gesture to differ.
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
