package run_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/agentdid"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// A RUN WORKER ACTS AS ITS CONVERSATION'S WORKER FOR ITS TASK. A tool that
// records who wrote something (a canvas store's writer lock) reads the name off
// $CODEAF_AGENT_DID, and a run worker that carried none was recorded as the
// folder it typed in, the same folder its manager and every sibling type in. A
// task handed to the run keeps its row's number as its store id, so its
// worker is `<did>#w<n>`; a task the run split off under it acts as that row.
func TestBashWorkerCommandsCarryTheirTasksWorkerDID(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	did, err := agentdid.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ task, parent, want string }{
		{task: "7", want: did + "#w7"},
		{task: "split", parent: "7", want: did + "#w7"},
	} {
		t.Run(c.task, func(t *testing.T) {
			store := runOpenStore(t)
			specs := []plandb.TaskSpec{{ID: "7", ParentID: store.RootID(), Title: "Spine card", Description: "write the spine card"}}
			if c.parent != "" {
				specs = append(specs, plandb.TaskSpec{ID: c.task, ParentID: c.parent, Title: "A part", Description: "write a part"})
			}
			if _, err := store.AddMany(specs); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Claim(c.task, c.task); err != nil {
				t.Fatalf("claim the worker's task: %v", err)
			}
			workspace := t.TempDir()
			record := filepath.Join(workspace, "who.txt")
			write, _ := json.Marshal(struct {
				Command string `json:"command"`
			}{`printf %s "$CODEAF_AGENT_DID" > ` + record})
			seat := &seat{script: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) { return toolReply(string(write)), nil },
			}}
			worker := run.NewBashWorker(store, workspace, "test/model", seat, run.WithAgentDID(did))
			// ONE STEP IS ENOUGH: the cap ends the worker after its command, and what
			// the worker came to is not this test's question; what its command saw is.
			if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 1), *store.Task(c.task)); err != nil {
				t.Logf("the worker ended with %v", err)
			}
			raw, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(raw)); got != c.want {
				t.Fatalf("the worker's command saw CODEAF_AGENT_DID=%q, want %q", got, c.want)
			}
		})
	}
}
