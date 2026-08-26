package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE TASKS PLACE ─────────────────────────────────────────────────────────
//
// The roster's column is THIS SESSION'S record and the machine's is on disk, and
// until this place the only door onto the second one was a completion somebody
// had to already be typing a message to reach. These are the whole of that
// claim: the place opens and closes, it draws this window's own work beside every
// other conversation's and groups it by what you do next, the fullscreen pages
// never disagree about which one owns the frame, and the column offers the door
// exactly when there is something behind it.

// ctrlDot is the page's own key (taskview.go's [taskSheetKey]).
func ctrlDot() tea.KeyPressMsg { return tea.KeyPressMsg{Code: '.', Mod: tea.ModCtrl} }

// pastTask is one row of the project's record as internal/session hands it over:
// work that landed, in a conversation that is not this one.
func pastTask(id, name, title string, ago time.Duration) session.TaskIndexEntry {
	return session.TaskIndexEntry{
		ID:      id,
		Name:    name,
		Label:   title,
		Title:   title,
		Status:  string(session.TaskDone),
		Outcome: "it came home clean",
		// The age is measured from the SURFACE'S clock and not from the wall's
		// ([taskFixtureNow] says why): a row dated from time.Now() beside a pinned
		// clock is a row that landed in the future.
		EndedAt:   taskFixtureNow.Add(-ago),
		SessionID: "an-earlier-conversation",
	}
}

// taskSheetText is the page as a reader sees it.
func taskSheetText(a *app) string {
	width, height := a.size()
	lines, _, _, _ := a.taskSheetFrame(width, height)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = plain(line)
	}
	return strings.Join(out, "\n")
}

// THE KEY OPENS IT AND esc CLOSES IT, and while it is up it is the WHOLE frame:
// no conversation, no box, no status line. A page you read the conversation past
// is a page nobody finishes reading.
func TestTheTaskPageOpensOnItsKeyAndTakesTheWholeFrame(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)

	drive(t, a, ctrlDot())
	if !a.taskSheet.open {
		t.Fatal("ctrl+. did not open the task page")
	}
	frame, _, _ := a.frame()
	// THE PLACE NAMES ITSELF ON THE TAB BAR. This page used to draw its own
	// title row, `history … esc close`, across its head; the router draws the
	// seven places above the rule and the shared hint line says how to leave, so
	// a title here would be the frame naming itself twice (pages.go).
	if !strings.Contains(plain(frame), pageTasks.word()) {
		t.Fatalf("the frame is not the tasks place:\n%s", plain(frame))
	}
	// The conversation is not drawn under it, and neither is the box a person
	// types into: the page took the frame whole.
	//
	// The probe is the page's own FOOT rather than the box's `› ` glyph. Those
	// two cells stopped being the box's alone the day this page's cursor row
	// started leading with the mark every list on this surface leads with
	// ([overlayLead]) — so a frame with a cursor on it contains `› ` whether or
	// not a box was drawn, and the question "did anything come after the page"
	// is the one actually being asked.
	lines := strings.Split(plain(frame), "\n")
	last := ""
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			last = strings.TrimSpace(line)
		}
	}
	// The foot is the ROUTER's now, and this place's own sentence is the head of
	// it ([app.placeHint] appends the two keys that are true on every place). The
	// sentence itself is SCREEN 1e's and is pinned word for word in
	// place_tasks_test.go; what this asserts is that nothing is drawn UNDER it.
	if last != placeTailed(a.taskSheetKeysLine()) {
		t.Fatalf("something is drawn under the place — it ends on %q, want its own foot:\n%s", last, plain(frame))
	}

	drive(t, a, key("esc"))
	if a.taskSheet.open {
		t.Fatal("esc did not close the task page")
	}
}

// THE KEY FALLS THROUGH ON A PROJECT THAT HAS RUN NOTHING, and /history says so
// rather than raising a page with a title and nothing under it. The emptiness law
// reaches modals: a fullscreen page with no rows is the loudest way of saying
// nothing.
func TestTheTaskPageRefusesToOpenWithNoTasksAtAll(t *testing.T) {
	a, _, _ := taskApp(t)

	drive(t, a, ctrlDot())
	if a.taskSheet.open {
		t.Fatal("ctrl+. raised an empty task page")
	}

	a.slash("/history")
	if a.taskSheet.open {
		t.Fatal("/history raised an empty task page")
	}
	if text := taskText(a); !strings.Contains(text, taskSheetEmpty) {
		t.Fatalf("/history said nothing about why it opened nothing:\n%s", text)
	}
}

// THE NOTE AND THE BODY NEVER DISAGREE ABOUT WHETHER THE PLACE IS EMPTY.
//
// This is the emptiness law with two halves, and the wave that broke it broke
// exactly this: the body drew three sentences teaching what tasks are while the
// line above the composer read `5 running · 1 earlier`. One reading answers both,
// so there are only ever three honest frames — prose and no count, rows and a
// count, or a query that found nothing saying so.
func TestTheTasksNoteAndItsBodyNeverDisagreeAboutBeingEmpty(t *testing.T) {
	a, _, _ := taskApp(t)

	// Nothing anywhere: the tab bar still walks in, the body teaches, and the
	// note says NOTHING — a count beside that prose is the pair the law forbids.
	a.showPage(pageTasks)
	if !a.taskSheet.open {
		t.Fatal("the tab bar did not walk into an empty tasks place")
	}
	if note := a.placeNote(a.width); len(note) != 0 {
		t.Fatalf("an empty place counted what it does not have: %q", note)
	}
	text := taskSheetText(a)
	if !strings.Contains(text, "tasks is the history of work this machine has run.") {
		t.Fatalf("the empty place does not say what it is for:\n%s", text)
	}
	a.closeTaskSheet()

	// Work behind it: the body draws rows and the note counts exactly them, and
	// the teaching prose is gone.
	railRun(a)
	if !a.openTaskSheet() {
		t.Fatal("the place refused to open over this window's own work")
	}
	if note := plain(strings.Join(a.placeNote(a.width), "\n")); !strings.Contains(note, "4 "+taskSheetNowHead) {
		t.Fatalf("the note does not count the rows the body drew: %q", note)
	}
	if text := taskSheetText(a); strings.Contains(text, "tasks is the history of work") {
		t.Fatalf("a place with rows on it taught what a task is:\n%s", text)
	}

	// AND A QUERY THAT MATCHED NOTHING IS NOT AN EMPTY PLACE. There is work here;
	// the words hid it. The note says so and the body stays blank rather than
	// teaching somebody who did not ask.
	drive(t, a, key("z"), key("z"))
	note := plain(strings.Join(a.placeNote(a.width), "\n"))
	if !strings.Contains(note, taskSheetFilterNone) {
		t.Fatalf("a query that matched nothing said nothing: %q", note)
	}
	if text := taskSheetText(a); strings.Contains(text, "tasks is the history of work") {
		t.Fatalf("a filtered-empty place taught what a task is:\n%s", text)
	}
}

// THE PAGE'S COMMAND IS /history AND IT IS NOT SPELLED WITH "task". /task means
// give aforge work — three rows of the list say so — and a plural beside them was
// a command that answered the muscle memory for starting one.
func TestTheTaskPageCommandIsHistoryAndNothingSpellsItTasks(t *testing.T) {
	var named bool
	for _, c := range commands {
		if c.name == "history" {
			named = true
		}
		for _, word := range append([]string{c.name}, c.alias...) {
			if word == "tasks" {
				t.Fatalf("/tasks is back in the command list, beside /task")
			}
		}
	}
	if !named {
		t.Fatal("no /history row in the command list")
	}

	a, _, _ := taskApp(t)
	railRun(a)
	a.slash("/history")
	if !a.taskSheet.open {
		t.Fatal("/history did not open the task page")
	}
	// And the place names itself on the tab bar, with the word the tab bar and
	// the manual both call it. The COMMAND keeps its own older word — /history
	// still opens this, and [taskSheetWord] is still what that command is called
	// — which is why the two are checked apart.
	if text := taskSheetText(a); !strings.Contains(text, pageTasks.word()) {
		t.Fatalf("the place does not say what it is:\n%s", text)
	}
}

// THE PLACE DRAWS THIS WINDOW'S OWN WORK BESIDE EVERY OTHER CONVERSATION'S, AND
// THE COLUMN CANNOT DO EITHER.
//
// This used to pin a `running` TREE over a flat `earlier` list, with the column's
// own connectors above and nothing but the project's record below. The tree is
// gone — the place is the machine's whole record and groups by what you do next
// — but the laws underneath it are not: this session's live work reaches the
// page (it is in no file until it lands, which is the bug that started this),
// work another conversation ran reaches it too, and the two are grouped by what
// you do about them rather than by whose they are.
func TestTheTaskPageDrawsThisWindowsWorkBesideEveryOtherConversations(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("4", "sweep-the-call-sites", "Sweep the call sites", 3*time.Hour),
		pastTask("9", "port-the-parser", "Port the parser", 40*time.Hour),
	}

	if !a.openTaskSheet() {
		t.Fatal("the page refused to open on a session with work in it")
	}
	text := taskSheetText(a)

	// THIS WINDOW'S OWN LIVE WORK IS ON THE PAGE. An ordinary task writes no row
	// into the project's file until it lands, so the graph is the only authority
	// for it and a page that read the file alone showed none of it.
	for _, want := range []string{
		taskSheetNowHead, "Ship the port", "Write the tree", "Cut the goldens", "Wire the seam",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("this window's own work is missing %q:\n%s", want, text)
		}
	}
	// A SETTLED MEMBER OF A LIVE FAMILY IS STILL ON THE PAGE. It is filed by what
	// it IS rather than by what it hangs off, which is the whole of the regrouping.
	if !strings.Contains(text, "Read the law") {
		t.Fatalf("a settled node of this session dropped off the page:\n%s", text)
	}
	// AND SO IS WORK ANOTHER CONVERSATION RAN, which the column cannot show at all.
	for _, want := range []string{"Sweep the call sites", "Port the parser", taskSheetPastHead} {
		if !strings.Contains(text, want) {
			t.Fatalf("the page is missing %q:\n%s", want, text)
		}
	}
	// THE GROUPING IS BY WHAT YOU DO NEXT AND NEVER BY WHOSE WORK IT IS: what is
	// running leads, what landed today follows, and everything older is last.
	running := strings.Index(text, taskSheetNowHead)
	today := strings.Index(text, "done today")
	earlier := strings.Index(text, taskSheetPastHead)
	if running < 0 || today < running || earlier < today {
		t.Fatalf("the sections are absent or out of order (%d/%d/%d):\n%s", running, today, earlier, text)
	}
	if at := strings.Index(text, "Port the parser"); at < earlier {
		t.Fatalf("work from forty hours ago is drawn above %q:\n%s", taskSheetPastHead, text)
	}
	if at := strings.Index(text, "Sweep the call sites"); at < today || at > earlier {
		t.Fatalf("work that landed today is not under `done today`:\n%s", text)
	}
	// NO SHAPE IS CLAIMED. The connectors said which node hangs off which; a list
	// grouped by state has no parentage to draw, and drawing one would be a claim
	// about kinship the grouping has just thrown away.
	for _, gone := range []string{treeBranch, treeLast} {
		if strings.Contains(text, gone) {
			t.Fatalf("the place drew a tree connector %q:\n%s", gone, text)
		}
	}
	// And the note counts every section it drew, in the same words they are
	// headed with, with none of them written as a zero.
	for _, want := range []string{"4 " + taskSheetNowHead, "2 done today", "1 " + taskSheetPastHead} {
		if !strings.Contains(text, want) {
			t.Fatalf("the note does not count what is on the page (%q):\n%s", want, text)
		}
	}
	if strings.Contains(text, "0 "+taskSheetNowHead) || strings.Contains(text, "0 "+taskSheetPastHead) {
		t.Fatalf("the note wrote a section as a zero:\n%s", text)
	}
}

// ONE PIECE OF WORK IS DRAWN ONCE, however many authorities know about it. The
// project's index carries this session's live rows and so does the graph they
// came off, so a node in both is one node: the pair (conversation, id) is what
// identifies a row of work, and the freshest authority wins it.
func TestTheTaskPageDoesNotRepeatWorkTheTreeIsAlreadyShowing(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		{
			ID: "3", Name: "write-the-tree", Label: "Write the tree", Title: "Write the tree",
			Status: string(session.TaskRunning), SessionID: "this-one",
		},
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}

	if !a.openTaskSheet() {
		t.Fatal("the page refused to open")
	}
	text := taskSheetText(a)
	if n := strings.Count(text, "Write the tree"); n != 1 {
		t.Fatalf("the running node is drawn %d times, want once:\n%s", n, text)
	}
}

// ONLY ONE PAGE MAY BELIEVE IT OWNS THE FRAME. Opening either closes the other,
// in both directions, because view.go draws the settings panel first and a page
// opened under it would take the keyboard and never be seen.
func TestTheTwoFullscreenPagesAreNeverBothOpen(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)

	a.openSettings()
	if !a.openTaskSheet() {
		t.Fatal("the task page refused to open over the settings panel")
	}
	if a.sheet.open {
		t.Fatal("opening the task page left the settings panel open under it")
	}

	a.openSettings()
	if a.taskSheet.open {
		t.Fatal("opening the settings panel left the task page open under it")
	}
}

// THE PAGE WALKS ITS ROWS AND STEPS OVER THE SECTION WORDS. A cursor that could
// land on a rule is a cursor that answers enter with nothing.
func TestTheTaskPageCursorNeverLandsOnASectionWord(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open")
	}

	lines := a.tasksFiltered().lay(a.width)
	for step := 0; step < len(lines)+4; step++ {
		if _, ok := a.taskSheetCurrent(); !ok {
			t.Fatalf("the cursor fell off the page after %d steps down", step)
		}
		if kind := lines[a.taskSheet.cursor].kind; kind != tasksLineTask {
			t.Fatalf("the cursor landed on a line of kind %v, which answers to nothing", kind)
		}
		drive(t, a, key("down"))
	}
	// It clamps at the end rather than wrapping, the way every other list here
	// walks, and end takes it there in one press.
	stops := a.taskSheet.stops(a)
	if len(stops) < 2 {
		t.Fatalf("the fixture left %d rows to walk", len(stops))
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnd})
	if a.taskSheet.cursor != stops[len(stops)-1] {
		t.Fatalf("end left the cursor on line %d, want the last row at %d", a.taskSheet.cursor, stops[len(stops)-1])
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyHome})
	if a.taskSheet.cursor != stops[0] {
		t.Fatalf("home left the cursor on line %d, want the first row at %d", a.taskSheet.cursor, stops[0])
	}
}

// enter ON A NODE THIS SESSION HOLDS OPENS ITS ROOM, which is exactly what enter
// on the roster does. One door onto one task, reached from two lists.
func TestEnterOnTheTaskPageOpensThatTasksRoom(t *testing.T) {
	// roomApp's one node is 7, and it is the only family here, so the cursor opens
	// on it.
	a, _, _ := roomApp(t)
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open")
	}

	drive(t, a, key("enter"))
	if a.taskSheet.open {
		t.Fatal("opening a room left the page standing over it")
	}
	if a.room == nil || a.room.id != 7 {
		t.Fatalf("enter did not open the focused task's room: %+v", a.room)
	}
}

// enter ON WORK ANOTHER CONVERSATION RAN GOES INSIDE IT, because there is no
// room to open: a room is a live lane onto a node in THIS session's graph, and
// that session is closed. What it opens instead is the card — everything the
// project wrote down about that piece of work, over the same page, with the list
// still underneath (taskrecord.go).
func TestEnterOnAnEarlierConversationsTaskGoesInsideIt(t *testing.T) {
	a, _, _ := taskApp(t)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open on a project with only a record")
	}
	// THE FOOT SAYS WHICH DOOR enter IS. A page that promised a room over work
	// that has none would be lying about its own key.
	// The clause is asked for on its own. The router appends `alt+. map · tab next
	// place` to every place's sentence ([placeTailed]), so the foot is assembled
	// rather than quoted — and [tasksEnterInsideWord] is exactly the clause that
	// says which door enter is, which is what this test is about.
	if text := taskSheetText(a); !strings.Contains(text, tasksEnterInsideWord) {
		t.Fatalf("the foot promises a room over a task that has none:\n%s", text)
	}
	if text := taskSheetText(a); strings.Contains(text, tasksEnterRoomWord) {
		t.Fatalf("the foot offers a room over work this window never ran:\n%s", text)
	}

	drive(t, a, key("enter"))
	if !a.taskSheet.open || !a.taskSheet.detailOn {
		t.Fatalf("enter did not go inside: open=%v inside=%v", a.taskSheet.open, a.taskSheet.detailOn)
	}
	card := taskSheetText(a)
	for _, want := range []string{"Port the parser", "done", "it came home clean", taskCardKeys} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not say %q:\n%s", want, card)
		}
	}

	// esc BACKS OUT ONE LAYER: the list, not the conversation.
	drive(t, a, key("esc"))
	if !a.taskSheet.open || a.taskSheet.detailOn {
		t.Fatalf("esc did not come back to the list: open=%v inside=%v", a.taskSheet.open, a.taskSheet.detailOn)
	}
	drive(t, a, key("esc"))
	if a.taskSheet.open {
		t.Fatal("the second esc did not close the page")
	}
}

// AND THE MENTION SURVIVES ON `m`, in the card's own foot. It could not stay on
// enter and it could not move to a letter on the LIST — every printable key
// there is the filter — so it lives on the one page here that is read rather
// than typed at.
func TestMFromInsideAnOldTaskStillWritesTheMention(t *testing.T) {
	a, _, _ := taskApp(t)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open on a project with only a record")
	}
	drive(t, a, key("enter"))

	// A half-written sentence is KEPT: the name is appended to it, because the box
	// is where the person was part-way through saying what the name was for.
	a.input.setText("what happened in")
	drive(t, a, key("m"))
	if a.taskSheet.open {
		t.Fatal("writing the mention left the page up")
	}
	if got := string(a.input.value); got != "what happened in @port-the-parser " {
		t.Fatalf("the draft reads %q", got)
	}
}

// THE CARD SAYS WHAT THE RECORD KNOWS AND NOTHING IT DOES NOT. A row that spent
// nothing has no money line, one that wrote nothing has no file count — the
// emptiness law reaching every line of a card whose whole content is optional.
func TestTheRecordCardDrawsOnlyTheFactsItHas(t *testing.T) {
	a, _, _ := taskApp(t)
	rich := pastTask("9", "port-the-parser", "Port the parser", time.Hour)
	rich.Model, rich.Cost, rich.Tokens, rich.FilesChanged = "anthropic/claude-sonnet-4.5", 0.42, 12000, 3
	rich.ArtifactURI = "git:task/port-the-parser-9c1a2f"
	bare := pastTask("11", "mix-the-audio", "Mix the audio", 2*time.Hour)
	bare.Outcome = ""
	a.comp.tasks = []session.TaskIndexEntry{rich, bare}
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open")
	}

	drive(t, a, key("enter"))
	card := taskSheetText(a)
	for _, want := range []string{
		"anthropic/claude-sonnet-4.5", "$0.42", "12k tok", "3 files changed",
		taskCardBranchWord + " · task/port-the-parser-9c1a2f",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not say %q:\n%s", want, card)
		}
	}
	drive(t, a, key("esc"))

	// And the row that knows none of those says none of them, rather than $0.00,
	// 0 tok and a count of nothing.
	drive(t, a, key("down"))
	drive(t, a, key("enter"))
	card = taskSheetText(a)
	for _, never := range []string{"$0.00", "0 tok", "0 files changed", taskCardBranchWord, taskCardTreeWord} {
		if strings.Contains(card, never) {
			t.Fatalf("the card wrote %q about a task nothing is known about:\n%s", never, card)
		}
	}
}

// THE LAST THING THE TASK SAID IS READ OFF ITS OWN JOURNAL, which is what the
// row's transcript address was carried for: the outcome above it is that
// message's first sentence and nothing more (session's PeekReport).
func TestTheRecordCardShowsWhatTheTaskSaidAtTheEnd(t *testing.T) {
	a, _, _ := taskApp(t)
	journal := filepath.Join(t.TempDir(), "20260819-120133_9.jsonl")
	lines := []string{
		`{"type":"message","role":"user","content":"port the parser"}`,
		`{"type":"message","role":"assistant","content":"looking at the grammar first"}`,
		`{"type":"message","role":"assistant","content":"Ported the parser and the suite passes.\nNothing else was touched."}`,
	}
	if err := os.WriteFile(journal, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := pastTask("9", "port-the-parser", "Port the parser", time.Hour)
	entry.TranscriptURI = "file://" + journal
	a.comp.tasks = []session.TaskIndexEntry{entry}
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open")
	}

	// The read is a command off the loop, which is what [drive] runs for us.
	drive(t, a, key("enter"))
	if a.taskSheet.tail != mustPeekReport(t, journal) {
		t.Fatalf("the card read back %q", a.taskSheet.tail)
	}
	card := taskSheetText(a)
	for _, want := range []string{
		taskCardTailHead, "Ported the parser and the suite passes.", "Nothing else was touched.",
		taskCardTranscriptWord,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not say %q:\n%s", want, card)
		}
	}
	// It is the LAST thing said and not everything said.
	if strings.Contains(card, "looking at the grammar first") {
		t.Fatalf("the card printed the whole transcript:\n%s", card)
	}
}

// mustPeekReport reads a node's journal the way the card's own command does.
func mustPeekReport(t *testing.T, path string) string {
	t.Helper()
	tail, ok := session.PeekReport(path)
	if !ok {
		t.Fatalf("nothing was read back out of %s", path)
	}
	return tail
}

// A ROW WHOSE TRANSCRIPT IS GONE SAYS SO. The path is printed on the band above,
// so silence under it would read as a card that gave up half way.
func TestTheRecordCardSaysWhenTheTranscriptIsGone(t *testing.T) {
	a, _, _ := taskApp(t)
	entry := pastTask("9", "port-the-parser", "Port the parser", time.Hour)
	entry.TranscriptURI = "file://" + filepath.Join(t.TempDir(), "never-written.jsonl")
	a.comp.tasks = []session.TaskIndexEntry{entry}
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open")
	}
	drive(t, a, key("enter"))
	a.taskSheet.tailRead = true
	if card := taskSheetText(a); !strings.Contains(card, taskCardTailGone) {
		t.Fatalf("the card said nothing about a transcript that is not there:\n%s", card)
	}
}

// A TASK THIS SESSION IS HOLDING STILL OPENS ITS ROOM. Only work from a
// conversation that is closed opens the card.
func TestALiveRowStillOpensItsRoomFromTheColumn(t *testing.T) {
	// roomApp's one node is 7, and it is the only family here.
	a, _, _ := roomApp(t)
	a.profileDir = t.TempDir()
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	rosterText(a, a.viewHeight())

	drive(t, a, ctrlT())
	drive(t, a, key("enter"))
	if a.room == nil || a.room.id != 7 {
		t.Fatalf("enter on a live row did not open its room: %+v", a.room)
	}
	if a.taskSheet.detailOn {
		t.Fatal("a live row opened the record card")
	}
}

// ── the column's one door onto the page ─────────────────────────────────────

// THE LINE IS DRAWN WHEN THERE IS WORK BEHIND IT AND NOT OTHERWISE, and it WEARS
// THE NAME OF WHAT IS BEHIND IT. A door over a column that is already showing
// everything is a row that promises a page and delivers the list you were
// looking at.
func TestTheColumnOffersItsDoorOnlyWhenThereIsSomethingBehindIt(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	// One node, nothing folded, and no record: the column is showing the whole of
	// what there is to show.
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	for _, gone := range []string{taskSheetMoreHint, taskSheetPastHint} {
		if rail := rosterText(a, a.viewHeight()); strings.Contains(rail, gone) {
			t.Fatalf("the column offered %q with nothing behind it:\n%s", gone, rail)
		}
	}

	// A landed node of THIS session's, already on the column, still earns nothing:
	// it is the same row said twice.
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("1", "ship-the-port", "Ship the port", time.Minute),
	}
	for _, gone := range []string{taskSheetMoreHint, taskSheetPastHint} {
		if rail := rosterText(a, a.viewHeight()); strings.Contains(rail, gone) {
			t.Fatalf("the column offered %q for a row it is already drawing:\n%s", gone, rail)
		}
	}

	// Work an EARLIER conversation ran is work this column does not show at all, so
	// the door appears — and it says `earlier`, which is what is behind it.
	a.comp.tasks = append(a.comp.tasks,
		pastTask("9", "port-the-parser", "Port the parser", 40*time.Hour))
	rail := rosterText(a, a.viewHeight())
	if !strings.Contains(rail, taskSheetPastHint) {
		t.Fatalf("the column hid a record this session never ran:\n%s", rail)
	}
	// AND THERE IS EXACTLY ONE OF IT. Two lines onto one page is two doors out of
	// a room with one.
	if n := strings.Count(rail, taskSheetKey+" "); n != 1 {
		t.Fatalf("the column drew %d doors onto the page, want 1:\n%s", n, rail)
	}
	// Raw rows, not railText: the reading helper strips ANSI, and paint can
	// only be asserted where the paint still is.
	painted := strings.Join(a.railRows(a.viewHeight()), "\n")
	if !strings.Contains(painted, a.pal.data(taskSheetKey)) ||
		!strings.Contains(painted, a.pal.dim(" "+taskSheetPastHead)) {
		t.Fatalf("the page door does not use the shared hint palette:\n%q", painted)
	}
	// IT SITS ABOVE THE COLUMN'S OWN DOOR. The way out of anything is the last
	// line of it, and this one is a way further in.
	lines := strings.Split(strings.TrimRight(rail, "\n"), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.Contains(last, railStowHint) {
		t.Fatalf("the column's own door is no longer its last line: %q", last)
	}
}

// A FOLDED FAMILY EARNS IT TOO, because a folded root is one row standing for
// work the column is deliberately not drawing — and with no record behind it the
// same line says `view more` instead, because there is no earlier work to
// promise.
func TestAFoldedFamilyEarnsTheViewMoreLine(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)
	if rail := rosterText(a, a.viewHeight()); strings.Contains(rail, taskSheetMoreHint) {
		t.Fatalf("an open column with no record offered more:\n%s", rail)
	}
	a.railSetOpen(a.tasks[1], false)
	rail := rosterText(a, a.viewHeight())
	if !strings.Contains(rail, taskSheetMoreHint) {
		t.Fatalf("a folded family did not earn the line:\n%s", rail)
	}
	if strings.Contains(rail, taskSheetPastHint) {
		t.Fatalf("a column with no record promised earlier work:\n%s", rail)
	}
}

// AND THE LINE IS A BUTTON AS WELL AS A KEY. A row that names a chord and cannot
// be pressed is an affordance for one of the two hands — and one that cannot
// light under the pointer is a button nothing says is a button.
func TestPressingViewMoreOpensTheTaskPage(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)
	a.railSetOpen(a.tasks[1], false)

	at := railMoreLine(t, a)
	if _, took := a.railPress(a.bodyWidth()+4, at+a.topHeight()); !took {
		t.Fatal("the press fell through the column")
	}
	if !a.taskSheet.open {
		t.Fatal("pressing the door did not open the task page")
	}
	// The column is left exactly as it was: the page is somewhere you go and come
	// back from, not a state the column enters.
	if a.railAway {
		t.Fatal("opening the page put the column away")
	}
	a.closeTaskSheet()

	// AND IT LIGHTS UNDER THE POINTER, because a button nothing says is a button
	// is a button for the hand that already knew. The pointer arriving anywhere on
	// the column can put the width offer in the footer and move the rows under it,
	// so the line is found again once the hand is inside.
	a.setHover(a.bodyWidth()+4, at+a.topHeight())
	at = railMoreLine(t, a)
	a.setHover(a.bodyWidth()+4, at+a.topHeight())
	if !a.hoveringRailMore() {
		t.Fatalf("the pointer over the column's door onto the page lit nothing: %+v", a.hot)
	}
}

// railMoreLine is the row of the drawn column carrying its door onto the task
// page.
func railMoreLine(t *testing.T, a *app) int {
	t.Helper()
	height := a.viewHeight()
	view, _ := a.railView(height)
	for i, line := range view {
		if line.more {
			return i
		}
	}
	t.Fatalf("the column drew no door onto the page:\n%s", rosterText(a, height))
	return -1
}

// ── the column keeps what is running ────────────────────────────────────────

// WORK THAT IS RUNNING IS NEVER SCROLLED OFF THE COLUMN. The families already
// sort so that everything moving leads; this is the other half of it — a cursor
// walked down through sixty landed nodes takes the window with it and leaves the
// running head where it is.
func TestRunningWorkStaysOnTheColumnHoweverFarTheCursorWalks(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	for i := 2; i <= 60; i++ {
		a.taskUpdate(update(uint64(i), "landed "+itoa(i), session.TaskDone,
			session.TaskNotice{Merge: mergeWordMerged}))
	}

	drive(t, a, ctrlT())
	for i := 0; i < 40; i++ {
		drive(t, a, key("down"))
	}
	rail := rosterText(a, a.viewHeight())
	if !strings.Contains(rail, "Ship the port") {
		t.Fatalf("the running task scrolled off the column:\n%s", rail)
	}
	// And the record under it did move, which is what the cursor was walking
	// through: the pin is the head alone.
	if strings.Contains(rail, "landed 2 ") {
		t.Fatalf("nothing scrolled at all:\n%s", rail)
	}
}

// ── typing at the page ──────────────────────────────────────────────────────

// TYPING FILTERS BOTH SECTIONS AT ONCE. A record of four hundred tasks is
// reached by remembering a word of a title, and the page is the whole frame —
// there is no box underneath for a letter to land in.
func TestTypingOnTheTaskPageFiltersBothSections(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
		pastTask("11", "mix-the-audio", "Mix the audio", 40*time.Hour),
	}
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open")
	}

	// "port" is in one live title and one row of another conversation's work, so
	// both sections survive it and everything else goes.
	drive(t, a, key("p"), key("o"), key("r"), key("t"))
	text := taskSheetText(a)
	for _, want := range []string{
		taskSheetNowHead, "Ship the port",
		"done today", "Port the parser",
		taskSheetFilterWord + "port",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the filtered page is missing %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"Write the tree", "Mix the audio"} {
		if strings.Contains(text, gone) {
			t.Fatalf("%q survived the filter:\n%s", gone, text)
		}
	}

	// A SECTION WITH NO MATCH IS NOT DRAWN AT ALL, heading and all: a rule with
	// nothing under it says the query found something and lost it.
	drive(t, a, key("ctrl+u"))
	for _, r := range "audio" {
		drive(t, a, key(string(r)))
	}
	text = taskSheetText(a)
	if strings.Contains(text, taskSheetNowHead) {
		t.Fatalf("the %q rule is drawn over nothing:\n%s", taskSheetNowHead, text)
	}
	if !strings.Contains(text, "Mix the audio") {
		t.Fatalf("the record row that matches is gone:\n%s", text)
	}

	// AND A QUERY THAT MATCHES NOTHING SAYS SO, rather than leaving a blank page
	// that reads as broken.
	drive(t, a, key("ctrl+u"))
	drive(t, a, key("z"), key("z"))
	if text := taskSheetText(a); !strings.Contains(text, taskSheetFilterNone) {
		t.Fatalf("an empty filter result says nothing:\n%s", text)
	}
	// backspace edits it rather than closing anything.
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := a.taskSheetFilter(); got != "z" {
		t.Fatalf("backspace left the filter %q", got)
	}
}

// esc BACKS OUT ONE LAYER AT A TIME — the filter first, the page second — which
// is the settings panel's own layering. A key that closed the page from inside a
// filter would throw away the only thing on screen the person typed.
func TestEscOnTheTaskPageClearsTheFilterBeforeItCloses(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open")
	}
	drive(t, a, key("t"), key("r"), key("e"), key("e"))
	if a.taskSheetFilter() != "tree" {
		t.Fatalf("the filter reads %q", a.taskSheetFilter())
	}

	drive(t, a, key("esc"))
	if !a.taskSheet.open {
		t.Fatal("the first esc closed the page instead of the filter")
	}
	if a.taskSheetFiltering() {
		t.Fatalf("the first esc left the filter %q", a.taskSheetFilter())
	}
	drive(t, a, key("esc"))
	if a.taskSheet.open {
		t.Fatal("the second esc did not close the page")
	}

	// The chord is not a layer: it closes the page from inside a filter.
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open again")
	}
	drive(t, a, key("t"))
	drive(t, a, ctrlDot())
	if a.taskSheet.open {
		t.Fatal("ctrl+. did not close a filtered page")
	}
	// And a page opened again opens unfiltered: the query goes with the page.
	if !a.openTaskSheet() {
		t.Fatal("the page refused to open a third time")
	}
	if a.taskSheetFiltering() {
		t.Fatalf("the page reopened still filtered by %q", a.taskSheetFilter())
	}
}

// ── the column is this conversation's work alone ────────────────────────────

// THE COLUMN DRAWS NO ROW OF THE PROJECT'S RECORD. It carried a dulled sample of
// six under this session's own work for a while; six rows out of two thousand is
// a sample rather than a record, it stood where the column's own label goes, and
// the roster's cursor walked out of this conversation into another one without
// the column ever saying so. What it has instead is the door.
func TestTheColumnDrawsNoneOfTheProjectsRecord(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
		pastTask("11", "mix-the-audio", "Mix the audio", 40*time.Hour),
	}

	rail := rosterText(a, a.viewHeight())
	if !strings.Contains(rail, "Ship the port") {
		t.Fatalf("this conversation's own work is not on the column:\n%s", rail)
	}
	for _, gone := range []string{"Port the parser", "Mix the audio"} {
		if strings.Contains(rail, gone) {
			t.Fatalf("the column drew %q, which belongs to the task page:\n%s", gone, rail)
		}
	}
	// AND THE DOOR STANDS IN THEIR PLACE, so the work is one press away rather
	// than behind a chord nobody has been told about.
	if !strings.Contains(rail, taskSheetPastHint) {
		t.Fatalf("the column dropped the record and offered no door onto it:\n%s", rail)
	}
	// The rows the record used to take are the session's own now, whatever the
	// column's height: a tall column draws no more of the record than a short one.
	for _, height := range []int{9, 20, 40} {
		text := rosterText(a, height)
		if strings.Contains(text, "Port the parser") {
			t.Fatalf("a %d-row column drew the record:\n%s", height, text)
		}
	}
}

// THE RECORD NEVER COSTS RUNNING WORK A ROW, which used to be a rule the layout
// kept and is now true by construction: there is nothing of it on the column to
// evict anything.
func TestAColumnFullOfRunningWorkKeepsAllOfItAndStillOffersTheDoor(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	for i := 1; i <= 8; i++ {
		a.taskUpdate(update(uint64(i), "running "+itoa(i), session.TaskRunning, session.TaskNotice{}))
	}
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("90", "port-the-parser", "Port the parser", time.Hour),
	}

	rail := rosterText(a, 9)
	if strings.Contains(rail, "Port the parser") {
		t.Fatalf("the record took a row from running work:\n%s", rail)
	}
	// FOUR AND NOT FIVE, because the column opens with its section label now
	// (margin.go): the label is geography and it is pinned above the work like
	// every other line of the moving head, so a nine-row column spends one of its
	// rows saying where it is. What it must never spend a row on is the RECORD,
	// which is what this test is about and is still true above.
	for i := 1; i <= 4; i++ {
		if !strings.Contains(rail, "running "+itoa(i)) {
			t.Fatalf("running %d was evicted from the column:\n%s", i, rail)
		}
	}
	if !strings.Contains(rail, taskSheetPastHint) {
		t.Fatalf("the column offered no door onto the record:\n%s", rail)
	}
}

// THE EMPTY COLUMN SAYS WHAT IT IS FOR WHATEVER THE PROJECT HAS BEHIND IT. The
// label is about THIS CONVERSATION — it has run nothing yet — and the door under
// it is about the project. The two are not in competition, which they were while
// the record's rows stood where the label goes.
func TestAnEmptySessionSaysSoAndStillNamesTheDoorOntoTheRecord(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()

	// Nothing anywhere: the column stands, because it always does, and says what
	// the place is for. No door — there is nothing behind one.
	if !a.railShowing() {
		t.Fatal("the permanent column did not stand on an empty session")
	}
	bare := rosterText(a, a.viewHeight())
	if strings.Contains(bare, "no tasks yet") {
		t.Fatalf("an empty column announced its emptiness:\n%s", bare)
	}
	for _, gone := range []string{taskSheetPastHint, taskSheetMoreHint} {
		if strings.Contains(bare, gone) {
			t.Fatalf("an empty project drew %q, which it does not have:\n%s", gone, bare)
		}
	}

	// A record behind it, and the same empty session: the label STAYS — this
	// conversation has still run nothing — and the door appears under it.
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	rail := rosterText(a, a.viewHeight())
	if strings.Contains(rail, "no tasks yet") {
		t.Fatalf("a record-only column announced this session's emptiness:\n%s", rail)
	}
	if !strings.Contains(rail, taskSheetPastHint) {
		t.Fatalf("the record-only column offered no door onto the record:\n%s", rail)
	}
	if strings.Contains(rail, "Port the parser") {
		t.Fatalf("the column drew a row of the record:\n%s", rail)
	}
	for _, absent := range []string{marginTasksWord, marginStandWord} {
		if strings.Contains(rail, absent) {
			t.Fatalf("the empty column draws the empty section label %q:\n%s", absent, rail)
		}
	}

	// ctrl+g still closes a column standing on the label alone — it is thirty
	// columns of somebody else's paragraph either way.
	drive(t, a, key("ctrl+g"))
	if a.railShowing() {
		t.Fatal("ctrl+g left an empty column standing")
	}
	drive(t, a, key("ctrl+g"))
	if !a.railShowing() {
		t.Fatal("ctrl+g did not bring the empty column back")
	}
}

// THE ROSTER'S WALK STOPS AT THIS CONVERSATION'S LAST TASK. It used to carry on
// into the record's rows, which is how a person holding ↓ left this conversation
// for another one; the record is the task page's, and the page is where it is
// walked.
func TestTheRostersCursorStopsAtTheLastTaskOfThisConversation(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "Mix audio", session.TaskDone, session.TaskNotice{Merge: mergeWordMerged}))
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	rosterText(a, a.viewHeight())

	drive(t, a, ctrlT())
	if a.railWhere.id != 1 {
		t.Fatalf("ctrl+t did not park the cursor on this session's node: %+v", a.railWhere)
	}
	for i := 0; i < 20; i++ {
		drive(t, a, key("down"))
	}
	if a.railWhere.id != 2 {
		t.Fatalf("the walk did not clamp on this conversation's last task: %+v", a.railWhere)
	}
	// AND enter DOWN THERE IS STILL A ROOM'S DOOR AND NEVER A RECORD CARD'S. Every
	// row of this column is a node this conversation holds, so there is no row for
	// the card to be about (the room itself is asserted where a fixture has one:
	// TestALiveRowStillOpensItsRoomFromTheColumn).
	drive(t, a, key("enter"))
	if a.taskSheet.detailOn {
		t.Fatal("a row of this conversation's work opened a record card")
	}
	if a.railWhere.id != 2 {
		t.Fatalf("enter moved the cursor off this conversation's work: %+v", a.railWhere)
	}
}

// AND ctrl+t FALLS THROUGH ON A COLUMN MADE ONLY OF THE PROJECT'S RECORD. There
// are no rows down there to put a cursor on any more, and a key that hands the
// keyboard to an empty list is a key that does nothing — which is the same rule
// ctrl+t has always had ([app.railAvail]).
func TestCtrlTFallsThroughOnAColumnWithOnlyTheProjectsRecord(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	rosterText(a, a.viewHeight())

	drive(t, a, ctrlT())
	if a.railHold {
		t.Fatal("ctrl+t took the keyboard for a column with no rows on it")
	}
	// The door is still there, and it is how the record is reached.
	if rail := rosterText(a, a.viewHeight()); !strings.Contains(rail, taskSheetPastHint) {
		t.Fatalf("the record-only column offered no door:\n%s", rail)
	}
}

// ── a task is named by its title, everywhere ────────────────────────────────

// A NAME ARRIVING LATE IS STILL A NAME. The de-dup that keeps one landing from
// being drawn twice is keyed on (id, state), and it used to throw away the
// notice that carried the TITLE when the state had not moved — so a node
// published before its title was known was called "task 19" on the column, on
// the strip, in its room's header and on the card that landed, for the whole of
// its life. The id-form is what a NAMELESS node is called and never what a named
// one is.
func TestANodeTakesItsNameFromALaterUpdateInTheSameState(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(19, "", session.TaskRunning, session.TaskNotice{}))
	node := a.tasks[19]
	if node == nil {
		t.Fatal("the untitled update admitted no node")
	}
	if node.title != taskIDWord(19) {
		t.Fatalf("a node nobody has named is called %q, want %q", node.title, taskIDWord(19))
	}

	// A room opened on it while it is nameless carries the same word — and takes
	// the real one the moment it arrives, because a header taken once at the door
	// is the one place the id-form could outlive the naming.
	a.openRoom(19, node.title)

	a.taskUpdate(update(19, "Rebuild the quant engine", session.TaskRunning, session.TaskNotice{}))
	if node.label != "Rebuild the quant engine" {
		t.Fatalf("the node kept the label %q", node.label)
	}
	if strings.Contains(node.title, taskIDWord(19)) {
		t.Fatalf("the row is still called %q", node.title)
	}
	if !strings.Contains(rosterText(a, a.viewHeight()), "Rebuild the quant") {
		t.Fatalf("the column does not name the task:\n%s", rosterText(a, a.viewHeight()))
	}
	if a.room != nil && a.room.title == taskIDWord(19) {
		t.Fatalf("the room's header kept the id-form over a node that has a name")
	}

	// AND AN EMPTY TITLE NEVER TAKES A NAME AWAY. A producer that says nothing
	// about the name has not renamed anything — putting "task 19" back over a row
	// that knows what it is would be the same bug from the other side.
	a.taskUpdate(update(19, "", session.TaskRunning, session.TaskNotice{Doing: "writing the engine"}))
	if node.label != "Rebuild the quant engine" {
		t.Fatalf("a nameless update renamed the node to %q", node.label)
	}
}

// A ROW DRAWN UNDER A SENTENCE TAKES THE SHORT NAME WHEN IT LANDS.
//
// This is the other half of the case above, and it is the one people complained
// about: a node admitted with no name of its own is published under whatever
// sentence its door had — the person's opening words, a path they pasted — and
// the column, which shows three words, names the work after the front of that
// sentence. The engine names it a moment later (internal/session's taskname.go)
// and republishes the row, which reaches this surface as an ordinary update in
// the state it was already in.
func TestARowDrawnUnderASentenceTakesTheShortNameWhenItArrives(t *testing.T) {
	a, _, _ := taskApp(t)
	const sentence = "read /Users/me/src and say what the parser does"
	a.taskUpdate(update(4, sentence, session.TaskRunning, session.TaskNotice{}))
	node := a.tasks[4]
	if node == nil {
		t.Fatal("the update admitted no node")
	}
	// Until the name lands the row is the front of the sentence, which is exactly
	// what it was before — the fallback, and never a placeholder word.
	if node.title != "read /Users/me/src and" {
		t.Fatalf("the row is drawn as %q before the name lands", node.title)
	}

	a.taskUpdate(update(4, "parser recon", session.TaskRunning, session.TaskNotice{}))
	if node.title != "parser recon" {
		t.Fatalf("the row is called %q, want the short name", node.title)
	}
	// AND IT IS ON THE COLUMN WHOLE. The engine asks for as many words as this
	// surface draws ([taskTitleWords] is session.TaskNameWords), so a name that
	// was made to fit the column is never cut to fit it.
	if !strings.Contains(rosterText(a, a.viewHeight()), "parser recon") {
		t.Fatalf("the column does not carry the whole name:\n%s", rosterText(a, a.viewHeight()))
	}
}

// THE COLUMN'S WORD CAP AND THE NAMER'S ARE ONE FIGURE. Two copies would drift,
// and the drift is invisible in both directions: a namer asked for more words
// than the column shows pays for words nobody reads, and one asked for fewer
// leaves the column half empty.
func TestTheNameCapIsTheEnginesOwnFigure(t *testing.T) {
	if taskTitleWords != session.TaskNameWords {
		t.Fatalf("the column cuts to %d words and the namer asks for %d",
			taskTitleWords, session.TaskNameWords)
	}
}
