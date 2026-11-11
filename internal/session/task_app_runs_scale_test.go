package session

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"weak"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// A LIST OF A CONVERSATION'S RUNS OPENS ITS PLAN ONCE. Each run row used to
// open the plan store for itself, and every open read the whole plan, so a
// conversation with a hundred and fifty rows read a hundred and fifty plans
// to draw one list — gigabytes of allocation, seconds of work, for every
// listing of one agent's tasks.
func TestAppTasksOpensThePlanOnceForAllTheRunRows(t *testing.T) {
	const runs = 150
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	specs := make([]plandb.TaskSpec, runs)
	for i := range specs {
		specs[i] = plandb.TaskSpec{ID: fmt.Sprintf("r%d", i), Title: fmt.Sprintf("Run step %d", i), Description: "a step with some words in it to give the row a size"}
	}
	seedPlanStore(t, path, "chat-a", specs...)
	for i := range specs {
		writePlanTrajectory(t, dir, specs[i].ID, `{"kind":"step","step":1,"command":"$ echo one","observation":"one"}`)
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	graph := agent.graph()
	graph.mu.Lock()
	graph.runs = map[uint64][]TaskNotice{}
	for i := range specs {
		id := uint64(1000 + i)
		graph.runs[id] = []TaskNotice{{ID: id, Title: specs[i].Title, State: TaskDone, PlanTask: specs[i].ID}}
		graph.runRuns = append(graph.runRuns, id)
	}
	graph.mu.Unlock()

	openStore, readFile, openFile := openPlanReadStore, readPlanTrajectoryFile, openPlanTrajectoryFile
	t.Cleanup(func() {
		openPlanReadStore, readPlanTrajectoryFile, openPlanTrajectoryFile = openStore, readFile, openFile
	})
	opens := map[string]int{}
	reads := map[string]int{}
	openPlanReadStore = func(path string) *plandb.Store { opens[path]++; return openStore(path) }
	readPlanTrajectoryFile = func(path string) ([]byte, error) { reads[path]++; return readFile(path) }
	openPlanTrajectoryFile = func(path string) (*os.File, error) { reads[path]++; return openFile(path) }

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	views, err := agent.AppTasks()
	runtime.ReadMemStats(&after)
	t.Logf("listing %d run rows allocated %d bytes", runs, after.TotalAlloc-before.TotalAlloc)
	if err != nil {
		t.Fatal(err)
	}
	withSteps := 0
	for _, view := range views {
		if view.StepsKnown {
			withSteps++
		}
	}
	if withSteps != runs {
		t.Fatalf("%d of %d run rows carried their steps", withSteps, runs)
	}
	if got := opens[path]; got != 1 {
		t.Errorf("listing opened the live plan %d times, want 1", got)
	}
	for _, spec := range specs {
		path := planTrajectoryPath(dir, spec.ID)
		if got := reads[path]; got != 1 {
			t.Errorf("listing read %s %d times, want 1", spec.ID, got)
			break
		}
	}
	for path, count := range reads {
		if count != 1 {
			t.Errorf("trajectory %s was read %d times, want 1", path, count)
			break
		}
	}
	const budget = 16 << 20
	if spent := after.TotalAlloc - before.TotalAlloc; spent > budget {
		t.Fatalf("listing %d run rows allocated %d bytes, want under %d", runs, spent, budget)
	}
}

func TestAppTaskListingRereadsChangedTrajectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "work", Title: "Work"})
	writePlanTrajectory(t, dir, "work", `{"kind":"step","step":1,"command":"echo first"}`)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	graph := agent.graph()
	graph.mu.Lock()
	graph.runs = map[uint64][]TaskNotice{7: {{ID: 7, Title: "Work", State: TaskDone, PlanTask: "work"}}}
	graph.runRuns = []uint64{7}
	graph.mu.Unlock()
	for _, count := range []int{1, 2} {
		views, err := agent.AppTasks()
		if err != nil {
			t.Fatal(err)
		}
		seen := 0
		for _, view := range views {
			switch view.ID {
			case "7", "t-work":
				if view.Step != count {
					t.Errorf("%s has %d steps, want %d", view.ID, view.Step, count)
				}
				seen++
			}
		}
		if seen != 2 {
			t.Fatalf("saw %d work projections, want 2", seen)
		}
		writePlanTrajectory(t, dir, "work", `{"kind":"step","step":1,"command":"echo first"}`, `{"kind":"step","step":2,"command":"echo next"}`)
	}
}

func TestPlanTaskListingDoesNotRetainTrajectoryPayloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	specs := []plandb.TaskSpec{{ID: "alpha", Title: "Alpha"}, {ID: "beta", Title: "Beta"}, {ID: "gamma", Title: "Gamma"}}
	seedPlanStore(t, path, "chat-a", specs...)
	for _, spec := range specs {
		writePlanTrajectory(t, dir, spec.ID, `{"kind":"step","step":1,"command":"echo recorded"}`)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	readFile := readPlanTrajectoryFile
	t.Cleanup(func() { readPlanTrajectoryFile = readFile })
	var previous weak.Pointer[byte]
	checked := 0
	readPlanTrajectoryFile = func(path string) ([]byte, error) {
		runtime.GC()
		if previous != (weak.Pointer[byte]{}) {
			checked++
			if previous.Value() != nil {
				t.Error("previous trajectory payload is retained while reading the next task")
			}
		}
		data, err := os.ReadFile(path)
		if len(data) > 0 {
			previous = weak.Make(&data[0])
		}
		return data, err
	}
	rows := agent.PlanTasks()
	if len(rows) != 4 {
		t.Fatalf("read %d plan rows, want 4", len(rows))
	}
	for _, row := range rows[1:] {
		if row.Steps != 1 {
			t.Errorf("%s has %d steps, want 1", row.ID, row.Steps)
		}
	}
	if checked != 2 {
		t.Fatalf("checked %d prior payloads, want 2", checked)
	}
}
