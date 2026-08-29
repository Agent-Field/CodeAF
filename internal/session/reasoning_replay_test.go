package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func assertCarriedReasoning(t *testing.T, ctx context.Context, text string, details json.RawMessage) {
	t.Helper()
	for _, carried := range provider.MessageReasoningFrom(ctx) {
		if carried.Text != text {
			continue
		}
		if carried.Field != "reasoning_content" {
			t.Fatalf("reasoning field = %q, want reasoning_content", carried.Field)
		}
		if string(carried.Details) != string(details) {
			t.Fatalf("reasoning details = %s, want byte-identical %s", carried.Details, details)
		}
		return
	}
	t.Fatalf("next request carried no reasoning %q: %#v", text, provider.MessageReasoningFrom(ctx))
}

func TestToolLoopPassesStreamedReasoningBackOnTheAssistantMessage(t *testing.T) {
	details := json.RawMessage(`[{"type":"reasoning.text","text":"kept"}]`)
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.EmitReasoning(ctx, "reasoning_content", "the file explains it", details)
			return toolResponse("read-1", "read", `{"path":"note.txt"}`), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			assertCarriedReasoning(t, ctx, "the file explains it", details)
			return textResponse("Done. What next?"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("answer"), 0o600); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "read the note"))
	if completer.requests() < 2 {
		t.Fatalf("requests = %d, want the tool round and continuation", completer.requests())
	}
}

func TestResumedSessionReplaysJournaledReasoning(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	details := json.RawMessage(`[{"type":"reasoning.text","text":"resume"}]`)
	firstClient := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.EmitReasoning(ctx, "reasoning_content", "durable thought", details)
			return toolResponse("read-1", "read", `{"path":"note.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Done. What next?"), nil
		},
	}}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("answer"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{Workspace: dir, Model: "test/model", System: "SYSTEM", SessionFile: transcript}
	first, err := newAgent(config, firstClient)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, first, "read it"))
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	secondClient := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		assertCarriedReasoning(t, ctx, "durable thought", details)
		return textResponse("Still here. What next?"), nil
	}}}
	second, err := newAgent(config, secondClient)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	collect(t, mustSubmit(t, second, "continue"))
}

func TestHedgeLoserReasoningNeverReachesTheWinningStep(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			<-ctx.Done()
			provider.EmitReasoning(ctx, "reasoning_content", "loser", nil)
			return nil, ctx.Err()
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.EmitReasoning(ctx, "reasoning_content", "winner", nil)
			return textResponse("winner"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	partial := &partialBuffer{}
	reasoning := &reasoningBuffer{}
	observer := func(event provider.StreamEvent) {
		if event.Kind == provider.StreamReasoning {
			reasoning.write(event)
		}
	}
	ctx := provider.WithStreamObserver(context.Background(), observer)
	ctx = withInteractiveHedge(ctx, observer, hedgeTestFloor)
	response, _, err := agent.completeWithRetryReasoning(ctx, newEventHub(), "hedge/reasoning", effort.None,
		partial, reasoning, &warmBatch{}, &formingBatch{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text() != "winner" {
		t.Fatalf("answer = %q", response.Text())
	}
	if got := reasoning.snapshot().Text; got != "winner" {
		t.Fatalf("kept reasoning = %q, want only winner", got)
	}
}

// The journal names the model each piece of working came from, so a resumed
// conversation that has switched models hands the provider a sidecar it can
// keep home (provider.MessageReasoning.Model) rather than a 404 to discover.
func TestJournaledReasoningRemembersWhichModelProducedIt(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	details := json.RawMessage(`[{"type":"reasoning.text","text":"resume"}]`)
	firstClient := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.EmitReasoning(ctx, "reasoning_content", "durable thought", details)
			return toolResponse("read-1", "read", `{"path":"note.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Done. What next?"), nil
		},
	}}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("answer"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{Workspace: dir, Model: "test/model", System: "SYSTEM", SessionFile: transcript}
	first, err := newAgent(config, firstClient)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, first, "read it"))
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	switched := config
	switched.Model = "test/other-model"
	secondClient := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		tagged := 0
		for _, carried := range provider.MessageReasoningFrom(ctx) {
			if carried.Text == "" && len(carried.Details) == 0 {
				continue
			}
			if carried.Model != "test/model" {
				t.Fatalf("carried working is tagged %q, want the model that produced it", carried.Model)
			}
			tagged++
		}
		if tagged == 0 {
			t.Fatal("the journaled working did not come back at all")
		}
		return textResponse("Still here."), nil
	}}}
	second, err := newAgent(switched, secondClient)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	collect(t, mustSubmit(t, second, "continue"))
}
