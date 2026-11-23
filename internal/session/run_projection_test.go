package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestRunProjectionMatchesActualTurnAndSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	next := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		return textResponse("identical answer"), nil
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{next, next}}, func(c *Config) { c.SessionFile = path })
	var runs []string
	for range 2 {
		events, err := agent.Submit(context.Background(), "identical question")
		if err != nil {
			t.Fatal(err)
		}
		var run string
		for _, e := range collect(t, events) {
			if e.Kind == EventTurnDone {
				run = e.RunID
			}
		}
		if run == "" {
			t.Fatal("actual turn did not carry its immutable run")
		}
		runs = append(runs, run)
	}
	if runs[0] == runs[1] {
		t.Fatal("different turns shared one run")
	}
	check := func(entries []DisplayEntry) {
		t.Helper()
		n := 0
		for _, entry := range entries {
			if entry.Role == "assistant" {
				if n >= len(runs) || entry.RunID != runs[n] {
					t.Fatalf("answer lost producing run: %+v", entry)
				}
				n++
			}
		}
		if n != 2 {
			t.Fatalf("got %d answers", n)
		}
	}
	check(agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	check(ReadTranscript(path).Entries)
}

func TestRunProjectionPreservesToolOnlyOccurrencesThroughCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	response := toolResponse("same-provider-call", "read", `{"path":"x"}`)
	message := ai.Message{Role: "assistant", Content: assistantContent(response), ToolCalls: response.Choices[0].Message.ToolCalls}
	agent.recordAssistant(message, provider.MessageReasoning{}, "run-one")
	agent.recordAssistant(message, provider.MessageReasoning{}, "run-two")
	agent.recordAssistant(textMessage("assistant", "legacy unknown"), provider.MessageReasoning{})
	check := func(entries []DisplayEntry) {
		t.Helper()
		var got []string
		for _, entry := range entries {
			if entry.Role == "tool" {
				got = append(got, entry.RunID)
			}
			if entry.Text == "legacy unknown" && entry.RunID != "" {
				t.Fatal("legacy acquired a run")
			}
		}
		if len(got) != 2 || got[0] != "run-one" || got[1] != "run-two" {
			t.Fatalf("tool occurrence runs = %v", got)
		}
	}
	check(agent.Transcript())
	agent.file.appendCompaction(compactionPass{}, 0, agent.snapshot()[1:])
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	check(ReadTranscript(path).Entries)
}

func TestRunHubDoesNotRelabelBackgroundFactsOrFoldDifferentRuns(t *testing.T) {
	hub := newEventHub()
	hub.runID = "foreground"
	events := hub.subscribe()
	hub.send(Event{Kind: EventTextDelta, Text: "one"})
	hub.send(Event{Kind: EventMediaUpdate, Media: &MediaChange{ID: "source", Run: "background"}})
	hub.send(Event{Kind: EventTextDelta, Text: "other", RunID: "explicit"})
	hub.close()
	got := collect(t, events)
	if len(got) != 3 || got[0].RunID != "foreground" || got[1].RunID != "" || got[1].Media.Run != "background" || got[2].RunID != "explicit" {
		t.Fatalf("wrong producer join: %+v", got)
	}
	if foldsInto(got[0], got[2]) {
		t.Fatal("text from distinct runs folded together")
	}
}

func TestInterruptedRunProjectionUsesItsOwnHubAfterAnotherTurnExists(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	old := newEventHub()
	old.runID = "old-run"
	newer := newEventHub()
	newer.runID = "new-run"
	agent.hub = newer
	partial := &partialBuffer{}
	partial.write("[update] A genuine partial finding.")
	agent.keepPartial(partial, old)
	entries := agent.Transcript()
	if len(entries) != 1 || entries[0].RunID != "old-run" || !entries[0].Interrupted {
		t.Fatalf("partial borrowed current turn: %+v", entries)
	}
}
