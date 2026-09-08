package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A CONTINUATION IS CONTEXT, NOT ANOTHER QUESTION. Reopening an older journal
// must retain the real exchange without putting the engine's prompt in it.
func TestReopenedConversationKeepsCarryOnOutOfTheExchange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	messages := []ai.Message{
		textMessage("user", "Please finish the work."),
		textMessage("assistant", "The first part is ready."),
		textMessage("user", checkpointCarryOnLead+"Check the remaining file."),
		textMessage("assistant", "The work is finished."),
		textMessage("user", "[carry on] Please continue with the next item."),
	}
	for _, message := range messages {
		agent.record(message)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	for name, entries := range map[string][]DisplayEntry{
		"memory":           displayEntries(messages),
		"reopened journal": ReadTranscript(path).Entries,
	} {
		t.Run(name, func(t *testing.T) {
			if len(entries) != 4 {
				t.Fatalf("exchange has %d entries, want four", len(entries))
			}
			for _, e := range entries {
				if strings.Contains(e.Text, checkpointCarryOnLead) {
					t.Fatal("the engine's continuation appeared as conversation")
				}
			}
			if entries[0].Text != "Please finish the work." || entries[2].Text != "The work is finished." || entries[3].Text != "[carry on] Please continue with the next item." {
				t.Fatalf("the person's words or the final answer changed: %+v", entries)
			}
		})
	}
}

// Compaction measures the same display list that replay later pages through.
func TestDisplayCountExcludesInternalContinuationContext(t *testing.T) {
	messages := []ai.Message{
		textMessage("user", "Please finish the work."),
		textMessage("user", checkpointCarryOnLead+"Check the remaining file."),
		textMessage("user", volatileNoteOpening+"temporary context"),
		textMessage("assistant", "The work is finished."),
	}
	if got, want := countEntries(messages), len(displayEntries(messages)); got != want {
		t.Fatalf("compaction counted %d display entries, replay draws %d", got, want)
	}
}
