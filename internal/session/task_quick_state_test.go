package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Contract C4.a and C4.b: the node's ending outranks a stale promotion, while
// Undo and an approved promotion outrank that ending.
func TestAppQuickStatePrecedenceIncludesStopped(t *testing.T) {
	for _, test := range []struct {
		name     string
		state    TaskState
		grew     bool
		promoted bool
		undone   bool
		want     string
	}{
		{"running", TaskRunning, false, false, false, "running"},
		{"waiting", TaskRunning, true, false, false, "grew"},
		{"finished", TaskDone, false, false, false, "done"},
		{"finished after ask", TaskDone, true, false, false, "done"},
		{"failed", TaskFailed, false, false, false, "stopped"},
		{"failed after ask", TaskFailed, true, false, false, "stopped"},
		{"promoted", TaskFailed, true, true, false, "promoted"},
		{"undone", TaskFailed, true, true, true, "undone"},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent, node, _ := appNodeFixture(t, true)
			node.graph.mu.Lock()
			node.state = test.state
			node.spec.quick.promoted, node.spec.quick.undone = test.promoted, test.undone
			if test.grew {
				node.spec.quick.promotion, node.spec.quick.grew = 2, "needs a shell"
			}
			node.graph.mu.Unlock()
			rows, err := agent.AppQuickTasks()
			if err != nil || len(rows) != 1 || rows[0].State != test.want {
				t.Fatalf("rows = %+v (%v), want %s", rows, err, test.want)
			}
		})
	}
}

// Contract C4.a, C4.c and C4.d: a real worker writes then fails; its stopped
// state, files and Undo survive closing and reopening the conversation.
func TestAppQuickFailureKeepsItsFilesAndStateAcrossAReopen(t *testing.T) {
	model := newQuickLanes([]step{quickCall("quick", "draft a note", "STOPPED-CONTRACT draft a note", nil, nil), finalText("started")})
	fail := func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("provider returned error: 401 access denied")
	}
	model.lane("STOPPED-CONTRACT",
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("write-note", "write", `{"path":"note.txt","content":"unfinished"}`), nil
		}, fail, fail, fail, fail, fail, fail, fail, fail, fail, fail, fail, fail,
		fail, fail, fail, fail, fail, fail, fail, fail, fail, fail, fail, fail,
	)
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	agent, graph, workspace := quickAgent(t, model, func(c *Config) { c.SessionFile = path })
	onATestClock(t)
	collect(t, mustSubmit(t, agent, "draft a note"))
	node := quickNodeSaying(t, graph, "STOPPED-CONTRACT")
	waitDoneNode(t, node)
	before, _ := agent.AppQuickTasks()
	if len(before) != 1 || before[0].State != "stopped" || len(before[0].Files) != 1 {
		t.Fatalf("failed quick work = %+v", before)
	}
	graph.checkpoint()
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	fresh, _ := newTestAgent(t, model, func(c *Config) { c.SessionFile, c.Workspace = path, workspace })
	after, _ := fresh.AppQuickTasks()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("quick changed across restart: %+v -> %+v", before, after)
	}
	undone, changed, err := fresh.UndoQuickTask(node.id)
	if err != nil || len(undone) != 1 || len(changed) != 0 {
		t.Fatalf("Undo after restart = %v %v %v", undone, changed, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("unfinished file survived Undo: %v", err)
	}
}
