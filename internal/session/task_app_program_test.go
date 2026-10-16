package session

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// programAppFixture is a senior-dev task on disk with a hello of four stages, a
// stage line, a step and an ending, read through the real AppTask door.
func programAppFixture(t *testing.T, tail ...delegate.Action) (*Agent, string) {
	t.Helper()
	agent, dir := programPageFixture(t)
	folder := plandb.TaskDir(dir, "alpha")
	record, _ := delegate.ReadProgram(folder)
	record.Stages = []string{"bootstrap", "intake", "mystery", "implement"}
	record.StartedAt = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	if err := delegate.WriteProgram(folder, record); err != nil {
		t.Fatal(err)
	}
	agent.config.Delegates = []delegate.Delegate{{Name: "senior-dev", Listens: true,
		StageWords: map[string]string{"bootstrap": "starting", "intake": "reading the brief", "implement": "working"}}}
	at := time.Date(2026, 10, 16, 9, 1, 0, 0, time.UTC)
	for i, action := range append([]delegate.Action{
		delegate.StageAction(at, delegate.StageRecord{Stage: "intake", Status: "captured"}),
		delegate.StepAction(at.Add(time.Second), delegate.StepRecord{Tool: "read", Step: "explore", Command: "read internal/auth/middleware.go"}),
	}, tail...) {
		if action.At.IsZero() {
			action.At = at.Add(time.Duration(i) * time.Minute)
		}
		if err := delegate.AppendAction(folder, action); err != nil {
			t.Fatal(err)
		}
	}
	return agent, dir
}

func appTaskOf(t *testing.T, agent *Agent) AppTaskView {
	t.Helper()
	rows, err := agent.AppTasks()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if view, found, err := agent.AppTask(row.ID); err == nil && found && view.Program != nil {
			return view
		}
	}
	t.Fatalf("no program task among %d rows", len(rows))
	return AppTaskView{}
}

// THE APP'S TASK READ CARRIES THE PROGRAM THE PAGE READ ALREADY HELD.
func TestAppTaskCarriesTheProgramTheStageWordsAndTheLastStage(t *testing.T) {
	agent, _ := programAppFixture(t)
	view := appTaskOf(t, agent)
	p := view.Program
	if p.Name != "senior-dev" || p.CeilingUSD != 5 || p.Calls != 2 || p.Effort != "high" || !p.Listens || p.LastStage != "intake" {
		t.Fatalf("program = %+v", p)
	}
	// A wordless stage keeps the word standing before it, as the live step does.
	want := []AppProgramStage{{"bootstrap", "starting"}, {"intake", "reading the brief"}, {"mystery", "reading the brief"}, {"implement", "working"}}
	if len(p.Stages) != 4 || p.Stages[2] != want[2] || p.Stages[3] != want[3] || p.Stages[0] != want[0] {
		t.Fatalf("stages = %+v, want %+v", p.Stages, want)
	}
	if len(p.Actions) != 2 || p.Ending != nil || p.StartedAt.IsZero() {
		t.Fatalf("actions %d ending %v started %v", len(p.Actions), p.Ending, p.StartedAt)
	}
	// It crosses the remote wire as the field it is.
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var back AppTaskView
	if err := json.Unmarshal(raw, &back); err != nil || back.Program == nil || back.Program.LastStage != "intake" || len(back.Program.Stages) != 4 {
		t.Fatalf("remote round trip: %v %+v", err, back.Program)
	}
}

func TestAProgramWithNoWordsIsShownByItsStageNames(t *testing.T) {
	if got := AppProgramStageWord(delegate.Delegate{Name: "x"}, "recon", "starting"); got != "recon" {
		t.Fatalf("word = %q", got)
	}
	words := delegate.Delegate{StageWords: map[string]string{}}
	if got := AppProgramStageWord(words, "mystery", ""); got != "" {
		t.Fatalf("a first wordless stage has no word, got %q", got)
	}
}

// AN ENDING IS CURRENT ONLY WHILE IT IS THE LOG'S LAST LINE.
func TestTheEndingIsCurrentOnlyWhileItIsTheLastLine(t *testing.T) {
	end := delegate.EndAction(time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC), delegate.Terminal{Status: delegate.StatusPass, Message: "handed in",
		Data: map[string]json.RawMessage{"submitted": json.RawMessage(`true`), "findings": json.RawMessage(`null`)}})
	agent, dir := programAppFixture(t, end)
	p := appTaskOf(t, agent).Program
	if p.Ending == nil || p.Ending.Text != "handed in" || p.EndStatus != delegate.StatusPass ||
		p.Ending.Ending == nil || p.Ending.Ending.Submitted == nil || !*p.Ending.Ending.Submitted || p.Ending.Ending.Findings != nil {
		t.Fatalf("ending = %+v status %q", p.Ending, p.EndStatus)
	}
	later := delegate.StageAction(time.Date(2026, 10, 16, 9, 40, 0, 0, time.UTC), delegate.StageRecord{Stage: "implement", Status: "running"})
	if err := delegate.AppendAction(plandb.TaskDir(dir, "alpha"), later); err != nil {
		t.Fatal(err)
	}
	p = appTaskOf(t, agent).Program
	if p.Ending != nil || p.LastStage != "implement" {
		t.Fatalf("a run begun again still shows its old ending: %+v last %q", p.Ending, p.LastStage)
	}
}

func TestAnOrdinaryTaskAppReadCarriesNoProgram(t *testing.T) {
	agent, dir := programPageFixture(t)
	seedPlanStore(t, filepath.Join(dir, planStoreFilename), "chat-a", plandb.TaskSpec{ID: "beta", Title: "Beta", Description: "plain"})
	rows, _ := agent.AppTasks()
	seen := false
	for _, row := range rows {
		if view, found, _ := agent.AppTask(row.ID); found && view.Title == "Beta" {
			seen = true
			if view.Program != nil {
				t.Fatalf("an ordinary task carries a program: %+v", view.Program)
			}
		}
	}
	if !seen {
		t.Fatalf("the ordinary task was not among %d rows", len(rows))
	}
}
