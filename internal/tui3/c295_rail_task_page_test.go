package tui3

import (
	"strings"
	"sync"
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
	if plan := a.roomPlan(); plan == nil || plan.id != "2" {
		t.Fatalf("rail click did not open the task room over store task 2: room=%v", a.roomOpen())
	}
	if got := planRoomText(t, a); !strings.Contains(got, "replace the parser") {
		t.Fatalf("the rail did not open the task's room:\n%s", got)
	}
}

func TestRailEnterReadsTheStoreAtTheGestureAndOpensTheTaskPage(t *testing.T) {
	a, fake := railTaskPageApp(t, true)
	a.railWhere, a.railHold = railSpot{id: 2}, true
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if plan := a.roomPlan(); fake.pages != 1 || plan == nil || plan.id != "2" {
		t.Fatalf("rail enter reads=%d room=%v", fake.pages, a.roomOpen())
	}
}

func TestRailRowWithoutAStoredPageStillOpensItsRoom(t *testing.T) {
	a, fake := railTaskPageApp(t, false)
	clickRail(t, a, 0)
	if fake.pages != 1 {
		t.Fatalf("room fallback did %d page reads, want one gesture read", fake.pages)
	}
	if a.roomPlan() != nil || !a.roomOpen() || a.room.id != 2 {
		t.Fatalf("absent page opened room=%v id=%d", a.roomOpen(), roomID(a))
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
			t.Fatalf("a settled frame closed the task room:\n%s", plain(frame))
		}
	}
	// A ROOM ON A RUNNING TASK FOLLOWS IT ON ITS OWN BEAT, never on the paint
	// clock: ten frames cost no read of the page and no read of the rows.
	if fake.rows != reads || fake.pages != pages {
		t.Fatalf("frames called the agent: row reads %d→%d, page reads %d→%d", reads, fake.rows, pages, fake.pages)
	}
	if a.roomPlan() == nil {
		t.Fatal("ten settled frames closed the task room")
	}
}

func TestEscFromARailTaskPageReturnsExactlyToTheConversation(t *testing.T) {
	a, _ := railTaskPageApp(t, true)
	a.input.value = []rune("draft stays here")
	clickRail(t, a, 0)
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.at(pageTasks) || a.roomOpen() {
		t.Fatalf("esc left tasks=%v room=%v", a.at(pageTasks), a.roomOpen())
	}
	if got := string(a.input.value); got != "draft stays here" {
		t.Fatalf("esc returned with draft %q", got)
	}
}

func TestAChildRemainsOpenableFromARailTaskRoom(t *testing.T) {
	a, fake := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	if got := planRoomText(t, a); !strings.Contains(got, "cover the parser") {
		t.Fatalf("the run's room does not draw its part:\n%s", got)
	}
	openPlanRoomNow(t, a, "3")
	if plan := a.roomPlan(); fake.pages != 2 || plan == nil || plan.id != "3" {
		t.Fatalf("child open reads=%d room=%v", fake.pages, a.roomOpen())
	}
	if got := planRoomText(t, a); !strings.Contains(got, "the child page") {
		t.Fatalf("the child's room was not drawn:\n%s", got)
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
		case strings.Contains(text, "land the parser"):
			rootAt = i
		case strings.Contains(text, "write the parser"):
			firstAt = i
		case strings.Contains(text, "cover the parser"):
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
	if plan := a.roomPlan(); plan == nil || plan.id != "p2" {
		t.Fatalf("the press opened room=%v, want the second part's room", a.roomOpen())
	}
	if got := planRoomText(t, a); !strings.Contains(got, "the second part's own page") {
		t.Fatalf("the room drawn is not the part's:\n%s", got)
	}
}

// A LETTER TYPED INTO THE PAGE'S NOTE IS A LETTER. The page opened from the rail
// sits over a conversation whose own box is empty, and the key that raises the
// stop card is read above every page; a note holding that letter raised the card
// mid-word, and the card then swallowed the rest of the sentence.
func TestANoteTypedOnARailTaskPageNeverRaisesTheStopCard(t *testing.T) {
	a, _ := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	if a.roomPlan() == nil {
		t.Fatal("the rail row did not open its room")
	}
	for _, r := range "an example" {
		drive(t, a, key(string(r)))
	}
	if a.stopping() {
		t.Fatal("a letter in a note raised the stop card")
	}
	if got := string(a.input.value); got != "an example" {
		t.Fatalf("the room's box holds %q, want %q", got, "an example")
	}
}

// heldRailPlan holds the page's read open until the test lets it go, which is
// the gap a person types into on a hosted conversation.
//
// ONLY THE FIRST READ IS HELD. A later read — the page following its task, or
// the receipt a note's send reads back — answers at once, rather than closing
// the start signal a second time.
type heldRailPlan struct {
	*railPlanCounter
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *heldRailPlan) PlanTaskPage(id string) (session.PlanTaskPage, bool) {
	h.once.Do(func() {
		close(h.started)
		<-h.release
	})
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
	if a.roomPlan() == nil {
		t.Fatal("the answer did not open the room")
	}
	if got := string(a.input.value); got != "an example" {
		t.Fatalf("the room's box holds %q, want every key typed while it opened: %q", got, "an example")
	}
}
