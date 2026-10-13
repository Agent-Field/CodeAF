package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func TestFactoryHandoffStopsTheOriginTaskAndKeepsItsReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "origin", Title: "Original work"}, plandb.TaskSpec{ID: "other", Title: "Other work"})
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	if err := agent.PlanCancelWithReason("t-origin", "Handed to the factory"); err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	task := store.Task("origin")
	if task.Status != plandb.StatusCancelled || !strings.Contains(task.Error, "Handed to the factory") {
		t.Fatal(task)
	}
	if store.Task("other").Status == plandb.StatusCancelled {
		t.Fatal("hand-off stopped unrelated work")
	}
}
