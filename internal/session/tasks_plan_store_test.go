package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// THE `tasks` TOOL READS A RUN BY THE NAMES A PERSON SEES. The fixture is the
// shape the real binary left on 2026-09-19: a hand-off the rail shows as `#2`
// is the store's task `2`, the run made two parts and two checks under it with
// ids of the store's own, and everything is done. The two calls replayed are
// the ones a real conversation made and was told `No task "1" in this project`
// and `No tasks have run in this project yet` for.
func TestTasksToolReadsFinishedRunFromPlanStore(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	path := filepath.Join(t.TempDir(), planStoreFilename)
	store, err := plandb.Open(path, "the project", "2", "Add Quad and Quint", "add two functions, each with its test", "chat-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "zevxkd", ParentID: "2", Title: "Add Quad", Description: "append Quad and its table test"},
		{ID: "gwhguv", ParentID: "2", Title: "Add Quint", Description: "append Quint and its table test"},
		{ID: "tytva5", ParentID: "2", Title: "check: Add Quint", Role: "check", Description: "rerun the declared check"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, done := range []struct{ id, result string }{
		{"zevxkd", "Quad appended; its test passes"},
		{"gwhguv", "Quint appended after Quad\nand its table test covers zero and negatives"},
		{"tytva5", "holds: the declared check exits 0"},
		{"2", "both parts landed"},
	} {
		// THE ROOT IS FINISHED BY THE STORE'S OWN DOOR FOR IT, once its parts are
		// home; a task with parts is not a leaf anybody claims.
		if done.id == "2" {
			if err := store.CompleteRoot(done.result); err != nil {
				t.Fatalf("complete the root: %v", err)
			}
			continue
		}
		if _, err := store.Claim(done.id, done.id); err != nil {
			t.Fatalf("claim %s: %v", done.id, err)
		}
		if _, err := store.Done(done.id, done.id, done.result, nil, nil); err != nil {
			t.Fatalf("done %s: %v", done.id, err)
		}
	}
	_ = store.Close()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")

	listing, failed := runTool(t, agent, "tasks", `{}`)
	if failed {
		t.Fatalf("the listing failed: %s", listing)
	}
	for _, want := range []string{"#2 · Add Quad and Quint · done · both parts landed", "#2.1 · Add Quad · done", "#2.2 · Add Quint · done · Quint appended after Quad", "#2.3 · check: Add Quint"} {
		if !strings.Contains(listing, want) {
			t.Errorf("the listing misses %q:\n%s", want, listing)
		}
	}
	if strings.Contains(listing, "No tasks have run") {
		t.Errorf("the listing still says nothing ran:\n%s", listing)
	}

	for _, name := range []string{"2.2", "#2.2"} {
		part, failed := runTool(t, agent, "tasks", `{"id":"`+name+`"}`)
		if failed {
			t.Fatalf("reading %s failed: %s", name, part)
		}
		for _, want := range []string{"#2.2 · Add Quint · done", "append Quint and its table test", "Quint appended after Quad\nand its table test covers zero and negatives", "holds: the declared check exits 0"} {
			if !strings.Contains(part, want) {
				t.Errorf("reading %s misses %q:\n%s", name, want, part)
			}
		}
	}
	whole, failed := runTool(t, agent, "tasks", `{"id":"2"}`)
	if failed || !strings.Contains(whole, "#2 · Add Quad and Quint · done") || !strings.Contains(whole, "both parts landed") {
		t.Fatalf("reading the hand-off by the rail's own number: failed=%v\n%s", failed, whole)
	}
	for _, answer := range []string{listing, whole} {
		for _, id := range []string{"zevxkd", "gwhguv", "tytva5", "t-2"} {
			if strings.Contains(answer, id) {
				t.Errorf("a store id %q reached an answer:\n%s", id, answer)
			}
		}
	}

	// A NAME THE RUN DOES NOT HOLD IS THE SHIPPED READER'S TO ANSWER, as before.
	other, _ := runTool(t, agent, "tasks", `{"id":"9"}`)
	if !strings.Contains(other, `No task "9"`) {
		t.Errorf("an id the run does not hold was not handed to the shipped reader:\n%s", other)
	}
}

// WITH NO RUN THE LISTING IS WHAT IT ALWAYS WAS.
func TestTasksToolWithNoRunAnswersAsBefore(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	listing, _ := runTool(t, agent, "tasks", `{}`)
	if !strings.Contains(listing, "No tasks have run") {
		t.Fatalf("the listing of a conversation with no run changed:\n%s", listing)
	}
}
