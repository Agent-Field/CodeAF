package session

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/roles"
)

func TestPlainBeltLandingReceiptRetainsExactTaskIdentityAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the run", "1", "Parser", "repair parser")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := &beltRun{store: store, root: store.RootID(), row: 9, title: "Parser", brief: "repair parser"}
	summary := RunSummary{Outcome: beltRunOutcomeDone, Result: "The parser handles an empty map."}
	original := beltRunOutcomeNote(store, run.root, summary, RunLanding{}, a.beltRunSpan(run))
	a.deliverBeltRunLanding(run, summary, RunLanding{})
	assert := func(entries []DisplayEntry) {
		t.Helper()
		if len(entries) != 1 || !entries[0].RoutineCompletion || entries[0].Text != original || len(entries[0].CompletionTasks) != 1 {
			t.Fatalf("plain belt receipt has no grouping provenance: %#v", entries)
		}
		tag := entries[0].CompletionTasks[0]
		if tag.ID != 9 || tag.Title != "Parser" || tag.Request != "repair parser" || tag.State != TaskDone {
			t.Fatalf("run identity changed: %+v", tag)
		}
	}
	assert(a.Transcript())
	if completer.requests() != 0 {
		t.Fatal("plain /task landing started a new model turn")
	}
	if err := a.Close(); err != nil {
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
	assert(shapeEntries(saved.messages, &sessionFile{presentation: saved.presentation, notes: saved.notes, replyTags: saved.replyTags}))
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("presentation rewrote landing journal")
	}
}

func TestOwedBeltLandingGroupsReceiptButKeepsTheActualAnswerVisible(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{finalText("The parser handles an empty map.")}}
	a, _ := newTestAgent(t, completer, func(c *Config) {
		c.AskConsent = false
		c.RolesSource = tierSettings(map[string]string{roles.TierKey(roles.TierLow): "test/cheap-model"})
	})
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "run", "1", "Parser", "repair parser")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	question := "What did the repair find?"
	if _, err := store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
		t.Fatal(err)
	}
	run := &beltRun{store: store, root: store.RootID(), row: 9, title: "Parser", brief: "repair parser"}
	a.deliverBeltRunLanding(run, RunSummary{Outcome: beltRunOutcomeDone, Result: "Root output preserved."}, RunLanding{})
	beltRunWaitFor(t, "the actual owed landing answer", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.running && completer.requests() >= 1 })
	receipt, answer := false, false
	for _, e := range a.Transcript() {
		if e.RoutineCompletion {
			receipt = true
			if len(e.CompletionTasks) != 1 || e.CompletionTasks[0].ID != 9 || e.CompletionTasks[0].Request != question || !strings.Contains(e.Text, "Root output preserved.") {
				t.Fatalf("owed receipt lost source or result: %+v", e)
			}
		}
		if e.Role == "assistant" && e.Answer && strings.Contains(e.Text, "The parser handles an empty map.") {
			answer = true
		}
	}
	if !receipt || !answer || completer.requests() != 1 {
		t.Fatalf("owed wake/answer changed: receipt=%v answer=%v calls=%d", receipt, answer, completer.requests())
	}
}

// A REPORT TURN DOES NOT REPEAT THE PERSON. An owed landing hands the model the
// person's question above the run's outcome, and the receipt that turn leaves
// in the conversation must not draw those words again as though they were said
// twice: it shows the outcome, live and after reopening, while the model still
// reads the question.
func TestOwedBeltLandingReceiptDoesNotRepeatThePersonsWords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{finalText("All three pieces are placed.")}}
	a, _ := newTestAgent(t, completer, func(c *Config) {
		c.SessionFile = path
		c.AskConsent = false
		c.RolesSource = tierSettings(map[string]string{roles.TierKey(roles.TierLow): "test/cheap-model"})
	})
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "run", "1", "Story", "write a third story")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	question := "okay write a third story please"
	if _, err := store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
		t.Fatal(err)
	}
	run := &beltRun{store: store, root: store.RootID(), row: 3, title: "Story", brief: "write a third story"}
	a.deliverBeltRunLanding(run, RunSummary{Outcome: beltRunOutcomeDone, Result: "The spine card is placed."}, RunLanding{})
	beltRunWaitFor(t, "the owed landing's turn", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.running && completer.requests() >= 1 })
	completer.mu.Lock()
	sent := false
	for _, messages := range completer.seen {
		for _, message := range messages {
			if message.Role == "user" && strings.Contains(messageText(message), question) && strings.Contains(messageText(message), "The spine card is placed.") {
				sent = true
			}
		}
	}
	completer.mu.Unlock()
	if !sent {
		t.Fatal("the model was not handed the person's question with the outcome")
	}
	assert := func(where string, entries []DisplayEntry) {
		t.Helper()
		receipts := 0
		for _, e := range entries {
			if e.Role == "user" && strings.Contains(e.Text, question) {
				continue // the person's own message, if the fixture had one
			}
			if strings.Contains(e.Text, question) {
				t.Fatalf("%s: a %s entry repeats the person's words: %q", where, e.Role, e.Text)
			}
			if e.RoutineCompletion {
				receipts++
				if !strings.Contains(e.Text, "The spine card is placed.") {
					t.Fatalf("%s: the receipt lost the outcome: %q", where, e.Text)
				}
			}
		}
		if receipts != 1 {
			t.Fatalf("%s: %d receipts, want 1: %#v", where, receipts, entries)
		}
	}
	assert("live", a.Transcript())
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	saved, err := replaySessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assert("reopened", shapeEntries(saved.messages, &sessionFile{presentation: saved.presentation, notes: saved.notes, replyTags: saved.replyTags}))
}
