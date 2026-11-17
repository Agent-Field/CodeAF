package factory

import (
	"strings"
	"testing"
)

// A checklist is the steps of one deliverable inside its task (org-space
// DESIGN §5.2): created from plain lines with ids c1…, ticked by id, replaced
// whole by a person, and never more than its limits.
func TestAChecklistIsCreatedFromLinesAndTickedById(t *testing.T) {
	task, err := NewTask("ws_pantry", "t1", TaskCreate{Title: "Draft the launch post", Request: "A post for Friday", DoneBy: TaskDoneBy{Kind: TaskByAfi},
		Checklist: []string{"Outline", "  First   draft ", "", "Check the numbers"}}, PersonRef{ID: "priya", Name: "priya"}, TaskOwner{Kind: TaskOwnerEveryone}, nil, taskNow)
	if err != nil {
		t.Fatal(err)
	}
	want := []TaskCheckItem{{ID: "c1", Text: "Outline"}, {ID: "c2", Text: "First draft"}, {ID: "c3", Text: "Check the numbers"}}
	if len(task.Checklist) != 3 || task.Checklist[0] != want[0] || task.Checklist[1] != want[1] || task.Checklist[2] != want[2] {
		t.Fatalf("checklist %+v", task.Checklist)
	}
	ch, err := ApplyPatch(task, TaskPatch{CheckDone: []string{"c1", "c3"}}, TaskActor{Handle: "priya", Edit: true}, byPriya, nil, taskIDs(), taskNow)
	if err != nil || !ch.Task.Checklist[0].Done || ch.Task.Checklist[1].Done || !ch.Task.Checklist[2].Done {
		t.Fatalf("ticked: %+v %v", ch.Task.Checklist, err)
	}
	if task.Checklist[0].Done {
		t.Fatal("ticking changed the task it was given")
	}
	ch, err = ApplyPatch(ch.Task, TaskPatch{CheckUndone: []string{"c3"}}, TaskActor{Handle: "priya", Edit: true}, byPriya, nil, taskIDs(), taskNow)
	if err != nil || ch.Task.Checklist[2].Done {
		t.Fatalf("unticked: %+v %v", ch.Task.Checklist, err)
	}
	if _, err := ApplyPatch(ch.Task, TaskPatch{CheckDone: []string{"c9"}}, TaskActor{Handle: "priya", Edit: true}, byPriya, nil, taskIDs(), taskNow); err == nil || !strings.Contains(err.Error(), `no line "c9"`) {
		t.Fatalf("a line that is not there was ticked: %v", err)
	}
	if _, err := ApplyPatch(ch.Task, TaskPatch{CheckDone: []string{"c1"}}, TaskActor{Handle: "abir"}, byPriya, nil, taskIDs(), taskNow); err == nil {
		t.Fatal("someone who only views ticked a line")
	}
}

func TestAPersonReplacesTheChecklistAndNewLinesTakeFreshIds(t *testing.T) {
	task := personTask("t1", "priya")
	task.Checklist = []TaskCheckItem{{ID: "c1", Text: "Outline", Done: true}, {ID: "c2", Text: "Draft"}}
	list := []TaskCheckItem{{ID: "c2", Text: "Draft"}, {Text: "Proofread"}, {ID: "c1", Text: "Outline", Done: true}}
	ch, err := ApplyPatch(task, TaskPatch{Checklist: &list}, TaskActor{Handle: "priya", Edit: true}, byPriya, nil, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	got := ch.Task.Checklist
	if len(got) != 3 || got[0].ID != "c2" || got[1] != (TaskCheckItem{ID: "c3", Text: "Proofread"}) || !got[2].Done {
		t.Fatalf("replaced: %+v", got)
	}
	empty := []TaskCheckItem{}
	if ch, err := ApplyPatch(task, TaskPatch{Checklist: &empty}, TaskActor{Handle: "priya", Edit: true}, byPriya, nil, taskIDs(), taskNow); err != nil || ch.Task.Checklist != nil {
		t.Fatalf("an emptied checklist is no checklist: %+v %v", ch.Task.Checklist, err)
	}
}

func TestAChecklistKeepsItsLimits(t *testing.T) {
	long := make([]TaskCheckItem, TaskChecklistMost+1)
	for i := range long {
		long[i].Text = "step"
	}
	if _, err := cleanChecklist(long); err == nil {
		t.Fatal("a checklist past its most was kept")
	}
	if _, err := cleanChecklist([]TaskCheckItem{{Text: strings.Repeat("x", TaskChecklistLineMost+1)}}); err == nil {
		t.Fatal("a line past its most was kept")
	}
	if _, err := cleanChecklist([]TaskCheckItem{{ID: "c1", Text: "a"}, {ID: "c1", Text: "b"}}); err == nil {
		t.Fatal("two lines shared an id")
	}
}
