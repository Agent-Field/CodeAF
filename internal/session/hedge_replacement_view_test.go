package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const hedgeReplacementLine = "that lane went quiet — this answer is coming from another one"

// A replacement mid-stream discards the dead answer from the live page just as
// it does from the journal. Replaying the public events as a surface does must
// leave one whole answer, with the replacement line carried by the discard.
func TestAHedgeReplacementWithdrawsTheVisiblePartialAnswer(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "the dead lane's half answer")
			provider.Emit(ctx, provider.StreamReplaced, hedgeReplacementLine)
			provider.Emit(ctx, provider.StreamDelta, "the rescued answer")
			return textResponse("the rescued answer"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	collected := collect(t, mustSubmit(t, agent, "answer the question"))
	var visible strings.Builder
	for _, event := range collected {
		switch event.Kind {
		case EventTextDelta:
			visible.WriteString(event.Text)
		case EventRetrying:
			visible.Reset()
		case EventError:
			t.Fatalf("the replacement did not finish: %v", event.Err)
		}
	}
	kept := messageText(lastMessage(agent))
	if kept != "the rescued answer" {
		t.Fatalf("journal answer = %q", kept)
	}
	if visible.String() != kept {
		t.Fatalf("visible answer = %q, journal = %q; events: %v", visible.String(), kept, kinds(collected))
	}
	if got := textsOfKind(collected, EventRetrying); len(got) != 1 || got[0] != hedgeReplacementLine {
		t.Fatalf("replacement announcements = %q, want the one hedge line", got)
	}
	for _, notice := range textsOfKind(collected, EventNotice) {
		if notice == hedgeReplacementLine {
			t.Fatalf("the replacement arrived as a plain notice: %q", notice)
		}
	}
}

// Stopping after the rescue takes over keeps only the rescuing lane's partial
// answer. The dead lane's words are void at the replacement boundary and may
// not be joined to the answer the interrupt records.
func TestStoppingAHedgeReplacementNeverJournalsTheDeadPartial(t *testing.T) {
	takenOver := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "the dead lane's half answer")
			provider.Emit(ctx, provider.StreamReplaced, hedgeReplacementLine)
			provider.Emit(ctx, provider.StreamDelta, "the rescuing lane's partial answer")
			close(takenOver)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	events := mustSubmit(t, agent, "answer the question")
	<-takenOver
	agent.Interrupt()
	collected := collect(t, events)
	if got := messageText(lastMessage(agent)); got != "the rescuing lane's partial answer" {
		t.Fatalf("stopping after the rescue kept %q", got)
	}
	if got := countOfKind(collected, EventRetrying); got != 1 {
		t.Fatalf("replacement announcements = %d, want exactly one", got)
	}
}

// An adapter notice only explains how the request was reshaped. It remains a
// plain note, withdraws nothing, and leaves the streamed answer whole on both
// the page and in the journal.
func TestAProviderNoticeDoesNotWithdrawTheVisibleAnswer(t *testing.T) {
	const notice = "Retry 1/3: dropped tools"
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "the first half ")
			provider.Emit(ctx, provider.StreamNotice, notice)
			provider.Emit(ctx, provider.StreamDelta, "and the second half")
			return textResponse("the first half and the second half"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	collected := collect(t, mustSubmit(t, agent, "answer the question"))
	var visible strings.Builder
	for _, event := range collected {
		if event.Kind == EventTextDelta {
			visible.WriteString(event.Text)
		}
	}
	if got := textsOfKind(collected, EventNotice); len(got) != 1 || got[0] != notice {
		t.Fatalf("provider notices = %q, want the request-reshaping line", got)
	}
	if got := countOfKind(collected, EventRetrying); got != 0 {
		t.Fatalf("a plain notice withdrew the answer %d times", got)
	}
	kept := messageText(lastMessage(agent))
	if visible.String() != kept {
		t.Fatalf("visible answer = %q, journal = %q", visible.String(), kept)
	}
}
