package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func TestTasksNeverNamesAMissingConversationByItsID(t *testing.T) {
	now := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	id := "e328e12690fba3e9"
	item := tasksItem{entry: session.TaskIndexEntry{SessionID: id, ID: "1", Title: "write hello.txt", Status: string(session.TaskDone), EndedAt: now}}
	tree := tasksTreeOf([]tasksItem{item}, now, tasksSort{})
	if len(tree.groups) != 1 || tree.groups[0].chat.title != unnamedConversationWord {
		t.Fatalf("missing conversation drawn by its id: %+v", tree.groups)
	}
}

func TestUnusedCurrentConversationRemainsReachableOnAShortTaskPage(t *testing.T) {
	now := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	for _, state := range []session.TaskState{session.TaskUnverified, session.TaskDone} {
		t.Run(string(state), func(t *testing.T) {
			a := newTestApp(&fakeAgent{})
			a.width, a.height = 120, 14
			a.clock = func() time.Time { return now }
			a.file = "/journals/9c1d4a0b7e2f6538/transcript.jsonl"
			own := session.SessionRow{ID: "9c1d4a0b7e2f6538", Transcript: a.file, Open: true}
			other := session.SessionRow{ID: "e328e12690fba3e9", Transcript: "/journals/e328e12690fba3e9/transcript.jsonl", At: now}
			other.Tasks.Rows = []session.TaskIndexEntry{{SessionID: other.ID, ID: "1", Title: "write hello.txt", Status: string(state), EndedAt: now}}
			world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{other}}}}
			r := readTasks(world, tasksMine{row: own}, session.LastDays(now, 14), tasksSort{}, time.Time{}, now)
			a.raisePlace(pageTasks)
			a.taskSheet = tasksPlace{world: world, reading: r}
			lines := r.lay(a.taskSheetListWidth())
			for at, line := range lines {
				if line.kind != tasksLineChat || line.chat.row.ID != own.ID {
					continue
				}
				a.taskSheet.cursor = at
				frame, _, _, _ := a.taskSheetFrame(a.width, a.height)
				page := plain(strings.Join(frame, "\n"))
				if !strings.Contains(page, unnamedConversationWord) || strings.Contains(page, own.ID) {
					t.Fatalf("current conversation cannot be reached at 14 rows:\n%s", page)
				}
				return
			}
			t.Fatal("current conversation is not selectable")
		})
	}
}

func TestMissingConversationKeepsTheTaskRecordAndLandingState(t *testing.T) {
	now := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	for _, state := range []session.TaskState{session.TaskUnverified, session.TaskDone} {
		entry := session.TaskIndexEntry{SessionID: "e328e12690fba3e9", ID: "1", Title: "write hello.txt", Status: string(state), EndedAt: now}
		r := readTasks(session.World{}, tasksMine{rows: []tasksMineRow{{entry: entry}}}, session.LastDays(now, 14), tasksSort{}, time.Time{}, now)
		lines := r.lay(120)
		found := false
		for at, line := range lines {
			if line.kind != tasksLineTask {
				continue
			}
			found = true
			if !r.picks(lines, at) || line.item.entry.Status != string(state) {
				t.Fatalf("task lost its state or record door: %+v", line)
			}
			page := plain(r.paint(lines, at, 120, newPalette(tokens.NoColor, false), false))
			if !strings.Contains(page, taskStateWord(entry, false)) {
				t.Fatalf("task lost its own landing word: %q", page)
			}
		}
		if !found {
			t.Fatalf("task landed %s but its record row is absent", state)
		}
	}
}

func TestTasksListsTheUnusedCurrentConversationAfterEitherLanding(t *testing.T) {
	now := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	for _, state := range []session.TaskState{session.TaskUnverified, session.TaskDone} {
		t.Run(string(state), func(t *testing.T) {
			own := session.SessionRow{ID: "9c1d4a0b7e2f6538", Transcript: "/journals/9c1d4a0b7e2f6538/transcript.jsonl", Open: true, At: now}
			other := session.SessionRow{ID: "e328e12690fba3e9", Transcript: "/journals/e328e12690fba3e9/transcript.jsonl", At: now}
			other.Tasks.Rows = []session.TaskIndexEntry{{SessionID: other.ID, ID: "1", Title: "write hello.txt", Status: string(state), EndedAt: now}}
			world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{other}}}}
			r := readTasks(world, tasksMine{row: own}, session.LastDays(now, 14), tasksSort{}, time.Time{}, now)
			if !strings.Contains(r.head(120, false), "2 chats") {
				t.Fatalf("fixture lost a counted conversation: %s", r.head(120, false))
			}
			listed := map[string]bool{}
			for _, line := range r.lay(120) {
				if line.kind == tasksLineChat {
					listed[line.chat.row.ID] = true
				}
			}
			if !listed[own.ID] || !listed[other.ID] {
				t.Fatalf("counted conversations are missing after %s: %v", state, listed)
			}
		})
	}
}
