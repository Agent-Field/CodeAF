package tui3

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestTaskDeletionReconcilesLiveRailAndEveryRecordView(t *testing.T) {
	for _, page := range []page{pageHome, pageTasks} {
		t.Run(fmt.Sprint(page), func(t *testing.T) {
			lab := newSwitchLab(t)
			const ownerID = "bbbb000000000001"
			rows := []session.TaskIndexEntry{
				{ID: "101", PlanID: "t-parent", Title: "Delete this live parent", SessionID: ownerID, Status: string(session.TaskDone), EndedAt: lab.now},
				{ID: "102", Parent: "101", Title: "Delete this live child", SessionID: ownerID, Status: string(session.TaskDone), EndedAt: lab.now},
				{ID: "103", Title: "Keep this live sibling", SessionID: ownerID, Status: string(session.TaskDone), EndedAt: lab.now},
			}
			for _, row := range rows {
				lab.task("-beta", row)
			}
			a := lab.open(180, 60)
			owner := selectHomeTask(t, a, "101")
			a.file, a.workspace = owner.Transcript, owner.Workspace
			a.agent = &recordActionAgent{fakeAgent: &fakeAgent{model: "m"}, rows: rows}
			notices := []session.TaskNotice{
				{ID: 101, PlanID: "t-parent", Title: rows[0].Title, State: session.TaskDone},
				{ID: 102, Parent: 101, Title: rows[1].Title, State: session.TaskDone},
				{ID: 103, Title: rows[2].Title, State: session.TaskDone},
			}
			for i := range notices {
				a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &notices[i]})
			}
			if len(a.taskOrder) != 3 {
				t.Fatal("fixture did not populate live rail")
			}
			a.comp.tasks = append([]session.TaskIndexEntry(nil), rows...)
			a.planRowsFront = a.frontGen
			a.planRows = []session.PlanTaskRow{{ID: "t-parent"}, {ID: "t-child", Parent: "t-parent"}, {ID: "t-sibling"}}
			pilotStopped := false
			a.pilots = map[uint64]*taskPilot{102: {id: 102, stop: func() { pilotStopped = true }}}
			a.railWhere = railSpot{id: 102}
			a.railHold = true
			a.showPage(page)
			// Both pages dispatch the same confirmed deletion command.
			drain(t, a, a.deleteRecord(owner, &rows[0]))
			assertKept := func() {
				t.Helper()
				if !reflect.DeepEqual(a.taskOrder, []uint64{103}) || a.tasks[101] != nil || a.tasks[102] != nil || a.tasks[103] == nil {
					t.Fatalf("live rail retained deleted subtree: order=%v", a.taskOrder)
				}
				if a.railWhere.id != 103 {
					t.Fatalf("rail cursor still points at removed task: %+v", a.railWhere)
				}
				for _, row := range a.comp.tasks {
					if row.SessionID == ownerID && row.ID != "103" {
						t.Fatalf("mention retained deleted row: %v", row.ID)
					}
				}
				world := a.readWorld()
				for _, project := range world.Projects {
					for _, chat := range project.Sessions {
						for _, row := range chat.Tasks.Rows {
							if row.SessionID == ownerID && (row.ID == "101" || row.ID == "102") {
								t.Fatal("Home world retained deleted task")
							}
						}
					}
				}
				a.showPage(pageTasks)
				for _, item := range a.tasksFiltered().items {
					if item.entry.SessionID == ownerID && (item.entry.ID == "101" || item.entry.ID == "102") {
						t.Fatal("Sessions retained deleted task")
					}
				}
			}
			assertKept()
			if !pilotStopped || a.pilots[102] != nil {
				t.Fatal("deleted task retained its watcher")
			}
			if len(a.planRows) != 1 || a.planRows[0].ID != "t-sibling" {
				t.Fatal("cached plan retained deleted subtree")
			}
			if got := a.keepPlanTaskRecords([]session.PlanTaskRow{{ID: "t-parent"}, {ID: "t-child", Parent: "t-parent"}}); len(got) != 0 {
				t.Fatal("late plan snapshot restored deleted tasks")
			}
			// Delayed events and snapshots must not resurrect either projection.
			for i := range notices {
				a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &notices[i]})
			}
			drive(t, a, tasksLoadedMsg{rows: rows, known: true})
			assertKept()
			a.dropTasks()
			for i := range notices {
				a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &notices[i]})
			}
			if !reflect.DeepEqual(a.taskOrder, []uint64{103}) {
				t.Fatalf("switch-back replay resurrected tasks: %v", a.taskOrder)
			}
			// Numeric graph IDs are local to their conversation.
			a.dropTasks()
			a.file = filepath.Join(filepath.Dir(filepath.Dir(owner.Transcript)), "other-owner", "transcript.jsonl")
			a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &notices[0]})
			if a.tasks[101] == nil {
				t.Fatal("deletion leaked into another conversation")
			}
		})
	}
}

func TestPersistedTaskDeletionReconcilesAnAlreadyLoadedRail(t *testing.T) {
	a, files := homeTabsFixture(t)
	notice := session.TaskNotice{ID: 1, PlanID: "t-one", Title: "Deleted in another view", State: session.TaskDone}
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &notice})
	dir := filepath.Dir(files[0])
	meta, err := session.LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	meta.DeletedTasks = map[string]bool{"t-one": true}
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	a.readWorld()
	if len(a.taskOrder) != 0 || a.tasks[1] != nil {
		t.Fatal("persisted deletion did not reconcile loaded rail")
	}
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &notice})
	if len(a.taskOrder) != 0 {
		t.Fatal("late aliased notice restored deleted task")
	}
}
