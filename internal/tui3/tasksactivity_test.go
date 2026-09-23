package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func TestTasksMoveWholeTreesBetweenRunningAndCompletedByActivity(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	chat := func(id string, ago time.Duration) session.SessionRow {
		return session.SessionRow{ID: id, Title: "conversation " + id, Transcript: "/chats/" + id + "/transcript.jsonl", At: now.Add(-ago)}
	}
	a, b, c, d := chat("a", 48*time.Hour), chat("b", time.Hour), chat("c", 72*time.Hour), chat("d", 72*time.Hour)
	a.Tasks.Rows = []session.TaskIndexEntry{
		{SessionID: "a", ID: "1", Label: "old parent", Status: string(session.TaskDone), EndedAt: now.Add(-30 * 24 * time.Hour)},
		{SessionID: "a", ID: "2", Parent: "1", Label: "new child", Status: string(session.TaskRunning), StartedAt: now.Add(-2 * time.Hour)},
	}
	a.Live = true
	a.Presence.RunningTasks = []session.PresenceTask{{ID: "2", State: string(session.TaskRunning), StartedAt: now.Add(-2 * time.Hour)}}
	c.Tasks.Rows = []session.TaskIndexEntry{{SessionID: "c", ID: "1", Label: "recent landing", Status: string(session.TaskDone), EndedAt: now.Add(-30 * time.Minute)}}
	d.Live = true
	d.Tasks.Rows = []session.TaskIndexEntry{{SessionID: "d", ID: "1", Label: "another running task", Status: string(session.TaskRunning), StartedAt: now.Add(-time.Hour)}}
	d.Presence.RunningTasks = []session.PresenceTask{{ID: "1", State: string(session.TaskRunning), StartedAt: now.Add(-time.Hour)}}
	read := func() tasksReading {
		world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{a, b, c, d}}}}
		return readTasks(world, tasksMine{}, session.LastDays(now, 14), tasksSort{}, time.Time{}, now)
	}
	check := func(r tasksReading, running, completed string) {
		t.Helper()
		ids := func(section tasksSection) string {
			var out []string
			for _, group := range r.tree().in(section) {
				out = append(out, group.chat.row.ID)
			}
			return strings.Join(out, ",")
		}
		if got := ids(tasksRunning); got != running {
			t.Fatalf("running=%q, want %q", got, running)
		}
		if got := ids(tasksCompleted); got != completed {
			t.Fatalf("completed=%q, want %q", got, completed)
		}
		if len(r.items) != 4 {
			t.Fatalf("time window split the conversation tree: %d tasks", len(r.items))
		}
		parent, child := tasksLineOf(t, r.lay(120), "old parent"), tasksLineOf(t, r.lay(120), "new child")
		if !parent.open || !parent.under || len(child.kin) <= len(parent.kin) {
			t.Fatal("nested tree did not open completely")
		}
	}
	check(read(), "d,a", "c,b")
	a.Live = false
	a.Presence.RunningTasks = nil
	a.Tasks.Rows[1].Status = string(session.TaskDone)
	a.Tasks.Rows[1].EndedAt = now.Add(-time.Minute)
	check(read(), "d", "a,c,b")
	// A new run moves the same intact tree back into running.
	a.Live = true
	a.Tasks.Rows[1].Status = string(session.TaskRunning)
	a.Tasks.Rows[1].EndedAt = time.Time{}
	a.Tasks.Rows[1].StartedAt = now
	a.Presence.RunningTasks = []session.PresenceTask{{ID: "2", State: string(session.TaskRunning), StartedAt: now}}
	check(read(), "a,d", "c,b")
	// Reading later does not pretend that every live task just became active.
	started := now
	now = now.Add(time.Hour)
	r := read()
	if stamp := r.tree().in(tasksRunning)[0].chat.rank.at; !stamp.Equal(started) {
		t.Fatalf("activity stamp=%v", stamp)
	}
}

func TestTasksFilteringKeepsConversationInItsUnfilteredSection(t *testing.T) {
	a := tasksTableApp(t)
	a.taskSheet.query.setText("annual toggle")
	r := a.tasksFiltered()
	if len(r.tree().in(tasksRunning)) != 1 || len(r.tree().in(tasksCompleted)) != 0 {
		t.Fatal("filtering out the question moved its conversation to completed")
	}
}

func TestTasksUseHomeConversationTitlesAndBullets(t *testing.T) {
	a, _ := homeTabsFixture(t)
	a.linear = true
	a.taskSheet = a.takeTaskReading()
	open, _ := homeConversationLines(a)
	cell := open[0].cell
	for _, state := range []struct{ working, unread bool }{{false, false}, {true, false}, {false, true}} {
		a.state = stateIdle
		if state.working {
			a.state = stateWorking
		}
		if a.unreadChats == nil {
			a.unreadChats = map[string]bool{}
		}
		a.unreadChats[a.frontTabKey()] = state.unread
		r := a.tasksFiltered()
		lines := r.lay(180)
		found := false
		for i, line := range lines {
			if line.kind != tasksLineChat || line.chat.row.Transcript != a.file {
				continue
			}
			row := plain(r.paint(lines, i, 180, a.pal, false))
			if !strings.Contains(row, cell.title) {
				t.Fatalf("Home title %q absent from Tasks: %q", cell.title, row)
			}
			if bullet := plain(a.homeConversationBullet(cell, a.pal)); !strings.Contains(row, bullet+" "+cell.title) {
				t.Fatalf("Home bullet %q absent from Tasks: %q", bullet, row)
			}
			found = true
		}
		if !found {
			t.Fatal("Home conversation absent from Tasks")
		}
	}
	// A question on a task marks its conversation with Home's question bullet.
	r := tasksChatReading()
	for i, line := range r.lay(180) {
		if line.kind == tasksLineChat && line.chat.row.ID == "room-a" {
			if row := plain(r.paint(r.lay(180), i, 180, a.pal, false)); !strings.Contains(row, a.pal.glyph(tokens.GNeedsHuman)) {
				t.Fatalf("question bullet missing: %q", row)
			}
		}
	}
}
