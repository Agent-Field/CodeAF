package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the harness ─────────────────────────────────────────────────────────────

// staticTool answers every call with the same text, however long.
func staticTool(name, output string) bare.Tool {
	return bare.Tool{
		Name:        name,
		Description: "returns a fixed answer",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return output, false, nil
		},
	}
}

// oneCallThenAnswer scripts a turn that calls a tool once and then answers.
func oneCallThenAnswer(id, name string) []step {
	return []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, name, "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}
}

// answerOnly scripts a turn with no tool call at all.
func answerOnly() []step {
	return []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	}}
}

// toolTexts is every tool-result message in the live transcript, in order.
func toolTexts(a *Agent) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var texts []string
	for _, message := range a.messages {
		if message.Role == "tool" {
			texts = append(texts, messageContentText(message))
		}
	}
	return texts
}

const heavyBytes = 5000

func heavyOutput(marker string) string {
	return marker + strings.Repeat("x", heavyBytes)
}

// ── the pass ────────────────────────────────────────────────────────────────

// A heavy result from an old turn becomes a pointer to its own bytes; the bytes
// are on disk; the journal keeps the whole thing.
func TestStubReplacesOldHeavyResultsAndKeepsTheBytes(t *testing.T) {
	heavy := heavyOutput("BIGREAD")
	steps := oneCallThenAnswer("call-1", "fat")
	for turn := 0; turn < 4; turn++ {
		steps = append(steps, answerOnly()...)
	}
	completer := &scriptedCompleter{steps: steps}

	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = journal
	})
	agent.tools = append(agent.tools, staticTool("fat", heavy))

	for turn := 1; turn <= 5; turn++ {
		events, err := agent.Submit(context.Background(), fmt.Sprintf("turn %d", turn))
		if err != nil {
			t.Fatalf("Submit %d: %v", turn, err)
		}
		collect(t, events)
		if turn != 1 {
			continue
		}
		// The turn that PRODUCED it is the most recent turn there is: the result
		// is still whole.
		if texts := toolTexts(agent); len(texts) != 1 || texts[0] != heavy {
			t.Fatal("a result was stubbed in the turn that produced it")
		}
	}

	texts := toolTexts(agent)
	if len(texts) != 1 {
		t.Fatalf("tool messages: got %d, want 1", len(texts))
	}
	if !strings.HasPrefix(texts[0], stubMarker) {
		t.Fatalf("the old heavy result was not stubbed: %.80q", texts[0])
	}
	if !strings.Contains(texts[0], fmt.Sprintf("%d bytes", len(heavy))) {
		t.Fatalf("the stub does not say how big the result was: %q", texts[0])
	}

	// The path in the stub is relative to the workspace — what the read tool
	// takes — and the file behind it is the result, byte for byte.
	path := stubPathIn(t, texts[0])
	bytes, err := os.ReadFile(filepath.Join(workspace, path))
	if err != nil {
		t.Fatalf("the stub names a path with nothing behind it: %v", err)
	}
	if string(bytes) != heavy {
		t.Fatalf("the artifact is %d bytes, the result was %d", len(bytes), len(heavy))
	}

	// The journal is the record and the record is whole.
	journaled, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !strings.Contains(string(journaled), strings.Repeat("x", heavyBytes)) {
		t.Fatal("the journal lost the result it recorded — only the live context may be stubbed")
	}
	if strings.Contains(string(journaled), stubMarker) {
		t.Fatal("a stub line was written to the journal")
	}
}

// Recent results and small results are left exactly as they are.
func TestStubLeavesRecentAndSmallResultsAlone(t *testing.T) {
	heavy := heavyOutput("RECENT")
	small := "a short answer"

	var steps []step
	steps = append(steps, oneCallThenAnswer("small-1", "thin")...)
	for turn := 0; turn < 4; turn++ {
		steps = append(steps, answerOnly()...)
	}
	steps = append(steps, oneCallThenAnswer("heavy-1", "fat")...)
	completer := &scriptedCompleter{steps: steps}

	agent, _ := newTestAgent(t, completer, nil)
	agent.tools = append(agent.tools, staticTool("thin", small), staticTool("fat", heavy))

	for turn := 1; turn <= 6; turn++ {
		events, err := agent.Submit(context.Background(), fmt.Sprintf("turn %d", turn))
		if err != nil {
			t.Fatalf("Submit %d: %v", turn, err)
		}
		collect(t, events)
	}

	texts := toolTexts(agent)
	if len(texts) != 2 {
		t.Fatalf("tool messages: got %d, want 2", len(texts))
	}
	if texts[0] != small {
		t.Fatalf("a small old result was stubbed: %q", texts[0])
	}
	if texts[1] != heavy {
		t.Fatalf("the newest turn's result was stubbed: %.80q", texts[1])
	}
}

// The estimate reads the transcript, so a stubbed result stops weighing what it
// weighed — no bookkeeping, no second number to keep in step.
func TestStubbingShrinksTheContextEstimate(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	heavy := heavyOutput("WEIGHT")

	agent.mu.Lock()
	for turn := 1; turn <= 6; turn++ {
		agent.messages = append(agent.messages,
			textMessage("user", fmt.Sprintf("turn %d", turn)),
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
				ID: fmt.Sprintf("c%d", turn), Function: ai.ToolCallFunction{Name: "fat", Arguments: "{}"},
			}}},
			ai.Message{Role: "tool", ToolCallID: fmt.Sprintf("c%d", turn),
				Content: []ai.ContentPart{{Type: "text", Text: heavy}}},
		)
	}
	agent.mu.Unlock()

	before := agent.ContextTokens()
	agent.stubOldOutputs()
	after := agent.ContextTokens()

	// Six turns, four kept: two heavy results become two lines.
	if shrunk := before - after; shrunk < 2*heavyBytes/bytesPerToken-100 {
		t.Fatalf("the estimate fell by %d tokens, want about %d", shrunk, 2*heavyBytes/bytesPerToken)
	}

	// And a second pass is a no-op: a stub is never stubbed again, and no second
	// artifact is written for it.
	settled := agent.ContextTokens()
	agent.stubOldOutputs()
	if agent.ContextTokens() != settled {
		t.Fatal("a second pass changed a transcript it had already stubbed")
	}
	entries, err := os.ReadDir(droppingsDir(Place{}, workspace, droppingStubs))
	if err != nil {
		t.Fatalf("read stub directory: %v", err)
	}
	// Both stubbed results are the same bytes, so they are one file: the artifact
	// is named by its own digest.
	if len(entries) != 1 {
		t.Fatalf("stub artifacts: got %d, want 1", len(entries))
	}
}

// The cut is the Nth user message from the end, and a conversation shorter than
// that has nothing old enough to stub.
func TestStubCutKeepsTheLastFourTurns(t *testing.T) {
	messages := []ai.Message{textMessage("system", "SYSTEM")}
	if cut := stubCut(messages); cut != 0 {
		t.Fatalf("empty conversation: cut %d, want 0", cut)
	}
	var starts []int
	for turn := 1; turn <= 6; turn++ {
		starts = append(starts, len(messages))
		messages = append(messages,
			textMessage("user", "ask"),
			textMessage("assistant", "answer"))
	}
	if cut, want := stubCut(messages), starts[len(starts)-stubKeepTurns]; cut != want {
		t.Fatalf("cut %d, want %d (the %dth user message from the end)", cut, want, stubKeepTurns)
	}
	if cut := stubCut(messages[:starts[2]]); cut != 0 {
		t.Fatalf("two turns: cut %d, want 0 — nothing is old enough yet", cut)
	}
}

// A session with no workspace has nowhere to put the bytes, so it stubs nothing:
// a stub whose artifact was never written would be a pointer to nothing.
func TestStubDoesNothingWithoutAWorkspaceToWriteTo(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	heavy := heavyOutput("NOWHERE")
	agent.mu.Lock()
	for turn := 1; turn <= 6; turn++ {
		agent.messages = append(agent.messages,
			textMessage("user", "ask"),
			ai.Message{Role: "tool", ToolCallID: fmt.Sprintf("c%d", turn),
				Content: []ai.ContentPart{{Type: "text", Text: heavy}}})
	}
	agent.config.Workspace = ""
	agent.mu.Unlock()

	agent.stubOldOutputs()
	for _, text := range toolTexts(agent) {
		if strings.HasPrefix(text, stubMarker) {
			t.Fatal("a result was stubbed with nowhere to write the bytes")
		}
	}
}

// stubPathIn pulls the artifact path out of a stub line.
func stubPathIn(t *testing.T, line string) string {
	t.Helper()
	_, tail, found := strings.Cut(line, "full output: ")
	if !found {
		t.Fatalf("the stub names no path: %q", line)
	}
	return strings.TrimSuffix(strings.TrimSpace(tail), "]")
}
