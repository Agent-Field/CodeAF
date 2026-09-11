package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// tasksTableFixture is a record with something in every column: three
// conversations, a family, money that ranges over two orders of magnitude, a row
// that cost nothing anybody recorded, and one piece of work nobody could check.
//
// IT IS BUILT FOR THE SORT. Two of its conversations stand in the SAME section
// so that the order of the roots is a claim about their totals rather than about
// their headings, and the rows inside one of them have three different prices so
// that descending is distinguishable from arriving.
func tasksTableFixture() (session.World, session.UsageWindow, time.Time) {
	loc := time.FixedZone("fixture", -4*60*60)
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, loc)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	work := func(room, id, label, status string, ended time.Time, files int, cost float64) session.TaskIndexEntry {
		return session.TaskIndexEntry{
			ID: id, Label: label, Title: label, SessionID: room, Status: status,
			EndedAt: ended, FilesChanged: files, Cost: cost,
		}
	}
	first := session.SessionRow{ID: "room-a", Title: "the pricing page", Project: "aforge", Open: true}
	first.Tasks.Rows = []session.TaskIndexEntry{
		work("room-a", "1", "put the annual toggle up", string(session.TaskDone), ago(time.Hour), 2, 1.50),
		work("room-a", "2", "pages one two", string(session.TaskDone), ago(2*time.Hour), 1, .25),
		work("room-a", "3", "check the copy", string(session.TaskUnverified), ago(3*time.Hour), 0, 0),
	}
	second := session.SessionRow{ID: "room-b", Title: "thor clips", Project: "media", Open: true}
	second.Tasks.Rows = []session.TaskIndexEntry{
		work("room-b", "1", "render the fight clip", string(session.TaskDone), ago(5*time.Hour), 12, .10),
	}
	third := session.SessionRow{ID: "room-c", Title: "the corpus sweep", Project: "aforge", Open: true}
	third.Tasks.Rows = []session.TaskIndexEntry{
		work("room-c", "1", "sweep the corpus", string(session.TaskDone), ago(4*time.Hour), 3, 4.20),
		work("room-c", "2", "count the tokens", string(session.TaskDone), ago(6*time.Hour), 1, .60),
	}
	third.Tasks.Rows[1].Parent = "1"
	world := session.World{Projects: []session.Project{
		{Name: "aforge", Sessions: []session.SessionRow{first, third}},
		{Name: "media", Sessions: []session.SessionRow{second}},
	}, Read: now}
	return world, session.LastDays(now, 7), now
}

// tasksTableApp is that record on the page, with the folds where the place puts
// them and the cursor settled.
func tasksTableApp(t *testing.T) *app {
	t.Helper()
	world, win, now := tasksTableFixture()
	a := &app{pal: newPalette(tokens.NoColor, false)}
	a.width, a.height = 122, 40
	a.clock = func() time.Time { return now }
	a.raisePlace(pageTasks)
	a.taskSheet.world = world
	a.taskSheet.reading = readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now)
	a.taskSheet.awayAt, a.taskSheet.mineAt = a.elsewhere().Read, a.railStamp
	a.taskSheet.cursor = a.tasksSettle(0)
	return a
}

// tasksWorkLines is every row of WORK on the laid-out page, in the order it is
// drawn — which is the order the sort is a claim about.
func tasksWorkLines(lines []tasksLine) []tasksLine {
	var out []tasksLine
	for _, line := range lines {
		if line.kind == tasksLineTask {
			out = append(out, line)
		}
	}
	return out
}

// ── the columns ─────────────────────────────────────────────────────────────

// THE COLUMNS ARE FIXED AT NINETY CELLS AND OVER, AND THE STATE GOES FIRST UNDER
// IT. spec.md §2: the fold, the mark, the name, `state` at twenty cells, the
// sort key's own cells right-aligned, and one cell of air. Only the name flexes
// (rowfit.go law 1), so the two figures a person is comparing down the page are
// in the same cells on every row of one frame.
func TestTheTasksTableHoldsItsColumnsToTheCellAtEveryWidth(t *testing.T) {
	world, win, now := tasksTableFixture()
	reading := tasksOpen(readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now))
	for _, width := range []int{122, 100, 90} {
		state, second, name := tasksColumns(width, tasksByAge)
		if state != tasksStateCells || second != tasksKeyCells {
			t.Fatalf("at %d cells the columns came out state=%d key=%d, want %d and %d",
				width, state, second, tasksStateCells, tasksKeyCells)
		}
		if want := width - state - second - tasksColumnAir; name != want {
			t.Fatalf("at %d cells the name took %d and the columns left %d", width, name, want)
		}
		// AND THE PAGE REALLY DRAWS THEM: every row of work says its state word.
		for _, line := range tasksWorkLines(reading.lay(width)) {
			row := plain(tasksRow(line, width, now, tasksSort{}, newPalette(tokens.NoColor, false), false))
			if word := taskStateWord(line.item.entry, line.item.runs); !strings.Contains(row, word) {
				t.Fatalf("at %d cells a row gave up its state:\n  %s", width, strings.TrimRight(row, " "))
			}
		}
	}
	// UNDER NINETY THE STATE COLUMN GOES AND THE KEY COLUMN STAYS. The state is
	// the wider of the two and the one a narrow frame can put somewhere else —
	// the cursor's own line grows it ([tasksReasonLine]) — while a figure has
	// nowhere else on the page to be.
	for _, width := range []int{89, 60} {
		state, second, name := tasksColumns(width, tasksByAge)
		if state != 0 {
			t.Fatalf("at %d cells the state column is still %d cells wide", width, state)
		}
		if second != tasksKeyCells {
			t.Fatalf("at %d cells the sort key's column came out %d cells", width, second)
		}
		if name != width-second-tasksColumnAir {
			t.Fatalf("at %d cells the name took %d of the %d left to it", width, name, width-second-tasksColumnAir)
		}
	}
}

// A TABLE'S COLUMNS DO NOT MOVE. Every row of one frame — a conversation with no
// mark, a worker four levels down a family, a loner — puts its `state` cell in
// the same cells and ends on the same cell. The row's lead is spent out of the
// NAME ([tasksTableRow]); a row that measured its columns from what its own lead
// left put them one or two cells further left on every level of a family, which
// is the defect this law was written after.
func TestEveryRowOfOneFramePutsItsColumnsInTheSameCells(t *testing.T) {
	world, win, now := tasksTableFixture()
	reading := tasksOpen(readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now))
	pal := newPalette(tokens.NoColor, false)
	// EVERY WIDTH FROM THE PHONE'S EDGE UP, because the defect lived at the
	// floor: a row that asked [tasksColumns] of what its own lead left crossed
	// the ninety-cell line a few cells before a root did, so on a frame a little
	// over ninety a worker drew no state column under a conversation that did.
	for width := 60; width <= 130; width++ {
		stateCells, _, nameCells := tasksColumns(width, tasksByAge)
		if stateCells == 0 {
			continue
		}
		start := nameCells
		lines := reading.lay(width)
		checked := 0
		for i, line := range lines {
			var want string
			switch line.kind {
			case tasksLineTask:
				want = rowTail([]rowField{tasksStateField(line)}, stateCells)
			case tasksLineChat:
				want = rowTail([]rowField{tasksChatStateField(line.chat)}, stateCells)
			default:
				continue
			}
			row := []rune(plain(reading.paint(lines, i, width, pal, false)))
			if len(row) != width {
				t.Fatalf("at %d cells a row is %d cells wide:\n  %q", width, len(row), string(row))
			}
			cell := strings.TrimRight(string(row[start:start+stateCells]), " ")
			if cell != strings.TrimSpace(want) || row[start-1] != ' ' {
				t.Fatalf("at %d cells the state column of\n  %q\nreads %q at cell %d, want %q",
					width, string(row), cell, start, want)
			}
			checked++
		}
		if checked < 8 && width == 90 {
			t.Fatalf("at %d cells the law was asked of %d rows, and the fixture has three conversations and six rows of work", width, checked)
		}
	}
}

// THE STATE CELL IS NEVER BLANK ON A ROW OF WORK. It is the one thing this list
// is read for, and a hole in that column is a row a person has to open to find
// out whether it wants them.
func TestEveryRowOfWorkSaysWhatItIs(t *testing.T) {
	world, win, now := tasksTableFixture()
	reading := tasksOpen(readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now))
	for _, line := range tasksWorkLines(reading.lay(122)) {
		if got := rowTail([]rowField{tasksStateField(line)}, tasksStateCells); strings.TrimSpace(got) == "" {
			t.Fatalf("%q drew nothing in the state column", tasksLabel(line.item.entry))
		}
	}
	// AND A SHUT ROOT'S CELL IS ITS COUNT, which is the question a shut fold
	// raises: how much is behind this, and does any of it want me.
	for _, line := range reading.lay(122) {
		if line.kind != tasksLineChat {
			continue
		}
		got := rowTail([]rowField{tasksChatStateField(line.chat)}, tasksStateCells)
		if strings.TrimSpace(got) == "" {
			t.Fatalf("the conversation %q drew nothing in the state column", line.chat.title)
		}
		if !strings.HasPrefix(strings.TrimSpace(got), itoa(line.chat.whole)+" ") {
			t.Fatalf("the conversation %q says %q and it is holding %d pieces of work",
				line.chat.title, strings.TrimSpace(got), line.chat.whole)
		}
	}
}

// A SHUT ROOT'S COUNT IS THE ROWS IT IS HIDING, at every depth of what is under
// it. A root over one task with a worker beneath it saying `1 done` beside a
// heading saying `2 folded away` is two numbers about the same rows.
func TestAShutRootCountsEveryRowItHides(t *testing.T) {
	world, win, now := tasksTableFixture()
	reading := readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now)
	lines := reading.lay(122)
	if work := tasksWorkLines(lines); len(work) != 0 {
		t.Fatalf("a fresh reading drew %d rows of work, and every conversation opens shut", len(work))
	}
	for _, line := range lines {
		if line.kind != tasksLineChat {
			continue
		}
		// Open this one alone and count what appeared.
		one := reading
		one.open = map[tasksKey]bool{line.family: true}
		under := 0
		for _, kid := range one.lay(122) {
			if kid.kind == tasksLineTask && kid.item.entry.SessionID == line.chat.row.ID {
				under++
			}
		}
		// `→` opens one level; the families under it stay shut, and the count is
		// still the whole of what the root is standing over.
		deep := reading
		deep.unfolded = true
		whole := 0
		for _, kid := range deep.lay(122) {
			if kid.kind == tasksLineTask && kid.item.entry.SessionID == line.chat.row.ID {
				whole++
			}
		}
		if line.chat.whole != whole {
			t.Fatalf("%q says it holds %d and opening it whole draws %d (one press draws %d)",
				line.chat.title, line.chat.whole, whole, under)
		}
	}
}

// ── the sort ────────────────────────────────────────────────────────────────

// SORTING BY COST ORDERS EVERY LEVEL OF THE TREE BY MONEY AND FLATTENS NONE OF
// IT: the work inside a conversation descends, the conversations descend by
// their totals inside their own section, a row nobody priced sinks to the bottom
// of its group, the second column shows the money, and the label wears the
// arrow. Asking for the same key again turns the whole thing round.
func TestSortingByCostOrdersEveryLevelAndSaysSo(t *testing.T) {
	world, win, now := tasksTableFixture()
	by := tasksSort{key: tasksByCost}
	reading := tasksOpen(readTasks(world, tasksMine{}, win, by, time.Time{}, now))

	// INSIDE ONE CONVERSATION, DEAREST FIRST, AND THE UNPRICED ROW LAST.
	inside := []string{}
	for _, line := range tasksWorkLines(reading.lay(122)) {
		if line.item.entry.SessionID == "room-a" {
			inside = append(inside, tasksLabel(line.item.entry))
		}
	}
	want := []string{"put the annual toggle up", "pages one two", "check the copy"}
	if strings.Join(inside, " / ") != strings.Join(want, " / ") {
		t.Fatalf("sorted by cost one conversation reads\n  %s\nwant\n  %s",
			strings.Join(inside, " / "), strings.Join(want, " / "))
	}

	// AND THE CONVERSATIONS INSIDE ONE SECTION GO BY THEIR TOTALS. The sweep cost
	// $4.80 altogether and the clips cost ten cents, and both finished today.
	roots := []string{}
	for _, line := range reading.lay(122) {
		if line.kind == tasksLineChat && line.chat.state == tasksToday {
			roots = append(roots, line.chat.title)
		}
	}
	if strings.Join(roots, " / ") != "the corpus sweep / thor clips" {
		t.Fatalf("the conversations of one section read %q, want the dearest first", strings.Join(roots, " / "))
	}

	// AND THE COLUMN SHOWS THE MONEY, in the money's own ink and nowhere else.
	row := tasksDrawnRow(tasksPage(reading, 122), "put the annual toggle up")
	if !strings.HasSuffix(row, "$1.50") {
		t.Fatalf("a row sorted by cost ends on\n  %s\nand the column it is sorted by is the money", row)
	}
	blank := tasksDrawnRow(tasksPage(reading, 122), "check the copy")
	if strings.Contains(blank, "$") {
		t.Fatalf("a row nobody priced drew a figure:\n  %s", blank)
	}

	// AND THE LABEL WEARS THE ARROW.
	_, second := tasksControlLabels(by)
	if second != "cost "+tasksSortDown {
		t.Fatalf("the sorted column's label reads %q", second)
	}
	if _, back := tasksControlLabels(by.on(tasksByCost)); back != "cost "+tasksSortUp {
		t.Fatalf("the same key again leaves the label reading %q", back)
	}

	// AND THE SAME KEY AGAIN REVERSES THE WHOLE PAGE.
	up := tasksOpen(readTasks(world, tasksMine{}, win, by.on(tasksByCost), time.Time{}, now))
	back := []string{}
	for _, line := range tasksWorkLines(up.lay(122)) {
		if line.item.entry.SessionID == "room-a" {
			back = append(back, tasksLabel(line.item.entry))
		}
	}
	if strings.Join(back, " / ") != "pages one two / put the annual toggle up" &&
		back[len(back)-1] != "check the copy" {
		t.Fatalf("reversed, the conversation reads %q", strings.Join(back, " / "))
	}
	if back[len(back)-1] != "check the copy" {
		t.Fatalf("reversed, the row nobody priced came off the bottom: %q", strings.Join(back, " / "))
	}
}

// THE CHORD AND THE POINTER ARE ONE DOOR. `alt+s` walks the keys and a press on
// a label names one; both go through [app.taskSheetSortBy], so a column clicked
// twice and a key cycled round to itself behave the same way.
//
// IT IS A CHORD AND NOT `s` (the ruling of 2026-09-11). Every printable key on
// this page goes into the filter, so a bare `s` would cost a person `sweep`,
// `stop` and `site`.
func TestAPressOnTheCostLabelSortsTheListByCost(t *testing.T) {
	// AT A FRAME THE PANE SPLITS AND AT ONE IT DOES NOT. The list is drawn in 72
	// of 122 cells while the pane stands beside it, and a hit map that measured
	// its columns from the FRAME put every label fifty cells to the right of the
	// word a person was pointing at — so at 122 no click on a label sorted
	// anything. The press here is at the cell the label is PAINTED in.
	for _, width := range []int{122, 100} {
		a := tasksTableApp(t)
		a.width = width
		if a.taskSheet.order.key != tasksByAge {
			t.Fatalf("the page opens sorted by %q", a.taskSheet.order.key.word())
		}
		// A PRESS ON THE COLUMN ALREADY SORTED TURNS IT ROUND.
		x, y := tasksLabelAt(t, a, tasksByAge.word())
		a.taskSheetPress(x, y)
		if now := a.taskSheet.order; now.key != tasksByAge || !now.back {
			t.Fatalf("at %d cells a press on the age label left the page on %q back=%v", width, now.key.word(), now.back)
		}
		// AND THE STATE LABEL BESIDE IT IS ITS OWN COLUMN — where there is one.
		if stateCells, _, _ := tasksColumns(a.taskSheetListWidth(), tasksByAge); stateCells > 0 {
			x, y = tasksLabelAt(t, a, tasksByState.word())
			a.taskSheetPress(x, y)
			if a.taskSheet.order.key != tasksByState {
				t.Fatalf("at %d cells a press on the state label sorted by %q", width, a.taskSheet.order.key.word())
			}
		}
		// AND THE SECOND LABEL IS THE SORT KEY'S OWN COLUMN WHATEVER IT IS
		// SHOWING: walked round to cost with the chord, a press on `cost ↓` is a
		// press on cost. The chord and the pointer are one door.
		for i := 0; i < int(tasksSortKeyCount)+1 && a.taskSheet.order.key != tasksByCost; i++ {
			drive(t, a, key(tasksSortKeyChord))
		}
		if _, second := tasksControlLabels(a.taskSheet.order); second != "cost "+tasksSortDown {
			t.Fatalf("%q never reached cost: the second label reads %q", tasksSortKeyChord, second)
		}
		x, y = tasksLabelAt(t, a, tasksByCost.word())
		a.taskSheetPress(x, y)
		if now := a.taskSheet.order; now.key != tasksByCost || !now.back {
			t.Fatalf("at %d cells a press on the cost label left the page on %q back=%v", width, now.key.word(), now.back)
		}
		// AND THE OTHER CHORD TURNS THE COLUMN THE PAGE IS ON ROUND, rather than
		// walking back a key: what a person means by shift here is "the other
		// way", not "the previous column".
		was := a.taskSheet.order
		drive(t, a, key(tasksSortBackChord))
		if now := a.taskSheet.order; now.key != was.key || now.back == was.back {
			t.Fatalf("%q left the page sorted by %q back=%v, want cost the other way round",
				tasksSortBackChord, now.key.word(), now.back)
		}
	}
}

// tasksLabelAt is the screen cell a column label is PAINTED in, found on the
// control row of the frame the place really drew — which is the only honest
// place for a pointer test to aim: the arithmetic is what is under test.
func tasksLabelAt(t *testing.T, a *app, label string) (int, int) {
	t.Helper()
	width, height := a.size()
	lines, hits, _, _ := a.taskSheetFrame(width, height)
	for y, hit := range hits {
		if hit.kind != taskSheetHitControl {
			continue
		}
		row := []rune(plain(lines[y]))
		for x := 0; x+len(label) <= len(row); x++ {
			if string(row[x:x+len(label)]) == label {
				return x + 1, y
			}
		}
		t.Fatalf("the control row does not draw %q:\n  %s", label, string(row))
	}
	t.Fatalf("the control row answers no press:\n%s", strings.Join(lines, "\n"))
	return 0, 0
}

// THE PAGE'S OWN TWO KEYS ARE NAMED ON EVERY FRAME, INCLUDING THE ONE IT OPENS
// ON. The foot has two roads through it — a conversation's row returns early
// with its own clauses — and with every fold opening shut the cursor's first
// resting place IS a conversation, so a page key named only on the other road
// would be named on no frame a person meets first. The tmux drive found this:
// the tasks place as it opens said neither key.
func TestTheTasksFootNamesThePagesKeysOverAConversationToo(t *testing.T) {
	a := tasksTableApp(t)
	if _, ok := a.taskSheetChat(); !ok {
		t.Fatalf("the page did not open with the cursor on a conversation: %d", a.taskSheet.cursor)
	}
	got := a.taskSheetKeysLine()
	for _, want := range []string{tasksSortHint(a.taskSheet.order), tasksFilterHint} {
		if !strings.Contains(got, want) {
			t.Fatalf("over a conversation the foot reads\n  %q\nand never names %q", got, want)
		}
	}
	// AND THE CLAUSE THAT MOVES WITH A FILTER MOVES ON THIS ROAD TOO.
	a.taskSheet.query.setText("pages")
	a.taskSheetTyped()
	a.taskSheet.cursor = tasksPointAtRoot(t, a, "the pricing page")
	if got := a.taskSheetKeysLine(); !strings.Contains(got, tasksClearFilterWord) ||
		strings.Contains(got, tasksFilterHint) {
		t.Fatalf("a filtered foot over a conversation reads %q", got)
	}
}

// ── the folds ───────────────────────────────────────────────────────────────

// EVERY CONVERSATION AND EVERY FAMILY OPENS SHUT (owner, 2026-09-11), `→` opens
// one, and A TYPED FILTER OPENS EVERY CONVERSATION WITH A MATCHING ROW — then
// gives the person their own folds back when it clears. A row that matched and
// is sitting behind a fold is a row the query appears not to have found.
func TestTheTasksPageOpensFoldedAndAFilterOpensWhatItMatched(t *testing.T) {
	a := tasksTableApp(t)
	width, _ := a.size()
	page := tasksPageFolded(a.tasksFiltered(), width)
	for _, name := range []string{"the pricing page", "thor clips", "the corpus sweep"} {
		if !strings.Contains(page, name) {
			t.Fatalf("a fresh page is missing the conversation %q:\n%s", name, page)
		}
	}
	if work := tasksWorkLines(a.tasksFiltered().lay(width)); len(work) != 0 {
		t.Fatalf("a fresh page drew %d rows of work under its roots:\n%s", len(work), page)
	}

	// `→` OPENS ONE, AND ONE ONLY.
	tasksPointAtRoot(t, a, "the pricing page")
	if !a.taskSheetFold(true) {
		t.Fatal("`→` did nothing over a shut conversation")
	}
	page = tasksPageFolded(a.tasksFiltered(), width)
	if !strings.Contains(page, "pages one two") {
		t.Fatalf("`→` did not put the conversation's work on the page:\n%s", page)
	}
	if strings.Contains(page, "render the fight clip") {
		t.Fatalf("`→` opened a conversation nobody was standing on:\n%s", page)
	}
	if !a.taskSheetFold(false) {
		t.Fatal("`←` did nothing over the conversation it had just opened")
	}

	// A TYPED FILTER OPENS WHAT IT MATCHED.
	a.taskSheet.query.setText("pages")
	a.taskSheetTyped()
	page = tasksPageFolded(a.tasksFiltered(), width)
	if !strings.Contains(page, "pages one two") {
		t.Fatalf("the filter left its own match behind a fold:\n%s", page)
	}
	if strings.Contains(page, "render the fight clip") || strings.Contains(page, "sweep the corpus") {
		t.Fatalf("the filter kept work nothing matched:\n%s", page)
	}

	// AND CLEARING IT GIVES THE PERSON THEIR OWN FOLDS BACK.
	a.taskSheet.query.setText("")
	a.taskSheetTyped()
	page = tasksPageFolded(a.tasksFiltered(), width)
	if strings.Contains(page, "pages one two") {
		t.Fatalf("the cleared filter left the conversation it opened standing open:\n%s", page)
	}
	if !strings.Contains(page, "the pricing page") {
		t.Fatalf("the cleared filter took the conversation off the page:\n%s", page)
	}
}

// tasksPointAtRoot parks the cursor on a conversation's own row, WITHOUT opening
// anything — which is what the fold tests need and what [tasksPointAt] cannot
// promise, because reaching a piece of work means opening the fold over it.
func tasksPointAtRoot(t *testing.T, a *app, title string) int {
	t.Helper()
	width, _ := a.size()
	for at, line := range a.tasksFiltered().lay(width) {
		if line.kind == tasksLineChat && line.chat.title == title {
			a.taskSheet.cursor = at
			return at
		}
	}
	t.Fatalf("no conversation on the page is called %q", title)
	return -1
}

// ── the reason ──────────────────────────────────────────────────────────────

// UNDER A HUNDRED AND TEN CELLS THE CURSOR'S ROW GROWS ONE DIM LINE, and the
// line is the state and the reason joined the one way this codebase joins them
// ([session.TaskStatus.RowWord]). The reason left the row when the row became a
// table; a frame too narrow for the pane beside the list is where it has to go
// instead, and it is one row of the page rather than twenty.
func TestUnderTheePaneFloorTheCursorsRowGrowsItsReason(t *testing.T) {
	world, win, now := tasksTableFixture()
	reading := tasksOpen(readTasks(world, tasksMine{}, win, tasksSort{}, time.Time{}, now))
	var unchecked tasksItem
	for _, item := range reading.items {
		if tasksLabel(item.entry) == "check the copy" {
			unchecked = item
		}
	}
	pal := newPalette(tokens.NoColor, false)
	got := plain(tasksReasonLine(unchecked, 100, pal))
	if want := unchecked.status().RowWord(); got != want {
		t.Fatalf("the grown line reads\n  %q\nwant\n  %q", got, want)
	}
	if !strings.Contains(got, "your call") || !strings.Contains(got, "nobody could check it") {
		t.Fatalf("the grown line over work nobody could check reads %q", got)
	}
	// AND IT IS DRAWN ONLY WHERE THERE IS NO PANE FOR IT — asked of the pane
	// itself, so the two can never disagree about one frame.
	a := tasksTableApp(t)
	for width := 60; width <= 200; width++ {
		a.width = width
		if grows, pane := tasksReasonShowing(a), a.taskPaneShowing(); grows && pane {
			t.Fatalf("at %d cells the list grows a line the pane beside it is already saying", width)
		} else if !grows && !pane && layoutTier(width) != tierPhone {
			t.Fatalf("at %d cells the reason is on no line of the frame at all", width)
		}
	}
}
