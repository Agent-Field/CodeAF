package session

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// A belt-on conversation reads the run store through the same numbers the rail
// draws. This replays the failed #1 read followed by the listing from fact 2.
func TestTasksToolReadsFinishedRunFromPlanStore(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a",
		plandb.TaskSpec{ID: "first-part", Title: "First part", Description: "change the first file"},
		plandb.TaskSpec{ID: "second-part", Title: "Second part", Description: "change the second file"},
		plandb.TaskSpec{ID: "second-check", Title: "Check second part", Role: "check", Description: "prove the second part"},
	)
	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, result string }{{"first-part", "first part result"}, {"second-check", "holds: second part test passed"}, {"second-part", "second part changed parser\nand retained detail"}} {
		if _, err := store.Claim(tc.id, tc.id); err != nil {
			t.Fatalf("claim %s: %v", tc.id, err)
		}
		if _, err := store.Done(tc.id, tc.id, tc.result, nil, nil); err != nil {
			t.Fatalf("done %s: %v", tc.id, err)
		}
	}
	_ = store.Close()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")

	byNumber, failed := runTool(t, agent, "tasks", `{"id":"1"}`)
	if failed {
		t.Fatalf("tasks #1 failed: %s", byNumber)
	}
	listing, failed := runTool(t, agent, "tasks", `{}`)
	if failed {
		t.Fatalf("tasks listing failed: %s", listing)
	}
	for _, want := range []string{"#1", "#2", "#3", "Second part", "second part changed parser"} {
		if !strings.Contains(listing, want) {
			t.Fatalf("listing misses %q:\n%s", want, listing)
		}
	}
	if strings.Contains(listing, "t-second-part") || strings.Contains(byNumber, "t-root") {
		t.Fatalf("internal store id leaked:\n%s\n%s", listing, byNumber)
	}
	byStoreID, failed := runTool(t, agent, "tasks", `{"id":"second-part"}`)
	if failed {
		t.Fatalf("store id read failed: %s", byStoreID)
	}
	for _, want := range []string{"change the second file", "second part changed parser\nand retained detail", "holds: second part test passed"} {
		if !strings.Contains(byStoreID, want) {
			t.Fatalf("one-task answer misses %q:\n%s", want, byStoreID)
		}
	}
	if strings.Contains(byStoreID, "second-part") {
		t.Fatalf("one-task answer leaked store id: %s", byStoreID)
	}
	_ = fmt.Sprint(byNumber)
}
