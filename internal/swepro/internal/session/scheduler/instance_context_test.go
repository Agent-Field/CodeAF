// This file covers instance binding at swe-pro/src/session/plandb-scheduler.ts:2471-4462.
package scheduler

import (
	"context"
	"sync"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/project"
)

type contextCapturingStepLoop struct {
	mu       sync.Mutex
	captured map[string]project.InstanceContext
}

func (s *contextCapturingStepLoop) RunLeaf(ctx context.Context, input LeafRunRequest) (LeafRunResult, error) {
	instance, ok := project.FromContext(ctx)
	if !ok {
		return LeafRunResult{ErrorName: "missing project instance context"}, nil
	}
	s.mu.Lock()
	s.captured[input.TaskID] = instance
	s.mu.Unlock()
	return LeafRunResult{Parts: []LeafPart{{Type: "text", Text: "done"}}}, nil
}

func TestCallLeafBindsIndependentPlanDBInstanceContexts(t *testing.T) {
	stepLoop := &contextCapturingStepLoop{captured: map[string]project.InstanceContext{}}
	scheduler := NewScheduler(SchedulerOptions{Workspace: "/repo", StepLoop: stepLoop})
	var wait sync.WaitGroup
	for _, taskID := range []string{"a", "b"} {
		taskID := taskID
		wait.Add(1)
		go func() {
			defer wait.Done()
			result := scheduler.callLeaf(context.Background(), LeafRunRequest{
				Workspace: "/repo", Worktree: "/repo/.plandb/wt-" + taskID,
				DBPath: "/repo/.plandb.db", ProjectID: "project", RootTaskID: "root",
				TaskID: taskID,
			}, taskID, "coder")
			if result.ErrorName != "" {
				t.Errorf("%s: %s", taskID, result.ErrorName)
			}
		}()
	}
	wait.Wait()
	for _, taskID := range []string{"a", "b"} {
		instance := stepLoop.captured[taskID]
		if instance.Directory != "/repo/.plandb/wt-"+taskID || instance.Worktree != "/repo" {
			t.Fatalf("%s context = %#v", taskID, instance)
		}
		if instance.PlanDB == nil || instance.PlanDB.TaskID != taskID ||
			instance.PlanDB.RootTaskID != "root" || instance.PlanDB.ProjectID != "project" {
			t.Fatalf("%s PlanDB context = %#v", taskID, instance.PlanDB)
		}
	}
}
