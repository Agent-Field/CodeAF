package session

// The few internal fixtures an external test reaches through, so a law about a
// real engine behind a real socket (package session_test) stands on the same
// producers the internal laws do rather than on a copy of them.

import (
	"encoding/json"
	"os"
	"testing"
)

// RunningNodeForTest is a running node nobody is in yet, on its own checkpoint
// file ([nodeWithNobodyIn], [steerStore]).
func RunningNodeForTest(t *testing.T, path string) (*Agent, uint64) {
	t.Helper()
	agent, id, _ := nodeWithNobodyIn(t)
	steerStore(t, agent.taskNode(id), path)
	return agent, id
}

// StandAReaderInForTest puts a real worker in the node's room ([workerIn]).
func StandAReaderInForTest(t *testing.T, agent *Agent, id uint64) {
	t.Helper()
	workerIn(t, agent.taskNode(id))
}

// ReopenedNotesForTest is the node's page notes as a node restored from the
// checkpoint file reads them ([restoreNode]).
func ReopenedNotesForTest(t *testing.T, agent *Agent, id uint64, path string) []AppTaskNote {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document taskDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	for _, record := range document.Nodes {
		if record.ID != id {
			continue
		}
		restored := restoreNode(agent.taskNode(id).graph, record)
		restored.graph.mu.Lock()
		defer restored.graph.mu.Unlock()
		return restored.appNotesLocked()
	}
	t.Fatalf("no node %d on the disk", id)
	return nil
}
