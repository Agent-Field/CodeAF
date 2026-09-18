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
		ID: "room-a", Title: "shipping the gate", Project: "codeaf",
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
		ID: "room-b", Title: "thor clips", Project: "media",
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
	r.open = map[tasksKey]bool{tasksChatKey("room-a"): true, {session: "room-a", id: "2"}: true}
	page := tasksPageFolded(r, 120)
	if !strings.Contains(page, "port the lexer") || strings.Contains(page, "port the token table") {
		t.Fatalf("a shut worker did not take its own workers with it:\n%s", page)
	}
	if !strings.Contains(page, tasksUnderWord(1)) {
		t.Fatalf("the shut worker does not say what it is holding:\n%s", page)
	}
}

// A QUERY SHOWS WHAT IT FOUND WHERE IT SITS. A hit dropped out from under the
// work it was cut from, and out from under the conversation that asked for it,
// is a hit with the only thing that explains it taken away — and a hit left
// behind a fold nobody opened is a query that appears to have found nothing.
func TestAQueryKeepsTheAncestorsOfWhatItFoundAndOpensThePath(t *testing.T) {
	a := tasksChatApp(t)
	a.taskSheet.query.setText("token table")
	a.taskSheetTyped()

	r := a.tasksFiltered()
	width, _ := a.size()
	page := tasksPage(r, width)
	for _, want := range []string{"shipping the gate", "port the parser", "port the lexer", "port the token table"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the query lost %q from\n%s", want, page)
		}
	}
	// AND NOTHING ELSE CAME WITH THEM.
	for _, gone := range []string{"rotate the certificate", "thor clips", "render the fight clip"} {
		if strings.Contains(page, gone) {
			t.Fatalf("the query kept %q, which matches nothing:\n%s", gone, page)
		}
	}
	// THE HEAD STILL COUNTS THE PLACE AND NOT THE QUERY.
	if got := r.head(width, false); !strings.Contains(got, "5 pieces of work") {
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
	row := session.SessionRow{ID: "room-a", Title: "the split", Project: "codeaf"}
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
func TestTwoRunsWearingTheSameIdKeepTheirOwnFolds(t *testing.T) {
	r := tasksChatReading()
	r.query = "show runs"
	r.open = map[tasksKey]bool{{session: "room-a", id: "1"}: true, {session: "room-b", id: "1"}: false}
	lines := r.lay(120)
	first := tasksLineOf(t, lines, "port the parser")
	second := tasksLineOf(t, lines, "render the fight clip")
	if first.family == second.family || tasksKeyOf(first.item.entry) == tasksKeyOf(second.item.entry) {
		t.Fatalf("two runs wearing task id 1 shared an identity: %+v %+v", first.family, second.family)
	}
	if !strings.Contains(tasksPageFolded(r, 120), "render the fight clip") {
		t.Fatal("one run's fold hid the other run's root")
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
