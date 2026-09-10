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

// A later observation is still unconsumed even when the provider repeats the
// earlier call's ID, arguments and output. The real folding gates must preserve
// that observation until another successful change uses it.
func TestTurnFoldRepeatedCallDoesNotInheritConsumption(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ContextWindow = bigTestWindow
	})
	agent.running = true
	agent.turnFloor = len(agent.messages)
	agent.messages = append(agent.messages, repeatedReadBatch()...)
	ep := &episode{agent: agent, seenThrough: len(agent.messages)}
	ep.markSeenReadsConsumed()
	for round := 1; round < 24; round++ {
		agent.messages = append(agent.messages, repeatedReadBatch()...)
	}
	before := append([]ai.Message(nil), agent.messages...)
	agent.foldTurnOutputs(len(agent.messages), ep.consumedReads, nil)
	if !reflect.DeepEqual(agent.messages, before) {
		t.Fatal("a repeated call inherited an earlier observation's consumption and was folded")
	}

	// Once a later change has used all of these observations, ordinary folding
	// still reclaims the working set rather than disabling compaction altogether.
	ep.seenThrough = len(agent.messages)
	ep.markSeenReadsConsumed()
	agent.foldTurnOutputs(len(agent.messages), ep.consumedReads, nil)
	if reflect.DeepEqual(agent.messages, before) {
		t.Fatal("consumed observations no longer fold above the working-set limit")
	}
}

// Moving retained messages must not move their consumption to a different
// occurrence. General compaction can rebuild the slice around its kept tail.
func TestTurnFoldConsumptionSurvivesRetainedMessageMovement(t *testing.T) {
	agent := &Agent{messages: repeatedReadBatch()}
	ep := &episode{agent: agent, seenThrough: len(agent.messages)}
	ep.markSeenReadsConsumed()
	agent.messages = append([]ai.Message{{Role: "user"}}, agent.messages...)
	agent.messages = append(agent.messages, repeatedReadBatch()...)
	batches := turnFoldBatches(agent.messages, 0, len(agent.messages), ep.consumedReads)
	if len(batches) != 1 || !reflect.DeepEqual(batches[0].indices, []int{2}) {
		t.Fatalf("consumption followed an ID or index instead of the retained call: %+v", batches)
	}
}
