package session

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// L-LOOP2 C7a: the app's tree links use exactly the ids the listing emits.
func TestAppPlanChildrenLinkToTheirListedRunAndParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), planStoreFilename)
	seedPlanStore(t, path, "chat-a")
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AddManyForWorker(store.RootID(), []plandb.TaskSpec{{ID: "child", ParentID: store.RootID(), Title: "Child"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddManyForWorker("child", []plandb.TaskSpec{{ID: "grandchild", ParentID: "child", Title: "Grandchild"}}); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	graph := agent.graph()
	graph.mu.Lock()
	graph.runs = map[uint64][]TaskNotice{7: {{ID: 7, PlanTask: store.RootID(), Title: "Run", State: TaskRunning, Run: "group-7"}}}
	graph.runRuns = []uint64{7}
	graph.mu.Unlock()
	views, err := agent.AppTasks()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]AppTaskView{}
	for _, view := range views {
		byID[view.ID] = view
	}
	if len(views) != 3 || byID["t-child"].Parent != "7" || byID["t-grandchild"].Parent != "t-child" {
		t.Fatalf("listing loses or duplicates the tree: %+v", views)
	}
	for _, view := range views {
		if view.Group != "group-7" {
			t.Fatalf("run group lost: %+v", view)
		}
	}
}

// L-LOOP2 C8a, C8b: the existing stop door cancels open children even when the
// root had already ended, and stale published run rows cannot keep spinning.
func TestPlanStopEndsOpenWorkBelowAnAlreadyEndedRoot(t *testing.T) {
	for _, ended := range []bool{false, true} {
		t.Run(map[bool]string{false: "running root", true: "done root"}[ended], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), planStoreFilename)
			seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "8", Title: "Joined task"}, plandb.TaskSpec{ID: "child", Title: "Worker task"})
			store, err := plandb.Open(path, "", "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			for _, id := range []string{"8", "child"} {
				if _, err := store.Claim(id, id, "test-owner"); err != nil {
					t.Fatal(err)
				}
			}
			if ended {
				// Reproduce a retained store whose root ended before its workers.
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				_, err = db.Exec("UPDATE tasks SET status = 'done', result = 'already finished' WHERE id = ?", store.RootID())
				_ = db.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
			armPlanStore(t, agent, path, "chat-a")
			graph := agent.graph()
			rootState := TaskRunning
			if ended {
				rootState = TaskDone
			}
			graph.mu.Lock()
			graph.runs = map[uint64][]TaskNotice{7: {
				{ID: 7, PlanTask: store.RootID(), Title: "Run", State: rootState},
				{ID: 8, Parent: 7, Title: "Joined task", State: TaskRunning},
			}}
			graph.runRuns = []uint64{7}
			graph.mu.Unlock()
			if _, err := agent.Cancel(CancelTask + ":7"); err != nil {
				t.Fatal(err)
			}
			for _, task := range store.Tasks() {
				if !terminalStoreStatus(task.Status) || task.ClaimedBy != "" || task.Owner != "" {
					t.Fatalf("stop left open work: %+v", task)
				}
			}
			// A late worker result cannot overwrite the cancelled task.
			_, _ = store.Done("child", "child", "late result", nil, nil)
			views, err := agent.AppTasks()
			if err != nil {
				t.Fatal(err)
			}
			for _, view := range views {
				if view.State == "running" {
					t.Fatalf("stop left a running app row: %+v", view)
				}
			}
			if ended && (store.Task(store.RootID()).Status != plandb.StatusDone || store.Task(store.RootID()).Result != "already finished") {
				t.Fatal("stop rewrote the root's earlier ending")
			}
		})
	}
}

// L-LOOP2 C3: the actual worker constructor binds its own task beside its run.
func TestRunWorkerShellBindsItsOwnTask(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, filepath.Join(t.TempDir(), "stub-codeaf"))
	agent := newRunBeltWorker(t, effort.None)
	prefix := agent.graph().planBashPrefix()
	for _, binding := range []string{plandb.RunEnv + "=", plandb.TaskEnv + "="} {
		if !strings.Contains(prefix, binding+quoteShWord(planRootID)) {
			t.Fatalf("worker lacks %s: %s", binding, prefix)
		}
	}
}
