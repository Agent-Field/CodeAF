package tui3

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE RECORD BESIDE THE LIST ──────────────────────────────────────────────
//
// What these hold the pane to: it is there at the width that says it is and
// absent at every other, it says the same things about a row as the card does,
// its digits answer through the card's own door, it never reads a disk twice for
// one row and never reads one on a draw, and a row with nothing to say draws
// nothing rather than a line of holes.

// paneSeam is the seam as a reader sees it, with its gutter trimmed off so a row
// can be searched for it.
var paneSeam = strings.TrimSpace(taskPaneSeam)

// paneText is everything the pane drew, one row per line, stripped of paint.
func paneText(a *app) string {
	return strings.Join(paneRows(a), "\n")
}

// paneRows is the pane's own rows: the cells to the right of the seam on every
// frame row that has one.
func paneRows(a *app) []string {
	width, height := a.size()
	lines, _, _, _ := a.taskSheetFrame(width, height)
	left := a.taskSheetListWidth()
	at := left + ansi.StringWidth(taskPaneSeam)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		bare := plain(line)
		if ansi.StringWidth(bare) <= left || !strings.Contains(bare, paneSeam) {
			continue
		}
		out = append(out, strings.TrimRight(ansi.Cut(bare, at, at+width), " "))
	}
	return out
}

// paneLandingLab is a window holding ONE piece of work of its own that has
// landed on the person's call, with the landing question standing, on the tasks
// place at the given width.
//
// IT IS THE WINDOW'S OWN NODE ON PURPOSE. The answer road for a row of the
// record is the block above this conversation ([app.taskRecordLanding] says why
// the session has to match), so a fixture built out of another conversation's
// index rows could draw the chips and never press one.
func paneLandingLab(t *testing.T, width int) (*app, *roomQuestionFake) {
	t.Helper()
	base, fake, _ := roomApp(t)
	agent := &roomQuestionFake{roomFake: fake}
	base.agent = agent
	base.profileDir = t.TempDir()
	now := base.now()
	base.clock = func() time.Time { return now }
	// Somebody is at this keyboard (questiondelivery.go's [awayAfter]).
	base.lastQuestionKey = now
	base.width, base.height = width, 30
	drive(t, base, streamEventMsg{gen: base.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskUnverified, unverifiedNotice("nobody could check it — the checker never answered"))})
	q := landingAsk(7, session.QuestionLanding, "Fix the nil-map crash", askCheckReason, "accept", "not right")
	q.Asked = now
	base.questionFold(session.Event{Kind: session.EventQuestion, Question: &q})
	// Drawn, then aged past the block's settle guard (question.go).
	base.questionRows(base.width)
	now = now.Add(2 * questionSettle)
	base.showPage(pageTasks)
	paneCursorOnWork(t, base)
	return base, agent
}

// paneCursorOnWork puts the cursor on the first row of WORK, which is where a
// person's first `↓` lands.
func paneCursorOnWork(t *testing.T, a *app) {
	t.Helper()
	lines := a.tasksFiltered().lay(a.taskSheetListWidth())
	for at := range lines {
		if lines[at].kind == tasksLineTask {
			a.taskSheet.cursor = at
			return
		}
	}
	t.Fatalf("the tasks place drew no row of work:\n%s", placeText(a))
}

// THE BODY SPLITS AT THE WIDTH THE LAW NAMES AND AT NO OTHER. One cell under the
// floor there is no pane, no seam, and the whole frame is the list.
func TestTheTasksBodySplitsOnlyWhereTheFrameIsWideEnough(t *testing.T) {
	a, _ := paneLandingLab(t, taskPaneFloor+12)
	if cols := taskPaneCols(a.width); cols == 0 {
		t.Fatalf("a %d-cell frame drew no pane", a.width)
	}
	if !strings.Contains(placeText(a), paneSeam) {
		t.Fatalf("a split frame drew no seam:\n%s", placeText(a))
	}
	if got, want := a.taskSheetListWidth(), a.width-taskPaneCols(a.width)-ansi.StringWidth(taskPaneSeam); got != want {
		t.Fatalf("the list was laid out in %d cells, want %d", got, want)
	}

	a.width = taskPaneFloor - 1
	if cols := taskPaneCols(a.width); cols != 0 {
		t.Fatalf("a %d-cell frame drew a %d-cell pane", a.width, cols)
	}
	if text := placeText(a); strings.Contains(text, paneSeam) {
		t.Fatalf("a frame under the floor drew a seam:\n%s", text)
	}
	if got := a.taskSheetListWidth(); got != a.width {
		t.Fatalf("the list gave up %d cells to a pane that is not there", a.width-got)
	}
}

// THE PANE IS CLAMPED AT BOTH ENDS. Its share of a frame is worth nothing if it
// leaves either half unreadable, so the share is bounded rather than trusted.
func TestThePanesWidthIsAShareOfTheFrameClampedAtBothEnds(t *testing.T) {
	for width := taskPaneFloor; width < 400; width++ {
		cols := taskPaneCols(width)
		if cols < taskPaneThin || cols > taskPaneWide {
			t.Fatalf("a %d-cell frame drew a %d-cell pane, want between %d and %d",
				width, cols, taskPaneThin, taskPaneWide)
		}
		if left := taskPaneList(width); left <= cols {
			t.Fatalf("at %d cells the pane took %d and left the list %d", width, cols, left)
		}
	}
}

// THE PANE AND THE CARD SAY THE SAME THINGS ABOUT ONE ROW. They are two widths
// of one account, so a word on one and not the other is the drift the shared
// builders exist to stop.
//
// IT IS A ROW OF THE RECORD AND NOT ONE OF THIS WINDOW'S OWN NODES, because a
// node this session is holding has a ROOM and `enter` opens that instead
// ([tasksPlace.enter] states the ladder) — the card is the door for work another
// conversation ran.
func TestThePaneAndTheRecordCardAgreeAboutOneRow(t *testing.T) {
	a, _ := paneReadLab(t, taskPaneFloor+12)
	item, ok := a.taskSheetCurrent()
	if !ok {
		t.Fatal("no row under the cursor")
	}
	pane := paneText(a)
	shared := []string{
		taskRecordWords(item.entry),
		dollars(item.entry.Cost),
		strings.TrimSpace(item.entry.Model),
		taskRecordBranch(item.entry),
	}
	for _, want := range shared {
		if want == "" {
			t.Fatal("the fixture has nothing to compare on")
		}
		if !strings.Contains(pane, want) {
			t.Fatalf("the pane does not say %q:\n%s", want, pane)
		}
	}

	a.taskSheetEnter()
	if !a.taskSheet.detailOn {
		t.Fatalf("enter on a record row opened no card:\n%s", placeText(a))
	}
	card := placeText(a)
	for _, want := range shared {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not say %q the pane said:\n%s", want, card)
		}
	}
	// AND THE STATE WORD IS ONE WORD, read off the one reading both draw from.
	if word := item.status().RowWord(); word != "" {
		if !strings.Contains(pane, word) {
			t.Fatalf("the pane does not say the row's own word %q:\n%s", word, pane)
		}
	}
}

// A DIGIT ON THE LIST ANSWERS THE ROW UNDER THE CURSOR, through the one door the
// card's own keys go through, and it reaches the engine as that row's own answer.
func TestADigitOnTheTasksListAnswersTheRowUnderTheCursor(t *testing.T) {
	a, agent := paneLandingLab(t, taskPaneFloor+12)
	if text := paneText(a); !strings.Contains(text, "1 accept") || !strings.Contains(text, "2 not right") {
		t.Fatalf("the verb line does not offer the two answers:\n%s", text)
	}
	drive(t, a, key("1"))
	if len(agent.answer) != 1 || agent.answer[0].ID != 7 || agent.answer[0].FirstKey() != session.LandingYesKey {
		t.Fatalf("the digit reached the door as %+v", agent.answer)
	}
	if filter := a.taskSheetFilter(); filter != "" {
		t.Fatalf("the digit was also typed into the filter, which now reads %q", filter)
	}
}

// AND THE LETTERS STILL ANSWER. The digit is a second name for the same answer,
// not a replacement for the one a hand learned on the card.
func TestTheLandingsOwnLetterStillAnswersThroughTheOneDoor(t *testing.T) {
	a, agent := paneLandingLab(t, taskPaneFloor+12)
	item, ok := a.taskSheetCurrent()
	if !ok {
		t.Fatal("no row under the cursor")
	}
	if _, took := a.taskRecordAnswer(item.entry, session.LandingYesKey); !took {
		t.Fatal("the landing's own letter was not taken")
	}
	if len(agent.answer) != 1 || agent.answer[0].FirstKey() != session.LandingYesKey {
		t.Fatalf("the letter reached the door as %+v", agent.answer)
	}
}

// A ROW THAT IS NOT THE PERSON'S CALL IGNORES DIGITS, and the key is a character
// like any other — the filter's, which is what every printable key here is.
func TestADigitOverARowWithNoQuestionIsTyped(t *testing.T) {
	a, agent := paneLandingLab(t, taskPaneFloor+12)
	// The question goes, and with it the road: the row is still on the page and
	// still the same row, and now nothing is asking anything about it.
	a.questions = nil
	drive(t, a, key("1"))
	if len(agent.answer) != 0 {
		t.Fatalf("a digit over a row with no question answered %+v", agent.answer)
	}
	if filter := a.taskSheetFilter(); filter != "1" {
		t.Fatalf("the filter reads %q, want the digit to have been typed", filter)
	}
	if text := paneText(a); strings.Contains(text, "1 accept") {
		t.Fatalf("the verb line offered an answer with no road behind it:\n%s", text)
	}
}

// paneReadLab is a hosted surface with three rows of another machine's work,
// counting every journal the pane asks for.
func paneReadLab(t *testing.T, width int) (*app, *atomic.Int64) {
	t.Helper()
	a := hostedPlaceLab(t)
	a.width, a.height = width, 30
	now := time.Now()
	rows := make([]session.TaskIndexEntry, 0, 3)
	for i := range 3 {
		entry := farCardEntry(now)
		entry.ID = itoa(i + 1)
		entry.Label = "widening the pipe " + itoa(i+1)
		entry.Title = entry.Label
		entry.EndedAt = now.Add(-time.Duration(i+1) * time.Minute)
		rows = append(rows, entry)
	}
	a.world = func() (session.World, bool) {
		return session.World{
			Read: now,
			Projects: []session.Project{{
				Dir: "-srv-code-api", Path: "/srv/code/api", Name: "api",
				Sessions: []session.SessionRow{{
					ID: "bbbb000000000002", Title: "rewriting the importer",
					Project: "api", ProjectDir: "/srv/code/api", Workspace: "/srv/code/api",
					At: now, Created: now,
					Tasks: session.TaskRollup{Rows: rows},
				}},
			}},
		}, true
	}
	var asked atomic.Int64
	a.farRecord = func(string, int) (session.TaskRecord, error) {
		asked.Add(1)
		return session.TaskRecord{Report: "it went fine.\n\nand then some more.", Kept: true}, nil
	}
	a.showPage(pageTasks)
	paneCursorOnWork(t, a)
	return a, &asked
}

// NOTHING ON THE DRAW PATH READS A JOURNAL. A frame is built thirty times a
// second and a journal is a whole session file read forward; a pane that read one
// while laying itself out would stutter the surface for as long as it was up.
func TestDrawingThePaneReadsNoJournal(t *testing.T) {
	a, asked := paneReadLab(t, taskPaneFloor+12)
	for range 20 {
		paneText(a)
	}
	if got := asked.Load(); got != 0 {
		t.Fatalf("%d journals were read by drawing the pane, want none", got)
	}
}

// A HELD ARROW PAYS FOR NOTHING IT WALKS PAST. Every move arms a settle and only
// the settle armed by the move that STOPPED does any reading.
func TestWalkingTheListReadsOneJournalPerSettle(t *testing.T) {
	a, asked := paneReadLab(t, taskPaneFloor+12)
	var last tea.Cmd
	for range 3 {
		last = placeTasks{}.key(a, key("down"))
	}
	if got := asked.Load(); got != 0 {
		t.Fatalf("walking read %d journals before anything settled", got)
	}
	if last == nil {
		t.Fatal("the last move armed no settle")
	}
	settle, ok := paneSettleOf(t, last)
	if !ok {
		t.Fatalf("the move armed something else: %#v", last())
	}
	if cmd := a.taskPaneSettled(settle); cmd != nil {
		if msg, ok := cmd().(taskTailMsg); ok {
			a.taskTailRead(msg)
		}
	}
	if got := asked.Load(); got != 1 {
		t.Fatalf("one settle read %d journals, want 1", got)
	}
	// AND THE ROW IS NOT READ AGAIN. What has been read is kept for as long as
	// the place is open, so walking back up costs nothing.
	if cmd := a.taskPaneFollow(); cmd != nil {
		t.Fatal("a row already read armed another settle")
	}
	if text := paneText(a); !strings.Contains(text, "it went fine.") {
		t.Fatalf("the report did not reach the pane:\n%s", text)
	}
	if text := paneText(a); strings.Contains(text, "and then some more.") {
		t.Fatalf("the pane drew more than the first paragraph:\n%s", text)
	}
}

// A SETTLE ARMED BY AN EARLIER MOVE IS DROPPED. It is about a row the cursor
// walked through, which is exactly the read this mechanism exists to not pay for.
func TestASettleFromAnEarlierMoveReadsNothing(t *testing.T) {
	a, asked := paneReadLab(t, taskPaneFloor+12)
	first := placeTasks{}.key(a, key("down"))
	if first == nil {
		t.Fatal("the first move armed no settle")
	}
	stale, ok := paneSettleOf(t, first)
	if !ok {
		t.Fatalf("the move armed something else: %#v", first())
	}
	// The cursor moves on; the settle above is about the row behind it.
	placeTasks{}.key(a, key("down"))
	if cmd := a.taskPaneSettled(stale); cmd != nil {
		t.Fatal("a stale settle asked for a journal")
	}
	if got := asked.Load(); got != 0 {
		t.Fatalf("a stale settle read %d journals", got)
	}
}

// A CLICK PREVIEWS AND THE SECOND CLICK OPENS, which is the pointer grammar every
// place with a preview keeps. The first press is what the pane is for.
func TestAClickOnATaskRowPreviewsAndTheSecondOpensIt(t *testing.T) {
	a, _ := paneReadLab(t, taskPaneFloor+12)
	width, height := a.size()
	_, hits, _, _ := a.taskSheetFrame(width, height)
	row, want := -1, -1
	lines := a.tasksFiltered().lay(a.taskSheetListWidth())
	for y, hit := range hits {
		if _, work := a.tasksFiltered().at(lines, hit.index); !work {
			continue
		}
		if hit.kind == taskSheetHitRow && hit.index != a.taskSheet.cursor {
			row, want = y, hit.index
			break
		}
	}
	if row < 0 {
		t.Fatalf("the fixture drew no second row to click:\n%s", placeText(a))
	}
	a.taskSheetPress(2, row)
	if a.taskSheet.detailOn {
		t.Fatal("the first click opened a card instead of previewing")
	}
	if a.taskSheet.cursor != want {
		t.Fatalf("the first click left the cursor on %d, want %d", a.taskSheet.cursor, want)
	}
	a.taskSheetPress(2, row)
	if !a.taskSheet.detailOn {
		t.Fatalf("the second click on the same row opened nothing:\n%s", placeText(a))
	}
}

// AND WHERE THERE IS NO PANE ONE CLICK OPENS, exactly as it always did: there is
// no preview for a first click to buy.
func TestAClickOnANarrowFrameOpensOnTheFirstPress(t *testing.T) {
	a, _ := paneReadLab(t, taskPaneFloor-1)
	width, height := a.size()
	_, hits, _, _ := a.taskSheetFrame(width, height)
	row := -1
	lines := a.tasksFiltered().lay(a.taskSheetListWidth())
	for y, hit := range hits {
		if _, work := a.tasksFiltered().at(lines, hit.index); !work {
			continue
		}
		if hit.kind == taskSheetHitRow {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatalf("the fixture drew no row to click:\n%s", placeText(a))
	}
	a.taskSheetPress(2, row)
	if !a.taskSheet.detailOn {
		t.Fatalf("one click on a frame with no pane opened nothing:\n%s", placeText(a))
	}
}

// A CLICK ON THE PANE'S OWN ANSWER ANSWERS IT, and never the row it is drawn
// beside.
func TestAClickOnThePanesAcceptAnswersTheRow(t *testing.T) {
	a, agent := paneLandingLab(t, taskPaneFloor+12)
	// THE COLUMN IS READ OFF THE PAINTED FRAME and never off the hit map this is
	// testing: a test that took both halves of the arithmetic from one source
	// could not catch the two disagreeing, which is the whole failure it is here
	// for.
	width, height := a.size()
	lines, _, _, _ := a.taskSheetFrame(width, height)
	row, at := -1, -1
	for y, line := range lines {
		bare := plain(line)
		want := strings.Index(bare, "1 accept")
		if !strings.Contains(bare, paneSeam) || want < 0 {
			continue
		}
		row, at = y, ansi.StringWidth(bare[:want])
		break
	}
	if row < 0 {
		t.Fatalf("the pane drew no accept to click:\n%s", placeText(a))
	}
	a.taskSheetPress(at, row)
	if len(agent.answer) != 1 || agent.answer[0].ID != 7 || agent.answer[0].FirstKey() != session.LandingYesKey {
		t.Fatalf("the click reached the door as %+v", agent.answer)
	}
	if a.taskSheet.detailOn {
		t.Fatal("a click on the pane's answer also opened the card")
	}
}

// THE EMPTINESS LAW REACHES EVERY LINE OF THE PANE. A row that spent nothing,
// wrote nothing and left no branch draws none of those lines — not a zero, not a
// dash, and not a blank where they would have been.
func TestAPaneOverARowWithNothingToSayDrawsNothingForIt(t *testing.T) {
	a, _ := paneLandingLab(t, taskPaneFloor+12)
	item, ok := a.taskSheetCurrent()
	if !ok {
		t.Fatal("no row under the cursor")
	}
	bare := session.TaskIndexEntry{ID: item.entry.ID, SessionID: item.entry.SessionID, Title: "a bare row"}
	if line := a.taskPaneFacts(bare, 44); line != "" {
		t.Fatalf("a row with no facts drew %q", line)
	}
	if rows := a.taskPaneWhere(bare); len(rows) != 0 {
		t.Fatalf("a row with no branch and no rung drew %q", rows)
	}
	if rows := a.taskPaneFiles(bare, 40); len(rows) != 0 {
		t.Fatalf("a row that wrote nothing drew %d file rows", len(rows))
	}
	for _, banned := range []string{"$0.00", "0 files", "0 file"} {
		if line := a.taskPaneFacts(bare, 44); strings.Contains(line, banned) {
			t.Fatalf("the facts line drew %q", banned)
		}
	}
}

// THE FILE BAND NAMES WHAT IT HAS AND COUNTS WHAT IT HAS NOT, against the
// record's own honest total rather than against the list it kept.
func TestThePaneNamesFiveFilesAndCountsTheRest(t *testing.T) {
	a, _ := paneLandingLab(t, taskPaneFloor+12)
	entry := session.TaskIndexEntry{
		ID: "1", Title: "wrote a lot", FilesChanged: 9,
		Files: []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go"},
	}
	rows := a.taskPaneFiles(entry, 40)
	if len(rows) != taskPaneFileRows+1 {
		t.Fatalf("the band drew %d rows, want %d paths and one count", len(rows), taskPaneFileRows)
	}
	if last := plain(rows[len(rows)-1]); last != itoa(entry.FilesChanged-taskPaneFileRows)+taskPaneMoreWord {
		t.Fatalf("the band counted the rest as %q", last)
	}
}

// A SHORT FRAME KEEPS THE HEAD AND THE VERBS. The verb line is the last band, so
// a pane cut from the bottom would drop the two answers and keep the file paths —
// which is the one part of a preview nobody can act on.
//
// IT IS DRIVEN THROUGH THE WHOLE BODY AND NOT THROUGH [app.taskPaneRows] ALONE,
// because the defect this is written against was in the JOIN: the page's head
// sentence takes a row of the frame and none of the pane, and a pane built at the
// body's full room lost its last row to that — which the trim had just made the
// verb line.
func TestAShortFrameKeepsThePanesTitleAndItsVerbs(t *testing.T) {
	a, _ := paneLandingLab(t, taskPaneFloor+12)
	item, ok := a.taskSheetCurrent()
	if !ok {
		t.Fatal("no row under the cursor")
	}
	a.height = 18
	rows := paneRows(a)
	drawn := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row) != "" {
			drawn = append(drawn, row)
		}
	}
	if len(drawn) < 2 {
		t.Fatalf("the pane drew %d rows on a short frame:\n%s", len(drawn), strings.Join(rows, "\n"))
	}
	if want := strings.TrimSpace(plain(fit(taskRecordWords(item.entry), taskPaneCols(a.width)))); strings.TrimSpace(drawn[0]) != want {
		t.Fatalf("the short pane opens on %q, want the title %q", drawn[0], want)
	}
	if last := drawn[len(drawn)-1]; !strings.Contains(last, "1 accept") {
		t.Fatalf("the short pane ends on %q, want the verbs:\n%s", last, strings.Join(rows, "\n"))
	}
}

// AND THE HEAD SENTENCE NEVER EATS A ROW OF THE PANE. It spends a row of the
// FRAME and none of the preview, so the pane is built at the room that is left.
func TestThePagesHeadDoesNotCostThePaneItsLastRow(t *testing.T) {
	a, _ := paneLandingLab(t, taskPaneFloor+12)
	a.height = 18
	withHead := paneRows(a)
	if len(withHead) == 0 {
		t.Fatalf("the pane drew nothing:\n%s", placeText(a))
	}
	last := ""
	for _, row := range withHead {
		if strings.TrimSpace(row) != "" {
			last = row
		}
	}
	if !strings.Contains(last, "1 accept") {
		t.Fatalf("with the head on screen the pane ends on %q, want the verbs:\n%s",
			last, strings.Join(withHead, "\n"))
	}
}

// WHAT HAS NOT LANDED IS READ AGAIN WHEN THE CURSOR ARRIVES. A row still writing
// its journal says something different every time somebody walks onto it, so a
// kept report would be a preview frozen at whatever the work had said the first
// time a person passed.
func TestTheReportOfWorkStillRunningIsReadAgainOnArrival(t *testing.T) {
	a, _ := paneReadLab(t, taskPaneFloor+12)
	item, ok := a.taskSheetCurrent()
	if !ok {
		t.Fatal("no row under the cursor")
	}
	a.taskPaneKeep(tasksKeyOf(item.entry), item.entry.EndedAt, "what it said the first time")
	if cmd := a.taskPaneFollow(); cmd != nil {
		t.Fatal("a landed row that has been read armed another settle")
	}

	// THE SAME ROW, STILL RUNNING. The reading is rebuilt so the cursor's row is
	// the live one, and arriving on it has to ask again.
	live := item.entry
	live.EndedAt, live.Status = time.Time{}, string(session.TaskRunning)
	a.taskSheet.reading.items[0].entry = live
	a.taskSheet.reading.items[0].runs = true
	a.taskPaneKeep(tasksKeyOf(live), live.EndedAt, "what it said the first time")
	if cmd := a.taskPaneFollow(); cmd == nil {
		t.Fatal("a row that has not landed was treated as read for good")
	}
}

// AND A ROW THAT LANDED AGAIN IS NOT THE ROW THAT WAS READ. One piece of work
// can land twice, and the second landing writes a second report over the first.
func TestARowThatLandedAgainReadsAsUnread(t *testing.T) {
	a, _ := paneReadLab(t, taskPaneFloor+12)
	item, ok := a.taskSheetCurrent()
	if !ok {
		t.Fatal("no row under the cursor")
	}
	a.taskPaneKeep(tasksKeyOf(item.entry), item.entry.EndedAt, "the first landing's report")
	if kept, read := a.taskPaneKept(item.entry); !read || kept != "the first landing's report" {
		t.Fatalf("the read was not kept: %q %v", kept, read)
	}
	again := item.entry
	again.EndedAt = again.EndedAt.Add(time.Minute)
	if _, read := a.taskPaneKept(again); read {
		t.Fatal("a second landing read back the first landing's report")
	}
}

// A MACHINE THAT COULD NOT BE ASKED IS NOT AN ANSWER ABOUT THE ROW. `unread` is a
// fact about the LINK, and keeping it would file "this row had nothing to say"
// against a journal nobody has read yet — permanently, for the life of the place.
func TestALinkThatWouldNotAnswerIsNotKeptAsSilence(t *testing.T) {
	a, asked := paneReadLab(t, taskPaneFloor+12)
	a.farRecord = func(string, int) (session.TaskRecord, error) {
		asked.Add(1)
		return session.TaskRecord{}, errors.New("the link did not answer")
	}
	item, ok := a.taskSheetCurrent()
	if !ok {
		t.Fatal("no row under the cursor")
	}
	cmd := a.readTaskTail(item.entry)
	if cmd == nil {
		t.Fatal("the row named no journal to read")
	}
	msg, ok := cmd().(taskTailMsg)
	if !ok || !msg.unread {
		t.Fatalf("the failed read answered %#v", msg)
	}
	a.taskTailRead(msg)
	if _, read := a.taskPaneKept(item.entry); read {
		t.Fatal("a link that would not answer was kept as a row with nothing to say")
	}
	if follow := a.taskPaneFollow(); follow == nil {
		t.Fatal("arriving on the row again asked for nothing")
	}
}

// WORK ANOTHER WINDOW IS RUNNING HAS NO REPORT HERE TO READ. Its pane says where
// the work is and stops there, which is why arriving on one arms no journal read
// ([app.taskPaneFollow] asks the same question of the same field): those rows are
// live, so a read armed for one would open a file whose text is thrown away, on
// every arrival, for as long as the other window ran.
func TestAnotherWindowsWorkPreviewsWhereItIsAndNothingElse(t *testing.T) {
	a, _ := paneReadLab(t, taskPaneFloor+12)
	item, ok := a.taskSheetCurrent()
	if !ok {
		t.Fatal("no row under the cursor")
	}
	away := item
	away.away, away.window = true, "the other window"
	a.taskPaneKeep(tasksKeyOf(away.entry), away.entry.EndedAt, "a report nobody should see here")

	var drawn []string
	for _, row := range a.taskPaneRecord(away, taskPaneCols(a.width)) {
		if text := strings.TrimSpace(plain(row.text)); text != "" {
			drawn = append(drawn, text)
		}
	}
	page := strings.Join(drawn, "\n")
	// The sentence is WRAPPED to the pane, so it is read back with its folds
	// taken out rather than looked for on one row.
	if said := strings.Join(strings.Fields(page), " "); !strings.Contains(said, strings.TrimSpace(taskAwayCardWhere("the other window"))) {
		t.Fatalf("the pane does not say where the work is:\n%s", page)
	}
	for _, banned := range []string{"a report nobody should see here", taskPaneOpenWord, dollars(away.entry.Cost)} {
		if strings.Contains(page, banned) {
			t.Fatalf("the pane over another window's work drew %q:\n%s", banned, page)
		}
	}
}

// paneSettleOf is one armed command read back as the settle it should be. A
// command the place answered with may be a batch — the key door batches the
// place's own answer with the pane's follow — so the settle is looked for inside
// whatever came back rather than assumed to be the whole of it.
func paneSettleOf(t *testing.T, cmd tea.Cmd) (taskPaneSettleMsg, bool) {
	t.Helper()
	if cmd == nil {
		return taskPaneSettleMsg{}, false
	}
	switch msg := cmd().(type) {
	case taskPaneSettleMsg:
		return msg, true
	case tea.BatchMsg:
		for _, one := range msg {
			if settle, ok := paneSettleOf(t, one); ok {
				return settle, true
			}
		}
	}
	return taskPaneSettleMsg{}, false
}
