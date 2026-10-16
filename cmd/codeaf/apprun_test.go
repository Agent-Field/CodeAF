package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAppRunProjectionCarriesOnlyRecordedEntryIdentity(t *testing.T) {
	entries := appEntriesOf("agent", []session.DisplayEntry{
		{Role: "assistant", Text: "same", RunID: "run-one", Answer: true},
		{Role: "tool", Tool: "read", CallID: "call", RunID: "run-one"},
		{Role: "assistant", Text: "same", RunID: "run-two", Answer: true},
		{Role: "assistant", Text: "legacy", Answer: true},
	}, time.Now(), nil)
	if len(entries) != 4 || entries[0].RunID != "run-one" || entries[1].RunID != "run-one" || entries[2].RunID != "run-two" || entries[3].RunID != "" {
		t.Fatalf("entry producer identities changed: %+v", entries)
	}
}

func TestAppRunProjectionKeepsActualLiveAnswerAndToolProducer(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{
		{Kind: session.EventTextDelta, Text: "before", RunID: "actual-run"},
		{Kind: session.EventToolBegin, CallID: "call", Tool: "read", RunID: "actual-run"},
		{Kind: session.EventToolEnd, CallID: "call", Tool: "read", RunID: "actual-run"},
		{Kind: session.EventTextDelta, Text: "after", RunID: "actual-run"},
	}}
	backend, rec, agent := workBackend(t, conv)
	if _, err := backend.Send(agent.ID, "hello", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 3*time.Second, func(event string, _ any) bool { return event == appwire.EventTurnDone })
	rec.mu.Lock()
	defer rec.mu.Unlock()
	answers, tools := 0, 0
	for _, value := range rec.data {
		switch v := value.(type) {
		case appwire.EntryAdded:
			if v.Entry.Kind == appwire.EntryAnswer || v.Entry.Kind == appwire.EntryUpdate {
				answers++
				if v.Entry.RunID != "actual-run" {
					t.Fatalf("answer run=%+v", v.Entry)
				}
			}
		case appwire.TurnTool:
			tools++
			if v.RunID != "actual-run" {
				t.Fatalf("tool run=%+v", v)
			}
		}
	}
	if answers != 2 || tools < 2 {
		t.Fatalf("saw %d answers and %d tool events", answers, tools)
	}
}
