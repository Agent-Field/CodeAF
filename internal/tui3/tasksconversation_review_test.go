package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestTasksIncludesMainChatsBeforeTheyDelegateWork(t *testing.T) {
	now := time.Now()
	row := session.SessionRow{ID: "main-chat", Title: "Investigate parser failures", Transcript: "/chat/main/transcript.jsonl", At: now}
	world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}
	r := readTasks(world, tasksMine{}, session.LastDays(now, 14), time.Time{}, now)
	line := tasksLineOf(t, r.lay(80), row.Title)
	if line.kind != tasksLineChat || line.folds || len(r.items) != 0 {
		t.Fatalf("a main chat with no children became a worker or a dead fold: %+v", line)
	}
	if !strings.Contains(r.head(80, false), "1 chat") {
		t.Fatalf("the heading lost the main chat: %s", r.head(80, false))
	}
	a := tasksChatApp(t)
	a.taskSheet.reading = r
	a.taskSheet.query.setText("Investigate")
	filtered := a.tasksFiltered()
	tasksLineOf(t, filtered.lay(80), row.Title)
	if note := strings.Join(a.taskSheet.note(a, 80), ""); strings.Contains(note, "nothing matches") {
		t.Fatalf("a matching main chat is called no match: %s", note)
	}
}

func TestTasksDeepAncestrySurvivesFilteringWithoutACutoff(t *testing.T) {
	now := time.Now()
	row := session.SessionRow{ID: "deep", Title: "Repair nested parser"}
	for id := 1; id <= 130; id++ {
		parent := ""
		if id > 1 {
			parent = itoa(id - 1)
		}
		row.Tasks.Rows = append(row.Tasks.Rows, session.TaskIndexEntry{ID: itoa(id), Parent: parent, SessionID: row.ID,
			Title: "Parser level " + itoa(id), Status: string(session.TaskDone), EndedAt: now})
	}
	world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}
	a := tasksChatApp(t)
	a.taskSheet.reading = readTasks(world, tasksMine{}, session.LastDays(now, 14), time.Time{}, now)
	a.taskSheet.query.setText("130")
	r := a.tasksFiltered()
	if len(r.items) != 130 {
		t.Fatalf("search discarded ancestors: got %d, want 130", len(r.items))
	}
	for id := 2; id <= 130; id++ {
		if got := r.tree().up[tasksKey{session: row.ID, id: itoa(id)}].id; got != itoa(id-1) {
			t.Fatalf("level %d lost its parent: %s", id, got)
		}
	}
	if n := tasksWorkRows(r.lay(60)); n != 130 {
		t.Fatalf("search hid deep work: %d rows", n)
	}
}

func TestTaskTallyCountsActualStatesWithinAnUrgentConversation(t *testing.T) {
	r := tasksChatReading()
	if got := r.tally(); !strings.Contains(got, "1 needs your look") || strings.Contains(got, "4 needs your look") {
		t.Fatalf("the urgent conversation reclassified its finished siblings: %s", got)
	}
}

func TestOpeningMainChatFromTasksLeavesItsChildRoom(t *testing.T) {
	a, _, _ := roomApp(t)
	a.title = "Repair the parser"
	a.input.setText("main draft")
	a.openRoom(7, "Fix the nil-map crash")
	a.input.setText("child draft")
	a.showPage(pageTasks)
	if _, ok := a.taskSheetChat(); !ok {
		t.Fatal("the conversation is not the first selectable row")
	}
	a.taskSheetEnter()
	if a.roomOpen() || a.at(pageTasks) {
		t.Fatal("opening the main conversation left its child room or Tasks open")
	}
	if a.input.String() != "main draft" {
		t.Fatalf("the main draft was not restored: %q", a.input.String())
	}
	a.openRoom(7, "Fix the nil-map crash")
	if a.input.String() != "child draft" {
		t.Fatalf("the child draft was lost: %q", a.input.String())
	}
}

func TestTasksRefreshesMainChatStateWithoutAWorkerUpdate(t *testing.T) {
	a, _, _ := roomApp(t)
	a.state = stateWorking
	a.showPage(pageTasks)
	stamp, away := a.railStamp, a.elsewhere().Read
	a.state = stateIdle
	a.taskSheet.regroup(a)
	if a.taskSheet.mine.row.Presence.State != session.PresenceIdle {
		t.Fatal("the main chat still appears to be working after its turn ended")
	}
	if stamp != a.railStamp || !away.Equal(a.elsewhere().Read) {
		t.Fatal("the fixture accidentally refreshed a worker or another window")
	}
}

func TestAwayPresenceKeepsItsKnownChatAndAncestry(t *testing.T) {
	world, win, now := tasksChatFixture()
	mine := tasksMine{away: []session.ElsewhereTask{{SessionID: "room-a", Session: "another window",
		Task: session.PresenceTask{ID: "4", Title: "port the token table", State: string(session.TaskRunning)}}}}
	r := readTasks(world, mine, win, time.Time{}, now)
	key := tasksKey{session: "room-a", id: "4"}
	tree := r.tree()
	if tree.up[key].id != "3" {
		t.Fatal("the latest presence erased the known parent")
	}
	item := tree.at[key]
	if item.row.Title != "shipping the gate" || !tasksMatches(item, "shipping the gate") {
		t.Fatal("the latest presence erased its searchable main chat")
	}
}

func TestLiveTaskReadingPreservesGraphParents(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		a, _, _ := roomApp(t)
		drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Repair lexer", session.TaskQueued, session.TaskNotice{Parent: 7})})
		if indexed {
			a.comp.tasks = []session.TaskIndexEntry{{SessionID: a.taskSheetSelfRow().ID, ID: "8", Title: "Repair lexer", Status: string(session.TaskQueued)}}
		}
		a.showPage(pageTasks)
		tree := a.taskSheet.reading.tree()
		child := tasksKey{session: a.taskSheetSelfRow().ID, id: "8"}
		if tree.up[child].id != "7" {
			t.Fatalf("live graph parent lost (indexed=%v)", indexed)
		}
		a.taskSheet.query.setText("Repair lexer")
		filtered := a.tasksFiltered()
		if len(filtered.items) != 2 {
			t.Fatalf("live search lost the parent (indexed=%v): %d rows", indexed, len(filtered.items))
		}
	}
}

func TestEveryMainChatDoorLeavesTheChildViewAndRestoresDrafts(t *testing.T) {
	for _, door := range []string{"home", "search", "tasks"} {
		t.Run(door, func(t *testing.T) {
			a, _, _ := roomApp(t)
			a.title = "Repair the parser"
			row := a.taskSheetSelfRow()
			a.input.setText("main draft")
			a.openRoom(7, "Fix the nil-map crash")
			a.input.setText("child draft")
			switch door {
			case "home":
				a.showPage(pageHome)
				a.homeOpenLine(homeLine{row: row})
			case "search":
				a.showPage(pageSearch)
				a.openConversationRow(row)
			case "tasks":
				a.showPage(pageTasks)
				a.taskSheetEnter()
			}
			if a.roomOpen() || a.input.String() != "main draft" {
				t.Fatalf("%s did not return to the main chat and draft", door)
			}
			a.openRoom(7, "Fix the nil-map crash")
			if a.input.String() != "child draft" {
				t.Fatal("the child draft was lost")
			}
		})
	}
}
