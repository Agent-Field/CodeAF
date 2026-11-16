package session

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// L-LOOP2 C8a: starting another run cleans a done root's open descendants.
// The stop also repairs retained stores written before that cleanup existed.
func TestStopRetainedRunLeavesNewerRunUntouched(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "set aside", true: "older archive"}[legacy], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), planStoreFilename)
			agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
			graph := agent.graph()
			chat := graph.planChat()
			seedPlanStore(t, path, chat, plandb.TaskSpec{ID: "8", Title: "Joined task"}, plandb.TaskSpec{ID: "child", Title: "Worker task"})
			store, err := plandb.Open(path, "", "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"8", "child"} {
				if _, err := store.Claim(id, id, "old-owner"); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			writeSQL := func(path, query string) {
				t.Helper()
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				_, err = db.Exec(query)
				_ = db.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			writeSQL(path, "UPDATE tasks SET status = 'done', result = 'already finished' WHERE id = 'root'")
			armPlanStore(t, agent, path, chat)
			graph.mu.Lock()
			graph.runs = map[uint64][]TaskNotice{7: {
				{ID: 7, PlanTask: "root", Title: "Old run", State: TaskDone},
				{ID: 8, Parent: 7, Title: "Joined task", State: TaskRunning},
			}}
			graph.runRuns = []uint64{7}
			graph.mu.Unlock()
			_, newer, err := agent.openBeltRunStore(graph, path, "9", "New run", "Independent work", false)
			if err != nil {
				t.Fatal(err)
			}
			defer newer.Close()
			before := newer.Tasks()
			archived, err := plandb.Open(path+".1", "", "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			defer archived.Close()
			if legacy {
				writeSQL(path+".1", "UPDATE tasks SET status = 'claimed', claimed_by = id, owner = 'old-owner' WHERE id IN ('8', 'child')")
			} else {
				for _, task := range archived.Tasks() {
					if !terminalStoreStatus(task.Status) || task.ID != "root" && (task.ClaimedBy != "" || task.Owner != "") {
						t.Fatalf("archiving left open work: %+v", task)
					}
				}
			}
			if legacy {
				writeSQL(path+".1", "CREATE TRIGGER reject_stop BEFORE DELETE ON tasks BEGIN SELECT RAISE(ABORT, 'stop write refused'); END")
				if _, err := agent.Cancel(CancelTask + ":7"); err == nil || !strings.Contains(err.Error(), "stop write refused") {
					t.Fatalf("stop hid a retained-store write error: %v", err)
				}
				writeSQL(path+".1", "DROP TRIGGER reject_stop")
			}
			if _, err := agent.Cancel(CancelTask + ":7"); err != nil {
				t.Fatal(err)
			}
			for _, task := range archived.Tasks() {
				if !terminalStoreStatus(task.Status) || task.ID != "root" && (task.ClaimedBy != "" || task.Owner != "") {
					t.Fatalf("stop left retained work running: %+v", task)
				}
			}
			root := archived.Task("root")
			if root.Status != plandb.StatusDone || root.Result != "already finished" || root.ClaimedBy != "runtime" {
				t.Fatalf("old root's ending changed: %+v", root)
			}
			if !reflect.DeepEqual(before, newer.Tasks()) {
				t.Fatal("stopping the retained run changed the newer run")
			}
			views, err := agent.AppTasks()
			if err != nil {
				t.Fatal(err)
			}
			newerShown := false
			for _, view := range views {
				if view.ID == "t-9" {
					newerShown = true
					if view.State != "running" {
						t.Fatalf("stop changed the newer app row: %+v", view)
					}
				} else if view.State == "running" {
					t.Fatalf("retained task still reads running: %+v", view)
				}
			}
			if !newerShown {
				t.Fatal("stop hid the newer run from the app listing")
			}
		})
	}
}
