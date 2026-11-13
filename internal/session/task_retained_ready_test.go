package session

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// L-LOOP2 C8, C6: only ready work without a live supervisor changes its word,
// including a canonical run row that hides its duplicate plan alias.
func TestRetainedReadyWorkReadsQueuedOnlyAfterItsRunLeaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "8", Title: "Ready work"})
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	t.Cleanup(func() {
		agent.beltMu.Lock()
		agent.beltRun = nil
		agent.beltMu.Unlock()
	})
	armPlanStore(t, agent, path, "chat-a")
	graph := agent.graph()
	graph.mu.Lock()
	graph.runs = map[uint64][]TaskNotice{7: {
		{ID: 7, PlanTask: store.RootID(), Title: "Run", State: TaskRunning},
		{ID: 8, Parent: 7, PlanTask: "8", Title: "Ready work", State: TaskRunning},
	}}
	graph.runRuns = []uint64{7}
	graph.mu.Unlock()
	for _, state := range []string{"live", "ending", "gone"} {
		agent.beltMu.Lock()
		agent.beltRun = &beltRun{store: store, ending: state == "ending"}
		if state == "gone" {
			agent.beltRun = nil
		}
		agent.beltMu.Unlock()
		want, appWant := "queued", "waiting"
		if state == "live" {
			want, appWant = "running", "running"
		}
		page, ok := agent.PlanTaskPage("t-8")
		if !ok || page.Row.Status != "ready" || page.Row.StateWord() != want {
			t.Errorf("%s page = %+v, found=%v, want %s", state, page.Row, ok, want)
		}
		for _, row := range agent.PlanTasks() {
			if row.ID == "t-8" && row.StateWord() != want {
				t.Errorf("%s list row = %+v, want %s", state, row, want)
			}
		}
		views, err := agent.AppTasks()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, view := range views {
			if view.ID == "8" {
				found = true
				if view.State != appWant {
					t.Errorf("%s app row = %+v, want %s", state, view, appWant)
				}
			}
		}
		if !found {
			t.Errorf("%s canonical row disappeared", state)
		}
	}
}
