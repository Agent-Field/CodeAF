package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
)

type shellGuardTestHarness struct {
	registry  *Registry
	active    *MemoryPlanDBActiveStore
	ctx       context.Context
	call      steploop.ToolCall
	workspace string
	projectID string
	rootID    plandb.TaskID
}

func newShellGuardTestHarness(t *testing.T, command string) shellGuardTestHarness {
	t.Helper()
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	project := db.Init("guard-close")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: project.ID, CustomID: "t-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	active := &MemoryPlanDBActiveStore{}
	registry := NewWithOptions(workspace, RegistryOptions{PlanDBActive: active})
	message := "Project: " + string(project.ID) + "\nRoot task: " + string(root.ID)
	ctx := steploop.WithToolMessages(context.Background(), []msgmodel.WithParts{{
		Parts: msgmodel.Parts{msgmodel.TextPart{Text: message}},
	}})
	input, err := json.Marshal(bashInput{Command: command})
	if err != nil {
		t.Fatal(err)
	}
	return shellGuardTestHarness{
		registry: registry, active: active, ctx: ctx, workspace: workspace,
		projectID: string(project.ID), rootID: root.ID,
		call: steploop.ToolCall{
			Name: "bash", Input: input, SessionID: "ses-guard-close",
			MessageID: "msg-guard-close", Agent: "root-orchestrator",
		},
	}
}

func (h shellGuardTestHarness) directPackages(t *testing.T) []*plandb.Task {
	t.Helper()
	var packages []*plandb.Task
	for _, task := range plandb.GetPlanDB().ListTasks(&plandb.ListTasksFilter{Project: h.projectID}) {
		if task.Description != nil && IsHarnessDirectPackageDescription(*task.Description) {
			packages = append(packages, task)
		}
	}
	return packages
}

func assertClosedDirectPackage(t *testing.T, h shellGuardTestHarness, wantResult string) *plandb.Task {
	t.Helper()
	packages := h.directPackages(t)
	if len(packages) == 0 {
		t.Fatal("guard did not create a direct package")
	}
	task := packages[len(packages)-1]
	if task.Status != plandb.StatusDone || !strings.Contains(string(task.Result), wantResult) {
		t.Fatalf("direct package = %#v, result = %s", task, task.Result)
	}
	if active := h.active.Get(h.call.SessionID, h.workspace); active != nil {
		t.Fatalf("active binding was not cleared: %#v", active)
	}
	return task
}

func TestBashClosesCreatedPlanDBShellPackageWithObservedExitCode(t *testing.T) {
	for _, test := range []struct {
		name    string
		command string
		want    string
	}{
		{name: "exit zero", command: "printf success # go test ./...", want: "Direct command completed: exit=0"},
		{name: "exit non-zero", command: "exit 7 # go test ./...", want: "Direct command completed: exit=7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newShellGuardTestHarness(t, test.command)
			if _, err := h.registry.Execute(h.ctx, h.call); err != nil {
				t.Fatal(err)
			}
			assertClosedDirectPackage(t, h, test.want)
		})
	}
}

func TestBashClosesCreatedPlanDBShellPackageOnMemoCacheHit(t *testing.T) {
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	h := newShellGuardTestHarness(t, "printf memo-hit # go test ./...")
	if _, err := h.registry.Execute(h.ctx, h.call); err != nil {
		t.Fatal(err)
	}
	result, err := h.registry.Execute(h.ctx, h.call)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "[codeaf: cached") {
		t.Fatalf("second result was not served from the test memo: %q", result.Output)
	}
	if packages := h.directPackages(t); len(packages) != 2 {
		t.Fatalf("direct package count = %d, want one closed package per call", len(packages))
	}
	assertClosedDirectPackage(t, h, "Direct command completed: exit=0")
}

func TestBashClosesCreatedPlanDBShellPackageWithoutObservedExit(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		h := newShellGuardTestHarness(t, "sleep 30 # go test ./...")
		timeout := 25
		h.call.Input, _ = json.Marshal(bashInput{Command: "sleep 30 # go test ./...", TimeoutMS: &timeout})
		if _, err := h.registry.Execute(h.ctx, h.call); err != nil {
			t.Fatal(err)
		}
		assertClosedDirectPackage(t, h, "Direct command did not complete")
	})

	t.Run("context aborted", func(t *testing.T) {
		h := newShellGuardTestHarness(t, "sleep 30 # go test ./...")
		ctx, cancel := context.WithCancel(h.ctx)
		timer := time.AfterFunc(25*time.Millisecond, cancel)
		t.Cleanup(func() {
			timer.Stop()
			cancel()
		})
		if _, err := h.registry.Execute(ctx, h.call); !errors.Is(err, context.Canceled) {
			t.Fatalf("Execute error = %v, want context cancellation", err)
		}
		assertClosedDirectPackage(t, h, "Direct command did not complete")
	})
}

func TestBashDoesNotCloseReusedPlanDBShellPackage(t *testing.T) {
	h := newShellGuardTestHarness(t, "printf reused # go test ./...")
	description := "Planner-authored QA task.\n\ntask_role: qa\naccess: read"
	task, err := plandb.GetPlanDB().AddTask(plandb.AddTaskInput{
		Title: "planned QA", Description: &description, Project: h.projectID,
		Parent: h.rootID, CustomID: "t-planned-qa", Kind: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.registry.Execute(h.ctx, h.call); err != nil {
		t.Fatal(err)
	}
	got := plandb.GetPlanDB().GetTask(task.ID)
	if got == nil || got.Status != plandb.StatusRunning {
		t.Fatalf("reused task was auto-closed: %#v", got)
	}
	active := h.active.Get(h.call.SessionID, h.workspace)
	if active == nil || active.TaskID != string(task.ID) {
		t.Fatalf("reused active binding was cleared: %#v", active)
	}
}

func TestMemoryPlanDBActiveStoreClearRequiresMatchingTask(t *testing.T) {
	store := &MemoryPlanDBActiveStore{}
	store.Set("ses", "/workspace", PlanDBActiveTask{TaskID: "t-current"})
	store.Clear("ses", "/workspace", "t-different")
	if got := store.Get("ses", "/workspace"); got == nil || got.TaskID != "t-current" {
		t.Fatalf("mismatched clear changed active task: %#v", got)
	}
	store.Clear("ses", "/workspace", "t-current")
	if got := store.Get("ses", "/workspace"); got != nil {
		t.Fatalf("matching clear left active task: %#v", got)
	}
	store.Set("ses", "/workspace", PlanDBActiveTask{TaskID: "t-current"})
	store.Clear("ses", "/workspace", "")
	if got := store.Get("ses", "/workspace"); got != nil {
		t.Fatalf("unconditional clear left active task: %#v", got)
	}
}

func TestIsHarnessDirectPackageDescriptionRequiresExactFirstLine(t *testing.T) {
	if !IsHarnessDirectPackageDescription("Harness-created direct verification package.\n\ntask_role: qa") {
		t.Fatal("exact harness verification marker was not recognized")
	}
	if IsHarnessDirectPackageDescription("Planner-written task.\nHarness-created direct verification package.\nMention only.") {
		t.Fatal("later harness wording was misclassified as a direct package")
	}
}
