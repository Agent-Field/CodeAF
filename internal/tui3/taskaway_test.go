package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE PROJECT IS BIGGER THAN THIS WINDOW ──────────────────────────────────
//
// Two aforge windows open on one directory used to be able to see nothing of
// each other: a task started in the first appeared on no surface of the second,
// and a task some window was killed in the middle of went on claiming `running`
// in the shared file forever with nobody left to correct it. These are the whole
// of the fix — the history page draws the other windows' work, every record row
// has its claim of running judged against what those windows actually say, and
// the roster's own tree is left exactly as it was.

// awayFake is a surface's agent that also answers "what do the other windows
// have out", and counts how many times it was asked — which is the whole of the
// cache's claim ([app.refreshElsewhere]).
type awayFake struct {
	*taskFake
	away  session.Elsewhere
	reads int
}

func (f *awayFake) Elsewhere() session.Elsewhere {
	f.reads++
	return f.away
}

// awayApp is [taskApp] with the other-windows seam behind it and a pinned clock
// over it, because a cache measured in seconds cannot be tested by waiting.
func awayApp(t *testing.T) (*app, *awayFake, func(time.Duration)) {
	t.Helper()
	agent := &awayFake{taskFake: &taskFake{
		fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4-flash"},
		updates:   make(chan session.Event, 8),
	}}
	a := newTestApp(agent)
	a.width, a.height = 200, 24
	// The places root is pinned for [taskApp]'s reason: the tasks place reads the
	// MACHINE, so an unpinned root reads the developer's own history.
	a.homeRoot = t.TempDir()
	now := taskFixtureNow
	a.clock = func() time.Time { return now }
	return a, agent, func(d time.Duration) { now = now.Add(d) }
}

// window is one other aforge, still speaking, with the work it has out. What
// that window is CALLED is not on it: a window's name comes out of its own
// meta.json, so the reading carries it beside the rows rather than inside them
// ([session.NewElsewhere] takes it as a map).
func window(id string, tasks ...session.PresenceTask) session.SessionPresence {
	return session.SessionPresence{
		SessionID:    id,
		UpdatedAt:    time.Now(),
		State:        session.PresenceWorking,
		RunningTasks: tasks,
	}
}

// theirTask is one row of the project's index another conversation wrote, in
// whatever state the file remembers it.
func theirTask(id, session_, title, status string) session.TaskIndexEntry {
	return session.TaskIndexEntry{
		ID:        id,
		Name:      session.TaskSlug(title),
		Label:     title,
		Title:     title,
		Status:    status,
		SessionID: session_,
	}
}

// A ROW OF THE FILE THAT SAYS `running` GOES WHEREVER SOMETHING TRUE CAN BE SAID
// ABOUT IT. While the window that wrote it is still holding the node it belongs
// under `running` with that window named on it; the moment nothing is behind the
// claim it drops into the record and says `incomplete`.
//
// Both halves matter and the second one is the bug: the row used to be dropped
// on sight for saying `running`, which took a task some window was killed in the
// middle of off every surface this program draws.
func TestARowClaimingToRunMovesWhenNobodyIsBehindIt(t *testing.T) {
	a, agent, _ := awayApp(t)
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		theirTask("3", "the-other-window", "Port the parser", string(session.TaskRunning)),
	}
	agent.away = session.NewElsewhere(time.Now(), nil,
		window("the-other-window", session.PresenceTask{
			ID: "3", Title: "Port the parser", State: string(session.TaskRunning)}))

	a.slash("/history")
	if !a.at(pageTasks) {
		t.Fatal("/history did not open over another window's running work")
	}
	row := awayRowWith(t, taskSheetText(a), "Port the parser")
	if !strings.Contains(row, taskAwayWord) {
		t.Fatalf("a row a window still holds is not drawn as that window's work:\n%s", row)
	}
	if strings.Contains(row, taskRecordStoppedWord) {
		t.Fatalf("a row a window still holds was called incomplete:\n%s", row)
	}
	if section := awaySectionOf(t, a, "Port the parser"); section != taskSheetNowHead {
		t.Fatalf("work another window is holding is filed under %q", section)
	}

	// THE WINDOW GOES AWAY AND THE CLAIM GOES WITH IT. Nothing rewrote the file;
	// what changed is that nobody is standing behind the word in it any more.
	agent.away, a.away = session.Elsewhere{}, elsewhereCache{}
	row = awayRowWith(t, taskSheetText(a), "Port the parser")
	if !strings.Contains(row, taskRecordStoppedWord) {
		t.Fatalf("a row nobody is running does not say %q:\n%s", taskRecordStoppedWord, row)
	}
	if strings.Contains(row, taskAwayWord) {
		t.Fatalf("a row nobody is running is still credited to a window:\n%s", row)
	}
	if section := awaySectionOf(t, a, "Port the parser"); section != taskSheetPastHead {
		t.Fatalf("work nobody is running is filed under %q", section)
	}
}

// AND THE COLUMN DRAWS NONE OF IT. The judgement about a row that claims to be
// running belongs to the page, because the page is the only surface that draws
// the project's record at all now: the column is this conversation's work
// (taskview.go). A row another window is holding is on no row of this column
// whatever it says about itself.
func TestTheColumnDrawsNoRowOfAnotherWindowsWork(t *testing.T) {
	a, agent, _ := awayApp(t)
	a.comp.tasks = []session.TaskIndexEntry{
		theirTask("3", "the-other-window", "Port it", string(session.TaskRunning)),
	}
	agent.away = session.NewElsewhere(time.Now(), nil,
		window("the-other-window", session.PresenceTask{
			ID: "3", Title: "Port it", State: string(session.TaskRunning)}))

	if row, ok := railRowFor(a, 20, "Port it"); ok {
		t.Fatalf("another window's work is on a row of this conversation's column: %q", row)
	}
	// What the column carries instead is the door onto the page that has it.
	if rail := strings.Join(railText(a, 20), "\n"); !strings.Contains(rail, taskSheetPastHint) {
		t.Fatalf("the column offered no door onto the record:\n%s", rail)
	}
	// And it stays off the column when nobody is holding it either: a row that
	// claims to be running with nothing behind it is still not this conversation's
	// work, and the place is where that judgement is drawn.
	agent.away, a.away = session.Elsewhere{}, elsewhereCache{}
	if row, ok := railRowFor(a, 20, "Port it"); ok {
		t.Fatalf("a row nobody is running turned up on the column: %q", row)
	}
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a row nobody is running")
	}
	if row := awayRowWith(t, taskSheetText(a), "Port it"); !strings.Contains(row, taskRecordStoppedWord) {
		t.Fatalf("the place does not say the row is %q:\n%s", taskRecordStoppedWord, row)
	}
}

// awaySectionOf is the section's word a title is drawn under on the page, which
// is the only way to tell "running" from "earlier" apart on a flat frame.
func awaySectionOf(t *testing.T, a *app, title string) string {
	t.Helper()
	for _, item := range a.tasksFiltered().items {
		if item.entry.Title == title {
			return tasksSectionWord(item.section)
		}
	}
	t.Fatalf("no row on the page is %q", title)
	return ""
}

// THE HISTORY PAGE SHOWS WORK THAT IS IN NO FILE. An ordinary task writes no row
// into the project's index until it lands, so the window next door is the ONLY
// place its running work can be read from — and this is the case a person hit:
// a second aforge in a directory that was busy, showing nothing at all.
func TestTheHistoryPageShowsAnotherWindowsRunningWork(t *testing.T) {
	a, agent, _ := awayApp(t)
	agent.away = session.NewElsewhere(time.Now(),
		map[string]string{"the-other-window": "Fix the nil-map crash"},
		window("the-other-window", session.PresenceTask{
			ID: "7", Title: "Sweep the call sites", State: string(session.TaskRunning)}))

	// The session itself has run nothing and the project's file is empty: every
	// row on this page belongs to somebody else.
	if !openTaskPlaceWithRows(a) {
		t.Fatal("/history refused to open over work happening in another window")
	}
	page := taskSheetText(a)
	if !strings.Contains(page, taskSheetNowHead) {
		t.Fatalf("the page has no running section:\n%s", page)
	}
	row := awayRowWith(t, page, "Sweep the call sites")
	// THE WINDOW IS NAMED ON THE ROW, because "something is running somewhere"
	// is half an answer.
	if !strings.Contains(row, taskAwayWord) {
		t.Fatalf("the row does not say it belongs to another window:\n%s", row)
	}
	if !strings.Contains(row, "Fix the nil-map crash") {
		t.Fatalf("the row does not say WHICH window:\n%s", row)
	}

	// AND IT LEAVES WHEN THE WINDOW DOES. Nothing announces a window closing —
	// it simply stops speaking, and the reading stops carrying it.
	agent.away, a.away = session.Elsewhere{}, elsewhereCache{}
	if page := taskSheetText(a); strings.Contains(page, "Sweep the call sites") {
		t.Fatalf("a window that closed is still drawn as running:\n%s", page)
	}
}

// A WINDOW WITH NO NAME IS STILL A PLACE. The row says where the work is and
// stops there rather than putting a separator in front of nothing.
func TestAnotherWindowWithNoNameSaysOnlyThatItIsAnotherWindow(t *testing.T) {
	a, agent, _ := awayApp(t)
	agent.away = session.NewElsewhere(time.Now(), nil,
		window("the-other-window", session.PresenceTask{
			ID: "7", Title: "Sweep the call sites", State: string(session.TaskRunning)}))
	if !openTaskPlaceWithRows(a) {
		t.Fatal("/history refused to open over another window's work")
	}
	row := awayRowWith(t, taskSheetText(a), "Sweep the call sites")
	if !strings.Contains(row, taskAwayWord) {
		t.Fatalf("the row does not say where the work is:\n%s", row)
	}
	// THE MISSING NAME IS ABSENT, NOT BLANK — asked of the note itself rather
	// than by hunting for `another window · ` in the drawn row.
	//
	// WHY THIS MOVED. The row's tail used to be cells laid out with spaces
	// between them, so `another window` was the only thing on the row that could
	// put a ` · ` after itself and looking for one was the same question as this
	// one. The tail is now one ranked row of facts joined by [railSep]
	// (tasksplace.go's fitter), and the fact after the note is the AGE — `now`,
	// on every row another window is still holding — so the old spelling read the
	// join between two true facts as a missing third one. The law it was written
	// for is [taskAwayNote]'s and is unchanged: no separator with nothing behind
	// it. So that is what is asked, at the seam that decides it and again on the
	// drawn row, where a blank between two separators is the shape the defect
	// would take.
	note := ""
	for _, item := range a.tasksFiltered().items {
		if item.entry.Title == "Sweep the call sites" {
			note = tasksNote(item)
		}
	}
	if note != taskAwayWord {
		t.Fatalf("an unnamed window's note is %q, want the bare %q", note, taskAwayWord)
	}
	for _, seg := range strings.Split(strings.TrimSpace(row), railSep) {
		if strings.TrimSpace(seg) == "" {
			t.Fatalf("an unnamed window left a separator standing in for its name:\n%s", row)
		}
	}
}

// THE ROSTER'S TREE IS THIS SESSION'S AND NOTHING ELSE'S. Another window's work
// has no parent here and no room behind it; hanging it off this session's forest
// would be a shape claiming a kinship nothing has.
func TestTheRostersTreeIsUnchangedByOtherWindows(t *testing.T) {
	a, agent, _ := awayApp(t)
	railRun(a)
	before := strings.Join(railText(a, 20), "\n")

	agent.away, a.away = session.NewElsewhere(time.Now(), nil,
		window("the-other-window", session.PresenceTask{
			ID: "7", Title: "Sweep the call sites", State: string(session.TaskRunning)})),
		elsewhereCache{}

	after := strings.Join(railText(a, 20), "\n")
	if before != after {
		t.Fatalf("another window's work changed this session's roster:\n--- before\n%s\n--- after\n%s",
			before, after)
	}
	if strings.Contains(after, "Sweep the call sites") {
		t.Fatalf("another window's task is on the roster's own tree:\n%s", after)
	}
}

// ANOTHER WINDOW'S WORK IS PRESSED, AND WHAT IT OPENS IS THE ANSWER.
//
// THIS TEST USED TO ASSERT THE OPPOSITE, and the reversal is the whole of the
// fix. The row took no cursor and `enter` did nothing, on the argument that
// there is no room to open — the node is in another process's graph — and
// nothing landed for a mention to point at. Both halves of that argument are
// still true and neither of them was ever a reason to leave the row inert: a
// person reading a list where nine rows answer and the tenth silently refuses
// cannot tell a refusal from a broken screen, and the one thing they needed —
// WHICH window has this, and that they have to go there — was exactly what
// pressing it could not say. So the row is a stop, and the card behind it says
// it. Nothing here invents a lane into another window: what changed is that the
// refusal is a page rather than a keystroke that does nothing.
func TestAnotherWindowsRowOpensTheCardThatSaysWhereTheWorkIs(t *testing.T) {
	a, agent, _ := awayApp(t)
	agent.away = session.NewElsewhere(time.Now(),
		map[string]string{"the-other-window": "docs pass"},
		window("the-other-window", session.PresenceTask{
			ID: "7", Title: "Sweep the call sites", State: string(session.TaskRunning)}))
	if !openTaskPlaceWithRows(a) {
		t.Fatal("/history refused to open over another window's work")
	}
	item, ok := a.taskSheetCurrent()
	if !ok || !item.away {
		t.Fatalf("the cursor cannot stand on another window's work: %+v %t", item, ok)
	}
	// AND THE FOOT NAMES WHAT IS REALLY BEHIND THE KEY. Not a room — this window
	// has none to open — but the page that says where the work is.
	line := a.taskSheetKeysLine()
	if !strings.Contains(line, tasksEnterAwayWord) {
		t.Fatalf("the foot says %q and never names the door under the cursor", line)
	}
	for _, promised := range []string{tasksEnterRoomWord, tasksEnterInsideWord, tasksVerbsWord} {
		if strings.Contains(line, promised) {
			t.Fatalf("the foot promises %q over another window's work:\n\t%s", promised, line)
		}
	}

	a.taskSheetEnter()
	if !a.taskSheet.detailOn || !a.taskSheet.awayOwner.on {
		t.Fatalf("enter opened no recovery card: detail=%t away=%+v",
			a.taskSheet.detailOn, a.taskSheet.awayOwner)
	}
	if a.roomOpen() {
		t.Fatal("enter opened a room onto a node this window does not hold")
	}
	card := taskSheetText(a)
	// THE TWO SENTENCES, AND THE WINDOW NAMED IN THE FIRST OF THEM.
	for _, want := range []string{
		"Sweep the call sites", "docs pass",
		taskAwayCardWhere("docs pass"), taskAwayCardNoRoom,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card never says %q:\n%s", want, card)
		}
	}
	// AND IT OFFERS NO MENTION, on the foot or on the key. Nothing has landed for
	// an "@" name to resolve against, and a name that resolved against THIS
	// session's task 7 would be the wrong task under the right number.
	if strings.Contains(card, "m puts it in your message") {
		t.Fatalf("the card offers a mention over work that has not landed:\n%s", card)
	}
	a.taskCardKey("m")
	if text := string(a.input.value); strings.Contains(text, "@") {
		t.Fatalf("m wrote a mention for work that has not landed: %s", text)
	}

	// esc BACKS OUT ONE LAYER, and the owner goes with the card rather than
	// leaking onto the next row somebody opens.
	a.taskCardKey("esc")
	if a.taskSheet.detailOn || a.taskSheet.awayOwner.on {
		t.Fatalf("esc left the card up: detail=%t away=%+v",
			a.taskSheet.detailOn, a.taskSheet.awayOwner)
	}
	if !a.at(pageTasks) {
		t.Fatal("esc closed the whole page instead of backing out to the list")
	}
}

// A WINDOW WITH NO NAME STILL GETS THE WHOLE ANSWER. The first sentence stops
// where the name would go rather than trailing off after a colon, which is
// [taskAwayNote]'s law said in prose — and the second is unchanged, because what
// to do about the work does not depend on what the window is called.
func TestTheRecoveryCardOverAnUnnamedWindowStillSaysWhatToDo(t *testing.T) {
	a, agent, _ := awayApp(t)
	agent.away = session.NewElsewhere(time.Now(), nil,
		window("the-other-window", session.PresenceTask{
			ID: "7", Title: "Sweep the call sites", State: string(session.TaskRunning)}))
	if !openTaskPlaceWithRows(a) {
		t.Fatal("/history refused to open over another window's work")
	}
	a.taskSheetEnter()
	card := taskSheetText(a)
	for _, want := range []string{taskAwayCardWhere(""), taskAwayCardNoRoom} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card never says %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, ": .") || strings.Contains(card, "project: ") {
		t.Fatalf("an unnamed window left a colon standing in for its name:\n%s", card)
	}
}

// AND THE POINTER AND THE KEYBOARD OPEN THE SAME THING. A click on the row is
// the row's cursor plus its enter, which is what the page promises for every
// other row it draws — so a person who reaches for the mouse must not find a
// door that only the keyboard has.
func TestClickingAnotherWindowsRowOpensTheSameCardAsEnter(t *testing.T) {
	for _, width := range []int{44, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, agent, _ := awayApp(t)
			a.width = width
			agent.away = session.NewElsewhere(time.Now(),
				map[string]string{"the-other-window": "docs pass"},
				window("the-other-window", session.PresenceTask{
					ID: "7", Title: "Sweep the call sites", State: string(session.TaskRunning)}))
			if !openTaskPlaceWithRows(a) {
				t.Fatal("/history refused to open over another window's work")
			}
			// The keyboard first, so the two answers can be compared rather than
			// asserted apart.
			a.taskSheetEnter()
			typed := taskSheetText(a)
			a.taskCardKey("esc")

			row := awayHitRow(t, a, "Sweep the call sites")
			a.taskSheetPress(2, row)
			if !a.taskSheet.detailOn || !a.taskSheet.awayOwner.on {
				t.Fatalf("a click on the row opened no recovery card at %d columns:\n%s",
					width, taskSheetText(a))
			}
			if clicked := taskSheetText(a); clicked != typed {
				t.Fatalf("the pointer and the keyboard opened different pages at %d columns:\n--- enter\n%s\n--- click\n%s",
					width, typed, clicked)
			}
		})
	}
}

// awayHitRow is the SCREEN ROW one title is drawn on, resolved through the
// page's own hit map — which is what a press will name. Counting drawn lines
// would find the row at [tierPhone] and miss that a card is two lines there.
func awayHitRow(t *testing.T, a *app, title string) int {
	t.Helper()
	width, height := a.size()
	lines, hits, _, _ := a.taskSheetFrame(width, height)
	for y := range lines {
		if y < len(hits) && hits[y].kind == taskSheetHitRow && strings.Contains(plain(lines[y]), title) {
			return y
		}
	}
	t.Fatalf("no pressable row is %q:\n%s", title, strings.Join(drawnRows(lines), "\n"))
	return -1
}

// THE FILTER REACHES THE OTHER WINDOWS TOO. A person typing a word they half
// remember is asking about the DIRECTORY's work, and a section the query cannot
// touch is a section that looks broken when it survives a filter that emptied
// everything else.
func TestTheFilterReachesAnotherWindowsWork(t *testing.T) {
	a, agent, _ := awayApp(t)
	agent.away = session.NewElsewhere(time.Now(), nil,
		window("the-other-window",
			session.PresenceTask{ID: "7", Title: "Sweep the call sites", State: string(session.TaskRunning)},
			session.PresenceTask{ID: "8", Title: "Port the parser", State: string(session.TaskRunning)}))
	if !openTaskPlaceWithRows(a) {
		t.Fatal("/history refused to open over another window's work")
	}
	a.taskSheet.query.setText("parser")
	a.taskSheetTyped()
	page := taskSheetText(a)
	if !strings.Contains(page, "Port the parser") {
		t.Fatalf("the filter lost the row it matched:\n%s", page)
	}
	if strings.Contains(page, "Sweep the call sites") {
		t.Fatalf("the filter did not reach another window's rows:\n%s", page)
	}
}

// THE READING IS TAKEN ON A CLOCK AND NOT ON A FRAME. It is a directory read and
// a file per window; thirty of those a second is a surface that stutters on a
// slow home directory for an answer that changes every few seconds at most.
func TestTheOtherWindowsAreReadOnceAndHeld(t *testing.T) {
	a, agent, tick := awayApp(t)
	agent.away = session.NewElsewhere(time.Now(), nil,
		window("the-other-window", session.PresenceTask{
			ID: "7", Title: "Sweep the call sites", State: string(session.TaskRunning)}))

	a.refreshElsewhere()
	if agent.reads != 1 {
		t.Fatalf("the first ask read %d times, want 1", agent.reads)
	}
	// Every frame inside the window answers out of what is held.
	for i := 0; i < 20; i++ {
		a.refreshElsewhere()
		a.elsewhere()
	}
	if agent.reads != 1 {
		t.Fatalf("the reading was taken %d times inside its own window, want 1", agent.reads)
	}
	// And it is taken again once the window has passed.
	tick(elsewhereEvery + time.Second)
	a.refreshElsewhere()
	if agent.reads != 2 {
		t.Fatalf("the reading was taken %d times after the window passed, want 2", agent.reads)
	}
}

// awayRowWith is the one drawn line a title is on, and it fails loudly when the
// title is on no line at all — a test that searched the whole page would pass on
// a row that says the right words in the wrong place.
func awayRowWith(t *testing.T, page, title string) string {
	t.Helper()
	for _, line := range strings.Split(page, "\n") {
		if strings.Contains(line, title) {
			return line
		}
	}
	t.Fatalf("no row says %q:\n%s", title, page)
	return ""
}
