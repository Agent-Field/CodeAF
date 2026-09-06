package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNamedSendIsDurableBeforeItsReceiptReturns(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	graph := agent.taskNode(id).graph
	path := filepath.Join(t.TempDir(), "tasks.json")
	graph.mu.Lock()
	graph.store = newTaskStore(path)
	graph.mu.Unlock()
	graph.checkpoint()
	from := SteerSource{Scope: "window-before-crash", Seq: 1, At: time.Now()}
	if _, err := agent.SteerTaskFrom(id, "retain this correction across engine restart", from); err != nil {
		t.Fatal(err)
	}
	// No Close or later task transition: the engine can die after returning.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), from.Scope) {
		t.Fatalf("acknowledged send identity is absent from the durable checkpoint: %s", raw)
	}
}
