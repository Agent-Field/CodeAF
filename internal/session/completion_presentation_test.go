package session

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestCompletionPresentationSurvivesCompactionAndReopenWithoutChangingContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	text := "job 7 exited 0: report ready\n" + jobExitNewsRule
	routine := jobNote(text)
	routine.authored = true
	agent.mu.Lock()
	agent.recordUserLocked(routine)
	// Equal words typed by the person are a separate occurrence, not a receipt.
	quoted := userText(text)
	quoted.message = agent.messages[len(agent.messages)-1]
	agent.recordUserLocked(quoted)
	alert := wakeNote("Your requested watch matched: release is ready")
	alert.authored = true
	agent.recordUserLocked(alert)
	agent.mu.Unlock()
	assert := func(entries []DisplayEntry) {
		t.Helper()
		if len(entries) != 3 || !entries[0].RoutineCompletion || entries[0].Role != "aside" || entries[0].Text != text || entries[1].RoutineCompletion || entries[1].Text != text || entries[2].RoutineCompletion {
			t.Fatalf("receipt presentation crossed occurrences or removed an alert: %#v", entries)
		}
	}
	assert(agent.Transcript())
	window := append([]ai.Message(nil), agent.snapshot()[1:]...)
	if messageContentText(window[0]) != text {
		t.Fatal("model completion payload changed")
	}
	agent.file.appendCompaction(compactionPass{}, 0, window)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assert(shapeEntries(replayed.messages, &sessionFile{presentation: replayed.presentation, notes: replayed.notes}))
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("presentation cleanup modified the original journal")
	}
}

func TestCompletionPresentationKeepsMixedWatchAndTeamBatchesVisible(t *testing.T) {
	task := wakeNote("task 3 done: Build")
	task.completionOnly = true
	job := jobNote("job 7 exited 0")
	watch := wakeNote("Requested watch matched")
	team := wakeNote("A teammate sent a result")
	for _, tc := range []struct {
		name  string
		notes []userMessage
		quiet bool
	}{
		{"tasks and processes", []userMessage{task, job}, true},
		{"watch", []userMessage{task, watch}, false},
		{"team", []userMessage{job, team}, false},
		{"empty", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch := batchSessionNotes(tc.notes)
			if batch.completionOnly != tc.quiet {
				t.Fatalf("mixed result visibility changed: %#v", batch)
			}
			if len(tc.notes) > 0 && !batch.wake {
				t.Fatal("presentation removed the owed engine wake")
			}
			for _, note := range tc.notes {
				if !bytes.Contains([]byte(batch.text()), []byte(note.text())) {
					t.Fatal("presentation discarded background evidence")
				}
			}
		})
	}
}

func TestLegacyCompletionPresentationRequiresStandaloneRuntimeProvenance(t *testing.T) {
	notice := TaskNotice{ID: 9, State: TaskDone, Title: "Build report"}
	task := landingNoteLead(notice) + taskNote(notice, "task://report", TaskSettle(""), landingAddress{})
	job := "job 7 exited 0: report ready\n" + jobExitNewsRule + "\n\nretained output"
	for _, tc := range []struct {
		name, text string
		note       bool
		quiet      bool
	}{
		{"old task receipt", task, true, true},
		{"old job receipt", job, true, true},
		{"person quotes task", task, false, false},
		{"person quotes job", job, false, false},
		{"unmarked exit", "job 7 exited 0", true, false},
		{"quoted runtime text", "Here is an example:\n" + task, true, false},
		{"legacy mixed batch", "while you worked:\n\n" + task + "\n\nWatch matched", true, false},
		{"fallback answer", "program ended, but the chat could not finish its answer, so here is its result", true, false},
		{"work stop", "The time limit was reached; work stopped", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := sessionEntry{Type: "message", Role: "user", Note: tc.note, Content: tc.text}
			if got := legacyRoutineCompletion(entry); got != tc.quiet {
				t.Fatalf("legacy classification = %v, want %v", got, tc.quiet)
			}
		})
	}
}

func TestCompletionGroupingRetainsReceiptSourcesAcrossReopenWithoutEditingJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	tag := TaskReplyTag{ID: 9, Title: "Build report", Request: "Keep the original request", State: TaskDone, Revision: 2}
	receipt := wakeNote("exact retained runtime delivery payload")
	receipt.authored, receipt.completionOnly = true, true
	receipt.replyTags = []TaskReplyTag{tag}
	agent.mu.Lock()
	agent.recordUserLocked(receipt)
	agent.mu.Unlock()
	agent.recordAssistant(textMessage("assistant", "The requested report is ready."), provider.MessageReasoning{})
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := replaySessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entries := shapeEntries(saved.messages, &sessionFile{presentation: saved.presentation, notes: saved.notes, replyTags: saved.replyTags})
	if len(entries) != 2 || !entries[0].RoutineCompletion || len(entries[0].CompletionTasks) != 1 || entries[0].CompletionTasks[0] != tag || entries[0].Text != receipt.text() {
		t.Fatalf("grouped receipt lost exact source facts: %#v", entries)
	}
	if len(entries[0].ReplyTags) != 0 || len(entries[1].ReplyTags) != 1 || entries[1].ReplyTags[0] != tag {
		t.Fatal("receipt grouping consumed answer attribution")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("grouping rewrote existing journal")
	}
}

func TestLegacyCrewGroupingRequiresRuntimeAuthorshipAndCompleteReceipt(t *testing.T) {
	crew := "task 7 crew · bugfix · worker model-a · planner model-b · checker model-c · $0.310 (est $0.520)"
	for _, tc := range []struct {
		text       string
		note, want bool
	}{{crew, true, true}, {crew, false, false}, {"An example: " + crew, true, false}, {crew + "\nRequested alert: check deployment", true, false}, {"task 7 crew · user supplied prose", true, false}} {
		if got := legacyRoutineCompletion(sessionEntry{Role: "user", Note: tc.note, Content: tc.text}); got != tc.want {
			t.Fatalf("receipt=%q authored=%v grouped=%v", tc.text, tc.note, got)
		}
	}
}

func TestRequestedWatchAttentionSurvivesBatchAndReopenWithoutExposingHousekeeping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()
	agent.enqueueWatchNote("release", "watch release: until matched\nREADY", true)
	agent.mu.Lock()
	if len(agent.steering) != 1 || !agent.steering[0].attention {
		agent.mu.Unlock()
		t.Fatal("watch firing has no typed attention")
	}
	alert := agent.steering[0]
	batch := batchSessionNotes([]userMessage{jobNote("job 3 exited 0"), alert})
	agent.recordUserLocked(batch)
	agent.recordUserLocked(userMessage{message: textMessage("user", "provider retry bookkeeping"), authored: true})
	agent.mu.Unlock()
	if !batch.attention || batch.completionOnly {
		t.Fatal("mixed requested alert became routine completion")
	}
	assert := func(entries []DisplayEntry) {
		t.Helper()
		if len(entries) != 2 || !entries[0].Addressed || entries[0].RoutineCompletion || entries[1].Addressed {
			t.Fatalf("alert or housekeeping audience changed: %#v", entries)
		}
	}
	assert(agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	saved, err := replaySessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assert(shapeEntries(saved.messages, &sessionFile{presentation: saved.presentation, notes: saved.notes, replyTags: saved.replyTags}))
}

func TestLegacyAttentionProtectsTerminalWatchAndAmbiguousMixedOnly(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"watch release: until matched\nREADY", true}, {"watch build: quiet for 2m\nPASS", true}, {"while you worked:\n\ntask receipt\n\nwatch alert", true},
		{"watch release: tick 9\nREADY", false}, {"provider retry bookkeeping", false}, {helpersNoteOpening + "\n" + helpersNoteBody, false},
	} {
		mark := legacyPresentation(sessionEntry{Role: "user", Note: true, Content: tc.text})
		if got := mark != nil && mark.Attention; got != tc.want {
			t.Fatalf("legacy attention=%v for %q", got, tc.text)
		}
	}
}
