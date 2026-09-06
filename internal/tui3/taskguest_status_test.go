package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestGuestStatusBecomesHistoricalWhenItsLaneEnds(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	enterAway(t, a)
	ownerSays(t, a, session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, State: session.TaskRunning}})
	a.tookGuestNotice(taskGuestNoticeMsg{gen: a.room.gen, closed: true})
	if !a.roomGuestStale() {
		t.Fatal("a closed status lane still claims current status")
	}
	if !strings.Contains(roomText(a), roomGuestStaleWord) {
		t.Fatal("the page hides its loss of current status")
	}
}

func TestGuestKeepsTheOwnersStoppedState(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	enterAway(t, a)
	ownerSays(t, a, session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, State: session.TaskFailed, Stopped: true}})
	if !a.roomNode().stopped || !a.room.done {
		t.Fatal("the guest lost the owner's stopped state")
	}
	if a.tasks[7].stopped {
		t.Fatal("the guest stopped this conversation's task with the same id")
	}
}

func TestGuestIgnoresQueuedNoticesAfterItsOwnerIsGone(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	enterAway(t, a)
	a.room.guest.lost = true
	if cmd := ownerSays(t, a, session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, State: session.TaskDone}}); cmd != nil {
		t.Fatal("lost guest rearmed its status lane")
	}
	if a.roomNode().state != session.TaskRunning {
		t.Fatal("a queued notice changed a lost guest's last reading")
	}
}
