package tui3

import (
	"errors"
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

// Opening a foreign task must not borrow either local draft, even when its id collides.
func TestGuestComposerKeepsConversationAndLocalTaskDraftsSeparate(t *testing.T) {
	a, _ := guestLab(t)
	a.input.setText("main conversation draft")
	a.openRoom(7, "Port the parser")
	a.input.setText("local task draft")
	enterAway(t, a)
	if !a.input.empty() || a.composerOwner != guestRecipient(theirLiveSession.Transcript, 7) {
		t.Fatalf("foreign task borrowed a local composer: owner=%+v text=%q", a.composerOwner, a.input.String())
	}
	a.input.setText("foreign page draft")
	a.closeRoom()
	if got := a.input.String(); got != "main conversation draft" {
		t.Fatalf("main draft = %q", got)
	}
	a.openRoom(7, "Port the parser")
	if got := a.input.String(); got != "local task draft" {
		t.Fatalf("local task draft = %q", got)
	}
}

func TestGuestFooterDoesNotCallLastKnownWorkRunning(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	enterAway(t, a)
	a.tookGuestNotice(taskGuestNoticeMsg{gen: a.room.gen, closed: true})
	if got, _ := a.stateWord(); got != "reading" {
		t.Fatalf("stale footer = %q", got)
	}
	a.room.guest.lost = true
	if got, _ := a.stateWord(); got != "reading" {
		t.Fatalf("lost footer = %q", got)
	}
}

func TestFinishedGuestStillLearnsThatItsOwnerWasReplaced(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	enterAway(t, a)
	ownerSays(t, a, session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, State: session.TaskDone}})
	first := a.farRoomPoll(a.room.gen)
	if first == nil {
		t.Fatal("finished guest stopped checking its owner")
	}
	if next := a.farRoomRead(first().(roomRecordMsg)); next == nil {
		t.Fatal("successful finished read did not rearm owner check")
	}
	a.room.guest.room = func(uint64, int) (session.TaskRecord, error) {
		return session.TaskRecord{}, errors.New(taskGuestGoneMark)
	}
	cmd := a.farRoomPoll(a.room.gen)
	if cmd == nil {
		t.Fatal("finished guest never read its replaced owner")
	}
	a.farRoomRead(cmd().(roomRecordMsg))
	if !a.room.guest.lost || !strings.Contains(roomText(a), taskGuestGoneWord) {
		t.Fatal("finished guest concealed owner replacement")
	}
	if cmd := a.farRoomPoll(a.room.gen); cmd != nil {
		t.Fatal("lost guest continued polling")
	}
}
