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

func TestWorkTabDrawsTheTasksPlacesOwnRows(t *testing.T) {
	a, _ := workTabFixture(t)
	openWorkTabNow(t, a)
	text := plain(strings.Join(a.workTabFrame(a.width, a.height), "\n"))
	for _, want := range []string{"Root", "Fix the flake", "$ go test ./internal/tui3", "queued · waits: Root"} {
		if !strings.Contains(text, want) {
			t.Fatalf("work tab missing %q:\n%s", want, text)
		}
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
	if text := plain(strings.Join(a.workTabFrame(a.width, a.height), "\n")); !strings.Contains(text, "you") || !strings.Contains(text, "keep the middleware order") {
		t.Fatalf("work tab lacks note receipt:\n%s", text)
	}
}

func TestWorkTabEscReturnsToConversationAndLandingCardRemains(t *testing.T) {
	a, fake := workTabFixture(t)
	openWorkTabNow(t, a)
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.workTabOn {
		t.Fatal("esc left the work tab open")
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
