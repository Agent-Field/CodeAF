package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A PROPOSAL ITS CLOCK STARTED IS NOT "NO LONGER NEEDED", even when its
// question closes before the task's own update arrives. The two travel on
// different lanes, so the receipt is read off the withdrawal's own word
// ([session.Withdrawal.Started]); a withdrawal without it, with no started task
// behind it, is still a proposal taken back.
func TestAppClockStartedProposalReceiptBeatsItsTaskUpdate(t *testing.T) {
	for _, started := range []bool{true, false} {
		t.Run(map[bool]string{true: "started", false: "taken back"}[started], func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			conv := &fakeConversation{}
			_, rec, _ := workBackend(t, conv)
			q := session.Question{ID: 7, Kind: session.QuestionTask, Head: "Start the task?", Deadline: time.Now().Add(4 * time.Second)}
			conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
			rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
			gone := q
			gone.Withdrawn = &session.Withdrawal{Reason: "it started on its own, as the card said it would", At: time.Now(), Started: started}
			conv.raise(session.Event{Kind: session.EventQuestionWithdrawn, Question: &gone})
			i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
				added, ok := data.(appwire.EntryAdded)
				return ok && added.Entry.Kind == appwire.EntryReceipt
			})
			rec.mu.Lock()
			text := rec.data[i].(appwire.EntryAdded).Entry.Text
			rec.mu.Unlock()
			if want := map[bool]string{true: "Started on its own", false: "No longer needed"}[started]; !strings.HasPrefix(text, want) {
				t.Fatalf("receipt = %q, want it to start %q", text, want)
			}
		})
	}
}

// A TASK ON THE WIRE NAMES WHO IT ACTS AS, so the app can let go of what it
// keeps under that name when the task ends ([appwire.Task.Worker]).
func TestAppTaskCarriesItsWorker(t *testing.T) {
	notice := session.TaskNotice{ID: 4, Title: "Opening scene", State: session.TaskDone, Worker: "did:key:zabc#w4"}
	task, ok := appTaskOf("a1", &notice, false, "", time.Now())
	if !ok || task.Worker != "did:key:zabc#w4" || task.State != appwire.TaskDone {
		t.Fatalf("task = %+v", task)
	}
}
