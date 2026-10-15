package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// entryWith waits for the entry.added matching.
func entryWith(rec *appRecorder, t *testing.T, match func(appwire.Entry) bool) appwire.Entry {
	t.Helper()
	i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && match(added.Entry)
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.data[i].(appwire.EntryAdded).Entry
}

// A STEER IS THE ENGINE'S. It is pending when sent, consumed at the step the
// engine reads it, and when the turn ends first it falls through and its own
// stream becomes the next turn, with the person's words as its message.
func TestAppSteerIsConsumedOrFallsThroughIntoTheNextTurn(t *testing.T) {
	hold := make(chan struct{})
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventToolBegin, CallID: "c1", Tool: "bash", Args: `{"command":"go test ./..."}`}}, hold: hold}
	backend, rec, agent := workBackend(t, conv)
	if err := backend.Steer(agent.ID, "too early", ""); err == nil {
		t.Fatal("a steer with nothing running was taken")
	}
	if _, err := backend.Send(agent.ID, "run the suite", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool { _, ok := data.(appwire.TurnTool); return ok })

	first := make(chan session.Event, 4)
	conv.mu.Lock()
	conv.steer = first
	conv.mu.Unlock()
	if err := backend.Steer(agent.ID, "skip the e2e suite", "s-1"); err != nil {
		t.Fatal(err)
	}
	pending := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntrySteer })
	if pending.State != appwire.SteerPending || pending.ClientID != "s-1" || pending.Text != "skip the e2e suite" {
		t.Fatalf("the pending steer = %+v", pending)
	}
	first <- session.Event{Kind: session.EventSteerAccepted}
	first <- session.Event{Kind: session.EventSteerConsumed}
	close(first)
	consumed := entryWith(rec, t, func(e appwire.Entry) bool { return e.ID == pending.ID && e.State == appwire.SteerConsumed })
	if consumed.Step == nil || *consumed.Step != 1 {
		t.Fatalf("the consumed steer = %+v", consumed)
	}

	late := make(chan session.Event, 8)
	conv.mu.Lock()
	conv.steer = late
	conv.mu.Unlock()
	if err := backend.Steer(agent.ID, "and bump the version", "s-2"); err != nil {
		t.Fatal(err)
	}
	late <- session.Event{Kind: session.EventSteerFellThrough}
	late <- session.Event{Kind: session.EventTextDelta, Text: "Bumped."}
	late <- session.Event{Kind: session.EventTurnDone}
	close(late)
	close(hold)
	entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntrySteer && e.State == appwire.SteerFellThrough })
	user := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryUser && e.Text == "and bump the version" })
	if user.ClientID != "s-2" || user.Turn == 0 {
		t.Fatalf("the fallen-through message = %+v", user)
	}
	answer := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryAnswer && e.Text == "Bumped." })
	if answer.Turn != user.Turn {
		t.Fatalf("the answer belongs to turn %d, the message to %d", answer.Turn, user.Turn)
	}
}

// THE QUEUE IS EDITED IN PLACE AND SENT IN ITS ORDER when the turn ends; a
// message already sent cannot be edited.
func TestAppQueueIsEditedReorderedAndSentInOrder(t *testing.T) {
	hold := make(chan struct{})
	conv := &fakeConversation{hold: hold}
	backend, rec, agent := workBackend(t, conv)
	if _, err := backend.Send(agent.ID, "first", ""); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, text := range []string{"then the README", "and the patch version", "tag it"} {
		queued, err := backend.Queue(agent.ID, text, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, queued[len(queued)-1].ID)
	}
	if _, err := backend.EditQueued(agent.ID, ids[1], "and bump the patch version"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.MoveQueued(agent.ID, ids[2], 0); err != nil {
		t.Fatal(err)
	}
	queued, err := backend.RemoveQueued(agent.ID, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 2 || queued[0].Text != "tag it" || queued[1].Text != "and bump the patch version" {
		t.Fatalf("the queue = %+v", queued)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.QueueChanged)
		return ok && len(c.Queued) == 2
	})
	conv.mu.Lock()
	conv.hold = nil
	conv.mu.Unlock()
	close(hold)
	deadline := time.Now().Add(5 * time.Second)
	for {
		conv.mu.Lock()
		texts := append([]string(nil), conv.texts...)
		conv.mu.Unlock()
		if len(texts) == 3 {
			if texts[1] != "tag it" || texts[2] != "and bump the patch version" {
				t.Fatalf("sent in the order %q", texts)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sent %q", texts)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := backend.EditQueued(agent.ID, ids[2], "too late"); err == nil || !strings.Contains(err.Error(), "already been sent") {
		t.Fatalf("an edit after the send = %v", err)
	}
}

// AN ASK CARRIES THE DOCK'S FACTS from its question: where it comes from, the
// evidence, the choices with the suggestion, the clock and the tasks it holds
// up; putting it aside and handing it back are answers of their own.
func TestAppAskCarriesTheDocksFactsAndCanBeHandedBack(t *testing.T) {
	conv := &fakeConversation{}
	backend, rec, agent := workBackend(t, conv)
	deadline := time.Now().Add(12 * time.Second).Round(time.Second)
	q := session.Question{
		ID: 9, Kind: session.QuestionAsk, Ask: session.AskChoice, Head: "Should trailing commas be on by default?", Reason: "the parser accepts both",
		Subject: session.SubjectRef{Kind: session.SubjectNode, ID: 4},
		Options: []session.AnswerOption{{Key: "strict", Label: "Keep strict", Body: "Opt in with strict: false."}, {Key: "tolerant", Label: "Tolerant by default"}},
		Pick:    &session.Pick{Key: "strict"}, Deadline: deadline,
		Blocking: session.Blocking{Tasks: []string{"5"}},
		Attach:   []session.Block{{Kind: session.BlockText, Body: "a\nb\nc\nd\ne\nf\ng\nh\ni\nj"}},
	}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	i := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	ask := rec.data[i].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	if ask.Source == nil || ask.Source.Kind != "task" || ask.Source.Task != agent.ID+":4" || ask.Reason != "the parser accepts both" {
		t.Fatalf("where the ask comes from = %+v", ask)
	}
	if len(ask.Choices) != 2 || !ask.Choices[0].Suggested || ask.Choices[1].Suggested || ask.Choices[0].Detail == "" {
		t.Fatalf("the choices = %+v", ask.Choices)
	}
	if ask.ClockUntil == nil || !ask.ClockUntil.Equal(deadline) || len(ask.HoldingUp) != 1 || ask.HoldingUp[0] != agent.ID+":5" {
		t.Fatalf("the clock and what it holds up = %+v", ask)
	}
	if ask.Evidence == nil || len(ask.Evidence.Lines) != appEvidenceLines || ask.Evidence.More != 2 {
		t.Fatalf("the evidence = %+v", ask.Evidence)
	}
	if err := backend.AnswerAskWith(agent.ID, ask.ID, appwire.AskAnswer{Later: true}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		o, ok := data.(appwire.AskOpened)
		return ok && o.Ask.ID == ask.ID && o.Ask.Later
	})
	conv.mu.Lock()
	conv.onAnswer = func(answer session.Answer) {
		closed := session.Question{Kind: answer.Kind, ID: answer.ID}
		conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &closed, Answer: &answer})
	}
	conv.mu.Unlock()
	if err := backend.AnswerAskWith(agent.ID, ask.ID, appwire.AskAnswer{YouDecide: true}); err != nil {
		t.Fatal(err)
	}
	receipt := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryReceipt })
	if !strings.HasPrefix(receipt.Text, "You let it decide · it chose Keep strict · ") {
		t.Fatalf("the receipt = %q", receipt.Text)
	}
	conv.mu.Lock()
	got := conv.answers[0]
	conv.mu.Unlock()
	if got.Key != "strict" {
		t.Fatalf("the engine was handed %+v", got)
	}
}

// FILES AND LINKS IN THE COMPOSER: a reference finds the workspace's files, a
// path is stat'ed as there, missing or outside, a long paste becomes a card,
// and a message's attachments ride on its entry while the engine is sent words.
func TestAppFilesAndAttachmentsRideOnTheMessage(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	backend, rec, agent := workBackend(t, conv)
	work := agent.Workspace
	if err := os.MkdirAll(filepath.Join(work, "internal", "retry"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "internal", "retry", "backoff.go"), []byte("package retry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	groups, err := backend.FindFiles(agent.ID, "backo", 10)
	if err != nil || len(groups) != 1 || len(groups[0].Files) != 1 || groups[0].Files[0].Path != filepath.Join("internal", "retry", "backoff.go") {
		t.Fatalf("files.find = %+v %v", groups, err)
	}
	stats, err := backend.StatFiles(agent.ID, []string{"internal/retry/backoff.go", "gone.go", "/etc/hosts"})
	if err != nil || stats[0].State != appwire.FileExists || stats[1].State != appwire.FileMissing || stats[2].State != appwire.FileOutside {
		t.Fatalf("files.stat = %+v %v", stats, err)
	}
	if _, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachPaste}, "short"); err == nil {
		t.Fatal("a short paste became a card")
	}
	paste, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachPaste}, strings.Repeat("--- FAIL: TestLoad\n", 20))
	if err != nil || paste.Lines == nil || *paste.Lines != 20 {
		t.Fatalf("the paste card = %+v %v", paste, err)
	}
	file, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachFile, Path: "internal/retry/backoff.go"}, "")
	if err != nil || file.State != appwire.FileExists {
		t.Fatalf("the file chip = %+v %v", file, err)
	}
	if _, err := backend.SendWith(agent.ID, "why does it fail?", "m-1", []string{file.ID, paste.ID}); err != nil {
		t.Fatal(err)
	}
	user := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryUser })
	if user.Text != "why does it fail?" || len(user.Attachments) != 2 {
		t.Fatalf("the person's entry = %+v", user)
	}
	conv.mu.Lock()
	sent := conv.texts[0]
	conv.mu.Unlock()
	if !strings.Contains(sent, "Attached file: internal/retry/backoff.go") || !strings.Contains(sent, "--- FAIL: TestLoad") {
		t.Fatalf("the engine was sent %q", sent)
	}
}

// A NOTE TO A TASK is waiting until the engine says a step carried it, and
// then read at that step.
func TestAppTaskNoteIsReadAtAStep(t *testing.T) {
	conv := &fakeConversation{appTasks: []session.AppTaskView{{ID: "4", Title: "Update fixtures", State: "running"}}}
	backend, rec, agent := workBackend(t, conv)
	entry, err := backend.NoteTask(agent.ID+":4", "Also cover the empty-array case.", "n-1")
	if err != nil || entry.State != appwire.NoteWaiting || entry.ToolFacts.Task != agent.ID+":4" || entry.From != "you" {
		t.Fatalf("the note = %+v %v", entry, err)
	}
	step := 5
	conv.mu.Lock()
	conv.appTasks[0].Notes = []session.AppTaskNote{{Text: "Also cover the empty-array case.", ReadAtStep: &step}}
	conv.mu.Unlock()
	i := rec.waitFor(t, 6*time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.NoteChanged)
		return ok && changed.State == appwire.NoteRead
	})
	rec.mu.Lock()
	read := rec.data[i].(appwire.NoteChanged)
	rec.mu.Unlock()
	if read.ID != entry.ID || read.ReadAtStep == nil || *read.ReadAtStep != 5 {
		t.Fatalf("the read receipt = %+v", read)
	}
	if err := backend.RetryTask(agent.ID + ":4"); err != nil {
		t.Fatal(err)
	}
	conv.mu.Lock()
	retried := append([]uint64(nil), conv.retried...)
	conv.mu.Unlock()
	if len(retried) != 1 || retried[0] != 4 {
		t.Fatalf("retried %v", retried)
	}
}

// A LISTING SAYS WHAT THE LIVE TURN SAID: an update before a tool call, and a
// mark where a compaction summarized what came before, which it shows once.
func TestAppListingNamesUpdatesAndSummaries(t *testing.T) {
	record := session.Record{
		Earlier: []session.DisplayEntry{{Role: "user", Text: "old question"}, {Role: "assistant", Text: "old answer"}},
		Entries: []session.DisplayEntry{{Role: "user", Text: "rewritten"}, {Role: "user", Text: "now"}, {Role: "assistant", Text: "looking"}, {Role: "tool", Tool: "read", CallID: "c1", Answered: true}, {Role: "assistant", Text: "done"}},
		Floor:   1,
	}
	entries := appEntriesOf("a", appRecordItems(record), time.Now(), nil)
	var kinds []string
	for _, e := range entries {
		kinds = append(kinds, e.Kind)
	}
	want := []string{appwire.EntryUser, appwire.EntryAnswer, appwire.EntrySummary, appwire.EntryUser, appwire.EntryUpdate, appwire.EntryTool, appwire.EntryAnswer}
	if strings.Join(kinds, " ") != strings.Join(want, " ") {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
}

// A LONG OUTPUT KEEPS ITS LAST 512 KB, and says so on its first line.
func TestAppLongOutputIsTrimmedToItsLast512KB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.log")
	line := strings.Repeat("x", 99) + "\n"
	if err := os.WriteFile(path, []byte(strings.Repeat(line, 8000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if start := appTrimmedStart(path); start == 0 {
		t.Fatal("a long output is read whole")
	}
	appTrimOutput(path)
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), appTrimmedLine+"\n") || len(data) > appOutputKeep+len(appTrimmedLine)+1 {
		t.Fatalf("trimmed to %d bytes, %v", len(data), err)
	}
}
