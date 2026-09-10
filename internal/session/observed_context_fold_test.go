package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Research makes no file changes. Observations that the next decision has read
// must still become recoverable views once they leave the recent working set.
func TestObservedContextFoldsResearchThroughSubmitAndReplay(t *testing.T) {
	const rounds = 32
	output := "OBSERVATION HEAD\n" + strings.Repeat("middle evidence\n", 1000) + "FAILED BEHAVIORAL EXAMPLE\n"
	var steps []step
	for round := 0; round < rounds; round++ {
		round := round
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			response := toolResponseWithText("reused-read", "read", fmt.Sprintf(`{"path":"evidence-%d"}`, round), fmt.Sprintf("research round %d", round))
			response.Choices[0].Message.ToolCalls = append(response.Choices[0].Message.ToolCalls, ai.ToolCall{ID: "reused-shell", Function: ai.ToolCallFunction{Name: "bash", Arguments: fmt.Sprintf(`{"command":"observe %d"}`, round)}})
			return response, nil
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("research complete"), nil
	})
	completer := &scriptedCompleter{steps: steps}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = bigTestWindow
		config.CompactEnabled = false
		config.SessionFile = journal
	})
	for index := range agent.tools {
		if agent.tools[index].Name == "read" || agent.tools[index].Name == "bash" {
			name := agent.tools[index].Name
			agent.tools[index] = staticTool(name, output)
			agent.tools[index].Execute = func(_ context.Context, args json.RawMessage) (string, bool, error) {
				return name + "\n" + string(args) + "\n" + output, false, nil
			}
		}
	}
	events, err := agent.Submit(context.Background(), "Investigate this example without changing files.")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	found := map[string]bool{}
	for request := 0; request < completer.requests(); request++ {
		messages := completer.request(request)
		calls := toolResultCalls(messages)
		for index, message := range messages {
			text := messageContentText(message)
			if message.Role != "tool" || !strings.HasPrefix(text, compactReducedMarker) {
				continue
			}
			if !strings.Contains(text, "OBSERVATION HEAD") || !strings.Contains(text, "FAILED BEHAVIORAL EXAMPLE") {
				t.Fatalf("lost head or failure tail: %s", text)
			}
			header, _, _ := strings.Cut(text, "\n")
			path := stubPathIn(t, header)
			if !filepath.IsAbs(path) {
				path = filepath.Join(workspace, path)
			}
			full, err := os.ReadFile(path)
			call := calls[index]
			if call == nil {
				t.Fatal("reduced result lost its own call")
			}
			want := call.Function.Name + "\n" + call.Function.Arguments + "\n" + output
			if err != nil || string(full) != want {
				t.Fatalf("full result unavailable: %v", err)
			}
			found[message.ToolCallID] = true
		}
	}
	if !found["reused-read"] || !found["reused-shell"] {
		t.Fatalf("research did not fold both observations: %v", found)
	}
	agent.mu.Lock()
	before := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()
	if !holdsText(before, "Investigate this example without changing files.") || !holdsText(before, "research round 0") {
		t.Fatal("fold changed the original request or assistant history")
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := newAgent(Config{Workspace: workspace, Model: "test/model", System: "SYSTEM", SessionFile: journal, ContextWindow: bigTestWindow}, &refusingCompleter{t: t})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if !reflect.DeepEqual(resumed.messages, before) {
		t.Fatal("replayed window differs from the live window")
	}
}

func TestObservedContextAcceptsUsefulPartialReclamation(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.ContextWindow = bigTestWindow })
	agent.running = true
	agent.turnFloor = len(agent.messages)
	for round := 0; round < 16; round++ {
		agent.messages = append(agent.messages, repeatedReadBatch()...)
	}
	horizon := len(agent.messages)
	// Unseen results alone exceed the lower target. They cannot veto useful
	// reclamation of the older observations, nor can they be rewritten themselves.
	for round := 0; round < 20; round++ {
		agent.messages = append(agent.messages, repeatedReadBatch()...)
	}
	before := append([]ai.Message(nil), agent.messages...)
	agent.foldTurnOutputs(horizon, nil)
	if reflect.DeepEqual(agent.messages, before) {
		t.Fatal("a protected remainder vetoed useful partial reclamation")
	}
	if !reflect.DeepEqual(agent.messages[horizon:len(before)], before[horizon:]) {
		t.Fatal("partial reclamation changed unseen observations")
	}
	if turnToolBytes(agent.messages, agent.turnFloor) <= turnWorkingTarget(agent.window())*bytesPerToken {
		t.Fatal("fixture no longer exercises an unreachable lower target")
	}
	first := append([]ai.Message(nil), agent.messages...)
	agent.foldTurnOutputs(horizon, nil)
	if !reflect.DeepEqual(agent.messages, first) {
		t.Fatal("a second pass rewrote an already-reduced result")
	}
}

// An old completed batch is indivisible even when its sibling is only one
// message beyond the observation horizon. Provider IDs may repeat elsewhere.
func TestObservedContextKeepsPartialAndMismatchedBatches(t *testing.T) {
	messages := repeatedReadBatch()
	messages[0].ToolCalls = append(messages[0].ToolCalls, ai.ToolCall{ID: "shell", Function: ai.ToolCallFunction{Name: "bash"}})
	messages = append(messages, ai.Message{Role: "tool", ToolCallID: "shell", Content: []ai.ContentPart{{Type: "text", Text: turnFoldOutput()}}})
	if batches := turnFoldBatches(messages, 0, 2); len(batches) != 0 {
		t.Fatalf("partial batch folded: %+v", batches)
	}
	if batches := turnFoldBatches(messages, 0, 3); len(batches) != 1 || len(batches[0].indices) != 2 {
		t.Fatalf("complete mixed batch unavailable: %+v", batches)
	}
	messages[2].ToolCallID = "unrelated"
	if batches := turnFoldBatches(messages, 0, 3); len(batches) != 0 {
		t.Fatalf("unmatched result made a complete batch: %+v", batches)
	}
}

func TestObservedContextDoesNotFoldUnrecoverableResults(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.ContextWindow = bigTestWindow })
	agent.running = true
	agent.turnFloor = len(agent.messages)
	for round := 0; round < 24; round++ {
		agent.messages = append(agent.messages, repeatedReadBatch()...)
	}
	// No workspace and no journal means no honest retrievable original.
	agent.config.Workspace = ""
	agent.file = nil
	before := append([]ai.Message(nil), agent.messages...)
	agent.foldTurnOutputs(len(agent.messages), nil)
	if !reflect.DeepEqual(agent.messages, before) {
		t.Fatal("unrecoverable result was reduced")
	}
}

// Economical text views must retain other observations and leave the earlier
// request's shared content slice untouched.
func TestObservedContextTextViewPreservesOtherContent(t *testing.T) {
	message := ai.Message{Role: "tool", ToolCallID: "mixed", Content: []ai.ContentPart{{Type: "text", Text: "head"}, {Type: "image"}, {Type: "text", Text: "tail"}}}
	before := append([]ai.ContentPart(nil), message.Content...)
	reduced := replaceToolText(message, "view")
	if !reflect.DeepEqual(message.Content, before) {
		t.Fatal("retained content was mutated")
	}
	if len(reduced.Content) != 2 || reduced.Content[0].Text != "view" || !reflect.DeepEqual(reduced.Content[1], before[1]) {
		t.Fatalf("non-text observation was lost: %+v", reduced.Content)
	}
}
