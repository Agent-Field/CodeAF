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

// A HAND-OFF THE LISTING SHOWS IS ONE `continue` CAN NAME. qloop run31, hiring
// t2: `tasks` listed `#1 · … · done`, and `tasks {"id":"1","continue":true}` was
// refused three times as a task this conversation did not hold, because the
// reader below leaves this conversation's own runs out of its rows. The model
// then copied that refusal onto the person's card. A finished run is answered
// for what it is — done, with the move that is actually open — and a part of a
// run is told it cannot be continued on its own; neither is the stranger's
// refusal any more.
func TestContinueOnAFinishedHandOffSaysItIsDoneNotThatItIsNotHeld(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	path := filepath.Join(t.TempDir(), planStoreFilename)
	store, err := plandb.Open(path, "the project", "1", "Hiring dashboard", "build the hiring dashboard", "chat-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "p1", ParentID: "1", Title: "Summary tab", Description: "count the stages"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim("p1", "p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Done("p1", "p1", "counted", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteRoot("built the dashboard"); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")

	listing, _ := runTool(t, agent, "tasks", `{}`)
	if !strings.Contains(listing, "#1 · Hiring dashboard · done") {
		t.Fatalf("the listing does not show the hand-off:\n%s", listing)
	}
	for _, id := range []string{`"1"`, `1`, `"#1"`} {
		answer, failed := runTool(t, agent, "tasks", `{"id":`+id+`,"continue":true}`)
		if !failed {
			t.Fatalf("continue %s on a finished run read as a resume:\n%s", id, answer)
		}
		if !strings.Contains(answer, "#1 · Hiring dashboard is done, so there is nothing to continue") {
			t.Errorf("continue %s did not say the run is done:\n%s", id, answer)
		}
		for _, wrong := range []string{"not one this conversation holds", "graph", "another window"} {
			if strings.Contains(answer, wrong) {
				t.Errorf("continue %s still answers as a task this conversation does not hold (%q):\n%s", id, wrong, answer)
			}
		}
	}
	part, failed := runTool(t, agent, "tasks", `{"id":"1.1","continue":true}`)
	if !failed || !strings.Contains(part, "#1.1 · Summary tab is one part of a run") {
		t.Errorf("continue on a part: failed=%v\n%s", failed, part)
	}
}
