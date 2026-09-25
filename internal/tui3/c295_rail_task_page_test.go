package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// railPlanCounter proves that opening the store is a gesture, never paint.
type railPlanCounter struct {
	*planFake
	rows  int
	pages int
}

func (f *railPlanCounter) PlanTasks() []session.PlanTaskRow {
	f.rows++
	return f.planFake.PlanTasks()
}

func (f *railPlanCounter) PlanTaskPage(id string) (session.PlanTaskPage, bool) {
	f.pages++
	return f.planFake.PlanTaskPage(id)
}

func (f *railPlanCounter) SteerTask(uint64, string) (session.SteerReceipt, error) {
	return session.SteerReceipt{}, nil
}

func (f *railPlanCounter) WatchTask(uint64) (<-chan session.Event, error) {
	return make(chan session.Event), nil
}

func (f *railPlanCounter) TaskJournal(uint64) string { return "" }

func railTaskPageApp(t *testing.T, held bool) (*app, *railPlanCounter) {
	t.Helper()
	row := session.PlanTaskRow{ID: "2", Title: "land the parser", Status: "running"}
	pages := map[string]session.PlanTaskPage{}
	if held {
		child := session.PlanTaskRow{ID: "3", Parent: "2", Title: "cover the parser", Status: "done"}
		pages["2"] = session.PlanTaskPage{Row: row, Description: "replace the parser", Children: []session.PlanTaskRow{child}}
		pages["3"] = session.PlanTaskPage{Row: child, Description: "the child page"}
	}
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, pages)
	counted := &railPlanCounter{planFake: fake}
	a.agent = counted
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(2, row.Title, session.TaskRunning, session.TaskNotice{})})
	return a, counted
}

func TestRailClickReadsTheStoreAtTheGestureAndOpensTheTaskPage(t *testing.T) {
	a, fake := railTaskPageApp(t, true)
	if fake.pages != 0 {
		t.Fatalf("building and drawing the conversation read %d task pages", fake.pages)
	}
	clickRail(t, a, 0)
	if fake.pages != 1 {
		t.Fatalf("rail click read task pages %d times, want once at the gesture", fake.pages)
	}
	if !a.taskSheet.planOn || a.taskSheet.plan.Row.ID != "2" || a.roomOpen() {
		t.Fatalf("rail click opened plan=%v id=%q room=%v", a.taskSheet.planOn, a.taskSheet.plan.Row.ID, a.roomOpen())
	}
	if got := taskSheetText(a); !strings.Contains(got, "replace the parser") {
		t.Fatalf("the rail did not open taskPlanBody:\n%s", got)
	}
}

func TestRailEnterReadsTheStoreAtTheGestureAndOpensTheTaskPage(t *testing.T) {
	a, fake := railTaskPageApp(t, true)
	a.railWhere, a.railHold = railSpot{id: 2}, true
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if fake.pages != 1 || !a.taskSheet.planOn || a.taskSheet.plan.Row.ID != "2" || a.roomOpen() {
		t.Fatalf("rail enter reads=%d plan=%v id=%q room=%v", fake.pages, a.taskSheet.planOn, a.taskSheet.plan.Row.ID, a.roomOpen())
	}
}

func TestRailRowWithoutAStoredPageStillOpensItsRoom(t *testing.T) {
	a, fake := railTaskPageApp(t, false)
	clickRail(t, a, 0)
	if fake.pages != 1 {
		t.Fatalf("room fallback did %d page reads, want one gesture read", fake.pages)
	}
	if a.taskSheet.planOn || !a.roomOpen() || a.room.id != 2 {
		t.Fatalf("absent page opened plan=%v room=%v id=%d", a.taskSheet.planOn, a.roomOpen(), roomID(a))
	}
}

func TestRailTaskPageSurvivesSettledFramesAndFramesDoNotReadTheAgent(t *testing.T) {
	a, fake := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	for i := range fake.plan {
		fake.plan[i].Status = "done"
	}
	for i := range a.taskSheet.mine.plan {
		a.taskSheet.mine.plan[i].Status = "done"
	}
	settled := fake.planFake.pages["2"]
	settled.Row.Status = "done"
	fake.planFake.pages["2"] = settled
	reads, pages := fake.rows, fake.pages
	for range 10 {
		drive(t, a, frameMsg{})
		frame, _, _ := a.frame()
		if !strings.Contains(plain(frame), "replace the parser") {
			t.Fatalf("a settled frame closed the task page:\n%s", plain(frame))
		}
	}
	// A PAGE ON A RUNNING TASK FOLLOWS IT, one read at a time on the paint clock,
	// and the read that finds the task settled is the last: ten frames on a
	// settled page cost that one read and no more, and no frame reads the rows.
	if fake.rows != reads || fake.pages > pages+1 {
		t.Fatalf("a settled page kept calling the agent: row reads %d→%d, page reads %d→%d", reads, fake.rows, pages, fake.pages)
	}
	if !a.taskSheet.planOn {
		t.Fatal("ten settled frames closed the stored task page")
	}
}

func TestEscFromARailTaskPageReturnsExactlyToTheConversation(t *testing.T) {
	a, _ := railTaskPageApp(t, true)
	a.input.value = []rune("draft stays here")
	clickRail(t, a, 0)
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.taskSheet.planOn || a.at(pageTasks) || a.roomOpen() {
		t.Fatalf("esc left plan=%v tasks=%v room=%v", a.taskSheet.planOn, a.at(pageTasks), a.roomOpen())
	}
	if got := string(a.input.value); got != "draft stays here" {
		t.Fatalf("esc returned with draft %q", got)
	}
}

func TestAChildRemainsOpenableFromARailTaskPage(t *testing.T) {
	a, fake := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	a.taskSheet.planAt = 0
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if fake.pages != 2 || a.taskSheet.plan.Row.ID != "3" || len(a.taskSheet.planBack) != 1 {
		t.Fatalf("child open reads=%d id=%q back=%d", fake.pages, a.taskSheet.plan.Row.ID, len(a.taskSheet.planBack))
	}
	if got := taskSheetText(a); !strings.Contains(got, "the child page") {
		t.Fatalf("child taskPlanBody was not drawn:\n%s", got)
	}
}

func TestRoomGoneWordIsNeverDrawnForATaskTheStoreHolds(t *testing.T) {
	a, _ := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	frame, _, _ := a.frame()
	if strings.Contains(plain(frame), roomGoneWord) {
		t.Fatalf("a held task drew the missing-room sentence:\n%s", plain(frame))
	}
}

// A RUN IS DRAWN AS ITS OWN ROW WITH ITS PARTS UNDER IT, and every one of those
// rows opens its own page. On the real screen a run was drawn as its parts with
// nothing over them: the reading leaves the run's own plan row to the node that
// carries it, and the rail dropped that node row because the store held its
// title. And a press on the rail was answered out of the view from BEFORE the
// run's rows were put in, so a part's row opened nothing.
func TestARunIsItsOwnRowWithItsPartsUnderItAndEachOpensItsPage(t *testing.T) {
	root := session.PlanTaskRow{ID: "2", Title: "land the parser", Status: "running"}
	first := session.PlanTaskRow{ID: "p1", Parent: "2", Title: "write the parser", Status: "running"}
	second := session.PlanTaskRow{ID: "p2", Parent: "2", Title: "cover the parser", Status: "pending"}
	pages := map[string]session.PlanTaskPage{
		"2":  {Row: root, Description: "replace the parser"},
		"p1": {Row: first, Description: "the first part's own page"},
		"p2": {Row: second, Description: "the second part's own page"},
	}
	a, _ := planAppWith(t, []session.PlanTaskRow{root, first, second}, pages)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(2, root.Title, session.TaskRunning, session.TaskNotice{})})
	readPlanRows(t, a)

	view, _ := a.railDrawnView(a.viewHeight())
	rootAt, firstAt, secondAt := -1, -1, -1
	for i, line := range view {
		text := plain(line.text)
		switch {
		// The second part's row names the run it waits on, so the run's own
		// row is the first that carries its title.
		case strings.Contains(text, "land the parser") && rootAt < 0:
			rootAt = i
		case strings.Contains(text, "write the parser"):
			firstAt = i
		case strings.Contains(text, "cover the"):
			secondAt = i
		}
	}
	if rootAt < 0 || firstAt < rootAt || secondAt < firstAt {
		t.Fatalf("the run's row is at %d and its parts at %d and %d, want the run first and its parts under it", rootAt, firstAt, secondAt)
	}
	if view[secondAt].plan != "p2" {
		t.Fatalf("the second part's line carries task %q, want its own", view[secondAt].plan)
	}
	cmd, took := a.railPress(a.railLeft()+6, a.bodyTop()+secondAt)
	if !took {
		t.Fatal("a press on a part's row was not the rail's")
	}
	drain(t, a, cmd)
	if !a.taskSheet.planOn || a.taskSheet.plan.Row.ID != "p2" || !a.railTaskPlanOn {
		t.Fatalf("the press opened plan=%v task=%q over the chat=%v, want the second part's page", a.taskSheet.planOn, a.taskSheet.plan.Row.ID, a.railTaskPlanOn)
	}
	if got := taskSheetText(a); !strings.Contains(got, "the second part's own page") {
		t.Fatalf("the page drawn is not the part's:\n%s", got)
	}
}

// A LETTER TYPED INTO THE PAGE'S NOTE IS A LETTER. The page opened from the rail
// sits over a conversation whose own box is empty, and the key that raises the
// stop card is read above every page; a note holding that letter raised the card
// mid-word, and the card then swallowed the rest of the sentence.
func TestANoteTypedOnARailTaskPageNeverRaisesTheStopCard(t *testing.T) {
	a, _ := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	if !a.railTaskPlanOn {
		t.Fatal("the rail row did not open its page")
	}
	for _, r := range "an example" {
		drive(t, a, key(string(r)))
	}
	if a.stopping() {
		t.Fatal("a letter in a note raised the stop card")
	}
	if got := a.taskSheet.planNote.String(); got != "an example" {
		t.Fatalf("the note box holds %q, want %q", got, "an example")
	}
}

// heldRailPlan holds the page's read open until the test lets it go, which is
// the gap a person types into on a hosted conversation.
type heldRailPlan struct {
	*railPlanCounter
	started chan struct{}
	release chan struct{}
}

func (h *heldRailPlan) PlanTaskPage(id string) (session.PlanTaskPage, bool) {
	close(h.started)
	<-h.release
	return h.railPlanCounter.PlanTaskPage(id)
}

// THE GAP BELONGS TO THE PAGE FOR EVERY KEY, the stop card's included. That key
// is read above every page, so holding the gap's keys at the page's own rung
// was not enough: with a task running, a note holding that letter raised the
// card while the page was still on its way, and the card took the rest.
func TestALetterTypedWhileARailPageOpensNeverRaisesTheStopCard(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	held := &heldRailPlan{railPlanCounter: counted, started: make(chan struct{}), release: make(chan struct{})}
	a.agent = held
	cmd := a.openRailPlan("2", nil)
	answer := make(chan tea.Msg, 1)
	go func() { answer <- cmd() }()
	<-held.started
	for _, r := range "an example" {
		drive(t, a, key(string(r)))
	}
	if a.stopping() {
		t.Fatal("a letter typed while the page was opening raised the stop card")
	}
	close(held.release)
	drive(t, a, <-answer)
	if !a.railTaskPlanOn {
		t.Fatal("the answer did not open the page")
	}
	if got := a.taskSheet.planNote.String(); got != "an example" {
		t.Fatalf("the page's box holds %q, want every key typed while it opened: %q", got, "an example")
	}
}
