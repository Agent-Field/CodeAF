package tool

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
)

func TestRegisteredPlanDBExecutesWithTypedParentContract(t *testing.T) {
	// Validation contract C1: the model-visible plandb definition reaches the
	// working ExecutePlanDB path and scopes adds under the typed assigned task.
	var commands [][]string
	registry := NewWithOptions(t.TempDir(), RegistryOptions{
		PlanRun: func(argv []string) plandb.RunResult {
			commands = append(commands, append([]string(nil), argv...))
			body, _ := json.Marshal(map[string]any{"id": "child"})
			return plandb.RunResult{Code: 0, Stdout: body}
		},
	})
	ctx := project.WithContext(context.Background(), project.InstanceContext{
		Directory: registry.workDir, Worktree: registry.workDir,
		PlanDB: &project.PlanDB{ProjectID: "project", RootTaskID: "root", TaskID: "parent"},
	})
	result, err := registry.Execute(ctx, steploop.ToolCall{
		Name: "plandb", Input: json.RawMessage(`{"op":"add","title":"child"}`), Agent: "root-orchestrator",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"plandb", "add", "child", "--json", "--parent", "parent",
		"--description", "context_inputs: parent",
	}
	if len(commands) != 1 || !reflect.DeepEqual(commands[0], want) {
		t.Fatalf("commands = %v, want %v", commands, want)
	}
	if result.Title != "plandb add" || result.Output == "" {
		t.Fatalf("result = %+v", result)
	}
}
