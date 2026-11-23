package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// This is checkpoint API acceptance with fictional rows, not live-task proof.
func TestIndependentForkKeepsTaskResultsWithoutRunnableFrontier(t *testing.T) {
	place := newPlace(t, true)
	doc := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 4,
		Nodes: []taskRecord{{ID: 1, Title: "completed", Brief: "brief", Acceptance: "exists", State: TaskDone, Report: "report", Result: &taskResultRecord{Text: "full answer"}, Worktree: "/foreign/tree", WorkspaceURI: "foreign", ChatURI: "foreign"}, {ID: 2, Title: "queued", Brief: "brief", Acceptance: "exists", State: TaskQueued}},
		Runs:  []runRecord{{ID: 3, Title: "active", State: TaskRunning, Report: "partial", Copy: &TaskCopyRecord{Dir: "/foreign/run"}, PendingRun: &PendingRunRecord{}}, {ID: 4, Title: "done", State: TaskDone, Report: "finished", Copy: &TaskCopyRecord{Dir: "/foreign/done"}}}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(place.Tasks(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	complete, err := ForkTaskHistory(place)
	if err != nil {
		t.Fatal(err)
	}
	if len(complete) != 2 || complete[0].ID != "1" || complete[1].ID != "4" {
		t.Fatalf("completed results selection=%v", complete)
	}
	got, ok := loadTaskCheckpoint(place.Tasks())
	if !ok {
		t.Fatal("fork checkpoint unreadable")
	}
	if len(got.Nodes) != 0 || len(got.Runs) != 4 || got.Seq != 4 {
		t.Fatalf("fork retained executable nodes or lost history: %+v", got)
	}
	for _, row := range got.Runs {
		if !row.State.settled() || row.Copy != nil || row.PendingRun != nil {
			t.Fatalf("fork history can resume: %+v", row)
		}
	}
	if got.Runs[0].Report != "report" || got.Runs[0].Result != "full answer" {
		t.Fatal("completed result lost")
	}
	archived, err := os.ReadFile(filepath.Join(ForkHistoryDir(place), "tasks.json"))
	if err != nil || string(archived) != string(raw) {
		t.Fatalf("original checkpoint lost: %v", err)
	}
	again, err := ForkTaskHistory(place)
	if err != nil || len(again) != 2 {
		t.Fatalf("fork boundary not retry-safe: %v %v", again, err)
	}
}
