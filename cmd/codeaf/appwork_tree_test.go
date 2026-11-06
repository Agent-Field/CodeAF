package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// L-LOOP2 C7a, C7b, C8a: the view-to-wire mapper tasks.list uses keeps every
// parent id, the run's group and the ended state. The session listing that
// produces these views is covered in internal/session.
func TestAppTasksListPreservesRunSubtaskParents(t *testing.T) {
	const agent = "agent-1"
	views := []session.AppTaskView{
		{ID: "7", Title: "Run", State: "done", Group: "group-7"},
		{ID: "t-child", Parent: "7", Title: "Child", State: "stopped", Group: "group-7"},
		{ID: "t-grandchild", Parent: "t-child", Title: "Grandchild", State: "stopped", Group: "group-7"},
		{ID: "9", Parent: "8", Title: "Ordinary task", State: "done"},
		{ID: "10", Parent: "0", Title: "Top-level task", State: "done"},
	}
	byID := map[string]appwire.Task{}
	for _, view := range views {
		task := appTaskOfView(agent, view)
		byID[task.ID] = task
		if task.State == appwire.TaskRunning {
			t.Fatalf("ended task reads running: %+v", task)
		}
	}
	if got := byID["t-child"].Parent; got != "agent-1:7" {
		t.Fatalf("a child of the run root has parent %q, want the run row agent-1:7", got)
	}
	if got := byID["t-grandchild"].Parent; got != "t-child" {
		t.Fatalf("a grandchild has parent %q, want its parent row t-child", got)
	}
	if got := byID["agent-1:9"].Parent; got != "agent-1:8" {
		t.Fatalf("an ordinary numeric parent changed: %q", got)
	}
	if got := byID["agent-1:10"].Parent; got != "" {
		t.Fatalf("a zero parent became %q, want none", got)
	}
	for _, id := range []string{"agent-1:7", "t-child", "t-grandchild"} {
		if byID[id].Group != "group-7" {
			t.Fatalf("wire lost group: %+v", byID[id])
		}
	}
}
