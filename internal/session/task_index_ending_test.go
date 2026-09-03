package session

import (
	"encoding/json"
	"strings"
	"testing"
)

// The record carries the engine's existing ending as data. Surfaces can then
// call a refused claim incomplete without parsing a report sentence, while old
// rows keep the absence that tells them to retain their old failed rendering.
func TestTheTaskIndexCarriesRefusedEndingsAdditively(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{
		graph:  graph,
		id:     4,
		state:  TaskFailed,
		ending: TaskEndingRefused,
		spec:   taskSpec{title: "review the pull request diff"},
	}
	graph.mu.Lock()
	entry := node.indexEntryLocked("aaaa1111aaaa1111")
	graph.mu.Unlock()
	if entry.Ending != TaskEndingRefused {
		t.Fatalf("ending = %q, want %q", entry.Ending, TaskEndingRefused)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ending":"refused"`) {
		t.Fatalf("refused ending was not written: %s", raw)
	}

	var old TaskIndexEntry
	if err := json.Unmarshal([]byte(`{"id":"4","name":"review","label":"review","title":"review","status":"failed"}`), &old); err != nil {
		t.Fatalf("an old row no longer decodes: %v", err)
	}
	if old.Ending != "" {
		t.Fatalf("old row ending = %q, want unknown", old.Ending)
	}
	without, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), `"ending"`) {
		t.Fatalf("an unknown ending was written: %s", without)
	}
}
