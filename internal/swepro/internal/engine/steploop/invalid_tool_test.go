package steploop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/orclient"
)

type releasedTool struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (tool releasedTool) Execute(_ context.Context, _ ToolCall) (ToolResult, error) {
	close(tool.started)
	<-tool.release
	return ToolResult{Title: "done", Output: "answered", Metadata: msgmodel.RawObject("{}")}, nil
}

func TestWaitForResultDefinitionKeepsProcessorStreamOpen(t *testing.T) {
	fixedSeams(t)
	store := &memoryStore{}
	assistant := baseAssistant("msg_0001", "msg_0000", "coder", "", nil).Info.(msgmodel.Assistant)
	assistant.Finish = nil
	if err := store.UpdateMessage(context.Background(), assistant); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	processor := NewProcessor(ProcessorOptions{
		Store: store, Assistant: assistant,
		Tools: []ToolDefinition{{
			Provider:      orclient.Tool{Type: "function", Name: "question"},
			WaitForResult: true,
		}},
		Executor: releasedTool{started: started, release: release},
	})
	stream := &SliceStream{Parts: []orclient.StreamPart{
		orclient.ToolInputStartPart{ID: "call_1", ToolName: "question"},
		orclient.ToolCallPart{ToolCallID: "call_1", ToolName: "question", Input: `{}`},
		finishPart(orclient.FinishToolCalls),
	}}
	done := make(chan error, 1)
	go func() {
		_, err := processor.Process(context.Background(), stream)
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("processor finished while result-blocking tool was pending: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	parts := store.rawSnapshot()[0].Parts
	toolPart := parts[1].(msgmodel.ToolPart)
	if toolPart.State.ToolStatus() != msgmodel.ToolStatusCompleted {
		t.Fatalf("tool state = %s", fixtureStringify(t, toolPart.State))
	}
}

func TestUnknownToolCompletesAsInvalidAndContinues(t *testing.T) {
	fixedSeams(t)
	store := &memoryStore{}
	assistant := baseAssistant("msg_0001", "msg_0000", "coder", "", nil).Info.(msgmodel.Assistant)
	assistant.Finish = nil
	if err := store.UpdateMessage(context.Background(), assistant); err != nil {
		t.Fatal(err)
	}
	processor := NewProcessor(ProcessorOptions{
		Store:     store,
		Assistant: assistant,
		Tools: []ToolDefinition{{Provider: orclient.Tool{
			Type: "function", Name: "read",
		}}},
	})
	stream := &SliceStream{Parts: []orclient.StreamPart{
		orclient.ToolInputStartPart{ID: "call_1", ToolName: "missing"},
		orclient.ToolCallPart{ToolCallID: "call_1", ToolName: "missing", Input: `{}`},
		finishPart(orclient.FinishToolCalls),
	}}

	result, err := processor.Process(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	if result != ResultContinue {
		t.Fatalf("Process result = %q, want %q", result, ResultContinue)
	}

	parts := store.rawSnapshot()[0].Parts
	assertPartTypes(t, parts, "step-start,tool,step-finish")
	toolPart := parts[1].(msgmodel.ToolPart)
	if toolPart.Tool != orclient.InvalidToolName || toolPart.State.ToolStatus() != msgmodel.ToolStatusCompleted {
		t.Fatalf("unknown tool state = %#v", toolPart)
	}
	state := fixtureStringify(t, toolPart.State)
	wantOutput := "The arguments provided to the tool are invalid: " +
		"Model tried to call unavailable tool 'missing'. Available tools: invalid, read."
	for _, fragment := range []string{
		`"title":"Invalid Tool"`,
		`"metadata":{}`,
		`"output":` + fixtureStringify(t, wantOutput),
	} {
		if !strings.Contains(state, fragment) {
			t.Fatalf("invalid state %s missing %s", state, fragment)
		}
	}
}
