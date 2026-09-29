package session

import (
	"context"
	"fmt"
	"testing"
)

// RunScriptedSession drives one real turn of a real agent (real journal, real
// meta.json stamps, real ledger rows) against a scripted model, inside a cell
// folder at dir, and closes it. It is the writer half of the index-rebuild
// test in package session_test, which cannot reach newTestAgent.
func RunScriptedSession(t *testing.T, dir, text string) {
	t.Helper()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = Place{Dir: dir}.Transcript()
		c.Place = Place{Dir: dir, Workspace: c.Workspace}
	})
	events, err := a.Submit(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.SettleWrites()
	CloseUsage() // the exit door: no writer may hold the ledger a test then removes
}

// RunLandedTasks lands one task per title through a real agent over the
// session folder at dir: each writes its journal into the folder's node
// journals, its row into the bucket index, and its record into the checkpoint.
func RunLandedTasks(t *testing.T, dir string, titles ...string) {
	t.Helper()
	place := Place{Dir: dir}
	a := journalAgent(t, place)
	graph := a.graph()
	graph.mu.Lock()
	graph.run = func(*TaskNode) {}
	graph.mu.Unlock()
	for i, title := range titles {
		id := graph.reserve()
		graph.admit(id, taskSpec{title: title, brief: "do " + title, acceptance: title + " is done"})
		node := graph.node(id)
		node.setJournal(touchJournal(t, place, fmt.Sprintf("20260824-10150%d_%s.jsonl", i, itoa64(id))))
		node.finish("landed "+title, nil, "", "")
		graph.complete(node, TaskDone)
		waitDoneNode(t, node)
		awaitRecord(t, place.Tasks(), id, func(r taskRecord) bool { return r.State == TaskDone && r.Journal != "" }, "landed")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.SettleWrites()
}
