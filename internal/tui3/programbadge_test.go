package tui3

// A program's work wears its badge everywhere a task is named, and an ordinary
// task wears nothing. These tests drive the badge the way a window meets it —
// off the engine's own notices, before any plan row has been read — and hold it
// to the column's laws: it never leads, it never costs the title its floor
// before the handle has gone, it is never a press target, and it is drawn
// without asking the agent anything.

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// programTitle is the task the fixtures hand to a program.
const programTitle = "rewrite the auth middleware"

// programNotice is the first row a program's run publishes: running, from the
// hand-off, and naming the program that has the work.
func programNotice(program string) session.TaskNotice {
	return session.TaskNotice{Program: program, StartedAt: taskFixtureNow.Add(-3 * time.Minute)}
}

// programRailApp is a window holding one program's run, known from its notice
// alone — no plan row has been read, which is the first second of every run and
// the first frame after every conversation switch.
func programRailApp(t *testing.T, program string) *app {
	t.Helper()
	a, _, _ := taskApp(t)
	a.height = 30
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, programTitle, session.TaskRunning, programNotice(program))})
	if node := a.tasks[7]; node == nil || node.program != program {
		t.Fatalf("the node did not keep its program: %+v", a.tasks[7])
	}
	return a
}

// programRailRow is the program's head line on the column at a frame width,
// widened or not, painted.
func programRailRow(t *testing.T, a *app, width int, wide bool) string {
	t.Helper()
	a.width, a.railWide = width, wide
	for _, row := range a.railRows(a.viewHeight()) {
		if strings.Contains(plain(row), "rewrite") {
			return row
		}
	}
	t.Fatalf("the program's row is not on the column at %d columns:\n%s", width,
		strings.Join(railText(a, a.viewHeight()), "\n"))
	return ""
}

// THE BADGE AT EVERY WIDTH THE COLUMN STANDS AT, in the order the column gives
// its cells up: at thirty columns the whole badge and no handle, at twenty-four
// the short badge and the handle back, and at forty-six both whole. Every row
// fits its column, and the title still starts two cells past the seam.
func TestAProgramsRowWearsItsBadgeAtEveryWidthOfTheColumn(t *testing.T) {
	a := programRailApp(t, "senior-dev")
	for _, tier := range []struct {
		name        string
		width       int
		wide        bool
		cols        int
		want, never []string
	}{
		{"railCols", 120, false, railCols, []string{"[senior-dev]"}, []string{"#7", "[sd]"}},
		{"railSlimCols", 110, false, railSlimCols, []string{"[sd]", "#7"}, []string{"[senior-dev]"}},
		{"railWideCols", 120, true, railWideCols, []string{"[senior-dev]", "#7"}, []string{"[sd]"}},
	} {
		row := plain(programRailRow(t, a, tier.width, tier.wide))
		t.Logf("%s: %q", tier.name, row)
		for _, word := range tier.want {
			if !strings.Contains(row, word) {
				t.Fatalf("at %s the program's row is %q, want %q on it", tier.name, row, word)
			}
		}
		for _, word := range tier.never {
			if strings.Contains(row, word) {
				t.Fatalf("at %s the program's row is %q, which should not carry %q", tier.name, row, word)
			}
		}
		if cells := ansi.StringWidth(row); cells > tier.cols {
			t.Fatalf("at %s the row is %d cells in a %d-cell column: %q", tier.name, cells, tier.cols, row)
		}
		if got, want := leadCells(t, row, "rewrit"), ansi.StringWidth(railSeam)+2; got != want {
			t.Fatalf("at %s the title starts at cell %d, want %d — the badge must never lead:\n%q", tier.name, got, want, row)
		}
		// AND THE BADGE COMES AFTER THE TITLE, never before it.
		if strings.Index(row, "[") < strings.Index(row, "rewrit") {
			t.Fatalf("at %s the badge stands in front of the title: %q", tier.name, row)
		}
	}
}

// AN ORDINARY TASK WEARS NO BADGE, at any width — the goldens in
// railtree_test.go and railwork_test.go hold its row to the byte; this holds the
// one fact they cannot, that nothing bracketed appears for work no program had.
func TestAnOrdinaryTasksRowWearsNoBadge(t *testing.T) {
	a := programRailApp(t, "senior-dev")
	a.taskUpdate(update(8, "Fix the loader nil-map", session.TaskRunning, session.TaskNotice{}))
	for _, width := range []int{110, 120} {
		a.width = width
		row, ok := railRowFor(a, a.viewHeight(), "Fix the loader")
		if !ok {
			t.Fatalf("the ordinary task is not on the column at %d", width)
		}
		if strings.Contains(row, "[") || !strings.Contains(row, "#8") {
			t.Fatalf("an ordinary task's row at %d columns is %q, want its handle and no badge", width, row)
		}
	}
	if programBadge("").known() {
		t.Fatal("no program answered a badge")
	}
}

// ANY PROGRAM GETS ITS OWN BADGE FROM ITS NAME, and nothing here names
// senior-dev: a program that ships next year needs no change to this surface.
func TestASecondProgramDrawsItsOwnBadge(t *testing.T) {
	a := programRailApp(t, "doc-writer")
	if row := plain(programRailRow(t, a, 120, false)); !strings.Contains(row, "[doc-writer]") || strings.Contains(row, "senior-dev") {
		t.Fatalf("a doc-writer's row is %q, want its own badge", row)
	}
	if row := plain(programRailRow(t, a, 110, false)); !strings.Contains(row, "[dw]") {
		t.Fatalf("a doc-writer's narrow row is %q, want its initials", row)
	}
	for name, want := range map[string]rowField{
		"senior-dev":   {full: "[senior-dev]", short: "[sd]"},
		"doc-writer":   {full: "[doc-writer]", short: "[dw]"},
		"gpt-5-writer": {full: "[gpt-5-writer]", short: "[g5w]"},
		"fake":         {full: "[fake]", short: "[f]"},
	} {
		if got := programBadge(name); got != want {
			t.Fatalf("programBadge(%q) = %+v, want %+v", name, got, want)
		}
	}
}

// THE BADGE IS NOT A TARGET. A press on it opens the program's task, the way a
// press anywhere else on the row does, and folds nothing: the only span a row
// reports beside its fold cell is a folded family's count.
func TestAPressOnTheBadgeOpensTheTaskAndFoldsNothing(t *testing.T) {
	a, _, _ := roomApp(t)
	a.width, a.height = 120, 30
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning, programNotice("senior-dev"))})
	if a.tasks[7].program != "senior-dev" {
		t.Fatalf("a same-state row naming the program was thrown away by the de-dup: %+v", a.tasks[7])
	}
	_, cell, count := a.railEntryRows(railEntry{node: a.tasks[7]}, a.railRoom())
	if cell.pressable() || count.pressable() {
		t.Fatalf("the program's row reported a pressable span: cell %+v, count %+v", cell, count)
	}
	y := railRowY(t, a, 7)
	row := railText(a, a.viewHeight())[y-a.bodyTop()]
	at := strings.Index(row, "[senior-dev]")
	if at < 0 {
		t.Fatalf("the program's row wears no badge: %q", row)
	}
	opened := map[uint64]bool{}
	for id, open := range a.railOpen {
		opened[id] = open
	}
	railClick(t, a, a.bodyWidth()+ansi.StringWidth(row[:at])+1, y)
	if !a.roomOpen() || roomID(a) != 7 {
		t.Fatalf("a press on the badge did not open the task: room %d", roomID(a))
	}
	for id, open := range a.railOpen {
		if opened[id] != open {
			t.Fatalf("a press on the badge folded row %d", id)
		}
	}
}

// DRAWING THE BADGE READS NOTHING. It comes off the node the notice made, and
// for a node an older engine never named, off the plan rows the surface already
// holds — never a call to the agent while a frame is being drawn.
func TestDrawingAProgramsBadgeReadsNothingFromTheAgent(t *testing.T) {
	row := programRow()
	row.ID = "7"
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, nil)
	counted := &railPlanCounter{planFake: fake}
	a.agent = counted
	a.width, a.height = 120, 30
	// AN OLDER ENGINE'S ROW, naming no program: the badge is read off the held
	// plan row.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, row.Title, session.TaskRunning, session.TaskNotice{StartedAt: programRunBegan})})
	rows, pages := counted.rows, counted.pages
	if drawn := plain(strings.Join(a.railRows(a.viewHeight()), "\n")); !strings.Contains(drawn, "[senior-dev]") {
		t.Fatalf("a node named only by its held plan row wears no badge:\n%s", drawn)
	}
	a.stripRow(90)
	a.taskSheetOwnRows()
	if counted.rows != rows || counted.pages != pages {
		t.Fatalf("drawing the badge read the agent: rows %d→%d, pages %d→%d", rows, counted.rows, pages, counted.pages)
	}
}

// THE BADGE IS THERE BEFORE ANY PLAN ROW IS READ AND AFTER A CONVERSATION
// SWITCH. It used to be learned only off the run's plan rows, which a window
// reads on a beat of its own and stops holding the moment the conversation in
// front changes; the notice carries it now, so neither gap takes it away.
func TestTheBadgeOutlivesTheGapsInThePlanRows(t *testing.T) {
	a := programRailApp(t, "senior-dev")
	if _, held := a.heldPlanRows(); held {
		t.Fatal("the fixture holds plan rows, so this would prove nothing about the notice")
	}
	if row := plain(programRailRow(t, a, 120, false)); !strings.Contains(row, "[senior-dev]") {
		t.Fatalf("before any plan row was read the program's row is %q", row)
	}
	// A SWITCH AWAY AND BACK: the held rows belong to another front, and the
	// roster is replayed from the run's kept rows, which carry the program.
	a.frontGen++
	a.tasks, a.taskOrder, a.taskSeen = nil, nil, nil
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, programTitle, session.TaskRunning, programNotice("senior-dev"))})
	if row := plain(programRailRow(t, a, 120, false)); !strings.Contains(row, "[senior-dev]") {
		t.Fatalf("after a conversation switch the program's row is %q", row)
	}
}

// THE BRACKETS SURVIVE EVERY WAY OF DRAWING THE ROW: a terminal with no colour,
// the screen-reader tier, and the row a person is standing in, whose ground
// would swallow a chip. The painted badge is accent and bold where there is ink
// to paint with.
func TestTheBadgesBracketsSurviveEveryPalette(t *testing.T) {
	a := programRailApp(t, "senior-dev")
	a.width = 120
	painted := programRailRow(t, a, 120, false)
	if want := a.pal.programInk("[senior-dev]"); !strings.Contains(painted, want) {
		t.Fatalf("the badge is not painted by the one painter:\n%q\nwant it to carry %q", painted, want)
	}
	a.pal = newPalette(tokens.NoColor, false)
	if row := programRailRow(t, a, 120, false); row != plain(row) || !strings.Contains(row, "[senior-dev]") {
		t.Fatalf("with no colour the row is %q, want plain text carrying the brackets", row)
	}
	a.pal = newPalette(tokens.ANSI256, true)
	a.linear = true
	if row := plain(programRailRow(t, a, 120, false)); !strings.Contains(row, "[senior-dev]") {
		t.Fatalf("on the screen-reader tier the row is %q", row)
	}
	a.linear = false
	a.pal = newPalette(tokens.ANSI256, false)
	a.openRoom(7, programTitle)
	if row := plain(programRailRow(t, a, 120, false)); !strings.Contains(row, "[senior-dev]") {
		t.Fatalf("the row a person is standing in is %q, want the brackets on the selected ground", row)
	}
}

// THE ROOM'S OWN PANEL: the tree beside an open room is the same rows, and the
// title row over the room names the program too.
func TestTheRoomPanelAndItsTitleWearTheBadge(t *testing.T) {
	a, _, _ := roomApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning, programNotice("senior-dev"))})
	a.width, a.height = 120, 48
	a.openRoom(7, "Fix the nil-map crash")
	a.railHold = false
	if !a.roomPanelShowing(a.viewHeight()) {
		t.Fatal("the room's panel is not showing, so this would prove nothing")
	}
	lines, _ := a.railView(a.viewHeight())
	var tree []string
	for _, line := range lines {
		if line.roomSection == roomPanelTree {
			tree = append(tree, plain(line.text))
		}
	}
	if !strings.Contains(strings.Join(tree, "\n"), "[senior-dev]") {
		t.Fatalf("the room panel's tree does not wear the badge:\n%s", strings.Join(tree, "\n"))
	}
	title := plain(a.roomTitleRow(a.width))
	if !strings.Contains(title, "Fix the nil-map crash [senior-dev]") {
		t.Fatalf("the room's title row is %q, want the badge beside the title", title)
	}
	if cells := ansi.StringWidth(title); cells > a.width {
		t.Fatalf("the room's title row is %d cells in a %d-cell row", cells, a.width)
	}
}

// A PAGE ONTO ANOTHER CONVERSATION'S WORK WEARS THAT WORK'S BADGE, AND NEVER THE
// ONE A LOCAL TASK OF THE SAME NUMBER WEARS. Task ids restart with every
// conversation, so the page onto their task 7 stands beside this window's own
// task 7 — here a senior-dev run, whose plan row the surface holds. The page's
// header used to fall through to that row by the bare number and draw
// `[senior-dev]` over work no program had; and when the owner said its own work
// was a program's, the page dropped it.
func TestAGuestPageWearsItsOwnWorksBadgeAndNeverTheLocalTasks(t *testing.T) {
	a, _ := planAppWith(t, []session.PlanTaskRow{programRow()}, nil)
	a.width, a.height = 120, 30
	a.taskUpdate(update(7, programTitle, session.TaskRunning, session.TaskNotice{}))
	if drawn := plain(strings.Join(a.railRows(a.viewHeight()), "\n")); !strings.Contains(drawn, "[senior-dev]") {
		t.Fatalf("this window's own task 7 wears no badge, so this would prove nothing:\n%s", drawn)
	}
	door := &guestDoor{}
	door.watching()
	a.openTaskOwner = door.open
	a.away = elsewhereCache{read: true, at: a.now(), held: session.NewElsewhere(a.now(),
		map[string]string{"the-other-window": "docs pass"},
		window("the-other-window", session.PresenceTask{
			ID: "7", Title: "Port the parser", State: string(session.TaskRunning)}))}
	enterAway(t, a)
	if !a.roomIsGuest() {
		t.Fatal("the row opened no reading page")
	}
	if title := plain(a.roomTitleRow(a.width)); !strings.Contains(title, "Port the parser") || strings.Contains(title, "[") {
		t.Fatalf("another conversation's ordinary task 7 reads %q, wearing the badge of this window's own task 7", title)
	}
	// AND WHEN ITS OWNER SAYS ITS WORK IS A PROGRAM'S, THE PAGE WEARS THAT BADGE.
	ownerSays(t, a, session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
		ID: 7, Title: "Port the parser", State: session.TaskRunning, Program: "doc-writer"}})
	if title := plain(a.roomTitleRow(a.width)); !strings.Contains(title, "Port the parser [doc-writer]") {
		t.Fatalf("the owner said its task 7 is doc-writer's and the page reads %q", title)
	}
	if node := a.tasks[7]; node == nil || node.program != "" {
		t.Fatalf("the owner's notice named a program on this window's own task 7: %+v", node)
	}
}

// ANOTHER CONVERSATION'S PROGRAM WORK OPENS WEARING ITS BADGE, off the row the
// page was opened from and before its owner has said anything: the project's
// index names the program, and the row of that work another window has out
// keeps it, as it keeps the family it belongs to.
func TestAnotherConversationsProgramWorkOpensWearingItsBadge(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	theirs := theirLiveSession
	theirs.Tasks = session.TaskRollup{Rows: []session.TaskIndexEntry{{
		ID: "7", SessionID: theirs.ID, Label: "Port the parser", Title: "Port the parser",
		Status: string(session.TaskRunning), Program: "senior-dev",
	}}}
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the tasks place opened with no rows on it")
	}
	// The walk of the disk, as this fixture's machine would have answered it,
	// read the way the place's own rebuild reads it ([tasksPlace.regroup]).
	p := &a.taskSheet
	p.world = session.World{Projects: []session.Project{{Sessions: []session.SessionRow{theirs}}}}
	p.reading = readTasks(p.world, p.mine, p.reading.win, p.order, p.reading.seen, p.reading.now)
	r := a.tasksFiltered()
	width, _ := a.size()
	lines := r.lay(width)
	found := false
	for at := range lines {
		if item, ok := r.at(lines, at); ok && item.away {
			if item.entry.Program != "senior-dev" {
				t.Fatalf("the row another window has out lost the program its index row names: %+v", item.entry)
			}
			a.taskSheet.cursor, found = at, true
			break
		}
	}
	if !found {
		t.Fatalf("no row on the page belongs to another window:\n%s", taskSheetText(a))
	}
	cmd := a.taskSheetEnter()
	if cmd == nil {
		t.Fatal("enter over another window's running work did nothing at all")
	}
	msg, ok := cmd().(taskOwnerMsg)
	if !ok {
		t.Fatalf("enter did not ask the engine for the owner: %T", cmd())
	}
	a.tookTaskOwner(msg)
	if !a.roomIsGuest() {
		t.Fatal("the row opened no reading page")
	}
	if title := plain(a.roomTitleRow(a.width)); !strings.Contains(title, "Port the parser [senior-dev]") {
		t.Fatalf("another conversation's senior-dev task opens reading %q, want its badge", title)
	}
	// AND AN OWNER'S NOTICE THAT NAMES NO PROGRAM — an engine older than the
	// field — takes nothing away.
	ownerSays(t, a, session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
		ID: 7, Title: "Port the parser", State: session.TaskRunning}})
	if title := plain(a.roomTitleRow(a.width)); !strings.Contains(title, "Port the parser [senior-dev]") {
		t.Fatalf("a notice naming no program took the badge off the page: %q", title)
	}
}

// THE CARD A PERSON APPROVES NAMES THE PROGRAM: the block in the transcript
// wears the badge beside the name, and the question above the box says it in
// words — the same sentence the engine's own question object says.
func TestTheApprovalCardNamesTheProgram(t *testing.T) {
	a, _, _ := taskApp(t)
	ev := proposal(a, 7, 4*time.Second)
	ev.Task.Program = "senior-dev"
	drive(t, a, streamEventMsg{gen: a.gen, ev: ev})
	settleAsk(a)
	text := taskText(a)
	var drawn string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), taskHeadCorner) {
			drawn = line
		}
	}
	if !strings.Contains(drawn, "Fix the nil-map") || !strings.Contains(drawn, "[senior-dev]") {
		t.Fatalf("the card's head is %q, want its name and the program's badge:\n%s", drawn, text)
	}
	head := plain(a.taskHead(a.task, 80, false))
	if !strings.Contains(head, "[senior-dev]") || ansi.StringWidth(head) > 80 {
		t.Fatalf("the card's head is %q, want the badge inside the frame", head)
	}
	ask := taskAsk(a)
	if !strings.Contains(ask, "wants to start a [senior-dev] task: Fix the nil-map crash") {
		t.Fatalf("the question does not name the program:\n%s", ask)
	}
	if got, want := a.taskQuestion(ev.Task).Head, session.TaskProposalHead(*ev.Task); got != want {
		t.Fatalf("the surface asks %q and the engine %q — two questions about one proposal", got, want)
	}
	// AND THE RUN'S ROW, WHEN IT ARRIVES QUIET ABOUT ITS PROGRAM, KEEPS THE CARD'S.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})})
	if node := a.tasks[7]; node == nil || node.program != "senior-dev" {
		t.Fatalf("the approved node did not take the card's program: %+v", node)
	}
	// An ordinary proposal asks the sentence it always asked.
	plainAsk := session.TaskProposalHead(session.TaskNotice{Title: "Fix the nil-map crash"})
	if plainAsk != session.TaskProposalLead+"Fix the nil-map crash" {
		t.Fatalf("an ordinary proposal asks %q", plainAsk)
	}
}

// THE OTHER LISTS OF THE WORK: the strip that stands in for the column under a
// hundred columns wears the short badge, the `@` list and the tasks place wear
// the whole one, and home's work band too — and none of them moves a column or
// draws anything for an ordinary task.
func TestTheOtherListsOfTheWorkWearTheBadge(t *testing.T) {
	a := programRailApp(t, "senior-dev")
	chip, cols := a.stripLabel(a.tasks[7], 90)
	if !strings.Contains(plain(chip), "[sd]") || ansi.StringWidth(chip) != cols {
		t.Fatalf("the strip's chip is %q in %d cells, want the short badge and an honest width", plain(chip), cols)
	}
	a.taskUpdate(update(8, "Fix the loader nil-map", session.TaskRunning, session.TaskNotice{}))
	if chip, _ := a.stripLabel(a.tasks[8], 90); strings.Contains(plain(chip), "[") {
		t.Fatalf("an ordinary task's chip is %q", plain(chip))
	}

	entry := session.TaskIndexEntry{ID: "7", Label: programTitle, Title: programTitle, Status: "running", Program: "senior-dev"}
	if label := plain(taskRowLabel(entry, "3m", 120, a.pal)); !strings.HasSuffix(label, programTitle+" [senior-dev]") {
		t.Fatalf("the @ list's row is %q", label)
	}
	if block := taskPointerBlock(entry); !strings.Contains(block, "· via senior-dev") {
		t.Fatalf("the mention's block does not say which program had the work:\n%s", block)
	}
	ordinary := entry
	ordinary.Program = ""
	if label := plain(taskRowLabel(ordinary, "3m", 120, a.pal)); strings.Contains(label, "[") {
		t.Fatalf("an ordinary @ row is %q", label)
	}

	withBadge := plain(tasksTableRow("  ", 2, programTitle, "senior-dev", rowSay("running"), rowSay("3m"), a.pal.dim, 120, tasksByAge, a.pal, false, "", ""))
	without := plain(tasksTableRow("  ", 2, programTitle, "", rowSay("running"), rowSay("3m"), a.pal.dim, 120, tasksByAge, a.pal, false, "", ""))
	if !strings.Contains(withBadge, programTitle+" [senior-dev]") {
		t.Fatalf("the tasks place's row is %q", withBadge)
	}
	if ansi.StringWidth(withBadge) != ansi.StringWidth(without) || strings.Index(withBadge, "running") != strings.Index(without, "running") {
		t.Fatalf("the badge moved the table's columns:\n%q\n%q", withBadge, without)
	}
	if head := plain(tasksCardHead(tasksItem{entry: entry}, 40, a.pal, false)); !strings.Contains(head, "[senior-dev]") || ansi.StringWidth(head) > 40 {
		t.Fatalf("the tasks place's phone card head is %q", head)
	}

	band := plain(strings.Join(homeWorkName(entry, 44, taskFixtureNow, a.pal), "\n"))
	if !strings.Contains(band, programTitle+" [senior-dev]") {
		t.Fatalf("home's work band reads %q", band)
	}
	if band := plain(strings.Join(homeWorkName(ordinary, 44, taskFixtureNow, a.pal), "\n")); strings.Contains(band, "[") {
		t.Fatalf("home's work band reads %q for an ordinary task", band)
	}
}

// programLongLabel is a program's task named as long as the project's index
// names one (session's taskLabelLimit, fifty-six cells), which is the label a
// home cell or an `@` row is handed.
const programLongLabel = "Rewrite the auth middleware to use the new session store"

// HOME'S `needs you` ROWS AND ITS `since you left` LINES PAY FOR THE BADGE OUT
// OF THE TITLE, as every other list of the work does. The cell cuts a title
// from its right to keep the row's age, and a badge written onto the end of the
// title was the first thing it took: a senior-dev landing with a long name read
// `…session store [seni… 1h` on a two-column home and wore no badge at all on a
// three-column one. A landed line keeps the badge between the name and what the
// work came to, so the outcome gives way before the badge does.
func TestHomesRowsPayForTheBadgeOutOfTheTitle(t *testing.T) {
	pal := newTestPalette()
	now := taskFixtureNow
	entry := session.TaskIndexEntry{
		ID: "7", SessionID: "chat-1", Label: programLongLabel, Title: programLongLabel,
		Status: string(session.TaskDone), Program: "senior-dev",
		Outcome: "The middleware reads the new store.", EndedAt: now.Add(-time.Hour),
	}
	row := session.SessionRow{ID: "chat-1", Title: "the run", Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{entry}}}
	needs := needsCall(session.Project{}, row, entry, session.TaskStatus{}, now).line.cell
	landed := ledgerLanded(session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}, now.Add(-2*time.Hour))
	if len(landed) != 1 {
		t.Fatalf("the landing is not a line of `since you left`: %+v", landed)
	}
	ledger := leftLine(landed[0], now).cell
	if wide := plain(homeCellBody(ledger, 140, pal, false)); !strings.Contains(wide, programLongLabel+" [senior-dev] · The middleware reads the new store.") {
		t.Fatalf("a wide landed line reads %q, want the badge between the name and the outcome", wide)
	}
	for _, probe := range []struct {
		width int
		want  string
	}{
		{80, "[senior-dev]"}, {66, "[senior-dev]"}, {55, "[senior-dev]"}, {40, "[senior-dev]"}, {21, "[sd]"},
	} {
		for name, cell := range map[string]*homeCell{"needs you": needs, "since you left": ledger} {
			drawn := plain(homeCellBody(cell, probe.width, pal, false))
			t.Logf("%s at %d: %q", name, probe.width, drawn)
			if !strings.HasPrefix(drawn, "Rewrite the") || !strings.Contains(drawn, probe.want) || !strings.HasSuffix(drawn, " 1h") {
				t.Fatalf("%s's row at %d cells reads %q, want the title, %s and the age", name, probe.width, drawn, probe.want)
			}
			if cells := ansi.StringWidth(drawn); cells > probe.width {
				t.Fatalf("%s's row is %d cells in %d: %q", name, cells, probe.width, drawn)
			}
		}
	}
	// AN ORDINARY LANDING DRAWS WHAT IT ALWAYS DREW: its name and what it came to,
	// cut from the right, and nothing bracketed.
	entry.Program = ""
	row.Tasks.Rows[0] = entry
	plainNeeds := needsCall(session.Project{}, row, entry, session.TaskStatus{}, now).line.cell
	plainLanded := ledgerLanded(session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}, now.Add(-2*time.Hour))
	for _, width := range []int{140, 66, 40} {
		if drawn := plain(homeCellBody(plainNeeds, width, pal, false)); strings.Contains(drawn, "[") {
			t.Fatalf("an ordinary landing's needs row reads %q", drawn)
		}
		if drawn := plain(homeCellBody(leftLine(plainLanded[0], now).cell, width, pal, false)); strings.Contains(drawn, "[") {
			t.Fatalf("an ordinary landed line reads %q", drawn)
		}
	}
	if drawn := plain(homeCellBody(leftLine(plainLanded[0], now).cell, 140, pal, false)); !strings.HasPrefix(drawn, programLongLabel+" · The middleware") {
		t.Fatalf("an ordinary landed line reads %q", drawn)
	}
}

// THE `@` LIST PAYS FOR THE BADGE OUT OF THE TITLE TOO. Its row cuts a label
// from the right to keep the age at its edge, so a badge written onto the end
// of the label was gone under about seventy columns.
func TestTheMentionListPaysForTheBadgeOutOfTheTitle(t *testing.T) {
	a, _, _ := taskApp(t)
	entry := pastTask("7", "rewrite-the-auth-middleware", programLongLabel, time.Hour)
	entry.Program = "senior-dev"
	a.comp.tasks = []session.TaskIndexEntry{entry}
	drive(t, a, key("@"), key("r"), key("e"))
	drive(t, a, filesLoadedMsg{})
	for _, probe := range []struct {
		width int
		want  string
	}{
		{100, "[senior-dev]"}, {60, "[senior-dev]"}, {45, "[senior-dev]"}, {28, "[sd]"},
	} {
		found := ""
		for _, row := range a.comp.rows(probe.width, completeRows, a.pal, -1) {
			if line := plain(row); strings.Contains(line, "Rewrite") {
				found = line
			}
		}
		t.Logf("at %d: %q", probe.width, found)
		if !strings.Contains(found, probe.want) || ansi.StringWidth(found) > probe.width {
			t.Fatalf("the @ list's row at %d cells reads %q, want %s inside the frame", probe.width, found, probe.want)
		}
	}
}

// THE BADGE NEVER CUTS A TITLE BELOW ITS FLOOR. It falls to its short spelling
// first, and where even that would leave the title less than the floor the row
// draws no badge at all rather than a badge standing for a name nobody can read.
func TestTheBadgeYieldsBeforeTheTitleFloor(t *testing.T) {
	badge := programBadge("senior-dev")
	for _, probe := range []struct {
		room int
		want string
	}{
		{26, "[senior-dev]"},
		{25, "[senior-dev]"},
		{24, "[sd]"},
		{17, "[sd]"},
		{16, ""},
	} {
		if got := programSpelling(badge, programTitle, probe.room, railTitleFloor); got != probe.want {
			t.Fatalf("in %d cells the badge is %q, want %q", probe.room, got, probe.want)
		}
	}
	// A title shorter than the floor asks only for its own cells.
	if got := programSpelling(badge, "fix it", 19, railTitleFloor); got != "[senior-dev]" {
		t.Fatalf("a short title left the badge %q, want the whole of it", got)
	}
}

// THE RUN'S OWN PLAN ROW WEARS THE BADGE TOO. A program's store root is the same
// work as its node's row and is normally left out beside it, so when it is the
// row the column draws — the node's row was dropped for it — it is the only
// place the program's work is named, and it has to say whose it is. An ordinary
// plan row is drawn as it always was.
func TestAProgramsPlanRowOnTheRailWearsTheBadge(t *testing.T) {
	a, _, _ := taskApp(t)
	width := railCols - ansi.StringWidth(railSeam)
	row := programRow()
	rows, _, _ := a.railEntryRows(railEntry{node: planRailNode(row)}, width)
	if len(rows) == 0 {
		t.Fatal("a program's plan row drew nothing")
	}
	drawn := plain(rows[0])
	t.Logf("the plan row: %q", drawn)
	if !strings.Contains(drawn, "rewrite the") || !strings.Contains(drawn, "[s") {
		t.Fatalf("a program's plan row on the rail reads %q, want the program's badge", drawn)
	}
	if cells := ansi.StringWidth(drawn); cells > width {
		t.Fatalf("the plan row is %d cells in a %d-cell column: %q", cells, width, drawn)
	}
	row.Program, row.Stage = "", ""
	ordinary, _, _ := a.railEntryRows(railEntry{node: planRailNode(row)}, width)
	if len(ordinary) == 0 || strings.Contains(plain(ordinary[0]), "[") {
		t.Fatalf("an ordinary plan row reads %q", ordinary)
	}
}

// A PROGRAM'S RUNNING WORK OFFERS NO STEER in the `@` block the model reads,
// because the program reads no messages; it names the stop instead. An ordinary
// running task keeps its steer.
func TestAProgramsMentionBlockOffersTheStopAndNoSteer(t *testing.T) {
	entry := session.TaskIndexEntry{ID: "7", Label: programTitle, Title: programTitle, Status: "running", Program: "senior-dev"}
	block := taskPointerBlock(entry)
	if strings.Contains(block, "Steer:") || strings.Contains(block, " say ") {
		t.Fatalf("a program's block offers a steer it would refuse:\n%s", block)
	}
	if !strings.Contains(block, "senior-dev reads no messages; stop it with tasks id 7 stop") {
		t.Fatalf("a program's block does not name its one door:\n%s", block)
	}
	ordinary := entry
	ordinary.Program = ""
	if block := taskPointerBlock(ordinary); !strings.Contains(block, `Steer: tasks id 7 say "…"`) {
		t.Fatalf("an ordinary running task lost its steer:\n%s", block)
	}
}

// A PROGRAM'S LANDED CARD, OPENED, SAYS WHAT THE RUN COST, from the price its settled
// row carries (session's publishRunRow) — the card drew none while the row
// carried none.
func TestAProgramsLandedCardSaysWhatItCost(t *testing.T) {
	a := programRailApp(t, "senior-dev")
	settled := programNotice("senior-dev")
	settled.CostUSD, settled.EndedAt, settled.Report = 2.30, taskFixtureNow, "submitted and verified"
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, programTitle, session.TaskDone, settled)})
	at := a.doneEntryFor(7)
	if at < 0 {
		t.Fatal("the program's run drew no landed card")
	}
	done := a.entries[at].done
	done.open = true
	card := plain(strings.Join(a.doneRows(done, 120, false), "\n"))
	if !strings.Contains(card, "$2.30") {
		t.Fatalf("the program's landed card names no price:\n%s", card)
	}
}
