package tool

import (
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/plandb"
)

func TestExecutePlanDBIDKeyedEndToEnd(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	if result := plandb.RunPlanDB([]string{"plandb", "init", "demo"}); result.Code != 0 {
		t.Fatalf("init: %s", result.Stderr)
	}
	result, err := ExecutePlanDB(PlanDBParams{
		Op: "add_many", Project: "demo",
		Tasks: []PlanTaskInput{
			{ID: "root", Title: "root", Kind: "code"},
			{ID: "child", Title: "child", Kind: "test", Deps: []PlanDepRef{{TaskID: "root", Bare: true}}},
		},
	}, "", plandb.RunPlanDB)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || !strings.Contains(result.Output, `"created": 2`) {
		t.Fatalf("result = %+v", result)
	}
	list := plandb.RunPlanDB([]string{"plandb", "list", "--project", "demo", "--json"})
	if list.Code != 0 || strings.Count(string(list.Stdout), `"id"`) != 2 {
		t.Fatalf("list = %s / %s", list.Stdout, list.Stderr)
	}
}

func TestPlanDBGuardMutationAndVerificationPackages(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	plandb.RunPlanDB([]string{"plandb", "init", "demo"})
	root := plandb.RunPlanDB([]string{
		"plandb", "add", "root", "--json", "--project", "demo",
		"--description", "Root issue body",
	})
	rootObject := parsePlanJSON(root.Stdout)
	rootID, _ := rootObject["id"].(string)
	projectID, _ := rootObject["project_id"].(string)
	ctx := PlanDBGuardContext{
		Messages: []string{
			"Project: " + projectID + "\nRoot task: " + rootID,
		},
		SessionID: "ses", MessageID: "msg", Agent: "worker",
		Directory: t.TempDir(), Worktree: t.TempDir(),
	}
	active := &MemoryPlanDBActiveStore{}
	binding := EnsurePlanDBMutationPackage(ctx, []string{"src/a.go"}, plandb.RunPlanDB, active)
	if binding == nil || !binding.Created {
		t.Fatalf("mutation binding = %+v", binding)
	}
	// The active mutation package wins for verification, matching guard order.
	verify := EnsurePlanDBShellPackage(ctx, "go test ./...", plandb.RunPlanDB, active)
	if verify == nil || verify.Created || verify.TaskID != binding.TaskID {
		t.Fatalf("verification binding = %+v, mutation = %+v", verify, binding)
	}
	if got := EnsurePlanDBShellPackage(ctx, "echo hello", plandb.RunPlanDB, active); got != nil {
		t.Fatalf("non-verification binding = %+v", got)
	}
}

func TestContextsTaskFilterBugRemainsInCLIBridge(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	plandb.RunPlanDB([]string{"plandb", "init", "demo"})
	first := plandb.RunPlanDB([]string{"plandb", "add", "first", "--json", "--project", "demo"})
	second := plandb.RunPlanDB([]string{"plandb", "add", "second", "--json", "--project", "demo"})
	firstID, _ := parsePlanJSON(first.Stdout)["id"].(string)
	secondID, _ := parsePlanJSON(second.Stdout)["id"].(string)
	plandb.RunPlanDB([]string{"plandb", "context", "one", "--task", firstID, "--project", "demo", "--json"})
	plandb.RunPlanDB([]string{"plandb", "context", "two", "--task", secondID, "--project", "demo", "--json"})
	result := plandb.RunPlanDB([]string{"plandb", "contexts", "--task", firstID, "--json"})
	if strings.Count(string(result.Stdout), `"content"`) != 2 {
		t.Fatalf("contexts --task unexpectedly filtered: %s", result.Stdout)
	}
}
