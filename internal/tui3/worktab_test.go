package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

func workTabFixture(t *testing.T) (*app, *planFake) {
	t.Helper()
	root := session.PlanTaskRow{ID: "t-root", Title: "Root", Status: "running", Steps: 3}
	root.Live.Step, root.Live.Command = 4, "go test ./internal/tui3"
	child := session.PlanTaskRow{ID: "t-child", Title: "Fix the flake", Status: "pending", Parent: "t-root", Waits: []string{"t-root"}}
	a, fake := planAppWith(t, []session.PlanTaskRow{root, child}, map[string]session.PlanTaskPage{
		"t-root": {Row: root},
	})
	a.width, a.height = 120, 28
	// The strip reads the rows the task sheet carries, never the store; the
	// fixture hands it the fake's own slice so a test that moves a state moves
	// what the strip sees.
	a.taskSheet.mine.plan = fake.plan
	return a, fake
}

func TestWorkTabAppearsAfterConversationOnlyForALiveRun(t *testing.T) {
	a, fake := workTabFixture(t)
	tabs := a.tabList()
	if len(tabs) != 2 || !tabs[0].here || !tabs[1].work || tabs[1].word != "Root" {
		t.Fatalf("live run tabs = %+v, want conversation then titled work tab", tabs)
	}
	fake.plan[0].Status, fake.plan[1].Status = "done", "done"
	a.workTabStable()
	a.workTabStable()
	if tabs := a.tabList(); len(tabs) != 1 || tabs[0].work {
		t.Fatalf("landed stable run kept its tab: %+v", tabs)
	}
	noRun, _ := planAppWith(t, nil, nil)
	if tabs := noRun.tabList(); len(tabs) != 1 || tabs[0].work {
		t.Fatalf("conversation without a run gained a work tab: %+v", tabs)
	}
}

// THE RUN'S TAB OPENS THE RUN'S TASK ROOM, the one page every task opens, with
// the run's parts under it.
func TestWorkTabOpensTheRunsTaskRoom(t *testing.T) {
	a, _ := workTabFixture(t)
	openWorkTabNow(t, a)
	if plan := a.roomPlan(); plan == nil || plan.id != "t-root" {
		t.Fatalf("the run's tab did not open the run's room: room %v", a.roomOpen())
	}
	text := planRoomText(t, a)
	for _, want := range []string{"Root", "Fix the flake"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the run's room is missing %q:\n%s", want, text)
		}
	}
	if tabs := a.tabList(); len(tabs) != 2 || !tabs[1].here {
		t.Fatalf("the run's tab is not the one here while its room is open: %+v", tabs)
	}
}

func TestWorkTabNoteUsesPlanNoteAndShowsThePageReceipt(t *testing.T) {
	a, fake := workTabFixture(t)
	openWorkTabNow(t, a)
	for _, r := range "keep the middleware order" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(fake.noted) != 1 || fake.noted[0] != (planCall{id: "t-root", text: "keep the middleware order"}) {
		t.Fatalf("work tab note calls = %+v", fake.noted)
	}
	if text := planRoomText(t, a); !strings.Contains(text, "keep the middleware order") {
		t.Fatalf("the run's room lacks the note:\n%s", text)
	}
}

// THE ROOM NAMES NO AUTHOR BY A STORE ID. A note another author left was drawn
// under the store's own id for it (`2ytmh2 · …`): an author is the person or
// nothing (#1240).
func TestTheRunsRoomDrawsNoAuthorAsAStoreID(t *testing.T) {
	a, fake := workTabFixture(t)
	page := fake.pages["t-root"]
	page.Notes = []session.PlanTaskNote{
		{Author: "2ytmh2", Body: "the worker's own note"},
		{Person: true, Body: "keep the middleware order"},
	}
	fake.pages["t-root"] = page
	a.height = 40
	openWorkTabNow(t, a)
	text := planRoomText(t, a)
	if strings.Contains(text, "2ytmh2") {
		t.Fatalf("the room drew a store id as a note's author:\n%s", text)
	}
	if !strings.Contains(text, "the worker's own note") || !strings.Contains(text, "keep the middleware order") {
		t.Fatalf("the room lost a note:\n%s", text)
	}
}

func TestWorkTabEscReturnsToConversationAndLandingCardRemains(t *testing.T) {
	a, fake := workTabFixture(t)
	openWorkTabNow(t, a)
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.roomOpen() {
		t.Fatal("esc left the run's room open")
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Root", session.TaskDone, session.TaskNotice{Report: "the importer landed"})})
	fake.plan[0].Status, fake.plan[1].Status = "done", "done"
	a.workTabStable()
	a.workTabStable()
	if got := plain(func() string { s, _, _ := a.frame(); return s }()); !strings.Contains(got, "Root") || !strings.Contains(got, "the importer landed") {
		t.Fatalf("self-closing work tab removed the landing card:\n%s", got)
	}
}

func TestWorkTabKeepsTheStripSwitchKey(t *testing.T) {
	a, _ := workTabFixture(t)
	keepThree(t, a)
	openWorkTabNow(t, a)
	drive(t, a, key(hopOpenKey))
	if !a.hopShowing() {
		t.Fatal("the strip switch key did not open its conversation card from the work tab")
	}
}

// countingPlan counts the store reads a surface asks its agent for.
type countingPlan struct {
	*planFake
	reads int
}

func (c *countingPlan) PlanTasks() []session.PlanTaskRow {
	c.reads++
	return c.planFake.PlanTasks()
}

// THE TAB STRIP IS FRAME CODE AND NEVER OPENS THE STORE. It asked the agent for
// the run's rows twice on every frame, and behind that door is a database file;
// it reads the rows the task sheet already carries.
func TestTheWorkTabNeverAsksTheStoreFromAFrame(t *testing.T) {
	a, fake := workTabFixture(t)
	counted := &countingPlan{planFake: fake}
	a.agent = counted
	for i := 0; i < 3; i++ {
		if tabs := a.tabList(); len(tabs) != 2 || !tabs[1].work {
			t.Fatalf("the work tab is missing: %+v", tabs)
		}
		a.tabsRow(a.width)
	}
	if counted.reads != 0 {
		t.Fatalf("drawing the tab strip read the store %d times", counted.reads)
	}
}

func TestARunTabDoesNotDuplicateOrRenameItsConversationLists(t *testing.T) {
	a, _ := workTabFixture(t)
	if tabs := a.tabList(); len(tabs) != 2 || !tabs[1].work {
		t.Fatal("the fixture must include the run tab beside its conversation")
	}
	home := homeView{tabs: a.tabList}
	open, _ := home.conversationRows()
	if len(open) != 1 || open[0].title != a.title || open[0].session.Transcript != a.file {
		t.Fatalf("Home duplicated or renamed the conversation: %+v", open)
	}
	chats := a.openTabRows(a.now())
	if len(chats) != 1 || chats[0].title != a.title || chats[0].file != a.file {
		t.Fatalf("the chats menu duplicated or renamed the conversation: %+v", chats)
	}
	view := a.taskSheet.filtered(a).chatViews[a.file]
	if view.title != a.title {
		t.Fatalf("Sessions used the run title %q instead of %q", view.title, a.title)
	}
}
