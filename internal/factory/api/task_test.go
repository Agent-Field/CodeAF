package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// taskOnTheWire is a job at write with a task waiting behind the round
// running now.
func taskOnTheWire() factory.Item {
	it := factory.Item{ID: 7, Kind: factory.KindIssue, State: factory.StateRunning, Title: "fix",
		Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "make the change", On: true}, {Name: "proof", Kind: factory.StageChat, Ask: "show it", On: true}},
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{
			{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1, Note: "make the change"},
			{Name: "proof", Kind: factory.StageChat, State: factory.PhasePending, Note: "show it"},
		}}}
	task := factory.TaskPhase(it, factory.TaskByMainAgent, "Add a note on rounding.\nKeep it to two lines.")
	it.Stream.Phases = factory.InsertPhase(it.Stream.Phases, 1, task)
	return it
}

// AN EXTRA TASK IS A STEP ON THE WIRE LIKE ANY STEP THE RUN HOLDS: in the run
// and not in the pipeline, at its place among the steps, with its
// instructions as the step's ask and who asked as its by. A reader that
// knows nothing of tasks shows it as it shows a request for changes' prove.
func TestATaskIsAStepOnTheWire(t *testing.T) {
	it := taskOnTheWire()
	j := api.FromItem(it)
	if len(j.Steps) != 3 || j.Steps[1].Name != "task" {
		t.Fatalf("steps %+v", j.Steps)
	}
	task := j.Steps[1]
	if !task.InRun || task.InPipeline || task.Kind != "chat" || task.State != api.StepPending || task.By != factory.TaskByMainAgent ||
		task.Ask != "Add a note on rounding.\nKeep it to two lines." || task.Note != "Add a note on rounding." {
		t.Fatalf("the task's step: %+v", task)
	}
	raw, err := json.Marshal(j)
	if err != nil || !strings.Contains(string(raw), `"name":"task"`) {
		t.Fatalf("the job's JSON: %v", err)
	}
	var read api.Job
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	back := api.ToItem(read)
	if len(back.Stages) != 2 || len(back.Stream.Phases) != 3 {
		t.Fatalf("read back %d stages and %d phases, want the task in the run only", len(back.Stages), len(back.Stream.Phases))
	}
	if got := back.Stream.Phases[1]; got.Name != "task" || got.Task == nil || *got.Task != *it.Stream.Phases[1].Task {
		t.Fatalf("the task read back: %+v %+v", got, got.Task)
	}
	if back.Stream.Phases[0].Task != nil || back.Stream.Phases[2].Task != nil {
		t.Fatal("a step of the pipeline read back as a task")
	}
}

// A runner's report of its run's shape carries a task it placed, so the
// factory's copy follows the runner's order and keeps the task a task.
func TestARunnersShapeReportCarriesATask(t *testing.T) {
	it := taskOnTheWire()
	report := api.FromRunReport(factory.RunReport{Pipeline: &factory.PipelineChange{Stages: it.Stages, Phases: it.Stream.Phases}})
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var read api.WorkerReport
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	back := api.ToWorkerReport(read)
	if back.Pipeline == nil || len(back.Pipeline.Stages) != 2 || len(back.Pipeline.Phases) != 3 {
		t.Fatalf("the shape read back: %+v", back.Pipeline)
	}
	if got := back.Pipeline.Phases[1]; got.Name != "task" || got.Task == nil || got.Task.Words != it.Stream.Phases[1].Task.Words {
		t.Fatalf("the task in the shape: %+v", got)
	}
}

// The task door is one of the job's verbs.
func TestTaskIsAVerb(t *testing.T) {
	for _, v := range api.Verbs {
		if v.Name == "task" {
			return
		}
	}
	t.Fatal("api.Verbs does not name task")
}
