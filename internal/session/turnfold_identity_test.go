package session

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func repeatedReadBatch() []ai.Message {
	return []ai.Message{
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "call_0", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"same.txt"}`}}}},
		{Role: "tool", ToolCallID: "call_0", Content: []ai.ContentPart{{Type: "text", Text: turnFoldOutput()}}},
	}
}

// A repeated ID, command and result cannot make an unseen occurrence observed.
// The horizon belongs to this transcript rather than to a provider's ID map.
func TestTurnFoldRepeatedCallDoesNotInheritObservation(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.ContextWindow = bigTestWindow })
	agent.running = true
	agent.turnFloor = len(agent.messages)
	agent.messages = append(agent.messages, repeatedReadBatch()...)
	horizon := len(agent.messages)
	for round := 1; round < 24; round++ {
		agent.messages = append(agent.messages, repeatedReadBatch()...)
	}
	before := append([]ai.Message(nil), agent.messages...)
	agent.foldTurnOutputs(horizon, nil)
	if !reflect.DeepEqual(agent.messages, before) {
		t.Fatal("an unseen repeated call was folded or a trivial saving broke the cache")
	}
	agent.foldTurnOutputs(len(agent.messages), nil)
	if reflect.DeepEqual(agent.messages, before) {
		t.Fatal("observed results no longer fold above the working-set limit")
	}
}

// Moving retained messages cannot attach an old result to a later call with the
// same ID. Only complete batches inside the current observation horizon qualify.
func TestTurnFoldBatchIdentitySurvivesRetainedMessageMovement(t *testing.T) {
	messages := append([]ai.Message{{Role: "user"}}, repeatedReadBatch()...)
	messages = append(messages, repeatedReadBatch()...)
	batches := turnFoldBatches(messages, 0, 3)
	if len(batches) != 1 || !reflect.DeepEqual(batches[0].indices, []int{2}) {
		t.Fatalf("wrong observed occurrence: %+v", batches)
	}
	messages[1].ToolCalls = append(messages[1].ToolCalls, ai.ToolCall{ID: "missing", Function: ai.ToolCallFunction{Name: "bash"}})
	if batches := turnFoldBatches(messages, 0, 3); len(batches) != 0 {
		t.Fatalf("incomplete batch folded: %+v", batches)
	}
}
