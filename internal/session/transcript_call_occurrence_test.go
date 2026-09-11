package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Real execution, saved history, and rewind must agree about which command
// failed even when the provider restarts its call numbering on every response.
func TestRepeatedCallIDsKeepExplanationHistoryAndRewindInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call_0", "bash", `{"command":"exit 3"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call_0", "bash", `{"command":"echo second-result"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("finished"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
	collect(t, mustSubmit(t, agent, "Run the two commands and report their results."))
	why := agent.Why()
	for _, want := range []string{"ran exit 3 (failed)", "ran echo second-result (ok)"} {
		if !strings.Contains(why, want) {
			t.Fatalf("Why lost %q: %s", want, why)
		}
	}
	for name, entries := range map[string][]DisplayEntry{
		"live":    agent.Transcript(),
		"journal": ReadTranscript(path).Entries,
	} {
		var calls []DisplayEntry
		for _, entry := range entries {
			if entry.Tool != "" {
				calls = append(calls, entry)
			}
		}
		if len(calls) != 2 || !calls[0].Answered || !calls[1].Answered || !bashFailed(calls[0].Output) || !strings.Contains(calls[1].Output, "second-result") || strings.Contains(calls[0].Output, "second-result") {
			t.Fatalf("%s mixed repeated calls: %+v", name, calls)
		}
	}
	agent.mu.Lock()
	second := -1
	seen := 0
	for index, message := range agent.messages {
		if len(message.ToolCalls) > 0 {
			seen++
			if seen == 2 {
				second = index
				break
			}
		}
	}
	agent.mu.Unlock()
	if second < 0 {
		t.Fatal("second call was not recorded")
	}
	// The first result is already behind this cut. A later result bearing the
	// same ID must neither hide the point nor keep that later command alive.
	if _, err := agent.RewindAt(second); err != nil {
		t.Fatalf("rewind between completed calls: %v", err)
	}
	agent.mu.Lock()
	tail := agent.messages[len(agent.messages)-1]
	agent.mu.Unlock()
	if tail.Role != "tool" || !bashFailed(messageContentText(tail)) {
		t.Fatalf("rewind retained the wrong result: %+v", tail)
	}
	if strings.Contains(agent.Why(), "second-result") {
		t.Fatal("rewind left the second command in the explanation")
	}
}

// A result belongs to its preceding batch, including when a later batch is
// still in flight and its provider reuses a completed call's ID.
func TestTranscriptDoesNotAnswerAPendingReusedCall(t *testing.T) {
	first := toolResponse("call_0", "bash", `{"command":"echo first"}`).Choices[0].Message
	pending := toolResponse("call_0", "bash", `{"command":"echo pending"}`).Choices[0].Message
	result := textMessage("tool", "first")
	result.ToolCallID = "call_0"
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, message := range []ai.Message{textMessage("user", "Run both."), first, result, pending} {
		agent.record(message)
	}
	var calls []DisplayEntry
	for _, entry := range agent.Transcript() {
		if entry.Tool != "" {
			calls = append(calls, entry)
		}
	}
	if len(calls) != 2 || !calls[0].Answered || calls[0].Output != "first" || calls[1].Answered || calls[1].Output != "" {
		t.Fatalf("pending call borrowed an earlier result: %+v", calls)
	}
}

// Reserved legacy text remains hidden on replay; a person's ordinary prefix
// stays visible, and hiding context does not remove it from the model's record.
func TestLegacyCarryOnRemainsContextWhenReopened(t *testing.T) {
	const legacy = "[carry on] You stopped, but what was asked is not finished. Somebody reading the work against the request says this is what is left. Carry on with it, and do not summarise what you have already done:\nCheck the remaining file."
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	for _, message := range []ai.Message{
		textMessage("user", "Finish the work."), textMessage("user", legacy),
		textMessage("assistant", "Done."), textMessage("user", "[carry on] Please continue."),
	} {
		agent.record(message)
	}
	for name, entries := range map[string][]DisplayEntry{"live": agent.Transcript(), "journal": ReadTranscript(path).Entries} {
		if len(entries) != 3 || entries[0].Text != "Finish the work." || entries[1].Text != "Done." || entries[2].Text != "[carry on] Please continue." {
			t.Fatalf("%s rendered legacy context or hid the person: %+v", name, entries)
		}
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if countEntries(agent.messages) != len(displayEntries(agent.messages)) {
		t.Fatal("compaction count disagrees with history")
	}
	found := false
	for _, message := range agent.messages {
		if messageContentText(message) == legacy {
			found = true
		}
	}
	if !found {
		t.Fatal("display filtering deleted model context")
	}
}
