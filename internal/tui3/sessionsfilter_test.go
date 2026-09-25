package tui3

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestSessionsFilterKeepsWholeConversationsForEachKindOfName(t *testing.T) {
	for _, query := range []string{"Shipping the Gate", "codeaf", "/work/codeaf", "token table", "port the parser", "TOKEN TABLE", "QA acceptance"} {
		t.Run(query, func(t *testing.T) {
			a := tasksChatApp(t)
			for i := range a.taskSheet.reading.chats {
				a.taskSheet.reading.chats[i].Workspace = "/work/" + a.taskSheet.reading.chats[i].Project
			}
			for i := range a.taskSheet.reading.items {
				item := &a.taskSheet.reading.items[i]
				item.row.Workspace = "/work/" + item.row.Project
				if item.entry.ID == "4" {
					item.entry.Label = "QA acceptance"
				}
			}
			a.taskSheet.query.setText(query)
			r := a.tasksFiltered()
			if len(r.tree().groups) != 1 || r.tree().groups[0].chat.row.ID != "room-a" {
				t.Fatalf("query %q did not select just its owning conversation", query)
			}
			if len(r.items) != 4 || tasksWorkRows(r.lay(120)) != 4 {
				t.Fatalf("query %q fragmented its conversation: %d tasks", query, len(r.items))
			}
			if r.tree().up[tasksKey{session: "room-a", id: "4"}].id != "3" {
				t.Fatal("filter lost the nested task's parent")
			}
		})
	}
}

func TestSessionsFilterFindsTasklessChatsAndTheirProjectPath(t *testing.T) {
	a := tasksChatApp(t)
	now := a.now()
	row := session.SessionRow{ID: "empty", Transcript: "/journals/empty/transcript.jsonl", Title: "Investigate deployment", Project: "service", Workspace: "/work/service", At: now}
	world := session.World{Projects: []session.Project{{Name: "service", Sessions: []session.SessionRow{row}}}}
	a.taskSheet.reading = readTasks(world, tasksMine{}, session.LastDays(now, 14), tasksSort{}, time.Time{}, now)
	for _, query := range []string{"deployment", "service", "/work/service", "no-such-conversation"} {
		a.taskSheet.query.setText(query)
		r := a.tasksFiltered()
		want := 1
		if query == "no-such-conversation" {
			want = 0
		}
		if len(r.tree().groups) != want || len(r.items) != 0 {
			t.Fatalf("query %q: %d conversations, %d tasks", query, len(r.tree().groups), len(r.items))
		}
	}
}
