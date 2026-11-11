package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// EVERY TRANSCRIPT ENTRY SAYS WHO WROTE IT, from the producer's own marks: the
// person's message, codeaf's carry-on nudge in the person's seat, the model's
// answer and call, the runner's progress line and the session's quiet note.
// On the 2026-11-06 live run the step's brief and "[carry on] … stage_result"
// were drawn as the person's bubbles, because a user entry said no more.
func TestATranscriptEntrySaysWhoWroteIt(t *testing.T) {
	items := []session.DisplayEntry{
		{Role: "user", Text: "plan: You are the plan step of a codeaf run."},
		{Role: "assistant", Text: "Reading the ledger."},
		{Role: "tool", Tool: "read", CallID: "c1"},
		{Role: "user", Text: "[carry on] You stopped without calling stage_result, so the factory has nothing from this step yet."},
		{Role: "user", Text: "use the existing helper", Person: &session.PersonMark{}},
		{Role: "assistant", Text: "review done", FactoryProgress: true},
		{Role: "note", Text: "compacted"},
	}
	entries, _ := appEntryShapes("a1", items, time.Now(), nil)
	want := []string{appwire.AuthorPerson, appwire.AuthorAgent, appwire.AuthorAgent, appwire.AuthorEngine, appwire.AuthorPerson, appwire.AuthorEngine, appwire.AuthorEngine}
	if len(entries) != len(want) {
		t.Fatalf("%d entries, want %d", len(entries), len(want))
	}
	for i, e := range entries {
		if e.Author != want[i] {
			t.Errorf("entry %d (%s %q): author %q, want %q", i, e.Kind, e.Text, e.Author, want[i])
		}
	}

	// A FACTORY STEP'S FIRST MESSAGE IS ITS BRIEF, the engine's: the
	// conversation's owner record names its step.
	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	page := append([]appwire.Entry(nil), entries...)
	appMarkStepBrief(page, transcript)
	if page[0].Author != appwire.AuthorPerson {
		t.Fatal("a conversation with no owner record had its first message called the engine's")
	}
	owner, _ := json.Marshal(factoryChatOwner{Root: dir, Job: 7, Step: "plan"})
	if err := os.WriteFile(filepath.Join(dir, factoryOwnerFile), owner, 0o600); err != nil {
		t.Fatal(err)
	}
	appMarkStepBrief(page, transcript)
	if page[0].Author != appwire.AuthorEngine || page[4].Author != appwire.AuthorPerson {
		t.Fatalf("step chat: brief %q, the person's later message %q", page[0].Author, page[4].Author)
	}
	manager, _ := json.Marshal(factoryChatOwner{Root: dir, Job: 7})
	_ = os.WriteFile(filepath.Join(dir, factoryOwnerFile), manager, 0o600)
	page = append([]appwire.Entry(nil), entries...)
	appMarkStepBrief(page, transcript)
	if page[0].Author != appwire.AuthorPerson {
		t.Fatal("a job's manager conversation had the person's first message called the engine's")
	}
}
