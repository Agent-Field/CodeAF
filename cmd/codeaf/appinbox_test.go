package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// EVERY OPEN ASK IS AN INBOX ITEM with the turns before it; an answer from the
// Inbox is held and can be taken back, and an answer anywhere else clears the
// item with its receipt.
func TestAppInboxHoldsUndoesAndClearsEverywhere(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{
		{Kind: session.EventTextDelta, Text: "Looking at the build."},
		{Kind: session.EventTurnDone},
	}}
	backend, rec, agent := workBackend(t, conv)
	conv.mu.Lock()
	conv.onAnswer = func(answer session.Answer) {
		closed := session.Question{Kind: answer.Kind, ID: answer.ID}
		conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &closed, Answer: &answer})
	}
	conv.mu.Unlock()
	if _, err := backend.Send(agent.ID, "clean the build dir", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)

	first := session.Question{ID: 4, Kind: session.QuestionConsent, Head: "bash rm -rf build", Reason: "bash asks first", Options: session.AnswerOptions(session.QuestionConsent)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &first})
	i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool { _, ok := data.(appwire.InboxAddedEvent); return ok })
	rec.mu.Lock()
	item := rec.data[i].(appwire.InboxAddedEvent).Item
	rec.mu.Unlock()
	if item.ID != agent.ID+"/consent:4" || item.List != appwire.InboxWaiting || item.Where.Agent != agent.ID || len(item.Context) != 2 || item.Context[0].Text != "clean the build dir" {
		t.Fatalf("the item = %+v", item)
	}
	items, counts, _ := backend.ListInbox(appwire.InboxWaiting, "rm -rf")
	if len(items) != 1 || counts.Waiting != 1 {
		t.Fatalf("waiting = %+v %+v", items, counts)
	}

	until, err := backend.AnswerInbox(item.ID, appwire.OptionAllowOnce, "", "")
	if err != nil || until.Before(time.Now().Add(4*time.Second)) {
		t.Fatalf("the held answer = %v %v", until, err)
	}
	if err := backend.UndoInbox(item.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(appInboxHold + 500*time.Millisecond)
	conv.mu.Lock()
	sent := len(conv.answers)
	conv.mu.Unlock()
	if sent != 0 {
		t.Fatal("an answer taken back was sent")
	}

	if err := backend.LaterInbox(item.ID, true); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.InboxAddedEvent)
		return ok && added.Item.ID == item.ID && added.Item.List == appwire.InboxLater
	})
	// Answered in the dock, it leaves the Inbox with its receipt.
	if err := backend.AnswerAsk(agent.ID, "consent:4", appwire.OptionAllowOnce, ""); err != nil {
		t.Fatal(err)
	}
	j := rec.waitFor(t, 5*time.Second, func(event string, data any) bool { _, ok := data.(appwire.InboxResolvedEvent); return ok })
	rec.mu.Lock()
	resolved := rec.data[j].(appwire.InboxResolvedEvent)
	rec.mu.Unlock()
	if resolved.ID != item.ID || resolved.By != appwire.ClosedByApp || !strings.HasPrefix(resolved.Receipt, "Allowed once · you · ") {
		t.Fatalf("resolved = %+v", resolved)
	}
	done, counts, _ := backend.ListInbox(appwire.InboxDone, "")
	if len(done) != 1 || counts.DoneToday != 1 || counts.Later != 0 || done[0].Answered == nil {
		t.Fatalf("done = %+v %+v", done, counts)
	}
	if a, task, err := backend.OpenInbox(item.ID); err != nil || a != agent.ID || task != "" {
		t.Fatalf("open = %s %s %v", a, task, err)
	}
}
