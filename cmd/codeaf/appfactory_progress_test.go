package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// jobConversation is a job's conversation on this machine's disk, seeded the
// way the factory's talk maker leaves one, and its agent id.
func jobConversation(t *testing.T) (transcript, id string) {
	t.Helper()
	workspace := t.TempDir()
	transcript, err := appMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SeedConversation(transcript, workspace, "#12 · fix the ledger", "[factory item #12]\nYou are the manager of #12."); err != nil {
		t.Fatal(err)
	}
	return transcript, appAgentID(transcript)
}

// heldOverFakes is a backend whose conversations open as fakes over their
// real transcripts, recording every event it says.
func heldOverFakes(t *testing.T) (*appBackend, *appRecorder) {
	t.Helper()
	useLocalHome(t)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	return backend, rec
}

// addedSaying waits for the entry.added that says text, and answers its entry.
func addedSaying(t *testing.T, rec *appRecorder, text string) appwire.Entry {
	t.Helper()
	at := rec.waitFor(t, 10*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return event == appwire.EventEntryAdded && ok && added.Entry.Text == text
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.data[at].(appwire.EntryAdded).Entry
}

// A JOB'S PROGRESS LINE REACHES THE APP AS IT IS WRITTEN, as the entry it is:
// the same id, words and step a later reading of the conversation answers. A
// step line carries its step; a line about the job carries none.
func TestAJobsProgressLineReachesTheAppAsItIsWritten(t *testing.T) {
	backend, rec := heldOverFakes(t)
	transcript, id := jobConversation(t)
	if err := backend.holdJobChat(id); err != nil {
		t.Fatal(err)
	}
	review := &factory.StepProgress{Step: "review", Round: 2, Event: factory.ProgressDone}
	if err := session.AppendProgress(transcript, "review   done · 4m · clean", review); err != nil {
		t.Fatal(err)
	}
	live := addedSaying(t, rec, "review done · 4m · clean")
	want := appwire.EntryProgress{Step: "review", Round: 2, Event: "done"}
	if live.Agent != id || live.Kind != appwire.EntryAnswer || live.Progress == nil || *live.Progress != want {
		t.Fatalf("the live entry is %+v", live)
	}
	if err := session.AppendProgress(transcript, "landed · proof sheet ready · your approval", nil); err != nil {
		t.Fatal(err)
	}
	landed := addedSaying(t, rec, "landed · proof sheet ready · your approval")
	if landed.Progress != nil {
		t.Fatalf("a line about the job carries a step: %+v", landed.Progress)
	}

	page, _, err := backend.Entries(id, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) < 2 {
		t.Fatalf("the conversation reads %+v", page)
	}
	read, last := page[len(page)-2], page[len(page)-1]
	if read.ID != live.ID || read.Text != live.Text || read.Progress == nil || *read.Progress != want {
		t.Fatalf("the reading answers %+v for the line said live as %+v", read, live)
	}
	if last.ID != landed.ID || last.Progress != nil {
		t.Fatalf("the reading answers %+v for the job's line said live as %+v", last, landed)
	}
}

// A CONVERSATION THE APP DOES NOT HOLD IS TOLD NOTHING LIVE: it is read whole,
// line and step and all, when the app opens it.
//
// THE SILENCE IS READ AFTER THE NEWS HAS BEEN HANDLED, never before it could
// arrive: a line in a conversation the app does hold is written after, and the
// desk tells lines in the order they were written, so once that line's entry
// has been said the first line's news has been handled too.
func TestAProgressLineInAConversationTheAppDoesNotHoldIsReadWhenItOpens(t *testing.T) {
	backend, rec := heldOverFakes(t)
	transcript, id := jobConversation(t)
	held, heldID := jobConversation(t)
	if err := backend.holdJobChat(heldID); err != nil {
		t.Fatal(err)
	}
	started := &factory.StepProgress{Step: "plan", Round: 1, Event: factory.ProgressStarted}
	if err := session.AppendProgress(transcript, "plan started", started); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendProgress(held, "review started", &factory.StepProgress{Step: "review", Round: 1, Event: factory.ProgressStarted}); err != nil {
		t.Fatal(err)
	}
	if after := addedSaying(t, rec, "review started"); after.Agent != heldID {
		t.Fatalf("the held conversation's line was said for %s", after.Agent)
	}
	rec.mu.Lock()
	for i, event := range rec.events {
		if added, ok := rec.data[i].(appwire.EntryAdded); event == appwire.EventEntryAdded && ok && added.Entry.Agent == id {
			rec.mu.Unlock()
			t.Fatalf("a conversation nobody holds was told %+v", added.Entry)
		}
	}
	rec.mu.Unlock()
	page, _, err := backend.Entries(id, "", 10)
	if err != nil || len(page) == 0 {
		t.Fatalf("the conversation reads %+v, %v", page, err)
	}
	if got := page[len(page)-1]; got.Text != "plan started" || got.Progress == nil || *got.Progress != (appwire.EntryProgress{Step: "plan", Round: 1, Event: "started"}) {
		t.Fatalf("the line reads %+v", got)
	}
}
