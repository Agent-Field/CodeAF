package run_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/run"
)

// A RUNNING SCRIPT IS PUBLISHED AS A SCRIPT. The live step carries the tool the
// belt began, so a page draws the step in flight as what it is rather than as
// a bash command spelled in JSON.
func TestBashWorkerPublishesAScriptAsItsLiveTool(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_TASK_SCRIPT", "on")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	workspace := t.TempDir()
	release := filepath.Join(workspace, "release")
	source := "await tools.bash({command: " + jsonString("while [ ! -f "+release+" ]; do sleep 0.02; done") + "})"
	args, _ := json.Marshal(map[string]string{"source": source})
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
				ID: "script-1", Type: "function", Function: ai.ToolCallFunction{Name: "script", Arguments: string(args)},
			}}}}}}, nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(finishCommand("root", "the script ran")), nil
		},
	}}
	worker := run.NewBashWorker(store, workspace, "test/model", seat)
	done := make(chan error, 1)
	go func() {
		_, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID()))
		done <- err
	}()
	live := waitForLiveStep(t, store, store.RootID())
	if live.Tool != "script" {
		t.Errorf("the live step's tool = %q, want script", live.Tool)
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}
}
