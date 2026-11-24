package serve

import "github.com/Agent-Field/codeaf/internal/factory"

// nameTasks writes onto each task the name its people joined the organization
// with (L1 F17, L3 F-14), the way a workstream already names its own people. A
// task made from a desk names its owner and doer by handle alone, and a
// hand-off names the new owner by handle, so a desk read "Owner undefined
// (you)" and "abir" beside "Abir Das". A name that is empty or only the handle
// is replaced; a person the organization knows no name for keeps their handle.
// The stored task is not rewritten: the name is the organization's, read when
// the task is.
func nameTasks(nameOf func(string) string, tasks []factory.WorkstreamTask) {
	better := func(id, name string) string {
		if id == "" || (name != "" && name != id) {
			return name
		}
		return nameOf(id)
	}
	for i := range tasks {
		t := &tasks[i]
		if t.Owner.Kind == factory.TaskOwnerPerson {
			t.Owner.Name = better(t.Owner.ID, t.Owner.Name)
		}
		if t.DoneBy.Kind == factory.TaskByPerson {
			t.DoneBy.Name = better(t.DoneBy.ID, t.DoneBy.Name)
		}
		if t.CameFrom.Kind == factory.TaskByPerson {
			t.CameFrom.Name = better(t.CameFrom.ID, t.CameFrom.Name)
		}
		t.CreatedBy.Name = better(t.CreatedBy.ID, t.CreatedBy.Name)
	}
}

// nameTask is nameTasks for one task.
func nameTask(nameOf func(string) string, t factory.WorkstreamTask) factory.WorkstreamTask {
	one := []factory.WorkstreamTask{t}
	nameTasks(nameOf, one)
	return one[0]
}
