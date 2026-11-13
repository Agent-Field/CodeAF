package tui3

// A task on either engine opens the same room with the same two tabs. A line
// typed into a run task room reaches its store as a note.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A task on either engine opens the same room with the same two tabs. A line
// typed into a run task room reaches its store as a note.
func roomTrailText(t *testing.T, a *app) string {
	t.Helper()
	frame, _, _ := a.frame()
	rows := strings.Split(plain(frame), "\n")
	at := a.roomHeadRow()
	if at < 0 || at >= len(rows) {
		t.Fatalf("no trail row at %d in a frame of %d rows", at, len(rows))
	}
	return rows[at]
}

func TestANewEngineTaskAndAnOlderEngineTaskOpenTheSameRoomWithTheSameTabs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stored bool
		work   string
	}{
		// A task on either engine opens the same room with the same two tabs. A line
		// typed into a run task room reaches its store as a note.
		{name: "new engine", stored: true, work: "this engine does not read the run's working copy"},
		// A task on either engine opens the same room with the same two tabs. A line
		// typed into a run task room reaches its store as a note.
		{name: "older engine", stored: false, work: "the files this task changes are listed here when it lands"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := railTaskPageApp(t, tc.stored)
			clickRail(t, a, 0)
			if !a.roomOpen() {
				t.Fatalf("a press on the %s task's row opened no task room", tc.name)
			}
			trail := roomTrailText(t, a)
			for _, word := range []string{"transcript", "work"} {
				if !strings.Contains(trail, word) {
					t.Fatalf("the %s task's room has no %q tab on its trail row:\n%s", tc.name, word, trail)
				}
			}
			drive(t, a, tea.KeyPressMsg{Code: tea.KeyTab})
			if !a.roomOpen() {
				t.Fatalf("tab over an empty box left the %s task's room", tc.name)
			}
			frame, _, _ := a.frame()
			if !strings.Contains(plain(frame), tc.work) {
				t.Fatalf("tab did not open the %s task's work tab (want %q):\n%s", tc.name, tc.work, plain(frame))
			}
		})
	}
}

func TestAMessageTypedIntoANewEngineTasksRoomReachesItsStoreAsANote(t *testing.T) {
	a, fake := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	if !a.roomOpen() {
		t.Fatal("a press on a run's task's row opened no task room")
	}
	typeText(t, a, "use the fast parser")
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(fake.noted) != 1 || fake.noted[0].id != "2" || fake.noted[0].text != "use the fast parser" {
		t.Fatalf("the line did not reach the store as one note on task 2: %+v", fake.noted)
	}
	if len(fake.sent) != 0 {
		t.Fatalf("a note typed into a task's room started a chat turn: %q", fake.sent)
	}
	if !a.roomOpen() {
		t.Fatal("sending the note left the room")
	}
	frame, _, _ := a.frame()
	if !strings.Contains(plain(frame), "use the fast parser") {
		t.Fatalf("the note is not on the room it was typed into:\n%s", plain(frame))
	}
}
