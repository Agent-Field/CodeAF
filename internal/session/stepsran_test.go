package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// A model whose own program runs its tools (Claude Code) hands its finished
// steps up the stream. Each is drawn as a row, kept in the conversation's
// file, and read by the next model as a call and its result — and nothing
// runs it a second time.
func TestStepsAnotherProgramRanAreDrawnKeptAndReadByTheNextModel(t *testing.T) {
	c := &scriptedCompleter{}
	file := filepath.Join(t.TempDir(), "chat.jsonl")
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.SessionFile = file })
	c.steps = []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		provider.EmitEvent(ctx, provider.StreamEvent{Kind: provider.StreamStepsRan,
			Delta: `[{"id":"claude_code_t1","name":"bash","arguments":{"command":"echo ran-elsewhere > proof.txt"},"output":"","took_ms":40},` +
				`{"id":"claude_code_t2","name":"read","arguments":{"path":"proof.txt"},"output":"ran-elsewhere","took_ms":5}]`})
		return textResponse("done"), nil
	}}
	ch, err := a.Submit(t.Context(), "make the proof")
	if err != nil {
		t.Fatal(err)
	}
	var began, ended []string
	for _, ev := range collect(t, ch) {
		switch ev.Kind {
		case EventToolBegin:
			began = append(began, ev.Tool)
		case EventToolEnd:
			ended = append(ended, ev.Tool+":"+ev.Output)
		}
	}
	if strings.Join(began, ",") != "bash,read" || strings.Join(ended, ",") != "bash:,read:ran-elsewhere" {
		t.Fatalf("rows began %v ended %v", began, ended)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	replay, err := replaySessionFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// user, two call/result pairs, the answer.
	if len(replay.messages) != 6 || messageContentText(replay.messages[5]) != "done" {
		t.Fatalf("the file kept %d messages: %+v", len(replay.messages), replay.messages)
	}
	c.steps = []step{func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		for _, msg := range messages {
			if msg.Role == "tool" && msg.ToolCallID == "claude_code_t2" && messageContentText(msg) == "ran-elsewhere" {
				return textResponse("I can see what it did."), nil
			}
		}
		t.Error("the next model's request lost the steps another program ran")
		return textResponse("missing"), nil
	}}
	resumed, err := newAgent(a.config, c)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	ch, err = resumed.Submit(t.Context(), "What did it do?")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
}
