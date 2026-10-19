package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Contract C3.a, C3.b, C3.c and C2.b: a real reopened backend lists and opens
// ordinary work beside a failed reading, and never counts or searches that
// quick row as a task, including when an old live overlay still names it.
func TestAppTaskListAndPagesExcludeQuickWorkAcrossAReopen(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	id := "00000000000000d1"
	dir := writeV3Session(t, bucket, id, "CONTRACT compare files", time.Now())
	if err := session.SaveMeta(dir, session.Meta{ID: id, Workspace: workspace, Created: time.Now(), LastUserAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	place := session.Place{Dir: dir}
	checkpoint := `{"type":"tasks","version":1,"seq":2,"nodes":[
{"id":1,"title":"CONTRACT real task","brief":"finish the change","acceptance":"the change works","state":"done","noted":true,"report":"finished"},
{"id":2,"title":"CONTRACT reading","brief":"read the files","state":"failed","kind":"quick","stopped":true,"report":"read handoff failed; running inline","quick":{"line":"read the files","reading":true}}]}`
	if err := os.WriteFile(place.Tasks(), []byte(checkpoint), 0600); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 19, 12, 22, 25, 0, time.UTC)
	var index []byte
	for _, row := range []session.TaskIndexEntry{
		{ID: "1", SessionID: id, Title: "CONTRACT real task", Label: "CONTRACT real task", Status: "done", EndedAt: at},
		{ID: "2", SessionID: id, Title: "CONTRACT reading", Label: "CONTRACT reading", Kind: session.TaskKindQuick, Status: "failed", EndedAt: at.Add(time.Minute)},
	} {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		index = append(append(index, raw...), '\n')
	}
	if err := os.WriteFile(session.TaskIndexPath(place.Transcript()), index, 0600); err != nil {
		t.Fatal(err)
	}
	for reopen := 0; reopen < 2; reopen++ {
		backend := newAppBackend(newAppRecorder(), appOpenInProcess)
		t.Cleanup(func() { backend.Close() })
		list, err := backend.ListTasks(id)
		if err != nil || len(list) != 1 || list[0].ID != id+":1" {
			t.Fatalf("reopen %d tasks.list = %+v (%v)", reopen, list, err)
		}
		for _, task := range list {
			page, err := backend.GetTask(task.ID)
			if err != nil || page.Task.ID != task.ID {
				t.Fatalf("tasks.list named %s but tasks.get = %+v (%v)", task.ID, page, err)
			}
		}
		live := backend.liveOf(backend.agent(id))
		views, err := live.conn.Conv.AppTasks()
		if err != nil || len(views) != 1 || views[0].ID != "1" {
			t.Fatalf("live task graph includes quick work: %+v (%v)", views, err)
		}
		// A quick row left by an older lane must not re-enter through its overlay.
		backend.locked(func() {
			live.tasks[id+":2"] = appwire.Task{ID: id + ":2", Agent: id, Title: "CONTRACT reading", State: appwire.TaskFailed}
		})
		list, err = backend.ListTasks(id)
		if err != nil || len(list) != 1 || list[0].ID != id+":1" {
			t.Fatalf("live overlay resurrected quick work: %+v (%v)", list, err)
		}
		hits := backend.searchTasks("CONTRACT", 1)
		if len(hits) != 1 || hits[0].ID != id+":1" {
			t.Fatalf("quick work consumed the search limit: %+v", hits)
		}
		items, _, err := backend.ListInbox("", "")
		if err != nil || len(items) != 0 {
			t.Fatalf("failed reading reached the Inbox: %+v (%v)", items, err)
		}
		quicks, err := live.conn.Conv.AppQuickTasks()
		if err != nil || len(quicks) != 1 || appQuickOf(id, quicks[0], nil).State != appwire.QuickStopped {
			t.Fatalf("reading lost its truthful quick surface: %+v (%v)", quicks, err)
		}
		if err := backend.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Contract C3.a: even without a disk record, a quick notice cannot become a
// task on the live lane; ListTasks leaves the quick.changed surface separate.
func TestAppQuickNoticeNeverBecomesATaskOnTheLiveLane(t *testing.T) {
	conv := &fakeConversation{quickTasks: []session.AppQuickTaskView{{ID: "7", Title: "reading", State: "stopped"}}}
	backend, rec, agent := workBackend(t, conv)
	conv.lane(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, Kind: session.TaskKindQuick, Title: "reading", State: session.TaskFailed}})
	rec.waitFor(t, 2*time.Second, func(_ string, data any) bool {
		_, ok := data.(appwire.QuickChangedEvent)
		return ok
	})
	list, err := backend.ListTasks(agent.ID)
	if err != nil || len(list) != 0 {
		t.Fatalf("quick notice reached tasks.list: %+v (%v)", list, err)
	}
	if _, ok := appTaskOf(agent.ID, &session.TaskNotice{ID: 7, Kind: session.TaskKindQuick, State: session.TaskFailed}, false, "", time.Now()); ok {
		t.Fatal("a quick notice can still be converted into a task")
	}
}

// Contract C4.a, C4.b and C4.c: stopped is distinct on the wire, retains its
// files, and cannot become conflict; no unknown state quietly becomes done, and
// promoted keeps the done it has always been sent as.
func TestAppQuickWirePreservesStoppedFilesAndOnlyDoneCanConflict(t *testing.T) {
	for _, state := range []string{"running", "grew", "done", "stopped", "undone", "promoted", "future"} {
		t.Run(state, func(t *testing.T) {
			view := session.AppQuickTaskView{ID: "7", Title: "quick work", State: state, Grew: "needs a shell", Files: []session.AppQuickFile{{Path: "note.txt", Added: 3, Removed: 1}}}
			plain := appQuickOf("agent", view, nil)
			want := state
			if state == "promoted" {
				want = appwire.QuickDone // promoted has always been sent as done
			}
			if plain.State != want || len(plain.Files) != 1 || plain.Files[0].Path != "note.txt" || plain.Files[0].Add != 3 || plain.Files[0].Del != 1 {
				t.Fatalf("%s became %+v", state, plain)
			}
			conflicted := appQuickOf("agent", view, []string{"note.txt"})
			if want == appwire.QuickDone {
				if conflicted.State != appwire.QuickConflict || conflicted.Conflict == nil {
					t.Fatalf("done lost its conflict: %+v", conflicted)
				}
			} else if conflicted.State != want || conflicted.Conflict != nil {
				t.Fatalf("%s became a conflict: %+v", state, conflicted)
			}
			if (plain.Grew != nil) != (state == "grew") {
				t.Fatalf("%s carries a dead promotion: %+v", state, plain)
			}
		})
	}
}
