package tui3

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"strings"
	"testing"
)

func TestConversationStoppedTaskNeverSaysFinished(t *testing.T) {
	a := statusApp(t)
	for _, stopped := range []bool{true, false} {
		a.dropTasks()
		notice := session.TaskNotice{ID: 17, Title: "Session stop", State: session.TaskRunning}
		a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &notice})
		notice.State = session.TaskDone
		if stopped {
			notice.State = session.TaskFailed
			notice.Stopped = true
			notice.Ending = session.TaskEndingStopped
		}
		a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &notice})
		a.room = &taskRoom{id: 17}
		want := "done"
		if stopped {
			want = "stopped"
		}
		if got := a.taskStatus(a.tasks[17]).Word; got != want {
			t.Fatalf("task bar says %q, want %q", got, want)
		}
		for _, line := range []string{a.roomFinishedRefusal().line(), a.roomDoneRefusal().line()} {
			if stopped && (!strings.Contains(line, "stopped") || strings.Contains(line, "finished")) {
				t.Fatalf("stopped room says %q", line)
			}
			if !stopped && !strings.Contains(line, "finished") {
				t.Fatalf("completed room says %q", line)
			}
		}
		// An identical notice cannot change the meaning of the ending.
		a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &notice})
		if got := a.taskStatus(a.tasks[17]).Word; got != want {
			t.Fatalf("repeat notice changed status to %q", got)
		}
		a.room.guest = &taskGuest{node: a.tasks[17], owner: "Another conversation"}
		guestLine := a.roomDoneRefusal().line()
		if stopped && (!strings.Contains(guestLine, "stopped") || strings.Contains(guestLine, "finished")) {
			t.Fatalf("guest stopped page says %q", guestLine)
		}
		a.room = nil
	}
}

func TestConversationTaskTotalSeparatesStoppedFromDone(t *testing.T) {
	a := statusApp(t)
	a.tasks = map[uint64]*taskNode{
		1: {id: 1, title: "Stopped parent", state: session.TaskFailed, stopped: true},
		2: {id: 2, parent: "1", title: "Stopped child", state: session.TaskFailed, ending: session.TaskEndingStopped},
		3: {id: 3, title: "Completed sibling", state: session.TaskDone},
	}
	a.taskOrder = []uint64{1, 2, 3}
	rows, _ := a.railFootRows(100, 40)
	text := plain(strings.Join(rows, "\n"))
	if !strings.Contains(text, "2 stopped") || !strings.Contains(text, "1 done") || strings.Contains(text, "3 done") {
		t.Fatalf("wrong task totals: %s", text)
	}
	delete(a.tasks, 3)
	a.taskOrder = []uint64{1, 2}
	rows, _ = a.railFootRows(100, 40)
	text = plain(strings.Join(rows, "\n"))
	if !strings.Contains(text, "2 stopped") || strings.Contains(text, "done") {
		t.Fatalf("stopped tasks counted as done: %s", text)
	}
}
