package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func roomSetupText(a *app) string {
	var words []string
	for _, line := range a.roomControlRows(a.railRoom()) {
		words = append(words, plain(line.text))
	}
	return strings.Join(words, "\n")
}

func TestRunRoomShowsAssignedSeatsWithoutUsageAndExplainsEditing(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-7", Title: "Interrupted work", Status: "failed", Interrupted: true}
	page := session.PlanTaskPage{Row: row, Models: []session.PlanTaskModel{
		{Seat: "worker", Model: "saved/worker-model"}, {Seat: "planner", Model: "saved/planner-model"}, {Seat: "checker", Model: "saved/checker-model"},
	}}
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: page})
	a.width, a.height = 160, 48
	openPlanRoomNow(t, a, row.ID)
	text := roomSetupText(a)
	for _, word := range []string{"Assigned crew", "Worker · worker-model", "Planner · planner-model", "Checker · checker-model", "Run models cannot be changed here.", "/crew sets the next run's crew."} {
		if !strings.Contains(text, word) {
			t.Fatalf("missing %q: %s", word, text)
		}
	}
	if strings.Contains(text, "Used model") || a.roomModelMovable() {
		t.Fatalf("assignment claimed usage or editing: %s", text)
	}
	for _, line := range a.roomControlRows(a.railRoom()) {
		if line.roomAction == "model" {
			t.Fatal("run model offered the ordinary task picker")
		}
	}
	page.Models = nil
	a.planRoomTake(page)
	if text = roomSetupText(a); !strings.Contains(text, "Model · not recorded") || strings.Contains(text, a.model) {
		t.Fatalf("missing history was hidden or borrowed: %s", text)
	}
}

func TestProgramSetupShowsEveryDeclaredModelAndItsThinking(t *testing.T) {
	a, agent := programRoomApp(t, 160, 48)
	page := agent.planFake.pages["7"]
	page.Program.Models = []string{"saved/first-model", "saved/second-model"}
	page.Program.Effort = "high"
	agent.planFake.setPage("7", page)
	openProgramRoomNow(t, a)
	text := roomSetupText(a)
	for _, word := range []string{"Model · first-model", "Model · second-model", "Thinking · high", "Models are fixed for this run.", "Choose models when starting it again."} {
		if !strings.Contains(text, word) {
			t.Fatalf("missing %q: %s", word, text)
		}
	}
}

func TestAnOrdinaryTaskWithNoRecordedModelKeepsItsPicker(t *testing.T) {
	a, _ := roomModelApp(t, "")
	a.width, a.height = 160, 48
	if text := roomSetupText(a); !strings.Contains(text, "Model · not recorded") {
		t.Fatalf("unknown model vanished: %s", text)
	}
	panelRow(t, a, "model")
}

func TestRunTaskLinksOpenTheSameModelPageAsTheSidebar(t *testing.T) {
	a, fake := railTaskPageApp(t, true)
	a.width, a.height = 160, 48
	page := fake.planFake.pages["2"]
	page.Models = []session.PlanTaskModel{{Seat: "worker", Model: "saved/worker-model"}, {Seat: "planner", Model: "saved/planner-model"}}
	fake.planFake.setPage("t-2", page)
	a.tasks[2].planTask = "t-2"
	a.openRoomFor(2, a.tasks[2].title)
	drain(t, a, a.takeRoomPump())
	if a.roomPlan() == nil || a.roomModelMovable() {
		t.Fatal("a run task link opened the ordinary task picker")
	}
	if text := roomSetupText(a); !strings.Contains(text, "Planner · planner-model") {
		t.Fatalf("the task link lost its saved seats: %s", text)
	}
}
