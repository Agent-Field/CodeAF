package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestTaskFooterFollowsTheWorkBeingRead(t *testing.T) {
	for _, width := range []int{44, 80, 120} {
		for _, state := range []session.TaskState{session.TaskRunning, session.TaskQueued, session.TaskDone, session.TaskFailed, session.TaskUnverified} {
			t.Run(fmt.Sprintf("%d/%s", width, state), func(t *testing.T) {
				a, _ := roomModelApp(t, "task-model")
				a.width = width
				node := a.roomNode()
				node.state = state
				// Deliberately disagree with the task, including a main-turn clock.
				a.state = stateWorking
				if state == session.TaskRunning {
					a.state = stateIdle
				}
				a.turnBegan = a.now().Add(-2 * time.Hour)
				want := a.roomStateWord(node)
				got, _ := a.stateSegment()
				if got != want {
					t.Fatalf("task footer = %q, want %q", got, want)
				}
				a.touch()
				if line := statusText(a); !strings.Contains(line, want) {
					t.Fatalf("visible footer missing task state %q: %q", want, line)
				}
				a.closeRoom()
				if got, _ := a.stateWord(); got != a.state.String() {
					t.Fatalf("leaving task did not restore main status: %q", got)
				}
			})
		}
	}
}

func TestTaskFooterKeepsPhaseAndKeyboardFeedback(t *testing.T) {
	a, _ := roomModelApp(t, "task-model")
	a.roomNode().doing = "awaiting your look"
	if got, _ := a.stateSegment(); got != "awaiting your look" {
		t.Fatalf("task phase lost: %q", got)
	}
	a.copy.on = true
	if got, _ := a.stateSegment(); got != a.copyWord() {
		t.Fatalf("copy keyboard feedback lost: %q", got)
	}
	a.copy.on = false
	a.roomNode().stopped = true
	if got, _ := a.stateSegment(); got != stoppingWord {
		t.Fatalf("stopped task still claims work: %q", got)
	}
	delete(a.tasks, a.room.id)
	if got, _ := a.stateSegment(); got != "reading" {
		t.Fatalf("unknown task borrowed main status: %q", got)
	}
}
