package tui3

// THE TASKS PLACE IS A TREE OF CONVERSATIONS, AND THIS FILE HOLDS IT TO THAT.
//
// Work does not happen on this machine outside a chat: somebody asks for
// something, the chat cuts it into tasks, and a task cuts itself into more. The
// page used to draw the middle of that sentence and throw both ends away —
// every task as a peer of every other, one level of family and no deeper — so
// eight workers of one conversation and one question asked in another arrived
// as nine equal things, and there was no way back from a piece of work to the
// chat that asked for it.
//
// What these pin is the shape and the four things it must not cost: a
// conversation is never a task, a row is never re-judged by its relatives, a
// record that lies about its own parents cannot hang the page, and nothing is
// invented for a conversation this surface cannot name.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// tasksChatFixture is two conversations, drawn from what the world scan knows.
//
// `shipping the gate` is the one a person has to come back to: one piece of work
// nobody could check, and beside it a piece of work that finished an hour ago
// with two more nested under it — a worker, and a worker OF that worker, which
// is the depth the old page could not draw. `thor clips` finished yesterday and
// has nothing under it but one row.
func tasksChatFixture() (session.World, session.UsageWindow, time.Time) {
	loc := time.FixedZone("fixture", -4*60*60)
	now := time.Date(2026, time.September, 6, 13, 0, 0, 0, loc)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	gate := session.SessionRow{
		ID: "room-a", Title: "Shipping the Gate", Project: "codeaf",
		Transcript: "/journals/room-a/session.jsonl", ProjectDir: "/work/codeaf",
	}
	// Every row carries its title as well as its label, because that is what a
	// query is asked of ([session.TaskMatches]) and these are searched below.
	work := func(id, parent, name string, status session.TaskState, ended time.Time) session.TaskIndexEntry {
		return session.TaskIndexEntry{
			SessionID: "room-a", ID: id, Parent: parent, Label: name, Title: name,
			Name: session.TaskSlug(name), Status: string(status), EndedAt: ended,
		}
	}
	gate.Tasks.Rows = []session.TaskIndexEntry{
		work("1", "", "rotate the certificate", session.TaskUnverified, ago(2*time.Hour)),
		work("2", "", "port the parser", session.TaskDone, ago(time.Hour)),
		work("3", "2", "port the lexer", session.TaskDone, ago(90*time.Minute)),
		work("4", "3", "port the token table", session.TaskDone, ago(100*time.Minute)),
	}
	clips := session.SessionRow{
		ID: "room-b", Title: "Thor Clips", Project: "media",
		Transcript: "/journals/room-b/session.jsonl", ProjectDir: "/work/media",
	}
	clips.Tasks.Rows = []session.TaskIndexEntry{
		{SessionID: "room-b", ID: "1", Label: "render the fight clip", Title: "render the fight clip",
			Name: "render-the-fight-clip", Status: string(session.TaskDone), EndedAt: ago(30 * time.Hour)},
	}
	world := session.World{Projects: []session.Project{
		{Name: "codeaf", Sessions: []session.SessionRow{gate}},
		{Name: "media", Sessions: []session.SessionRow{clips}},
	}, Read: now}
	return world, session.LastDays(now, 14), now
}

func tasksChatReading() tasksReading {
	world, win, now := tasksChatFixture()
	return readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now)
}

// tasksChatApp is that fixture on a surface, with the freshness stamps pinned so
// that a rebuild happens where a test asks for one and never under it.
func tasksChatApp(t *testing.T) *app {
	t.Helper()
	world, win, now := tasksChatFixture()
	a := &app{pal: newPalette(tokens.NoColor, false)}
	a.width, a.height = 120, 40
	a.clock = func() time.Time { return now }
	a.raisePlace(pageTasks)
	a.taskSheet.world = world
	a.taskSheet.reading = readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now)
	a.taskSheet.awayAt, a.taskSheet.mineAt = a.elsewhere().Read, a.railStamp
	a.taskSheet.cursor = a.tasksSettle(0)
	return a
}

// tasksLineOf is the laid-out line one name is drawn on.
func tasksLineOf(t *testing.T, lines []tasksLine, name string) tasksLine {
	t.Helper()
	for _, line := range lines {
		switch line.kind {
		case tasksLineChat:
			if line.chat.title == name {
				return line
			}
		case tasksLineTask:
			if tasksLabel(line.item.entry) == name {
				return line
			}
		}
	}
	t.Fatalf("nothing on the page is called %q", name)
	return tasksLine{}
}

// tasksPointAt parks the cursor on the line one name is drawn on, OPENING
// WHATEVER FOLD IS OVER IT — which is what a person does with `→` on the way to
// the row they came for, now that every conversation and every family opens shut
// ([tasksReading.opens]). A test that is ABOUT a fold looks at the layout itself
// rather than asking for a cursor.
func tasksPointAt(t *testing.T, a *app, name string) int {
	t.Helper()
	if at, found := tasksCursorOn(a, name); found {
		return at
	}
	openTaskFolds(a)
	if at, found := tasksCursorOn(a, name); found {
		return at
	}
	width, _ := a.size()
	t.Fatalf("no row on the page is called %q:\n%s", name, tasksPage(a.tasksFiltered(), width))
	return -1
}

// tasksCursorOn is that search over the page exactly as it stands.
func tasksCursorOn(a *app, name string) (int, bool) {
	width, _ := a.size()
	for at, line := range a.tasksFiltered().lay(width) {
		named := ""
		switch line.kind {
		case tasksLineChat:
			named = line.chat.title
		case tasksLineTask:
			named = tasksLabel(line.item.entry)
		default:
			continue
		}
		if named == name {
			a.taskSheet.cursor = at
			return at, true
		}
	}
	return 0, false
}

// THE WORK HANGS UNDER THE CHAT THAT ASKED FOR IT, and the chat is a row.
func TestEveryPieceOfWorkIsDrawnUnderItsOwnConversation(t *testing.T) {
	r := tasksChatReading()
	// EVERY CONVERSATION OPENS SHUT (the owner's ruling, 2026-09-11) and what this
	// test is about is the SHAPE behind that fold, so it reads the page after `→`.
	if shut := tasksLineOf(t, r.lay(120), "Shipping the Gate"); !shut.folds || !shut.open {
		t.Fatalf("a conversation came out folds=%v open=%v, and every fold on this page opens open",
			shut.folds, shut.open)
	}
	lines := tasksOpen(r).lay(120)

	chat := tasksLineOf(t, lines, "Shipping the Gate")
	if chat.kind != tasksLineChat {
		t.Fatalf("the conversation came out as a line of kind %v", chat.kind)
	}
	if !chat.folds || !chat.open {
		t.Fatalf("an opened conversation came out as folds=%v open=%v, and a page of chat titles with the work behind them is not this place",
			chat.folds, chat.open)
	}
	// TWO ROOTS AND NOT FOUR: the unchecked row and the parser, with the parser's
	// own workers behind its own fold.
	if chat.kids != 2 {
		t.Fatalf("the conversation says it holds %d rows, want its two pieces of work", chat.kids)
	}
	// AND NOTHING OF THAT CONVERSATION'S IS A PEER OF IT. Every row of work under
	// a chat is indented past it, which is the whole difference from the flat
	// list this page used to be.
	for _, line := range lines {
		if line.kind != tasksLineTask || line.item.entry.SessionID != "room-a" {
			continue
		}
		if !strings.HasPrefix(line.kin, tasksKinStep) {
			t.Fatalf("%q is drawn level with its conversation: kin=%q",
				tasksLabel(line.item.entry), line.kin)
		}
		if !line.under {
			t.Fatalf("%q does not know its conversation is named above it", tasksLabel(line.item.entry))
		}
	}
	// THE CONVERSATION IS NAMED ONCE. It used to be repeated on every row of work
	// it ran, twenty-odd cells a row, on the rows whose names were being cut.
	page := tasksPage(r, 120)
	if got := strings.Count(page, "Shipping the Gate"); got != 1 {
		t.Fatalf("the conversation is named %d times on\n%s", got, page)
	}
}

// A CONVERSATION IS A STOP AND IS NEVER A TASK.
//
// The row a person aims at opens the CHAT, and every question this page asks
// about a piece of work — the room, the card, the mention, `stop it` — answers
// no over it. A synthetic task standing in for a conversation would have arrived
// at all four wearing an id nothing in the record ever wrote.
func TestAConversationRowIsNeitherRunnableNorCancellable(t *testing.T) {
	a := tasksChatApp(t)
	tasksPointAt(t, a, "Shipping the Gate")

	chat, ok := a.taskSheetChat()
	if !ok {
		t.Fatal("the cursor is on the conversation and the place cannot name it")
	}
	if _, ok := a.taskSheetCurrent(); ok {
		t.Fatal("a conversation answered the question every caller asks about a piece of work")
	}
	if verbs := a.taskSheet.verbs(a); len(verbs) != 0 {
		t.Fatalf("the conversation was offered %d verbs of its own", len(verbs))
	}
	if chat.row.Transcript != "/journals/room-a/session.jsonl" {
		t.Fatalf("the conversation's door points at %q", chat.row.Transcript)
	}
	// ITS NAME CANNOT BE A TASK'S. The verb strip, the fold map and the cursor's
	// memory across a rebuild all hold both kinds of row in one key space.
	id := placeTasks{}.rowID(a)
	if id == "" || !strings.Contains(id, tasksChatMark) {
		t.Fatalf("the conversation is named %q, which a piece of work could answer to", id)
	}
	tasksPointAt(t, a, "rotate the certificate")
	if work := (placeTasks{}).rowID(a); work == id {
		t.Fatalf("a piece of work and its conversation are both called %q", id)
	}
	// AND THE FOOT SAYS WHAT THE KEY REALLY DOES over the conversation.
	tasksPointAt(t, a, "Shipping the Gate")
	if got := a.taskSheet.hint(a); !strings.Contains(got, tasksEnterOpenWord) {
		t.Fatalf("the foot over a conversation reads\n  %s\nand enter goes to that conversation", got)
	}
}

// THE WHOLE CONVERSATION STANDS UNDER ITS MOST URGENT WORK, AND EVERY ROW KEEPS
// ITS OWN STATE.
//
// These are two claims and they are deliberately not one. Where a conversation
// is DRAWN is the most urgent thing in it, because that is where a person will
// go looking for the rest of it; what any single row of it IS remains that row's
// own reading — a task waiting on a slot, or on nothing at all, never says a
// person is being asked for.
func TestAConversationStandsUnderItsMostUrgentWorkWithoutRefilingIt(t *testing.T) {
	r := tasksChatReading()
	// The page after `→`: where a conversation is FILED is the claim here, and
	// its work has to be on the page to say the second half of it.
	lines := tasksOpen(r).lay(120)

	head, seen := "", map[string]string{}
	for _, line := range lines {
		switch {
		case line.kind == tasksLineWord && line.owner < 0:
			head = line.text
		case line.kind == tasksLineChat:
			seen[line.chat.title] = head
		}
	}
	if got := seen["Shipping the Gate"]; !strings.HasPrefix(got, tasksSectionWord(tasksRunning)) {
		t.Fatalf("the conversation with unchecked work in it stands under %q", got)
	}
	if got := seen["Thor Clips"]; !strings.HasPrefix(got, tasksSectionWord(tasksCompleted)) {
		t.Fatalf("the conversation that finished yesterday stands under %q", got)
	}
	// AND ITS FINISHED WORK CAME WITH IT rather than being left behind under a
	// heading its conversation is not on.
	if got := tasksLineOf(t, lines, "port the parser"); got.item.entry.SessionID != "room-a" {
		t.Fatalf("the finished work of that conversation is not on the page")
	}
	// THE MARK IS THE ROW'S OWN. Only the work nobody could check wears the mark
	// that says a person is being asked for.
	for _, line := range lines {
		if line.kind != tasksLineTask {
			continue
		}
		want := taskEntryStatus(line.item.entry, line.item.runs).Attention
		if got := line.item.section == tasksNeeds; got != want {
			t.Fatalf("%q is judged needs-a-person=%v by the page and %v by its own reading",
				tasksLabel(line.item.entry), got, want)
		}
	}
	page := tasksPage(r, 120)
	for _, line := range strings.Split(page, "\n") {
		if !strings.Contains(line, tokens.GlyphNeedsHuman) {
			continue
		}
		if !strings.Contains(line, "rotate the certificate") && !strings.Contains(line, "Shipping the Gate") {
			t.Fatalf("a row that needs nobody wears the steer mark:\n\t%s", line)
		}
	}
}

// WORK NESTS AS DEEPLY AS THE RECORD SAYS IT DOES. A worker's own workers used
// to be drawn as roots beside the run that commissioned them, because the page
// read one level of family and stopped.
func TestWorkNestsToWhateverDepthTheRecordCarries(t *testing.T) {
	r := tasksChatReading()
	// The conversation and the two families are opened by hand: everything on
	// this page opens shut ([tasksReading.opens]) and the claim here is about the
	// DEPTH under them.
	r.open = map[tasksKey]bool{
		tasksChatKey("room-a"):       true,
		{session: "room-a", id: "2"}: true,
		{session: "room-a", id: "3"}: true,
	}
	lines := r.lay(120)

	parser := tasksLineOf(t, lines, "port the parser")
	lexer := tasksLineOf(t, lines, "port the lexer")
	table := tasksLineOf(t, lines, "port the token table")
	if !parser.folds || !lexer.folds {
		t.Fatalf("a piece of work with work under it does not fold: parser=%v lexer=%v", parser.folds, lexer.folds)
	}
	// EACH LEVEL SITS ONE STEP IN FROM THE ONE THAT ASKED FOR IT.
	one, two, three := ansi.StringWidth(parser.kin), ansi.StringWidth(lexer.kin), ansi.StringWidth(table.kin)
	if !(one < two && two < three) {
		t.Fatalf("the tree came out as %q / %q / %q", parser.kin, lexer.kin, table.kin)
	}
	// AND A ROW THAT HOLDS NOTHING WEARS THE CONNECTOR, while a row that holds
	// something wears the fold a person can press.
	if !strings.HasSuffix(table.kin, tasksKinLast) {
		t.Fatalf("the deepest row wears %q", table.kin)
	}
	if !strings.HasSuffix(lexer.kin, tasksFoldOpen) {
		t.Fatalf("a worker with workers of its own wears %q rather than its fold", lexer.kin)
	}
	// SHUT AGAIN, WHAT IS BEHIND THE FOLD IS BEHIND IT — at every depth.
	r.open = map[tasksKey]bool{tasksChatKey("room-a"): true, {session: "room-a", id: "2"}: true, {session: "room-a", id: "3"}: false}
	page := tasksPageFolded(r, 120)
	if !strings.Contains(page, "port the lexer") || strings.Contains(page, "port the token table") {
		t.Fatalf("a shut worker did not take its own workers with it:\n%s", page)
	}
	if !strings.Contains(page, tasksUnderWord(1)) {
		t.Fatalf("the shut worker does not say what it is holding:\n%s", page)
	}
}

// A CONVERSATION OPENS BY DEFAULT AND STAYS SHUT WHEN IT IS SHUT.
//
// The two folds on this page have opposite defaults on purpose, and the fold map
// used to be a SET — a key in it meant open and shutting one deleted it, which
// over a conversation is a key that visibly does nothing.
func TestAConversationFoldRemembersBeingShut(t *testing.T) {
	a := tasksChatApp(t)
	// The conversation opens shut, so this starts from the page after `→` — the
	// claim is that `←` is remembered, and a fold already shut cannot say it.
	openTaskFolds(a)
	tasksPointAt(t, a, "Shipping the Gate")

	if !a.taskSheetFold(false) {
		t.Fatal("`←` did nothing over an open conversation")
	}
	r := a.tasksFiltered()
	width, _ := a.size()
	page := tasksPageFolded(r, width)
	if !strings.Contains(page, "Shipping the Gate") {
		t.Fatalf("shutting a conversation took its own row off the page:\n%s", page)
	}
	if strings.Contains(page, "rotate the certificate") {
		t.Fatalf("a shut conversation still draws its work:\n%s", page)
	}
	// THE COUNT UNDER IT IS UNTOUCHED AND THE HEADING RECONCILES THE TWO. The
	// foot is what the place is holding; the rows are what is on the page.
	held := len(r.section(tasksRunning))
	if got := r.shown(r.section(tasksRunning)); got != 0 || held == 0 {
		t.Fatalf("a shut conversation shows %d rows of %d held", got, held)
	}
	// AND IT IS STILL SHUT AFTER THE PAGE REBUILDS UNDER IT.
	world, win, now := tasksChatFixture()
	a.taskSheet.reading = readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now)
	if strings.Contains(tasksPageFolded(a.tasksFiltered(), width), "rotate the certificate") {
		t.Fatal("a rebuild opened a conversation somebody had shut")
	}
	if !a.taskSheetFold(true) {
		t.Fatal("`→` did nothing over a shut conversation")
	}
	if !strings.Contains(tasksPageFolded(a.tasksFiltered(), width), "rotate the certificate") {
		t.Fatal("`→` over a shut conversation did not put its work back")
	}
}

// THE CURSOR IS REMEMBERED BY WHAT IT IS ON, and a conversation is something it
// can be on. The rebuild lands every three seconds; a cursor kept as a line
// number changes which row it is on between somebody reading one and pressing
// enter.
func TestTheCursorKeepsAConversationAcrossARebuild(t *testing.T) {
	a := tasksChatApp(t)
	at := tasksPointAt(t, a, "Thor Clips")

	was, held := a.taskSheet.rowAt(a, at)
	if !held || !was.chat() {
		t.Fatalf("the cursor on a conversation is remembered as %+v", was)
	}
	// A rebuild with one more piece of work in it, which moves every line below
	// the row the cursor is on.
	world, win, now := tasksChatFixture()
	rows := &world.Projects[0].Sessions[0].Tasks.Rows
	*rows = append(*rows, session.TaskIndexEntry{
		SessionID: "room-a", ID: "9", Label: "size the corpus",
		Status: string(session.TaskRunning),
	})
	a.taskSheet.reading = readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now)

	line, found := a.taskSheet.lineOf(a, was)
	if !found {
		t.Fatal("the conversation the cursor was on is not on the rebuilt page")
	}
	a.taskSheet.cursor = line
	chat, ok := a.taskSheetChat()
	if !ok || chat.title != "Thor Clips" {
		t.Fatalf("the cursor came back on %+v, want the conversation it was on", chat)
	}
}

// A QUERY SHOWS WHAT IT FOUND WHERE IT SITS. A hit dropped out from under the
// work it was cut from, and out from under the conversation that asked for it,
// is a hit with the only thing that explains it taken away — and a hit left
// behind a fold nobody opened is a query that appears to have found nothing.
func TestAQueryKeepsTheWholeConversationAndOpensItsTree(t *testing.T) {
	a := tasksChatApp(t)
	a.taskSheet.query.setText("token table")
	a.taskSheetTyped()

	r := a.tasksFiltered()
	width, _ := a.size()
	page := tasksPage(r, width)
	for _, want := range []string{"Shipping the Gate", "rotate the certificate", "port the parser", "port the lexer", "port the token table"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the query lost %q from\n%s", want, page)
		}
	}
	// Other conversations stay out, even when they reuse the same task numbers.
	for _, gone := range []string{"Thor Clips", "render the fight clip"} {
		if strings.Contains(page, gone) {
			t.Fatalf("the query kept %q, which matches nothing:\n%s", gone, page)
		}
	}
	// THE HEAD STILL COUNTS THE PLACE AND NOT THE QUERY.
	if got := r.head(width, false); !strings.Contains(got, "5 subtasks") {
		t.Fatalf("a filtered page says the machine has run\n  %s", got)
	}
}

// A RECORD THAT LIES ABOUT ITS OWN PARENTS CANNOT HANG THE PAGE.
//
// The index is a FILE and a file can say anything, including that a row is its
// own grandparent. The walk that reads one may never be the thing that stops a
// frame painting, so a chain that does not end is not believed and the work is
// drawn where work with no parent is drawn.
func TestACircularRecordStillDrawsEveryRowOnce(t *testing.T) {
	now := time.Date(2026, time.September, 6, 13, 0, 0, 0, time.UTC)
	row := session.SessionRow{ID: "room-a", Title: "the knot", Project: "codeaf"}
	row.Tasks.Rows = []session.TaskIndexEntry{
		{SessionID: "room-a", ID: "1", Parent: "2", Label: "first half",
			Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour)},
		{SessionID: "room-a", ID: "2", Parent: "1", Label: "second half",
			Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour)},
		{SessionID: "room-a", ID: "3", Parent: "1", Label: "hangs off the knot",
			Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour)},
		{SessionID: "room-a", ID: "4", Parent: "4", Label: "its own parent",
			Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour)},
	}
	world := session.World{Projects: []session.Project{
		{Name: "codeaf", Sessions: []session.SessionRow{row}},
	}, Read: now}
	r := readTasks(world, tasksMine{}, session.LastDays(now, 7), tasksSort{}, time.Time{}, now)
	r.unfolded = true

	if len(r.items) != 4 {
		t.Fatalf("the reading holds %d rows of the four in the record", len(r.items))
	}
	page := tasksPage(r, 120)
	for _, want := range []string{"first half", "second half", "hangs off the knot", "its own parent"} {
		if got := strings.Count(page, want); got != 1 {
			t.Fatalf("%q is drawn %d times on\n%s", want, got, page)
		}
	}
}

// A CHILD WHOSE PARENT IS NOT ON THE PAGE STANDS ON ITS OWN rather than
// vanishing behind a row that is not there.
func TestAnOrphanedChildIsStillDrawn(t *testing.T) {
	now := time.Date(2026, time.September, 6, 13, 0, 0, 0, time.UTC)
	row := session.SessionRow{ID: "room-a", Title: "The Split", Project: "codeaf"}
	row.Tasks.Rows = []session.TaskIndexEntry{
		{SessionID: "room-a", ID: "8", Parent: "7", Label: "the worker whose run is gone",
			Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour)},
	}
	world := session.World{Projects: []session.Project{
		{Name: "codeaf", Sessions: []session.SessionRow{row}},
	}, Read: now}
	r := readTasks(world, tasksMine{}, session.LastDays(now, 7), tasksSort{}, time.Time{}, now)

	page := tasksPage(r, 120)
	if !strings.Contains(page, "the worker whose run is gone") {
		t.Fatalf("an orphan is off the page entirely:\n%s", page)
	}
	line := tasksLineOf(t, tasksOpen(r).lay(120), "the worker whose run is gone")
	if strings.Contains(line.kin, tasksKinCont) || strings.Contains(line.kin, tasksKinLast) {
		t.Fatalf("an orphan is drawn under a row that is not there: kin=%q", line.kin)
	}
}

// TWO CONVERSATIONS' TASK 1 ARE TWO PIECES OF WORK, and one fold is not the
// other's. Node ids restart with every conversation, so an id alone names a
// different task in every one of them.
func TestTwoConversationsWearingTheSameIdKeepTheirOwnFolds(t *testing.T) {
	r := tasksChatReading()
	// Both conversations in the fixture have a task numbered 1, and both open
	// shut — so one is opened by hand and the other left where it started.
	r.open = map[tasksKey]bool{tasksChatKey("room-a"): true, tasksChatKey("room-b"): false}
	page := tasksPageFolded(r, 120)
	if !strings.Contains(page, "rotate the certificate") {
		t.Fatalf("shutting one conversation shut another's work:\n%s", page)
	}
	if strings.Contains(page, "render the fight clip") {
		t.Fatalf("the shut conversation still drew its work:\n%s", page)
	}
	if !strings.Contains(page, "Thor Clips") || !strings.Contains(page, "Shipping the Gate") {
		t.Fatalf("a conversation went missing:\n%s", page)
	}
}

// A CONVERSATION NOTHING NAMED IS NOT A ROW. Nothing is invented for it: the
// work keeps the place it has always had, at the top of its section, and no fold
// is drawn over a blank.
func TestWorkWithMissingConversationMetadataStillHasAParentRow(t *testing.T) {
	now := time.Date(2026, time.September, 6, 13, 0, 0, 0, time.UTC)
	row := session.SessionRow{ID: "room-a", Project: "codeaf"}
	row.Tasks.Rows = []session.TaskIndexEntry{
		{SessionID: "room-a", ID: "1", Label: "port the parser",
			Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour)},
	}
	world := session.World{Projects: []session.Project{
		{Name: "codeaf", Sessions: []session.SessionRow{row}},
	}, Read: now}
	r := readTasks(world, tasksMine{}, session.LastDays(now, 7), tasksSort{}, time.Time{}, now)

	lines := r.lay(120)
	for _, line := range lines {
		if line.kind == tasksLineChat && line.chat.row.ID != "room-a" {
			t.Fatalf("task attributed to wrong conversation: %q", line.chat.row.ID)
		}
	}
	work := tasksLineOf(t, lines, "port the parser")
	if work.kin == "" {
		t.Fatalf("a page with no shape on it drew the column: %q", work.kin)
	}
	if !work.under {
		t.Fatal("the row has no conversation above it")
	}
}

// EVERY ROW KEEPS INSIDE ITS CELLS WITH A TREE ON THE PAGE, at every width.
// The column says where a row sits and may never take the cells the row's own
// name needs (rowfit.go, law 1).
func TestEveryRowOfTheTreeKeepsInsideItsCells(t *testing.T) {
	r := tasksChatReading()
	r.unfolded = true
	for _, width := range []int{40, 60, 80, 120, 200} {
		for i, drawn := range r.rows(width, newPalette(tokens.TrueColor, false)) {
			if got := ansi.StringWidth(drawn); got > width {
				t.Errorf("row %d drew %d cells at width %d: %q", i, got, width, plain(drawn))
			}
		}
		// AND THE DEEPEST ROW STILL SAYS WHAT IT IS.
		if !strings.Contains(tasksPage(r, width), "port the token") {
			t.Errorf("at %d columns the deepest row lost its name:\n%s", width, tasksPage(r, width))
		}
	}
}
