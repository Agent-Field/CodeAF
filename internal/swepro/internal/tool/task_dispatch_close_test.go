package tool

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/project"
)

// Contract (run-T/run-U class): a failed subagent dispatch must never leave
// its PlanDB bookkeeping package running — the root drain would stall on it.
// A package this call created closes with an honest failure result; a reused
// open planner task is released for the scheduler, never auto-closed.
func TestTaskDispatchErrorClosesCreatedPackage(t *testing.T) {
	var commands [][]string
	registry := NewWithOptions(t.TempDir(), RegistryOptions{
		PlanRun: func(argv []string) plandb.RunResult {
			commands = append(commands, append([]string(nil), argv...))
			if len(argv) > 1 && argv[1] == "list" {
				return plandb.RunResult{Code: 0, Stdout: []byte(`[]`)}
			}
			body, _ := json.Marshal(map[string]any{"id": "t-pkg"})
			return plandb.RunResult{Code: 0, Stdout: body}
		},
		TaskSpawner:   &fakeTaskSpawner{err: errors.New("spawn failed")},
		TaskSessionID: func() string { return "ses-child" },
	})
	ctx := project.WithContext(context.Background(), project.InstanceContext{
		Directory: registry.workDir, Worktree: registry.workDir,
		PlanDB: &project.PlanDB{ProjectID: "p-x", RootTaskID: "t-root", TaskID: "t-root"},
	})
	_, err := registry.Execute(ctx, steploop.ToolCall{
		Name: "task", SessionID: "ses-parent",
		Input: json.RawMessage(`{"description":"Audit completion","prompt":"check","subagent_type":"explorer"}`),
	})
	if err == nil {
		t.Fatal("dispatch error did not propagate")
	}
	want := []string{
		"plandb", "done", "t-pkg", "--agent", "explorer:ses-child",
		"--result", "Subagent dispatch failed: spawn failed", "--json",
	}
	found := false
	for _, argv := range commands {
		if reflect.DeepEqual(argv, want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no closing done command; commands = %v", commands)
	}
}

func TestTaskDispatchErrorReleasesReusedPlannerTask(t *testing.T) {
	var commands [][]string
	reservationSeenBeforeClaim := false
	var registry *Registry
	registry = NewWithOptions(t.TempDir(), RegistryOptions{
		PlanRun: func(argv []string) plandb.RunResult {
			commands = append(commands, append([]string(nil), argv...))
			if len(argv) > 1 && argv[1] == "list" {
				body, _ := json.Marshal([]map[string]any{
					{"id": "t-reuse", "title": "Audit completion", "status": "ready"},
				})
				return plandb.RunResult{Code: 0, Stdout: body}
			}
			if len(argv) > 2 && argv[1] == "task" && argv[2] == "claim" {
				reservationSeenBeforeClaim = reflect.DeepEqual(registry.GuardInFlightTaskIDs(), []string{"t-reuse"})
			}
			body, _ := json.Marshal(map[string]any{"id": "t-reuse"})
			return plandb.RunResult{Code: 0, Stdout: body}
		},
		TaskSpawner:   &fakeTaskSpawner{err: errors.New("spawn failed")},
		TaskSessionID: func() string { return "ses-child" },
	})
	ctx := project.WithContext(context.Background(), project.InstanceContext{
		Directory: registry.workDir, Worktree: registry.workDir,
		PlanDB: &project.PlanDB{ProjectID: "p-x", RootTaskID: "t-root", TaskID: "t-root"},
	})
	_, err := registry.Execute(ctx, steploop.ToolCall{
		Name: "task", SessionID: "ses-parent",
		Input: json.RawMessage(`{"description":"Audit completion","prompt":"check","subagent_type":"explorer"}`),
	})
	if err == nil {
		t.Fatal("dispatch error did not propagate")
	}
	release := []string{"plandb", "task", "release", "t-reuse", "--json"}
	releaseSeen := false
	for _, argv := range commands {
		if reflect.DeepEqual(argv, release) {
			releaseSeen = true
		}
		if len(argv) > 1 && argv[1] == "done" {
			t.Fatalf("reused planner task auto-closed: %v", argv)
		}
	}
	if !releaseSeen {
		t.Fatalf("reused task not released; commands = %v", commands)
	}
	if !reservationSeenBeforeClaim {
		t.Fatal("reused task was not reserved before claim/start")
	}
}

// Contract: the bookkeeping package is registered as in-flight guard work for
// the whole dispatch, so a concurrent drain sweep can neither close nor
// release it mid-dispatch (the run-N race class, task-tool edition).
func TestTaskDispatchHoldsGuardInFlightReservation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var observed []string
	reservationSeenBeforeClaim := false
	var registry *Registry
	registry = NewWithOptions(t.TempDir(), RegistryOptions{
		PlanRun: func(argv []string) plandb.RunResult {
			if len(argv) > 1 && argv[1] == "list" {
				return plandb.RunResult{Code: 0, Stdout: []byte(`[]`)}
			}
			if len(argv) > 2 && argv[1] == "task" && argv[2] == "claim" {
				reservationSeenBeforeClaim = reflect.DeepEqual(registry.GuardInFlightTaskIDs(), []string{"t-pkg"})
			}
			body, _ := json.Marshal(map[string]any{"id": "t-pkg"})
			return plandb.RunResult{Code: 0, Stdout: body}
		},
		TaskSpawner:   blockingTaskSpawner{started: started, release: release},
		TaskSessionID: func() string { return "ses-child" },
	})
	ctx := project.WithContext(context.Background(), project.InstanceContext{
		Directory: registry.workDir, Worktree: registry.workDir,
		PlanDB: &project.PlanDB{ProjectID: "p-x", RootTaskID: "t-root", TaskID: "t-root"},
	})
	done := make(chan error, 1)
	go func() {
		_, err := registry.Execute(ctx, steploop.ToolCall{
			Name: "task", SessionID: "ses-parent",
			Input: json.RawMessage(`{"description":"Audit completion","prompt":"check","subagent_type":"explorer"}`),
		})
		done <- err
	}()
	<-started
	observed = registry.GuardInFlightTaskIDs()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(observed) != 1 || observed[0] != "t-pkg" {
		t.Fatalf("in-flight ids during dispatch = %v, want [t-pkg]", observed)
	}
	if !reservationSeenBeforeClaim {
		t.Fatal("created task was not reserved before claim/start")
	}
	if after := registry.GuardInFlightTaskIDs(); len(after) != 0 {
		t.Fatalf("in-flight ids after dispatch = %v, want none", after)
	}
}

type blockingTaskSpawner struct {
	started chan struct{}
	release chan struct{}
}

func (s blockingTaskSpawner) SpawnTask(_ context.Context, _ TaskSpawnRequest) (TaskSpawnResult, error) {
	close(s.started)
	<-s.release
	return TaskSpawnResult{SessionID: "ses-child", ResultText: "ok"}, nil
}
