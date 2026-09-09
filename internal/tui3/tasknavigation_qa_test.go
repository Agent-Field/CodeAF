package tui3

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// Finishing the last running task must not hide the only visible way to read
// work that now needs the person's decision.
func TestTaskShortcutRemainsClickableWhenTheLastWorkerNeedsAttention(t *testing.T) {
	for _, width := range []int{44, 80, 99} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, _ := localTaskRoomLab(t)
			a.width = width
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskUnverified, session.TaskNotice{})})
			if !a.stripShowing() {
				t.Fatalf("the task shortcut disappeared when review became necessary:\n%s", plain(frame(a)))
			}
			if layoutTier(width) == tierPhone && !strings.Contains(stripText(a), "needs you") {
				t.Fatalf("phone shortcut hides the action needed: %q", stripText(a))
			}
			t.Logf("Before opening (%d columns):\n%s", width, plain(frame(a)))
			drive(t, a, tea.MouseClickMsg{X: 2, Y: a.headHeight(), Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{X: 2, Y: a.headHeight(), Button: tea.MouseLeft})
			if layoutTier(width) == tierPhone {
				if !a.at(pageTasks) {
					t.Fatal("phone shortcut did not open Tasks")
				}
				drive(t, a, key("down"), key("enter"))
			}
			if !a.roomOpen() || a.room.id != 7 || !strings.Contains(roomText(a), "Checking the parser now") {
				t.Fatal("click did not reach the task transcript")
			}
		})
	}
}

// On a small screen the summary must name a waiting decision even while an
// unrelated worker is still running.
func TestPhoneTaskShortcutPrioritizesAttentionOverRunning(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width = 44
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Review parser", session.TaskUnverified, session.TaskNotice{})}, streamEventMsg{gen: a.gen, ev: update(8, "Build docs", session.TaskRunning, session.TaskNotice{})})
	if text := stripText(a); !strings.Contains(text, "1 needs you") {
		t.Fatalf("a running worker hid the waiting decision: %q", text)
	}
}

// All room entry points share this constructor, including direct task-to-task
// navigation, so replacing a room owes the previous event source its release.
func TestSwitchingTaskRoomsReleasesThePreviousSubscription(t *testing.T) {
	a, _, _ := roomApp(t)
	a.openRoom(7, "Parser")
	stopped := 0
	a.room.stop = func() { stopped++ }
	a.openRoom(8, "Docs")
	if stopped != 1 {
		t.Fatalf("previous subscription released %d times, want once", stopped)
	}
	a.closeRoom()
	if stopped != 1 {
		t.Fatalf("the new room released the previous subscription again: %d", stopped)
	}
}
