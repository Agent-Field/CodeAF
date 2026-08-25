package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE TASKS PLACE'S FOOT, AND THE ONE VERB BEHIND IT ──────────────────────
//
// SCREEN 1e draws one line under this place:
//
//	enter open its room · → verbs: run it again, stop it · type to filter · tab next place
//
// These pin it word for word, and they pin the harder half of the same law:
// NOTHING IS NAMED THAT IS NOT BOUND. One of the design's two verbs has a seam
// and one does not, so one is on the strip and in the sentence and the other is
// on neither.

// tasksFootApp is the tasks place over a session that can END work: one node
// running, and the door [stopAgent] asserts behind it.
func tasksFootApp(t *testing.T) (*app, *stopFake) {
	t.Helper()
	a, agent := stopApp(t)
	if !a.openTaskSheet() {
		t.Fatal("the place refused to open over a running node")
	}
	return a, agent
}

// THE FOOT IS SCREEN 1e'S, WORD FOR WORD, over the row the design draws it over:
// a task this window is running. The router adds the two keys true on every
// place; everything before them is this place's own sentence.
func TestTheTasksFootIsScreenOneEWordForWord(t *testing.T) {
	a, _ := tasksFootApp(t)

	item, ok := a.taskSheetCurrent()
	if !ok || item.entry.Label != "Fix the nil-map crash" {
		t.Fatalf("the cursor is not on this window's running task: %+v", item.entry)
	}
	const want = "enter open its room · → verbs: stop it · type to filter"
	if got := a.taskSheetKeysLine(); got != want {
		t.Fatalf("the foot reads\n  %q\nwant\n  %q", got, want)
	}
	// AND IT IS THE LAST LINE OF THE FRAME, with the router's own two keys on the
	// end of it and no second foot under it.
	lines := strings.Split(plain(taskSheetText(a)), "\n")
	last := ""
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			last = strings.TrimSpace(line)
		}
	}
	if last != placeTailed(want) {
		t.Fatalf("the frame ends on %q, want %q", last, placeTailed(want))
	}
}

// EVERY CLAUSE IS TRUE OF THE ROW UNDER THE CURSOR OR IT IS NOT DRAWN.
//
// The design's line is one static sentence because its cursor is on one row.
// Walk the cursor and the sentence has to change with it, or the page is
// promising a door the row has not got.
func TestTheTasksFootSaysOnlyWhatIsTrueOfTheRowUnderIt(t *testing.T) {
	a, _ := tasksFootApp(t)
	// A row of work another conversation ran: no room, and nothing to stop.
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	if !a.openTaskSheet() {
		t.Fatal("the place refused to reopen")
	}
	for i := 0; i < 20; i++ {
		a.taskSheetMove(1)
	}
	item, ok := a.taskSheetCurrent()
	if !ok || item.entry.Title != "Port the parser" {
		t.Fatalf("the walk did not reach the earlier conversation's row: %+v", item)
	}
	const want = "enter go inside it · type to filter"
	if got := a.taskSheetKeysLine(); got != want {
		t.Fatalf("over work another conversation ran the foot reads\n  %q\nwant\n  %q", got, want)
	}

	// AND WHILE A FILTER IS ON, THE CLAUSE THAT MOVED IS THE ONE THAT IS SAID.
	// `type to filter` would be teaching somebody to do what they are doing; what
	// they cannot see is that esc now means the filter and not the page.
	a.taskSheet.query.setText("port")
	a.taskSheetTyped()
	if got := a.taskSheetKeysLine(); !strings.Contains(got, tasksClearFilterWord) {
		t.Fatalf("a filtered foot reads %q and never says what esc does now", got)
	}
	if got := a.taskSheetKeysLine(); strings.Contains(got, tasksTypeWord) {
		t.Fatalf("a filtered foot still offers %q: %q", tasksTypeWord, got)
	}
}

// NOTHING IS NAMED THAT IS NOT BOUND, AND NOTHING BOUND GOES UNNAMED. The foot's
// verb clause and the `→` strip are two readings of one list, so a person who
// reads a verb can press it and a person who opens the strip meets exactly what
// the foot promised.
func TestTheTasksVerbsOnTheStripAreTheVerbsInTheFoot(t *testing.T) {
	a, _ := tasksFootApp(t)

	verbs := a.taskSheet.verbs(a)
	if len(verbs) != 1 || verbs[0].word != stopActWord || verbs[0].key != 's' {
		t.Fatalf("the running row offers %+v, want one `s %s`", verbs, stopActWord)
	}
	if !strings.Contains(a.taskSheetKeysLine(), tasksVerbsWord+stopActWord) {
		t.Fatalf("the foot does not name the verb the row has: %q", a.taskSheetKeysLine())
	}

	// `→` DRAWS THEM, and the letter is a verb only while the word is on screen.
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyRight})
	if !a.strip.open {
		t.Fatal("→ on a row with a verb opened no strip")
	}
	strip := plain(strings.Join(a.verbStripRow(a.width), "\n"))
	if !strings.Contains(strip, "s "+stopActWord) {
		t.Fatalf("the strip does not draw the verb: %q", strip)
	}
}

// SCREEN 1e'S OTHER VERB HAS NO SEAM, SO IT IS ABSENT RATHER THAN DRAWN DEAD.
// Nothing on this machine re-runs a finished task — a record row is an account of
// work that happened — and a capability that cannot work is absent, not broken.
func TestTheTasksPlaceNeverNamesRunItAgain(t *testing.T) {
	a, _ := tasksFootApp(t)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	if !a.openTaskSheet() {
		t.Fatal("the place refused to reopen")
	}
	for step := 0; step < 20; step++ {
		if line := a.taskSheetKeysLine(); strings.Contains(line, "run it again") {
			t.Fatalf("the foot names a verb nothing is bound to: %q", line)
		}
		for _, v := range a.taskSheet.verbs(a) {
			if v.word == "run it again" {
				t.Fatal("the strip carries a verb with no seam behind it")
			}
		}
		a.taskSheetMove(1)
	}
	if strings.Contains(plain(taskSheetText(a)), "run it again") {
		t.Fatalf("`run it again` is drawn somewhere on the place:\n%s", plain(taskSheetText(a)))
	}
}

// A ROW WITH NOTHING TO STOP IS OFFERED NOTHING. Work another conversation ran
// has no node in this session's graph for a cancel id to name, and work that has
// settled has nothing left to end.
func TestTheTasksStripOffersNoVerbOverWorkItCannotStop(t *testing.T) {
	a, _ := tasksFootApp(t)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("9", "port-the-parser", "Port the parser", time.Hour),
	}
	if !a.openTaskSheet() {
		t.Fatal("the place refused to reopen")
	}
	for i := 0; i < 20; i++ {
		a.taskSheetMove(1)
	}
	if verbs := a.taskSheet.verbs(a); len(verbs) != 0 {
		t.Fatalf("work another conversation ran was offered %+v", verbs)
	}
	if a.openStrip() {
		t.Fatal("→ opened a strip over a row with no verbs")
	}
}

// AND A SESSION WITH NO DOOR ONTO STOPPING NAMES NO VERB AT ALL. The build guard
// is asked before the word is drawn, because a named key that could only ever
// answer with "stopping work is unavailable" is the place advertising something
// it has not got.
func TestTheTasksFootNamesNoVerbWithoutTheEnginesDoor(t *testing.T) {
	a, _, _ := taskApp(t)
	railRun(a)
	if !a.openTaskSheet() {
		t.Fatal("the place refused to open")
	}
	if verbs := a.taskSheet.verbs(a); len(verbs) != 0 {
		t.Fatalf("a session with no cancel door offered %+v", verbs)
	}
	const want = "enter open its room · type to filter"
	if got := a.taskSheetKeysLine(); got != want {
		t.Fatalf("the foot reads\n  %q\nwant\n  %q", got, want)
	}
}

// `s` ON THE STRIP ENDS THE WORK, through the door the card uses and with the
// engine's own sentence kept.
func TestSOnTheTasksStripStopsThatTaskThroughTheEnginesDoor(t *testing.T) {
	a, agent := tasksFootApp(t)
	agent.line = "stopping task 7 — its branch is kept"

	drive(t, a, tea.KeyPressMsg{Code: tea.KeyRight})
	drive(t, a, key("s"))

	if len(agent.asked) != 1 || agent.asked[0] != session.CancelTask+":7" {
		t.Fatalf("the engine was asked %v, want one %q", agent.asked, session.CancelTask+":7")
	}
	// The strip goes away with the row it was about, and the engine's line is
	// where the person is looking.
	if a.strip.open {
		t.Fatal("the verb left its strip standing")
	}
	if text := taskText(a); !strings.Contains(text, "stopping task 7") {
		t.Fatalf("the engine's own sentence is nowhere:\n%s", text)
	}
	// AND NO CONFIRMATION CARD WAS RAISED, because stop.go refuses to raise one
	// over a frame that is not drawing it — the strip is the deliberate gesture
	// here, and `→` then `s` is two presses with the verb on screen for the
	// second of them.
	if a.stopping() {
		t.Fatal("the strip raised a card this place cannot draw")
	}
}
