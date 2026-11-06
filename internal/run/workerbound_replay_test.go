package run_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// L-LOOP2 C1c, C3a, C3d, C3e: replay one real-shaped chained bash call as the
// first step of each worker, with renamed work bypassing the title guard.
func TestWorkerFirstStepCannotRecreateSiblingTasksAtTheRunRoot(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := startOpenStore(t, "Create the red file")
	workspace := t.TempDir()
	var mu sync.Mutex
	starts := map[string]int{}
	depthRefusals := 0
	factory := func(task plandb.Task) run.Worker {
		mu.Lock()
		starts[task.ID]++
		first := starts[task.ID] == 1
		mu.Unlock()
		command := `plandb add "Create red.txt" --description "Write 'red' into red.txt" && plandb add "Create green.txt" --description "Write green into green.txt" && plandb add "Create blue.txt" --description "Write blue into blue.txt"`
		if task.ID != store.RootID() {
			command = fmt.Sprintf(`plandb add "Create the green file for %s" --description "Write 'green' into green.txt" && plandb add "Make blue.txt for %s" --description "Write blue into blue.txt" && plandb add "Make the red file for %s" --description "Write red into red.txt"`, task.ID, task.ID, task.ID)
		}
		calls := 0
		completer := &seat{ever: func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			calls++
			if first && calls == 1 {
				return toolReply(bashArguments(t, command)), nil
			}
			for _, message := range messages {
				if strings.Contains(messageContent(message), "2 levels below") {
					mu.Lock()
					depthRefusals++
					mu.Unlock()
					break
				}
			}
			for _, child := range store.Tasks() {
				if child.ParentID == task.ID && child.Status != plandb.StatusDone && child.Status != plandb.StatusFailed && child.Status != plandb.StatusCancelled {
					return toolReply(bashArguments(t, "plandb wait --agent "+task.ID)), nil
				}
			}
			return toolReply(finishCommand(task.ID, "finished my own work")), nil
		}}
		return run.NewBashWorker(store, workspace, "test/model", completer)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	outcome, summary := run.Start(ctx, run.Spec{Store: store, Workspace: workspace, Title: "Create the red file", Brief: "Write red into red.txt", Slots: 1, Factory: factory, Limits: run.Limits{StepsPerTask: 4}})
	if outcome != run.OutcomeDone || ctx.Err() != nil {
		t.Fatalf("replay did not end: %s %+v %v", outcome, summary, ctx.Err())
	}
	added := map[string]int{}
	tasks := store.Tasks()
	for _, task := range tasks {
		if task.ID == store.RootID() {
			continue
		}
		if task.AddedByTask == "" || task.ParentID != task.AddedByTask {
			t.Fatalf("worker added outside its own task: %+v", task)
		}
		added[task.AddedByTask]++
		if added[task.AddedByTask] > 5 {
			t.Fatalf("one worker handed out more than five: %+v", added)
		}
		depth := 0
		for parent := task; parent.ID != store.RootID(); parent = store.Task(parent.ParentID) {
			depth++
			if depth > 2 {
				t.Fatalf("worker escaped the depth bound: %+v", task)
			}
		}
	}
	if len(tasks) != 13 || len(tasks) > 20 || added[store.RootID()] != 3 || depthRefusals == 0 {
		t.Fatalf("replay missed recursive renamed work or a real depth refusal: tasks=%d added=%v refusals=%d", len(tasks), added, depthRefusals)
	}
}
