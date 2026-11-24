package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// WHAT AFI MAKES IS A FILE (build spec v2, priority 1). A long comment from
// the server's Afi is never posted on the thread: it is kept as a document
// the task made, and the thread gets one "made" line naming it. A short
// comment is still a comment.
func TestAfisLongOutputBecomesAFileWithOneLine(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	draft := "# Release notes for 4.2\n\n" + strings.Repeat("- Fixed a parser bug that dropped unicode fractions in recipes.\n", 12)
	var taskID string
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		if turn.Task != nil {
			return AgentTurnEnd{}, nil
		}
		d := turn.Door.(workDoor)
		line, err := d.Work(ctx, "ws_task_create", json.RawMessage(`{"title":"Write the 4.2 notes","request":"Write them.","goal":"Ship 4.2","deliverable":"A document in Files","acceptance":["Covers each fix","Plain words"]}`))
		if err != nil {
			t.Fatal(err)
		}
		var made struct{ Task struct{ ID string } }
		_ = json.Unmarshal([]byte(line), &made)
		taskID = made.Task.ID
		args, _ := json.Marshal(map[string]string{"task": taskID, "text": draft})
		said, err := d.Work(ctx, "ws_task_comment", args)
		if err != nil {
			t.Fatalf("a long comment was refused: %v", err)
		}
		if !strings.Contains(said, "Release notes for 4.2") || !strings.Contains(said, "document") {
			t.Errorf("the model was told %q", said)
		}
		if _, err := d.Work(ctx, "ws_task_comment", json.RawMessage(`{"task":"`+taskID+`","text":"Wrote the notes."}`)); err != nil {
			t.Fatal(err)
		}
		return AgentTurnEnd{}, nil
	})
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "Write the notes."}, 202, nil)
	o.settle()

	var events api.WorkstreamTaskEvents
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID+"/tasks/"+taskID+"/events", -1, nil, 200, &events)
	var made, comments []factory.WorkstreamTaskEvent
	for _, e := range events.Events {
		switch e.Kind {
		case factory.TaskEventMade:
			made = append(made, e)
		case factory.TaskEventComment:
			comments = append(comments, e)
		}
	}
	if len(made) != 1 || made[0].File == "" || made[0].Text != "Release notes for 4.2" || made[0].By.Kind != factory.TaskByAfi {
		t.Fatalf("the made lines %+v", made)
	}
	if len(comments) != 1 || comments[0].Text != "Wrote the notes." {
		t.Fatalf("the comments %+v: the draft must not be posted as one", comments)
	}
	var files api.WorkstreamFiles
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID+"/files", -1, nil, 200, &files)
	for _, f := range files.Files {
		if f.ID == made[0].File {
			if f.FromTask != taskID || f.Kind != factory.FileDocument {
				t.Fatalf("the kept document %+v", f)
			}
			return
		}
	}
	t.Fatalf("the made line names %s, which is not in Files", made[0].File)
}

func TestACommentIsTooLongPastFourHundredCharactersOrThreeLines(t *testing.T) {
	for text, want := range map[string]bool{
		"Wrote the notes.":       false,
		"one\ntwo\nthree":        false,
		"one\ntwo\nthree\nfour":  true,
		strings.Repeat("a", 400): false,
		strings.Repeat("ä", 401): true,
	} {
		if got := factory.TaskCommentTooLong(text); got != want {
			t.Errorf("TaskCommentTooLong(%.20q) = %v", text, got)
		}
	}
	if n := factory.MadeFileName("Some notes\nmore", "Write the 4.2 notes"); n != "Write the 4.2 notes" {
		t.Errorf("named %q", n)
	}
}
