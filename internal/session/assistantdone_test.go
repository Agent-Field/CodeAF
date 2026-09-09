package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestAssistantDoneFollowsContentAndPrecedesTurnDone(t *testing.T) {
	client := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "finished answer")
			return textResponse("finished answer"), nil
		},
	}}
	agent, _ := newTestAgent(t, client, nil)
	events := collect(t, mustSubmit(t, agent, "answer this"))

	content := eventIndex(events, EventTextDelta)
	boundary := eventIndex(events, EventAssistantDone)
	done := eventIndex(events, EventTurnDone)
	if content < 0 || boundary < content || done < boundary {
		t.Fatalf("event order = %v, want content then assistant boundary then turn done", kinds(events))
	}
	if got := eventCount(events, EventAssistantDone); got != 1 {
		t.Fatalf("assistant boundaries = %d, want 1", got)
	}
}

func TestAssistantDoneExcludesUnfinishedResponses(t *testing.T) {
	tests := []struct {
		name  string
		steps []step
	}{
		{
			name: "tool-bearing",
			steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("call-1", "read", `{"path":"missing"}`), nil
				},
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return nil, errors.New("stop after tool round")
				},
			},
		},
		{
			name: "empty",
			steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return emptyResponse(), nil
				},
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return nil, errors.New("stop after empty retry")
				},
			},
		},
		{
			name: "error",
			steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return nil, errors.New("provider failed")
				},
			},
		},
		{
			name: "truncated continuation",
			steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return finishedResponse("unfinished", "length"), nil
				},
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return nil, errors.New("stop after continuation")
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{steps: test.steps}, nil)
			events := collect(t, mustSubmit(t, agent, "answer this"))
			if got := eventCount(events, EventAssistantDone); got != 0 {
				t.Fatalf("assistant boundaries = %d, want none; events = %v", got, kinds(events))
			}
		})
	}
}

func TestTaskCatchupAssistantDoneDropsJournaledAnswer(t *testing.T) {
	var catchup taskCatchup
	catchup.record(Event{Kind: EventThinking})
	catchup.record(Event{Kind: EventReasoning, Text: "considering"})
	catchup.record(Event{Kind: EventTextDelta, Text: "finished answer"})
	catchup.record(Event{Kind: EventAssistantDone})
	if replay := catchup.replay(); len(replay) != 0 {
		t.Fatalf("catch-up after assistant boundary = %v, want empty", kinds(replay))
	}
}

func eventIndex(events []Event, kind EventKind) int {
	for index, event := range events {
		if event.Kind == kind {
			return index
		}
	}
	return -1
}

func eventCount(events []Event, kind EventKind) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}
