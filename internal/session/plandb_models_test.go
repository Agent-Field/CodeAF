package session

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

func TestPlanTaskPageKeepsAssignedCrewWithoutAPricedCall(t *testing.T) {
	dir, chat := t.TempDir(), "model-page"
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, chat, plandb.TaskSpec{ID: "child", Title: "Child"})
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, a, path, chat)
	decision := crewroute.Decision{Crew: []crewroute.Pick{
		{Seat: crewroute.Worker, Model: "saved/worker"},
		{Seat: crewroute.Planner, Model: "saved/planner"},
		{Seat: crewroute.Checker, Model: "saved/checker"},
	}}
	// The screenshot's shape: recovery refused an unpriced interrupted call,
	// but the durable row still carries every accepted seat. No ledger rows.
	a.graph().keepRunRows(7, []TaskNotice{{ID: 7, PlanTask: planStoreID(planRootID), State: TaskInterrupted,
		CrewState: &TaskCrewRecord{Version: 1, Routed: true, Decision: decision, InFlight: 1}}})
	want := []PlanTaskModel{{"worker", "saved/worker"}, {"planner", "saved/planner"}, {"checker", "saved/checker"}}
	for _, id := range []string{planRootID, "child"} {
		page, ok := a.PlanTaskPage(id)
		if !ok || !reflect.DeepEqual(page.Models, want) {
			t.Fatalf("%s lost accepted crew: %+v", id, page.Models)
		}
		if page.Row.Model != "" || page.Row.Tokens != 0 || page.Row.USD != 0 {
			t.Fatalf("assignment invented usage: %+v", page.Row)
		}
		page.Models[0].Model = "changed by reader"
	}
	// A different run and the current profile cannot supply missing history.
	a.graph().keepRunRows(7, []TaskNotice{{ID: 7, PlanTask: "t-elsewhere", Model: "other/worker", Crew: &decision}})
	page, _ := a.PlanTaskPage("child")
	if len(page.Models) != 0 {
		t.Fatalf("another run supplied models: %+v", page.Models)
	}
}

func TestPlanTaskModelsUsesThePublishedCrewAndUnroutedCheckpoint(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	g := a.graph()
	saved := &TaskCrewRecord{Version: 1, Work: "saved/work", Plan: "saved/plan", Check: "saved/check"}
	g.keepRunRows(7, []TaskNotice{{ID: 7, PlanTask: "t-7", CrewState: saved}})
	if got := a.planTaskModels("7"); !reflect.DeepEqual(got, []PlanTaskModel{{"worker", "saved/work"}, {"planner", "saved/plan"}, {"checker", "saved/check"}}) {
		t.Fatalf("unrouted seats: %+v", got)
	}
	decision := &crewroute.Decision{Crew: []crewroute.Pick{{Seat: crewroute.Worker, Model: "current/work"}}}
	g.keepRunRows(7, []TaskNotice{{ID: 7, PlanTask: "t-7", Crew: decision, Model: "old/work", CrewState: saved}})
	if got := a.planTaskModels("7"); len(got) != 3 || got[0].Model != "current/work" {
		t.Fatalf("published worker lost to old history: %+v", got)
	}
}

func TestArchivedTaskModelsSurviveReopeningTheConversation(t *testing.T) {
	dir, chat := t.TempDir(), "archived-models"
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path+".1", chat, plandb.TaskSpec{ID: "child", Title: "Archived child"})
	open := func() *Agent {
		a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
			c.SessionFile = filepath.Join(dir, "transcript.jsonl")
		})
		armPlanStore(t, a, path, chat)
		return a
	}
	first := open()
	first.graph().mu.Lock()
	first.graph().seq = 7
	first.graph().mu.Unlock()
	first.graph().keepRunRows(7, []TaskNotice{{ID: 7, PlanTask: planStoreID(planRootID), State: TaskInterrupted,
		CrewState: &TaskCrewRecord{Version: 1, Work: "accepted/worker", Plan: "accepted/planner", Check: "accepted/checker", InFlight: 1}}})
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := open()
	page, ok := second.PlanTaskPage("child")
	if !ok || !reflect.DeepEqual(page.Models, []PlanTaskModel{{"worker", "accepted/worker"}, {"planner", "accepted/planner"}, {"checker", "accepted/checker"}}) {
		t.Fatalf("reopening lost the archived assignment: %+v (found %v)", page.Models, ok)
	}
	if page.Row.Model != "" {
		t.Fatalf("a saved assignment became usage: %+v", page.Row)
	}
}
