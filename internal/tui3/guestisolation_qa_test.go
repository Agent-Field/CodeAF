package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"testing"
)

func TestLocalDesignProgressNeverWritesOntoAGuestTaskWithTheSameNumber(t *testing.T) {
	a, _ := guestLab(t)
	enterAway(t, a)
	a.beginHarnessCard(session.Event{Kind: session.EventHarnessDesign, ID: 12, Task: &session.TaskNotice{ID: 7}})
	a.room.harnessProgress = "the guest's own progress"
	a.progressHarnessRoom(session.Event{Kind: session.EventHarnessProgress, ID: 12, Phase: "designing", Hint: "local work"})
	if a.room.harnessProgress != "the guest's own progress" {
		t.Fatal("local progress overwrote the guest's page")
	}
	a.finishHarnessCard(session.Event{Kind: session.EventHarnessDesign, ID: 12, Harness: &subharness.Harness{}})
	if a.room.harnessProgress != "the guest's own progress" {
		t.Fatal("local completion cleared the guest's progress")
	}
}

func TestForwardFromAGuestOpensTheLocalTaskEvenWhenNumbersMatch(t *testing.T) {
	a, _ := guestLab(t)
	enterAway(t, a)
	a.navForward()
	if a.room == nil || a.roomIsGuest() || a.room.id != 7 {
		t.Fatal("forward treated the guest as the local task and opened nothing")
	}
}
