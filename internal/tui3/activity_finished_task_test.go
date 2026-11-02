package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAFinishedTaskAppearsOnceInActivity(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "native"}[native], func(t *testing.T) {
			now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
			root := filepath.Join(t.TempDir(), "chat-one")
			file := filepath.Join(root, "transcript.jsonl")
			if native {
				file = filepath.Join(root, ".cell", "transcript.jsonl")
			}
			a := newTestApp(&fakeAgent{})
			a.file, a.title = file, "background task creating hello file"
			a.clock = func() time.Time { return now }
			a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
				ID: 1, Title: "Create hello-task.txt", State: session.TaskDone,
				PlanTask: "t-1", EndedAt: now,
			}})
			entry := session.TaskIndexEntry{SessionID: "chat-one", ID: "1", Title: "Create hello-task.txt", Label: "Create hello-task.txt", Status: string(session.TaskDone), EndedAt: now}
			row := session.SessionRow{ID: "chat-one", Transcript: file, Title: a.title, Project: "ws9", ProjectDir: "/project/ws9", At: now, Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{entry}, Done: 1}}
			world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}
			entry.Status = string(session.TaskRunning)
			a.comp.tasks = []session.TaskIndexEntry{entry}
			mine := a.taskSheetMine()
			mine.plan = []session.PlanTaskRow{{ID: "t-1", Title: entry.Title, Status: "done", Ended: now, Steps: 2, USD: .0013}}
			reading := readTasks(world, mine, session.LastDays(now, 14), tasksSort{}, time.Time{}, now)
			page := tasksPage(reading, 140)
			t.Logf("chats=%d tasks=%d\n%s", len(reading.tree().groups), len(reading.items), page)
			if len(reading.tree().groups) != 1 || len(reading.items) != 1 || reading.items[0].status().Word != "done" || strings.Contains(page, "incomplete") {
				t.Fatal("one finished task must appear under one chat and say done")
			}
		})
	}
}

func TestTheRailKeepsOnlyActiveWork(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 120, 40
	a.file = "/chats/chat-one/.cell/transcript.jsonl"
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
		ID: 1, Title: "Create hello-task.txt", State: session.TaskDone, PlanTask: "t-1",
	}})
	view := strings.Join(railText(a, a.viewHeight()), "\n")
	if strings.Contains(view, "hello-task") {
		t.Fatalf("finished run occupies the active sidebar: %s", view)
	}
	if len(a.tasks) != 1 || a.tasks[1].state != session.TaskDone {
		t.Fatal("finished run was discarded rather than kept as history")
	}
	t.Log("finished run is replayed and retained; the active sidebar omits it")
}
