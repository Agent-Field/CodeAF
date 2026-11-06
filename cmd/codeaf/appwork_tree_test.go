package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// L-LOOP2 C7a, C7b, C8a: tasks.list preserves every parent id and ended state.
func TestAppTasksListPreservesRunSubtaskParents(t *testing.T) {
	conv := &fakeConversation{appTasks: []session.AppTaskView{
		{ID: "7", Title: "Run", State: "done", Group: "group-7"},
		{ID: "t-child", Parent: "7", Title: "Child", State: "stopped", Group: "group-7"},
		{ID: "t-grandchild", Parent: "t-child", Title: "Grandchild", State: "stopped", Group: "group-7"},
		{ID: "9", Parent: "8", Title: "Ordinary task", State: "done"},
	}}
	backend, _, agent := workBackend(t, conv)
	listed, err := backend.ListTasks(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]appwire.Task{}
	for _, task := range listed {
		byID[task.ID] = task
		if task.State == appwire.TaskRunning {
			t.Fatalf("ended task reads running: %+v", task)
		}
	}
	if byID["t-child"].Parent != agent.ID+":7" || byID["t-grandchild"].Parent != "t-child" || byID[agent.ID+":9"].Parent != agent.ID+":8" {
		t.Fatalf("wire parent does not match its listed row: %+v", listed)
	}
	for _, id := range []string{agent.ID + ":7", "t-child", "t-grandchild"} {
		if byID[id].Group != "group-7" {
			t.Fatalf("wire lost group: %+v", byID[id])
		}
	}
}
