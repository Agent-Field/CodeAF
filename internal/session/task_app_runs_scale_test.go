package session

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

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

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	views, err := agent.AppTasks()
	runtime.ReadMemStats(&after)
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
	const budget = 16 << 20
	if spent := after.TotalAlloc - before.TotalAlloc; spent > budget {
		t.Fatalf("listing %d run rows allocated %d bytes, want under %d", runs, spent, budget)
	}
}
