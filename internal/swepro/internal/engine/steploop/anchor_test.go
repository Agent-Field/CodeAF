package steploop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/orclient"
)

// vanishingUserStore is a store whose user messages disappear from the reads
// after the first one. It is the shape of every way the loop's anchor can go
// missing mid-run — a projection generation that has not caught up, a
// compaction filter that narrowed past it, a session read that raced a
// rewrite — reduced to the one fact the loop can actually observe.
type vanishingUserStore struct {
	*memoryStore
	reads int
}

func (s *vanishingUserStore) Messages(
	ctx context.Context, sessionID string,
) ([]msgmodel.WithParts, error) {
	msgs, err := s.memoryStore.Messages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	s.reads++
	if s.reads <= 1 {
		return msgs, nil
	}
	kept := make([]msgmodel.WithParts, 0, len(msgs))
	for _, msg := range msgs {
		if _, isUser := msg.Info.(msgmodel.User); !isUser {
			kept = append(kept, msg)
		}
	}
	return kept, nil
}

// A run that has already produced work does not throw that work away because
// the stream lost its anchor.
//
// Measured (audit-notes/headless-regression-audit.md §10, defect 1): a coding
// run whose deliverable built clean and passed tests in all six packages was
// reported to aforge as `the coding pipeline crashed: No user message found in
// stream. This should never happen.` — a provider failure recorded against a
// working result, in the same measured lines that decide which worker gets the
// next coding job. The anchor is gone, so there is nothing further to answer;
// that is an ending, not a crash.
func TestLostAnchorAfterWorkEndsTheLoopInsteadOfFailingTheRun(t *testing.T) {
	fixedSeams(t)
	store := &vanishingUserStore{memoryStore: &memoryStore{
		messages: []msgmodel.WithParts{baseUser("msg_0000", "build", msgmodel.Parts{
			msgmodel.TextPart{
				PartBase: msgmodel.PartBase{ID: "prt_0000", SessionID: "ses_1", MessageID: "msg_0000"},
				Text:     "double one",
			},
		})},
	}}
	client := &scriptedClient{scripts: [][]orclient.StreamPart{{
		orclient.ToolInputStartPart{ID: "call_1", ToolName: "double"},
		orclient.ToolCallPart{ToolCallID: "call_1", ToolName: "double", Input: `{"x":1}`},
		finishPart(orclient.FinishToolCalls),
	}}}

	loop := Loop{Store: store, Client: client, Models: testResolver(), Executor: &immediateTool{}}
	final, err := loop.Run(context.Background(), RunOptions{
		SessionID: "ses_1", Workspace: "/work", Worktree: "/work",
		Tools: []ToolDefinition{{Provider: orclient.Tool{
			Type: "function", Name: "double", Description: "double",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}}},
	})
	if err != nil {
		t.Fatalf("a finished turn was discarded because the anchor went missing: %v", err)
	}
	if final.ID == "" {
		t.Fatal("the run returned no assistant message at all")
	}
	if len(client.requests) != 1 {
		t.Fatalf("LLM turns = %d, want the one turn that ran", len(client.requests))
	}
}

// The genuine version of the error is still fatal: a session that has never
// carried a prompt has nothing to run, and saying so is the only honest answer.
func TestNoPromptAtAllIsStillARefusal(t *testing.T) {
	fixedSeams(t)
	store := &memoryStore{messages: []msgmodel.WithParts{
		baseAssistant("msg_0000", "msg_none", "build", orclient.FinishStop, msgmodel.Parts{}),
	}}
	loop := Loop{Store: store, Client: &scriptedClient{}, Models: testResolver(), Executor: &immediateTool{}}
	_, err := loop.Run(context.Background(), RunOptions{
		SessionID: "ses_1", Workspace: "/work", Worktree: "/work",
	})
	if err == nil || !strings.Contains(err.Error(), "No user message found in stream") {
		t.Fatalf("error = %v, want the no-prompt refusal", err)
	}
}
