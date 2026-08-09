package tool

import (
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/plandb"
)

func waitForInFlight(t *testing.T, registry *Registry, want int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ids := registry.GuardInFlightTaskIDs()
		if len(ids) == want {
			return ids
		}
		if time.Now().After(deadline) {
			t.Fatalf("guard in-flight ids = %v, want %d entries", ids, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Contract: while a guard-bound bash command executes, the registry reports
// the bound task as in-flight and the package is still open; once the tool
// returns, the reservation is gone and the created package is closed by the
// tool (never by anything else mid-command).
func TestGuardBoundCommandIsInFlightUntilToolReturn(t *testing.T) {
	h := newShellGuardTestHarness(t, "sleep 1 # go test ./...")
	done := make(chan error, 1)
	go func() {
		_, err := h.registry.Execute(h.ctx, h.call)
		done <- err
	}()
	ids := waitForInFlight(t, h.registry, 1)
	packages := h.directPackages(t)
	if len(packages) != 1 || string(packages[0].ID) != ids[0] {
		t.Fatalf("in-flight ids %v do not match created package %#v", ids, packages)
	}
	if packages[0].Status != plandb.StatusRunning {
		t.Fatalf("package closed mid-command: %#v", packages[0])
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	waitForInFlight(t, h.registry, 0)
	assertClosedDirectPackage(t, h, "Direct command completed: exit=0")
}

// Contract: a reused planner task is reserved for the duration of the command
// and left open afterwards — release is the drain's job, only once no tool
// call is executing against it.
func TestReusedPlannerTaskIsInFlightButNeverAutoClosed(t *testing.T) {
	h := newShellGuardTestHarness(t, "sleep 1 # go test ./...")
	description := "Planner-authored QA task.\n\ntask_role: qa\naccess: read"
	task, err := plandb.GetPlanDB().AddTask(plandb.AddTaskInput{
		Title: "planned QA", Description: &description, Project: h.projectID,
		Parent: h.rootID, CustomID: "t-planned-qa", Kind: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, execErr := h.registry.Execute(h.ctx, h.call)
		done <- execErr
	}()
	ids := waitForInFlight(t, h.registry, 1)
	if ids[0] != string(task.ID) {
		t.Fatalf("in-flight ids %v, want reused planner task %s", ids, task.ID)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	waitForInFlight(t, h.registry, 0)
	if got := plandb.GetPlanDB().GetTask(task.ID); got == nil || got.Status != plandb.StatusRunning {
		t.Fatalf("reused planner task was auto-closed: %#v", got)
	}
}
