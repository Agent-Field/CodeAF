package main

import (
	"fmt"
	"os"
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
	if _, err := backend.holdJobChat(id); err != nil {
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
	if _, err := backend.holdJobChat(heldID); err != nil {
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

func TestRecordedFactorySteerEchoStaysHiddenLiveAndAfterReload(t *testing.T) {
	backend, rec := heldOverFakes(t)
	transcript, id := jobConversation(t)
	if _, err := backend.holdJobChat(id); err != nil {
		t.Fatal(err)
	}
	const echo = "steer: Everything ok here?"
	if err := session.AppendProgress(transcript, echo, nil); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendProgress(transcript, "plan started", nil); err != nil {
		t.Fatal(err)
	}
	// News is delivered in order; this establishes that the echo was processed.
	live := addedSaying(t, rec, "plan started")
	rec.mu.Lock()
	for _, data := range rec.data {
		if added, ok := data.(appwire.EntryAdded); ok && added.Entry.Agent == id && added.Entry.Text == echo {
			t.Error("the runner echo was emitted live")
		}
	}
	rec.mu.Unlock()
	for range 2 {
		page, _, err := backend.Entries(id, "", 100)
		if err != nil || len(page) != 1 || page[0].Text != "plan started" || page[0].ID != live.ID {
			t.Fatalf("reloaded history exposed the echo or changed ids: %+v, %v", page, err)
		}
	}
}

// A WOKEN TURN'S ANSWER NEVER TAKES A PROGRESS LINE'S ID. The desk lane's
// trace: the manager answered under the person's message (a2.0), the factory
// wrote "stopped" between turns (a2.1 on every reading), and the woken turn,
// counting from the last turn's one answer, said its answer live as a2.1 too;
// the desk's upsert dropped it, and the reading gave it a2.2. The next live id
// is read from the transcript, so live and read back agree and never collide.
func TestAWokenAnswerAfterAProgressLineTakesTheIdTheReadingGivesIt(t *testing.T) {
	backend, rec := heldOverFakes(t)
	transcript, id := jobConversation(t)
	appendLine := func(line string) {
		t.Helper()
		f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	appendLine(`{"type":"message","role":"user","content":"add a security step after review"}`)
	appendLine(`{"type":"message","role":"assistant","content":"Added security after review."}`)
	if err := session.AppendProgress(transcript, "stopped", &factory.StepProgress{Step: "write", Round: 1, Event: factory.ProgressStopped}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.holdJobChat(id); err != nil {
		t.Fatal(err)
	}
	page, _, err := backend.Entries(id, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	taken := map[string]bool{}
	n := -1
	for _, e := range page {
		taken[e.ID] = true
		if e.Kind == appwire.EntryUser {
			fmt.Sscanf(e.ID, "u%d", &n)
		}
	}
	if n < 0 {
		t.Fatalf("no person's message in %+v", page)
	}
	// The last live turn said one answer under n; the progress line written
	// after it took the next id. The woken turn must not reuse it.
	a := backend.agent(id)
	live := backend.liveOf(a)
	backend.locked(func() { live.lastN, live.lastMsgs, live.lastNotices = n, 1, 0 })
	events := make(chan session.Event, 2)
	events <- session.Event{Kind: session.EventTextDelta, Text: "Security is running now."}
	events <- session.Event{Kind: session.EventTurnDone}
	close(events)
	backend.wokenTurn(a, live, appTurnElsewhere{events: events})
	said := addedSaying(t, rec, "Security is running now.").ID
	if taken[said] {
		t.Fatalf("the woken answer was said as %s, an id the reading already gives: %v", said, taken)
	}
	appendLine(`{"type":"message","role":"assistant","content":"Security is running now."}`)
	page, _, err = backend.Entries(id, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if got := page[len(page)-1]; got.ID != said || got.Text != "Security is running now." {
		t.Fatalf("the reading gives the woken answer %s (%q), it was said live as %s", got.ID, got.Text, said)
	}
}
